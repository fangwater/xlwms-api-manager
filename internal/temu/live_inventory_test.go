package temu

import "testing"

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
