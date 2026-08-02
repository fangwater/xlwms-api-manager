package xlwms

import "testing"

func TestCanonicalJSONSortsKeysRecursively(t *testing.T) {
	value := map[string]any{"pageSize": 10, "filters": map[string]any{"z": 1, "A": 2}, "page": 1}
	encoded, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"filters":{"A":2,"z":1},"page":1,"pageSize":10}`
	if string(encoded) != want {
		t.Fatalf("got %s, want %s", encoded, want)
	}
}

func TestBuildAuthCodeMatchesVerifiedPythonImplementation(t *testing.T) {
	got, err := BuildAuthCode("app-key", "app-secret", "1711009072", map[string]any{"page": 1, "pageSize": 10})
	if err != nil {
		t.Fatal(err)
	}
	want := "d836dc928ca9a81cd81ecf95a7bea541a3b764071d3a010a6f2732aa02b4be56"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestInventoryPathsCoverOfficialEndpoints(t *testing.T) {
	want := map[string]string{
		"integrated":      "/v1/integratedInventory/pageOpen",
		"stock_age":       "/v1/integratedInventory/pageStockAge",
		"stock_flow":      "/v1/integratedInventory/pageStockFlow",
		"box_stock":       "/v1/boxStock/page",
		"box_stock_age":   "/v1/boxStock/pageStockAge",
		"box_segment_age": "/v1/boxStock/pageSegmentStockAge",
		"box_stock_flow":  "/v1/boxStock/pageStockFlow",
	}
	for kind, path := range want {
		if InventoryPaths[kind] != path {
			t.Fatalf("%s path is %q, want %q", kind, InventoryPaths[kind], path)
		}
	}
}

func TestInventoryParameterValidation(t *testing.T) {
	if err := validateInventoryParameters("stock_age", map[string]any{}); err == nil {
		t.Fatal("stock age must require stockItemType")
	}
	if err := validateInventoryParameters("stock_age", map[string]any{"stockItemType": 0}); err != nil {
		t.Fatal(err)
	}
	if err := validateInventoryParameters("box_stock_age", map[string]any{"boxType": "A", "sku": "S"}); err == nil {
		t.Fatal("box age filters must be exclusive")
	}
	if err := validateInventoryParameters("box_stock_flow", map[string]any{"startTime": "2026-01-01 00:00:00"}); err == nil {
		t.Fatal("box flow dates must be paired")
	}
	if err := validateInventoryParameters("stock_flow", map[string]any{"startTime": "2026-01-01"}); err == nil {
		t.Fatal("product flow dates must be paired")
	}
	if err := validateInventoryParameters("stock_flow", map[string]any{"startTime": "2026-01-01", "endTime": "2026-01-31"}); err != nil {
		t.Fatal(err)
	}
}
