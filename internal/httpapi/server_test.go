package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealth(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("got status %d", response.Code)
	}
	if header := response.Header().Get("Cache-Control"); header != "no-store" {
		t.Fatalf("unexpected cache header %q", header)
	}
}

func TestOutboundRejectsUnknownOperation(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/outbound/not-real", strings.NewReader(`{"warehouse":"WH1","data":{}}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("got status %d", recorder.Code)
	}
}

func TestOutboundValidatesBeforeCredentialLookup(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/outbound/tracking-label-update", strings.NewReader(`{"warehouse":"WH1","data":{"outboundOrderNo":"O1"}}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d", recorder.Code)
	}
}

func TestCostEndpointsRejectInvalidDateRangesBeforeStoreLookup(t *testing.T) {
	tests := []string{
		"/v1/funds-flows?start_date=2026-08-05&end_date=2026-08-01",
		"/v1/cost-details?start_date=08-01-2026",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			handler := New(nil, nil, nil, time.Second, slog.Default())
			request := httptest.NewRequest(http.MethodGet, path, nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("got status %d", recorder.Code)
			}
		})
	}
}
