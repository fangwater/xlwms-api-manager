package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestInventoryAlertStockSQLUsesSellableProductInventoryAndNormalizesFilters(t *testing.T) {
	query, args := inventoryAlertStockSQL(InventoryAlertFilter{
		WarehouseCode: " east-01 ",
		Query:         " demo-sku ",
	})
	for _, fragment := range []string{
		"i.inventory_kind='integrated'",
		"i.stock_type=0",
		"w.is_active",
		"sum(i.product_available_amount)",
		"i.wh_code=$1",
		"i.sku ILIKE $2",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("inventory alert query is missing %q: %s", fragment, query)
		}
	}
	wantArgs := []any{"EAST-01", "%demo-sku%"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected inventory alert filter args: %#v", args)
	}
}
