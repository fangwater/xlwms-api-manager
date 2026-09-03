package store

import (
	"errors"
	"testing"
)

func TestValidateOMSAccountIdentity(t *testing.T) {
	key, label, err := validateOMSAccountIdentity(" Backup-Account ", " 备用账户 ")
	if err != nil {
		t.Fatal(err)
	}
	if key != "backup-account" || label != "备用账户" {
		t.Fatalf("normalized account = %q %q", key, label)
	}
	for _, invalid := range []string{"", "-backup", "bad account", "账户"} {
		if _, _, err := validateOMSAccountIdentity(invalid, "备用账户"); !errors.Is(err, ErrInvalidFulfillmentAccount) {
			t.Fatalf("key %q error = %v", invalid, err)
		}
	}
	if _, _, err := validateOMSAccountIdentity("backup", " "); !errors.Is(err, ErrInvalidFulfillmentAccount) {
		t.Fatalf("empty label error = %v", err)
	}
}

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
