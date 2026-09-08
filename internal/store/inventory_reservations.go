package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"xlwms-api-manager/internal/model"

	"github.com/jackc/pgx/v5"
)

const (
	fulfillmentInventoryReservationTTL = 24 * time.Hour
	reservationObservationMaxAge       = 10 * time.Minute
)

var (
	ErrInvalidInventoryReservation  = errors.New("invalid fulfillment inventory reservation")
	ErrInventoryReservationCapacity = errors.New("fulfillment inventory reservation capacity exhausted")
)

type InventoryReservationCapacityError struct {
	WarehouseCode     string
	WarehouseSKU      string
	Requested         int
	ObservedAvailable int
	AlreadyReserved   int
}

func (e *InventoryReservationCapacityError) Error() string {
	return fmt.Sprintf("warehouse %s SKU %s has %d observed available, %d already reserved, cannot reserve %d",
		e.WarehouseCode, e.WarehouseSKU, e.ObservedAvailable, e.AlreadyReserved, e.Requested)
}

func (e *InventoryReservationCapacityError) Unwrap() error {
	return ErrInventoryReservationCapacity
}

type inventoryReservationResource struct {
	warehouseCode string
	warehouseSKU  string
}

func (p *Postgres) ReserveFulfillmentInventory(ctx context.Context, request model.FulfillmentInventoryReservationRequest) (model.FulfillmentInventoryReservation, error) {
	now := time.Now()
	request, err := normalizeInventoryReservation(request, now)
	if err != nil {
		return model.FulfillmentInventoryReservation{}, err
	}
	if _, err := p.FulfillmentShop(ctx, request.Platform, request.ShopCode); err != nil {
		return model.FulfillmentInventoryReservation{}, err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return model.FulfillmentInventoryReservation{}, fmt.Errorf("begin fulfillment inventory reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	orderLock := strings.Join([]string{"fulfillment-inventory-order", request.Platform, request.ShopCode, request.OrderKey}, "|")
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, orderLock); err != nil {
		return model.FulfillmentInventoryReservation{}, fmt.Errorf("lock fulfillment order reservation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE xlwms_fulfillment_inventory_reservations
SET status='expired', updated_at=$4
WHERE platform=$1 AND shop_code=$2 AND order_key=$3 AND status='active' AND expires_at<=$4
`, request.Platform, request.ShopCode, request.OrderKey, now); err != nil {
		return model.FulfillmentInventoryReservation{}, fmt.Errorf("expire order inventory reservations: %w", err)
	}

	existing, err := loadActiveOrderReservations(ctx, tx, request.Platform, request.ShopCode, request.OrderKey, now)
	if err != nil {
		return model.FulfillmentInventoryReservation{}, err
	}
	resources := reservationResources(request, existing)
	for _, resource := range resources {
		resourceLock := strings.Join([]string{"fulfillment-inventory-resource", resource.warehouseCode, resource.warehouseSKU}, "|")
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, resourceLock); err != nil {
			return model.FulfillmentInventoryReservation{}, fmt.Errorf("lock fulfillment inventory resource: %w", err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE xlwms_fulfillment_inventory_reservations
SET status='expired', updated_at=$3
WHERE wh_code=$1 AND warehouse_sku=$2 AND status='active' AND expires_at<=$3
`, resource.warehouseCode, resource.warehouseSKU, now); err != nil {
			return model.FulfillmentInventoryReservation{}, fmt.Errorf("expire fulfillment inventory reservations: %w", err)
		}
	}

	existing, err = loadActiveOrderReservations(ctx, tx, request.Platform, request.ShopCode, request.OrderKey, now)
	if err != nil {
		return model.FulfillmentInventoryReservation{}, err
	}
	expiresAt := now.Add(fulfillmentInventoryReservationTTL)
	if sameInventoryReservation(existing, request) {
		if _, err := tx.Exec(ctx, `
UPDATE xlwms_fulfillment_inventory_reservations
SET expires_at=$4, updated_at=$5
WHERE platform=$1 AND shop_code=$2 AND order_key=$3 AND status='active'
`, request.Platform, request.ShopCode, request.OrderKey, expiresAt, now); err != nil {
			return model.FulfillmentInventoryReservation{}, fmt.Errorf("extend fulfillment inventory reservation: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return model.FulfillmentInventoryReservation{}, fmt.Errorf("commit fulfillment inventory reservation: %w", err)
		}
		return inventoryReservationResult(request, false, expiresAt), nil
	}

	for _, item := range request.Items {
		var reserved int
		if err := tx.QueryRow(ctx, `
SELECT coalesce(sum(quantity), 0)::integer
FROM xlwms_fulfillment_inventory_reservations
WHERE wh_code=$1 AND warehouse_sku=$2 AND status='active' AND expires_at>$3
  AND NOT (platform=$4 AND shop_code=$5 AND order_key=$6)
`, request.WarehouseCode, item.WarehouseSKU, now, request.Platform, request.ShopCode, request.OrderKey).Scan(&reserved); err != nil {
			return model.FulfillmentInventoryReservation{}, fmt.Errorf("sum fulfillment inventory reservations: %w", err)
		}
		if item.Quantity > item.ObservedAvailable-reserved {
			return model.FulfillmentInventoryReservation{}, &InventoryReservationCapacityError{
				WarehouseCode: request.WarehouseCode, WarehouseSKU: item.WarehouseSKU,
				Requested: item.Quantity, ObservedAvailable: item.ObservedAvailable, AlreadyReserved: reserved,
			}
		}
	}

	if _, err := tx.Exec(ctx, `
UPDATE xlwms_fulfillment_inventory_reservations
SET status='released', released_at=$4, updated_at=$4
WHERE platform=$1 AND shop_code=$2 AND order_key=$3 AND status='active'
`, request.Platform, request.ShopCode, request.OrderKey, now); err != nil {
		return model.FulfillmentInventoryReservation{}, fmt.Errorf("replace fulfillment inventory reservation: %w", err)
	}
	for _, item := range request.Items {
		if _, err := tx.Exec(ctx, `
INSERT INTO xlwms_fulfillment_inventory_reservations(
    platform,shop_code,order_key,warehouse_key,wh_code,warehouse_sku,quantity,
    observed_available,observed_at,status,expires_at,released_at,created_at,updated_at
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'active',$10,NULL,$11,$11)
ON CONFLICT(platform,shop_code,order_key,warehouse_sku) DO UPDATE SET
    warehouse_key=excluded.warehouse_key,
    wh_code=excluded.wh_code,
    quantity=excluded.quantity,
    observed_available=excluded.observed_available,
    observed_at=excluded.observed_at,
    status='active',
    expires_at=excluded.expires_at,
    released_at=NULL,
    created_at=excluded.created_at,
    updated_at=excluded.updated_at
`, request.Platform, request.ShopCode, request.OrderKey, request.WarehouseKey, request.WarehouseCode,
			item.WarehouseSKU, item.Quantity, item.ObservedAvailable, request.ObservedAt, expiresAt, now); err != nil {
			return model.FulfillmentInventoryReservation{}, fmt.Errorf("save fulfillment inventory reservation: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.FulfillmentInventoryReservation{}, fmt.Errorf("commit fulfillment inventory reservation: %w", err)
	}
	return inventoryReservationResult(request, true, expiresAt), nil
}

func (p *Postgres) ReleaseFulfillmentInventory(ctx context.Context, release model.FulfillmentInventoryReservationRelease) (bool, error) {
	platform, shopCode, err := normalizeShopIdentity(release.Platform, release.ShopCode)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrInvalidInventoryReservation, err)
	}
	orderKey := strings.TrimSpace(release.OrderKey)
	if orderKey == "" {
		return false, fmt.Errorf("%w: order_key is required", ErrInvalidInventoryReservation)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin fulfillment inventory release: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	orderLock := strings.Join([]string{"fulfillment-inventory-order", platform, shopCode, orderKey}, "|")
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, orderLock); err != nil {
		return false, fmt.Errorf("lock fulfillment order release: %w", err)
	}
	tag, err := tx.Exec(ctx, `
UPDATE xlwms_fulfillment_inventory_reservations
SET status='released', released_at=now(), updated_at=now()
WHERE platform=$1 AND shop_code=$2 AND order_key=$3 AND status='active'
`, platform, shopCode, orderKey)
	if err != nil {
		return false, fmt.Errorf("release fulfillment inventory reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit fulfillment inventory release: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func normalizeInventoryReservation(request model.FulfillmentInventoryReservationRequest, now time.Time) (model.FulfillmentInventoryReservationRequest, error) {
	platform, shopCode, err := normalizeShopIdentity(request.Platform, request.ShopCode)
	if err != nil {
		return request, fmt.Errorf("%w: %v", ErrInvalidInventoryReservation, err)
	}
	request.Platform = platform
	request.ShopCode = shopCode
	request.OrderKey = strings.TrimSpace(request.OrderKey)
	request.WarehouseKey = strings.ToUpper(strings.TrimSpace(request.WarehouseKey))
	request.WarehouseCode = strings.ToUpper(strings.TrimSpace(request.WarehouseCode))
	if request.OrderKey == "" || request.WarehouseKey == "" || request.WarehouseCode == "" {
		return request, fmt.Errorf("%w: order_key, warehouse_key and wh_code are required", ErrInvalidInventoryReservation)
	}
	if request.ObservedAt.IsZero() || request.ObservedAt.Before(now.Add(-reservationObservationMaxAge)) || request.ObservedAt.After(now.Add(time.Minute)) {
		return request, fmt.Errorf("%w: observed_at must be within the last %s", ErrInvalidInventoryReservation, reservationObservationMaxAge)
	}
	if len(request.Items) == 0 || len(request.Items) > 100 {
		return request, fmt.Errorf("%w: items must contain between 1 and 100 SKUs", ErrInvalidInventoryReservation)
	}
	type aggregate struct {
		quantity  int
		available int
	}
	bySKU := make(map[string]aggregate, len(request.Items))
	for _, item := range request.Items {
		sku := strings.TrimSpace(item.WarehouseSKU)
		if sku == "" || item.Quantity <= 0 || item.ObservedAvailable < 0 {
			return request, fmt.Errorf("%w: every item requires warehouse_sku, positive quantity and non-negative observed_available", ErrInvalidInventoryReservation)
		}
		current, exists := bySKU[sku]
		if !exists {
			bySKU[sku] = aggregate{quantity: item.Quantity, available: item.ObservedAvailable}
			continue
		}
		current.quantity += item.Quantity
		if item.ObservedAvailable < current.available {
			current.available = item.ObservedAvailable
		}
		bySKU[sku] = current
	}
	request.Items = make([]model.FulfillmentInventoryReservationItem, 0, len(bySKU))
	for sku, item := range bySKU {
		request.Items = append(request.Items, model.FulfillmentInventoryReservationItem{
			WarehouseSKU: sku, Quantity: item.quantity, ObservedAvailable: item.available,
		})
	}
	sort.Slice(request.Items, func(i, j int) bool { return request.Items[i].WarehouseSKU < request.Items[j].WarehouseSKU })
	return request, nil
}

func loadActiveOrderReservations(ctx context.Context, tx pgx.Tx, platform, shopCode, orderKey string, now time.Time) ([]model.FulfillmentInventoryReservation, error) {
	rows, err := tx.Query(ctx, `
SELECT warehouse_key,wh_code,warehouse_sku,quantity,observed_available,observed_at,expires_at
FROM xlwms_fulfillment_inventory_reservations
WHERE platform=$1 AND shop_code=$2 AND order_key=$3 AND status='active' AND expires_at>$4
ORDER BY warehouse_sku
`, platform, shopCode, orderKey, now)
	if err != nil {
		return nil, fmt.Errorf("load fulfillment inventory reservation: %w", err)
	}
	defer rows.Close()
	result := make([]model.FulfillmentInventoryReservation, 0)
	for rows.Next() {
		var warehouseKey, warehouseCode string
		var item model.FulfillmentInventoryReservationItem
		var observedAt, expiresAt time.Time
		if err := rows.Scan(&warehouseKey, &warehouseCode, &item.WarehouseSKU, &item.Quantity, &item.ObservedAvailable, &observedAt, &expiresAt); err != nil {
			return nil, fmt.Errorf("scan fulfillment inventory reservation: %w", err)
		}
		result = append(result, model.FulfillmentInventoryReservation{
			Platform: platform, ShopCode: shopCode, OrderKey: orderKey, WarehouseKey: warehouseKey,
			WarehouseCode: warehouseCode, ObservedAt: observedAt, Items: []model.FulfillmentInventoryReservationItem{item}, ExpiresAt: expiresAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fulfillment inventory reservation: %w", err)
	}
	return result, nil
}

func reservationResources(request model.FulfillmentInventoryReservationRequest, existing []model.FulfillmentInventoryReservation) []inventoryReservationResource {
	unique := make(map[string]inventoryReservationResource, len(request.Items)+len(existing))
	add := func(code, sku string) {
		key := fmt.Sprintf("%d:%s:%s", len(code), code, sku)
		unique[key] = inventoryReservationResource{warehouseCode: code, warehouseSKU: sku}
	}
	for _, item := range request.Items {
		add(request.WarehouseCode, item.WarehouseSKU)
	}
	for _, reservation := range existing {
		for _, item := range reservation.Items {
			add(reservation.WarehouseCode, item.WarehouseSKU)
		}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	resources := make([]inventoryReservationResource, 0, len(keys))
	for _, key := range keys {
		resources = append(resources, unique[key])
	}
	return resources
}

func sameInventoryReservation(existing []model.FulfillmentInventoryReservation, request model.FulfillmentInventoryReservationRequest) bool {
	if len(existing) != len(request.Items) {
		return false
	}
	for i, reservation := range existing {
		if reservation.WarehouseKey != request.WarehouseKey || reservation.WarehouseCode != request.WarehouseCode || len(reservation.Items) != 1 {
			return false
		}
		stored := reservation.Items[0]
		requested := request.Items[i]
		if stored.WarehouseSKU != requested.WarehouseSKU || stored.Quantity != requested.Quantity {
			return false
		}
	}
	return true
}

func inventoryReservationResult(request model.FulfillmentInventoryReservationRequest, created bool, expiresAt time.Time) model.FulfillmentInventoryReservation {
	return model.FulfillmentInventoryReservation{
		Platform: request.Platform, ShopCode: request.ShopCode, OrderKey: request.OrderKey,
		WarehouseKey: request.WarehouseKey, WarehouseCode: request.WarehouseCode,
		ObservedAt: request.ObservedAt, Items: request.Items, Created: created, ExpiresAt: expiresAt,
	}
}
