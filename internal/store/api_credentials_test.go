package store

import "testing"

func TestWarehouseAPICredentialKeyGroupsSharedCredentials(t *testing.T) {
	first := warehouseAPICredentialKey("https://api.xlwms.com/openapi/", " shared-key ")
	second := warehouseAPICredentialKey("https://api.xlwms.com/openapi", "shared-key")
	if first != second {
		t.Fatalf("equivalent credentials have different keys: %q and %q", first, second)
	}
	if first == warehouseAPICredentialKey("https://api.xlwms.com/openapi", "other-key") {
		t.Fatal("different App Keys were grouped together")
	}
}

func TestNormalizeWarehouseAPIInventoryDeduplicatesByWarehouseAndSKU(t *testing.T) {
	items := normalizeWarehouseAPIInventory([]WarehouseAPIInventoryItem{
		{WarehouseCode: " arpca01 ", WarehouseName: "Old", WarehouseSKU: "SKU-1"},
		{WarehouseCode: "ARPCA01", WarehouseName: "ARP West", WarehouseSKU: "SKU-1", ProductName: "Hanger"},
		{WarehouseCode: "HYTX30", WarehouseSKU: "SKU-2"},
		{WarehouseCode: "", WarehouseSKU: "ignored"},
	})
	if len(items) != 2 {
		t.Fatalf("normalized inventory = %#v", items)
	}
	if items[0].WarehouseCode != "ARPCA01" || items[0].WarehouseName != "ARP West" || items[0].ProductName != "Hanger" {
		t.Fatalf("unexpected first item %#v", items[0])
	}
}
