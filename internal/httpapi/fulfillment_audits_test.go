package httpapi

import (
	"encoding/csv"
	"strings"
	"testing"

	"xlwms-api-manager/internal/model"
)

func TestManualFulfillmentCSVIncludesBOMAndEscapesSpreadsheetFormula(t *testing.T) {
	status := 0
	contents, err := manualFulfillmentCSV([]model.FulfillmentAudit{{
		ShopName: "PANDA HOMES", PlatformOrderNo: "=unsafe", WarehouseCode: "HYTX30",
		OMSStatus: "pending", OMSStatusCode: &status,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(contents), "\xEF\xBB\xBF") {
		t.Fatal("CSV must include a UTF-8 BOM")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(contents), "\xEF\xBB\xBF")))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1][2] != "'=unsafe" || rows[1][5] != "新建" || rows[1][7] != "领星出库单为新建状态" {
		t.Fatalf("unexpected CSV rows: %#v", rows)
	}
}

func TestFulfillmentOMSStatusUsesOriginalCode(t *testing.T) {
	cancelled := 4
	item := model.FulfillmentAudit{OMSStatus: "exception", OMSStatusCode: &cancelled}
	if got := fulfillmentOMSStatusLabel(item.OMSStatus, item.OMSStatusCode); got != "已取消" {
		t.Fatalf("unexpected cancelled label %q", got)
	}
	if got := manualFulfillmentReason(item); got != "领星出库单已取消" {
		t.Fatalf("unexpected cancelled reason %q", got)
	}
}

func TestSpreadsheetSafeLeavesOrderNumbersUnchanged(t *testing.T) {
	if got := spreadsheetSafe(" PO-211-1 "); got != "PO-211-1" {
		t.Fatalf("unexpected safe value %q", got)
	}
}
