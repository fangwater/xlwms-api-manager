package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"xlwms-api-manager/internal/model"
)

const (
	maxPlatformSKUMappingItems = 20
	maxPlatformSKUResolveBatch = 500
)

var platformNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type PlatformSKUMappingFilter struct {
	Platform string
	Query    string
	Status   string
	Page     int
	PageSize int
}

func NormalizePlatformSKUMapping(input model.PlatformSKUMapping) (model.PlatformSKUMapping, error) {
	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	input.PlatformSKU = strings.TrimSpace(input.PlatformSKU)
	input.Source = strings.TrimSpace(input.Source)
	if input.Source == "" {
		input.Source = "manual"
	}
	if !platformNamePattern.MatchString(input.Platform) {
		return model.PlatformSKUMapping{}, errors.New("platform must contain only lowercase letters, numbers, underscores or hyphens")
	}
	if input.PlatformSKU == "" || len(input.PlatformSKU) > 255 {
		return model.PlatformSKUMapping{}, errors.New("platform_sku is required and must not exceed 255 characters")
	}
	if len(input.Source) > 64 {
		return model.PlatformSKUMapping{}, errors.New("source must not exceed 64 characters")
	}
	if len(input.Items) == 0 || len(input.Items) > maxPlatformSKUMappingItems {
		return model.PlatformSKUMapping{}, fmt.Errorf("items must contain between 1 and %d warehouse SKUs", maxPlatformSKUMappingItems)
	}
	seen := make(map[string]struct{}, len(input.Items))
	items := make([]model.PlatformSKUMappingItem, 0, len(input.Items))
	for _, item := range input.Items {
		item.WarehouseSKU = strings.TrimSpace(item.WarehouseSKU)
		if item.WarehouseSKU == "" || len(item.WarehouseSKU) > 255 {
			return model.PlatformSKUMapping{}, errors.New("warehouse_sku is required and must not exceed 255 characters")
		}
		if item.Quantity < 1 || item.Quantity > 999999 {
			return model.PlatformSKUMapping{}, errors.New("quantity must be between 1 and 999999")
		}
		if _, exists := seen[item.WarehouseSKU]; exists {
			return model.PlatformSKUMapping{}, fmt.Errorf("duplicate warehouse_sku %q", item.WarehouseSKU)
		}
		seen[item.WarehouseSKU] = struct{}{}
		items = append(items, model.PlatformSKUMappingItem{WarehouseSKU: item.WarehouseSKU, Quantity: item.Quantity})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].WarehouseSKU < items[j].WarehouseSKU })
	input.Items = items
	return input, nil
}

func normalizePlatformSKUResolveRequest(platform string, requested []string) (string, []string, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if !platformNamePattern.MatchString(platform) {
		return "", nil, errors.New("invalid platform")
	}
	if len(requested) == 0 || len(requested) > maxPlatformSKUResolveBatch {
		return "", nil, fmt.Errorf("platform_skus must contain between 1 and %d values", maxPlatformSKUResolveBatch)
	}
	seen := make(map[string]struct{}, len(requested))
	normalized := make([]string, 0, len(requested))
	for _, sku := range requested {
		sku = strings.TrimSpace(sku)
		if sku == "" || len(sku) > 255 {
			return "", nil, errors.New("platform_skus contains an empty or oversized value")
		}
		if _, exists := seen[sku]; exists {
			continue
		}
		seen[sku] = struct{}{}
		normalized = append(normalized, sku)
	}
	return platform, normalized, nil
}

func (p *Postgres) ListPlatformSKUMappings(ctx context.Context, filter PlatformSKUMappingFilter) (model.PlatformSKUMappingPage, error) {
	filter.Platform = strings.ToLower(strings.TrimSpace(filter.Platform))
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 200 {
		filter.PageSize = 50
	}
	statusClause := ""
	switch filter.Status {
	case "enabled":
		statusClause = "AND enabled"
	case "disabled":
		statusClause = "AND NOT enabled"
	case "", "all":
	default:
		return model.PlatformSKUMappingPage{}, errors.New("status must be all, enabled or disabled")
	}
	var total int
	if err := p.pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT platform,platform_sku FROM xlwms_platform_sku_mappings
			WHERE ($1='' OR platform=$1)
			  AND ($2='' OR platform_sku ILIKE '%'||$2||'%' OR warehouse_sku ILIKE '%'||$2||'%')
			  `+statusClause+`
			GROUP BY platform,platform_sku
		) mappings
	`, filter.Platform, filter.Query).Scan(&total); err != nil {
		return model.PlatformSKUMappingPage{}, fmt.Errorf("count platform SKU mappings: %w", err)
	}
	rows, err := p.pool.Query(ctx, `
		WITH keys AS (
			SELECT platform,platform_sku,max(updated_at) AS updated_at
			FROM xlwms_platform_sku_mappings
			WHERE ($1='' OR platform=$1)
			  AND ($2='' OR platform_sku ILIKE '%'||$2||'%' OR warehouse_sku ILIKE '%'||$2||'%')
			  `+statusClause+`
			GROUP BY platform,platform_sku
			ORDER BY updated_at DESC,platform,platform_sku
			LIMIT $3 OFFSET $4
		)
		SELECT mapping.platform,mapping.platform_sku,mapping.source,mapping.enabled,
		       mapping.created_at,mapping.updated_at,mapping.warehouse_sku,mapping.quantity,
		       coalesce(spec.product_name,''),spec.length_cm,spec.width_cm,spec.height_cm,spec.weight_kg,
		       coalesce(spec.enabled AND spec.length_cm IS NOT NULL AND spec.width_cm IS NOT NULL
		         AND spec.height_cm IS NOT NULL AND spec.weight_kg IS NOT NULL,false)
		FROM keys
		JOIN xlwms_platform_sku_mappings mapping USING(platform,platform_sku)
		LEFT JOIN xlwms_warehouse_sku_specs spec ON spec.warehouse_sku=mapping.warehouse_sku
		ORDER BY keys.updated_at DESC,mapping.platform,mapping.platform_sku,mapping.warehouse_sku
	`, filter.Platform, filter.Query, filter.PageSize, (filter.Page-1)*filter.PageSize)
	if err != nil {
		return model.PlatformSKUMappingPage{}, fmt.Errorf("list platform SKU mappings: %w", err)
	}
	defer rows.Close()
	records, err := scanPlatformSKUMappingRows(rows)
	if err != nil {
		return model.PlatformSKUMappingPage{}, err
	}
	pages := 0
	if total > 0 {
		pages = (total + filter.PageSize - 1) / filter.PageSize
	}
	return model.PlatformSKUMappingPage{Records: records, Total: total, Page: filter.Page, PageSize: filter.PageSize, Pages: pages}, nil
}

func (p *Postgres) ResolvePlatformSKUMappings(ctx context.Context, platform string, requested []string) (model.PlatformSKUMappingResolution, error) {
	platform, requested, err := normalizePlatformSKUResolveRequest(platform, requested)
	if err != nil {
		return model.PlatformSKUMappingResolution{}, err
	}
	rows, err := p.pool.Query(ctx, `
		SELECT mapping.platform,mapping.platform_sku,mapping.source,mapping.enabled,
		       mapping.created_at,mapping.updated_at,mapping.warehouse_sku,mapping.quantity,
		       coalesce(spec.product_name,''),spec.length_cm,spec.width_cm,spec.height_cm,spec.weight_kg,
		       coalesce(spec.enabled AND spec.length_cm IS NOT NULL AND spec.width_cm IS NOT NULL
		         AND spec.height_cm IS NOT NULL AND spec.weight_kg IS NOT NULL,false)
		FROM xlwms_platform_sku_mappings mapping
		LEFT JOIN xlwms_warehouse_sku_specs spec ON spec.warehouse_sku=mapping.warehouse_sku
		WHERE mapping.platform=$1 AND mapping.platform_sku=ANY($2::text[]) AND mapping.enabled
		ORDER BY mapping.platform_sku,mapping.warehouse_sku
	`, platform, requested)
	if err != nil {
		return model.PlatformSKUMappingResolution{}, fmt.Errorf("resolve platform SKU mappings: %w", err)
	}
	defer rows.Close()
	mappings, err := scanPlatformSKUMappingRows(rows)
	if err != nil {
		return model.PlatformSKUMappingResolution{}, err
	}
	found := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		found[mapping.PlatformSKU] = struct{}{}
	}
	unmapped := make([]string, 0)
	for _, sku := range requested {
		if _, ok := found[sku]; !ok {
			unmapped = append(unmapped, sku)
		}
	}
	return model.PlatformSKUMappingResolution{Platform: platform, Mappings: mappings, UnmappedSKUs: unmapped}, nil
}

type mappingRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanPlatformSKUMappingRows(rows mappingRows) ([]model.PlatformSKUMapping, error) {
	records := make([]model.PlatformSKUMapping, 0)
	index := make(map[string]int)
	for rows.Next() {
		var mapping model.PlatformSKUMapping
		var item model.PlatformSKUMappingItem
		if err := rows.Scan(&mapping.Platform, &mapping.PlatformSKU, &mapping.Source, &mapping.Enabled,
			&mapping.CreatedAt, &mapping.UpdatedAt, &item.WarehouseSKU, &item.Quantity,
			&item.ProductName, &item.LengthCM, &item.WidthCM, &item.HeightCM, &item.WeightKG, &item.SpecComplete); err != nil {
			return nil, fmt.Errorf("scan platform SKU mapping: %w", err)
		}
		key := mapping.Platform + "\x00" + mapping.PlatformSKU
		position, exists := index[key]
		if !exists {
			position = len(records)
			index[key] = position
			mapping.Items = make([]model.PlatformSKUMappingItem, 0, 1)
			records = append(records, mapping)
		}
		records[position].Items = append(records[position].Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate platform SKU mappings: %w", err)
	}
	return records, nil
}

func (p *Postgres) ReplacePlatformSKUMapping(ctx context.Context, input model.PlatformSKUMapping) (model.PlatformSKUMapping, error) {
	normalized, err := NormalizePlatformSKUMapping(input)
	if err != nil {
		return model.PlatformSKUMapping{}, err
	}
	if err := p.replacePlatformSKUMappings(ctx, []model.PlatformSKUMapping{normalized}); err != nil {
		return model.PlatformSKUMapping{}, err
	}
	rows, err := p.pool.Query(ctx, `
		SELECT mapping.platform,mapping.platform_sku,mapping.source,mapping.enabled,
		       mapping.created_at,mapping.updated_at,mapping.warehouse_sku,mapping.quantity,
		       coalesce(spec.product_name,''),spec.length_cm,spec.width_cm,spec.height_cm,spec.weight_kg,
		       coalesce(spec.enabled AND spec.length_cm IS NOT NULL AND spec.width_cm IS NOT NULL
		         AND spec.height_cm IS NOT NULL AND spec.weight_kg IS NOT NULL,false)
		FROM xlwms_platform_sku_mappings mapping
		LEFT JOIN xlwms_warehouse_sku_specs spec ON spec.warehouse_sku=mapping.warehouse_sku
		WHERE mapping.platform=$1 AND mapping.platform_sku=$2
		ORDER BY mapping.warehouse_sku
	`, normalized.Platform, normalized.PlatformSKU)
	if err != nil {
		return model.PlatformSKUMapping{}, err
	}
	defer rows.Close()
	mappings, err := scanPlatformSKUMappingRows(rows)
	if err != nil {
		return model.PlatformSKUMapping{}, err
	}
	if len(mappings) != 1 {
		return model.PlatformSKUMapping{}, errors.New("saved platform SKU mapping could not be read")
	}
	return mappings[0], nil
}

func (p *Postgres) ImportPlatformSKUMappings(ctx context.Context, inputs []model.PlatformSKUMapping) (int, error) {
	if len(inputs) == 0 || len(inputs) > 1000 {
		return 0, errors.New("mappings must contain between 1 and 1000 values")
	}
	normalized := make([]model.PlatformSKUMapping, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		mapping, err := NormalizePlatformSKUMapping(input)
		if err != nil {
			return 0, err
		}
		key := mapping.Platform + "\x00" + mapping.PlatformSKU
		if _, exists := seen[key]; exists {
			return 0, fmt.Errorf("duplicate mapping for %s/%s", mapping.Platform, mapping.PlatformSKU)
		}
		seen[key] = struct{}{}
		normalized = append(normalized, mapping)
	}
	if err := p.replacePlatformSKUMappings(ctx, normalized); err != nil {
		return 0, err
	}
	return len(normalized), nil
}

func (p *Postgres) replacePlatformSKUMappings(ctx context.Context, mappings []model.PlatformSKUMapping) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin platform SKU mapping update: %w", err)
	}
	defer tx.Rollback(ctx)
	for _, mapping := range mappings {
		if _, err := tx.Exec(ctx, `DELETE FROM xlwms_platform_sku_mappings WHERE platform=$1 AND platform_sku=$2`, mapping.Platform, mapping.PlatformSKU); err != nil {
			return fmt.Errorf("replace platform SKU mapping: %w", err)
		}
		for _, item := range mapping.Items {
			if _, err := tx.Exec(ctx, `
				INSERT INTO xlwms_platform_sku_mappings(platform,platform_sku,warehouse_sku,quantity,source,enabled)
				VALUES($1,$2,$3,$4,$5,$6)
			`, mapping.Platform, mapping.PlatformSKU, item.WarehouseSKU, item.Quantity, mapping.Source, mapping.Enabled); err != nil {
				return fmt.Errorf("insert platform SKU mapping: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit platform SKU mapping update: %w", err)
	}
	return nil
}

func (p *Postgres) DeletePlatformSKUMapping(ctx context.Context, platform, platformSKU string) (bool, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	platformSKU = strings.TrimSpace(platformSKU)
	if !platformNamePattern.MatchString(platform) || platformSKU == "" {
		return false, errors.New("valid platform and platform_sku are required")
	}
	result, err := p.pool.Exec(ctx, `DELETE FROM xlwms_platform_sku_mappings WHERE platform=$1 AND platform_sku=$2`, platform, platformSKU)
	if err != nil {
		return false, fmt.Errorf("delete platform SKU mapping: %w", err)
	}
	return result.RowsAffected() > 0, nil
}
