package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xlwms-api-manager/internal/model"
)

func TestNormalizeTemuSKUsAcceptsSingleAndListAndDeduplicates(t *testing.T) {
	got, err := normalizeTemuSKUs(" SKU-1 ", []string{"SKU-2", "SKU-1", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "SKU-1" || got[1] != "SKU-2" {
		t.Fatalf("unexpected SKUs: %#v", got)
	}
}

func TestNormalizeTemuSKUsRejectsEmptyAndCommaDelimitedValues(t *testing.T) {
	if _, err := normalizeTemuSKUs("", nil); err == nil {
		t.Fatal("empty request must fail")
	}
	if _, err := normalizeTemuSKUs("SKU-1,SKU-2", nil); err == nil {
		t.Fatal("comma-delimited SKU must fail")
	}
}

func TestNormalizeTemuSKUsLimitsBatchSize(t *testing.T) {
	values := make([]string, maxTemuQuerySKUs+1)
	for index := range values {
		values[index] = fmt.Sprintf("SKU-%d", index)
	}
	if _, err := normalizeTemuSKUs("", values); err == nil {
		t.Fatal("oversized SKU query must fail")
	}
}

func TestTemuWarehouseAvailabilityRouteValidatesRequest(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/temu/warehouse-availability/query", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestNormalizeTemuRequestPreservesExactWarehouseSKUAndMergesQuantity(t *testing.T) {
	skus, items, err := normalizeTemuRequest(temuWarehouseQueryRequest{Items: []model.WarehouseSKUQuantity{
		{WarehouseSKU: "PH+H-12Pcs-Black-42cm", Quantity: 1},
		{WarehouseSKU: "PH+H-12Pcs-Black-42cm", Quantity: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(skus) != 1 || skus[0] != "PH+H-12Pcs-Black-42cm" {
		t.Fatalf("warehouse SKU must be preserved exactly: %#v", skus)
	}
	if len(items) != 1 || items[0].WarehouseSKU != skus[0] || items[0].Quantity != 3 {
		t.Fatalf("unexpected normalized items: %#v", items)
	}
}
