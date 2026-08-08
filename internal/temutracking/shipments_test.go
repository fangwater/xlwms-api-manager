package temutracking

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPurchasedShipmentsByPlatformOrderNosQueriesEveryShop(t *testing.T) {
	t.Parallel()
	requested := map[string][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/temu/api/system/shops":
			_, _ = writer.Write([]byte(`{"success":true,"data":{"shops":[{"code":"shop-a","name":"Shop A"},{"code":"shop-b","name":"Shop B"}]}}`))
		case "/temu/api/shipments/lookup":
			var input struct {
				ParentOrderSNs []string `json:"parent_order_sns"`
			}
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			shop := request.Header.Get("X-Temu-Shop")
			requested[shop] = input.ParentOrderSNs
			if shop == "shop-b" {
				_, _ = writer.Write([]byte(`{"success":true,"data":[{"parent_order_sn":"PO-2","status":"shipped","oms_warehouse_key":"DPS004","oms_warehouse_code":"DPSCA004","tracking_number":"TRACK-2"}]}`))
				return
			}
			_, _ = writer.Write([]byte(`{"success":true,"data":[]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	shipments, err := NewClient(server.URL+"/temu", time.Second).PurchasedShipmentsByPlatformOrderNos(
		context.Background(), []string{"PO-1", "PO-2"},
	)
	if err != nil {
		t.Fatal(err)
	}
	shipment, found := shipments["PO-2"]
	if !found || shipment.StoreCode != "shop-b" || shipment.OMSWarehouseCode != "DPSCA004" {
		t.Fatalf("unexpected shipments: %#v", shipments)
	}
	if len(requested) != 2 || len(requested["shop-a"]) != 2 || len(requested["shop-b"]) != 2 {
		t.Fatalf("requested shops = %#v", requested)
	}
}

func TestPurchasedShipmentsByPlatformOrderNosRejectsCrossShopDuplicate(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/system/shops" {
			_, _ = writer.Write([]byte(`{"success":true,"data":{"shops":[{"code":"a"},{"code":"b"}]}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"success":true,"data":[{"parent_order_sn":"PO-1","status":"shipped","oms_warehouse_key":"WEST","oms_warehouse_code":"WH-WEST"}]}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, time.Second).PurchasedShipmentsByPlatformOrderNos(context.Background(), []string{"PO-1"})
	if err == nil {
		t.Fatal("expected duplicate shipment error")
	}
}
