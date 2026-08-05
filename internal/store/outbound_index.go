package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"xlwms-api-manager/internal/model"
)

type OutboundOrderFilter struct {
	WarehouseCode string
	Query         string
	Page          int
	PageSize      int
}

func (p *Postgres) OutboundSyncWatermark(ctx context.Context, accountKey string) (*time.Time, error) {
	var watermark *time.Time
	if err := p.pool.QueryRow(ctx, `
SELECT watermark_at FROM xlwms_outbound_sync_watermarks WHERE account_key=$1
`, accountKey).Scan(&watermark); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get outbound sync watermark: %w", err)
	}
	return watermark, nil
}

func (p *Postgres) SaveOutboundSyncWindow(ctx context.Context, accountKey, credentialCode string, start, through time.Time, records []model.OutboundOrderIndex) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, record := range records {
		if err = upsertOutboundOrder(ctx, tx, accountKey, record); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `
INSERT INTO xlwms_outbound_sync_windows (
account_key,window_start,window_end,record_count,completed_at
) VALUES ($1,$2,$3,$4,now())
ON CONFLICT (account_key,window_start) DO UPDATE SET
window_end=EXCLUDED.window_end,record_count=EXCLUDED.record_count,completed_at=now()
`, accountKey, start, through, len(records)); err != nil {
		return fmt.Errorf("record outbound sync window: %w", err)
	}
	if _, err = tx.Exec(ctx, `
INSERT INTO xlwms_outbound_sync_watermarks (
account_key,credential_code,coverage_started_at,watermark_at,last_completed_at,last_error,updated_at
) VALUES ($1,$2,$3,$4,now(),'',now())
ON CONFLICT (account_key) DO UPDATE SET
credential_code=EXCLUDED.credential_code,
coverage_started_at=least(coalesce(xlwms_outbound_sync_watermarks.coverage_started_at,EXCLUDED.coverage_started_at),EXCLUDED.coverage_started_at),
watermark_at=greatest(coalesce(xlwms_outbound_sync_watermarks.watermark_at,EXCLUDED.watermark_at),EXCLUDED.watermark_at),
last_completed_at=now(),last_error='',updated_at=now()
`, accountKey, strings.ToUpper(strings.TrimSpace(credentialCode)), start, through); err != nil {
		return fmt.Errorf("advance outbound sync watermark: %w", err)
	}
	return tx.Commit(ctx)
}

func (p *Postgres) SaveOutboundStatusRecords(ctx context.Context, accountKey string, records []model.OutboundOrderIndex) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, record := range records {
		if err := upsertOutboundOrder(ctx, tx, accountKey, record); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func upsertOutboundOrder(ctx context.Context, tx pgx.Tx, accountKey string, record model.OutboundOrderIndex) error {
	record.OutboundOrderNo = strings.TrimSpace(record.OutboundOrderNo)
	if record.OutboundOrderNo == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `
INSERT INTO xlwms_outbound_order_index (
account_key,wh_code,outbound_order_no,third_order_no,refer_order_no,platform_order_no,
status,tracking_number,order_created_at,outbound_at,last_seen_at,updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now(),now())
ON CONFLICT (account_key,outbound_order_no) DO UPDATE SET
wh_code=coalesce(nullif(EXCLUDED.wh_code,''),xlwms_outbound_order_index.wh_code),
third_order_no=coalesce(nullif(EXCLUDED.third_order_no,''),xlwms_outbound_order_index.third_order_no),
refer_order_no=coalesce(nullif(EXCLUDED.refer_order_no,''),xlwms_outbound_order_index.refer_order_no),
platform_order_no=coalesce(nullif(EXCLUDED.platform_order_no,''),xlwms_outbound_order_index.platform_order_no),
status=EXCLUDED.status,
tracking_number=coalesce(nullif(EXCLUDED.tracking_number,''),xlwms_outbound_order_index.tracking_number),
order_created_at=coalesce(EXCLUDED.order_created_at,xlwms_outbound_order_index.order_created_at),
outbound_at=coalesce(EXCLUDED.outbound_at,xlwms_outbound_order_index.outbound_at),
last_seen_at=now(),updated_at=now()
`, accountKey, strings.ToUpper(strings.TrimSpace(record.WarehouseCode)), record.OutboundOrderNo,
		strings.TrimSpace(record.ThirdOrderNo), strings.TrimSpace(record.ReferOrderNo),
		strings.TrimSpace(record.PlatformOrderNo), record.Status, strings.TrimSpace(record.TrackingNumber),
		record.OrderCreatedAt, record.OutboundAt)
	if err != nil {
		return fmt.Errorf("upsert outbound order %s: %w", record.OutboundOrderNo, err)
	}
	return nil
}

func (p *Postgres) OutboundWindowCovered(ctx context.Context, accountKey string, start time.Time) (bool, error) {
	var covered bool
	if err := p.pool.QueryRow(ctx, `
SELECT EXISTS(
    SELECT 1 FROM xlwms_outbound_sync_windows
    WHERE account_key=$1 AND window_start=$2 AND window_end >= $2+interval '1 hour'
)
`, accountKey, start).Scan(&covered); err != nil {
		return false, fmt.Errorf("check outbound sync window: %w", err)
	}
	return covered, nil
}

func (p *Postgres) OutboundSyncCoverage(ctx context.Context) (*time.Time, *time.Time, error) {
	var start, through *time.Time
	if err := p.pool.QueryRow(ctx, `
SELECT max(coverage_started_at),min(watermark_at)
FROM xlwms_outbound_sync_watermarks WHERE watermark_at IS NOT NULL
`).Scan(&start, &through); err != nil {
		return nil, nil, fmt.Errorf("get outbound sync coverage: %w", err)
	}
	return start, through, nil
}

func (p *Postgres) MarkOutboundSyncFailure(ctx context.Context, accountKey, credentialCode string, syncErr error) error {
	message := ""
	if syncErr != nil {
		message = syncErr.Error()
	}
	_, err := p.pool.Exec(ctx, `
INSERT INTO xlwms_outbound_sync_watermarks (account_key,credential_code,last_error,updated_at)
VALUES ($1,$2,$3,now())
ON CONFLICT (account_key) DO UPDATE SET
credential_code=EXCLUDED.credential_code,last_error=EXCLUDED.last_error,updated_at=now()
`, accountKey, strings.ToUpper(strings.TrimSpace(credentialCode)), message)
	return err
}

func (p *Postgres) LatestOutboundQueryEvent(ctx context.Context) (*time.Time, error) {
	var value *time.Time
	if err := p.pool.QueryRow(ctx, `SELECT max(watermark_at) FROM xlwms_outbound_sync_watermarks`).Scan(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func (p *Postgres) OutboundOrdersByReferences(ctx context.Context, references []string) ([]model.OutboundOrderIndex, error) {
	normalized := normalizedOrderReferences(references)
	if len(normalized) == 0 {
		return nil, nil
	}
	rows, err := p.pool.Query(ctx, outboundOrderIndexSelect+`
WHERE upper(platform_order_no)=ANY($1) OR upper(third_order_no)=ANY($1)
   OR upper(refer_order_no)=ANY($1) OR upper(outbound_order_no)=ANY($1)
ORDER BY order_created_at DESC NULLS LAST,updated_at DESC`, normalized)
	if err != nil {
		return nil, fmt.Errorf("find outbound orders by reference: %w", err)
	}
	defer rows.Close()
	return scanOutboundOrderIndex(rows)
}

func (p *Postgres) ListOutboundOrders(ctx context.Context, filter OutboundOrderFilter) ([]model.OutboundOrderIndex, int, *time.Time, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 30
	}
	where := []string{"true"}
	args := make([]any, 0, 4)
	add := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if warehouse := strings.ToUpper(strings.TrimSpace(filter.WarehouseCode)); warehouse != "" {
		where = append(where, "wh_code="+add(warehouse))
	}
	if references := normalizedOrderReferences(strings.FieldsFunc(filter.Query, func(r rune) bool { return r == ',' || r == '，' || r == '\n' })); len(references) > 0 {
		placeholder := add(references)
		where = append(where, "(upper(platform_order_no)=ANY("+placeholder+") OR upper(third_order_no)=ANY("+placeholder+") OR upper(refer_order_no)=ANY("+placeholder+") OR upper(outbound_order_no)=ANY("+placeholder+"))")
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM xlwms_outbound_order_index WHERE "+clause, args...).Scan(&total); err != nil {
		return nil, 0, nil, err
	}
	watermark, err := p.LatestOutboundQueryEvent(ctx)
	if err != nil {
		return nil, 0, nil, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := p.pool.Query(ctx, outboundOrderIndexSelect+` WHERE `+clause+`
ORDER BY order_created_at DESC NULLS LAST,updated_at DESC
LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, watermark, err
	}
	defer rows.Close()
	items, err := scanOutboundOrderIndex(rows)
	return items, total, watermark, err
}

func normalizedOrderReferences(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

const outboundOrderIndexSelect = `SELECT wh_code,outbound_order_no,third_order_no,refer_order_no,
platform_order_no,status,tracking_number,order_created_at,outbound_at
FROM xlwms_outbound_order_index `

type outboundOrderRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanOutboundOrderIndex(rows outboundOrderRows) ([]model.OutboundOrderIndex, error) {
	items := make([]model.OutboundOrderIndex, 0)
	for rows.Next() {
		var item model.OutboundOrderIndex
		if err := rows.Scan(&item.WarehouseCode, &item.OutboundOrderNo, &item.ThirdOrderNo,
			&item.ReferOrderNo, &item.PlatformOrderNo, &item.Status, &item.TrackingNumber,
			&item.OrderCreatedAt, &item.OutboundAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
