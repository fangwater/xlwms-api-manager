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
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/temutracking"
)

type fakePlatformOrderAccountStore struct {
	summaries     []model.OMSAccountSummary
	loginAccounts map[string]model.OMSLoginAccount
	accountErrors map[string]error
}

func (f *fakePlatformOrderAccountStore) ListOMSAccountSummaries(context.Context, bool) ([]model.OMSAccountSummary, error) {
	return f.summaries, nil
}

func (f *fakePlatformOrderAccountStore) ExistingWarehouseCodes(_ context.Context, codes []string) ([]string, error) {
	return codes, nil
}

func (f *fakePlatformOrderAccountStore) OMSAccount(_ context.Context, key string) (model.OMSLoginAccount, error) {
	if err := f.accountErrors[key]; err != nil {
		return model.OMSLoginAccount{}, err
	}
	account, exists := f.loginAccounts[key]
	if !exists {
		return model.OMSLoginAccount{}, store.ErrOMSAccountNotFound
	}
	return account, nil
}

func (f *fakePlatformOrderAccountStore) SetOMSAccount(_ context.Context, key, username, password string) (model.OMSLoginAccount, error) {
	if f.loginAccounts == nil {
		f.loginAccounts = map[string]model.OMSLoginAccount{}
	}
	account := model.OMSLoginAccount{Key: key, Label: strings.ToUpper(key) + " 账户", Username: username, Password: password, Hint: username, Enabled: true}
	f.loginAccounts[key] = account
	return account, nil
}

func (f *fakePlatformOrderAccountStore) CreateOMSAccount(_ context.Context, key, label, username, password string, warehouseCodes []string) (model.OMSAccountSummary, error) {
	if f.loginAccounts == nil {
		f.loginAccounts = map[string]model.OMSLoginAccount{}
	}
	if _, exists := f.loginAccounts[key]; exists {
		return model.OMSAccountSummary{}, store.ErrOMSAccountExists
	}
	f.loginAccounts[key] = model.OMSLoginAccount{Key: key, Label: label, Username: username, Password: password, Enabled: true}
	item := model.OMSAccountSummary{Key: key, Label: label, UsernameHint: username, Enabled: true, WarehouseCodes: warehouseCodes}
	f.summaries = append(f.summaries, item)
	return item, nil
}

type fakeSelectablePlatformAccounts struct {
	accountOperators map[string]platformOrderAccount
	options          []platformOrderAccountOption
	selectedAccounts []string
}

type fakeMutablePlatformAccounts struct {
	*fakeSelectablePlatformAccounts
	updateErr       error
	updatedKey      string
	createdKey      string
	createdLabel    string
	upgradedKey     string
	upgradeUsername string
}

func (f *fakeMutablePlatformAccounts) UpdateAccountCredentials(_ context.Context, key, _, _ string) error {
	f.updatedKey = key
	return f.updateErr
}

func (f *fakeMutablePlatformAccounts) CreateAccount(_ context.Context, key, label, _, _ string, warehouseCodes []string) (model.OMSAccountSummary, error) {
	f.createdKey = key
	f.createdLabel = label
	return model.OMSAccountSummary{Key: key, Label: label, Enabled: true, WarehouseCodes: warehouseCodes}, nil
}

func (f *fakeMutablePlatformAccounts) UpgradeAccountPassword(_ context.Context, key, username, _, _ string) error {
	f.upgradedKey = key
	f.upgradeUsername = username
	return nil
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

func TestPlatformOrderAccountUpdateSavesVerifiedExplicitLogin(t *testing.T) {
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
		if payload.LoginAccount != "new-dps" {
			_, _ = writer.Write([]byte(`{"code":4213,"msg":"账号已锁定,请联系超管修改或重置密码","data":{}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"code":200,"msg":"ok","data":{"token":"dps-token"}}`))
	}))
	defer server.Close()

	accountStore := &fakePlatformOrderAccountStore{
		loginAccounts: map[string]model.OMSLoginAccount{
			"dps": {Key: "dps", Username: "old-dps", Password: "old-password", Enabled: true},
		},
	}
	resolver := &postgresPlatformOrderAccounts{
		store: accountStore, baseURL: server.URL, timeout: time.Second,
	}
	if err := resolver.UpdateAccountCredentials(context.Background(), "dps", "new-dps", "new-password"); err != nil {
		t.Fatal(err)
	}
	account := accountStore.loginAccounts["dps"]
	if account.Username != "new-dps" || account.Password != "new-password" {
		t.Fatalf("DPS account = %#v", account)
	}
}

func TestPlatformOrderAccountCreateValidatesLoginAndPersists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/gateway/woms/auth/login":
			_, _ = writer.Write([]byte(`{"code":200,"msg":"ok","data":{"token":"new-token"}}`))
		case "/gateway/woms/warehouse/options":
			_, _ = writer.Write([]byte(`{"code":200,"msg":"ok","data":[{"whCode":"HYTX30","whNameCn":"ARP East"}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	accountStore := &fakePlatformOrderAccountStore{}
	resolver := &postgresPlatformOrderAccounts{store: accountStore, baseURL: server.URL, timeout: time.Second}
	item, err := resolver.CreateAccount(context.Background(), "backup", "备用账户", "backup-user", "password", []string{"HYTX30"})
	if err != nil {
		t.Fatal(err)
	}
	if item.Key != "backup" || item.Label != "备用账户" || len(item.WarehouseCodes) != 1 {
		t.Fatalf("created account = %#v", item)
	}
	if accountStore.loginAccounts["backup"].Username != "backup-user" {
		t.Fatal("verified account credentials were not persisted")
	}
}

func TestFulfillmentAccountCreateEndpoint(t *testing.T) {
	accounts := &fakeMutablePlatformAccounts{fakeSelectablePlatformAccounts: &fakeSelectablePlatformAccounts{}}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, nil, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/fulfillment-policies/accounts", strings.NewReader(
		`{"key":"backup","label":"备用账户","username":"backup-user","password":"password","warehouse_codes":["HYTX30"]}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated || accounts.createdKey != "backup" || accounts.createdLabel != "备用账户" {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlatformOrderAccountUpdateSavesVerifiedSharedLogin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/gateway/woms/auth/login" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{"code":200,"msg":"ok","data":{"token":"arp-token"}}`))
	}))
	defer server.Close()

	accountStore := &fakePlatformOrderAccountStore{loginAccounts: map[string]model.OMSLoginAccount{
		"arp": {Key: "arp", Username: "old-arp", Password: "old-password", Enabled: true},
	}}
	resolver := &postgresPlatformOrderAccounts{
		store: accountStore, baseURL: server.URL, timeout: time.Second,
		shared:         oms.NewClient(server.URL, "old-arp", "old-password", time.Second),
		sharedUsername: "old-arp", sharedPassword: "old-password",
	}
	if err := resolver.UpdateAccountCredentials(context.Background(), "arp", "new-arp", "new-password"); err != nil {
		t.Fatal(err)
	}
	stored := accountStore.loginAccounts[defaultPlatformOrderAccountKey]
	if stored.Username != "new-arp" || stored.Password != "new-password" {
		t.Fatalf("stored ARP account = %#v", stored)
	}
	account, err := resolver.OperatorForAccount(context.Background(), "arp")
	if err != nil {
		t.Fatal(err)
	}
	if account == resolver.shared && resolver.sharedUsername == "old-arp" {
		t.Fatal("updated ARP account still uses the previous shared client")
	}
}

func TestDisabledSharedAccountDoesNotFallBackToEnvironmentCredentials(t *testing.T) {
	accountStore := &fakePlatformOrderAccountStore{accountErrors: map[string]error{
		defaultPlatformOrderAccountKey: store.ErrOMSAccountDisabled,
	}}
	resolver := &postgresPlatformOrderAccounts{
		store:  accountStore,
		shared: readyPlatformOrderOperator(), sharedUsername: "legacy-user", sharedPassword: "legacy-password",
	}
	if _, err := resolver.OperatorForAccount(context.Background(), defaultPlatformOrderAccountKey); !errors.Is(err, errPlatformOrderAccountUnavailable) {
		t.Fatalf("disabled shared account error = %v", err)
	}
}

func TestPlatformOrderAccountPasswordUpgradeSavesVerifiedSharedLogin(t *testing.T) {
	const (
		currentPassword = "Old!Password7Q"
		newPassword     = "Fresh!Moon9Qz"
	)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/gateway/woms/auth/login":
			var payload struct {
				Password string `json:"password"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Error(err)
				return
			}
			if payload.Password == currentPassword {
				_, _ = writer.Write([]byte(`{"code":4011,"msg":"请更新登录密码","data":{"loginAction":"NEED_UPDATE_PASSWORD","securitySessionToken":"session-token"}}`))
				return
			}
			if payload.Password != newPassword {
				t.Errorf("unexpected login password")
			}
			_, _ = writer.Write([]byte(`{"code":200,"msg":"ok","data":{"token":"fresh-token"}}`))
		case "/gateway/woms/auth/securityUpgrade/updatePassword":
			var payload struct {
				SecuritySessionToken string `json:"securitySessionToken"`
				NewPassword          string `json:"newPassword"`
				ConfirmPassword      string `json:"confirmPassword"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Error(err)
				return
			}
			if payload.SecuritySessionToken != "session-token" || payload.NewPassword != newPassword || payload.ConfirmPassword != newPassword {
				t.Errorf("unexpected password upgrade payload")
			}
			_, _ = writer.Write([]byte(`{"code":200,"msg":"ok","data":{}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	accountStore := &fakePlatformOrderAccountStore{loginAccounts: map[string]model.OMSLoginAccount{
		"arp": {Key: "arp", Username: "old-arp", Password: currentPassword, Enabled: true},
	}}
	resolver := &postgresPlatformOrderAccounts{
		store: accountStore, baseURL: server.URL, timeout: time.Second,
		shared:         oms.NewClient(server.URL, "old-arp", currentPassword, time.Second),
		sharedUsername: "old-arp", sharedPassword: currentPassword,
	}
	if err := resolver.UpgradeAccountPassword(context.Background(), "arp", "arp-user", currentPassword, newPassword); err != nil {
		t.Fatal(err)
	}
	stored := accountStore.loginAccounts[defaultPlatformOrderAccountKey]
	if stored.Username != "arp-user" || stored.Password != newPassword {
		t.Fatalf("stored ARP credentials were not updated")
	}
	if resolver.sharedUsername != "arp-user" || resolver.sharedPassword != newPassword {
		t.Fatal("shared OMS client was not replaced after password upgrade")
	}
}

func TestPlatformOrderAccountPasswordUpgradeValidatesAccountBeforeRemoteChange(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		http.NotFound(writer, request)
	}))
	defer server.Close()

	accountStore := &fakePlatformOrderAccountStore{}
	resolver := &postgresPlatformOrderAccounts{
		store: accountStore, baseURL: server.URL, timeout: time.Second,
		shared: readyPlatformOrderOperator(), sharedUsername: "arp-user", sharedPassword: "Old!Password7Q",
	}
	err := resolver.UpgradeAccountPassword(context.Background(), "unknown", "arp-user", "Old!Password7Q", "Fresh!Moon9Qz")
	if !errors.Is(err, errPlatformOrderAccountNotFound) {
		t.Fatalf("error = %v, want errPlatformOrderAccountNotFound", err)
	}
	if requests != 0 {
		t.Fatalf("remote OMS received %d requests for an invalid account", requests)
	}
}

func TestPlatformOrderAccountUpdateReturnsPasswordUpgradeCode(t *testing.T) {
	accounts := &fakeMutablePlatformAccounts{
		fakeSelectablePlatformAccounts: &fakeSelectablePlatformAccounts{
			options: []platformOrderAccountOption{{Key: defaultPlatformOrderAccountKey, Label: "ARP 账户"}},
		},
		updateErr: oms.ErrPasswordUpdateRequired,
	}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, nil, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPatch, "/v1/platform-orders/accounts/arp", strings.NewReader(`{"username":"arp-user","password":"current-password"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"code":"OMS_PASSWORD_UPDATE_REQUIRED"`) {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlatformOrderAccountPasswordUpgradeEndpoint(t *testing.T) {
	accounts := &fakeMutablePlatformAccounts{
		fakeSelectablePlatformAccounts: &fakeSelectablePlatformAccounts{
			options: []platformOrderAccountOption{{Key: defaultPlatformOrderAccountKey, Label: "ARP 账户"}},
		},
	}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, nil, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/accounts/arp/password-upgrade", strings.NewReader(
		`{"username":"arp-user","current_password":"current-password","new_password":"new-password","confirm_new_password":"new-password"}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || accounts.upgradedKey != "arp" || accounts.upgradeUsername != "arp-user" {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlatformOrderAccountsListsExplicitAccountsAndWarehouses(t *testing.T) {
	accountStore := &fakePlatformOrderAccountStore{
		summaries: []model.OMSAccountSummary{
			{Key: "arp", Label: "ARP 账户", UsernameHint: "AR***NT", WarehouseCodes: []string{"ARPCA01", "HYTX30"}},
			{Key: "dps", Label: "DPS 账户", UsernameHint: "DP***NT", WarehouseCodes: []string{"DPSCA004", "DPSNY002"}},
		},
	}
	resolver := &postgresPlatformOrderAccounts{store: accountStore}
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
	if options[1].Key != "dps" || options[1].Label != "DPS 账户" ||
		len(options[1].WarehouseCodes) != 2 {
		t.Fatalf("DPS option = %#v", options[1])
	}
}

func TestPlatformOrderAccountUsesExplicitKeyAndRejectsWarehouseAlias(t *testing.T) {
	shared := readyPlatformOrderOperator()
	accountStore := &fakePlatformOrderAccountStore{
		loginAccounts: map[string]model.OMSLoginAccount{
			"arp": {Key: "arp", Username: "arp-user", Password: "arp-password", Enabled: true},
			"dps": {Key: "dps", Username: "dps-user", Password: "dps-password", Enabled: true},
		},
	}
	resolver := &postgresPlatformOrderAccounts{
		store: accountStore, baseURL: "https://oms.example.test", timeout: time.Second,
		shared: shared, sharedUsername: "arp-user", sharedPassword: "arp-password",
	}
	dpsAccount, err := resolver.OperatorForAccount(context.Background(), dpsPlatformOrderAccountKey)
	if err != nil {
		t.Fatal(err)
	}
	if dpsAccount == shared {
		t.Fatal("DPS account unexpectedly resolved to shared ARP client")
	}
	if _, err := resolver.OperatorForAccount(context.Background(), "warehouse:DPSNY002"); !errors.Is(err, errPlatformOrderAccountNotFound) {
		t.Fatalf("warehouse alias error = %v", err)
	}
}

func TestPendingPlatformOrdersUsesSelectedAccount(t *testing.T) {
	arp := readyPlatformOrderOperator()
	dps := readyPlatformOrderOperator()
	accounts := &fakeSelectablePlatformAccounts{
		accountOperators: map[string]platformOrderAccount{
			defaultPlatformOrderAccountKey: arp,
			dpsPlatformOrderAccountKey:     dps,
		},
		options: []platformOrderAccountOption{
			{Key: defaultPlatformOrderAccountKey, Label: "ARP 账户"},
			{Key: dpsPlatformOrderAccountKey, Label: "DPS 账户", WarehouseCodes: []string{"DPSCA004", "DPSNY002"}},
		},
	}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, arp, nil, nil, accounts, time.Second, slog.Default())

	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/pending?account=dps&page=3&page_size=40", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if arp.page != 0 || dps.page != 3 || dps.pageSize != 40 {
		t.Fatalf("pending queries ARP=(%d,%d), DPS=(%d,%d)", arp.page, arp.pageSize, dps.page, dps.pageSize)
	}
	if len(accounts.selectedAccounts) != 1 || accounts.selectedAccounts[0] != dpsPlatformOrderAccountKey {
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
			dpsPlatformOrderAccountKey:     dps,
		},
	}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, arp, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/PO-A", nil)
	request.Header.Set(platformOrderAccountHeader, dpsPlatformOrderAccountKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if arp.allStatusLookup != "" || dps.allStatusLookup != "PO-A" {
		t.Fatalf("all-status lookups ARP=%q DPS=%q", arp.allStatusLookup, dps.allStatusLookup)
	}
	if len(accounts.selectedAccounts) != 1 || accounts.selectedAccounts[0] != dpsPlatformOrderAccountKey {
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
			dpsPlatformOrderAccountKey:     dps,
		},
		options: []platformOrderAccountOption{
			{Key: defaultPlatformOrderAccountKey, Label: "ARP 账户"},
			{Key: dpsPlatformOrderAccountKey, Label: "DPS 账户", WarehouseCodes: []string{"DPSCA004", "DPSNY002"}},
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
		Success bool                         `json:"success"`
		Data    []platformOrderAccountOption `json:"data"`
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
			{Key: dpsPlatformOrderAccountKey, Label: "DPS 账户", WarehouseCodes: []string{"DPSCA004", "DPSNY002"}},
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

func TestAssignAndApproveUsesSelectedAccountForWarehouseAssignment(t *testing.T) {
	arp := readyPlatformOrderOperator()
	dps := readyPlatformOrderOperator()
	dps.warehouses = []oms.WarehouseOption{{WarehouseCode: "WH-2", WarehouseName: "DPS warehouse"}}
	dps.resolved = []oms.PendingOrder{{OrderNo: "DPS-OMS-A", PlatformOrderNo: "PO-A", Status: 0}}
	dps.assignmentResult = oms.AssignmentResult{TotalQuantity: 1, SuccessQuantity: 1}
	accounts := &fakeSelectablePlatformAccounts{accountOperators: map[string]platformOrderAccount{
		defaultPlatformOrderAccountKey: arp, dpsPlatformOrderAccountKey: dps,
	}}
	mappings := &fakePlatformMappings{mappings: []temutracking.WarehouseMapping{{
		OMSKey: "DPS", OMSWarehouseCode: "WH-2", TemuWarehouseID: "PLATFORM-DPS", TemuName: "DPS",
	}}}
	fulfillment := &fakePlatformFulfillment{audits: []model.FulfillmentAudit{{
		Platform: "temu", PlatformOrderNo: "PO-A", Active: true, WarehouseKey: "DPS", WarehouseCode: "WH-2",
	}}}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, arp, mappings, fulfillment, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/assign-and-approve", strings.NewReader(
		`{"platform_order_nos":["PO-A"],"account":"dps","logistics_carrier":"other","confirmation":"CONFIRM_AND_APPROVE"}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if arp.assignCalls != 0 || dps.assignCalls != 1 {
		t.Fatalf("assignment calls ARP=%d DPS=%d", arp.assignCalls, dps.assignCalls)
	}
	if dps.assignment.WarehouseCode != "WH-2" {
		t.Fatalf("selected account assignment = %#v", dps.assignment)
	}
	if len(dps.lookupOrderNos) == 0 || dps.lookupOrderNos[0] != "PO-A" {
		t.Fatalf("selected account did not resolve its OMS order number: %#v", dps.lookupOrderNos)
	}
	if len(dps.assignment.Orders) != 1 || dps.assignment.Orders[0] != "DPS-OMS-A" {
		t.Fatalf("selected account assignment used the wrong OMS order number: %#v", dps.assignment.Orders)
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
			dpsPlatformOrderAccountKey:     dps,
		},
	}
	fulfillment := readyPlatformFulfillment("PO-A")
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, arp, readyPlatformMappings(), fulfillment, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/routing-preview", strings.NewReader(
		`{"platform_order_nos":["PO-A"],"account":"dps"}`,
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
