package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"xlwms-api-manager/internal/model"
)

var (
	SupportedFulfillmentWarehouseKeys = []string{"DPS002", "ARP_EAST", "DPS004", "ARP_WEST"}
	SupportedAutomaticCarrierCodes    = []string{"GOFO", "SWIFTX", "SPEEDX", "YANWEN", "UPS", "USPS", "FEDEX"}
)

func NormalizeFulfillmentWarehouseKey(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	for _, candidate := range SupportedFulfillmentWarehouseKeys {
		if value == candidate {
			return value, nil
		}
	}
	return "", errors.New("warehouse_key must be DPS002, ARP_EAST, DPS004 or ARP_WEST")
}

func NormalizeWarehouseSKU(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("warehouse_sku is required")
	}
	if len(value) > 255 {
		return "", errors.New("warehouse_sku cannot exceed 255 characters")
	}
	return value, nil
}

func ValidateCarrierPolicies(warehouseKey string, policies []model.CarrierPolicy) ([]model.CarrierPolicy, error) {
	warehouseKey, err := NormalizeFulfillmentWarehouseKey(warehouseKey)
	if err != nil {
		return nil, err
	}
	if len(policies) != len(SupportedAutomaticCarrierCodes) {
		return nil, fmt.Errorf("all %d supported carriers are required", len(SupportedAutomaticCarrierCodes))
	}
	supported := make(map[string]bool, len(SupportedAutomaticCarrierCodes))
	for _, code := range SupportedAutomaticCarrierCodes {
		supported[code] = true
	}
	seenCodes := make(map[string]bool, len(policies))
	seenPriorities := make(map[int]bool, len(policies))
	normalized := make([]model.CarrierPolicy, 0, len(policies))
	for _, policy := range policies {
		code := strings.ToUpper(strings.TrimSpace(policy.CarrierCode))
		if !supported[code] || seenCodes[code] {
			return nil, fmt.Errorf("unsupported or duplicate carrier %q", policy.CarrierCode)
		}
		if policy.Priority < 1 || policy.Priority > len(policies) || seenPriorities[policy.Priority] {
			return nil, errors.New("carrier priorities must be unique consecutive values")
		}
		seenCodes[code], seenPriorities[policy.Priority] = true, true
		normalized = append(normalized, model.CarrierPolicy{WarehouseKey: warehouseKey, CarrierCode: code, Priority: policy.Priority, Enabled: policy.Enabled})
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Priority < normalized[j].Priority })
	return normalized, nil
}

func (p *Postgres) CarrierPolicies(ctx context.Context, platform, warehouseSKU string) ([]model.WarehouseCarrierPolicies, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return nil, err
	}
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	rows, err := p.pool.Query(ctx, `
SELECT defaults.warehouse_key,defaults.carrier_code,
       coalesce(overrides.priority,defaults.priority),coalesce(overrides.enabled,defaults.enabled),
       (overrides.carrier_code IS NOT NULL)
FROM xlwms_platform_carrier_policies defaults
LEFT JOIN xlwms_platform_sku_carrier_policies overrides
  ON overrides.platform=defaults.platform AND overrides.warehouse_sku=$2
 AND overrides.warehouse_key=defaults.warehouse_key AND overrides.carrier_code=defaults.carrier_code
WHERE defaults.platform=$1
ORDER BY defaults.warehouse_key,coalesce(overrides.priority,defaults.priority),defaults.carrier_code
`, platform, warehouseSKU)
	if err != nil {
		return nil, fmt.Errorf("list carrier policies: %w", err)
	}
	defer rows.Close()
	byWarehouse := make(map[string][]model.CarrierPolicy)
	customized := make(map[string]bool)
	for rows.Next() {
		var item model.CarrierPolicy
		var custom bool
		if err := rows.Scan(&item.WarehouseKey, &item.CarrierCode, &item.Priority, &item.Enabled, &custom); err != nil {
			return nil, fmt.Errorf("scan carrier policy: %w", err)
		}
		byWarehouse[item.WarehouseKey] = append(byWarehouse[item.WarehouseKey], item)
		customized[item.WarehouseKey] = customized[item.WarehouseKey] || custom
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]model.WarehouseCarrierPolicies, 0, len(SupportedFulfillmentWarehouseKeys))
	for _, warehouseKey := range SupportedFulfillmentWarehouseKeys {
		source := "platform_default"
		if customized[warehouseKey] {
			source = "platform_sku"
		}
		result = append(result, model.WarehouseCarrierPolicies{
			WarehouseKey: warehouseKey, WarehouseSKU: warehouseSKU, Customized: customized[warehouseKey],
			Source: source, Carriers: byWarehouse[warehouseKey],
		})
	}
	return result, nil
}

func (p *Postgres) ReplaceCarrierPolicies(ctx context.Context, platform, warehouseSKU, warehouseKey string, policies []model.CarrierPolicy) (model.WarehouseCarrierPolicies, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return model.WarehouseCarrierPolicies{}, err
	}
	warehouseKey, err = NormalizeFulfillmentWarehouseKey(warehouseKey)
	if err != nil {
		return model.WarehouseCarrierPolicies{}, err
	}
	policies, err = ValidateCarrierPolicies(warehouseKey, policies)
	if err != nil {
		return model.WarehouseCarrierPolicies{}, err
	}
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return model.WarehouseCarrierPolicies{}, err
	}
	defer tx.Rollback(ctx)
	table := "xlwms_platform_carrier_policies"
	deleteSQL := "DELETE FROM " + table + " WHERE platform=$1 AND warehouse_key=$2"
	if warehouseSKU != "" {
		if warehouseSKU, err = NormalizeWarehouseSKU(warehouseSKU); err != nil {
			return model.WarehouseCarrierPolicies{}, err
		}
		table = "xlwms_platform_sku_carrier_policies"
		deleteSQL = "DELETE FROM " + table + " WHERE platform=$1 AND warehouse_sku=$2 AND warehouse_key=$3"
		_, err = tx.Exec(ctx, deleteSQL, platform, warehouseSKU, warehouseKey)
	} else {
		_, err = tx.Exec(ctx, deleteSQL, platform, warehouseKey)
	}
	if err != nil {
		return model.WarehouseCarrierPolicies{}, fmt.Errorf("clear carrier policies: %w", err)
	}
	for _, policy := range policies {
		if warehouseSKU == "" {
			_, err = tx.Exec(ctx, `INSERT INTO xlwms_platform_carrier_policies(platform,warehouse_key,carrier_code,priority,enabled,updated_at) VALUES($1,$2,$3,$4,$5,now())`, platform, warehouseKey, policy.CarrierCode, policy.Priority, policy.Enabled)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO xlwms_platform_sku_carrier_policies(platform,warehouse_sku,warehouse_key,carrier_code,priority,enabled,updated_at) VALUES($1,$2,$3,$4,$5,$6,now())`, platform, warehouseSKU, warehouseKey, policy.CarrierCode, policy.Priority, policy.Enabled)
		}
		if err != nil {
			return model.WarehouseCarrierPolicies{}, fmt.Errorf("save carrier policy: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.WarehouseCarrierPolicies{}, err
	}
	groups, err := p.CarrierPolicies(ctx, platform, warehouseSKU)
	if err != nil {
		return model.WarehouseCarrierPolicies{}, err
	}
	for _, group := range groups {
		if group.WarehouseKey == warehouseKey {
			return group, nil
		}
	}
	return model.WarehouseCarrierPolicies{}, errors.New("saved carrier policy was not found")
}

func (p *Postgres) ResetSKUCarrierPolicies(ctx context.Context, platform, warehouseSKU, warehouseKey string) error {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return err
	}
	warehouseSKU, err = NormalizeWarehouseSKU(warehouseSKU)
	if err != nil {
		return err
	}
	warehouseKey, err = NormalizeFulfillmentWarehouseKey(warehouseKey)
	if err != nil {
		return err
	}
	command, err := p.pool.Exec(ctx, `DELETE FROM xlwms_platform_sku_carrier_policies WHERE platform=$1 AND warehouse_sku=$2 AND warehouse_key=$3`, platform, warehouseSKU, warehouseKey)
	if err != nil {
		return fmt.Errorf("reset SKU carrier policies: %w", err)
	}
	if command.RowsAffected() == 0 {
		return errors.New("SKU has no carrier policy override for this warehouse")
	}
	return nil
}

func (p *Postgres) DisabledWarehousesForPlatformSKUs(ctx context.Context, platform string, warehouseSKUs []string) (map[string]map[string]bool, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string]bool, len(warehouseSKUs))
	if len(warehouseSKUs) == 0 {
		return result, nil
	}
	rows, err := p.pool.Query(ctx, `SELECT warehouse_sku,warehouse_key FROM xlwms_platform_sku_disabled_warehouses WHERE platform=$1 AND warehouse_sku=ANY($2)`, platform, warehouseSKUs)
	if err != nil {
		return nil, fmt.Errorf("resolve disabled warehouses: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sku, key string
		if err := rows.Scan(&sku, &key); err != nil {
			return nil, err
		}
		if result[sku] == nil {
			result[sku] = make(map[string]bool)
		}
		result[sku][key] = true
	}
	return result, rows.Err()
}

func (p *Postgres) ListPlatformSKUFulfillmentPolicies(ctx context.Context, platform, query string, page, pageSize int) ([]model.PlatformSKUFulfillmentPolicy, int, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 30
	}
	query = strings.TrimSpace(query)
	var total int
	if err := p.pool.QueryRow(ctx, `
WITH sku_catalog AS (
  SELECT warehouse_sku FROM xlwms_warehouse_sku_specs
  UNION SELECT warehouse_sku FROM xlwms_platform_sku_disabled_warehouses WHERE platform=$1
  UNION SELECT warehouse_sku FROM xlwms_platform_sku_carrier_policies WHERE platform=$1
)
SELECT count(*) FROM sku_catalog catalog
LEFT JOIN xlwms_warehouse_sku_specs specs ON specs.warehouse_sku=catalog.warehouse_sku
WHERE $2='' OR catalog.warehouse_sku ILIKE '%'||$2||'%' OR coalesce(specs.product_name,'') ILIKE '%'||$2||'%'
`, platform, query).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := p.pool.Query(ctx, `
WITH sku_catalog AS (
  SELECT warehouse_sku FROM xlwms_warehouse_sku_specs
  UNION SELECT warehouse_sku FROM xlwms_platform_sku_disabled_warehouses WHERE platform=$1
  UNION SELECT warehouse_sku FROM xlwms_platform_sku_carrier_policies WHERE platform=$1
)
SELECT catalog.warehouse_sku,coalesce(specs.product_name,''),
       coalesce(array_agg(disabled.warehouse_key ORDER BY disabled.warehouse_key) FILTER (WHERE disabled.warehouse_key IS NOT NULL),'{}'),
       count(disabled.warehouse_key)>0,coalesce(max(disabled.updated_at),specs.updated_at,now())
FROM sku_catalog catalog
LEFT JOIN xlwms_warehouse_sku_specs specs ON specs.warehouse_sku=catalog.warehouse_sku
LEFT JOIN xlwms_platform_sku_disabled_warehouses disabled ON disabled.platform=$1 AND disabled.warehouse_sku=catalog.warehouse_sku
WHERE $2='' OR catalog.warehouse_sku ILIKE '%'||$2||'%' OR coalesce(specs.product_name,'') ILIKE '%'||$2||'%'
GROUP BY catalog.warehouse_sku,specs.product_name,specs.updated_at
ORDER BY catalog.warehouse_sku LIMIT $3 OFFSET $4`, platform, query, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list SKU fulfillment policies: %w", err)
	}
	defer rows.Close()
	items := make([]model.PlatformSKUFulfillmentPolicy, 0, pageSize)
	for rows.Next() {
		var item model.PlatformSKUFulfillmentPolicy
		item.Platform = platform
		if err := rows.Scan(&item.WarehouseSKU, &item.ProductName, &item.DisabledWarehouseKeys, &item.Customized, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (p *Postgres) ReplacePlatformSKUDisabledWarehouses(ctx context.Context, platform, warehouseSKU string, disabledKeys []string) (model.PlatformSKUFulfillmentPolicy, error) {
	platform, err := NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return model.PlatformSKUFulfillmentPolicy{}, err
	}
	warehouseSKU, err = NormalizeWarehouseSKU(warehouseSKU)
	if err != nil {
		return model.PlatformSKUFulfillmentPolicy{}, err
	}
	seen := make(map[string]bool)
	keys := make([]string, 0, len(disabledKeys))
	for _, value := range disabledKeys {
		key, keyErr := NormalizeFulfillmentWarehouseKey(value)
		if keyErr != nil {
			return model.PlatformSKUFulfillmentPolicy{}, keyErr
		}
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return model.PlatformSKUFulfillmentPolicy{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM xlwms_platform_sku_disabled_warehouses WHERE platform=$1 AND warehouse_sku=$2`, platform, warehouseSKU); err != nil {
		return model.PlatformSKUFulfillmentPolicy{}, err
	}
	for _, key := range keys {
		if _, err := tx.Exec(ctx, `INSERT INTO xlwms_platform_sku_disabled_warehouses(platform,warehouse_sku,warehouse_key,updated_at) VALUES($1,$2,$3,now())`, platform, warehouseSKU, key); err != nil {
			return model.PlatformSKUFulfillmentPolicy{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.PlatformSKUFulfillmentPolicy{}, err
	}
	var item model.PlatformSKUFulfillmentPolicy
	item.Platform, item.WarehouseSKU, item.DisabledWarehouseKeys, item.Customized = platform, warehouseSKU, keys, len(keys) > 0
	err = p.pool.QueryRow(ctx, `
SELECT coalesce((SELECT product_name FROM xlwms_warehouse_sku_specs WHERE warehouse_sku=$2),''),
       coalesce((SELECT max(updated_at) FROM xlwms_platform_sku_disabled_warehouses WHERE platform=$1 AND warehouse_sku=$2),now())
`, platform, warehouseSKU).Scan(&item.ProductName, &item.UpdatedAt)
	if err != nil {
		return item, fmt.Errorf("load saved SKU fulfillment policy: %w", err)
	}
	return item, nil
}
