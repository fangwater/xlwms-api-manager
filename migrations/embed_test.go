package migrations

import (
	"strings"
	"testing"
)

func TestInitSQLDefinesSKUCombinationTablesAndRelationships(t *testing.T) {
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS xlwms_sku_combinations",
		"substitute_for_sku text UNIQUE REFERENCES xlwms_warehouse_sku_specs",
		"CREATE TABLE IF NOT EXISTS xlwms_sku_combination_items",
		"combination_id bigint NOT NULL REFERENCES xlwms_sku_combinations(id) ON DELETE CASCADE",
		"warehouse_sku text NOT NULL REFERENCES xlwms_warehouse_sku_specs(warehouse_sku) ON DELETE RESTRICT",
	} {
		if !strings.Contains(InitSQL, fragment) {
			t.Fatalf("InitSQL missing %q", fragment)
		}
	}
}

func TestInitSQLMigratesInventoryThresholdsToPlatformScopeBeforeDroppingLegacyTables(t *testing.T) {
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS xlwms_platform_inventory_thresholds",
		"CREATE TABLE IF NOT EXISTS xlwms_platform_sku_inventory_thresholds",
		"inventory threshold migration found % platform default conflicts",
		"inventory threshold migration found % platform SKU conflicts",
		"inventory threshold migration failed to verify % platform default rows",
		"inventory threshold migration failed to verify % shop default rows",
		"inventory threshold migration failed to verify % global SKU rows",
		"inventory threshold migration failed to verify % platform SKU rows",
	} {
		if !strings.Contains(InitSQL, fragment) {
			t.Fatalf("InitSQL missing inventory threshold migration fragment %q", fragment)
		}
	}
	doEnd := strings.Index(InitSQL, "$inventory_threshold_migration$;")
	if doEnd < 0 {
		t.Fatal("InitSQL missing inventory threshold migration block")
	}
	for _, table := range []string{
		"xlwms_shop_sku_inventory_thresholds",
		"xlwms_shop_inventory_thresholds",
		"xlwms_sku_inventory_thresholds",
		"xlwms_inventory_threshold_defaults",
	} {
		drop := "DROP TABLE IF EXISTS " + table
		if index := strings.Index(InitSQL, drop); index < doEnd {
			t.Fatalf("legacy table %s is dropped before migration validation", table)
		}
	}
}

func TestInitSQLDefinesPlatformSKUWarehousePoliciesWithoutShopScope(t *testing.T) {
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS xlwms_platform_carrier_policies",
		"PRIMARY KEY (platform, warehouse_key, carrier_code)",
		"CREATE TABLE IF NOT EXISTS xlwms_platform_sku_carrier_policies",
		"PRIMARY KEY (platform, warehouse_sku, warehouse_key, carrier_code)",
		"CREATE TABLE IF NOT EXISTS xlwms_platform_sku_disabled_warehouses",
		"PRIMARY KEY (platform, warehouse_sku, warehouse_key)",
	} {
		if !strings.Contains(InitSQL, fragment) {
			t.Fatalf("InitSQL missing fulfillment policy fragment %q", fragment)
		}
	}
}
