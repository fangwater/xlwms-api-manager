package temu

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"xlwms-api-manager/internal/model"
)

func TestOnlyHoustonStockCanSelectWithoutEastOrWestStock(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":200,"data":{"pages":1,"records":[{"whCode":"ARP06A","sku":"hanger","productStockDtl":{"availableAmount":100}}]}}`)
	}))
	defer api.Close()
	result := QueryScopedInventory(context.Background(), map[string][]model.WarehouseCredentials{"hanger": {{WarehouseSummary: model.WarehouseSummary{Code: "ARP06A", APIBaseURL: api.URL, Active: true}, APICredentialKey: "test-api", OMSAccountKey: "arp", AppKey: "test", AppSecret: "test"}}}, time.Second, time.Now())
	decision := BuildSKUDecision("hanger", result.InventoryBySKU["hanger"], model.InventoryThresholds{TotalThreshold: 50})
	if !result.Complete || decision.RequiresManual || decision.TotalAvailableAmount != 100 {
		t.Fatalf("Houston only: complete=%v decision=%+v", result.Complete, decision)
	}
	found := false
	for _, w := range decision.Warehouses {
		if w.WarehouseKey == "ARP_HOUSTON" {
			found = w.Selectable && w.APIBinding != nil && w.APIBinding.OMSAccountKey == "arp"
		}
		if w.WarehouseKey == "ARP_ATLANTA" && w.Selectable {
			t.Fatal("unlisted Atlanta was selected")
		}
	}
	if !found {
		t.Fatal("Houston is absent from flat candidates")
	}
	ApplyInventoryCorrections(&result, map[string]map[string]model.InventoryCorrection{"hanger": {"ARP06A": {CorrectionMode: "absolute", CorrectionAmount: 0}}})
	if !BuildSKUDecision("hanger", result.InventoryBySKU["hanger"], model.InventoryThresholds{TotalThreshold: 50}).RequiresManual {
		t.Fatal("zero correction did not remove Houston")
	}
}

func TestSKURestrictionUpdatesFlatHoustonCandidates(t *testing.T) {
	inventory := completeInventory(0, 100, 0, 0)
	inventory["ARP06A"] = WarehouseInventory{Active: true, QueryStatus: QuerySucceeded, SKUFound: true, AvailableAmount: 100}
	decision := BuildSKUDecision("hanger", inventory, model.InventoryThresholds{})
	ApplyPlatformSKUWarehouseRestrictions(&decision, map[string]bool{"ARP_HOUSTON": true})
	for _, w := range decision.Warehouses {
		if w.WarehouseKey == "ARP_HOUSTON" && (w.Selectable || !w.PlatformSKUDisabled) {
			t.Fatal("flat candidate bypassed SKU restriction")
		}
	}
}
