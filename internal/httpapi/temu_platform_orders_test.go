package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xlwms-api-manager/internal/oms"
)

func TestTemuPlatformOrderUsesExplicitAccountAndReturnsMinimalAllStatusResult(t *testing.T) {
	arp := readyPlatformOrderOperator()
	dps := readyPlatformOrderOperator()
	dps.allStatusRecords = []oms.PendingOrder{{
		OrderNo: "OMS-DPS-1", PlatformOrderNo: "PO-ALL-1", PlatformCode: "temu", Status: 2,
		SubStatus: 7, SendWarehouseCode: "DPSNY002", TrackNo: "TRACK-1",
		OrderTime: "2026-08-10 09:00:00", CreateTime: "2026-08-10 09:05:00",
		AuditTime: "2026-08-11 14:21:30", MarkShipmentStatus: 1,
		StoreName: "must-not-leak", PlatformSKUList: []oms.OrderProduct{{SKU: "must-not-leak"}},
	}}
	accounts := &fakeSelectablePlatformAccounts{accountOperators: map[string]platformOrderAccount{
		defaultPlatformOrderAccountKey: arp, "warehouse:DPSCA004": dps,
	}}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, arp, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/temu/platform-orders/PO-ALL-1", nil)
	request.Header.Set(platformOrderAccountHeader, "warehouse:DPSCA004")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if dps.allStatusLookup != "PO-ALL-1" || arp.allStatusLookup != "" {
		t.Fatalf("wrong account queried: arp=%q dps=%q", arp.allStatusLookup, dps.allStatusLookup)
	}
	var payload struct {
		Success bool                          `json:"success"`
		Data    temuPlatformOrderLookupResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data.Account != "warehouse:DPSCA004" || !payload.Data.Found ||
		payload.Data.MatchCount != 1 || len(payload.Data.Orders) != 1 || payload.Data.QueriedAt.IsZero() {
		t.Fatalf("unexpected response: %#v", payload)
	}
	order := payload.Data.Orders[0]
	if order.Status != 2 || order.StatusKey != temuPlatformOrderStatusProcessing || order.StatusText != "处理中" ||
		order.SendWarehouseCode != "DPSNY002" || order.TrackingNumber != "TRACK-1" || order.AuditTime == "" {
		t.Fatalf("unexpected normalized order: %#v", order)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "must-not-leak") || strings.Contains(body, "platformSkuList") || strings.Contains(body, "storeName") {
		t.Fatalf("response leaked fields outside the service contract: %s", body)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control = %q", recorder.Header().Get("Cache-Control"))
	}
}

func TestTemuPlatformOrderRequiresExplicitAccount(t *testing.T) {
	source := readyPlatformOrderOperator()
	handler := NewWithPlatformOrders(nil, nil, nil, source, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/temu/platform-orders/PO-A", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || source.allStatusLookup != "" ||
		!strings.Contains(recorder.Body.String(), "X-OMS-Account or account is required") {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestTemuPlatformOrderAcceptsAccountQueryAndReturnsEmptyOrders(t *testing.T) {
	source := readyPlatformOrderOperator()
	handler := NewWithPlatformOrders(nil, nil, nil, source, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/temu/platform-orders/PO-MISSING?account=arp", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || source.allStatusLookup != "PO-MISSING" ||
		!strings.Contains(recorder.Body.String(), `"found":false`) ||
		!strings.Contains(recorder.Body.String(), `"match_count":0`) ||
		!strings.Contains(recorder.Body.String(), `"orders":[]`) {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestTemuPlatformOrderRejectsConflictingAccountSelectors(t *testing.T) {
	source := readyPlatformOrderOperator()
	handler := NewWithPlatformOrders(nil, nil, nil, source, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/temu/platform-orders/PO-A?account=arp", nil)
	request.Header.Set(platformOrderAccountHeader, "warehouse:DPSCA004")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || source.allStatusLookup != "" ||
		!strings.Contains(recorder.Body.String(), "conflicting OMS account selectors") {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestTemuPlatformOrderPreservesUnknownRawStatus(t *testing.T) {
	source := readyPlatformOrderOperator()
	source.allStatusRecords = []oms.PendingOrder{{OrderNo: "OMS-X", PlatformOrderNo: "PO-X", Status: 9}}
	handler := NewWithPlatformOrders(nil, nil, nil, source, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/temu/platform-orders/PO-X?account=arp", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":9`) ||
		!strings.Contains(recorder.Body.String(), `"status_key":"unknown"`) ||
		strings.Contains(recorder.Body.String(), `"status_text"`) {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestTemuPlatformOrderStatusUsesOfficialWebClientMapping(t *testing.T) {
	tests := []struct {
		status int
		key    string
		text   string
	}{
		{0, temuPlatformOrderStatusPending, "待处理"},
		{1, temuPlatformOrderStatusAwaitingPlatformLabel, "待获取平台面单"},
		{2, temuPlatformOrderStatusProcessing, "处理中"},
		{3, temuPlatformOrderStatusShipped, "已发货"},
		{4, temuPlatformOrderStatusCanceled, "已取消"},
		{5, temuPlatformOrderStatusException, "异常"},
		{6, temuPlatformOrderStatusAwaitingInvoice, "待开票"},
		{9, temuPlatformOrderStatusUnknown, ""},
	}
	for _, test := range tests {
		key, text := temuPlatformOrderStatus(test.status)
		if key != test.key || text != test.text {
			t.Fatalf("status %d = (%q, %q), want (%q, %q)", test.status, key, text, test.key, test.text)
		}
	}
}
