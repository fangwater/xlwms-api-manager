package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUpdateWarehouseSKUSpecRejectsPrimaryKeyChange(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPatch, "/v1/warehouse-sku-specs/SKU-A", strings.NewReader(`{"warehouse_sku":"SKU-B","enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "warehouse_sku cannot be changed") {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func TestUpdateWarehouseSKUSpecRequiresEnabledState(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPatch, "/v1/warehouse-sku-specs/SKU-A", strings.NewReader(`{"warehouse_sku":"SKU-A"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "enabled is required") {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func TestUpdateWarehouseSKUPackageSpecRequiresAllMeasurements(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPatch, "/v1/warehouse-sku-specs/SKU-A/package", strings.NewReader(`{"length_cm":10}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "weight_kg are required") {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}
