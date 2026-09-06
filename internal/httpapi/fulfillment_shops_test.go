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
	"xlwms-api-manager/internal/store"
)

type fakeFulfillmentShopStore struct {
	items           []model.FulfillmentShop
	includeDisabled bool
	upserted        model.FulfillmentShop
	patched         model.FulfillmentShop
	actor           string
	created         bool
	err             error
}

func (f *fakeFulfillmentShopStore) ListFulfillmentShops(_ context.Context, includeDisabled bool) ([]model.FulfillmentShop, error) {
	f.includeDisabled = includeDisabled
	return append([]model.FulfillmentShop(nil), f.items...), f.err
}

func (f *fakeFulfillmentShopStore) UpsertFulfillmentShop(_ context.Context, platform, shopCode, shopName string, enabled bool, actor string) (model.FulfillmentShop, bool, error) {
	f.upserted = model.FulfillmentShop{Platform: platform, ShopCode: shopCode, ShopName: shopName, Enabled: enabled}
	f.actor = actor
	return f.upserted, f.created, f.err
}

func (f *fakeFulfillmentShopStore) UpdateFulfillmentShop(_ context.Context, platform, shopCode string, shopName *string, enabled *bool, actor string) (model.FulfillmentShop, error) {
	f.patched = model.FulfillmentShop{Platform: platform, ShopCode: shopCode, ShopName: "Existing", Enabled: true}
	if shopName != nil {
		f.patched.ShopName = *shopName
	}
	if enabled != nil {
		f.patched.Enabled = *enabled
	}
	f.actor = actor
	return f.patched, f.err
}

func fulfillmentShopTestHandler(source fulfillmentShopStore, user, password string) http.Handler {
	server := &Server{
		fulfillmentShops: source,
		consoleUser:      user,
		consolePassword:  password,
		requestTimeout:   time.Second,
		logger:           slog.Default(),
	}
	mux := http.NewServeMux()
	server.registerFulfillmentShopRoutes(mux)
	return mux
}

func TestFulfillmentShopWritesRequireConsoleCredentials(t *testing.T) {
	handler := fulfillmentShopTestHandler(&fakeFulfillmentShopStore{}, "operator", "secret")
	request := httptest.NewRequest(http.MethodPost, "/v1/fulfillment-shops", strings.NewReader(`{"platform":"temu","shop_code":"new-shop","shop_name":"New Shop"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("unauthorized response = %d headers=%v", response.Code, response.Header())
	}
}

func TestFulfillmentShopWritesFailClosedWithoutConfiguredCredentials(t *testing.T) {
	handler := fulfillmentShopTestHandler(&fakeFulfillmentShopStore{}, "", "")
	request := httptest.NewRequest(http.MethodPost, "/v1/fulfillment-shops", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured credentials response = %d", response.Code)
	}
}

func TestCreateFulfillmentShop(t *testing.T) {
	source := &fakeFulfillmentShopStore{created: true}
	handler := fulfillmentShopTestHandler(source, "operator", "secret")
	request := httptest.NewRequest(http.MethodPost, "/v1/fulfillment-shops", strings.NewReader(`{"platform":"temu","shop_code":"new-shop","shop_name":"New Shop","enabled":false}`))
	request.SetBasicAuth("operator", "secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create response = %d %s", response.Code, response.Body.String())
	}
	if source.upserted.ShopCode != "new-shop" || source.upserted.Enabled || source.actor != "operator" {
		t.Fatalf("unexpected upsert: %#v actor=%q", source.upserted, source.actor)
	}
}

func TestPatchFulfillmentShop(t *testing.T) {
	source := &fakeFulfillmentShopStore{}
	handler := fulfillmentShopTestHandler(source, "operator", "secret")
	request := httptest.NewRequest(http.MethodPatch, "/v1/fulfillment-shops/temu/new-shop", strings.NewReader(`{"shop_name":"Renamed","enabled":false}`))
	request.SetBasicAuth("operator", "secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("patch response = %d %s", response.Code, response.Body.String())
	}
	if source.patched.Platform != "temu" || source.patched.ShopCode != "new-shop" || source.patched.ShopName != "Renamed" || source.patched.Enabled || source.actor != "operator" {
		t.Fatalf("unexpected patch: %#v actor=%q", source.patched, source.actor)
	}
}

func TestPatchFulfillmentShopReturnsNotFound(t *testing.T) {
	source := &fakeFulfillmentShopStore{err: store.ErrFulfillmentShopNotFound}
	handler := fulfillmentShopTestHandler(source, "operator", "secret")
	request := httptest.NewRequest(http.MethodPatch, "/v1/fulfillment-shops/temu/missing", strings.NewReader(`{"enabled":false}`))
	request.SetBasicAuth("operator", "secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("not found response = %d %s", response.Code, response.Body.String())
	}
}

func TestPatchFulfillmentShopRequiresAChange(t *testing.T) {
	handler := fulfillmentShopTestHandler(&fakeFulfillmentShopStore{}, "operator", "secret")
	request := httptest.NewRequest(http.MethodPatch, "/v1/fulfillment-shops/temu/existing", strings.NewReader(`{}`))
	request.SetBasicAuth("operator", "secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "shop_name or enabled is required") {
		t.Fatalf("empty patch response = %d %s", response.Code, response.Body.String())
	}
}

func TestListFulfillmentShopsCanIncludeDisabled(t *testing.T) {
	source := &fakeFulfillmentShopStore{items: []model.FulfillmentShop{{Platform: "temu", ShopCode: "disabled", ShopName: "Disabled", Enabled: false}}}
	handler := fulfillmentShopTestHandler(source, "operator", "secret")
	request := httptest.NewRequest(http.MethodGet, "/v1/fulfillment-shops?include_disabled=true", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !source.includeDisabled || !strings.Contains(response.Body.String(), `"shop_code":"disabled"`) {
		t.Fatalf("list response = %d include_disabled=%v body=%s", response.Code, source.includeDisabled, response.Body.String())
	}
}
