package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/xlwms"
)

type stubWarehouseCredentialSource struct {
	warehouses    map[string]model.WarehouseCredentials
	requested     []string
	requireActive []bool
}

func (s *stubWarehouseCredentialSource) WarehouseCredentials(_ context.Context, code string, requireActive bool) (model.WarehouseCredentials, error) {
	s.requested = append(s.requested, code)
	s.requireActive = append(s.requireActive, requireActive)
	warehouse, ok := s.warehouses[code]
	if !ok {
		return model.WarehouseCredentials{}, errors.New("unknown warehouse")
	}
	return warehouse, nil
}

type parcelUpstreamExpectation struct {
	appKey    string
	appSecret string
	warehouse string
}

func newParcelUpstream(t *testing.T, expected parcelUpstreamExpectation) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if request.Method != http.MethodPost || request.URL.Path != xlwms.ParcelCreatePath {
			t.Errorf("upstream request = %s %s", request.Method, request.URL.Path)
		}
		var payload struct {
			AppKey  string `json:"appKey"`
			ReqTime string `json:"reqTime"`
			Data    any    `json:"data"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode upstream request: %v", err)
		}
		if payload.AppKey != expected.appKey {
			t.Error("upstream selected the wrong app key")
		}
		authCode, err := xlwms.BuildAuthCode(expected.appKey, expected.appSecret, payload.ReqTime, payload.Data)
		if err != nil {
			t.Errorf("build expected auth code: %v", err)
		}
		if request.URL.Query().Get("authcode") != authCode {
			t.Error("upstream selected the wrong signing credentials")
		}
		orders, ok := payload.Data.([]any)
		if !ok || len(orders) != 1 {
			t.Errorf("upstream data = %#v", payload.Data)
		} else if order, ok := orders[0].(map[string]any); !ok || order["whCode"] != expected.warehouse {
			t.Errorf("upstream order = %#v", orders[0])
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":200,"msg":"ok","data":[{"outboundOrderNo":"OB-1"}]}`))
	}))
	return server, &calls
}

func validParcelCreateBody(credentialWarehouse, orderWarehouse string) string {
	return fmt.Sprintf(`{"warehouse":%q,"data":[{"whCode":%q,"thirdOrderNo":"ORDER-1","subOrderType":1,"logisticsChannel":"CHANNEL-1","receiver":"Test","countryRegionCode":"US","provinceCode":"CA","provinceName":"California","cityName":"Los Angeles","postCode":"90001","addressOne":"Test address","productList":[{"sku":"SKU-1","quantity":1}]}]}`, credentialWarehouse, orderWarehouse)
}

func parcelHandler(credentials warehouseCredentialSource) http.Handler {
	server := &Server{warehouseCredentials: credentials, requestTimeout: time.Second}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/outbound/{operation}", server.outbound)
	return mux
}

func TestParcelCreateSelectsCredentialsAndAPIByWarehouse(t *testing.T) {
	firstAPI, firstCalls := newParcelUpstream(t, parcelUpstreamExpectation{
		appKey: "test-key-a", appSecret: "test-secret-a", warehouse: "WH-A",
	})
	defer firstAPI.Close()
	secondAPI, secondCalls := newParcelUpstream(t, parcelUpstreamExpectation{
		appKey: "test-key-b", appSecret: "test-secret-b", warehouse: "WH-B",
	})
	defer secondAPI.Close()

	credentials := &stubWarehouseCredentialSource{warehouses: map[string]model.WarehouseCredentials{
		"WH-A": {
			WarehouseSummary: model.WarehouseSummary{Code: "WH-A", APIBaseURL: firstAPI.URL},
			AppKey:           "test-key-a", AppSecret: "test-secret-a",
		},
		"WH-B": {
			WarehouseSummary: model.WarehouseSummary{Code: "WH-B", APIBaseURL: secondAPI.URL},
			AppKey:           "test-key-b", AppSecret: "test-secret-b",
		},
	}}
	handler := parcelHandler(credentials)

	for _, warehouse := range []string{"WH-A", "WH-B"} {
		request := httptest.NewRequest(http.MethodPost, "/v1/outbound/parcel-create", strings.NewReader(validParcelCreateBody(warehouse, warehouse)))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("warehouse %s status = %d: %s", warehouse, recorder.Code, recorder.Body.String())
		}
	}

	if *firstCalls != 1 || *secondCalls != 1 {
		t.Fatalf("upstream calls = (%d, %d)", *firstCalls, *secondCalls)
	}
	if fmt.Sprint(credentials.requested) != "[WH-A WH-B]" {
		t.Fatalf("credential selections = %#v", credentials.requested)
	}
	for _, required := range credentials.requireActive {
		if !required {
			t.Fatal("parcel creation must require an active warehouse")
		}
	}
}

func TestParcelCreateRejectsCredentialAndOrderWarehouseMismatch(t *testing.T) {
	credentials := &stubWarehouseCredentialSource{warehouses: map[string]model.WarehouseCredentials{
		"WH-A": {WarehouseSummary: model.WarehouseSummary{Code: "WH-A"}},
	}}
	request := httptest.NewRequest(http.MethodPost, "/v1/outbound/parcel-create", strings.NewReader(validParcelCreateBody("WH-A", "WH-B")))
	recorder := httptest.NewRecorder()

	parcelHandler(credentials).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "does not match") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
