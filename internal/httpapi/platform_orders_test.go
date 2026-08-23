package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/oms"
	"xlwms-api-manager/internal/sheinfulfillment"
	"xlwms-api-manager/internal/temutracking"
)

type fakePlatformOrders struct {
	page             int
	pageSize         int
	pendingErr       error
	warehouses       []oms.WarehouseOption
	channels         []oms.LogisticsChannelOption
	resolved         []oms.PendingOrder
	lookupOrderNos   []string
	allStatusRecords []oms.PendingOrder
	allStatusLookup  string
	assignment       oms.AssignmentRequest
	assignmentResult oms.AssignmentResult
	assignCalls      int
}

func (f *fakePlatformOrders) PendingOrders(_ context.Context, page, pageSize int) (oms.PendingOrderPage, error) {
	f.page, f.pageSize = page, pageSize
	if f.pendingErr != nil {
		return oms.PendingOrderPage{}, f.pendingErr
	}
	return oms.PendingOrderPage{
		Records: []oms.PendingOrder{{OrderNo: "OMS-DEMO", PlatformOrderNo: "PO-DEMO", Status: 0}},
		Total:   2222, Page: page, PageSize: pageSize, Pages: 23,
	}, nil
}

func (f *fakePlatformOrders) WarehouseOptions(context.Context) ([]oms.WarehouseOption, error) {
	return f.warehouses, nil
}

func (f *fakePlatformOrders) LogisticsChannels(context.Context, string) ([]oms.LogisticsChannelOption, error) {
	return f.channels, nil
}

func (f *fakePlatformOrders) PendingOrdersByPlatformOrderNos(_ context.Context, orderNos []string) ([]oms.PendingOrder, error) {
	f.lookupOrderNos = append([]string(nil), orderNos...)
	return f.resolved, nil
}

func (f *fakePlatformOrders) PlatformOrdersByPlatformOrderNo(_ context.Context, orderNo string) ([]oms.PendingOrder, error) {
	f.allStatusLookup = orderNo
	return f.allStatusRecords, nil
}

func (f *fakePlatformOrders) AssignAndApprove(_ context.Context, request oms.AssignmentRequest) (oms.AssignmentResult, error) {
	f.assignCalls++
	f.assignment = request
	return f.assignmentResult, nil
}

func TestPendingPlatformOrdersReturnsLoginFailure(t *testing.T) {
	source := &fakePlatformOrders{pendingErr: errors.New("OMS login failed (HTTP 200, code 4011): 请更新登录密码")}
	handler := NewWithPlatformOrders(nil, nil, nil, source, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/pending?page=1&page_size=30", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"error":"请更新登录密码"`) {
		t.Fatalf("expected public login error, got %s", recorder.Body.String())
	}
}

func TestPendingPlatformOrders(t *testing.T) {
	source := &fakePlatformOrders{}
	handler := NewWithPlatformOrders(nil, nil, nil, source, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/pending?page=2&page_size=500", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if source.page != 2 || source.pageSize != 100 {
		t.Fatalf("query = (%d, %d)", source.page, source.pageSize)
	}
	var payload struct {
		Success bool                 `json:"success"`
		Data    oms.PendingOrderPage `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data.Total != 2222 || payload.Data.QueriedAt.IsZero() {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestPendingPlatformOrdersSearchesExactPlatformOrderNumber(t *testing.T) {
	source := &fakePlatformOrders{resolved: []oms.PendingOrder{{OrderNo: "OMS-A", PlatformOrderNo: "PO-A", Status: 0}}}
	handler := NewWithPlatformOrders(nil, nil, nil, source, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/pending?q=PO-A&page=9&page_size=30", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(source.lookupOrderNos) != 1 || source.lookupOrderNos[0] != "PO-A" || source.page != 0 {
		t.Fatalf("unexpected lookup: %#v, paginated page: %d", source.lookupOrderNos, source.page)
	}
	var payload struct {
		Success bool                 `json:"success"`
		Data    oms.PendingOrderPage `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data.Total != 1 || payload.Data.Page != 1 || payload.Data.Pages != 1 || len(payload.Data.Records) != 1 {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestPendingPlatformOrdersSearchReturnsEmptyArrayWhenNotFound(t *testing.T) {
	source := &fakePlatformOrders{}
	handler := NewWithPlatformOrders(nil, nil, nil, source, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/pending?q=PO-MISSING", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"records":[]`) || !strings.Contains(recorder.Body.String(), `"total":0`) {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
	if len(source.lookupOrderNos) != 1 || source.lookupOrderNos[0] != "PO-MISSING" {
		t.Fatalf("unexpected lookup: %#v", source.lookupOrderNos)
	}
}

func TestPlatformOrderLookupReturnsAllStatusMatches(t *testing.T) {
	source := &fakePlatformOrders{allStatusRecords: []oms.PendingOrder{{
		OrderNo: "OMS-PROCESSING", PlatformOrderNo: "PO-A", Status: 2,
		OrderTime: "2026-08-01 01:02:03",
	}}}
	handler := NewWithPlatformOrders(nil, nil, nil, source, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/PO-A?account=arp", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if source.allStatusLookup != "PO-A" || len(source.lookupOrderNos) != 0 || source.page != 0 {
		t.Fatalf("unexpected lookup state: all=%q pending=%#v page=%d", source.allStatusLookup, source.lookupOrderNos, source.page)
	}
	var payload struct {
		Success bool                      `json:"success"`
		Data    platformOrderLookupResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || !payload.Data.Found || payload.Data.Account != "arp" ||
		len(payload.Data.Records) != 1 || payload.Data.Records[0].Status != 2 ||
		payload.Data.Records[0].OrderTime != "2026-08-01 01:02:03" || payload.Data.QueriedAt.IsZero() {
		t.Fatalf("unexpected response: %#v", payload)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control = %q", recorder.Header().Get("Cache-Control"))
	}
}

func TestPlatformOrderLookupReturnsFoundFalse(t *testing.T) {
	source := &fakePlatformOrders{}
	handler := NewWithPlatformOrders(nil, nil, nil, source, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/PO-MISSING", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK ||
		!strings.Contains(recorder.Body.String(), `"found":false`) ||
		!strings.Contains(recorder.Body.String(), `"records":[]`) {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlatformOrderLookupRejectsConflictingAccountSelectors(t *testing.T) {
	source := &fakePlatformOrders{}
	handler := NewWithPlatformOrders(nil, nil, nil, source, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/PO-A?account=arp", nil)
	request.Header.Set(platformOrderAccountHeader, "warehouse:DPSCA004")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || source.allStatusLookup != "" {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlatformOrderRoutingPreviewUsesSheinPurchasedLabelWarehouse(t *testing.T) {
	source := readyPlatformOrderOperator()
	source.resolved = source.resolved[:1]
	fulfillment := &fakePlatformFulfillment{audits: []model.FulfillmentAudit{{
		Platform: "shein", PlatformOrderNo: "PO-A", Active: true,
		WarehouseKey: "HYTX30", WarehouseCode: "WH-1",
	}}}
	handler := platformOrderOperationHandler(source, readyPlatformMappings(), fulfillment)
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/routing-preview", strings.NewReader(`{"platform_order_nos":["PO-A"]}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Success bool                    `json:"success"`
		Data    automaticRoutingPreview `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || !payload.Data.Ready || len(payload.Data.Routes) != 1 || payload.Data.Routes[0].WarehouseCode != "WH-1" {
		t.Fatalf("SHEIN purchased-label preview = %#v", payload)
	}
}

func TestPlatformOrderRoutingPreviewKeepsSheinEvidenceWhenTemuHasNoShipment(t *testing.T) {
	source := readyPlatformOrderOperator()
	source.resolved = source.resolved[:1]
	services := &fakePlatformServices{fakePlatformMappings: readyPlatformMappings(), shipments: map[string]temutracking.PurchasedShipment{}}
	fulfillment := &fakePlatformFulfillment{audits: []model.FulfillmentAudit{{
		Platform: "shein", PlatformOrderNo: "PO-A", Active: true,
		WarehouseKey: "HYTX30", WarehouseCode: "WH-1",
	}}}
	handler := newWithPlatformOrderOperations(nil, nil, nil, source, services, fulfillment, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/routing-preview", strings.NewReader(`{"platform_order_nos":["PO-A"]}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"ready":true`) {
		t.Fatalf("SHEIN audit evidence was dropped when Temu had no shipment: %s", recorder.Body.String())
	}
}

func TestPlatformOrderRoutingPreviewUsesAuthoritativeSheinPurchasedLabel(t *testing.T) {
	source := readyPlatformOrderOperator()
	source.resolved = source.resolved[:1]
	services := &fakeSheinPlatformServices{
		fakePlatformServices: &fakePlatformServices{fakePlatformMappings: readyPlatformMappings(), shipments: map[string]temutracking.PurchasedShipment{}},
		labels: map[string]sheinfulfillment.PurchasedLabel{"PO-A": {
			ShopCode: "beauty-hangers-home", PlatformOrderNo: "PO-A",
			OMSWarehouseKey: "EAST", OMSWarehouseCode: "WH-1", TrackingNumber: "GU-1",
		}},
	}
	handler := newWithPlatformOrderOperations(nil, nil, nil, source, services, &fakePlatformFulfillment{}, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/routing-preview", strings.NewReader(`{"platform_order_nos":["PO-A"]}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"ready":true`) || !strings.Contains(recorder.Body.String(), `"warehouse_code":"WH-1"`) {
		t.Fatalf("authoritative SHEIN preview failed: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlatformOrderRoutingPreviewUsesPurchasedLabelWarehouse(t *testing.T) {
	source := readyPlatformOrderOperator()
	handler := platformOrderOperationHandler(source, readyPlatformMappings(), readyPlatformFulfillment("PO-A", "PO-B"))
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/routing-preview", strings.NewReader(`{"platform_order_nos":["PO-A","PO-B"]}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Success bool                    `json:"success"`
		Data    automaticRoutingPreview `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || !payload.Data.Ready || len(payload.Data.Routes) != 2 || payload.Data.Routes[0].WarehouseCode != "WH-1" ||
		payload.Data.ChannelCode != oms.PlatformLabelChannelCode || len(payload.Data.Carriers) != 2 {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestPlatformOrderRoutingPreviewUsesAuthoritativeTemuShipment(t *testing.T) {
	source := readyPlatformOrderOperator()
	source.resolved = source.resolved[:1]
	source.warehouses = append(source.warehouses, oms.WarehouseOption{WarehouseCode: "WH-2", WarehouseName: "Temu purchase warehouse"})
	services := &fakePlatformServices{
		fakePlatformMappings: &fakePlatformMappings{mappings: []temutracking.WarehouseMapping{
			{OMSKey: "EAST", OMSWarehouseCode: "WH-1", TemuWarehouseID: "PLATFORM-1"},
			{OMSKey: "DPS", OMSWarehouseCode: "WH-2", TemuWarehouseID: "PLATFORM-DPS"},
		}},
		shipments: map[string]temutracking.PurchasedShipment{"PO-A": {
			StoreCode: "panda-homes", ParentOrderSN: "PO-A", Status: "shipped",
			OMSWarehouseKey: "DPS", OMSWarehouseCode: "WH-2", TrackingNumber: "TRACK-A",
		}},
	}
	handler := newWithPlatformOrderOperations(nil, nil, nil, source, services, readyPlatformFulfillment("PO-A"), time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/routing-preview", strings.NewReader(`{"platform_order_nos":["PO-A"]}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Success bool                    `json:"success"`
		Data    automaticRoutingPreview `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || !payload.Data.Ready || len(payload.Data.Routes) != 1 || payload.Data.Routes[0].WarehouseCode != "WH-2" {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestPlatformOrderRoutingPreviewDoesNotUseStaleCacheWhenTemuHasNoShipment(t *testing.T) {
	source := readyPlatformOrderOperator()
	source.resolved = source.resolved[:1]
	services := &fakePlatformServices{fakePlatformMappings: readyPlatformMappings(), shipments: map[string]temutracking.PurchasedShipment{}}
	handler := newWithPlatformOrderOperations(nil, nil, nil, source, services, readyPlatformFulfillment("PO-A"), time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/routing-preview", strings.NewReader(`{"platform_order_nos":["PO-A"]}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "未找到可靠购面单记录") {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlatformOrderRoutingPreviewUsesArchivedOutboundPurchasedLabelWarehouse(t *testing.T) {
	source := readyPlatformOrderOperator()
	source.resolved = source.resolved[:1]
	fulfillment := &fakePlatformFulfillment{audits: []model.FulfillmentAudit{{
		Platform: "temu", PlatformOrderNo: "PO-A", Active: false, OMSStatus: "outbound",
		WarehouseKey: "EAST", WarehouseCode: "WH-1",
	}}}
	handler := platformOrderOperationHandler(source, readyPlatformMappings(), fulfillment)
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/routing-preview", strings.NewReader(`{"platform_order_nos":["PO-A"]}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Success bool                    `json:"success"`
		Data    automaticRoutingPreview `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || !payload.Data.Ready || len(payload.Data.Routes) != 1 || payload.Data.Routes[0].WarehouseCode != "WH-1" {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestPlatformOrderRoutingPreviewRejectsInactiveUnconfirmedWarehouseEvidence(t *testing.T) {
	source := readyPlatformOrderOperator()
	source.resolved = source.resolved[:1]
	fulfillment := &fakePlatformFulfillment{audits: []model.FulfillmentAudit{{
		Platform: "temu", PlatformOrderNo: "PO-A", Active: false, OMSStatus: "not_found",
		WarehouseKey: "EAST", WarehouseCode: "WH-1",
	}}}
	handler := platformOrderOperationHandler(source, readyPlatformMappings(), fulfillment)
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/routing-preview", strings.NewReader(`{"platform_order_nos":["PO-A"]}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "未找到可靠购面单记录") {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlatformOrderRoutingPreviewPrefersPurchasedLabelOverOMSPlatformWarehouse(t *testing.T) {
	source := readyPlatformOrderOperator()
	source.resolved = source.resolved[:1]
	source.warehouses = append(source.warehouses, oms.WarehouseOption{WarehouseCode: "WH-2", WarehouseName: "Purchased-label warehouse"})
	mappings := &fakePlatformMappings{mappings: []temutracking.WarehouseMapping{
		{OMSKey: "EAST", OMSWarehouseCode: "WH-1", TemuWarehouseID: "PLATFORM-1", TemuName: "OMS platform warehouse"},
		{OMSKey: "DPS", OMSWarehouseCode: "WH-2", TemuWarehouseID: "PLATFORM-DPS", TemuName: "Purchased-label platform warehouse"},
	}}
	fulfillment := &fakePlatformFulfillment{
		audits: []model.FulfillmentAudit{{
			Platform: "temu", PlatformOrderNo: "PO-A", Active: true,
			WarehouseKey: "DPS", WarehouseCode: "WH-2",
		}},
	}
	handler := platformOrderOperationHandler(source, mappings, fulfillment)
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/routing-preview", strings.NewReader(`{"platform_order_nos":["PO-A"]}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Success bool                    `json:"success"`
		Data    automaticRoutingPreview `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || !payload.Data.Ready || len(payload.Data.Routes) != 1 ||
		payload.Data.Routes[0].WarehouseCode != "WH-2" || payload.Data.Routes[0].PlatformWarehouseID != "PLATFORM-DPS" {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestAssignAndApproveRejectsClientSelectedWarehouse(t *testing.T) {
	source := readyPlatformOrderOperator()
	handler := platformOrderOperationHandler(source, readyPlatformMappings(), readyPlatformFulfillment("PO-A"))
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/assign-and-approve", strings.NewReader(`{"platform_order_nos":["PO-A"],"warehouse_code":"WH-2","logistics_carrier":"other","confirmation":"CONFIRM_AND_APPROVE"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if source.assignCalls != 0 {
		t.Fatalf("assignment called %d times", source.assignCalls)
	}
}

func TestAssignAndApprovePlatformOrders(t *testing.T) {
	source := readyPlatformOrderOperator()
	handler := platformOrderOperationHandler(source, readyPlatformMappings(), readyPlatformFulfillment("PO-A", "PO-B"))
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/assign-and-approve", strings.NewReader(operationRequestBody))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if source.assignCalls != 1 {
		t.Fatalf("assignment called %d times", source.assignCalls)
	}
	if source.assignment.WarehouseCode != "WH-1" || source.assignment.LogisticsChannelCode != oms.PlatformLabelChannelCode ||
		source.assignment.LogisticsCarrier != oms.OtherCarrierValue || len(source.assignment.Orders) != 2 ||
		source.assignment.Orders[0] != "OMS-A" || source.assignment.Orders[1] != "OMS-B" {
		t.Fatalf("unexpected assignment: %#v", source.assignment)
	}
	var payload struct {
		Success bool                   `json:"success"`
		Data    assignAndApproveResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data.Total != 2 || payload.Data.Success != 2 || payload.Data.Failed != 0 {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestWarehouseAssignmentsAPI(t *testing.T) {
	source := readyPlatformOrderOperator()
	handler := platformOrderOperationHandler(source, readyPlatformMappings(), readyPlatformFulfillment("PO-A", "PO-B"))
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/warehouse-assignments", strings.NewReader(operationRequestBody))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if source.assignCalls != 1 {
		t.Fatalf("assignment called %d times", source.assignCalls)
	}
	var payload struct {
		Success bool                   `json:"success"`
		Data    assignAndApproveResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data.Account != defaultPlatformOrderAccountKey ||
		payload.Data.Total != 2 || payload.Data.Success != 2 || payload.Data.Failed != 0 ||
		len(payload.Data.Routes) != 2 || payload.Data.Routes[0].PlatformOrderNo != "PO-A" ||
		payload.Data.Routes[0].WarehouseCode != "WH-1" {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestWarehouseAssignmentsRejectsClientSelectedWarehouse(t *testing.T) {
	source := readyPlatformOrderOperator()
	handler := platformOrderOperationHandler(source, readyPlatformMappings(), readyPlatformFulfillment("PO-A"))
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/warehouse-assignments", strings.NewReader(
		`{"platform_order_nos":["PO-A"],"warehouse_code":"WH-2","logistics_carrier":"other","confirmation":"CONFIRM_AND_APPROVE"}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || source.assignCalls != 0 {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestAssignAndApproveRejectsOrdersNoLongerPending(t *testing.T) {
	source := readyPlatformOrderOperator()
	source.resolved = nil
	handler := platformOrderOperationHandler(source, readyPlatformMappings(), readyPlatformFulfillment("PO-A", "PO-B"))
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/assign-and-approve", strings.NewReader(operationRequestBody))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if source.assignCalls != 0 {
		t.Fatalf("assignment called %d times", source.assignCalls)
	}
}

func TestAssignAndApproveRejectsMissingPurchasedLabelRecord(t *testing.T) {
	source := readyPlatformOrderOperator()
	handler := platformOrderOperationHandler(source, readyPlatformMappings(), &fakePlatformFulfillment{})
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/assign-and-approve", strings.NewReader(operationRequestBody))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if source.assignCalls != 0 {
		t.Fatalf("assignment called %d times", source.assignCalls)
	}
	if !strings.Contains(recorder.Body.String(), "未找到可靠购面单记录") {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
}

const operationRequestBody = `{"platform_order_nos":["PO-A","PO-B"],"logistics_carrier":"other","confirmation":"CONFIRM_AND_APPROVE"}`

func readyPlatformOrderOperator() *fakePlatformOrders {
	return &fakePlatformOrders{
		warehouses: []oms.WarehouseOption{{WarehouseCode: "WH-1", WarehouseName: "Test warehouse"}},
		channels: []oms.LogisticsChannelOption{{
			LogisticsChannel: oms.PlatformLabelChannelCode, LogisticsChannelName: "Upload label",
			ChannelType: 3, GetSheetType: 1,
		}},
		resolved: []oms.PendingOrder{
			{OrderNo: "OMS-A", PlatformOrderNo: "PO-A", Status: 0, PlatformWarehouseDetails: []oms.PlatformWarehouseDetail{{WarehouseID: "PLATFORM-1", WarehouseName: "Platform east"}}},
			{OrderNo: "OMS-B", PlatformOrderNo: "PO-B", Status: 0, PlatformWarehouseDetails: []oms.PlatformWarehouseDetail{{WarehouseID: "PLATFORM-1", WarehouseName: "Platform east"}}},
		},
		assignmentResult: oms.AssignmentResult{TotalQuantity: 2, SuccessQuantity: 2},
	}
}

type fakePlatformMappings struct {
	mappings []temutracking.WarehouseMapping
}

func (f *fakePlatformMappings) WarehouseMappings(context.Context) ([]temutracking.WarehouseMapping, error) {
	return f.mappings, nil
}

func readyPlatformMappings() *fakePlatformMappings {
	return &fakePlatformMappings{mappings: []temutracking.WarehouseMapping{{
		OMSKey: "EAST", OMSWarehouseCode: "WH-1", TemuWarehouseID: "PLATFORM-1", TemuName: "Platform east",
	}}}
}

type fakePlatformServices struct {
	*fakePlatformMappings
	shipments map[string]temutracking.PurchasedShipment
}

type fakeSheinPlatformServices struct {
	*fakePlatformServices
	labels map[string]sheinfulfillment.PurchasedLabel
}

func (f *fakeSheinPlatformServices) PurchasedSheinLabelsByPlatformOrderNos(context.Context, []string) (map[string]sheinfulfillment.PurchasedLabel, error) {
	return f.labels, nil
}

func (f *fakePlatformServices) PurchasedShipmentsByPlatformOrderNos(context.Context, []string) (map[string]temutracking.PurchasedShipment, error) {
	return f.shipments, nil
}

type fakePlatformFulfillment struct {
	audits []model.FulfillmentAudit
}

func (f *fakePlatformFulfillment) FulfillmentAuditWarehouseEvidenceByPlatformOrderNos(context.Context, []string) ([]model.FulfillmentAudit, error) {
	return append([]model.FulfillmentAudit(nil), f.audits...), nil
}

func readyPlatformFulfillment(orderNos ...string) *fakePlatformFulfillment {
	source := &fakePlatformFulfillment{}
	for _, orderNo := range orderNos {
		source.audits = append(source.audits, model.FulfillmentAudit{
			Platform: "temu", PlatformOrderNo: orderNo, Active: true,
			WarehouseKey: "EAST", WarehouseCode: "WH-1",
		})
	}
	return source
}

func platformOrderOperationHandler(source *fakePlatformOrders, mappings *fakePlatformMappings, fulfillment platformOrderFulfillmentSource) http.Handler {
	return newWithPlatformOrderOperations(nil, nil, nil, source, mappings, fulfillment, time.Second, slog.Default())
}
