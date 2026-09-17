package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPlatformSKUMappingWritesDoNotRequireConsoleAuth(t *testing.T) {
	handler := newWithPlatformOrderAccountOperationsAuthenticated(nil, nil, nil, nil, nil, nil, nil, "operator", "secret", time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-sku-mappings", strings.NewReader(`{"platform":"shein","platform_sku":"S1","items":[]}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d", recorder.Code)
	}
}

func TestPlatformSKUMappingWriteValidatesBeforeStoreLookup(t *testing.T) {
	handler := newWithPlatformOrderAccountOperationsAuthenticated(nil, nil, nil, nil, nil, nil, nil, "operator", "secret", time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-sku-mappings", strings.NewReader(`{"platform":"shein/shop","platform_sku":"S1","items":[]}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d", recorder.Code)
	}
}
