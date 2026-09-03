package store

import "testing"

func TestNormalizeWarehouseCodesAllowsOneWarehouseForMultipleAccounts(t *testing.T) {
	first, err := normalizeWarehouseCodes([]string{" hytx30 ", "DPSNY002", "HYTX30"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := normalizeWarehouseCodes([]string{"HYTX30", "ARPCA01"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[1] != "HYTX30" || len(second) != 2 || second[1] != "HYTX30" {
		t.Fatalf("warehouse scope normalization removed an overlapping warehouse: first=%#v second=%#v", first, second)
	}
}

func TestNormalizePlatformSKUAccountRouteUsesPlatformAndWarehouseSKUOnly(t *testing.T) {
	platform, sku, account, err := normalizePlatformSKUAccountRoute(" SHEIN ", " SKU-1 ", " dps ")
	if err != nil {
		t.Fatal(err)
	}
	if platform != "shein" || sku != "SKU-1" || account != "dps" {
		t.Fatalf("unexpected route: %q %q %q", platform, sku, account)
	}
}
