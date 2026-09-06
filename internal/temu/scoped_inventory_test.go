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

func TestLaundryTwoWarehouseThreshold(t *testing.T) {
	thresholds := model.InventoryThresholds{TotalThreshold: 30, TotalInclusive: true, WarehouseCodes: []string{"HYTX30", "ARPCA01"}}
	for _, total := range []float64{29, 30, 31} {
		inventory := completeInventory(500, 10, 500, total-10)
		result := BuildSKUDecision("laundry", inventory, thresholds)
		if result.TotalAvailableAmount != total || result.RequiresManual != (total <= 30) {
			t.Fatalf("total %v: %+v", total, result)
		}
		for _, region := range result.RegionDecisions {
			for _, w := range region.Warehouses {
				if w.Provider == "DPS" && w.Selectable {
					t.Fatal("DPS outside the two-warehouse scope is selectable")
				}
			}
		}
	}
	inventory := completeInventory(500, 10, 500, 30)
	inventory["ARPCA01"] = WarehouseInventory{QueryStatus: QueryOutOfScope}
	if !BuildSKUDecision("laundry", inventory, thresholds).RequiresManual {
		t.Fatal("missing required warehouse must fail closed")
	}
}

func TestScopedInventoryDoesNotMixSharedWarehouseCredentials(t *testing.T) {
	server := func(amount int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"code":200,"data":{"pages":1,"records":[{"whCode":"HYTX30","sku":"laundry","stockType":0,"productStockDtl":{"availableAmount":%d}}]}}`, amount)
		}))
	}
	laundry := server(31)
	defer laundry.Close()
	hanger := server(999)
	defer hanger.Close()
	result := QueryScopedInventory(context.Background(), map[string][]model.WarehouseCredentials{
		"laundry": {{WarehouseSummary: model.WarehouseSummary{Code: "HYTX30", APIBaseURL: laundry.URL, Active: true}, APICredentialKey: "api-laundry", OMSAccountKey: "laundry", AppKey: "laundry-test", AppSecret: "test"}},
		"hanger":  {{WarehouseSummary: model.WarehouseSummary{Code: "HYTX30", APIBaseURL: hanger.URL, Active: true}, APICredentialKey: "api-hanger", OMSAccountKey: "hanger", AppKey: "hanger-test", AppSecret: "test"}},
	}, time.Second, time.Now())
	if !result.Complete || result.InventoryBySKU["laundry"]["HYTX30"].AvailableAmount != 31 {
		t.Fatalf("incorrect scoped inventory: complete=%v amount=%v", result.Complete, result.InventoryBySKU["laundry"]["HYTX30"].AvailableAmount)
	}
	if result.InventoryBySKU["laundry"]["DPSNY002"].QueryStatus != QueryOutOfScope {
		t.Fatal("unrelated warehouse required")
	}
	for _, sku := range []string{"laundry", "hanger"} {
		decision := BuildSKUDecision(sku, result.InventoryBySKU[sku], model.InventoryThresholds{})
		for _, region := range decision.RegionDecisions {
			for _, warehouse := range region.Warehouses {
				if warehouse.WarehouseCode == "HYTX30" && (warehouse.APIBinding == nil || warehouse.APIBinding.OMSAccountKey != sku || warehouse.APIBinding.CredentialKey != "api-"+sku) {
					t.Fatal("API binding was lost or mixed")
				}
			}
		}
	}
}
