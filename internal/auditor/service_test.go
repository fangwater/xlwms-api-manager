package auditor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xlwms-api-manager/internal/model"
)

func TestNormalizeOMSStatus(t *testing.T) {
	tests := map[int]string{0: "pending", 1: "pending", 2: "processing", 3: "outbound", 4: "exception", 5: "exception", 6: "exception", 7: "exception", 99: "unknown"}
	for status, expected := range tests {
		if actual := normalizeOMSStatus(status); actual != expected {
			t.Fatalf("status %d: got %q want %q", status, actual, expected)
		}
	}
}

func TestMatchIndexedOutboundRecordsUsesAllReferenceFields(t *testing.T) {
	items := []model.FulfillmentAudit{
		{ID: 1, PlatformOrderNo: "PO-1"},
		{ID: 2, PlatformOrderNo: "PO-2"},
		{ID: 3, PlatformOrderNo: "PO-3", WarehouseCode: "HYTX30"},
	}
	records := []model.OutboundOrderIndex{
		{ThirdOrderNo: "PO-1", OutboundOrderNo: "OB-1"},
		{ReferOrderNo: "PO-2", OutboundOrderNo: "OB-2"},
		{PlatformOrderNo: "PO-3", WarehouseCode: "OTHER", OutboundOrderNo: "OB-WRONG"},
		{PlatformOrderNo: "PO-3", WarehouseCode: "HYTX30", OutboundOrderNo: "OB-3"},
	}
	matches := matchIndexedOutboundRecords(items, records)
	if len(matches) != 3 || matches[2].OutboundOrderNo != "OB-2" || matches[3].OutboundOrderNo != "OB-3" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestParseXLWMSTime(t *testing.T) {
	value := parseXLWMSTime("2026-08-04 17:33:13")
	if value == nil || value.Year() != 2026 || value.Month() != time.August || value.Day() != 4 {
		t.Fatalf("unexpected parsed time: %v", value)
	}
	if parseXLWMSTime("") != nil {
		t.Fatal("empty time must stay nil")
	}
}

func TestUniqueWarehouseCredentialsDeduplicatesSharedAccounts(t *testing.T) {
	credentials := []model.WarehouseCredentials{
		{WarehouseSummary: model.WarehouseSummary{Code: "HYTX30", APIBaseURL: "https://api.example"}, AppKey: "arp"},
		{WarehouseSummary: model.WarehouseSummary{Code: "ARPCA01", APIBaseURL: "https://api.example"}, AppKey: "arp"},
		{WarehouseSummary: model.WarehouseSummary{Code: "DPSNY002", APIBaseURL: "https://api.example"}, AppKey: "dps"},
	}
	unique := uniqueWarehouseCredentials(credentials)
	if len(unique) != 2 || unique[0].Code != "HYTX30" || unique[1].Code != "DPSNY002" {
		t.Fatalf("unexpected unique credentials: %#v", unique)
	}
}

func TestOutboundSyncThroughUsesCompletedHour(t *testing.T) {
	now := time.Date(2026, time.August, 5, 14, 56, 31, 0, time.Local)
	got := outboundSyncThrough(now)
	want := time.Date(2026, time.August, 5, 14, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestOutboundSyncCursorBackfillsOnceThenUsesWatermark(t *testing.T) {
	through := time.Date(2026, time.August, 5, 16, 0, 0, 0, time.Local)
	if got, want := outboundSyncCursor(through, nil), through.Add(-24*time.Hour); !got.Equal(want) {
		t.Fatalf("bootstrap cursor = %v, want %v", got, want)
	}
	watermark := time.Date(2026, time.August, 5, 14, 0, 0, 0, time.Local)
	if got, want := outboundSyncCursor(through, &watermark), watermark.Add(-time.Hour); !got.Equal(want) {
		t.Fatalf("watermark cursor = %v, want %v", got, want)
	}
}

func TestResolutionFromAuditPreservesKnownMatch(t *testing.T) {
	status := 2
	item := model.FulfillmentAudit{
		WarehouseCode: "HYTX30", OMSStatus: "processing", OMSStatusCode: &status,
		OutboundOrderNo: "OB-1", OMSTrackingNumber: "TRACK-1",
	}
	resolution := resolutionFromAudit(item)
	if resolution.OMSStatus != "processing" || resolution.OutboundOrderNo != "OB-1" || resolution.TrackingNumber != "TRACK-1" {
		t.Fatalf("unexpected preserved resolution: %#v", resolution)
	}
}

func TestMergeFulfillmentCandidatesDeduplicatesStatusCandidates(t *testing.T) {
	merged := mergeFulfillmentCandidates(
		[]model.FulfillmentAudit{{ID: 1}, {ID: 2}},
		[]model.FulfillmentAudit{{ID: 2}, {ID: 3}},
	)
	if len(merged) != 3 || merged[0].ID != 1 || merged[2].ID != 3 {
		t.Fatalf("unexpected merged candidates: %#v", merged)
	}
}

func TestFetchOutboundWindowPaginatesAtOfficialPageSize(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		var payload struct {
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Data["pageSize"] != float64(outboundPageSize) || payload.Data["timeType"] != "orderCreateTime" {
			t.Fatalf("unexpected page request: %#v", payload.Data)
		}
		if payload.Data["startTime"] != "2026-08-05 13:00:00" || payload.Data["endTime"] != "2026-08-05 14:00:00" {
			t.Fatalf("unexpected watermark window: %#v", payload.Data)
		}
		records := make([]map[string]any, 0, outboundPageSize)
		if calls == 1 {
			for index := 0; index < outboundPageSize; index++ {
				records = append(records, map[string]any{"outboundOrderNo": "OTHER", "status": 2})
			}
		} else {
			records = append(records, map[string]any{
				"whCode": "HYTX30", "platformOrderNo": "PO-1", "outboundOrderNo": "OB-1", "status": 2,
			})
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"code": 200, "msg": "ok", "data": map[string]any{"records": records},
		})
	}))
	defer server.Close()

	service := &Service{requestTimeout: time.Second}
	records, err := service.fetchOutboundWindow(context.Background(), model.WarehouseCredentials{
		WarehouseSummary: model.WarehouseSummary{Code: "HYTX30", APIBaseURL: server.URL},
		AppKey:           "test", AppSecret: "test",
	}, time.Date(2026, time.August, 5, 13, 0, 0, 0, time.Local), time.Date(2026, time.August, 5, 14, 0, 0, 0, time.Local))
	if err != nil || calls != 2 || len(records) != outboundPageSize+1 || records[len(records)-1].PlatformOrderNo != "PO-1" {
		t.Fatalf("calls=%d records=%#v err=%v", calls, records, err)
	}
}

func TestFetchOutboundStatusesBatchesRealOutboundOrderNumbers(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		var payload struct {
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		value, _ := payload.Data["outboundOrderNos"].(string)
		if value == "" || strings.Contains(value, "PO-") {
			t.Fatalf("status query must contain real outbound order numbers: %#v", payload.Data)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"code": 200, "msg": "ok", "data": map[string]any{"records": []any{
				map[string]any{"outboundOrderNo": strings.Split(value, ",")[0], "status": 3},
			}},
		})
	}))
	defer server.Close()

	orderNos := make([]string, outboundStatusBatchSize+1)
	for index := range orderNos {
		orderNos[index] = fmt.Sprintf("OBS-%03d", index)
	}
	service := &Service{requestTimeout: time.Second}
	records, err := service.fetchOutboundStatuses(context.Background(), model.WarehouseCredentials{
		WarehouseSummary: model.WarehouseSummary{Code: "HYTX30", APIBaseURL: server.URL},
		AppKey:           "test", AppSecret: "test",
	}, orderNos)
	if err != nil || calls != 2 || len(records) != 2 || records[0].Status != 3 {
		t.Fatalf("calls=%d records=%#v err=%v", calls, records, err)
	}
}
