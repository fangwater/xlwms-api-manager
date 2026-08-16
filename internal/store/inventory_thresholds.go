package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xlwms-api-manager/internal/model"

	"github.com/jackc/pgx/v5"
)

type InventoryThresholdFilter struct {
	Query    string
	Page     int
	PageSize int
}

func NormalizeFulfillmentPlatform(platform string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "temu":
		return "temu", nil
	case "shein":
		return "shein", nil
	case "":
		return "", errors.New("platform is required")
	default:
		return "", errors.New("platform must be temu or shein")
	}
}

func NormalizeFulfillmentShopCode(shopCode string) (string, error) {
	shopCode = strings.ToLower(strings.TrimSpace(shopCode))
	if shopCode == "" {
		return "", errors.New("shop_code is required")
	}
	for _, character := range shopCode {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
			continue
		}
		return "", errors.New("shop_code is invalid")
	}
	if strings.HasPrefix(shopCode, "-") || strings.HasSuffix(shopCode, "-") || strings.Contains(shopCode, "--") {
		return "", errors.New("shop_code is invalid")
	}
	return shopCode, nil
}

func (p *Postgres) ListFulfillmentShops(ctx context.Context) ([]model.FulfillmentShop, error) {
	if err := p.ensureFulfillmentShopThresholds(ctx); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
SELECT platform, shop_code, shop_name, enabled
FROM xlwms_fulfillment_shops
WHERE enabled
ORDER BY platform, shop_name, shop_code
`)
	if err != nil {
		return nil, fmt.Errorf("list fulfillment shops: %w", err)
	}
	defer rows.Close()
	items := make([]model.FulfillmentShop, 0, 4)
	for rows.Next() {
		var item model.FulfillmentShop
		if err := rows.Scan(&item.Platform, &item.ShopCode, &item.ShopName, &item.Enabled); err != nil {
			return nil, fmt.Errorf("scan fulfillment shop: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) FulfillmentShop(ctx context.Context, platform, shopCode string) (model.FulfillmentShop, error) {
	platform, shopCode, err := normalizeShopIdentity(platform, shopCode)
	if err != nil {
		return model.FulfillmentShop{}, err
	}
	if err := p.ensureFulfillmentShopThresholds(ctx); err != nil {
		return model.FulfillmentShop{}, err
	}
	var item model.FulfillmentShop
	err = p.pool.QueryRow(ctx, `
SELECT platform, shop_code, shop_name, enabled
FROM xlwms_fulfillment_shops
WHERE platform=$1 AND shop_code=$2 AND enabled
`, platform, shopCode).Scan(&item.Platform, &item.ShopCode, &item.ShopName, &item.Enabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.FulfillmentShop{}, fmt.Errorf("fulfillment shop %s/%s was not found", platform, shopCode)
		}
		return model.FulfillmentShop{}, fmt.Errorf("get fulfillment shop: %w", err)
	}
	return item, nil
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

func (p *Postgres) ListShopInventoryThresholds(ctx context.Context) ([]model.ShopInventoryThresholds, error) {
	if err := p.ensureFulfillmentShopThresholds(ctx); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
SELECT s.platform, s.shop_code, s.shop_name, s.enabled,
coalesce(t.east_threshold, d.east_threshold)::float8,
coalesce(t.west_threshold, d.west_threshold)::float8,
coalesce(t.total_threshold, d.total_threshold)::float8,
(t.platform IS NOT NULL), coalesce(t.updated_at, d.updated_at)
FROM xlwms_fulfillment_shops s
CROSS JOIN xlwms_inventory_threshold_defaults d
LEFT JOIN xlwms_shop_inventory_thresholds t
  ON t.platform=s.platform AND t.shop_code=s.shop_code
WHERE s.enabled AND d.id=1
ORDER BY s.platform, s.shop_name, s.shop_code
`)
	if err != nil {
		return nil, fmt.Errorf("list shop inventory thresholds: %w", err)
	}
	defer rows.Close()
	items := make([]model.ShopInventoryThresholds, 0, 4)
	for rows.Next() {
		var item model.ShopInventoryThresholds
		if err := rows.Scan(
			&item.Platform, &item.ShopCode, &item.ShopName, &item.Enabled,
			&item.EastThreshold, &item.WestThreshold, &item.TotalThreshold,
			&item.Customized, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan shop inventory thresholds: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) ShopInventoryThresholds(ctx context.Context, platform, shopCode string) (model.ShopInventoryThresholds, error) {
	shop, err := p.FulfillmentShop(ctx, platform, shopCode)
	if err != nil {
		return model.ShopInventoryThresholds{}, err
	}
	var item model.ShopInventoryThresholds
	err = p.pool.QueryRow(ctx, `
SELECT coalesce(t.east_threshold, d.east_threshold)::float8,
coalesce(t.west_threshold, d.west_threshold)::float8,
coalesce(t.total_threshold, d.total_threshold)::float8,
(t.platform IS NOT NULL), coalesce(t.updated_at, d.updated_at)
FROM xlwms_inventory_threshold_defaults d
LEFT JOIN xlwms_shop_inventory_thresholds t
  ON t.platform=$1 AND t.shop_code=$2
WHERE d.id=1
`, shop.Platform, shop.ShopCode).Scan(
		&item.EastThreshold, &item.WestThreshold, &item.TotalThreshold,
		&item.Customized, &item.UpdatedAt,
	)
	if err != nil {
		return model.ShopInventoryThresholds{}, fmt.Errorf("get shop inventory thresholds: %w", err)
	}
	item.FulfillmentShop = shop
	return item, nil
}

func (p *Postgres) UpdateShopInventoryThresholds(ctx context.Context, platform, shopCode string, thresholds model.InventoryThresholds) (model.ShopInventoryThresholds, error) {
	shop, err := p.FulfillmentShop(ctx, platform, shopCode)
	if err != nil {
		return model.ShopInventoryThresholds{}, err
	}
	if _, err := p.pool.Exec(ctx, `
INSERT INTO xlwms_shop_inventory_thresholds (platform, shop_code, east_threshold, west_threshold, total_threshold, updated_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (platform, shop_code) DO UPDATE SET east_threshold=EXCLUDED.east_threshold,
west_threshold=EXCLUDED.west_threshold, total_threshold=EXCLUDED.total_threshold, updated_at=now()
`, shop.Platform, shop.ShopCode, thresholds.EastThreshold, thresholds.WestThreshold, thresholds.TotalThreshold); err != nil {
		return model.ShopInventoryThresholds{}, fmt.Errorf("save shop inventory thresholds: %w", err)
	}
	return p.ShopInventoryThresholds(ctx, shop.Platform, shop.ShopCode)
}

func (p *Postgres) ResetShopInventoryThresholds(ctx context.Context, platform, shopCode string) (model.ShopInventoryThresholds, error) {
	shop, err := p.FulfillmentShop(ctx, platform, shopCode)
	if err != nil {
		return model.ShopInventoryThresholds{}, err
	}
	if _, err := p.pool.Exec(ctx, `
DELETE FROM xlwms_shop_inventory_thresholds WHERE platform=$1 AND shop_code=$2
`, shop.Platform, shop.ShopCode); err != nil {
		return model.ShopInventoryThresholds{}, fmt.Errorf("reset shop inventory thresholds: %w", err)
	}
	return p.ShopInventoryThresholds(ctx, shop.Platform, shop.ShopCode)
}

func (p *Postgres) UpsertSKUInventoryThreshold(ctx context.Context, warehouseSKU string, thresholds model.InventoryThresholds) (model.SKUInventoryThreshold, error) {
	return p.UpsertShopSKUInventoryThreshold(ctx, "", "", warehouseSKU, thresholds)
}

func (p *Postgres) UpsertShopSKUInventoryThreshold(ctx context.Context, platform, shopCode, warehouseSKU string, thresholds model.InventoryThresholds) (model.SKUInventoryThreshold, error) {
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	if warehouseSKU == "" {
		return model.SKUInventoryThreshold{}, errors.New("warehouse_sku is required")
	}
	if platform == "" && shopCode == "" {
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
	shop, err := p.FulfillmentShop(ctx, platform, shopCode)
	if err != nil {
		return model.SKUInventoryThreshold{}, err
	}
	_, err = p.pool.Exec(ctx, `
INSERT INTO xlwms_shop_sku_inventory_thresholds (platform, shop_code, warehouse_sku, east_threshold, west_threshold, total_threshold, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (platform, shop_code, warehouse_sku) DO UPDATE SET east_threshold=EXCLUDED.east_threshold,
west_threshold=EXCLUDED.west_threshold, total_threshold=EXCLUDED.total_threshold, updated_at=now()
`, shop.Platform, shop.ShopCode, warehouseSKU, thresholds.EastThreshold, thresholds.WestThreshold, thresholds.TotalThreshold)
	if err != nil {
		return model.SKUInventoryThreshold{}, fmt.Errorf("save shop SKU inventory thresholds: %w", err)
	}
	return p.ShopSKUInventoryThreshold(ctx, shop.Platform, shop.ShopCode, warehouseSKU)
}

func (p *Postgres) DeleteSKUInventoryThreshold(ctx context.Context, warehouseSKU string) error {
	return p.DeleteShopSKUInventoryThreshold(ctx, "", "", warehouseSKU)
}

func (p *Postgres) DeleteShopSKUInventoryThreshold(ctx context.Context, platform, shopCode, warehouseSKU string) error {
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	if warehouseSKU == "" {
		return errors.New("warehouse_sku is required")
	}
	if platform == "" && shopCode == "" {
		command, err := p.pool.Exec(ctx, `DELETE FROM xlwms_sku_inventory_thresholds WHERE warehouse_sku=$1`, warehouseSKU)
		if err != nil {
			return fmt.Errorf("delete SKU inventory thresholds: %w", err)
		}
		if command.RowsAffected() == 0 {
			return errors.New("SKU has no custom inventory thresholds")
		}
		return nil
	}
	shop, err := p.FulfillmentShop(ctx, platform, shopCode)
	if err != nil {
		return err
	}
	command, err := p.pool.Exec(ctx, `
DELETE FROM xlwms_shop_sku_inventory_thresholds
WHERE platform=$1 AND shop_code=$2 AND warehouse_sku=$3
`, shop.Platform, shop.ShopCode, warehouseSKU)
	if err != nil {
		return fmt.Errorf("delete shop SKU inventory thresholds: %w", err)
	}
	if command.RowsAffected() == 0 {
		return errors.New("SKU has no custom inventory thresholds")
	}
	return nil
}

func (p *Postgres) SKUInventoryThreshold(ctx context.Context, warehouseSKU string) (model.SKUInventoryThreshold, error) {
	return p.ShopSKUInventoryThreshold(ctx, "", "", warehouseSKU)
}

func (p *Postgres) ShopSKUInventoryThreshold(ctx context.Context, platform, shopCode, warehouseSKU string) (model.SKUInventoryThreshold, error) {
	item, err := p.lookupSKUInventoryThreshold(ctx, platform, shopCode, warehouseSKU)
	if err != nil {
		return model.SKUInventoryThreshold{}, err
	}
	return item, nil
}

func (p *Postgres) ListInventoryThresholds(ctx context.Context, filter InventoryThresholdFilter, eastCodes, westCodes []string) ([]model.SKUInventoryThreshold, int, error) {
	return p.ListShopInventorySKUThresholds(ctx, "", "", filter, eastCodes, westCodes)
}

func (p *Postgres) ListShopInventorySKUThresholds(ctx context.Context, platform, shopCode string, filter InventoryThresholdFilter, eastCodes, westCodes []string) ([]model.SKUInventoryThreshold, int, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 30
	}
	query := strings.TrimSpace(filter.Query)
	shopScoped := platform != "" || shopCode != ""
	var shop model.FulfillmentShop
	if shopScoped {
		var err error
		shop, err = p.FulfillmentShop(ctx, platform, shopCode)
		if err != nil {
			return nil, 0, err
		}
	}
	var total int
	if err := p.pool.QueryRow(ctx, `
SELECT count(*) FROM xlwms_warehouse_sku_specs s
WHERE $1='' OR s.warehouse_sku ILIKE '%' || $1 || '%' OR coalesce(s.product_name,'') ILIKE '%' || $1 || '%'
`, query).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count inventory thresholds: %w", err)
	}
	var rows interface {
		Close()
		Next() bool
		Scan(dest ...any) error
		Err() error
	}
	var err error
	if shopScoped {
		rows, err = p.pool.Query(ctx, `
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
coalesce(st.east_threshold, shop.east_threshold, d.east_threshold)::float8,
coalesce(st.west_threshold, shop.west_threshold, d.west_threshold)::float8,
coalesce(st.total_threshold, shop.total_threshold, d.total_threshold)::float8,
(st.warehouse_sku IS NOT NULL),
CASE
  WHEN st.warehouse_sku IS NOT NULL THEN 'shop_sku'
  WHEN shop.platform IS NOT NULL THEN 'shop_default'
  ELSE 'global_default'
END,
i.inventory_at,
coalesce(st.updated_at, shop.updated_at, d.updated_at)
FROM xlwms_warehouse_sku_specs s
CROSS JOIN xlwms_inventory_threshold_defaults d
LEFT JOIN xlwms_shop_inventory_thresholds shop ON shop.platform=$4 AND shop.shop_code=$5
LEFT JOIN xlwms_shop_sku_inventory_thresholds st
  ON st.platform=$4 AND st.shop_code=$5 AND st.warehouse_sku=s.warehouse_sku
LEFT JOIN inventory i ON i.sku=s.warehouse_sku
WHERE $3='' OR s.warehouse_sku ILIKE '%' || $3 || '%' OR coalesce(s.product_name,'') ILIKE '%' || $3 || '%'
ORDER BY (coalesce(i.east_available,0)+coalesce(i.west_available,0)) ASC, s.warehouse_sku ASC
LIMIT $6 OFFSET $7
`, eastCodes, westCodes, query, shop.Platform, shop.ShopCode, filter.PageSize, (filter.Page-1)*filter.PageSize)
	} else {
		rows, err = p.pool.Query(ctx, `
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
(t.warehouse_sku IS NOT NULL),
CASE WHEN t.warehouse_sku IS NOT NULL THEN 'sku' ELSE 'global_default' END,
i.inventory_at, coalesce(t.updated_at,d.updated_at)
FROM xlwms_warehouse_sku_specs s
CROSS JOIN xlwms_inventory_threshold_defaults d
LEFT JOIN xlwms_sku_inventory_thresholds t ON t.warehouse_sku=s.warehouse_sku
LEFT JOIN inventory i ON i.sku=s.warehouse_sku
WHERE $3='' OR s.warehouse_sku ILIKE '%' || $3 || '%' OR coalesce(s.product_name,'') ILIKE '%' || $3 || '%'
ORDER BY (coalesce(i.east_available,0)+coalesce(i.west_available,0)) ASC, s.warehouse_sku ASC
LIMIT $4 OFFSET $5
`, eastCodes, westCodes, query, filter.PageSize, (filter.Page-1)*filter.PageSize)
	}
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
			&item.Customized, &item.Source, &item.InventoryAt, &item.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan inventory thresholds: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (p *Postgres) InventoryThresholdsForSKUs(ctx context.Context, warehouseSKUs []string) (map[string]model.InventoryThresholds, model.InventoryThresholds, error) {
	return p.InventoryThresholdsForShopSKUs(ctx, "", "", warehouseSKUs)
}

func (p *Postgres) InventoryThresholdsForShopSKUs(ctx context.Context, platform, shopCode string, warehouseSKUs []string) (map[string]model.InventoryThresholds, model.InventoryThresholds, error) {
	defaults, err := p.InventoryThresholdDefaults(ctx)
	if err != nil {
		return nil, defaults, err
	}
	shopDefaults := defaults
	shopScoped := platform != "" || shopCode != ""
	if shopScoped {
		shopThresholds, shopErr := p.ShopInventoryThresholds(ctx, platform, shopCode)
		if shopErr != nil {
			return nil, defaults, shopErr
		}
		shopDefaults = shopThresholds.InventoryThresholds
		defaults = shopDefaults
	}
	result := make(map[string]model.InventoryThresholds, len(warehouseSKUs))
	for _, sku := range warehouseSKUs {
		result[sku] = shopDefaults
	}
	if len(warehouseSKUs) == 0 {
		return result, defaults, nil
	}
	if !shopScoped {
		rows, queryErr := p.pool.Query(ctx, `
SELECT warehouse_sku, east_threshold::float8, west_threshold::float8, total_threshold::float8
FROM xlwms_sku_inventory_thresholds WHERE warehouse_sku=ANY($1)
`, warehouseSKUs)
		if queryErr != nil {
			return nil, defaults, fmt.Errorf("resolve SKU inventory thresholds: %w", queryErr)
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
	shop, err := p.FulfillmentShop(ctx, platform, shopCode)
	if err != nil {
		return nil, defaults, err
	}
	rows, err := p.pool.Query(ctx, `
SELECT warehouse_sku, east_threshold::float8, west_threshold::float8, total_threshold::float8
FROM xlwms_shop_sku_inventory_thresholds
WHERE platform=$1 AND shop_code=$2 AND warehouse_sku=ANY($3)
`, shop.Platform, shop.ShopCode, warehouseSKUs)
	if err != nil {
		return nil, defaults, fmt.Errorf("resolve shop SKU inventory thresholds: %w", err)
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

func (p *Postgres) lookupSKUInventoryThreshold(ctx context.Context, platform, shopCode, warehouseSKU string) (model.SKUInventoryThreshold, error) {
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	if warehouseSKU == "" {
		return model.SKUInventoryThreshold{}, errors.New("warehouse_sku is required")
	}
	if platform == "" && shopCode == "" {
		var item model.SKUInventoryThreshold
		err := p.pool.QueryRow(ctx, `
SELECT s.warehouse_sku, coalesce(s.product_name,''),
t.east_threshold::float8, t.west_threshold::float8, t.total_threshold::float8,
true, 'sku', t.updated_at
FROM xlwms_warehouse_sku_specs s
JOIN xlwms_sku_inventory_thresholds t ON t.warehouse_sku=s.warehouse_sku
WHERE s.warehouse_sku=$1
`, warehouseSKU).Scan(
			&item.WarehouseSKU, &item.ProductName, &item.EastThreshold, &item.WestThreshold,
			&item.TotalThreshold, &item.Customized, &item.Source, &item.UpdatedAt,
		)
		if err != nil {
			return item, fmt.Errorf("get SKU inventory thresholds: %w", err)
		}
		return item, nil
	}
	shop, err := p.FulfillmentShop(ctx, platform, shopCode)
	if err != nil {
		return model.SKUInventoryThreshold{}, err
	}
	var item model.SKUInventoryThreshold
	err = p.pool.QueryRow(ctx, `
SELECT s.warehouse_sku, coalesce(s.product_name,''),
coalesce(st.east_threshold, shop.east_threshold, d.east_threshold)::float8,
coalesce(st.west_threshold, shop.west_threshold, d.west_threshold)::float8,
coalesce(st.total_threshold, shop.total_threshold, d.total_threshold)::float8,
(st.warehouse_sku IS NOT NULL),
CASE
  WHEN st.warehouse_sku IS NOT NULL THEN 'shop_sku'
  WHEN shop.platform IS NOT NULL THEN 'shop_default'
  ELSE 'global_default'
END,
coalesce(st.updated_at, shop.updated_at, d.updated_at)
FROM xlwms_warehouse_sku_specs s
CROSS JOIN xlwms_inventory_threshold_defaults d
LEFT JOIN xlwms_shop_inventory_thresholds shop ON shop.platform=$1 AND shop.shop_code=$2
LEFT JOIN xlwms_shop_sku_inventory_thresholds st
  ON st.platform=$1 AND st.shop_code=$2 AND st.warehouse_sku=s.warehouse_sku
WHERE s.warehouse_sku=$3
`, shop.Platform, shop.ShopCode, warehouseSKU).Scan(
		&item.WarehouseSKU, &item.ProductName, &item.EastThreshold, &item.WestThreshold,
		&item.TotalThreshold, &item.Customized, &item.Source, &item.UpdatedAt,
	)
	if err != nil {
		return item, fmt.Errorf("get shop SKU inventory thresholds: %w", err)
	}
	return item, nil
}

func (p *Postgres) ensureFulfillmentShopThresholds(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `
INSERT INTO xlwms_fulfillment_shops (platform, shop_code, shop_name)
VALUES
    ('temu', 'panda-homes', 'PANDA HOMES'),
    ('temu', 'panda-buy', 'PANDA BUY'),
    ('shein', 'beauty-hangers-home', 'Beauty Hangers home')
ON CONFLICT (platform, shop_code) DO UPDATE SET
    shop_name = EXCLUDED.shop_name,
    enabled = true,
    updated_at = now()
`); err != nil {
		return fmt.Errorf("ensure fulfillment shops: %w", err)
	}
	return nil
}

func normalizeShopIdentity(platform, shopCode string) (string, string, error) {
	normalizedPlatform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return "", "", err
	}
	normalizedShop, err := NormalizeFulfillmentShopCode(shopCode)
	if err != nil {
		return "", "", err
	}
	return normalizedPlatform, normalizedShop, nil
}
