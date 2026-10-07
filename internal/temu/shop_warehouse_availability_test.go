package temu

import "testing"

func TestShopWarehousePausePreservesInventoryAndExcludesSelection(t *testing.T) {
	decision := SKUDecision{SKU: "test", TotalAvailableAmount: 100, RegionDecisions: []RegionDecision{{Region: "central", Warehouses: []WarehouseDecision{{WarehouseKey: "ARP_HOUSTON", Selectable: true, Recommended: true}}}, {Region: "east", Warehouses: []WarehouseDecision{{WarehouseKey: "ARP_EAST", Selectable: true}}}}}
	ApplyShopWarehouseAvailability(&decision, map[string]bool{"ARP_EAST": true})
	if decision.Warehouses[0].Selectable || decision.Warehouses[0].Recommended || decision.TotalAvailableAmount != 100 || !decision.Warehouses[1].Selectable {
		t.Fatal("pause changed inventory or leaked eligibility")
	}
	ApplyShopWarehouseAvailability(&decision, map[string]bool{})
	if !decision.RequiresManual {
		t.Fatal("all shops paused must require manual handling")
	}
}
