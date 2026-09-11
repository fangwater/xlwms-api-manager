package store

import (
	"testing"

	"xlwms-api-manager/internal/model"
)

func TestNormalizePlatformSKUMapping(t *testing.T) {
	got, err := NormalizePlatformSKUMapping(model.PlatformSKUMapping{
		Platform: " SHEIN ", PlatformSKU: " SKU-1 ", Enabled: true,
		Items: []model.PlatformSKUMappingItem{{WarehouseSKU: " B ", Quantity: 2}, {WarehouseSKU: "A", Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Platform != "shein" || got.PlatformSKU != "SKU-1" || got.Source != "manual" {
		t.Fatalf("unexpected normalized mapping: %#v", got)
	}
	if got.Items[0].WarehouseSKU != "A" || got.Items[1].WarehouseSKU != "B" {
		t.Fatalf("items were not normalized deterministically: %#v", got.Items)
	}
}

func TestNormalizePlatformSKUMappingRejectsAccountScopeAndBadRecipe(t *testing.T) {
	for _, input := range []model.PlatformSKUMapping{
		{Platform: "SHEIN/SHOP", PlatformSKU: "S1", Items: []model.PlatformSKUMappingItem{{WarehouseSKU: "W1", Quantity: 1}}},
		{Platform: "shein", PlatformSKU: "S1"},
		{Platform: "shein", PlatformSKU: "S1", Items: []model.PlatformSKUMappingItem{{WarehouseSKU: "W1", Quantity: 0}}},
		{Platform: "shein", PlatformSKU: "S1", Items: []model.PlatformSKUMappingItem{{WarehouseSKU: "W1", Quantity: 1}, {WarehouseSKU: " W1 ", Quantity: 2}}},
	} {
		if _, err := NormalizePlatformSKUMapping(input); err == nil {
			t.Fatalf("expected input to be rejected: %#v", input)
		}
	}
}

func TestNormalizePlatformSKUResolveRequestDeduplicatesWithoutChangingCase(t *testing.T) {
	platform, skus, err := normalizePlatformSKUResolveRequest(" SHEIN ", []string{" Seller-01 ", "Seller-01", "seller-01"})
	if err != nil {
		t.Fatal(err)
	}
	if platform != "shein" || len(skus) != 2 || skus[0] != "Seller-01" || skus[1] != "seller-01" {
		t.Fatalf("unexpected resolution request: %q %#v", platform, skus)
	}
}
