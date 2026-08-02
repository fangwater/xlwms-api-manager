package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xlwms-api-manager/internal/model"
)

type WarehouseSKUSpecFilter struct {
	Query    string
	Status   string
	Page     int
	PageSize int
}

type rowScanner interface {
	Scan(dest ...any) error
}

const warehouseSKUSpecColumns = `warehouse_sku, coalesce(product_name,''), length_cm, width_cm, height_cm,
	weight_kg, coalesce(note,''), enabled, source, first_seen_at, updated_at`

func scanWarehouseSKUSpec(row rowScanner) (model.WarehouseSKUSpec, error) {
	var item model.WarehouseSKUSpec
	err := row.Scan(&item.WarehouseSKU, &item.ProductName, &item.LengthCM, &item.WidthCM, &item.HeightCM,
		&item.WeightKG, &item.Note, &item.Enabled, &item.Source, &item.FirstSeenAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	item.MissingFields = warehouseSKUSpecMissingFields(item, true)
	item.Complete = len(item.MissingFields) == 0
	return item, nil
}

func warehouseSKUSpecMissingFields(item model.WarehouseSKUSpec, matched bool) []string {
	missing := make([]string, 0, 5)
	if !matched {
		return append(missing, "warehouse_sku")
	}
	if !item.Enabled {
		missing = append(missing, "enabled")
	}
	for _, field := range []struct {
		name  string
		value *float64
	}{{"length_cm", item.LengthCM}, {"width_cm", item.WidthCM}, {"height_cm", item.HeightCM}, {"weight_kg", item.WeightKG}} {
		if field.value == nil || *field.value <= 0 {
			missing = append(missing, field.name)
		}
	}
	return missing
}

func (p *Postgres) ListWarehouseSKUSpecs(ctx context.Context, filter WarehouseSKUSpecFilter) ([]model.WarehouseSKUSpec, int, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 30
	}
	where := []string{"1=1"}
	args := make([]any, 0, 4)
	add := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		placeholder := add("%" + query + "%")
		where = append(where, "(warehouse_sku ILIKE "+placeholder+" OR coalesce(product_name,'') ILIKE "+placeholder+" OR coalesce(note,'') ILIKE "+placeholder+")")
	}
	switch strings.ToLower(strings.TrimSpace(filter.Status)) {
	case "", "all":
	case "complete":
		where = append(where, "enabled AND length_cm>0 AND width_cm>0 AND height_cm>0 AND weight_kg>0")
	case "missing":
		where = append(where, "enabled AND (length_cm IS NULL OR width_cm IS NULL OR height_cm IS NULL OR weight_kg IS NULL)")
	case "disabled":
		where = append(where, "NOT enabled")
	default:
		return nil, 0, errors.New("status must be all, complete, missing, or disabled")
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM xlwms_warehouse_sku_specs WHERE "+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count warehouse SKU specs: %w", err)
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := p.pool.Query(ctx, `SELECT `+warehouseSKUSpecColumns+` FROM xlwms_warehouse_sku_specs WHERE `+clause+`
		ORDER BY (enabled AND length_cm>0 AND width_cm>0 AND height_cm>0 AND weight_kg>0), warehouse_sku
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list warehouse SKU specs: %w", err)
	}
	defer rows.Close()
	items := make([]model.WarehouseSKUSpec, 0, filter.PageSize)
	for rows.Next() {
		item, err := scanWarehouseSKUSpec(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan warehouse SKU spec: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func validatePositiveSpecValue(name string, value *float64) error {
	if value != nil && *value <= 0 {
		return fmt.Errorf("%s must be positive", name)
	}
	return nil
}

func (p *Postgres) UpsertWarehouseSKUSpec(ctx context.Context, item model.WarehouseSKUSpec) (model.WarehouseSKUSpec, error) {
	item.WarehouseSKU = strings.TrimSpace(item.WarehouseSKU)
	if item.WarehouseSKU == "" {
		return item, errors.New("warehouse_sku is required")
	}
	if len(item.WarehouseSKU) > 255 {
		return item, errors.New("warehouse_sku cannot exceed 255 characters")
	}
	for name, value := range map[string]*float64{"length_cm": item.LengthCM, "width_cm": item.WidthCM, "height_cm": item.HeightCM, "weight_kg": item.WeightKG} {
		if err := validatePositiveSpecValue(name, value); err != nil {
			return item, err
		}
	}
	row := p.pool.QueryRow(ctx, `
		INSERT INTO xlwms_warehouse_sku_specs
			(warehouse_sku, product_name, length_cm, width_cm, height_cm, weight_kg, note, enabled, source, updated_at)
		VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, NULLIF($7,''), $8, 'manual', now())
		ON CONFLICT (warehouse_sku) DO UPDATE SET
			product_name=EXCLUDED.product_name,
			length_cm=EXCLUDED.length_cm, width_cm=EXCLUDED.width_cm, height_cm=EXCLUDED.height_cm,
			weight_kg=EXCLUDED.weight_kg, note=EXCLUDED.note, enabled=EXCLUDED.enabled,
			source='manual', updated_at=now()
		RETURNING `+warehouseSKUSpecColumns,
		item.WarehouseSKU, strings.TrimSpace(item.ProductName), item.LengthCM, item.WidthCM, item.HeightCM,
		item.WeightKG, strings.TrimSpace(item.Note), item.Enabled)
	result, err := scanWarehouseSKUSpec(row)
	if err != nil {
		return item, fmt.Errorf("save warehouse SKU spec: %w", err)
	}
	return result, nil
}

func (p *Postgres) UpdateWarehouseSKUPackageSpec(ctx context.Context, warehouseSKU string, lengthCM, widthCM, heightCM, weightKG *float64) (model.WarehouseSKUSpec, error) {
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	if warehouseSKU == "" {
		return model.WarehouseSKUSpec{}, errors.New("warehouse_sku is required")
	}
	for name, value := range map[string]*float64{"length_cm": lengthCM, "width_cm": widthCM, "height_cm": heightCM, "weight_kg": weightKG} {
		if value == nil {
			return model.WarehouseSKUSpec{}, fmt.Errorf("%s is required", name)
		}
		if err := validatePositiveSpecValue(name, value); err != nil {
			return model.WarehouseSKUSpec{}, err
		}
	}
	row := p.pool.QueryRow(ctx, `
		UPDATE xlwms_warehouse_sku_specs
		SET length_cm=$2, width_cm=$3, height_cm=$4, weight_kg=$5, source='manual', updated_at=now()
		WHERE warehouse_sku=$1
		RETURNING `+warehouseSKUSpecColumns,
		warehouseSKU, lengthCM, widthCM, heightCM, weightKG)
	result, err := scanWarehouseSKUSpec(row)
	if err != nil {
		return model.WarehouseSKUSpec{}, fmt.Errorf("update warehouse SKU package spec: %w", err)
	}
	return result, nil
}

func (p *Postgres) ensureWarehouseSKUs(ctx context.Context, skus []string) error {
	for _, sku := range skus {
		if _, err := p.pool.Exec(ctx, `
			INSERT INTO xlwms_warehouse_sku_specs (warehouse_sku, source)
			VALUES ($1, 'order_discovery') ON CONFLICT (warehouse_sku) DO NOTHING
		`, sku); err != nil {
			return fmt.Errorf("discover warehouse SKU %s: %w", sku, err)
		}
	}
	return nil
}

func (p *Postgres) ResolveWarehouseSKUSpecs(ctx context.Context, requested []model.WarehouseSKUQuantity) (model.WarehouseSKUSpecResolution, error) {
	result := model.WarehouseSKUSpecResolution{Items: make([]model.WarehouseSKUSpecResolutionItem, 0), MissingSKUs: make([]string, 0)}
	quantities := make(map[string]int)
	order := make([]string, 0, len(requested))
	for _, raw := range requested {
		sku := strings.TrimSpace(raw.WarehouseSKU)
		if sku == "" || raw.Quantity <= 0 {
			return result, errors.New("warehouse_sku and a positive quantity are required")
		}
		if _, exists := quantities[sku]; !exists {
			order = append(order, sku)
		}
		quantities[sku] += raw.Quantity
	}
	if len(order) == 0 {
		return result, errors.New("items are required")
	}
	rows, err := p.pool.Query(ctx, `SELECT `+warehouseSKUSpecColumns+` FROM xlwms_warehouse_sku_specs WHERE warehouse_sku=ANY($1)`, order)
	if err != nil {
		return result, fmt.Errorf("resolve warehouse SKU specs: %w", err)
	}
	found := make(map[string]model.WarehouseSKUSpec, len(order))
	for rows.Next() {
		item, scanErr := scanWarehouseSKUSpec(rows)
		if scanErr != nil {
			rows.Close()
			return result, scanErr
		}
		found[item.WarehouseSKU] = item
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return result, err
	}
	missingRecords := make([]string, 0)
	for _, sku := range order {
		spec, matched := found[sku]
		missingFields := warehouseSKUSpecMissingFields(spec, matched)
		item := model.WarehouseSKUSpecResolutionItem{WarehouseSKU: sku, Quantity: quantities[sku], Matched: matched,
			Enabled: spec.Enabled, Complete: matched && len(missingFields) == 0, LengthCM: spec.LengthCM,
			WidthCM: spec.WidthCM, HeightCM: spec.HeightCM, WeightKG: spec.WeightKG, MissingFields: missingFields}
		result.Items = append(result.Items, item)
		if !item.Complete {
			result.MissingSKUs = append(result.MissingSKUs, sku)
		}
		if !matched {
			missingRecords = append(missingRecords, sku)
		}
	}
	if err := p.ensureWarehouseSKUs(ctx, missingRecords); err != nil {
		return result, err
	}
	if len(result.MissingSKUs) > 0 {
		result.ErrorCode = "WAREHOUSE_SKU_SPEC_INCOMPLETE"
		result.Error = "仓库SKU规格缺失或未启用: " + strings.Join(result.MissingSKUs, "、")
		return result, nil
	}
	totalQuantity := 0
	for _, quantity := range quantities {
		totalQuantity += quantity
	}
	if len(order) != 1 || totalQuantity != 1 {
		result.ErrorCode = "PACKAGE_REQUIRES_MANUAL_PACKING"
		result.Error = "一单多件或多个仓库SKU必须按实际装箱结果人工确认包裹规格"
		return result, nil
	}
	spec := found[order[0]]
	result.Complete = true
	result.Package = &model.WarehousePackageSpec{WarehouseSKU: spec.WarehouseSKU, Weight: *spec.WeightKG,
		WeightUnit: "kg", Length: *spec.LengthCM, Width: *spec.WidthCM, Height: *spec.HeightCM, DimensionUnit: "cm"}
	return result, nil
}
