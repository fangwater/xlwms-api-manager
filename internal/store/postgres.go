package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xlwms-api-manager/internal/credentials"
	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/migrations"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool   *pgxpool.Pool
	cipher *credentials.Cipher
}

func NewPostgres(ctx context.Context, databaseURL string, cipher *credentials.Cipher) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return &Postgres{pool: pool, cipher: cipher}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

func (p *Postgres) Migrate(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, migrations.InitSQL); err != nil {
		return fmt.Errorf("apply XLWMS migration: %w", err)
	}
	return nil
}

func (p *Postgres) UpsertWarehouse(ctx context.Context, code, name, apiBaseURL, appKey, appSecret string, active bool) (model.WarehouseSummary, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	appKey = strings.TrimSpace(appKey)
	appSecret = strings.TrimSpace(appSecret)
	apiBaseURL = strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	if code == "" || appKey == "" || appSecret == "" || apiBaseURL == "" {
		return model.WarehouseSummary{}, errors.New("wh_code, api_base_url, app_key and app_secret are required")
	}
	appKeyCiphertext, err := p.cipher.Encrypt(appKey)
	if err != nil {
		return model.WarehouseSummary{}, err
	}
	appSecretCiphertext, err := p.cipher.Encrypt(appSecret)
	if err != nil {
		return model.WarehouseSummary{}, err
	}
	var warehouse model.WarehouseSummary
	err = p.pool.QueryRow(ctx, `
		INSERT INTO xlwms_warehouses (
			wh_code, warehouse_name, api_base_url, app_key_ciphertext,
			app_secret_ciphertext, app_key_hint, is_active, disabled_at, updated_at
		) VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7,
			CASE WHEN $7 THEN NULL ELSE now() END, now())
		ON CONFLICT (wh_code) DO UPDATE SET
			warehouse_name = EXCLUDED.warehouse_name,
			api_base_url = EXCLUDED.api_base_url,
			app_key_ciphertext = EXCLUDED.app_key_ciphertext,
			app_secret_ciphertext = EXCLUDED.app_secret_ciphertext,
			app_key_hint = EXCLUDED.app_key_hint,
			is_active = EXCLUDED.is_active,
			disabled_at = CASE WHEN EXCLUDED.is_active THEN NULL ELSE coalesce(xlwms_warehouses.disabled_at, now()) END,
			updated_at = now()
		RETURNING wh_code, coalesce(warehouse_name, ''), api_base_url, app_key_hint, is_active, updated_at
	`, code, strings.TrimSpace(name), apiBaseURL, appKeyCiphertext, appSecretCiphertext, credentials.MaskAppKey(appKey), active).Scan(
		&warehouse.Code, &warehouse.Name, &warehouse.APIBaseURL, &warehouse.AppKeyHint, &warehouse.Active, &warehouse.UpdatedAt,
	)
	if err != nil {
		return model.WarehouseSummary{}, fmt.Errorf("upsert warehouse: %w", err)
	}
	return warehouse, nil
}

func (p *Postgres) ListWarehouses(ctx context.Context, activeOnly bool) ([]model.WarehouseSummary, error) {
	where := ""
	if activeOnly {
		where = "WHERE is_active"
	}
	rows, err := p.pool.Query(ctx, `
		SELECT wh_code, coalesce(warehouse_name, ''), api_base_url, app_key_hint, is_active, updated_at
		FROM xlwms_warehouses `+where+` ORDER BY wh_code
	`)
	if err != nil {
		return nil, fmt.Errorf("list warehouses: %w", err)
	}
	defer rows.Close()
	warehouses := make([]model.WarehouseSummary, 0)
	for rows.Next() {
		var warehouse model.WarehouseSummary
		if err := rows.Scan(&warehouse.Code, &warehouse.Name, &warehouse.APIBaseURL, &warehouse.AppKeyHint, &warehouse.Active, &warehouse.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan warehouse: %w", err)
		}
		warehouses = append(warehouses, warehouse)
	}
	return warehouses, rows.Err()
}

func (p *Postgres) SetWarehouseActive(ctx context.Context, code string, active bool) (model.WarehouseSummary, error) {
	var warehouse model.WarehouseSummary
	err := p.pool.QueryRow(ctx, `
		UPDATE xlwms_warehouses SET
			is_active = $1,
			disabled_at = CASE WHEN $1 THEN NULL ELSE now() END,
			updated_at = now()
		WHERE wh_code = $2
		RETURNING wh_code, coalesce(warehouse_name, ''), api_base_url, app_key_hint, is_active, updated_at
	`, active, strings.ToUpper(strings.TrimSpace(code))).Scan(
		&warehouse.Code, &warehouse.Name, &warehouse.APIBaseURL, &warehouse.AppKeyHint, &warehouse.Active, &warehouse.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.WarehouseSummary{}, errors.New("unknown warehouse")
	}
	if err != nil {
		return model.WarehouseSummary{}, fmt.Errorf("update warehouse: %w", err)
	}
	return warehouse, nil
}

func (p *Postgres) WarehouseCredentials(ctx context.Context, code string, requireActive bool) (model.WarehouseCredentials, error) {
	var result model.WarehouseCredentials
	var appKeyCiphertext, appSecretCiphertext string
	err := p.pool.QueryRow(ctx, `
		SELECT wh_code, coalesce(warehouse_name, ''), api_base_url, app_key_hint, is_active, updated_at,
		       app_key_ciphertext, app_secret_ciphertext
		FROM xlwms_warehouses WHERE wh_code = $1
	`, strings.ToUpper(strings.TrimSpace(code))).Scan(
		&result.Code, &result.Name, &result.APIBaseURL, &result.AppKeyHint, &result.Active, &result.UpdatedAt,
		&appKeyCiphertext, &appSecretCiphertext,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.WarehouseCredentials{}, errors.New("unknown warehouse")
	}
	if err != nil {
		return model.WarehouseCredentials{}, fmt.Errorf("get warehouse: %w", err)
	}
	if requireActive && !result.Active {
		return model.WarehouseCredentials{}, errors.New("warehouse is disabled")
	}
	if result.AppKey, err = p.cipher.Decrypt(appKeyCiphertext); err != nil {
		return model.WarehouseCredentials{}, fmt.Errorf("decrypt app key for %s: %w", result.Code, err)
	}
	if result.AppSecret, err = p.cipher.Decrypt(appSecretCiphertext); err != nil {
		return model.WarehouseCredentials{}, fmt.Errorf("decrypt app secret for %s: %w", result.Code, err)
	}
	return result, nil
}

func (p *Postgres) ActiveWarehouseCredentials(ctx context.Context) ([]model.WarehouseCredentials, error) {
	warehouses, err := p.ListWarehouses(ctx, true)
	if err != nil {
		return nil, err
	}
	result := make([]model.WarehouseCredentials, 0, len(warehouses))
	for _, warehouse := range warehouses {
		item, err := p.WarehouseCredentials(ctx, warehouse.Code, true)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

type FundsFlowFilter struct {
	WarehouseCode string
	Query         string
	DetailStatus  string
	StartDate     string
	EndDate       string
	Page          int
	PageSize      int
}

type CostDetailFilter struct {
	WarehouseCode string
	Query         string
	StartDate     string
	EndDate       string
	Page          int
	PageSize      int
}

func appendDateRangeFilters(where []string, args []any, column, startDate, endDate string) ([]string, []any) {
	add := func(value string) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if startDate = strings.TrimSpace(startDate); startDate != "" {
		where = append(where, column+" >= "+add(startDate)+"::date")
	}
	if endDate = strings.TrimSpace(endDate); endDate != "" {
		where = append(where, column+" < ("+add(endDate)+"::date + interval '1 day')")
	}
	return where, args
}

func (p *Postgres) ListFundsFlows(ctx context.Context, filter FundsFlowFilter) ([]model.FundsFlow, int, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 30
	}
	where := []string{"1=1"}
	args := make([]any, 0, 6)
	add := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if code := strings.ToUpper(strings.TrimSpace(filter.WarehouseCode)); code != "" {
		where = append(where, "wh_code = "+add(code))
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		placeholder := add("%" + query + "%")
		where = append(where, "(coalesce(order_no, '') ILIKE "+placeholder+" OR coalesce(platform_order_no, '') ILIKE "+placeholder+")")
	}
	if status := strings.TrimSpace(filter.DetailStatus); status != "" {
		where = append(where, "detail_sync_status = "+add(status))
	}
	where, args = appendDateRangeFilters(where, args, "cost_time", filter.StartDate, filter.EndDate)
	clause := strings.Join(where, " AND ")
	var total int
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM xlwms_funds_flows WHERE "+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count funds flows: %w", err)
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := p.pool.Query(ctx, `
		SELECT id, wh_code, coalesce(order_no, ''), coalesce(platform_order_no, ''),
		       coalesce(cost_total, 0)::float8, coalesce(currency_code, ''), cost_status,
		       module_type, cost_time, bill_status, coalesce(relate_bill_no, ''),
		       detail_sync_status, detail_attempts, coalesce(detail_error_message, '')
		FROM xlwms_funds_flows WHERE `+clause+`
		ORDER BY cost_time DESC NULLS LAST, id DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list funds flows: %w", err)
	}
	defer rows.Close()
	items := make([]model.FundsFlow, 0, filter.PageSize)
	for rows.Next() {
		var item model.FundsFlow
		if err := rows.Scan(
			&item.ID, &item.WarehouseCode, &item.OrderNo, &item.PlatformOrderNo,
			&item.CostTotal, &item.CurrencyCode, &item.CostStatus, &item.ModuleType,
			&item.CostTime, &item.BillStatus, &item.RelateBillNo,
			&item.DetailSyncStatus, &item.DetailAttempts, &item.DetailError,
		); err != nil {
			return nil, 0, fmt.Errorf("scan funds flow: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (p *Postgres) ListCostDetails(ctx context.Context, filter CostDetailFilter) ([]model.CostDetail, int, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 30
	}
	where := []string{"1=1"}
	args := make([]any, 0, 7)
	add := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	if code := strings.ToUpper(strings.TrimSpace(filter.WarehouseCode)); code != "" {
		where = append(where, "d.wh_code = "+add(code))
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		placeholder := add("%" + query + "%")
		where = append(where, "(d.cost_no ILIKE "+placeholder+" OR d.query_order_no ILIKE "+placeholder+" OR coalesce(d.platform_order_no, '') ILIKE "+placeholder+")")
	}
	where, args = appendDateRangeFilters(where, args, "d.create_time", filter.StartDate, filter.EndDate)
	clause := strings.Join(where, " AND ")
	var total int
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM xlwms_cost_details d WHERE "+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := p.pool.Query(ctx, `
		SELECT d.wh_code, d.cost_no, d.query_order_no, coalesce(d.cost_total, 0)::float8,
		       coalesce(d.currency_code, ''), d.module_type, d.cost_status, d.bill_status,
		       d.create_time, coalesce(d.platform_order_no, ''), count(i.id)::int
		FROM xlwms_cost_details d
		LEFT JOIN xlwms_cost_items i ON i.wh_code=d.wh_code AND i.cost_no=d.cost_no
		WHERE `+clause+`
		GROUP BY d.wh_code, d.cost_no
		ORDER BY d.create_time DESC NULLS LAST, d.cost_no
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]model.CostDetail, 0, filter.PageSize)
	for rows.Next() {
		var item model.CostDetail
		if err := rows.Scan(&item.WarehouseCode, &item.CostNo, &item.QueryOrderNo, &item.CostTotal, &item.CurrencyCode, &item.ModuleType, &item.CostStatus, &item.BillStatus, &item.CreateTime, &item.PlatformOrderNo, &item.ItemCount); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (p *Postgres) CostItems(ctx context.Context, warehouseCode, costNo string) ([]model.CostItem, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT coalesce(bill_item_name, ''), coalesce(bill_item_total, 0)::float8, charge_time
		FROM xlwms_cost_items WHERE wh_code=$1 AND cost_no=$2 ORDER BY item_index
	`, strings.ToUpper(strings.TrimSpace(warehouseCode)), strings.TrimSpace(costNo))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.CostItem, 0)
	for rows.Next() {
		var item model.CostItem
		if err := rows.Scan(&item.Name, &item.Total, &item.ChargeTime); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type DashboardSummary struct {
	ActiveWarehouses int                `json:"active_warehouses"`
	TotalWarehouses  int                `json:"total_warehouses"`
	FundsFlows       int                `json:"funds_flows"`
	CostDetails      int                `json:"cost_details"`
	PendingDetails   int                `json:"pending_details"`
	FailedDetails    int                `json:"failed_details"`
	CostByCurrency   map[string]float64 `json:"cost_by_currency"`
	Trend            []TrendPoint       `json:"trend"`
}

type TrendPoint struct {
	Date   string  `json:"date"`
	Amount float64 `json:"amount"`
}

func (p *Postgres) Dashboard(ctx context.Context, warehouseCode string, days int) (DashboardSummary, error) {
	if days < 1 || days > 90 {
		days = 14
	}
	var result DashboardSummary
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE is_active), count(*) FROM xlwms_warehouses`).Scan(&result.ActiveWarehouses, &result.TotalWarehouses); err != nil {
		return result, err
	}
	code := strings.ToUpper(strings.TrimSpace(warehouseCode))
	if err := p.pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE detail_sync_status='pending'), count(*) FILTER (WHERE detail_sync_status='error')
		FROM xlwms_funds_flows WHERE ($1='' OR wh_code=$1)
	`, code).Scan(&result.FundsFlows, &result.PendingDetails, &result.FailedDetails); err != nil {
		return result, err
	}
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM xlwms_cost_details WHERE ($1='' OR wh_code=$1)`, code).Scan(&result.CostDetails); err != nil {
		return result, err
	}
	result.CostByCurrency = make(map[string]float64)
	rows, err := p.pool.Query(ctx, `
		SELECT coalesce(currency_code, '未知'), coalesce(sum(cost_total), 0)::float8
		FROM xlwms_funds_flows WHERE ($1='' OR wh_code=$1) GROUP BY currency_code ORDER BY currency_code
	`, code)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var currency string
		var total float64
		if err := rows.Scan(&currency, &total); err != nil {
			rows.Close()
			return result, err
		}
		result.CostByCurrency[currency] = total
	}
	rows.Close()
	rows, err = p.pool.Query(ctx, `
		SELECT cost_time::date::text, count(*)::float8
		FROM xlwms_funds_flows
		WHERE ($1='' OR wh_code=$1) AND cost_time >= current_date - ($2::int - 1)
		GROUP BY cost_time::date ORDER BY cost_time::date
	`, code, days)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	result.Trend = make([]TrendPoint, 0, days)
	for rows.Next() {
		var point TrendPoint
		if err := rows.Scan(&point.Date, &point.Amount); err != nil {
			return result, err
		}
		result.Trend = append(result.Trend, point)
	}
	return result, rows.Err()
}

func FundsFlowSourceKey(warehouseCode string, record map[string]any, occurrence int) (string, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	payloadHash := sha256.Sum256(raw)
	identity := fmt.Sprintf("%s:%s:%d", strings.ToUpper(strings.TrimSpace(warehouseCode)), hex.EncodeToString(payloadHash[:]), occurrence)
	result := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(result[:]), nil
}

func (p *Postgres) StartSyncRun(ctx context.Context, warehouseCode, target string) (model.SyncRun, error) {
	var run model.SyncRun
	err := p.pool.QueryRow(ctx, `
		INSERT INTO xlwms_sync_runs (wh_code, target, status) VALUES ($1, $2, 'running')
		RETURNING id, coalesce(wh_code, ''), target, status, started_at
	`, nullIfEmpty(strings.ToUpper(strings.TrimSpace(warehouseCode))), target).Scan(&run.ID, &run.WarehouseCode, &run.Target, &run.Status, &run.StartedAt)
	return run, err
}

func (p *Postgres) FinishSyncRun(ctx context.Context, run model.SyncRun, syncErr error) error {
	status := "succeeded"
	errorMessage := ""
	if syncErr != nil {
		status = "failed"
		errorMessage = syncErr.Error()
	}
	_, err := p.pool.Exec(ctx, `
		UPDATE xlwms_sync_runs SET status=$2, pages=$3, records_seen=$4, records_saved=$5,
			targets=$6, succeeded=$7, failed=$8, cost_items=$9,
			error_message=NULLIF(left($10, 1000), ''), finished_at=now()
		WHERE id=$1
	`, run.ID, status, run.Pages, run.RecordsSeen, run.RecordsSaved, run.Targets, run.Succeeded, run.Failed, run.CostItems, errorMessage)
	return err
}

func (p *Postgres) SyncRun(ctx context.Context, id int64) (model.SyncRun, error) {
	var run model.SyncRun
	err := p.pool.QueryRow(ctx, `
		SELECT id, coalesce(wh_code, ''), target, status, pages, records_seen, records_saved,
		       targets, succeeded, failed, cost_items, coalesce(error_message, ''), started_at, finished_at
		FROM xlwms_sync_runs WHERE id=$1
	`, id).Scan(&run.ID, &run.WarehouseCode, &run.Target, &run.Status, &run.Pages, &run.RecordsSeen, &run.RecordsSaved, &run.Targets, &run.Succeeded, &run.Failed, &run.CostItems, &run.Error, &run.StartedAt, &run.FinishedAt)
	return run, err
}

func (p *Postgres) RecentSyncRuns(ctx context.Context, limit int) ([]model.SyncRun, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, coalesce(wh_code, ''), target, status, pages, records_seen, records_saved,
		       targets, succeeded, failed, cost_items, coalesce(error_message, ''), started_at, finished_at
		FROM xlwms_sync_runs ORDER BY started_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.SyncRun, 0, limit)
	for rows.Next() {
		var run model.SyncRun
		if err := rows.Scan(&run.ID, &run.WarehouseCode, &run.Target, &run.Status, &run.Pages, &run.RecordsSeen, &run.RecordsSaved, &run.Targets, &run.Succeeded, &run.Failed, &run.CostItems, &run.Error, &run.StartedAt, &run.FinishedAt); err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
