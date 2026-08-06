package store

import (
	"reflect"
	"testing"

	"xlwms-api-manager/internal/model"
)

func TestDateRangeFiltersComposeWithWarehouseFilter(t *testing.T) {
	where, args := appendDateRangeFilters([]string{"wh_code = $1"}, []any{"EAST-01"}, "cost_time", "2026-08-01", "2026-08-05")
	wantWhere := []string{"wh_code = $1", "cost_time >= $2::date", "cost_time < ($3::date + interval '1 day')"}
	wantArgs := []any{"EAST-01", "2026-08-01", "2026-08-05"}
	if !reflect.DeepEqual(where, wantWhere) || !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected composed filters: where=%#v args=%#v", where, args)
	}
}

func TestFundsFlowSourceKeyPreservesDistinctRowsAndOccurrences(t *testing.T) {
	first := map[string]any{"whCode": "PA30", "orderNo": "ORDER-1", "costTotal": 10}
	changed := map[string]any{"whCode": "PA30", "orderNo": "ORDER-1", "costTotal": 12}
	key1, _ := FundsFlowSourceKey("PA30", first, 1)
	key2, _ := FundsFlowSourceKey("PA30", changed, 1)
	key3, _ := FundsFlowSourceKey("PA30", first, 2)
	if key1 == key2 || key1 == key3 {
		t.Fatal("source keys must preserve row changes and identical occurrences")
	}
}

func TestIntegratedInventoryAmountsKeepProductStockSeparate(t *testing.T) {
	record := map[string]any{
		"productStockDtl":   map[string]any{"availableAmount": float64(12), "lockAmount": float64(3), "transportAmount": float64(5)},
		"boxStockDtl":       map[string]any{"availableAmount": float64(7), "lockAmount": float64(2), "transportAmount": float64(4)},
		"fbaReturnStockDtl": map[string]any{"availableAmount": float64(1), "lockAmount": float64(1), "transportAmount": float64(0)},
	}
	available, locked, transport := inventoryAmounts("integrated", record)
	if available != float64(20) || locked != float64(6) || transport != float64(9) {
		t.Fatalf("unexpected combined amounts: %v %v %v", available, locked, transport)
	}
	productAvailable, productLocked, productTransport := inventoryDetailAmounts(record, "productStockDtl")
	if productAvailable != 12 || productLocked != 3 || productTransport != 5 {
		t.Fatalf("unexpected product amounts: %v %v %v", productAvailable, productLocked, productTransport)
	}
}

func TestIntegratedInventoryIsSnapshotData(t *testing.T) {
	if !inventorySnapshotKind("integrated") {
		t.Fatal("integrated inventory must replace stale SKU rows")
	}
	if inventorySnapshotKind("stock_flow") {
		t.Fatal("stock flow must remain append-only")
	}
}

func TestWarehouseSKUSpecMissingFieldsRequiresExactCompleteSpec(t *testing.T) {
	length, width, height, weight := 41.0, 32.5, 7.0, 1.27
	spec := model.WarehouseSKUSpec{
		WarehouseSKU: "PH+H-12Pcs-Black-42cm",
		LengthCM:     &length,
		WidthCM:      &width,
		HeightCM:     &height,
		WeightKG:     &weight,
		Enabled:      true,
	}
	if missing := warehouseSKUSpecMissingFields(spec, true); len(missing) != 0 {
		t.Fatalf("complete exact spec reported missing fields: %#v", missing)
	}
	if missing := warehouseSKUSpecMissingFields(spec, false); len(missing) != 1 || missing[0] != "warehouse_sku" {
		t.Fatalf("unmatched warehouse SKU must fail exact matching: %#v", missing)
	}
}
