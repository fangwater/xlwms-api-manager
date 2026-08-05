package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"xlwms-api-manager/internal/model"
)

type FulfillmentAuditFilter struct {
	Archived          bool
	ShopCode          string
	WarehouseCode     string
	ExceptionCategory string
	OMSStatus         string
	Query             string
	Page              int
	PageSize          int
}

func fulfillmentAuditWhere(filter FulfillmentAuditFilter) (string, []any) {
	where := []string{"active"}
	if filter.Archived {
		where = []string{"NOT active", "oms_status='outbound'"}
	}
	args := make([]any, 0, 6)
	add := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if value := strings.ToLower(strings.TrimSpace(filter.ShopCode)); value != "" {
		where = append(where, "shop_code="+add(value))
	}
	if value := strings.ToUpper(strings.TrimSpace(filter.WarehouseCode)); value != "" {
		where = append(where, "wh_code="+add(value))
	}
	if value := strings.TrimSpace(filter.ExceptionCategory); value != "" {
		where = append(where, "exception_category="+add(value))
	}
	if value := strings.TrimSpace(filter.OMSStatus); value != "" {
		where = append(where, "oms_status="+add(value))
	}
	if value := strings.TrimSpace(filter.Query); value != "" {
		placeholder := add("%" + value + "%")
		where = append(where, "(platform_order_no ILIKE "+placeholder+" OR outbound_order_no ILIKE "+placeholder+" OR tracking_number ILIKE "+placeholder+" OR oms_tracking_number ILIKE "+placeholder+")")
	}
	return strings.Join(where, " AND "), args
}

func (p *Postgres) FulfillmentAuditShops(ctx context.Context, archived bool) ([]model.FulfillmentAuditShop, error) {
	condition := "active"
	if archived {
		condition = "NOT active AND oms_status='outbound'"
	}
	rows, err := p.pool.Query(ctx, `
SELECT shop_code,max(shop_name) FROM xlwms_fulfillment_audits
WHERE `+condition+` GROUP BY shop_code ORDER BY max(shop_name),shop_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.FulfillmentAuditShop, 0)
	for rows.Next() {
		var item model.FulfillmentAuditShop
		if err := rows.Scan(&item.Code, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) ReplaceFulfillmentAuditSnapshot(ctx context.Context, platform, shopCode, shopName string, items []model.FulfillmentAuditSnapshotItem) (int, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	shopCode = strings.ToLower(strings.TrimSpace(shopCode))
	shopName = strings.TrimSpace(shopName)
	if platform == "" || shopCode == "" {
		return 0, errors.New("platform and shop_code are required")
	}
	if len(items) > 5000 {
		return 0, errors.New("snapshot cannot contain more than 5000 orders")
	}
	cleaned := make([]model.FulfillmentAuditSnapshotItem, 0, len(items))
	orderNos := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item.PlatformOrderNo = strings.TrimSpace(item.PlatformOrderNo)
		if item.PlatformOrderNo == "" {
			return 0, errors.New("platform_order_no is required")
		}
		if _, exists := seen[item.PlatformOrderNo]; exists {
			continue
		}
		seen[item.PlatformOrderNo] = struct{}{}
		item.PlatformStatus = strings.TrimSpace(item.PlatformStatus)
		if item.PlatformStatus == "" {
			item.PlatformStatus = "pending_pickup"
		}
		item.WarehouseKey = strings.TrimSpace(item.WarehouseKey)
		item.WarehouseCode = strings.ToUpper(strings.TrimSpace(item.WarehouseCode))
		item.TrackingNumber = strings.TrimSpace(item.TrackingNumber)
		cleaned = append(cleaned, item)
		orderNos = append(orderNos, item.PlatformOrderNo)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	for _, item := range cleaned {
		_, err = tx.Exec(ctx, `
INSERT INTO xlwms_fulfillment_audits (
platform,shop_code,shop_name,platform_order_no,platform_status,
platform_status_code,platform_shipping_at,warehouse_key,wh_code,
tracking_number,oms_status,exception_category,active,last_seen_at,updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending_query','pending_query',true,now(),now())
ON CONFLICT (platform,shop_code,platform_order_no) DO UPDATE SET
shop_name=EXCLUDED.shop_name,
platform_status=EXCLUDED.platform_status,
platform_status_code=EXCLUDED.platform_status_code,
platform_shipping_at=coalesce(EXCLUDED.platform_shipping_at,xlwms_fulfillment_audits.platform_shipping_at),
warehouse_key=coalesce(nullif(EXCLUDED.warehouse_key,''),xlwms_fulfillment_audits.warehouse_key),
wh_code=coalesce(nullif(EXCLUDED.wh_code,''),xlwms_fulfillment_audits.wh_code),
tracking_number=coalesce(nullif(EXCLUDED.tracking_number,''),xlwms_fulfillment_audits.tracking_number),
oms_status=CASE
WHEN xlwms_fulfillment_audits.oms_status='outbound'
THEN xlwms_fulfillment_audits.oms_status
WHEN nullif(EXCLUDED.wh_code,'') IS NOT NULL
 AND nullif(EXCLUDED.wh_code,'') IS DISTINCT FROM nullif(xlwms_fulfillment_audits.wh_code,'')
THEN 'pending_query' ELSE xlwms_fulfillment_audits.oms_status END,
last_checked_at=CASE
WHEN xlwms_fulfillment_audits.oms_status='outbound'
THEN xlwms_fulfillment_audits.last_checked_at
WHEN nullif(EXCLUDED.wh_code,'') IS NOT NULL
 AND nullif(EXCLUDED.wh_code,'') IS DISTINCT FROM nullif(xlwms_fulfillment_audits.wh_code,'')
THEN NULL ELSE xlwms_fulfillment_audits.last_checked_at END,
exception_category=CASE
WHEN xlwms_fulfillment_audits.oms_status='outbound' THEN 'archived'
WHEN nullif(EXCLUDED.wh_code,'') IS NOT NULL
 AND nullif(EXCLUDED.wh_code,'') IS DISTINCT FROM nullif(xlwms_fulfillment_audits.wh_code,'')
THEN 'pending_query' ELSE xlwms_fulfillment_audits.exception_category END,
active=xlwms_fulfillment_audits.oms_status<>'outbound',
resolved_at=CASE WHEN xlwms_fulfillment_audits.oms_status='outbound'
THEN coalesce(xlwms_fulfillment_audits.resolved_at,now()) ELSE NULL END,
last_seen_at=now(),updated_at=now()
`, platform, shopCode, shopName, item.PlatformOrderNo, item.PlatformStatus,
			item.PlatformStatusCode, item.PlatformShippingAt, item.WarehouseKey,
			item.WarehouseCode, item.TrackingNumber)
		if err != nil {
			return 0, fmt.Errorf("upsert fulfillment audit %s: %w", item.PlatformOrderNo, err)
		}
	}
	if _, err = tx.Exec(ctx, `
UPDATE xlwms_fulfillment_audits SET
active=false,resolved_at=now(),updated_at=now()
WHERE platform=$1 AND shop_code=$2 AND active
  AND NOT (platform_order_no=ANY($3))
`, platform, shopCode, orderNos); err != nil {
		return 0, fmt.Errorf("resolve stale fulfillment audits: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(cleaned), nil
}

func (p *Postgres) FulfillmentAuditCandidates(ctx context.Context, limit int) ([]model.FulfillmentAudit, error) {
	if limit < 1 || limit > 5000 {
		limit = 5000
	}
	rows, err := p.pool.Query(ctx, fulfillmentAuditSelect+`
WHERE active AND oms_status<>'outbound'
  AND (last_checked_at IS NULL OR last_checked_at < now()-interval '10 minutes')
ORDER BY last_checked_at NULLS FIRST,(wh_code=''),updated_at,id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list fulfillment audit candidates: %w", err)
	}
	defer rows.Close()
	return scanFulfillmentAudits(rows)
}

func (p *Postgres) FulfillmentAuditStatusCandidates(ctx context.Context, limit int) ([]model.FulfillmentAudit, error) {
	if limit < 1 || limit > 5000 {
		limit = 5000
	}
	rows, err := p.pool.Query(ctx, fulfillmentAuditSelect+`
WHERE active AND outbound_order_no<>'' AND oms_status<>'outbound'
ORDER BY coalesce(last_checked_at,first_seen_at),id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list fulfillment status candidates: %w", err)
	}
	defer rows.Close()
	return scanFulfillmentAudits(rows)
}

func (p *Postgres) FulfillmentAuditsByPlatformOrderNos(ctx context.Context, orderNos []string) ([]model.FulfillmentAudit, error) {
	orderNos = normalizedOrderReferences(orderNos)
	if len(orderNos) == 0 {
		return nil, nil
	}
	rows, err := p.pool.Query(ctx, fulfillmentAuditSelect+`
WHERE active AND upper(platform_order_no)=ANY($1)
ORDER BY id`, orderNos)
	if err != nil {
		return nil, fmt.Errorf("list fulfillment audits by platform order: %w", err)
	}
	defer rows.Close()
	return scanFulfillmentAudits(rows)
}

func (p *Postgres) FulfillmentAuditCoverageHours(ctx context.Context, before time.Time, limit int) ([]time.Time, error) {
	if limit < 1 || limit > 5000 {
		limit = 5000
	}
	rows, err := p.pool.Query(ctx, `
SELECT DISTINCT date_trunc('hour',platform_shipping_at) AS window_start
FROM xlwms_fulfillment_audits
WHERE active AND outbound_order_no='' AND platform_shipping_at IS NOT NULL
  AND platform_shipping_at < $1
ORDER BY window_start DESC LIMIT $2
`, before, limit)
	if err != nil {
		return nil, fmt.Errorf("list fulfillment audit coverage hours: %w", err)
	}
	defer rows.Close()
	hours := make([]time.Time, 0)
	for rows.Next() {
		var hour time.Time
		if err := rows.Scan(&hour); err != nil {
			return nil, err
		}
		hours = append(hours, hour)
	}
	return hours, rows.Err()
}

func (p *Postgres) UpdateFulfillmentAuditResolution(ctx context.Context, id int64, resolution model.FulfillmentAuditResolution) error {
	resolution.WarehouseCode = strings.ToUpper(strings.TrimSpace(resolution.WarehouseCode))
	resolution.OMSStatus = strings.TrimSpace(resolution.OMSStatus)
	if resolution.OMSStatus == "" {
		resolution.OMSStatus = "unknown"
	}
	_, err := p.pool.Exec(ctx, `
UPDATE xlwms_fulfillment_audits SET
wh_code=coalesce(nullif($2,''),wh_code),
oms_status_since=CASE WHEN oms_status IS DISTINCT FROM $3 THEN now() ELSE oms_status_since END,
oms_processing_since=CASE
WHEN $3='processing' AND oms_status='processing' THEN coalesce(oms_processing_since,$5,now())
WHEN $3='processing' THEN coalesce($5,now()) ELSE NULL END,
oms_status=$3,oms_status_code=$4,
oms_order_created_at=coalesce($5,oms_order_created_at),
oms_outbound_at=coalesce($6,oms_outbound_at),
outbound_order_no=$7,oms_tracking_number=$8,sync_error=$9,
exception_category=CASE WHEN $3='outbound' THEN 'archived' ELSE exception_category END,
active=CASE WHEN $3='outbound' THEN false ELSE active END,
resolved_at=CASE WHEN $3='outbound' THEN coalesce(resolved_at,now()) ELSE resolved_at END,
last_checked_at=now(),updated_at=now()
WHERE id=$1
`, id, resolution.WarehouseCode, resolution.OMSStatus, resolution.OMSStatusCode,
		resolution.OMSOrderCreated, resolution.OMSOutboundAt, strings.TrimSpace(resolution.OutboundOrderNo),
		strings.TrimSpace(resolution.TrackingNumber), strings.TrimSpace(resolution.SyncError))
	if err != nil {
		return fmt.Errorf("update fulfillment audit resolution: %w", err)
	}
	return nil
}

func (p *Postgres) RefreshFulfillmentAuditCategories(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `
WITH classified AS (
SELECT id,oms_status='outbound' AS archive,CASE
WHEN oms_status='outbound' THEN 'archived'
WHEN oms_status='pending_query' THEN 'pending_query'
WHEN oms_status='query_error' THEN 'sync_error'
WHEN oms_status IN ('not_found','exception','unknown') THEN 'manual_required'
WHEN oms_status='processing'
 AND coalesce(oms_processing_since,oms_order_created_at,oms_status_since) <= now()-interval '24 hours'
THEN 'warehouse_overdue'
ELSE 'monitoring' END AS category
FROM xlwms_fulfillment_audits
WHERE active OR (oms_status='outbound' AND exception_category<>'archived')
)
UPDATE xlwms_fulfillment_audits audit SET
exception_category=classified.category,
active=CASE WHEN classified.archive THEN false ELSE audit.active END,
resolved_at=CASE WHEN classified.archive THEN coalesce(audit.resolved_at,now()) ELSE audit.resolved_at END,
updated_at=CASE WHEN audit.exception_category IS DISTINCT FROM classified.category
 OR (classified.archive AND audit.active) THEN now() ELSE audit.updated_at END
FROM classified WHERE audit.id=classified.id
`)
	return err
}

func (p *Postgres) ListFulfillmentAudits(ctx context.Context, filter FulfillmentAuditFilter) ([]model.FulfillmentAudit, int, model.FulfillmentAuditSummary, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 30
	}
	clause, args := fulfillmentAuditWhere(filter)
	var total int
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM xlwms_fulfillment_audits WHERE "+clause, args...).Scan(&total); err != nil {
		return nil, 0, model.FulfillmentAuditSummary{}, err
	}
	var summary model.FulfillmentAuditSummary
	if err := p.pool.QueryRow(ctx, `
SELECT count(*),count(*) FILTER (WHERE exception_category='pending_query'),
       count(*) FILTER (WHERE exception_category='manual_required'),
       count(*) FILTER (WHERE exception_category='warehouse_overdue'),
       count(*) FILTER (WHERE exception_category='sync_error'),
       count(*) FILTER (WHERE exception_category='monitoring')
FROM xlwms_fulfillment_audits WHERE active
`).Scan(&summary.Total, &summary.PendingQuery, &summary.ManualRequired, &summary.WarehouseOverdue, &summary.SyncError, &summary.Monitoring); err != nil {
		return nil, 0, summary, err
	}
	lastQueryAt, err := p.LatestOutboundQueryEvent(ctx)
	if err != nil {
		return nil, 0, summary, err
	}
	summary.LastQueryAt = lastQueryAt
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	orderBy := `CASE exception_category
WHEN 'manual_required' THEN 0 WHEN 'warehouse_overdue' THEN 1
WHEN 'sync_error' THEN 2 ELSE 3 END,
coalesce(oms_processing_since,oms_outbound_at,platform_shipping_at,first_seen_at),id`
	if filter.Archived {
		orderBy = `coalesce(oms_outbound_at,resolved_at,updated_at) DESC,id DESC`
	}
	rows, err := p.pool.Query(ctx, fulfillmentAuditSelect+` WHERE `+clause+`
ORDER BY `+orderBy+`
LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, summary, err
	}
	defer rows.Close()
	items, err := scanFulfillmentAudits(rows)
	return items, total, summary, err
}

func (p *Postgres) ExportManualFulfillmentAudits(ctx context.Context, filter FulfillmentAuditFilter) ([]model.FulfillmentAudit, error) {
	filter.ExceptionCategory = "manual_required"
	filter.Page, filter.PageSize = 0, 0
	clause, args := fulfillmentAuditWhere(filter)
	rows, err := p.pool.Query(ctx, fulfillmentAuditSelect+` WHERE `+clause+`
ORDER BY coalesce(platform_shipping_at,first_seen_at),shop_code,platform_order_no,id`, args...)
	if err != nil {
		return nil, fmt.Errorf("export manual fulfillment audits: %w", err)
	}
	defer rows.Close()
	return scanFulfillmentAudits(rows)
}

const fulfillmentAuditSelect = `SELECT id,platform,shop_code,shop_name,platform_order_no,
platform_status,platform_status_code,platform_shipping_at,warehouse_key,wh_code,
tracking_number,oms_status,oms_status_code,oms_status_since,oms_processing_since,
oms_order_created_at,oms_outbound_at,outbound_order_no,oms_tracking_number,
exception_category,sync_error,active,first_seen_at,last_seen_at,last_checked_at,resolved_at,updated_at
FROM xlwms_fulfillment_audits `

type fulfillmentAuditRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanFulfillmentAudits(rows fulfillmentAuditRows) ([]model.FulfillmentAudit, error) {
	items := make([]model.FulfillmentAudit, 0)
	for rows.Next() {
		var item model.FulfillmentAudit
		if err := rows.Scan(&item.ID, &item.Platform, &item.ShopCode, &item.ShopName,
			&item.PlatformOrderNo, &item.PlatformStatus, &item.PlatformStatusCode,
			&item.PlatformShippingAt, &item.WarehouseKey, &item.WarehouseCode,
			&item.TrackingNumber, &item.OMSStatus, &item.OMSStatusCode, &item.OMSStatusSince,
			&item.OMSProcessingSince, &item.OMSOrderCreatedAt, &item.OMSOutboundAt,
			&item.OutboundOrderNo, &item.OMSTrackingNumber, &item.ExceptionCategory,
			&item.SyncError, &item.Active, &item.FirstSeenAt, &item.LastSeenAt,
			&item.LastCheckedAt, &item.ResolvedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
