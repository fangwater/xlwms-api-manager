package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFulfillmentInventoryReservationRouteValidatesRequest(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/fulfillment/inventory-reservations", strings.NewReader(`{}`))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestFulfillmentInventoryReservationReleaseRouteValidatesRequest(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/fulfillment/inventory-reservations/release", strings.NewReader(`{}`))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestFulfillmentInventoryReservationRouteRejectsForwardedRequest(t *testing.T) {
	handler := New(nil, nil, nil, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/fulfillment/inventory-reservations", strings.NewReader(`{}`))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("X-Forwarded-For", "203.0.113.5")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusForbidden)
	}
}
