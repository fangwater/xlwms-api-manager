package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"xlwms-api-manager/internal/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const skuCombinationColumns = `id, name, coalesce(substitute_for_sku, ''), length_cm, width_cm, height_cm, weight_kg,
	calculated_length_cm, calculated_width_cm, calculated_height_cm, calculated_weight_kg,
	coalesce(note, ''), enabled, created_at, updated_at`

type SKUCombinationFilter struct {
	Query  string
	Status string
}

func scanSKUCombination(row rowScanner) (model.SKUCombination, error) {
	var item model.SKUCombination
	err := row.Scan(&item.ID, &item.Name, &item.SubstituteForSKU, &item.LengthCM, &item.WidthCM, &item.HeightCM, &item.WeightKG,
		&item.CalculatedLengthCM, &item.CalculatedWidthCM, &item.CalculatedHeightCM, &item.CalculatedWeightKG,
		&item.Note, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	if err == nil {
		item.Items = make([]model.SKUCombinationItem, 0)
		item.Corrected = skuCombinationCorrected(item)
	}
	return item, err
}

func skuCombinationCorrected(item model.SKUCombination) bool {
	values := []struct {
		actual     float64
		calculated *float64
	}{
		{item.LengthCM, item.CalculatedLengthCM}, {item.WidthCM, item.CalculatedWidthCM},
		{item.HeightCM, item.CalculatedHeightCM}, {item.WeightKG, item.CalculatedWeightKG},
	}
	for _, value := range values {
		if value.calculated != nil && math.Abs(value.actual-*value.calculated) > 0.000001 {
			return true
		}
	}
	return false
}

func validateSKUCombination(item model.SKUCombination) (model.SKUCombination, error) {
	item.Name = strings.TrimSpace(item.Name)
	item.SubstituteForSKU = strings.TrimSpace(item.SubstituteForSKU)
	item.Note = strings.TrimSpace(item.Note)
	if item.Name == "" || len([]rune(item.Name)) > 120 {
		return item, errors.New("name is required and cannot exceed 120 characters")
	}
	if len(item.Items) == 0 || len(item.Items) > 40 {
		return item, errors.New("items must contain between 1 and 40 SKU entries")
	}
	for name, value := range map[string]float64{
		"length_cm": item.LengthCM, "width_cm": item.WidthCM, "height_cm": item.HeightCM, "weight_kg": item.WeightKG,
	} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
			return item, fmt.Errorf("%s must be a positive finite number", name)
		}
	}
	calculated := map[string]*float64{
		"calculated_length_cm": item.CalculatedLengthCM, "calculated_width_cm": item.CalculatedWidthCM,
		"calculated_height_cm": item.CalculatedHeightCM, "calculated_weight_kg": item.CalculatedWeightKG,
	}
	nonNilCalculated := 0
	for name, value := range calculated {
		if value == nil {
			continue
		}
		nonNilCalculated++
		if math.IsNaN(*value) || math.IsInf(*value, 0) || *value <= 0 {
			return item, fmt.Errorf("%s must be a positive finite number", name)
		}
	}
	if nonNilCalculated != 0 && nonNilCalculated != len(calculated) {
		return item, errors.New("all calculated package values must be provided together")
	}
	seen := make(map[string]struct{}, len(item.Items))
	total := 0
	for index := range item.Items {
		item.Items[index].WarehouseSKU = strings.TrimSpace(item.Items[index].WarehouseSKU)
		sku := item.Items[index].WarehouseSKU
		if sku == "" || len(sku) > 255 || item.Items[index].Quantity <= 0 {
			return item, errors.New("each item requires a warehouse_sku and positive quantity")
		}
		if _, exists := seen[sku]; exists {
			return item, fmt.Errorf("warehouse_sku %s is duplicated", sku)
		}
		if sku == item.SubstituteForSKU {
			return item, errors.New("substitute_for_sku cannot also be a combination item")
		}
		seen[sku] = struct{}{}
		total += item.Items[index].Quantity
		if total > 300 {
			return item, errors.New("total item quantity cannot exceed 300")
		}
	}
	return item, nil
}

// ValidateSKUCombinationForAPI lets handlers reject invalid payloads before store access.
func ValidateSKUCombinationForAPI(item model.SKUCombination) (model.SKUCombination, error) {
	return validateSKUCombination(item)
}

func (p *Postgres) ListSKUCombinations(ctx context.Context, filter SKUCombinationFilter) ([]model.SKUCombination, error) {
	where := []string{"1=1"}
	args := make([]any, 0, 1)
	if query := strings.TrimSpace(filter.Query); query != "" {
		args = append(args, "%"+query+"%")
		where = append(where, `(name ILIKE $1 OR coalesce(substitute_for_sku, '') ILIKE $1 OR EXISTS (
			SELECT 1 FROM xlwms_sku_combination_items i WHERE i.combination_id=xlwms_sku_combinations.id AND i.warehouse_sku ILIKE $1))`)
	}
	switch strings.ToLower(strings.TrimSpace(filter.Status)) {
	case "", "all":
	case "active":
		where = append(where, "enabled")
	case "disabled":
		where = append(where, "NOT enabled")
	default:
		return nil, errors.New("status must be all, active, or disabled")
	}
	rows, err := p.pool.Query(ctx, `SELECT `+skuCombinationColumns+` FROM xlwms_sku_combinations WHERE `+strings.Join(where, " AND ")+` ORDER BY updated_at DESC, id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list SKU combinations: %w", err)
	}
	defer rows.Close()
	items := make([]model.SKUCombination, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		item, scanErr := scanSKUCombination(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan SKU combination: %w", scanErr)
		}
		items, ids = append(items, item), append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list SKU combinations: %w", err)
	}
	if err := p.loadSKUCombinationItems(ctx, items, ids); err != nil {
		return nil, err
	}
	return items, nil
}

func (p *Postgres) SKUCombination(ctx context.Context, id int64) (model.SKUCombination, error) {
	item, err := scanSKUCombination(p.pool.QueryRow(ctx, `SELECT `+skuCombinationColumns+` FROM xlwms_sku_combinations WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, errors.New("unknown SKU combination")
	}
	if err != nil {
		return item, fmt.Errorf("get SKU combination: %w", err)
	}
	items := []model.SKUCombination{item}
	if err := p.loadSKUCombinationItems(ctx, items, []int64{id}); err != nil {
		return item, err
	}
	return items[0], nil
}

func (p *Postgres) SKUCombinationForSubstitution(ctx context.Context, warehouseSKU string) (model.SKUCombination, error) {
	warehouseSKU = strings.TrimSpace(warehouseSKU)
	item, err := scanSKUCombination(p.pool.QueryRow(ctx, `SELECT `+skuCombinationColumns+` FROM xlwms_sku_combinations WHERE substitute_for_sku=$1 AND enabled`, warehouseSKU))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, errors.New("no active substitution for warehouse SKU")
	}
	if err != nil {
		return item, fmt.Errorf("get SKU substitution: %w", err)
	}
	items := []model.SKUCombination{item}
	if err := p.loadSKUCombinationItems(ctx, items, []int64{item.ID}); err != nil {
		return item, err
	}
	return items[0], nil
}

func (p *Postgres) loadSKUCombinationItems(ctx context.Context, combinations []model.SKUCombination, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	byID := make(map[int64]int, len(ids))
	for index, id := range ids {
		byID[id] = index
	}
	rows, err := p.pool.Query(ctx, `
		SELECT i.combination_id, i.warehouse_sku, coalesce(s.product_name, ''), i.quantity,
		       s.length_cm, s.width_cm, s.height_cm, s.weight_kg
		FROM xlwms_sku_combination_items i
		JOIN xlwms_warehouse_sku_specs s ON s.warehouse_sku=i.warehouse_sku
		WHERE i.combination_id=ANY($1) ORDER BY i.combination_id, i.warehouse_sku
	`, ids)
	if err != nil {
		return fmt.Errorf("list SKU combination items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var member model.SKUCombinationItem
		if err := rows.Scan(&id, &member.WarehouseSKU, &member.ProductName, &member.Quantity,
			&member.LengthCM, &member.WidthCM, &member.HeightCM, &member.WeightKG); err != nil {
			return fmt.Errorf("scan SKU combination item: %w", err)
		}
		if index, exists := byID[id]; exists {
			combinations[index].Items = append(combinations[index].Items, member)
		}
	}
	return rows.Err()
}

func (p *Postgres) CreateSKUCombination(ctx context.Context, item model.SKUCombination) (model.SKUCombination, error) {
	return p.saveSKUCombination(ctx, 0, item)
}

func (p *Postgres) UpdateSKUCombination(ctx context.Context, id int64, item model.SKUCombination) (model.SKUCombination, error) {
	if id <= 0 {
		return item, errors.New("invalid SKU combination id")
	}
	return p.saveSKUCombination(ctx, id, item)
}

func (p *Postgres) saveSKUCombination(ctx context.Context, id int64, item model.SKUCombination) (model.SKUCombination, error) {
	item, err := validateSKUCombination(item)
	if err != nil {
		return item, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return item, fmt.Errorf("begin SKU combination transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	allSKUs := make([]string, 0, len(item.Items)+1)
	for _, member := range item.Items {
		allSKUs = append(allSKUs, member.WarehouseSKU)
	}
	if item.SubstituteForSKU != "" {
		allSKUs = append(allSKUs, item.SubstituteForSKU)
	}
	var existing int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM xlwms_warehouse_sku_specs WHERE warehouse_sku=ANY($1)`, allSKUs).Scan(&existing); err != nil {
		return item, fmt.Errorf("validate combination SKUs: %w", err)
	}
	if existing != len(allSKUs) {
		return item, errors.New("one or more warehouse SKUs do not exist")
	}

	arguments := []any{item.Name, nullableString(item.SubstituteForSKU), item.LengthCM, item.WidthCM, item.HeightCM, item.WeightKG,
		item.CalculatedLengthCM, item.CalculatedWidthCM, item.CalculatedHeightCM, item.CalculatedWeightKG, item.Note, item.Enabled}
	var row pgx.Row
	if id == 0 {
		row = tx.QueryRow(ctx, `INSERT INTO xlwms_sku_combinations
			(name, substitute_for_sku, length_cm, width_cm, height_cm, weight_kg,
			 calculated_length_cm, calculated_width_cm, calculated_height_cm, calculated_weight_kg, note, enabled)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+skuCombinationColumns, arguments...)
	} else {
		arguments = append(arguments, id)
		row = tx.QueryRow(ctx, `UPDATE xlwms_sku_combinations SET
			name=$1, substitute_for_sku=$2, length_cm=$3, width_cm=$4, height_cm=$5, weight_kg=$6,
			calculated_length_cm=$7, calculated_width_cm=$8, calculated_height_cm=$9, calculated_weight_kg=$10,
			note=$11, enabled=$12, updated_at=now() WHERE id=$13 RETURNING `+skuCombinationColumns, arguments...)
	}
	saved, err := scanSKUCombination(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, errors.New("unknown SKU combination")
	}
	if err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return item, errors.New("substitute_for_sku already has a combination")
		}
		return item, fmt.Errorf("save SKU combination: %w", err)
	}
	if id != 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM xlwms_sku_combination_items WHERE combination_id=$1`, saved.ID); err != nil {
			return item, fmt.Errorf("replace SKU combination items: %w", err)
		}
	}
	for _, member := range item.Items {
		if _, err := tx.Exec(ctx, `INSERT INTO xlwms_sku_combination_items (combination_id, warehouse_sku, quantity) VALUES ($1,$2,$3)`, saved.ID, member.WarehouseSKU, member.Quantity); err != nil {
			return item, fmt.Errorf("save SKU combination item: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return item, fmt.Errorf("commit SKU combination: %w", err)
	}
	return p.SKUCombination(ctx, saved.ID)
}

func (p *Postgres) DeleteSKUCombination(ctx context.Context, id int64) error {
	command, err := p.pool.Exec(ctx, `DELETE FROM xlwms_sku_combinations WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete SKU combination: %w", err)
	}
	if command.RowsAffected() == 0 {
		return errors.New("unknown SKU combination")
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
