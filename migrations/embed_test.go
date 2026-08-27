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
