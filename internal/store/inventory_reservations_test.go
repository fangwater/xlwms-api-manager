package store

import (
	"errors"
	"testing"
	"time"

	"xlwms-api-manager/internal/model"
)

func TestNormalizeInventoryReservationAggregatesAndSortsItems(t *testing.T) {
	now := time.Now()
	request, err := normalizeInventoryReservation(model.FulfillmentInventoryReservationRequest{
		Platform: " TEMU ", ShopCode: "Panda-Homes", OrderKey: " PO-1 ",
		WarehouseKey: "dps002", WarehouseCode: "us-east", ObservedAt: now,
		Items: []model.FulfillmentInventoryReservationItem{
			{WarehouseSKU: "B", Quantity: 1, ObservedAvailable: 8},
			{WarehouseSKU: "A", Quantity: 2, ObservedAvailable: 5},
			{WarehouseSKU: "A", Quantity: 1, ObservedAvailable: 4},
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if request.Platform != "temu" || request.ShopCode != "panda-homes" || request.OrderKey != "PO-1" {
		t.Fatalf("unexpected normalized identity: %#v", request)
	}
	if request.WarehouseKey != "DPS002" || request.WarehouseCode != "US-EAST" {
		t.Fatalf("unexpected normalized warehouse: %#v", request)
	}
	if len(request.Items) != 2 || request.Items[0].WarehouseSKU != "A" || request.Items[0].Quantity != 3 || request.Items[0].ObservedAvailable != 4 || request.Items[1].WarehouseSKU != "B" {
		t.Fatalf("unexpected normalized items: %#v", request.Items)
	}
}

func TestNormalizeInventoryReservationRejectsStaleObservation(t *testing.T) {
	now := time.Now()
	_, err := normalizeInventoryReservation(model.FulfillmentInventoryReservationRequest{
		Platform: "temu", ShopCode: "panda-homes", OrderKey: "PO-1",
		WarehouseKey: "DPS002", WarehouseCode: "US-EAST", ObservedAt: now.Add(-reservationObservationMaxAge - time.Second),
		Items: []model.FulfillmentInventoryReservationItem{{WarehouseSKU: "A", Quantity: 1, ObservedAvailable: 1}},
	}, now)
	if !errors.Is(err, ErrInvalidInventoryReservation) {
		t.Fatalf("got %v, want invalid reservation", err)
	}
}

func TestInventoryReservationCapacityErrorSupportsErrorsIs(t *testing.T) {
	err := &InventoryReservationCapacityError{WarehouseCode: "WH", WarehouseSKU: "A", Requested: 2, ObservedAvailable: 2, AlreadyReserved: 1}
	if !errors.Is(err, ErrInventoryReservationCapacity) {
		t.Fatalf("capacity error must wrap sentinel: %v", err)
	}
}
