package oms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckAccessReportsForcedPasswordUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/gateway/woms/auth/login" {
			http.NotFound(writer, request)
			return
		}
		writeOMSJSON(writer, apiEnvelope[loginData]{Code: 4011, Msg: "请更新登录密码"})
	}))
	defer server.Close()
	client := NewClient(server.URL, "arp-user", "old-password", time.Second)
	err := client.CheckAccess(context.Background())
	if err == nil {
		t.Fatal("forced password update was treated as available")
	}
	if got := PublicAuthError(err); got != "请更新登录密码" {
		t.Fatalf("PublicAuthError = %q", got)
	}
	if got := AuthErrorMessage(errors.New("query OMS pending platform orders: timeout")); got != "" {
		t.Fatalf("non-auth error should stay empty, got %q", got)
	}
}

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

func TestPlatformOrdersByPlatformOrderNoUsesAllOrdersSearch(t *testing.T) {
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		switch request.URL.Path {
		case "/gateway/woms/auth/login":
			logins.Add(1)
			writeOMSJSON(writer, apiEnvelope[loginData]{Code: http.StatusOK, Data: loginData{Token: "test-token"}, Msg: "ok"})
		case "/gateway/woms/platform/order/list":
			if request.Header.Get("Authorization") != "Bearer test-token" {
				t.Fatalf("unexpected authorization header")
			}
			if body["status"] != "" || body["platformOrderNo"] != "PO-ALL-1" ||
				body["current"] != float64(1) || body["size"] != float64(100) {
				t.Fatalf("invalid all-orders lookup payload: %#v", body)
			}
			writeOMSJSON(writer, apiEnvelope[listData]{Code: http.StatusOK, Data: listData{
				Records: []PendingOrder{
					{OrderNo: "OMS-PENDING", PlatformOrderNo: "PO-ALL-1", Status: 0, OrderTime: "2026-08-01 01:02:03"},
					{OrderNo: "OMS-PROCESSING", PlatformOrderNo: "po-all-1", Status: 2},
					{OrderNo: "OMS-RELATED", PlatformOrderNo: "PO-ALL-10", Status: 3},
				},
				Total: 3, Size: 100, Current: 1, Pages: 1,
			}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "demo-user", "demo-password", time.Second)
	orders, err := client.PlatformOrdersByPlatformOrderNo(context.Background(), " PO-ALL-1 ")
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 2 || orders[0].OrderTime != "2026-08-01 01:02:03" || orders[1].Status != 2 {
		t.Fatalf("unexpected exact matches: %#v", orders)
	}
	if _, err := client.PlatformOrdersByPlatformOrderNo(context.Background(), "PO-ALL-1"); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 1 {
		t.Fatalf("login count = %d, want 1", logins.Load())
	}
}

func TestPlatformOrdersByPlatformOrderNoLimitsConcurrencyAndStartRate(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32
	var startsMu sync.Mutex
	starts := make([]time.Time, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/gateway/woms/auth/login":
			writeOMSJSON(writer, apiEnvelope[loginData]{Code: http.StatusOK, Data: loginData{Token: "test-token"}})
		case "/gateway/woms/platform/order/list":
			current := active.Add(1)
			for current > maxActive.Load() && !maxActive.CompareAndSwap(maxActive.Load(), current) {
			}
			startsMu.Lock()
			starts = append(starts, time.Now())
			startsMu.Unlock()
			time.Sleep(45 * time.Millisecond)
			active.Add(-1)
			writeOMSJSON(writer, apiEnvelope[listData]{Code: http.StatusOK, Data: listData{Records: []PendingOrder{}}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "demo-user", "demo-password", time.Second)
	client.platformOrderGate = newPlatformOrderQueryGate(2, 25*time.Millisecond)
	var group sync.WaitGroup
	errorsCh := make(chan error, 4)
	for index := 0; index < 4; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := client.PlatformOrdersByPlatformOrderNo(context.Background(), "PO-RATE")
			errorsCh <- err
		}()
	}
	group.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if maxActive.Load() != 2 {
		t.Fatalf("maximum active queries = %d, want 2", maxActive.Load())
	}
	startsMu.Lock()
	defer startsMu.Unlock()
	if len(starts) != 4 {
		t.Fatalf("query starts = %d, want 4", len(starts))
	}
	for index := 1; index < len(starts); index++ {
		if gap := starts[index].Sub(starts[index-1]); gap < 15*time.Millisecond {
			t.Fatalf("query start gap = %s, want at least 15ms", gap)
		}
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

func TestPendingOrderPlatformOrderTypeAcceptsStringAndNumber(t *testing.T) {
	var orders []PendingOrder
	if err := json.Unmarshal([]byte(`[{"platformOrderType":"normal"},{"platformOrderType":2}]`), &orders); err != nil {
		t.Fatal(err)
	}
	if len(orders) != 2 || string(orders[0].PlatformOrderType) != "normal" || string(orders[1].PlatformOrderType) != "2" {
		t.Fatalf("unexpected platform order types: %#v", orders)
	}
	encoded, err := json.Marshal(orders[1])
	if err != nil || !strings.Contains(string(encoded), `"platformOrderType":"2"`) {
		t.Fatalf("encoded order = %s, error = %v", encoded, err)
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
