package temutracking

import (
	"encoding/json"
	"testing"
)

func TestWarehouseImportDoesNotBorrowOtherShopAddress(t *testing.T) {
	var p warehouseManagementEnvelope
	if err := json.Unmarshal([]byte(`{"success":true,"data":{"warehouses":[{"warehouse_id":"WH-HOME","warehouse_name":"Houston","enable_buy_shipping_label":true,"shop_codes":["panda-homes"]}],"mappings":[{"temu_warehouse_id":"WH-HOME","oms_warehouse_code":"ARP06A","enabled":true}]}}`), &p); err != nil {
		t.Fatal(err)
	}
	own, err := optionsFromEnvelope("panda-homes", p, true)
	if err != nil || len(own) != 1 || !own[0].Enabled {
		t.Fatal("verified legacy mapping lost")
	}
	other, err := optionsFromEnvelope("panda-buy", p, true)
	if err != nil || len(other) != 0 {
		t.Fatal("cross-shop warehouse imported")
	}
}
