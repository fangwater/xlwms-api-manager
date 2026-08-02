package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	handler := New(nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/temu/warehouse-availability/query", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
