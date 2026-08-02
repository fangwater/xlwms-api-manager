package temu

import "testing"

func TestBuildSKUDecisionPrioritizesDPSWhenBothWarehousesHaveStock(t *testing.T) {
	decision := BuildSKUDecision("SKU-1", completeInventory(30, 30, 35, 25))
	if decision.RequiresManual {
		t.Fatalf("expected automatic selection, got %#v", decision)
	}
	assertRegion(t, decision, RegionEast, "DPS002", "DPS_PRIORITY_CLEAR_STOCK", false)
	assertRegion(t, decision, RegionWest, "DPS004", "DPS_PRIORITY_CLEAR_STOCK", false)
}

func TestBuildSKUDecisionUsesInclusiveSafetyThreshold(t *testing.T) {
	decision := BuildSKUDecision("SKU-1", completeInventory(25, 25, 60, 0))
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
	decision := BuildSKUDecision("SKU-1", completeInventory(0, 60, 0, 70))
	assertRegion(t, decision, RegionEast, "ARP_EAST", "ARP_FALLBACK_DPS_OUT_OF_STOCK", false)
	assertRegion(t, decision, RegionWest, "ARP_WEST", "ARP_FALLBACK_DPS_OUT_OF_STOCK", false)
	for _, region := range decision.RegionDecisions {
		if region.Warehouses[0].Selectable {
			t.Fatalf("zero-stock DPS warehouse must not be selectable: %#v", region.Warehouses[0])
		}
	}
}

func TestBuildSKUDecisionRequiresManualReviewWhenQueryIsIncomplete(t *testing.T) {
	inventory := completeInventory(60, 10, 60, 10)
	failed := inventory["DPSNY002"]
	failed.QueryStatus = QueryFailed
	failed.AvailableAmount = 0
	inventory["DPSNY002"] = failed
	decision := BuildSKUDecision("SKU-1", inventory)
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
	decision := BuildSKUDecision("SKU-1", inventory)
	east := regionByName(t, decision, RegionEast)
	if east.Warehouses[0].Selectable || east.Warehouses[0].ReasonCode != "ZERO_AVAILABLE_STOCK" {
		t.Fatalf("missing SKU must be treated as zero stock: %#v", east.Warehouses[0])
	}
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
