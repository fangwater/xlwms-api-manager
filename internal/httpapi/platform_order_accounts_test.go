package httpapi

import (
	"context"
	"encoding/json"
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

type fakePlatformOrderAccountStore struct {
	warehouses []model.WarehouseSummary
	accounts   map[string]model.WarehouseOMSAccount
}

func (f *fakePlatformOrderAccountStore) ListWarehousesWithOMS(context.Context, bool) ([]model.WarehouseSummary, error) {
	return f.warehouses, nil
}

func (f *fakePlatformOrderAccountStore) WarehouseOMSAccount(_ context.Context, code string, _ bool) (model.WarehouseOMSAccount, error) {
	account, exists := f.accounts[code]
	if !exists {
		return model.WarehouseOMSAccount{}, errPlatformOrderAccountNotFound
	}
	return account, nil
}

type fakeSelectablePlatformAccounts struct {
	accountOperators   map[string]platformOrderAccount
	warehouseOperators map[string]platformOrderOperator
	options            []platformOrderAccountOption
	selectedAccounts   []string
}

func (f *fakeSelectablePlatformAccounts) PlatformOrderAccounts(context.Context) ([]platformOrderAccountOption, error) {
	return f.options, nil
}

func (f *fakeSelectablePlatformAccounts) OperatorForAccount(_ context.Context, key string) (platformOrderAccount, error) {
	if key == "" {
		key = defaultPlatformOrderAccountKey
	}
	f.selectedAccounts = append(f.selectedAccounts, key)
	operator := f.accountOperators[key]
	if operator == nil {
		return nil, errPlatformOrderAccountNotFound
	}
	return operator, nil
}

func (f *fakeSelectablePlatformAccounts) OperatorForWarehouse(_ context.Context, warehouseCode string) (platformOrderOperator, error) {
	return f.warehouseOperators[warehouseCode], nil
}

type fakeWarehousePlatformAccounts struct {
	operators map[string]platformOrderOperator
	requested []string
}

func (f *fakeWarehousePlatformAccounts) OperatorForWarehouse(_ context.Context, warehouseCode string) (platformOrderOperator, error) {
	f.requested = append(f.requested, warehouseCode)
	return f.operators[warehouseCode], nil
}

func TestPlatformOrderAccountsDeduplicatesCredentials(t *testing.T) {
	shared := readyPlatformOrderOperator()
	accountStore := &fakePlatformOrderAccountStore{
		warehouses: []model.WarehouseSummary{
			{Code: "ARPCA01", OMSAccountConfigured: true},
			{Code: "ARPGA", OMSAccountConfigured: true},
			{Code: "DPSCA004", OMSAccountConfigured: true},
			{Code: "DPSNY002", OMSAccountConfigured: true},
		},
		accounts: map[string]model.WarehouseOMSAccount{
			"ARPCA01":  {WarehouseCode: "ARPCA01", Username: "arp-user", Password: "arp-password"},
			"ARPGA":    {WarehouseCode: "ARPGA", Username: "arp-user", Password: "arp-password"},
			"DPSCA004": {WarehouseCode: "DPSCA004", Username: "dps-user", Password: "dps-password"},
			"DPSNY002": {WarehouseCode: "DPSNY002", Username: "dps-user", Password: "dps-password"},
		},
	}
	resolver := &postgresPlatformOrderAccounts{
		store: accountStore, shared: shared, sharedUsername: "arp-user", sharedPassword: "arp-password",
	}
	options, err := resolver.PlatformOrderAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 2 {
		t.Fatalf("account options = %#v", options)
	}
	if options[0].Key != defaultPlatformOrderAccountKey || options[0].Label != "ARP 账户" ||
		len(options[0].WarehouseCodes) != 2 {
		t.Fatalf("ARP option = %#v", options[0])
	}
	if options[1].Key != "warehouse:DPSCA004" || options[1].Label != "DPS 账户" ||
		len(options[1].WarehouseCodes) != 2 {
		t.Fatalf("DPS option = %#v", options[1])
	}
}

func TestPlatformOrderAccountAcceptsAnyWarehouseCodeInCredentialGroup(t *testing.T) {
	shared := readyPlatformOrderOperator()
	accountStore := &fakePlatformOrderAccountStore{
		warehouses: []model.WarehouseSummary{
			{Code: "ARPCA01", OMSAccountConfigured: true},
			{Code: "HYTX30", OMSAccountConfigured: true},
			{Code: "DPSCA004", OMSAccountConfigured: true},
			{Code: "DPSNY002", OMSAccountConfigured: true},
		},
		accounts: map[string]model.WarehouseOMSAccount{
			"ARPCA01":  {WarehouseCode: "ARPCA01", Username: "arp-user", Password: "arp-password"},
			"HYTX30":   {WarehouseCode: "HYTX30", Username: "arp-user", Password: "arp-password"},
			"DPSCA004": {WarehouseCode: "DPSCA004", Username: "dps-user", Password: "dps-password"},
			"DPSNY002": {WarehouseCode: "DPSNY002", Username: "dps-user", Password: "dps-password"},
		},
	}
	resolver := &postgresPlatformOrderAccounts{
		store: accountStore, baseURL: "https://oms.example.test", timeout: time.Second,
		shared: shared, sharedUsername: "arp-user", sharedPassword: "arp-password",
	}
	arp, err := resolver.OperatorForAccount(context.Background(), "warehouse:HYTX30")
	if err != nil || arp != shared {
		t.Fatalf("ARP warehouse alias = %#v, err=%v", arp, err)
	}
	dpsEast, err := resolver.OperatorForAccount(context.Background(), "warehouse:DPSNY002")
	if err != nil {
		t.Fatal(err)
	}
	dpsWest, err := resolver.OperatorForAccount(context.Background(), "warehouse:DPSCA004")
	if err != nil {
		t.Fatal(err)
	}
	if dpsEast != dpsWest {
		t.Fatal("DPS warehouse aliases must resolve to the same OMS client")
	}
	dpsAccount, err := resolver.OperatorForAccount(context.Background(), dpsPlatformOrderAccountKey)
	if err != nil {
		t.Fatal(err)
	}
	if dpsAccount != dpsEast {
		t.Fatal("DPS account alias must resolve to the DPS OMS client")
	}
}

func TestPlatformOrderAccountReusesWarehouseClient(t *testing.T) {
	accountStore := &fakePlatformOrderAccountStore{
		accounts: map[string]model.WarehouseOMSAccount{
			"DPSCA004": {WarehouseCode: "DPSCA004", Username: "dps-user", Password: "dps-password"},
			"DPSNY002": {WarehouseCode: "DPSNY002", Username: "dps-user", Password: "dps-password"},
		},
	}
	resolver := &postgresPlatformOrderAccounts{store: accountStore, baseURL: "https://oms.example.test", timeout: time.Second}
	first, err := resolver.OperatorForWarehouse(context.Background(), "DPSCA004")
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.OperatorForWarehouse(context.Background(), "DPSNY002")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("expected warehouses sharing credentials to reuse one OMS client")
	}
}

func TestPendingPlatformOrdersUsesSelectedAccount(t *testing.T) {
	arp := readyPlatformOrderOperator()
	dps := readyPlatformOrderOperator()
	accounts := &fakeSelectablePlatformAccounts{
		accountOperators: map[string]platformOrderAccount{
			defaultPlatformOrderAccountKey: arp,
			"warehouse:DPSCA004":           dps,
		},
		options: []platformOrderAccountOption{
			{Key: defaultPlatformOrderAccountKey, Label: "ARP 账户"},
			{Key: "warehouse:DPSCA004", Label: "DPS 账户", WarehouseCodes: []string{"DPSCA004", "DPSNY002"}},
		},
	}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, arp, nil, nil, accounts, time.Second, slog.Default())

	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/pending?account=warehouse%3ADPSCA004&page=3&page_size=40", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if arp.page != 0 || dps.page != 3 || dps.pageSize != 40 {
		t.Fatalf("pending queries ARP=(%d,%d), DPS=(%d,%d)", arp.page, arp.pageSize, dps.page, dps.pageSize)
	}
	if len(accounts.selectedAccounts) != 1 || accounts.selectedAccounts[0] != "warehouse:DPSCA004" {
		t.Fatalf("selected accounts = %#v", accounts.selectedAccounts)
	}
}

func TestPlatformOrderLookupUsesSelectedAccountHeader(t *testing.T) {
	arp := readyPlatformOrderOperator()
	dps := readyPlatformOrderOperator()
	dps.allStatusRecords = []oms.PendingOrder{{
		OrderNo: "DPS-OMS-A", PlatformOrderNo: "PO-A", Status: 2,
		OrderTime: "2026-08-01 01:02:03",
	}}
	accounts := &fakeSelectablePlatformAccounts{
		accountOperators: map[string]platformOrderAccount{
			defaultPlatformOrderAccountKey: arp,
			"warehouse:DPSCA004":           dps,
		},
	}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, arp, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/PO-A", nil)
	request.Header.Set(platformOrderAccountHeader, "warehouse:DPSCA004")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if arp.allStatusLookup != "" || dps.allStatusLookup != "PO-A" {
		t.Fatalf("all-status lookups ARP=%q DPS=%q", arp.allStatusLookup, dps.allStatusLookup)
	}
	if len(accounts.selectedAccounts) != 1 || accounts.selectedAccounts[0] != "warehouse:DPSCA004" {
		t.Fatalf("selected accounts = %#v", accounts.selectedAccounts)
	}
	if !strings.Contains(recorder.Body.String(), `"status":2`) ||
		!strings.Contains(recorder.Body.String(), `"orderTime":"2026-08-01 01:02:03"`) {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
}

func TestPlatformOrderAccountsMarksOfflineLogins(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/gateway/woms/auth/login" {
			http.NotFound(writer, request)
			return
		}
		var payload struct {
			LoginAccount string `json:"loginAccount"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.LoginAccount == "arp-user" {
			_, _ = writer.Write([]byte(`{"code":4011,"msg":"请更新登录密码","data":{}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"code":4213,"msg":"账号已锁定,请联系超管修改或重置密码","data":{}}`))
	}))
	defer server.Close()

	arp := oms.NewClient(server.URL, "arp-user", "arp-password", time.Second)
	dps := oms.NewClient(server.URL, "dps-user", "dps-password", time.Second)
	accounts := &fakeSelectablePlatformAccounts{
		accountOperators: map[string]platformOrderAccount{
			defaultPlatformOrderAccountKey: arp,
			"warehouse:DPSCA004":           dps,
		},
		options: []platformOrderAccountOption{
			{Key: defaultPlatformOrderAccountKey, Label: "ARP 账户"},
			{Key: "warehouse:DPSCA004", Label: "DPS 账户", WarehouseCodes: []string{"DPSCA004", "DPSNY002"}},
		},
	}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, arp, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/accounts", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Success bool                          `json:"success"`
		Data    []platformOrderAccountOption  `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || len(payload.Data) != 2 {
		t.Fatalf("payload = %#v", payload)
	}
	for _, account := range payload.Data {
		if account.Available || account.Status != "offline" || account.Error == "" {
			t.Fatalf("account still looks available: %#v", account)
		}
	}
}

func TestPlatformOrderAccountsListsSelectableAccounts(t *testing.T) {
	arp := readyPlatformOrderOperator()
	accounts := &fakeSelectablePlatformAccounts{
		accountOperators: map[string]platformOrderAccount{defaultPlatformOrderAccountKey: arp},
		options: []platformOrderAccountOption{
			{Key: defaultPlatformOrderAccountKey, Label: "ARP 账户"},
			{Key: "warehouse:DPSCA004", Label: "DPS 账户", WarehouseCodes: []string{"DPSCA004", "DPSNY002"}},
		},
	}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, arp, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/accounts", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"label":"DPS 账户"`) {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestAssignAndApproveRefreshesWithARPAndUsesWarehouseAccountOrderNumber(t *testing.T) {
	shared := readyPlatformOrderOperator()
	shared.resolved = shared.resolved[:1]

	warehouseAccount := readyPlatformOrderOperator()
	warehouseAccount.warehouses = []oms.WarehouseOption{{WarehouseCode: "WH-2", WarehouseName: "DPS warehouse"}}
	warehouseAccount.resolved = []oms.PendingOrder{{OrderNo: "DPS-OMS-A", PlatformOrderNo: "PO-A", Status: 0}}
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
		t.Fatalf("ARP shared lookup did not resolve the order: %#v", shared.lookupOrderNos)
	}
	if shared.assignCalls != 0 || warehouseAccount.assignCalls != 1 {
		t.Fatalf("assignment calls ARP=%d warehouse=%d", shared.assignCalls, warehouseAccount.assignCalls)
	}
	if warehouseAccount.assignment.WarehouseCode != "WH-2" {
		t.Fatalf("warehouse account assignment = %#v", warehouseAccount.assignment)
	}
	if len(warehouseAccount.lookupOrderNos) != 1 || warehouseAccount.lookupOrderNos[0] != "PO-A" {
		t.Fatalf("warehouse account did not resolve its OMS order number: %#v", warehouseAccount.lookupOrderNos)
	}
	if len(warehouseAccount.assignment.Orders) != 1 || warehouseAccount.assignment.Orders[0] != "DPS-OMS-A" {
		t.Fatalf("warehouse account assignment used the wrong OMS order number: %#v", warehouseAccount.assignment.Orders)
	}
	if len(accounts.requested) != 1 || accounts.requested[0] != "WH-2" {
		t.Fatalf("requested warehouse accounts = %#v", accounts.requested)
	}
}

func TestWarehouseAssignmentsUsesHeaderSelectedAccount(t *testing.T) {
	arp := readyPlatformOrderOperator()
	dps := readyPlatformOrderOperator()
	dps.resolved = dps.resolved[:1]
	accounts := &fakeSelectablePlatformAccounts{
		accountOperators: map[string]platformOrderAccount{
			defaultPlatformOrderAccountKey: arp,
			dpsPlatformOrderAccountKey:     dps,
		},
		warehouseOperators: map[string]platformOrderOperator{"WH-1": dps},
	}
	handler := newWithPlatformOrderAccountOperations(
		nil, nil, nil, arp, readyPlatformMappings(), readyPlatformFulfillment("PO-A"), accounts, time.Second, slog.Default(),
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/warehouse-assignments", strings.NewReader(
		`{"platform_order_nos":["PO-A"],"logistics_carrier":"other","confirmation":"CONFIRM_AND_APPROVE"}`,
	))
	request.Header.Set(platformOrderAccountHeader, dpsPlatformOrderAccountKey)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if arp.assignCalls != 0 || dps.assignCalls != 1 {
		t.Fatalf("assignment calls ARP=%d DPS=%d", arp.assignCalls, dps.assignCalls)
	}
	if len(accounts.selectedAccounts) != 1 || accounts.selectedAccounts[0] != dpsPlatformOrderAccountKey {
		t.Fatalf("selected accounts = %#v", accounts.selectedAccounts)
	}
	if !strings.Contains(recorder.Body.String(), `"account":"dps"`) {
		t.Fatalf("response does not identify selected account: %s", recorder.Body.String())
	}
}

func TestWarehouseAssignmentsRejectsConflictingAccountSelectors(t *testing.T) {
	arp := readyPlatformOrderOperator()
	dps := readyPlatformOrderOperator()
	accounts := &fakeSelectablePlatformAccounts{
		accountOperators: map[string]platformOrderAccount{
			defaultPlatformOrderAccountKey: arp,
			dpsPlatformOrderAccountKey:     dps,
		},
		warehouseOperators: map[string]platformOrderOperator{"WH-1": dps},
	}
	handler := newWithPlatformOrderAccountOperations(
		nil, nil, nil, arp, readyPlatformMappings(), readyPlatformFulfillment("PO-A"), accounts, time.Second, slog.Default(),
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/warehouse-assignments", strings.NewReader(
		`{"platform_order_nos":["PO-A"],"account":"arp","logistics_carrier":"other","confirmation":"CONFIRM_AND_APPROVE"}`,
	))
	request.Header.Set(platformOrderAccountHeader, dpsPlatformOrderAccountKey)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || arp.assignCalls != 0 || dps.assignCalls != 0 {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(accounts.selectedAccounts) != 0 {
		t.Fatalf("account lookup occurred before conflict rejection: %#v", accounts.selectedAccounts)
	}
}

func TestRoutingPreviewChecksPendingStateWithSelectedAccount(t *testing.T) {
	arp := readyPlatformOrderOperator()
	arp.resolved = nil
	dps := readyPlatformOrderOperator()
	dps.resolved = dps.resolved[:1]
	accounts := &fakeSelectablePlatformAccounts{
		accountOperators: map[string]platformOrderAccount{
			defaultPlatformOrderAccountKey: arp,
			"warehouse:DPSCA004":           dps,
		},
		warehouseOperators: map[string]platformOrderOperator{"WH-1": dps},
	}
	fulfillment := readyPlatformFulfillment("PO-A")
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, arp, readyPlatformMappings(), fulfillment, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/routing-preview", strings.NewReader(
		`{"platform_order_nos":["PO-A"],"account":"warehouse:DPSCA004"}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(arp.lookupOrderNos) != 0 {
		t.Fatalf("ARP account unexpectedly checked selected DPS order: %#v", arp.lookupOrderNos)
	}
	if len(dps.lookupOrderNos) != 1 || dps.lookupOrderNos[0] != "PO-A" {
		t.Fatalf("DPS lookup = %#v", dps.lookupOrderNos)
	}
}

func TestPendingPlatformOrdersRejectsUnknownAccount(t *testing.T) {
	arp := readyPlatformOrderOperator()
	accounts := &fakeSelectablePlatformAccounts{
		accountOperators: map[string]platformOrderAccount{defaultPlatformOrderAccountKey: arp},
	}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, arp, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/pending?account=unknown", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if arp.page != 0 {
		t.Fatalf("ARP queried for invalid account: page=%d", arp.page)
	}
}

func TestPlatformOrderAccountLabelUsesCommonWarehousePrefix(t *testing.T) {
	if got := platformOrderAccountLabel([]string{"DPSCA004", "DPSNY002"}); got != "DPS 账户" {
		t.Fatalf("label = %q", got)
	}
}
