package sheinfulfillment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPurchasedSheinLabelsByPlatformOrderNosQueriesEveryShop(t *testing.T) {
	t.Parallel()
	requested := map[string][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/system/shops":
			_, _ = writer.Write([]byte(`{"success":true,"data":{"shops":[{"code":"shop-a","name":"Shop A"},{"code":"shop-b","name":"Shop B"}]}}`))
		case "/api/label-purchases/lookup":
			var input struct {
				PlatformOrderNos []string `json:"platform_order_nos"`
			}
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			shopCode := request.Header.Get("X-Shein-Shop")
			requested[shopCode] = input.PlatformOrderNos
			if shopCode == "shop-b" {
				_, _ = writer.Write([]byte(`{"success":true,"data":[{"platform_order_no":"GSU-2","oms_warehouse_key":"HYTX30","oms_warehouse_code":"HYTX30","tracking_number":"GU-2"}]}`))
				return
			}
			_, _ = writer.Write([]byte(`{"success":true,"data":[]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	labels, err := NewClient(server.URL, time.Second).PurchasedSheinLabelsByPlatformOrderNos(context.Background(), []string{"GSU-1", "GSU-2"})
	if err != nil {
		t.Fatal(err)
	}
	label, found := labels["GSU-2"]
	if !found || label.ShopCode != "shop-b" || label.OMSWarehouseCode != "HYTX30" {
		t.Fatalf("unexpected labels: %#v", labels)
	}
	if len(requested) != 2 || len(requested["shop-a"]) != 2 || len(requested["shop-b"]) != 2 {
		t.Fatalf("requested shops = %#v", requested)
	}
}

func TestPurchasedSheinLabelsByPlatformOrderNosRejectsCrossShopDuplicate(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/system/shops" {
			_, _ = writer.Write([]byte(`{"success":true,"data":{"shops":[{"code":"a"},{"code":"b"}]}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"success":true,"data":[{"platform_order_no":"GSU-1","oms_warehouse_key":"HYTX30","oms_warehouse_code":"HYTX30"}]}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, time.Second).PurchasedSheinLabelsByPlatformOrderNos(context.Background(), []string{"GSU-1"})
	if err == nil {
		t.Fatal("expected duplicate purchased-label error")
	}
}
