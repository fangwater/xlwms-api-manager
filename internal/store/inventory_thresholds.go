package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xlwms-api-manager/internal/model"
)

type InventoryThresholdFilter struct {
	Query    string
	Page     int
	PageSize int
}

func (p *Postgres) InventoryThresholdDefaults(ctx context.Context) (model.InventoryThresholds, error) {
	var result model.InventoryThresholds
	err := p.pool.QueryRow(ctx, `
SELECT east_threshold::float8, west_threshold::float8, total_threshold::float8
FROM xlwms_inventory_threshold_defaults WHERE id=1
`).Scan(&result.EastThreshold, &result.WestThreshold, &result.TotalThreshold)
	if err != nil {
		return result, fmt.Errorf("get inventory threshold defaults: %w", err)
	}
	return result, nil
}

func (p *Postgres) UpdateInventoryThresholdDefaults(ctx context.Context, thresholds model.InventoryThresholds) (model.InventoryThresholds, error) {
	var result model.InventoryThresholds
	err := p.pool.QueryRow(ctx, `
INSERT INTO xlwms_inventory_threshold_defaults (id, east_threshold, west_threshold, total_threshold, updated_at)
VALUES (1, $1, $2, $3, now())
ON CONFLICT (id) DO UPDATE SET east_threshold=EXCLUDED.east_threshold,
west_threshold=EXCLUDED.west_threshold, total_threshold=EXCLUDED.total_threshold, updated_at=now()
RETURNING east_threshold::float8, west_threshold::float8, total_threshold::float8
`, thresholds.EastThreshold, thresholds.WestThreshold, thresholds.TotalThreshold).Scan(
		&result.EastThreshold, &result.WestThreshold, &result.TotalThreshold,
	)
	if err != nil {
		return result, fmt.Errorf("update inventory threshold defaults: %w", err)
	}
	return result, nil
}

func (p *Postgres) UpsertSKUInventoryThreshold(ctx context.Context, warehouseSKU string, thresholds model.InventoryThresholds) (model.SKUInventoryThreshold, error) {
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	if warehouseSKU == "" {
		return model.SKUInventoryThreshold{}, errors.New("warehouse_sku is required")
	}
	_, err := p.pool.Exec(ctx, `
INSERT INTO xlwms_sku_inventory_thresholds (warehouse_sku, east_threshold, west_threshold, total_threshold, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (warehouse_sku) DO UPDATE SET east_threshold=EXCLUDED.east_threshold,
west_threshold=EXCLUDED.west_threshold, total_threshold=EXCLUDED.total_threshold, updated_at=now()
`, warehouseSKU, thresholds.EastThreshold, thresholds.WestThreshold, thresholds.TotalThreshold)
	if err != nil {
		return model.SKUInventoryThreshold{}, fmt.Errorf("save SKU inventory thresholds: %w", err)
	}
	return p.SKUInventoryThreshold(ctx, warehouseSKU)
}

func (p *Postgres) DeleteSKUInventoryThreshold(ctx context.Context, warehouseSKU string) error {
	command, err := p.pool.Exec(ctx, `DELETE FROM xlwms_sku_inventory_thresholds WHERE warehouse_sku=$1`, strings.TrimSpace(warehouseSKU))
	if err != nil {
		return fmt.Errorf("delete SKU inventory thresholds: %w", err)
	}
	if command.RowsAffected() == 0 {
		return errors.New("SKU has no custom inventory thresholds")
	}
	return nil
}

func (p *Postgres) SKUInventoryThreshold(ctx context.Context, warehouseSKU string) (model.SKUInventoryThreshold, error) {
	var item model.SKUInventoryThreshold
	err := p.pool.QueryRow(ctx, `
SELECT s.warehouse_sku, coalesce(s.product_name,''),
t.east_threshold::float8, t.west_threshold::float8, t.total_threshold::float8,
true, t.updated_at
FROM xlwms_warehouse_sku_specs s
JOIN xlwms_sku_inventory_thresholds t ON t.warehouse_sku=s.warehouse_sku
WHERE s.warehouse_sku=$1
`, strings.TrimSpace(warehouseSKU)).Scan(
		&item.WarehouseSKU, &item.ProductName, &item.EastThreshold, &item.WestThreshold,
		&item.TotalThreshold, &item.Customized, &item.UpdatedAt,
	)
	if err != nil {
		return item, fmt.Errorf("get SKU inventory thresholds: %w", err)
	}
	return item, nil
}

func (p *Postgres) ListInventoryThresholds(ctx context.Context, filter InventoryThresholdFilter, eastCodes, westCodes []string) ([]model.SKUInventoryThreshold, int, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 30
	}
	query := strings.TrimSpace(filter.Query)
	var total int
	if err := p.pool.QueryRow(ctx, `
SELECT count(*) FROM xlwms_warehouse_sku_specs s
WHERE $1='' OR s.warehouse_sku ILIKE '%' || $1 || '%' OR coalesce(s.product_name,'') ILIKE '%' || $1 || '%'
`, query).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count inventory thresholds: %w", err)
	}
	rows, err := p.pool.Query(ctx, `
WITH inventory AS (
SELECT i.sku,
coalesce(sum(i.product_available_amount) FILTER (WHERE i.wh_code=ANY($1)), 0) AS east_available,
coalesce(sum(i.product_available_amount) FILTER (WHERE i.wh_code=ANY($2)), 0) AS west_available,
max(i.last_seen_at) AS inventory_at
FROM xlwms_inventory_records i
JOIN xlwms_warehouses w ON w.wh_code=i.wh_code AND w.is_active
WHERE i.inventory_kind='integrated' AND i.stock_type=0
AND i.wh_code=ANY($1 || $2)
GROUP BY i.sku
)
SELECT s.warehouse_sku, coalesce(s.product_name,''),
coalesce(i.east_available,0)::float8, coalesce(i.west_available,0)::float8,
(coalesce(i.east_available,0)+coalesce(i.west_available,0))::float8,
coalesce(t.east_threshold,d.east_threshold)::float8,
coalesce(t.west_threshold,d.west_threshold)::float8,
coalesce(t.total_threshold,d.total_threshold)::float8,
(t.warehouse_sku IS NOT NULL), i.inventory_at, coalesce(t.updated_at,d.updated_at)
FROM xlwms_warehouse_sku_specs s
CROSS JOIN xlwms_inventory_threshold_defaults d
LEFT JOIN xlwms_sku_inventory_thresholds t ON t.warehouse_sku=s.warehouse_sku
LEFT JOIN inventory i ON i.sku=s.warehouse_sku
WHERE $3='' OR s.warehouse_sku ILIKE '%' || $3 || '%' OR coalesce(s.product_name,'') ILIKE '%' || $3 || '%'
ORDER BY (coalesce(i.east_available,0)+coalesce(i.west_available,0)) ASC, s.warehouse_sku ASC
LIMIT $4 OFFSET $5
`, eastCodes, westCodes, query, filter.PageSize, (filter.Page-1)*filter.PageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list inventory thresholds: %w", err)
	}
	defer rows.Close()
	items := make([]model.SKUInventoryThreshold, 0, filter.PageSize)
	for rows.Next() {
		var item model.SKUInventoryThreshold
		if err := rows.Scan(
			&item.WarehouseSKU, &item.ProductName, &item.EastAvailable, &item.WestAvailable,
			&item.TotalAvailable, &item.EastThreshold, &item.WestThreshold, &item.TotalThreshold,
			&item.Customized, &item.InventoryAt, &item.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan inventory thresholds: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (p *Postgres) InventoryThresholdsForSKUs(ctx context.Context, warehouseSKUs []string) (map[string]model.InventoryThresholds, model.InventoryThresholds, error) {
	defaults, err := p.InventoryThresholdDefaults(ctx)
	if err != nil {
		return nil, defaults, err
	}
	result := make(map[string]model.InventoryThresholds, len(warehouseSKUs))
	for _, sku := range warehouseSKUs {
		result[sku] = defaults
	}
	if len(warehouseSKUs) == 0 {
		return result, defaults, nil
	}
	rows, err := p.pool.Query(ctx, `
SELECT warehouse_sku, east_threshold::float8, west_threshold::float8, total_threshold::float8
FROM xlwms_sku_inventory_thresholds WHERE warehouse_sku=ANY($1)
`, warehouseSKUs)
	if err != nil {
		return nil, defaults, fmt.Errorf("resolve SKU inventory thresholds: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sku string
		var thresholds model.InventoryThresholds
		if err := rows.Scan(&sku, &thresholds.EastThreshold, &thresholds.WestThreshold, &thresholds.TotalThreshold); err != nil {
			return nil, defaults, err
		}
		result[sku] = thresholds
	}
	return result, defaults, rows.Err()
}
