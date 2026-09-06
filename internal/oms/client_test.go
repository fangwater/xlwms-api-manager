package oms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	if !errors.Is(err, ErrPasswordUpdateRequired) {
		t.Fatalf("CheckAccess error = %v, want ErrPasswordUpdateRequired", err)
	}
	if got := PublicAuthError(err); got != "请更新登录密码" {
		t.Fatalf("PublicAuthError = %q", got)
	}
	if got := AuthErrorMessage(errors.New("query OMS pending platform orders: timeout")); got != "" {
		t.Fatalf("non-auth error should stay empty, got %q", got)
	}
}

func TestCheckAccessReportsMFAVerificationSeparatelyFromPasswordUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/gateway/woms/auth/login" {
			http.NotFound(writer, request)
			return
		}
		writeOMSJSON(writer, apiEnvelope[loginData]{
			Code: 4011, Msg: "需要短信/邮箱二次验证",
			Data: loginData{LoginAction: "NEED_MFA_VERIFY", NeedVerify: true, ChallengeID: "challenge"},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "operator", "password", time.Second)
	err := client.CheckAccess(context.Background())
	if !errors.Is(err, ErrMFAVerificationRequired) || errors.Is(err, ErrPasswordUpdateRequired) {
		t.Fatalf("CheckAccess error = %v, want MFA verification only", err)
	}
	if got := PublicAuthError(err); got != "需要短信、邮箱或验证器二次验证" {
		t.Fatalf("PublicAuthError = %q", got)
	}
}

func TestMFAVerificationUsesOfficialChallengeFlowAndCachesToken(t *testing.T) {
	var loginCount atomic.Int32
	var originalFlow, originalFingerprint string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		switch request.URL.Path {
		case "/gateway/woms/auth/login":
			loginCount.Add(1)
			flow, _ := payload["loginFlowId"].(string)
			fingerprint, _ := payload["deviceFingerprint"].(string)
			if loginCount.Load() == 1 {
				originalFlow, originalFingerprint = flow, fingerprint
				http.SetCookie(writer, &http.Cookie{Name: "login-session", Value: "test-session", Path: "/"})
				writeOMSJSON(writer, apiEnvelope[loginData]{Code: 4011, Msg: "需要短信/邮箱二次验证", Data: loginData{
					LoginAction: "NEED_MFA_VERIFY", NeedVerify: true, ChallengeID: "challenge-1",
					MFAChannel: "EMAIL", MFAMaskedTarget: "m***@example.com",
				}})
				return
			}
			if flow != originalFlow || fingerprint != originalFingerprint {
				t.Fatalf("replayed login changed flow or fingerprint")
			}
			if cookie, err := request.Cookie("verified-session"); err != nil || cookie.Value != "test-verified" {
				t.Error("replayed login did not retain verification cookie")
			}
			writeOMSJSON(writer, apiEnvelope[loginData]{Code: http.StatusOK, Data: loginData{Token: "verified-token"}})
		case mfaSendCodePath:
			if cookie, err := request.Cookie("login-session"); err != nil || cookie.Value != "test-session" {
				t.Error("send-code request did not retain login cookie")
			}
			if payload["challengeId"] != "challenge-1" || payload["channel"] != "EMAIL" || payload["language"] != "zh" {
				t.Fatalf("unexpected send-code payload: %#v", payload)
			}
			writeOMSJSON(writer, apiEnvelope[any]{Code: http.StatusOK})
		case mfaVerifyPath:
			if cookie, err := request.Cookie("login-session"); err != nil || cookie.Value != "test-session" {
				t.Error("verification request did not retain login cookie")
			}
			http.SetCookie(writer, &http.Cookie{Name: "verified-session", Value: "test-verified", Path: "/"})
			if payload["challengeId"] != "challenge-1" || payload["verifyCode"] != "123456" {
				t.Fatalf("unexpected verification payload: %#v", payload)
			}
			writeOMSJSON(writer, apiEnvelope[any]{Code: http.StatusOK})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "operator", "password", time.Second)
	prompt, err := client.BeginMFAVerification(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Channel != "EMAIL" || prompt.MaskedTarget != "m***@example.com" || !prompt.CodeSent || prompt.CodeLength != 6 {
		t.Fatalf("prompt = %#v", prompt)
	}
	if err := client.CheckAccess(context.Background()); !errors.Is(err, ErrMFAVerificationRequired) {
		t.Fatalf("pending challenge status = %v", err)
	}
	if loginCount.Load() != 1 {
		t.Fatal("account refresh started a new login during verification")
	}
	if err := client.CompleteMFAVerification(context.Background(), "123456"); err != nil {
		t.Fatal(err)
	}
	if err := client.CheckAccess(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loginCount.Load() != 2 {
		t.Fatalf("login count = %d, want 2", loginCount.Load())
	}
	if _, err := client.BeginMFAVerification(context.Background()); !errors.Is(err, ErrMFANotRequired) {
		t.Fatalf("verified account challenge = %v", err)
	}
	if loginCount.Load() != 2 {
		t.Fatal("verified account started another login")
	}
}

func TestLoginAcceptsSuccessfulTokenWithMFASetupAdvisory(t *testing.T) {
	for _, code := range []int{http.StatusOK, 4011} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeOMSJSON(w, apiEnvelope[loginData]{Code: code, Data: loginData{Token: "test-token", NeedSetupMFA: true}})
			}))
			defer server.Close()
			err := NewClient(server.URL, "operator", "password", time.Second).CheckAccess(context.Background())
			if (err == nil) != (code == http.StatusOK) {
				t.Fatalf("login success = %v, response code = %d", err == nil, code)
			}
		})
	}
}

func TestMFAVerificationRejectsMalformedCode(t *testing.T) {
	client := NewClient("https://example.invalid", "operator", "password", time.Second)
	for _, code := range []string{"12345", "12345x", "1234567"} {
		if err := client.CompleteMFAVerification(context.Background(), code); !errors.Is(err, ErrInvalidMFACode) {
			t.Fatalf("code %q error = %v", code, err)
		}
	}
}

func TestTOTPRequiresActivationBeforeVerification(t *testing.T) {
	for _, activationCode := range []int{200, 4013} {
		t.Run(fmt.Sprint(activationCode), func(t *testing.T) {
			activated, verified := false, false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/gateway/woms/auth/login":
					if verified {
						writeOMSJSON(w, apiEnvelope[loginData]{Code: 200, Data: loginData{Token: "test-token"}})
						return
					}
					writeOMSJSON(w, apiEnvelope[loginData]{Code: 4011, Data: loginData{
						LoginAction: "NEED_MFA_VERIFY", NeedVerify: true, ChallengeID: "totp-challenge",
						AvailableTargets: []mfaTarget{{Channel: "TOTP"}},
					}})
				case mfaSendCodePath:
					var payload mfaSendCodePayload
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					if payload.ChallengeID != "totp-challenge" || payload.Channel != "TOTP" || payload.Language != "zh" {
						t.Error("incorrect TOTP activation payload")
					}
					activated = activationCode == 200
					writeOMSJSON(w, apiEnvelope[any]{Code: activationCode})
				case mfaVerifyPath:
					if !activated {
						t.Error("TOTP submitted before activation")
						writeOMSJSON(w, apiEnvelope[any]{Code: 4013})
						return
					}
					verified = true
					writeOMSJSON(w, apiEnvelope[any]{Code: 200})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := NewClient(server.URL, "operator", "password", time.Second)
			prompt, err := client.BeginMFAVerification(context.Background())
			if activationCode != 200 {
				var rejected *MFARequestError
				if !errors.As(err, &rejected) || client.mfa != nil {
					t.Fatalf("failed activation left an active challenge: %v", err)
				}
				return
			}
			if err != nil || !activated || prompt.Channel != "TOTP" || prompt.CodeSent {
				t.Fatalf("TOTP activation failed: %v", err)
			}
			if err := client.CompleteMFAVerification(context.Background(), "123456"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMFARejectionsPreserveReasonAndExpireChallenge(t *testing.T) {
	for _, test := range []struct {
		name     string
		code     int
		exceeded bool
		cleared  bool
		message  string
	}{
		{"incorrect", 4012, false, false, "验证码校验未通过"},
		{"exhausted", 4012, true, true, "验证码尝试次数已用完"},
		{"expired", 4013, false, true, "验证码会话已失效"},
		{"upstream", 500, false, false, "错误码 500"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeOMSJSON(w, apiEnvelope[any]{Code: test.code, Msg: "private upstream message", Data: map[string]bool{"attemptsExceeded": test.exceeded}})
			}))
			defer server.Close()
			client := NewClient(server.URL, "operator", "password", time.Second)
			client.mfa = &mfaVerificationSession{challengeID: "test-challenge"}
			err := client.CompleteMFAVerification(context.Background(), "123456")
			var upstream *MFARequestError
			if !errors.As(err, &upstream) || upstream.Code != test.code {
				t.Fatalf("unexpected error: %v", err)
			}
			if (client.mfa == nil) != test.cleared {
				t.Fatalf("challenge cleared = %v", client.mfa == nil)
			}
			if got := PublicAuthError(err); !strings.Contains(got, test.message) || strings.Contains(got, "private") {
				t.Fatalf("public error = %q", got)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatal("diagnostic error exposed upstream message")
			}
		})
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
