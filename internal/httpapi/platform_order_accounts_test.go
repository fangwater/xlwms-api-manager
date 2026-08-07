package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/oms"
	"xlwms-api-manager/internal/temutracking"
)

type fakeWarehousePlatformAccounts struct {
	operators map[string]platformOrderOperator
	requested []string
}

func (f *fakeWarehousePlatformAccounts) OperatorForWarehouse(_ context.Context, warehouseCode string) (platformOrderOperator, error) {
	f.requested = append(f.requested, warehouseCode)
	return f.operators[warehouseCode], nil
}

func TestAssignAndApproveUsesPurchasedWarehouseAccountWhileLookupRemainsShared(t *testing.T) {
	shared := readyPlatformOrderOperator()
	shared.resolved = shared.resolved[:1]

	warehouseAccount := readyPlatformOrderOperator()
	warehouseAccount.warehouses = []oms.WarehouseOption{{WarehouseCode: "WH-2", WarehouseName: "DPS warehouse"}}
	warehouseAccount.resolved = nil
	warehouseAccount.assignmentResult = oms.AssignmentResult{TotalQuantity: 1, SuccessQuantity: 1}

	accounts := &fakeWarehousePlatformAccounts{operators: map[string]platformOrderOperator{"WH-2": warehouseAccount}}
	mappings := &fakePlatformMappings{mappings: []temutracking.WarehouseMapping{{
		OMSKey: "DPS", OMSWarehouseCode: "WH-2", TemuWarehouseID: "PLATFORM-DPS", TemuName: "DPS",
	}}}
	fulfillment := &fakePlatformFulfillment{audits: []model.FulfillmentAudit{{
		Platform: "temu", PlatformOrderNo: "PO-A", Active: true, WarehouseKey: "DPS", WarehouseCode: "WH-2",
	}}}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, shared, mappings, fulfillment, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/assign-and-approve", strings.NewReader(
		`{"platform_order_nos":["PO-A"],"logistics_carrier":"other","confirmation":"CONFIRM_AND_APPROVE"}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(shared.lookupOrderNos) != 1 || shared.lookupOrderNos[0] != "PO-A" {
		t.Fatalf("shared lookup did not resolve the order: %#v", shared.lookupOrderNos)
	}
	if shared.assignCalls != 0 || warehouseAccount.assignCalls != 1 {
		t.Fatalf("assignment calls shared=%d warehouse=%d", shared.assignCalls, warehouseAccount.assignCalls)
	}
	if warehouseAccount.assignment.WarehouseCode != "WH-2" {
		t.Fatalf("warehouse account assignment = %#v", warehouseAccount.assignment)
	}
	if len(warehouseAccount.lookupOrderNos) != 0 {
		t.Fatalf("warehouse account unexpectedly performed shared lookup: %#v", warehouseAccount.lookupOrderNos)
	}
	if len(accounts.requested) != 1 || accounts.requested[0] != "WH-2" {
		t.Fatalf("requested warehouse accounts = %#v", accounts.requested)
	}
}
