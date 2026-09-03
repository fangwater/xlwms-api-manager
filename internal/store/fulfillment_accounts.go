package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"xlwms-api-manager/internal/model"

	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidFulfillmentAccount = errors.New("invalid fulfillment account configuration")
	ErrOMSAccountRouteNotFound   = errors.New("OMS account route is not configured")
)

type PlatformSKUOMSAccountFilter struct {
	Query    string
	Page     int
	PageSize int
}

func (p *Postgres) ListOMSAccountSummaries(ctx context.Context, includeDisabled bool) ([]model.OMSAccountSummary, error) {
	rows, err := p.pool.Query(ctx, `
SELECT account.account_key,account.account_label,account.account_hint,account.enabled,
       coalesce(array_agg(DISTINCT relation.wh_code ORDER BY relation.wh_code)
           FILTER (WHERE relation.wh_code IS NOT NULL),'{}'::text[]),
       count(DISTINCT route.platform || ':' || route.warehouse_sku),account.updated_at
FROM xlwms_oms_accounts account
LEFT JOIN xlwms_oms_account_warehouses relation ON relation.account_key=account.account_key
LEFT JOIN xlwms_platform_sku_oms_accounts route ON route.account_key=account.account_key
WHERE account.enabled OR $1
GROUP BY account.account_key,account.account_label,account.account_hint,account.enabled,account.updated_at
ORDER BY account.account_key
`, includeDisabled)
	if err != nil {
		return nil, fmt.Errorf("list OMS accounts: %w", err)
	}
	defer rows.Close()
	items := make([]model.OMSAccountSummary, 0)
	for rows.Next() {
		var item model.OMSAccountSummary
		if err := rows.Scan(&item.Key, &item.Label, &item.UsernameHint, &item.Enabled, &item.WarehouseCodes, &item.RouteCount, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan OMS account: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) OMSAccountSummary(ctx context.Context, accountKey string) (model.OMSAccountSummary, error) {
	accountKey = normalizeOMSAccountKey(accountKey)
	var item model.OMSAccountSummary
	err := p.pool.QueryRow(ctx, `
SELECT account.account_key,account.account_label,account.account_hint,account.enabled,
       coalesce(array_agg(DISTINCT relation.wh_code ORDER BY relation.wh_code)
           FILTER (WHERE relation.wh_code IS NOT NULL),'{}'::text[]),
       count(DISTINCT route.platform || ':' || route.warehouse_sku),account.updated_at
FROM xlwms_oms_accounts account
LEFT JOIN xlwms_oms_account_warehouses relation ON relation.account_key=account.account_key
LEFT JOIN xlwms_platform_sku_oms_accounts route ON route.account_key=account.account_key
WHERE account.account_key=$1
GROUP BY account.account_key,account.account_label,account.account_hint,account.enabled,account.updated_at
`, accountKey).Scan(&item.Key, &item.Label, &item.UsernameHint, &item.Enabled, &item.WarehouseCodes, &item.RouteCount, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.OMSAccountSummary{}, ErrOMSAccountNotFound
	}
	if err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("get OMS account summary: %w", err)
	}
	return item, nil
}

func (p *Postgres) OMSAccountWarehouseCodes(ctx context.Context, accountKey string) ([]string, error) {
	accountKey = normalizeOMSAccountKey(accountKey)
	if accountKey == "" {
		return nil, ErrOMSAccountNotFound
	}
	rows, err := p.pool.Query(ctx, `
SELECT relation.wh_code
FROM xlwms_oms_account_warehouses relation
JOIN xlwms_oms_accounts account ON account.account_key=relation.account_key AND account.enabled
WHERE relation.account_key=$1
ORDER BY relation.wh_code
`, accountKey)
	if err != nil {
		return nil, fmt.Errorf("list OMS account warehouses: %w", err)
	}
	defer rows.Close()
	items := make([]string, 0)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan OMS account warehouse: %w", err)
		}
		items = append(items, code)
	}
	return items, rows.Err()
}

func (p *Postgres) ReplaceOMSAccountWarehouses(ctx context.Context, accountKey string, warehouseCodes []string) (model.OMSAccountSummary, error) {
	accountKey = normalizeOMSAccountKey(accountKey)
	warehouseCodes, err := normalizeWarehouseCodes(warehouseCodes)
	if err != nil {
		return model.OMSAccountSummary{}, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("begin OMS account warehouse update: %w", err)
	}
	defer tx.Rollback(ctx)
	var accountExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xlwms_oms_accounts WHERE account_key=$1)`, accountKey).Scan(&accountExists); err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("check OMS account: %w", err)
	}
	if !accountExists {
		return model.OMSAccountSummary{}, ErrOMSAccountNotFound
	}
	if len(warehouseCodes) > 0 {
		var warehouseCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM xlwms_warehouses WHERE wh_code=ANY($1)`, warehouseCodes).Scan(&warehouseCount); err != nil {
			return model.OMSAccountSummary{}, fmt.Errorf("check OMS account warehouses: %w", err)
		}
		if warehouseCount != len(warehouseCodes) {
			return model.OMSAccountSummary{}, fmt.Errorf("%w: unknown warehouse", ErrInvalidFulfillmentAccount)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM xlwms_oms_account_warehouses WHERE account_key=$1`, accountKey); err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("clear OMS account warehouses: %w", err)
	}
	for _, code := range warehouseCodes {
		if _, err := tx.Exec(ctx, `INSERT INTO xlwms_oms_account_warehouses(account_key,wh_code) VALUES($1,$2)`, accountKey, code); err != nil {
			return model.OMSAccountSummary{}, fmt.Errorf("save OMS account warehouse: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("commit OMS account warehouse update: %w", err)
	}
	accounts, err := p.ListOMSAccountSummaries(ctx, true)
	if err != nil {
		return model.OMSAccountSummary{}, err
	}
	for _, account := range accounts {
		if account.Key == accountKey {
			return account, nil
		}
	}
	return model.OMSAccountSummary{}, ErrOMSAccountNotFound
}

func (p *Postgres) ListPlatformSKUOMSAccounts(ctx context.Context, platform string, filter PlatformSKUOMSAccountFilter) ([]model.PlatformSKUOMSAccount, int, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return nil, 0, err
	}
	filter.Page, filter.PageSize = normalizeAccountRoutePage(filter.Page, filter.PageSize)
	filter.Query = strings.TrimSpace(filter.Query)
	search := "%" + filter.Query + "%"
	var total int
	if err := p.pool.QueryRow(ctx, `
SELECT count(*) FROM xlwms_warehouse_sku_specs spec
WHERE spec.enabled AND ($1='' OR spec.warehouse_sku ILIKE $2 OR spec.product_name ILIKE $2)
`, filter.Query, search).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count platform SKU OMS accounts: %w", err)
	}
	rows, err := p.pool.Query(ctx, `
SELECT $1,spec.warehouse_sku,coalesce(spec.product_name,''),coalesce(route.account_key,''),
       coalesce(account.account_label,''),route.account_key IS NOT NULL,
       coalesce(route.updated_at,spec.updated_at)
FROM xlwms_warehouse_sku_specs spec
LEFT JOIN xlwms_platform_sku_oms_accounts route
  ON route.platform=$1 AND route.warehouse_sku=spec.warehouse_sku
LEFT JOIN xlwms_oms_accounts account ON account.account_key=route.account_key AND account.enabled
WHERE spec.enabled AND ($2='' OR spec.warehouse_sku ILIKE $3 OR spec.product_name ILIKE $3)
ORDER BY spec.warehouse_sku
LIMIT $4 OFFSET $5
`, platform, filter.Query, search, filter.PageSize, (filter.Page-1)*filter.PageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list platform SKU OMS accounts: %w", err)
	}
	defer rows.Close()
	items := make([]model.PlatformSKUOMSAccount, 0)
	for rows.Next() {
		var item model.PlatformSKUOMSAccount
		if err := rows.Scan(&item.Platform, &item.WarehouseSKU, &item.ProductName, &item.AccountKey, &item.AccountLabel, &item.Configured, &item.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan platform SKU OMS account: %w", err)
		}
		if item.AccountLabel == "" {
			item.Configured = false
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (p *Postgres) SetPlatformSKUOMSAccount(ctx context.Context, platform, warehouseSKU, accountKey string) (model.PlatformSKUOMSAccount, error) {
	platform, warehouseSKU, accountKey, err := normalizePlatformSKUAccountRoute(platform, warehouseSKU, accountKey)
	if err != nil {
		return model.PlatformSKUOMSAccount{}, err
	}
	var item model.PlatformSKUOMSAccount
	err = p.pool.QueryRow(ctx, `
WITH saved AS (
    INSERT INTO xlwms_platform_sku_oms_accounts(platform,warehouse_sku,account_key,updated_at)
    SELECT $1,spec.warehouse_sku,account.account_key,now()
    FROM xlwms_warehouse_sku_specs spec
    JOIN xlwms_oms_accounts account ON account.account_key=$3 AND account.enabled
    WHERE spec.warehouse_sku=$2 AND spec.enabled
    ON CONFLICT(platform,warehouse_sku) DO UPDATE SET account_key=EXCLUDED.account_key,updated_at=now()
    RETURNING platform,warehouse_sku,account_key,updated_at
)
SELECT saved.platform,saved.warehouse_sku,coalesce(spec.product_name,''),saved.account_key,
       account.account_label,true,saved.updated_at
FROM saved
JOIN xlwms_warehouse_sku_specs spec ON spec.warehouse_sku=saved.warehouse_sku
JOIN xlwms_oms_accounts account ON account.account_key=saved.account_key
`, platform, warehouseSKU, accountKey).Scan(
		&item.Platform, &item.WarehouseSKU, &item.ProductName, &item.AccountKey,
		&item.AccountLabel, &item.Configured, &item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.PlatformSKUOMSAccount{}, fmt.Errorf("%w: SKU or account was not found", ErrInvalidFulfillmentAccount)
	}
	if err != nil {
		return model.PlatformSKUOMSAccount{}, fmt.Errorf("save platform SKU OMS account: %w", err)
	}
	return item, nil
}

func (p *Postgres) DeletePlatformSKUOMSAccount(ctx context.Context, platform, warehouseSKU string) error {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return err
	}
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	if warehouseSKU == "" || len(warehouseSKU) > 255 {
		return fmt.Errorf("%w: invalid warehouse SKU", ErrInvalidFulfillmentAccount)
	}
	_, err = p.pool.Exec(ctx, `DELETE FROM xlwms_platform_sku_oms_accounts WHERE platform=$1 AND warehouse_sku=$2`, platform, warehouseSKU)
	if err != nil {
		return fmt.Errorf("delete platform SKU OMS account: %w", err)
	}
	return nil
}

func (p *Postgres) ResolveFulfillmentAccount(ctx context.Context, platform string, warehouseSKUs []string) (model.FulfillmentAccountDecision, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return model.FulfillmentAccountDecision{}, err
	}
	warehouseSKUs, err = normalizeWarehouseSKUs(warehouseSKUs)
	if err != nil {
		return model.FulfillmentAccountDecision{}, err
	}
	decision := model.FulfillmentAccountDecision{Platform: platform, WarehouseSKUs: warehouseSKUs, WarehouseCodes: []string{}}
	if len(warehouseSKUs) > 1 {
		decision.RequiresManual = true
		decision.DecisionCode = "MANUAL_MULTIPLE_SKUS"
		decision.Reason = "多件 SKU 订单继续转人工处理"
		return decision, nil
	}
	var enabled bool
	err = p.pool.QueryRow(ctx, `
SELECT route.account_key,account.enabled
FROM xlwms_platform_sku_oms_accounts route
JOIN xlwms_oms_accounts account ON account.account_key=route.account_key
WHERE route.platform=$1 AND route.warehouse_sku=$2
`, platform, warehouseSKUs[0]).Scan(&decision.AccountKey, &enabled)
	if errors.Is(err, pgx.ErrNoRows) || !enabled {
		decision.RequiresManual = true
		decision.DecisionCode = "MANUAL_OMS_ACCOUNT_NOT_CONFIGURED"
		decision.Reason = fmt.Sprintf("平台 %s 的 SKU %s 尚未配置 OMS 发货账户", strings.ToUpper(platform), warehouseSKUs[0])
		return decision, nil
	}
	if err != nil {
		return model.FulfillmentAccountDecision{}, fmt.Errorf("resolve platform SKU OMS account: %w", err)
	}
	decision.WarehouseCodes, err = p.OMSAccountWarehouseCodes(ctx, decision.AccountKey)
	if err != nil {
		return model.FulfillmentAccountDecision{}, err
	}
	if len(decision.WarehouseCodes) == 0 {
		decision.RequiresManual = true
		decision.DecisionCode = "MANUAL_OMS_ACCOUNT_HAS_NO_WAREHOUSE"
		decision.Reason = fmt.Sprintf("OMS 账户 %s 尚未配置可操作仓库", decision.AccountKey)
		return decision, nil
	}
	decision.Configured = true
	decision.DecisionCode = "OMS_ACCOUNT_READY"
	decision.Reason = "已按平台和仓库 SKU 确定 OMS 发货账户"
	return decision, nil
}

func normalizeWarehouseCodes(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		code := strings.ToUpper(strings.TrimSpace(value))
		if code == "" || len(code) > 100 {
			return nil, fmt.Errorf("%w: invalid warehouse code", ErrInvalidFulfillmentAccount)
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		result = append(result, code)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeWarehouseSKUs(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		sku := strings.TrimSpace(value)
		if sku == "" || len(sku) > 255 {
			return nil, fmt.Errorf("%w: invalid warehouse SKU", ErrInvalidFulfillmentAccount)
		}
		if _, exists := seen[sku]; exists {
			continue
		}
		seen[sku] = struct{}{}
		result = append(result, sku)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: warehouse SKU is required", ErrInvalidFulfillmentAccount)
	}
	sort.Strings(result)
	return result, nil
}

func normalizePlatformSKUAccountRoute(platform, warehouseSKU, accountKey string) (string, string, string, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return "", "", "", err
	}
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	accountKey = normalizeOMSAccountKey(accountKey)
	if warehouseSKU == "" || len(warehouseSKU) > 255 || accountKey == "" || len(accountKey) > 100 {
		return "", "", "", fmt.Errorf("%w: platform, warehouse SKU, and account are required", ErrInvalidFulfillmentAccount)
	}
	return platform, warehouseSKU, accountKey, nil
}

func normalizeAccountRoutePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 30
	}
	return page, pageSize
}
