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
	"xlwms-api-manager/internal/store"
)

type fakePlatformOrderAccountStore struct {
	summaries     []model.OMSAccountSummary
	loginAccounts map[string]model.OMSLoginAccount
	accountErrors map[string]error
}

func (f *fakePlatformOrderAccountStore) ListOMSAccountSummaries(context.Context, bool) ([]model.OMSAccountSummary, error) {
	return f.summaries, nil
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
	account := model.OMSLoginAccount{Key: key, Label: key, Username: username, Password: password, Hint: username, Enabled: true}
	f.loginAccounts[key] = account
	return account, nil
}

func (f *fakePlatformOrderAccountStore) CreateOMSAccount(_ context.Context, key, label, username, password string, credentialKeys []string) (model.OMSAccountSummary, error) {
	if f.loginAccounts == nil {
		f.loginAccounts = map[string]model.OMSLoginAccount{}
	}
	if _, exists := f.loginAccounts[key]; exists {
		return model.OMSAccountSummary{}, store.ErrOMSAccountExists
	}
	f.loginAccounts[key] = model.OMSLoginAccount{Key: key, Label: label, Username: username, Password: password, Enabled: true}
	item := model.OMSAccountSummary{Key: key, Label: label, UsernameHint: username, Enabled: true, APICredentialKeys: credentialKeys}
	f.summaries = append(f.summaries, item)
	return item, nil
}

type fakeSelectablePlatformAccounts struct {
	accountOperators map[string]platformOrderAccount
	options          []platformOrderAccountOption
	selectedAccounts []string
}

func (f *fakeSelectablePlatformAccounts) PlatformOrderAccounts(context.Context) ([]platformOrderAccountOption, error) {
	return f.options, nil
}

func (f *fakeSelectablePlatformAccounts) OperatorForAccount(_ context.Context, key string) (platformOrderAccount, error) {
	f.selectedAccounts = append(f.selectedAccounts, key)
	operator := f.accountOperators[key]
	if operator == nil {
		return nil, errPlatformOrderAccountNotFound
	}
	return operator, nil
}

type fakeMutablePlatformAccounts struct {
	*fakeSelectablePlatformAccounts
	updateErr       error
	updatedKey      string
	createdKey      string
	createdLabel    string
	createdAPIKeys  []string
	upgradedKey     string
	upgradeUsername string
	mfaPrompt       oms.MFAPrompt
	mfaErr          error
	mfaKey          string
	mfaCode         string
}

func (f *fakeMutablePlatformAccounts) UpdateAccountCredentials(_ context.Context, key, _, _ string) error {
	f.updatedKey = key
	return f.updateErr
}

func (f *fakeMutablePlatformAccounts) CreateAccount(_ context.Context, key, label, _, _ string, credentialKeys []string) (model.OMSAccountSummary, error) {
	f.createdKey, f.createdLabel, f.createdAPIKeys = key, label, credentialKeys
	return model.OMSAccountSummary{Key: key, Label: label, Enabled: true, APICredentialKeys: credentialKeys}, nil
}

func (f *fakeMutablePlatformAccounts) UpgradeAccountPassword(_ context.Context, key, username, _, _ string) error {
	f.upgradedKey, f.upgradeUsername = key, username
	return nil
}

func (f *fakeMutablePlatformAccounts) BeginAccountMFA(_ context.Context, key string) (oms.MFAPrompt, error) {
	f.mfaKey = key
	return f.mfaPrompt, f.mfaErr
}

func (f *fakeMutablePlatformAccounts) CompleteAccountMFA(_ context.Context, key, code string) error {
	f.mfaKey, f.mfaCode = key, code
	return f.mfaErr
}

func TestPlatformOrderAccountUpdateSavesVerifiedLogin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/gateway/woms/auth/login" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{"code":200,"msg":"ok","data":{"token":"token"}}`))
	}))
	defer server.Close()

	accountStore := &fakePlatformOrderAccountStore{loginAccounts: map[string]model.OMSLoginAccount{
		"laundry": {Key: "laundry", Username: "old", Password: "old-password", Enabled: true},
	}}
	resolver := &postgresPlatformOrderAccounts{store: accountStore, baseURL: server.URL, timeout: time.Second}
	if err := resolver.UpdateAccountCredentials(context.Background(), "laundry", "new-user", "new-password"); err != nil {
		t.Fatal(err)
	}
	account := accountStore.loginAccounts["laundry"]
	if account.Username != "new-user" || account.Password != "new-password" {
		t.Fatalf("account = %#v", account)
	}
}

func TestPlatformOrderAccountUpdateSavesLoginAcceptedForMFA(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"code":4011,"msg":"需要短信/邮箱二次验证","data":{"loginAction":"NEED_MFA_VERIFY","needVerify":true,"challengeId":"challenge"}}`))
	}))
	defer server.Close()

	accountStore := &fakePlatformOrderAccountStore{loginAccounts: map[string]model.OMSLoginAccount{
		"laundry": {Key: "laundry", Username: "old", Password: "old-password", Enabled: true},
	}}
	resolver := &postgresPlatformOrderAccounts{store: accountStore, baseURL: server.URL, timeout: time.Second}
	if err := resolver.UpdateAccountCredentials(context.Background(), "laundry", "operator", "accepted-password"); err != nil {
		t.Fatal(err)
	}
	if accountStore.loginAccounts["laundry"].Password != "accepted-password" {
		t.Fatal("MFA-accepted credentials were not saved")
	}
}

func TestPlatformOrderAccountCreatePersistsAPIBindings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/gateway/woms/auth/login" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{"code":200,"msg":"ok","data":{"token":"new-token"}}`))
	}))
	defer server.Close()

	accountStore := &fakePlatformOrderAccountStore{}
	resolver := &postgresPlatformOrderAccounts{store: accountStore, baseURL: server.URL, timeout: time.Second}
	item, err := resolver.CreateAccount(context.Background(), "laundry", "脏衣篓", "operator", "password", []string{"api-one"})
	if err != nil {
		t.Fatal(err)
	}
	if item.Key != "laundry" || len(item.APICredentialKeys) != 1 || item.APICredentialKeys[0] != "api-one" {
		t.Fatalf("created account = %#v", item)
	}
}

func TestPlatformOrderAccountCreatePersistsLoginRequiringPasswordUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/gateway/woms/auth/login" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{"code":4011,"msg":"请更新登录密码"}`))
	}))
	defer server.Close()

	accountStore := &fakePlatformOrderAccountStore{}
	resolver := &postgresPlatformOrderAccounts{store: accountStore, baseURL: server.URL, timeout: time.Second}
	item, err := resolver.CreateAccount(context.Background(), "laundry", "脏衣篓", "operator", "expired-password", []string{"api-one"})
	if err != nil {
		t.Fatal(err)
	}
	if item.Key != "laundry" || accountStore.loginAccounts["laundry"].Username != "operator" {
		t.Fatalf("created account = %#v", item)
	}
}

func TestFulfillmentAccountCreateEndpointUsesAPIBindings(t *testing.T) {
	accounts := &fakeMutablePlatformAccounts{fakeSelectablePlatformAccounts: &fakeSelectablePlatformAccounts{}}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, nil, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/fulfillment-policies/accounts", strings.NewReader(
		`{"key":"laundry","label":"脏衣篓","username":"operator","password":"password","api_credential_keys":["api-one"]}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || accounts.createdKey != "laundry" || len(accounts.createdAPIKeys) != 1 {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlatformOrderAccountsListsAliasesAndAPIBindings(t *testing.T) {
	resolver := &postgresPlatformOrderAccounts{store: &fakePlatformOrderAccountStore{summaries: []model.OMSAccountSummary{
		{Key: "hanger", Label: "FHZARP-衣架", APICredentialKeys: []string{"api-hanger"}, UsernameHint: "FH***RP", Enabled: true},
		{Key: "laundry", Label: "FHZARP-脏衣篓", APICredentialKeys: []string{"api-laundry"}, UsernameHint: "FH***75", Enabled: true},
	}}}
	options, err := resolver.PlatformOrderAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 2 || options[1].Label != "FHZARP-脏衣篓" || options[1].APICredentialKeys[0] != "api-laundry" {
		t.Fatalf("options = %#v", options)
	}
}

func TestRequestedPlatformOrderAccountUsesGenericCompatibilitySelector(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/pending", nil)
	if key, err := requestedPlatformOrderAccount(request); err != nil || key != defaultPlatformOrderAccountSelector {
		t.Fatalf("default account = %q, %v", key, err)
	}
	request.Header.Set(platformOrderAccountHeader, "laundry")
	key, err := requestedPlatformOrderAccount(request)
	if err != nil || key != "laundry" {
		t.Fatalf("selected account = %q, %v", key, err)
	}
}

func TestPendingPlatformOrdersUsesSelectedAccount(t *testing.T) {
	operator := readyPlatformOrderOperator()
	accounts := &fakeSelectablePlatformAccounts{accountOperators: map[string]platformOrderAccount{"laundry": operator}}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, nil, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/v1/platform-orders/pending?account=laundry", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || len(accounts.selectedAccounts) != 1 || accounts.selectedAccounts[0] != "laundry" {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlatformOrderAccountUpdateReturnsPasswordUpgradeCode(t *testing.T) {
	accounts := &fakeMutablePlatformAccounts{
		fakeSelectablePlatformAccounts: &fakeSelectablePlatformAccounts{},
		updateErr:                      oms.ErrPasswordUpdateRequired,
	}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, nil, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPatch, "/v1/platform-orders/accounts/laundry", strings.NewReader(`{"username":"operator","password":"password"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var payload response
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusConflict || payload.Code != "OMS_PASSWORD_UPDATE_REQUIRED" {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlatformOrderAccountMFAEndpoints(t *testing.T) {
	accounts := &fakeMutablePlatformAccounts{
		fakeSelectablePlatformAccounts: &fakeSelectablePlatformAccounts{},
		mfaPrompt:                      oms.MFAPrompt{Channel: "TOTP", MaskedTarget: "Authenticator", CodeLength: 6},
	}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, nil, nil, nil, accounts, time.Second, slog.Default())

	challengeRequest := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/accounts/laundry/mfa-challenge", nil)
	challengeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(challengeRecorder, challengeRequest)
	if challengeRecorder.Code != http.StatusOK || accounts.mfaKey != "laundry" ||
		strings.Contains(challengeRecorder.Body.String(), "challengeId") {
		t.Fatalf("unexpected challenge response %d: %s", challengeRecorder.Code, challengeRecorder.Body.String())
	}

	verifyRequest := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/accounts/laundry/mfa-verify", strings.NewReader(`{"code":"123456"}`))
	verifyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(verifyRecorder, verifyRequest)
	if verifyRecorder.Code != http.StatusOK || accounts.mfaKey != "laundry" || accounts.mfaCode != "123456" {
		t.Fatalf("unexpected verify response %d: %s", verifyRecorder.Code, verifyRecorder.Body.String())
	}
}

func TestPlatformOrderAccountMFAErrorReportsVerificationFailure(t *testing.T) {
	accounts := &fakeMutablePlatformAccounts{
		fakeSelectablePlatformAccounts: &fakeSelectablePlatformAccounts{},
		mfaErr:                         &oms.MFARequestError{Stage: "verify", HTTPStatus: 200, Code: 4012},
	}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, nil, nil, nil, accounts, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/platform-orders/accounts/laundry/mfa-verify", strings.NewReader(`{"code":"123456"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "OMS_MFA_4012") ||
		!strings.Contains(recorder.Body.String(), "验证码校验未通过") || strings.Contains(recorder.Body.String(), "更新登录密码") {
		t.Fatalf("unexpected verification failure: %d %s", recorder.Code, recorder.Body.String())
	}
}
