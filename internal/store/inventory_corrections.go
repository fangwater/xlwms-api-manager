package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xlwms-api-manager/internal/model"

	"github.com/jackc/pgx/v5"
)

type InventoryCorrectionFilter struct {
	WarehouseCode string
	Query         string
	Page          int
	PageSize      int
}

func (p *Postgres) InventoryCorrectionsForSKUs(ctx context.Context, warehouseSKUs []string) (map[string]map[string]model.InventoryCorrection, error) {
	result := make(map[string]map[string]model.InventoryCorrection)
	if len(warehouseSKUs) == 0 {
		return result, nil
	}
	rows, err := p.pool.Query(ctx, `
		SELECT c.wh_code, c.warehouse_sku, c.correction_mode, c.correction_amount,
		       c.note, c.created_at, c.updated_at
		FROM xlwms_inventory_corrections c
		JOIN xlwms_warehouses w ON w.wh_code=c.wh_code AND w.is_active
		WHERE c.warehouse_sku=ANY($1)
	`, warehouseSKUs)
	if err != nil {
		return nil, fmt.Errorf("list inventory corrections for SKUs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item model.InventoryCorrection
		if err := rows.Scan(
			&item.WarehouseCode, &item.WarehouseSKU, &item.CorrectionMode, &item.CorrectionAmount,
			&item.Note, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan inventory correction: %w", err)
		}
		if result[item.WarehouseSKU] == nil {
			result[item.WarehouseSKU] = make(map[string]model.InventoryCorrection)
		}
		result[item.WarehouseSKU][item.WarehouseCode] = item
	}
	return result, rows.Err()
}

func (p *Postgres) ListInventoryCorrections(ctx context.Context, filter InventoryCorrectionFilter) ([]model.InventoryCorrection, int, error) {
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
	if warehouseCode := strings.ToUpper(strings.TrimSpace(filter.WarehouseCode)); warehouseCode != "" {
		where = append(where, "c.wh_code="+add(warehouseCode))
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		placeholder := add("%" + query + "%")
		where = append(where, "(c.warehouse_sku ILIKE "+placeholder+" OR coalesce(s.product_name,'') ILIKE "+placeholder+")")
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := p.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM xlwms_inventory_corrections c
		JOIN xlwms_warehouse_sku_specs s ON s.warehouse_sku=c.warehouse_sku
		WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count inventory corrections: %w", err)
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := p.pool.Query(ctx, `
		SELECT c.wh_code, coalesce(w.warehouse_name,''), c.warehouse_sku,
		       coalesce(s.product_name,''), coalesce(stock.raw_available_amount,0)::float8,
		       CASE WHEN c.correction_mode='subtract'
		         THEN greatest(coalesce(stock.raw_available_amount,0)-c.correction_amount,0)
		         ELSE c.correction_amount END::float8,
		       c.correction_mode, c.correction_amount::float8,
		       c.note, c.created_at, c.updated_at
		FROM xlwms_inventory_corrections c
		JOIN xlwms_warehouses w ON w.wh_code=c.wh_code
		JOIN xlwms_warehouse_sku_specs s ON s.warehouse_sku=c.warehouse_sku
		LEFT JOIN LATERAL (
			SELECT coalesce(sum(i.product_available_amount),0) AS raw_available_amount
			FROM xlwms_inventory_records i
			WHERE i.inventory_kind='integrated' AND i.stock_type=0
			  AND i.wh_code=c.wh_code AND i.sku=c.warehouse_sku
		) stock ON true
		WHERE `+clause+`
		ORDER BY c.updated_at DESC, c.wh_code, c.warehouse_sku
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list inventory corrections: %w", err)
	}
	defer rows.Close()
	items := make([]model.InventoryCorrection, 0, filter.PageSize)
	for rows.Next() {
		var item model.InventoryCorrection
		if err := rows.Scan(
			&item.WarehouseCode, &item.WarehouseName, &item.WarehouseSKU, &item.ProductName,
			&item.RawAvailableAmount, &item.CorrectedAvailableAmount,
			&item.CorrectionMode, &item.CorrectionAmount, &item.Note,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan inventory correction: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (p *Postgres) SaveInventoryCorrection(ctx context.Context, warehouseCode, warehouseSKU, correctionMode string, correctionAmount float64, note string) (model.InventoryCorrection, error) {
	warehouseCode = strings.ToUpper(strings.TrimSpace(warehouseCode))
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	correctionMode = strings.ToLower(strings.TrimSpace(correctionMode))
	note = strings.TrimSpace(note)
	if warehouseCode == "" || warehouseSKU == "" {
		return model.InventoryCorrection{}, errors.New("wh_code and warehouse_sku are required")
	}
	if correctionMode == "" {
		correctionMode = "absolute"
	}
	if correctionMode != "absolute" && correctionMode != "subtract" {
		return model.InventoryCorrection{}, errors.New("correction_mode must be absolute or subtract")
	}
	if correctionAmount < 0 {
		return model.InventoryCorrection{}, errors.New("correction_amount cannot be negative")
	}
	command, err := p.pool.Exec(ctx, `
		INSERT INTO xlwms_inventory_corrections (
			wh_code, warehouse_sku, corrected_available_amount, correction_mode, correction_amount, note, updated_at
		)
		SELECT w.wh_code, s.warehouse_sku, $4, $3, $4, $5, now()
		FROM xlwms_warehouses w
		JOIN xlwms_warehouse_sku_specs s ON s.warehouse_sku=$2
		WHERE w.wh_code=$1
		ON CONFLICT (wh_code, warehouse_sku) DO UPDATE SET
			corrected_available_amount=EXCLUDED.correction_amount,
			correction_mode=EXCLUDED.correction_mode,
			correction_amount=EXCLUDED.correction_amount,
			note=EXCLUDED.note,
			updated_at=now()
	`, warehouseCode, warehouseSKU, correctionMode, correctionAmount, note)
	if err != nil {
		return model.InventoryCorrection{}, fmt.Errorf("save inventory correction: %w", err)
	}
	if command.RowsAffected() == 0 {
		return model.InventoryCorrection{}, errors.New("unknown warehouse or warehouse SKU")
	}
	return p.inventoryCorrection(ctx, warehouseCode, warehouseSKU)
}

func (p *Postgres) DeleteInventoryCorrection(ctx context.Context, warehouseCode, warehouseSKU string) (bool, error) {
	command, err := p.pool.Exec(ctx, `
		DELETE FROM xlwms_inventory_corrections WHERE wh_code=$1 AND warehouse_sku=$2
	`, strings.ToUpper(strings.TrimSpace(warehouseCode)), strings.TrimSpace(warehouseSKU))
	if err != nil {
		return false, fmt.Errorf("delete inventory correction: %w", err)
	}
	return command.RowsAffected() > 0, nil
}

func (p *Postgres) inventoryCorrection(ctx context.Context, warehouseCode, warehouseSKU string) (model.InventoryCorrection, error) {
	var item model.InventoryCorrection
	err := p.pool.QueryRow(ctx, `
		SELECT c.wh_code, coalesce(w.warehouse_name,''), c.warehouse_sku,
		       coalesce(s.product_name,''), coalesce(sum(i.product_available_amount),0)::float8,
		       CASE WHEN c.correction_mode='subtract'
		         THEN greatest(coalesce(sum(i.product_available_amount),0)-c.correction_amount,0)
		         ELSE c.correction_amount END::float8,
		       c.correction_mode, c.correction_amount::float8,
		       c.note, c.created_at, c.updated_at
		FROM xlwms_inventory_corrections c
		JOIN xlwms_warehouses w ON w.wh_code=c.wh_code
		JOIN xlwms_warehouse_sku_specs s ON s.warehouse_sku=c.warehouse_sku
		LEFT JOIN xlwms_inventory_records i ON i.inventory_kind='integrated' AND i.stock_type=0
		 AND i.wh_code=c.wh_code AND i.sku=c.warehouse_sku
		WHERE c.wh_code=$1 AND c.warehouse_sku=$2
		GROUP BY c.wh_code, w.warehouse_name, c.warehouse_sku, s.product_name,
		         c.corrected_available_amount, c.correction_mode, c.correction_amount,
		         c.note, c.created_at, c.updated_at
	`, warehouseCode, warehouseSKU).Scan(
		&item.WarehouseCode, &item.WarehouseName, &item.WarehouseSKU, &item.ProductName,
		&item.RawAvailableAmount, &item.CorrectedAvailableAmount,
		&item.CorrectionMode, &item.CorrectionAmount, &item.Note,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.InventoryCorrection{}, errors.New("inventory correction not found")
	}
	if err != nil {
		return model.InventoryCorrection{}, fmt.Errorf("get inventory correction: %w", err)
	}
	return item, nil
}
