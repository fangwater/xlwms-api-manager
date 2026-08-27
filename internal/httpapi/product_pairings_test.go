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

	"xlwms-api-manager/internal/oms"
)

type fakeProductPairingAccount struct {
	*fakePlatformOrders
	filter          oms.ProductPairingFilter
	page            oms.ProductPairingPage
	created         oms.ProductPairingInput
	deletedStore    string
	deletedPlatform string
	err             error
}

func (f *fakeProductPairingAccount) ProductPairings(_ context.Context, filter oms.ProductPairingFilter) (oms.ProductPairingPage, error) {
	f.filter = filter
	return f.page, f.err
}

func (f *fakeProductPairingAccount) CreateProductPairing(_ context.Context, input oms.ProductPairingInput) error {
	f.created = input
	return f.err
}

func (f *fakeProductPairingAccount) DeleteProductPairing(_ context.Context, storeCode, platformSKU string) error {
	f.deletedStore = storeCode
	f.deletedPlatform = platformSKU
	return f.err
}

func TestProductPairingHandlersUseSelectedAccount(t *testing.T) {
	arp := &fakeProductPairingAccount{fakePlatformOrders: readyPlatformOrderOperator()}
	dps := &fakeProductPairingAccount{
		fakePlatformOrders: readyPlatformOrderOperator(),
		page: oms.ProductPairingPage{
			Records: []oms.ProductPairing{{PlatformSKU: "20PCS", StoreCode: "TEMU-US", Items: []oms.ProductPairingItem{{SystemSKU: "10PCS", Quantity: 2}}}},
			Total:   1, Page: 3, PageSize: 40, Pages: 1,
		},
	}
	accounts := &fakeSelectablePlatformAccounts{accountOperators: map[string]platformOrderAccount{
		defaultPlatformOrderAccountKey: arp,
		"warehouse:DPSCA004":           dps,
	}}
	handler := newWithPlatformOrderAccountOperations(nil, nil, nil, arp, nil, nil, accounts, time.Second, slog.Default())

	listRequest := httptest.NewRequest(http.MethodGet, "/v1/product-pairings?page=3&page_size=40&store_code=TEMU-US&q=20PCS&query_field=platform_sku", nil)
	listRequest.Header.Set(platformOrderAccountHeader, "warehouse:DPSCA004")
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	if dps.filter.Page != 3 || dps.filter.PageSize != 40 || dps.filter.StoreCode != "TEMU-US" || dps.filter.Query != "20PCS" || arp.filter.Page != 0 {
		t.Fatalf("unexpected filters: DPS=%#v ARP=%#v", dps.filter, arp.filter)
	}
	var listPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Account string               `json:"account"`
			Records []oms.ProductPairing `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listPayload); err != nil {
		t.Fatal(err)
	}
	if !listPayload.Success || listPayload.Data.Account != "warehouse:DPSCA004" || len(listPayload.Data.Records) != 1 {
		t.Fatalf("unexpected list response: %#v", listPayload)
	}

	createRequest := httptest.NewRequest(http.MethodPost, "/v1/product-pairings", strings.NewReader(
		`{"account":"warehouse:DPSCA004","store_code":" TEMU-US ","platform_sku":" 20PCS ","items":[{"system_sku":" 10PCS ","quantity":2}]}`,
	))
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", createRecorder.Code, createRecorder.Body.String())
	}
	if dps.created.StoreCode != "TEMU-US" || dps.created.PlatformSKU != "20PCS" || len(dps.created.Items) != 1 ||
		dps.created.Items[0].SystemSKU != "10PCS" || dps.created.Items[0].Quantity != 2 {
		t.Fatalf("unexpected create input: %#v", dps.created)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/v1/product-pairings", strings.NewReader(
		`{"store_code":" TEMU-US ","platform_sku":" 20PCS "}`,
	))
	deleteRequest.Header.Set(platformOrderAccountHeader, "warehouse:DPSCA004")
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete status %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	if dps.deletedStore != "TEMU-US" || dps.deletedPlatform != "20PCS" {
		t.Fatalf("unexpected deleted key: %q %q", dps.deletedStore, dps.deletedPlatform)
	}

	dps.deletedStore = ""
	dps.deletedPlatform = ""
	proxyDeleteRequest := httptest.NewRequest(http.MethodPost, "/v1/product-pairings/delete", strings.NewReader(
		`{"account":"warehouse:DPSCA004","store_code":"TEMU-US","platform_sku":"20PCS"}`,
	))
	proxyDeleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(proxyDeleteRecorder, proxyDeleteRequest)
	if proxyDeleteRecorder.Code != http.StatusOK || dps.deletedStore != "TEMU-US" || dps.deletedPlatform != "20PCS" {
		t.Fatalf("proxy-compatible delete status %d: %s", proxyDeleteRecorder.Code, proxyDeleteRecorder.Body.String())
	}
}

func TestCreateProductPairingRejectsInvalidRequestBeforeCallingOMS(t *testing.T) {
	account := &fakeProductPairingAccount{fakePlatformOrders: readyPlatformOrderOperator()}
	handler := NewWithPlatformOrders(nil, nil, nil, account, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/product-pairings", strings.NewReader(
		`{"store_code":"TEMU-US","platform_sku":"20PCS","items":[{"system_sku":"10PCS","quantity":0}]}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if account.created.PlatformSKU != "" {
		t.Fatalf("OMS create should not be called: %#v", account.created)
	}
}

func TestValidateProductPairingReturnsWarehouseReadinessReason(t *testing.T) {
	account := &fakeProductPairingAccount{
		fakePlatformOrders: readyPlatformOrderOperator(),
		page: oms.ProductPairingPage{Records: []oms.ProductPairing{{
			PlatformSKU: "SKU-20", StoreCode: "TEMU-US",
			Items: []oms.ProductPairingItem{{SystemSKU: "SKU-10", Quantity: 2, ApproveStatus: 1}},
		}}, Total: 1, Pages: 1},
	}
	handler := NewWithPlatformOrders(nil, nil, nil, account, time.Second, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/v1/product-pairings/validate", strings.NewReader(
		`{"platform_sku":"SKU-20","items":[{"system_sku":"SKU-10","quantity":2}]}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Success bool                           `json:"success"`
		Data    productPairingValidationResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data.Ready || !strings.Contains(payload.Data.Reason, "尚未全部审核通过") {
		t.Fatalf("unexpected validation: %#v", payload)
	}
	if account.filter.Query != "SKU-20" || account.filter.QueryField != "platform_sku" || account.filter.PageSize != 100 {
		t.Fatalf("unexpected filter: %#v", account.filter)
	}

	account.page.Records[0].Items[0].ApproveStatus = 2
	approvedRequest := httptest.NewRequest(http.MethodPost, "/v1/product-pairings/validate", strings.NewReader(
		`{"platform_sku":"SKU-20","items":[{"system_sku":"SKU-10","quantity":2}]}`,
	))
	approvedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(approvedRecorder, approvedRequest)
	payload.Data = productPairingValidationResult{}
	if err := json.Unmarshal(approvedRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Data.Ready || payload.Data.ApprovedRecords != 1 || payload.Data.Reason != "" {
		t.Fatalf("unexpected approved validation: %#v", payload.Data)
	}
}
