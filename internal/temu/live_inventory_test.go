package temu

import (
	"testing"
	"time"

	"xlwms-api-manager/internal/model"
)

func TestNestedNumberUsesProductInventoryOnly(t *testing.T) {
	record := map[string]any{
		"productStockDtl":   map[string]any{"availableAmount": float64(12)},
		"boxStockDtl":       map[string]any{"availableAmount": float64(100)},
		"fbaReturnStockDtl": map[string]any{"availableAmount": float64(200)},
	}
	if got := nestedNumber(record, "productStockDtl", "availableAmount"); got != 12 {
		t.Fatalf("got %v, want product availability 12", got)
	}
}

func TestApplyInventoryCorrectionsOverridesSuccessfulWarehouseInventory(t *testing.T) {
	updatedAt := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	result := LiveInventoryResult{InventoryBySKU: map[string]map[string]WarehouseInventory{
		"SKU-1": {
			"DPSCA004": {Active: true, QueryStatus: QuerySucceeded, SKUFound: true, AvailableAmount: 63, RawAvailableAmount: 63},
		},
	}}
	ApplyInventoryCorrections(&result, map[string]map[string]model.InventoryCorrection{
		"SKU-1": {
			"DPSCA004": {CorrectionMode: "absolute", CorrectionAmount: 0, Note: "warehouse count", UpdatedAt: updatedAt},
		},
	})
	got := result.InventoryBySKU["SKU-1"]["DPSCA004"]
	if !got.Corrected || got.AvailableAmount != 0 || got.RawAvailableAmount != 63 {
		t.Fatalf("unexpected corrected inventory: %#v", got)
	}
	if got.CorrectionNote != "warehouse count" || got.CorrectionUpdatedAt == nil || !got.CorrectionUpdatedAt.Equal(updatedAt) {
		t.Fatalf("missing correction metadata: %#v", got)
	}
}

func TestApplyInventoryCorrectionsDoesNotMaskFailedLiveQuery(t *testing.T) {
	result := LiveInventoryResult{InventoryBySKU: map[string]map[string]WarehouseInventory{
		"SKU-1": {
			"DPSCA004": {Active: true, QueryStatus: QueryFailed},
		},
	}}
	ApplyInventoryCorrections(&result, map[string]map[string]model.InventoryCorrection{
		"SKU-1": {"DPSCA004": {CorrectionMode: "absolute", CorrectionAmount: 10}},
	})
	got := result.InventoryBySKU["SKU-1"]["DPSCA004"]
	if got.Corrected || got.AvailableAmount != 0 {
		t.Fatalf("failed query must remain fail-closed: %#v", got)
	}
}

func TestApplyInventoryCorrectionsSubtractsFromLiveInventory(t *testing.T) {
	result := LiveInventoryResult{InventoryBySKU: map[string]map[string]WarehouseInventory{
		"SKU-1": {"DPSCA004": {Active: true, QueryStatus: QuerySucceeded, SKUFound: true, AvailableAmount: 254, RawAvailableAmount: 254}},
		"SKU-2": {"DPSCA004": {Active: true, QueryStatus: QuerySucceeded, SKUFound: true, AvailableAmount: 80, RawAvailableAmount: 80}},
	}}
	correction := model.InventoryCorrection{CorrectionMode: "subtract", CorrectionAmount: 100}
	ApplyInventoryCorrections(&result, map[string]map[string]model.InventoryCorrection{
		"SKU-1": {"DPSCA004": correction},
		"SKU-2": {"DPSCA004": correction},
	})
	if got := result.InventoryBySKU["SKU-1"]["DPSCA004"].AvailableAmount; got != 154 {
		t.Fatalf("got %v, want 154", got)
	}
	if got := result.InventoryBySKU["SKU-2"]["DPSCA004"].AvailableAmount; got != 0 {
		t.Fatalf("subtraction must floor at zero, got %v", got)
	}
}

func TestConfiguredTemuWarehousesUseBusinessAndActualCodes(t *testing.T) {
	rules := WarehouseRules()
	wantKeys := []string{"DPS002", "ARP_EAST", "DPS004", "ARP_WEST"}
	wantCodes := []string{"DPSNY002", "HYTX30", "DPSCA004", "ARPCA01"}
	if len(rules) != len(wantCodes) {
		t.Fatalf("got %#v", rules)
	}
	for index := range wantCodes {
		if rules[index].Key != wantKeys[index] || rules[index].Code != wantCodes[index] {
			t.Fatalf("got %#v", rules)
		}
	}
}
