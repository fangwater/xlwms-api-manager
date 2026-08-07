package oms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPendingOrdersUsesVerifiedWebContract(t *testing.T) {
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if got := request.Header.Get("Track-Key"); got == "" || !strings.HasPrefix(got, "v2:") {
			t.Fatalf("missing Track-Key: %q", got)
		}
		switch request.URL.Path {
		case "/gateway/woms/auth/login":
			logins.Add(1)
			if request.Header.Get("X-Client-Type") != "web" || request.Header.Get("X-Device-Fingerprint") != body["deviceFingerprint"] {
				t.Fatalf("invalid login headers")
			}
			if body["businessType"] != "oms" || body["loginAccount"] != "demo-user" || body["password"] != "demo-password" {
				t.Fatalf("invalid login payload")
			}
			writeOMSJSON(writer, apiEnvelope[loginData]{Code: 200, Data: loginData{Token: "test-token"}, Msg: "ok"})
		case "/gateway/woms/platform/order/list":
			if request.Header.Get("Authorization") != "Bearer test-token" {
				t.Fatalf("unexpected authorization header")
			}
			if body["status"] != "0" || body["current"] != float64(2) || body["size"] != float64(30) {
				t.Fatalf("invalid list payload: %#v", body)
			}
			writeOMSJSON(writer, apiEnvelope[listData]{Code: 200, Data: listData{
				Records: []PendingOrder{{OrderNo: "OMS-DEMO", PlatformOrderNo: "PO-DEMO", Status: 0}},
				Total:   2222, Size: 30, Current: 2, Pages: 75,
			}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "demo-user", "demo-password", time.Second)
	first, err := client.PendingOrders(context.Background(), 2, 30)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 2222 || len(first.Records) != 1 || first.Records[0].PlatformOrderNo != "PO-DEMO" {
		t.Fatalf("unexpected result: %#v", first)
	}
	if _, err := client.PendingOrders(context.Background(), 2, 30); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 1 {
		t.Fatalf("login count = %d, want 1", logins.Load())
	}
}

func TestLogisticsAssignmentUsesVerifiedWebContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Track-Key"); got == "" || !strings.HasPrefix(got, "v2:") {
			t.Errorf("missing Track-Key: %q", got)
		}
		if request.URL.Path == "/gateway/woms/auth/login" {
			writeOMSJSON(writer, apiEnvelope[loginData]{Code: 200, Data: loginData{Token: "test-token"}})
			return
		}
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected authorization header")
		}
		switch request.URL.Path {
		case "/gateway/woms/warehouse/options":
			if request.Method != http.MethodGet || request.URL.Query().Get("status") != "0" {
				t.Errorf("unexpected warehouse request: %s %s", request.Method, request.URL.RawQuery)
			}
			writeOMSJSON(writer, apiEnvelope[[]WarehouseOption]{Code: 200, Data: []WarehouseOption{{WarehouseCode: "WH-1", WarehouseName: "Test warehouse"}}})
		case "/gateway/woms/logistics/channel/options":
			query := request.URL.Query()
			if request.Method != http.MethodGet || query.Get("whCode") != "WH-1" || query.Get("channelGroupFlag") != "1" || query.Get("lowPriceFlag") != "1" {
				t.Errorf("unexpected channel request: %s %s", request.Method, request.URL.RawQuery)
			}
			writeOMSJSON(writer, apiEnvelope[[]LogisticsChannelOption]{Code: 200, Data: []LogisticsChannelOption{{
				LogisticsChannel: PlatformLabelChannelCode, LogisticsChannelName: "Upload label",
				ChannelType: 3, GetSheetType: 1,
			}}})
		case "/gateway/woms/platform/order/list":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode lookup body: %v", err)
				return
			}
			platformOrderNo, _ := body["platformOrderNo"].(string)
			if body["status"] != "0" || body["current"] != float64(1) || body["size"] != float64(100) || platformOrderNo == "" {
				t.Errorf("unexpected pending lookup payload: %#v", body)
			}
			writeOMSJSON(writer, apiEnvelope[listData]{Code: 200, Data: listData{
				Records: []PendingOrder{{OrderNo: "OMS-" + platformOrderNo, PlatformOrderNo: platformOrderNo, Status: 0}},
				Total:   1, Size: 100, Current: 1, Pages: 1,
			}})
		case "/gateway/woms/platform/order/batchAllotWarehouse":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode assignment body: %v", err)
				return
			}
			if body["whCode"] != "WH-1" || body["logisticsChannelCode"] != PlatformLabelChannelCode ||
				body["logisticsChannelName"] != "Upload label" || body["logisticsCarrier"] != OtherCarrierValue ||
				body["channelGroupFlag"] != float64(0) || body["hasApprove"] != float64(1) {
				t.Errorf("unexpected assignment payload: %#v", body)
			}
			if _, exists := body["auditingFlag"]; exists {
				t.Errorf("batch fetch-only payload must not contain auditingFlag")
			}
			writeOMSJSON(writer, map[string]any{
				"code": 200,
				"data": map[string]any{
					"totalQuantity": "2", "successQuantity": "2", "failQuantity": "0", "failList": []any{},
				},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "demo-user", "demo-password", time.Second)
	warehouses, err := client.WarehouseOptions(context.Background())
	if err != nil || len(warehouses) != 1 || warehouses[0].WarehouseCode != "WH-1" {
		t.Fatalf("unexpected warehouses: %#v, %v", warehouses, err)
	}
	channels, err := client.LogisticsChannels(context.Background(), "WH-1")
	if err != nil || len(channels) != 1 || !channels[0].IsActivePlatformLabelUpload() {
		t.Fatalf("unexpected channels: %#v, %v", channels, err)
	}
	orders, err := client.PendingOrdersByPlatformOrderNos(context.Background(), []string{"PO-A", "PO-B"})
	if err != nil || len(orders) != 2 || orders[0].PlatformOrderNo != "PO-A" || orders[1].PlatformOrderNo != "PO-B" {
		t.Fatalf("unexpected resolved orders: %#v, %v", orders, err)
	}
	result, err := client.AssignAndApprove(context.Background(), AssignmentRequest{
		Orders: []string{"OMS-PO-A", "OMS-PO-B"}, WarehouseCode: "WH-1",
		LogisticsChannelCode: PlatformLabelChannelCode, LogisticsChannelName: "Upload label",
		LogisticsCarrier: OtherCarrierValue,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalQuantity != 2 || result.SuccessQuantity != 2 || result.FailQuantity != 0 {
		t.Fatalf("unexpected assignment result: %#v", result)
	}
}

func TestFlexibleIntAcceptsEmptyString(t *testing.T) {
	var option LogisticsChannelOption
	if err := json.Unmarshal([]byte(`{"channelType":1,"getSheetType":""}`), &option); err != nil {
		t.Fatal(err)
	}
	if option.ChannelType != 1 || option.GetSheetType != -1 {
		t.Fatalf("unexpected flexible values: %#v", option)
	}
}

func TestDeviceFingerprintMatchesOfficialAlgorithm(t *testing.T) {
	if got := deviceFingerprint(); got != "35b8f91d" {
		t.Fatalf("fingerprint = %q", got)
	}
	if got := buildTrackKey(http.MethodPost, []byte(`{"a":1}`)); got != "v2:gm5mfmQpnQK6xIO9KB7KvFgsior5LrUhQVNBXYLHAB8=" {
		t.Fatalf("Track-Key = %q", got)
	}
}

func writeOMSJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}
