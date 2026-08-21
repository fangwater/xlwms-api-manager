package temu

import (
	"testing"

	"xlwms-api-manager/internal/model"
)

func TestBuildSKUDecisionPrioritizesDPSWhenBothWarehousesHaveStock(t *testing.T) {
	decision := BuildSKUDecision("SKU-1", completeInventory(30, 30, 35, 25), defaultThresholds())
	if decision.RequiresManual {
		t.Fatalf("expected automatic selection, got %#v", decision)
	}
	assertRegion(t, decision, RegionEast, "DPS002", "DPS_PRIORITY_CLEAR_STOCK", false)
	assertRegion(t, decision, RegionWest, "DPS004", "DPS_PRIORITY_CLEAR_STOCK", false)
}

func TestBuildSKUDecisionUsesInclusiveSafetyThreshold(t *testing.T) {
	decision := BuildSKUDecision("SKU-1", completeInventory(25, 25, 60, 0), defaultThresholds())
	if !decision.RequiresManual {
		t.Fatal("stock equal to the threshold must require manual review")
	}
	east := regionByName(t, decision, RegionEast)
	if east.DecisionCode != "MANUAL_LOW_REGIONAL_STOCK" || east.AvailableAmount != 50 {
		t.Fatalf("unexpected east decision: %#v", east)
	}
	if east.RecommendedWarehouse != "" {
		t.Fatalf("low-stock region must not recommend a warehouse: %#v", east)
	}
}

func TestBuildSKUDecisionFallsBackToARPWhenDPSHasNoAvailableStock(t *testing.T) {
	decision := BuildSKUDecision("SKU-1", completeInventory(0, 60, 0, 70), defaultThresholds())
	assertRegion(t, decision, RegionEast, "ARP_EAST", "ARP_FALLBACK_DPS_OUT_OF_STOCK", false)
	assertRegion(t, decision, RegionWest, "ARP_WEST", "ARP_FALLBACK_DPS_OUT_OF_STOCK", false)
	for _, region := range decision.RegionDecisions {
		if region.Warehouses[0].Selectable {
			t.Fatalf("zero-stock DPS warehouse must not be selectable: %#v", region.Warehouses[0])
		}
	}
}

func TestBuildSKUDecisionReportsCorrectedInventory(t *testing.T) {
	inventory := completeInventory(60, 10, 63, 20)
	west := inventory["DPSCA004"]
	west.RawAvailableAmount = 63
	west.AvailableAmount = 0
	west.Corrected = true
	inventory["DPSCA004"] = west
	decision := BuildSKUDecision("SKU-1", inventory, defaultThresholds())
	region := regionByName(t, decision, RegionWest)
	corrected := region.Warehouses[0]
	if corrected.Selectable || !corrected.Corrected || corrected.RawAvailableAmount != 63 || corrected.ReasonCode != "CORRECTED_ZERO_AVAILABLE_STOCK" {
		t.Fatalf("unexpected corrected warehouse decision: %#v", corrected)
	}
}

func TestBuildSKUDecisionRequiresManualReviewWhenQueryIsIncomplete(t *testing.T) {
	inventory := completeInventory(60, 10, 60, 10)
	failed := inventory["DPSNY002"]
	failed.QueryStatus = QueryFailed
	failed.AvailableAmount = 0
	inventory["DPSNY002"] = failed
	decision := BuildSKUDecision("SKU-1", inventory, defaultThresholds())
	east := regionByName(t, decision, RegionEast)
	if !east.RequiresManual || east.DecisionCode != "MANUAL_INVENTORY_QUERY_INCOMPLETE" {
		t.Fatalf("failed warehouse query must require manual review: %#v", east)
	}
}

func TestBuildSKUDecisionTreatsMissingSKUAsZeroInventory(t *testing.T) {
	inventory := completeInventory(0, 60, 60, 10)
	dps := inventory["DPSNY002"]
	dps.SKUFound = false
	inventory["DPSNY002"] = dps
	decision := BuildSKUDecision("SKU-1", inventory, defaultThresholds())
	east := regionByName(t, decision, RegionEast)
	if east.Warehouses[0].Selectable || east.Warehouses[0].ReasonCode != "ZERO_AVAILABLE_STOCK" {
		t.Fatalf("missing SKU must be treated as zero stock: %#v", east.Warehouses[0])
	}
}

func TestBuildSKUDecisionUsesPerSKURegionalThresholds(t *testing.T) {
	thresholds := model.InventoryThresholds{EastThreshold: 80, WestThreshold: 20, TotalThreshold: 0}
	decision := BuildSKUDecision("SKU-1", completeInventory(30, 30, 35, 25), thresholds)
	east := regionByName(t, decision, RegionEast)
	west := regionByName(t, decision, RegionWest)
	if !east.RequiresManual || east.SafetyStockThreshold != 80 {
		t.Fatalf("expected custom east threshold to require manual review: %#v", east)
	}
	if west.RequiresManual || west.SafetyStockThreshold != 20 {
		t.Fatalf("expected custom west threshold to allow automatic selection: %#v", west)
	}
}

func TestBuildSKUDecisionUsesTotalThreshold(t *testing.T) {
	thresholds := model.InventoryThresholds{EastThreshold: 10, WestThreshold: 10, TotalThreshold: 130}
	decision := BuildSKUDecision("SKU-1", completeInventory(30, 30, 35, 25), thresholds)
	if !decision.RequiresManual || decision.DecisionCode != "MANUAL_LOW_TOTAL_STOCK" {
		t.Fatalf("expected total threshold to require manual review: %#v", decision)
	}
	if decision.TotalAvailableAmount != 120 {
		t.Fatalf("unexpected total available amount: %v", decision.TotalAvailableAmount)
	}
}

func defaultThresholds() model.InventoryThresholds {
	return model.InventoryThresholds{EastThreshold: 50, WestThreshold: 50, TotalThreshold: 0}
}

func completeInventory(eastDPS, eastARP, westDPS, westARP float64) map[string]WarehouseInventory {
	return map[string]WarehouseInventory{
		"DPSNY002": {Name: "DPS East", Active: true, QueryStatus: QuerySucceeded, SKUFound: true, AvailableAmount: eastDPS},
		"HYTX30":   {Name: "ARP East", Active: true, QueryStatus: QuerySucceeded, SKUFound: true, AvailableAmount: eastARP},
		"DPSCA004": {Name: "DPS West", Active: true, QueryStatus: QuerySucceeded, SKUFound: true, AvailableAmount: westDPS},
		"ARPCA01":  {Name: "ARP West", Active: true, QueryStatus: QuerySucceeded, SKUFound: true, AvailableAmount: westARP},
	}
}

func assertRegion(t *testing.T, decision SKUDecision, region, warehouseKey, decisionCode string, manual bool) {
	t.Helper()
	current := regionByName(t, decision, region)
	if current.RequiresManual != manual || current.RecommendedWarehouseKey != warehouseKey || current.DecisionCode != decisionCode {
		t.Fatalf("unexpected %s decision: %#v", region, current)
	}
}

func regionByName(t *testing.T, decision SKUDecision, region string) RegionDecision {
	t.Helper()
	for _, current := range decision.RegionDecisions {
		if current.Region == region {
			return current
		}
	}
	t.Fatalf("missing region %s", region)
	return RegionDecision{}
}
