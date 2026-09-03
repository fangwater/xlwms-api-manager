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

func (p *Postgres) ListPlatformInventoryThresholds(ctx context.Context) ([]model.PlatformInventoryThresholds, error) {
	rows, err := p.pool.Query(ctx, `
SELECT platform,east_threshold::float8,west_threshold::float8,total_threshold::float8,updated_at
FROM xlwms_platform_inventory_thresholds ORDER BY platform
`)
	if err != nil {
		return nil, fmt.Errorf("list platform inventory thresholds: %w", err)
	}
	defer rows.Close()
	items := make([]model.PlatformInventoryThresholds, 0, 2)
	for rows.Next() {
		var item model.PlatformInventoryThresholds
		if err := rows.Scan(&item.Platform, &item.EastThreshold, &item.WestThreshold, &item.TotalThreshold, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan platform inventory thresholds: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) PlatformInventoryThresholds(ctx context.Context, platform string) (model.PlatformInventoryThresholds, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return model.PlatformInventoryThresholds{}, err
	}
	var item model.PlatformInventoryThresholds
	err = p.pool.QueryRow(ctx, `
SELECT platform,east_threshold::float8,west_threshold::float8,total_threshold::float8,updated_at
FROM xlwms_platform_inventory_thresholds WHERE platform=$1
`, platform).Scan(&item.Platform, &item.EastThreshold, &item.WestThreshold, &item.TotalThreshold, &item.UpdatedAt)
	if err != nil {
		return item, fmt.Errorf("get platform inventory thresholds: %w", err)
	}
	return item, nil
}

func (p *Postgres) UpdatePlatformInventoryThresholds(ctx context.Context, platform string, thresholds model.InventoryThresholds) (model.PlatformInventoryThresholds, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return model.PlatformInventoryThresholds{}, err
	}
	if _, err := p.pool.Exec(ctx, `
INSERT INTO xlwms_platform_inventory_thresholds(platform,east_threshold,west_threshold,total_threshold,updated_at)
VALUES($1,$2,$3,$4,now())
ON CONFLICT(platform) DO UPDATE SET east_threshold=EXCLUDED.east_threshold,
west_threshold=EXCLUDED.west_threshold,total_threshold=EXCLUDED.total_threshold,updated_at=now()
`, platform, thresholds.EastThreshold, thresholds.WestThreshold, thresholds.TotalThreshold); err != nil {
		return model.PlatformInventoryThresholds{}, fmt.Errorf("save platform inventory thresholds: %w", err)
	}
	return p.PlatformInventoryThresholds(ctx, platform)
}

func (p *Postgres) UpsertPlatformSKUInventoryThreshold(ctx context.Context, platform, warehouseSKU string, thresholds model.InventoryThresholds) (model.SKUInventoryThreshold, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return model.SKUInventoryThreshold{}, err
	}
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	if warehouseSKU == "" {
		return model.SKUInventoryThreshold{}, errors.New("warehouse_sku is required")
	}
	if _, err := p.pool.Exec(ctx, `
INSERT INTO xlwms_platform_sku_inventory_thresholds(platform,warehouse_sku,east_threshold,west_threshold,total_threshold,updated_at)
VALUES($1,$2,$3,$4,$5,now())
ON CONFLICT(platform,warehouse_sku) DO UPDATE SET east_threshold=EXCLUDED.east_threshold,
west_threshold=EXCLUDED.west_threshold,total_threshold=EXCLUDED.total_threshold,updated_at=now()
`, platform, warehouseSKU, thresholds.EastThreshold, thresholds.WestThreshold, thresholds.TotalThreshold); err != nil {
		return model.SKUInventoryThreshold{}, fmt.Errorf("save platform SKU inventory thresholds: %w", err)
	}
	return p.PlatformSKUInventoryThreshold(ctx, platform, warehouseSKU)
}

func (p *Postgres) DeletePlatformSKUInventoryThreshold(ctx context.Context, platform, warehouseSKU string) error {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return err
	}
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	if warehouseSKU == "" {
		return errors.New("warehouse_sku is required")
	}
	command, err := p.pool.Exec(ctx, `
DELETE FROM xlwms_platform_sku_inventory_thresholds WHERE platform=$1 AND warehouse_sku=$2
`, platform, warehouseSKU)
	if err != nil {
		return fmt.Errorf("delete platform SKU inventory thresholds: %w", err)
	}
	if command.RowsAffected() == 0 {
		return errors.New("SKU has no platform-specific inventory thresholds")
	}
	return nil
}

func (p *Postgres) PlatformSKUInventoryThreshold(ctx context.Context, platform, warehouseSKU string) (model.SKUInventoryThreshold, error) {
	return p.lookupPlatformSKUInventoryThreshold(ctx, platform, warehouseSKU)
}

func (p *Postgres) ListPlatformInventorySKUThresholds(ctx context.Context, platform string, filter InventoryThresholdFilter, eastCodes, westCodes []string) ([]model.SKUInventoryThreshold, int, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return nil, 0, err
	}
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
coalesce(st.east_threshold, defaults.east_threshold)::float8,
coalesce(st.west_threshold, defaults.west_threshold)::float8,
coalesce(st.total_threshold, defaults.total_threshold)::float8,
(st.warehouse_sku IS NOT NULL),
CASE WHEN st.warehouse_sku IS NOT NULL THEN 'platform_sku' ELSE 'platform_default' END,
i.inventory_at,
coalesce(st.updated_at, defaults.updated_at)
FROM xlwms_warehouse_sku_specs s
JOIN xlwms_platform_inventory_thresholds defaults ON defaults.platform=$4
LEFT JOIN xlwms_platform_sku_inventory_thresholds st
  ON st.platform=$4 AND st.warehouse_sku=s.warehouse_sku
LEFT JOIN inventory i ON i.sku=s.warehouse_sku
WHERE $3='' OR s.warehouse_sku ILIKE '%' || $3 || '%' OR coalesce(s.product_name,'') ILIKE '%' || $3 || '%'
ORDER BY (coalesce(i.east_available,0)+coalesce(i.west_available,0)) ASC, s.warehouse_sku ASC
LIMIT $5 OFFSET $6
`, eastCodes, westCodes, query, platform, filter.PageSize, (filter.Page-1)*filter.PageSize)
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

func (p *Postgres) InventoryThresholdsForPlatformSKUs(ctx context.Context, platform string, warehouseSKUs []string) (map[string]model.InventoryThresholds, model.InventoryThresholds, error) {
	defaultsRecord, err := p.PlatformInventoryThresholds(ctx, platform)
	if err != nil {
		return nil, model.InventoryThresholds{}, err
	}
	defaults := defaultsRecord.InventoryThresholds
	result := make(map[string]model.InventoryThresholds, len(warehouseSKUs))
	for _, sku := range warehouseSKUs {
		result[sku] = defaults
	}
	if len(warehouseSKUs) == 0 {
		return result, defaults, nil
	}
	platform = defaultsRecord.Platform
	rows, err := p.pool.Query(ctx, `
SELECT warehouse_sku, east_threshold::float8, west_threshold::float8, total_threshold::float8
FROM xlwms_platform_sku_inventory_thresholds
WHERE platform=$1 AND warehouse_sku=ANY($2)
`, platform, warehouseSKUs)
	if err != nil {
		return nil, defaults, fmt.Errorf("resolve platform SKU inventory thresholds: %w", err)
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

func (p *Postgres) lookupPlatformSKUInventoryThreshold(ctx context.Context, platform, warehouseSKU string) (model.SKUInventoryThreshold, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return model.SKUInventoryThreshold{}, err
	}
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	if warehouseSKU == "" {
		return model.SKUInventoryThreshold{}, errors.New("warehouse_sku is required")
	}
	var item model.SKUInventoryThreshold
	err = p.pool.QueryRow(ctx, `
SELECT s.warehouse_sku, coalesce(s.product_name,''),
coalesce(st.east_threshold, defaults.east_threshold)::float8,
coalesce(st.west_threshold, defaults.west_threshold)::float8,
coalesce(st.total_threshold, defaults.total_threshold)::float8,
(st.warehouse_sku IS NOT NULL),
CASE WHEN st.warehouse_sku IS NOT NULL THEN 'platform_sku' ELSE 'platform_default' END,
coalesce(st.updated_at, defaults.updated_at)
FROM xlwms_warehouse_sku_specs s
JOIN xlwms_platform_inventory_thresholds defaults ON defaults.platform=$1
LEFT JOIN xlwms_platform_sku_inventory_thresholds st
  ON st.platform=$1 AND st.warehouse_sku=s.warehouse_sku
WHERE s.warehouse_sku=$2
`, platform, warehouseSKU).Scan(
		&item.WarehouseSKU, &item.ProductName, &item.EastThreshold, &item.WestThreshold,
		&item.TotalThreshold, &item.Customized, &item.Source, &item.UpdatedAt,
	)
	if err != nil {
		return item, fmt.Errorf("get platform SKU inventory thresholds: %w", err)
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
