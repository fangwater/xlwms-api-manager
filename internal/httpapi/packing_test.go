package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreatePackingPlanValidatesBeforeStoreAccess(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/packing/plans", strings.NewReader(`{
		"items": []
	}`))
	request.Header.Set("Content-Type", "application/json")
	responseRecorder := httptest.NewRecorder()

	handler.ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", responseRecorder.Code, http.StatusBadRequest, responseRecorder.Body.String())
	}
	if !strings.Contains(responseRecorder.Body.String(), "items are required") {
		t.Fatalf("unexpected body: %s", responseRecorder.Body.String())
	}
}

func TestCreatePackingPlanRejectsObsoleteCartonParameters(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/packing/plans", strings.NewReader(`{
		"items": [{"warehouse_sku": "SKU", "quantity": 1}],
		"carton": {"length_cm": 40, "width_cm": 30, "height_cm": 25, "max_weight_kg": 20, "count": 2}
	}`))
	request.Header.Set("Content-Type", "application/json")
	responseRecorder := httptest.NewRecorder()

	handler.ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", responseRecorder.Code, http.StatusBadRequest, responseRecorder.Body.String())
	}
	if !strings.Contains(responseRecorder.Body.String(), "invalid JSON request") {
		t.Fatalf("unexpected body: %s", responseRecorder.Body.String())
	}
}
