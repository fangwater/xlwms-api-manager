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

func TestInitSQLSeedsTemuFulfillmentShops(t *testing.T) {
	for _, fragment := range []string{
		"('temu', 'panda-homes', 'PANDA HOMES')",
		"('temu', 'panda-buy', 'PANDA BUY')",
		"('temu', 'hans-living', 'Hans Living')",
		"('temu', 'woven-whispers', 'WovenWhispers')",
	} {
		if !strings.Contains(InitSQL, fragment) {
			t.Fatalf("InitSQL missing fulfillment shop seed %q", fragment)
		}
	}
	for _, fragment := range []string{
		"ON CONFLICT (platform, shop_code) DO NOTHING",
		"CREATE TABLE IF NOT EXISTS xlwms_fulfillment_shop_audits",
		"previous_state jsonb",
		"current_state jsonb NOT NULL",
		"actor text NOT NULL",
	} {
		if !strings.Contains(InitSQL, fragment) {
			t.Fatalf("InitSQL missing fulfillment shop management fragment %q", fragment)
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
		"CREATE TABLE IF NOT EXISTS xlwms_platform_warehouse_carrier_rules",
		"allowed_carrier_codes text[] NOT NULL",
		"allowed_currency_codes text[] NOT NULL",
		"selection_mode text NOT NULL",
		"max_price_delta numeric NOT NULL",
		"warehouse_tie_priority integer NOT NULL",
		"CASE WHEN platform='temu' THEN 0.50 ELSE 0 END",
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

func TestInitSQLMigratesOMSAccountsToOpenAPIBindings(t *testing.T) {
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS xlwms_oms_account_api_credentials",
		"credential_key text PRIMARY KEY REFERENCES xlwms_api_credentials",
		"idx_xlwms_oms_account_api_credentials_account",
		"DROP TABLE IF EXISTS xlwms_platform_sku_oms_accounts",
		"DROP TABLE IF EXISTS xlwms_oms_account_warehouses",
		"DROP COLUMN oms_username_ciphertext",
		"DROP COLUMN oms_password_ciphertext",
		"DROP COLUMN oms_account_hint",
	} {
		if !strings.Contains(InitSQL, fragment) {
			t.Fatalf("InitSQL missing fulfillment account migration fragment %q", fragment)
		}
	}
}

func TestInitSQLDefinesIndependentWarehouseAPICredentialGroups(t *testing.T) {
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS xlwms_api_credentials",
		"credential_key text PRIMARY KEY",
		"CREATE TABLE IF NOT EXISTS xlwms_api_credential_inventory",
		"PRIMARY KEY (credential_key, wh_code, warehouse_sku)",
		"idx_xlwms_api_credential_inventory_warehouse_sku",
	} {
		if !strings.Contains(InitSQL, fragment) {
			t.Fatalf("InitSQL missing warehouse API credential fragment %q", fragment)
		}
	}
}

func TestInitSQLDefinesFulfillmentInventoryReservations(t *testing.T) {
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS xlwms_fulfillment_inventory_reservations",
		"PRIMARY KEY (platform, shop_code, order_key, warehouse_sku)",
		"REFERENCES xlwms_fulfillment_shops(platform, shop_code) ON DELETE RESTRICT",
		"idx_xlwms_fulfillment_inventory_reservations_active",
		"WHERE status='active'",
	} {
		if !strings.Contains(InitSQL, fragment) {
			t.Fatalf("InitSQL missing fulfillment inventory reservation fragment %q", fragment)
		}
	}
}

func TestInitSQLDefinesAccountIndependentPlatformSKUMappings(t *testing.T) {
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS xlwms_platform_sku_mappings",
		"PRIMARY KEY (platform, platform_sku, warehouse_sku)",
		"quantity integer NOT NULL CHECK (quantity > 0)",
		"idx_xlwms_platform_sku_mappings_lookup",
	} {
		if !strings.Contains(InitSQL, fragment) {
			t.Fatalf("InitSQL missing platform SKU mapping fragment %q", fragment)
		}
	}
	if strings.Contains(InitSQL, "PRIMARY KEY (platform, shop_code, platform_sku") ||
		strings.Contains(InitSQL, "PRIMARY KEY (platform, account_key, platform_sku") {
		t.Fatal("canonical platform SKU mappings must not be scoped by shop or OMS account")
	}
}
