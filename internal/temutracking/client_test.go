package temutracking

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOrderTrackingUsesShopHeaderAndOrderEndpoint(t *testing.T) {
	t.Parallel()
	var requestURI string
	var shopHeader string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestURI = request.RequestURI
		shopHeader = request.Header.Get("X-Temu-Shop")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"success":true,"data":{"store_code":"demo-shop","parent_order_sn":"PO 100","packages":[{"packageSn":"PKG-1","trackingNum":"LM-1","trackingInfo":[{"logisticsUpdatedAt":"2026-08-06T08:00:00Z","logisticsStatus":"Last Mile Carrier Picked up","statusText":"picked up"}]}]}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL+"/temu/", time.Second)
	result, err := client.OrderTracking(context.Background(), "demo-shop", "PO 100")
	if err != nil {
		t.Fatalf("OrderTracking() error = %v", err)
	}
	if shopHeader != "demo-shop" {
		t.Fatalf("X-Temu-Shop = %q, want demo-shop", shopHeader)
	}
	if requestURI != "/temu/api/orders/PO%20100/tracking?language=en" {
		t.Fatalf("RequestURI = %q", requestURI)
	}
	if len(result.Packages) != 1 || result.Packages[0].TrackingNum != "LM-1" {
		t.Fatalf("unexpected tracking result: %+v", result)
	}
}

func TestOrderTrackingReturnsSanitizedServiceError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`{"success":false,"error":"upstream unavailable"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, time.Second).OrderTracking(context.Background(), "demo-shop", "PO-1")
	if err == nil || !strings.Contains(err.Error(), "upstream unavailable") {
		t.Fatalf("OrderTracking() error = %v", err)
	}
}
