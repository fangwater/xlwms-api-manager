package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"io"
	"strings"
	"testing"

	"xlwms-api-manager/internal/model"
)

func TestManualFulfillmentCSVIncludesBOMAndEscapesSpreadsheetFormula(t *testing.T) {
	status := 4
	contents, err := manualFulfillmentCSV([]model.FulfillmentAudit{{
		ShopName: "PANDA HOMES", PlatformOrderNo: "=unsafe", WarehouseCode: "HYTX30",
		OMSStatus: "exception", OMSStatusCode: &status,
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
	if len(rows) != 2 || rows[1][2] != "'=unsafe" || rows[1][5] != "已取消" || rows[1][7] != "领星出库单已取消" {
		t.Fatalf("unexpected CSV rows: %#v", rows)
	}
}

func TestManualFulfillmentWarehouseZIPSplitsFilesByWarehouse(t *testing.T) {
	items := []model.FulfillmentAudit{
		{WarehouseCode: "HYTX30", PlatformOrderNo: "PO-1"},
		{WarehouseCode: "dpSny002", PlatformOrderNo: "PO-2"},
		{WarehouseCode: "HYTX30", PlatformOrderNo: "PO-3"},
		{PlatformOrderNo: "PO-4"},
	}
	contents, err := manualFulfillmentWarehouseZIP(items)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		t.Fatal(err)
	}
	expectedRows := map[string]int{
		"01-manual-fulfillment-orders-DPSNY002.csv":            2,
		"02-manual-fulfillment-orders-HYTX30.csv":              3,
		"03-manual-fulfillment-orders-unmatched-warehouse.csv": 2,
	}
	if len(reader.File) != len(expectedRows) {
		t.Fatalf("unexpected ZIP files: %d", len(reader.File))
	}
	for _, file := range reader.File {
		expected, exists := expectedRows[file.Name]
		if !exists {
			t.Fatalf("unexpected ZIP filename %q", file.Name)
		}
		if file.Mode().Perm() != 0o600 {
			t.Fatalf("unexpected ZIP mode for %q: %o", file.Name, file.Mode().Perm())
		}
		input, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(input)
		_ = input.Close()
		if err != nil {
			t.Fatal(err)
		}
		rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\xEF\xBB\xBF"))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != expected {
			t.Fatalf("unexpected row count for %q: %d", file.Name, len(rows))
		}
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
