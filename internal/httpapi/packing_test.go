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

func TestCreateSKUCombinationValidatesBeforeStoreAccess(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/packing/combinations", strings.NewReader(`{
		"name": "invalid", "length_cm": 20, "width_cm": 10, "height_cm": 5, "weight_kg": 1,
		"items": []
	}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "items must contain") {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
}

func TestSKUCombinationRoutesRejectInvalidIDsBeforeStoreAccess(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		request := httptest.NewRequest(method, "/v1/packing/combinations/not-a-number", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want %d; body=%s", method, recorder.Code, http.StatusBadRequest, recorder.Body.String())
		}
	}
}
