package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xlwms-api-manager/internal/model"

	"github.com/jackc/pgx/v5"
)

type InventoryAlertFilter struct {
	WarehouseCode string
	Query         string
	Status        string
	Page          int
	PageSize      int
}

func (p *Postgres) InventoryAlertDefault(ctx context.Context) (float64, error) {
	var threshold float64
	if err := p.pool.QueryRow(ctx, `
SELECT threshold::float8 FROM xlwms_inventory_alert_defaults WHERE id=1
`).Scan(&threshold); err != nil {
		return 0, fmt.Errorf("get inventory alert default: %w", err)
	}
	return threshold, nil
}

func (p *Postgres) UpdateInventoryAlertDefault(ctx context.Context, threshold float64) (float64, error) {
	var result float64
	if err := p.pool.QueryRow(ctx, `
INSERT INTO xlwms_inventory_alert_defaults (id, threshold, updated_at)
VALUES (1, $1, now())
ON CONFLICT (id) DO UPDATE SET threshold=EXCLUDED.threshold, updated_at=now()
RETURNING threshold::float8
`, threshold).Scan(&result); err != nil {
		return 0, fmt.Errorf("update inventory alert default: %w", err)
	}
	return result, nil
}

func (p *Postgres) UpsertWarehouseSKUInventoryAlertThreshold(ctx context.Context, warehouseCode, warehouseSKU string, threshold float64) (model.WarehouseSKUInventoryAlertThreshold, error) {
	warehouseCode = strings.ToUpper(strings.TrimSpace(warehouseCode))
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	if warehouseCode == "" || warehouseSKU == "" {
		return model.WarehouseSKUInventoryAlertThreshold{}, errors.New("wh_code and warehouse_sku are required")
	}
	var result model.WarehouseSKUInventoryAlertThreshold
	err := p.pool.QueryRow(ctx, `
INSERT INTO xlwms_warehouse_sku_alert_thresholds (wh_code, warehouse_sku, threshold, updated_at)
SELECT w.wh_code, s.warehouse_sku, $3, now()
FROM xlwms_warehouses w
JOIN xlwms_warehouse_sku_specs s ON s.warehouse_sku=$2
WHERE w.wh_code=$1 AND w.is_active AND EXISTS (
    SELECT 1 FROM xlwms_inventory_records i
    WHERE i.inventory_kind='integrated' AND i.stock_type=0
      AND i.wh_code=w.wh_code AND i.sku=s.warehouse_sku
)
ON CONFLICT (wh_code, warehouse_sku) DO UPDATE
SET threshold=EXCLUDED.threshold, updated_at=now()
RETURNING wh_code, warehouse_sku, threshold::float8, updated_at
`, warehouseCode, warehouseSKU, threshold).Scan(
		&result.WarehouseCode, &result.WarehouseSKU, &result.Threshold, &result.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errors.New("warehouse SKU has no current sellable inventory record")
	}
	if err != nil {
		return result, fmt.Errorf("save warehouse SKU inventory alert threshold: %w", err)
	}
	return result, nil
}

func (p *Postgres) DeleteWarehouseSKUInventoryAlertThreshold(ctx context.Context, warehouseCode, warehouseSKU string) error {
	command, err := p.pool.Exec(ctx, `
DELETE FROM xlwms_warehouse_sku_alert_thresholds WHERE wh_code=$1 AND warehouse_sku=$2
`, strings.ToUpper(strings.TrimSpace(warehouseCode)), strings.TrimSpace(warehouseSKU))
	if err != nil {
		return fmt.Errorf("delete warehouse SKU inventory alert threshold: %w", err)
	}
	if command.RowsAffected() == 0 {
		return errors.New("warehouse SKU has no custom inventory alert threshold")
	}
	return nil
}

func (p *Postgres) ListInventoryAlerts(ctx context.Context, filter InventoryAlertFilter) ([]model.InventoryAlert, int, model.InventoryAlertSummary, float64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 30
	}
	filter.Status = strings.ToLower(strings.TrimSpace(filter.Status))
	if filter.Status == "" {
		filter.Status = "alert"
	}
	if filter.Status != "alert" && filter.Status != "all" {
		return nil, 0, model.InventoryAlertSummary{}, 0, errors.New("status must be alert or all")
	}

	stockSQL, args := inventoryAlertStockSQL(filter)
	configuredSQL := `
SELECT s.*,
       coalesce(c.threshold,d.threshold)::float8 AS threshold,
       (c.wh_code IS NOT NULL) AS customized,
       (s.available_amount <= coalesce(c.threshold,d.threshold)) AS alert,
       coalesce(c.updated_at,d.updated_at) AS config_updated_at
FROM stock s
CROSS JOIN xlwms_inventory_alert_defaults d
LEFT JOIN xlwms_warehouse_sku_alert_thresholds c
  ON c.wh_code=s.wh_code AND c.warehouse_sku=s.warehouse_sku`

	var summary model.InventoryAlertSummary
	if err := p.pool.QueryRow(ctx, `
WITH stock AS (`+stockSQL+`), configured AS (`+configuredSQL+`)
SELECT count(*) FILTER (WHERE alert)::int,
       count(*) FILTER (WHERE available_amount<=0)::int,
       count(DISTINCT wh_code)::int,
       count(DISTINCT warehouse_sku)::int
FROM configured
`, args...).Scan(&summary.AlertCount, &summary.OutOfStockCount, &summary.WarehouseCount, &summary.SKUCount); err != nil {
		return nil, 0, summary, 0, fmt.Errorf("summarize inventory alerts: %w", err)
	}

	defaultThreshold, err := p.InventoryAlertDefault(ctx)
	if err != nil {
		return nil, 0, summary, 0, err
	}
	statusClause := "TRUE"
	if filter.Status == "alert" {
		statusClause = "alert"
	}
	var total int
	if err := p.pool.QueryRow(ctx, `
WITH stock AS (`+stockSQL+`), configured AS (`+configuredSQL+`)
SELECT count(*)::int FROM configured WHERE `+statusClause, args...).Scan(&total); err != nil {
		return nil, 0, summary, defaultThreshold, fmt.Errorf("count inventory alerts: %w", err)
	}

	pageArgs := append(append([]any{}, args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	limitPosition, offsetPosition := len(pageArgs)-1, len(pageArgs)
	rows, err := p.pool.Query(ctx, `
WITH stock AS (`+stockSQL+`), configured AS (`+configuredSQL+`)
SELECT wh_code, wh_name, warehouse_sku, product_name,
       total_amount, available_amount, lock_amount, transport_amount,
       threshold, customized, alert, inventory_at, config_updated_at
FROM configured
WHERE `+statusClause+`
ORDER BY alert DESC, available_amount ASC, wh_code ASC, warehouse_sku ASC
LIMIT $`+fmt.Sprint(limitPosition)+` OFFSET $`+fmt.Sprint(offsetPosition), pageArgs...)
	if err != nil {
		return nil, 0, summary, defaultThreshold, fmt.Errorf("list inventory alerts: %w", err)
	}
	defer rows.Close()
	items := make([]model.InventoryAlert, 0, filter.PageSize)
	for rows.Next() {
		var item model.InventoryAlert
		if err := rows.Scan(
			&item.WarehouseCode, &item.WarehouseName, &item.WarehouseSKU, &item.ProductName,
			&item.TotalAmount, &item.AvailableAmount, &item.LockAmount, &item.TransportAmount,
			&item.Threshold, &item.Customized, &item.Alert, &item.InventoryAt, &item.ConfigUpdatedAt,
		); err != nil {
			return nil, 0, summary, defaultThreshold, fmt.Errorf("scan inventory alert: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, summary, defaultThreshold, err
	}
	return items, total, summary, defaultThreshold, nil
}

func inventoryAlertStockSQL(filter InventoryAlertFilter) (string, []any) {
	where := []string{
		"i.inventory_kind='integrated'",
		"i.stock_type=0",
		"w.is_active",
		"NULLIF(BTRIM(i.sku),'') IS NOT NULL",
	}
	args := make([]any, 0, 2)
	add := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if warehouseCode := strings.ToUpper(strings.TrimSpace(filter.WarehouseCode)); warehouseCode != "" {
		where = append(where, "i.wh_code="+add(warehouseCode))
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		placeholder := add("%" + query + "%")
		where = append(where, "(i.sku ILIKE "+placeholder+" OR coalesce(i.product_name,'') ILIKE "+placeholder+" OR i.wh_code ILIKE "+placeholder+")")
	}
	return `
SELECT i.wh_code, coalesce(max(w.warehouse_name),'') AS wh_name,
       i.sku AS warehouse_sku, coalesce(max(i.product_name),'') AS product_name,
       coalesce(sum(i.product_total_amount),0)::float8 AS total_amount,
       coalesce(sum(i.product_available_amount),0)::float8 AS available_amount,
       coalesce(sum(i.product_lock_amount),0)::float8 AS lock_amount,
       coalesce(sum(i.product_transport_amount),0)::float8 AS transport_amount,
       max(i.last_seen_at) AS inventory_at
FROM xlwms_inventory_records i
JOIN xlwms_warehouses w ON w.wh_code=i.wh_code
WHERE ` + strings.Join(where, " AND ") + `
GROUP BY i.wh_code, i.sku`, args
}
