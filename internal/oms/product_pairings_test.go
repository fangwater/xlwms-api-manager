package oms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProductPairingsUseVerifiedWebContractsAndReuseSession(t *testing.T) {
	var logins atomic.Int32
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, request.URL.Path)
		if got := request.Header.Get("Track-Key"); got == "" || !strings.HasPrefix(got, "v2:") {
			t.Fatalf("missing Track-Key: %q", got)
		}
		switch request.URL.Path {
		case "/gateway/woms/auth/login":
			logins.Add(1)
			writeOMSJSON(writer, apiEnvelope[loginData]{Code: http.StatusOK, Data: loginData{Token: "pairing-token"}})
		case productPairingListPath:
			assertPairingAuthorization(t, request)
			want := map[string]any{
				"current": float64(2), "size": float64(40), "storeCodes": "TEMU-US",
				"orderType": "platformSku", "orderNo": "20PCS",
			}
			if !reflect.DeepEqual(body, want) {
				t.Fatalf("list payload = %#v, want %#v", body, want)
			}
			writeOMSJSON(writer, map[string]any{"code": 200, "data": map[string]any{
				"records": []any{map[string]any{
					"id": "81", "platformSku": "20PCS", "storeCode": "TEMU-US", "storeName": "Temu US",
					"skuList":    []any{map[string]any{"sku": "10PCS", "qty": "2", "productName": "10 pack", "skuApproveStatus": "1"}},
					"createTime": "2026-08-27 10:20:30",
				}},
				"total": 41, "size": 40, "current": 2, "pages": 2,
			}})
		case productPairingCreatePath:
			assertPairingAuthorization(t, request)
			want := map[string]any{
				"storeCode": "TEMU-US", "platformSku": "20PCS",
				"skuList": []any{map[string]any{"sku": "10PCS", "qty": float64(2)}},
			}
			if !reflect.DeepEqual(body, want) {
				t.Fatalf("create payload = %#v, want %#v", body, want)
			}
			writeOMSJSON(writer, map[string]any{"code": 200, "data": nil})
		case productPairingDeletePath:
			assertPairingAuthorization(t, request)
			want := map[string]any{"optList": []any{map[string]any{"platformSku": "20PCS", "storeCode": "TEMU-US"}}}
			if !reflect.DeepEqual(body, want) {
				t.Fatalf("delete payload = %#v, want %#v", body, want)
			}
			writeOMSJSON(writer, map[string]any{"code": 200, "data": nil})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "demo-user", "demo-password", time.Second)
	page, err := client.ProductPairings(context.Background(), ProductPairingFilter{
		Page: 2, PageSize: 40, StoreCode: " TEMU-US ", Query: " 20PCS ", QueryField: "platform_sku",
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 41 || page.Pages != 2 || page.Page != 2 || len(page.Records) != 1 {
		t.Fatalf("unexpected page: %#v", page)
	}
	got := page.Records[0]
	if got.ID != "81" || got.PlatformSKU != "20PCS" || got.StoreCode != "TEMU-US" || len(got.Items) != 1 ||
		got.Items[0].SystemSKU != "10PCS" || got.Items[0].Quantity != 2 || got.Items[0].ApproveStatus != 1 {
		t.Fatalf("unexpected pairing: %#v", got)
	}
	input := ProductPairingInput{
		StoreCode: " TEMU-US ", PlatformSKU: " 20PCS ",
		Items: []ProductPairingItem{{SystemSKU: " 10PCS ", Quantity: 2}},
	}
	if err := client.CreateProductPairing(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteProductPairing(context.Background(), " TEMU-US ", " 20PCS "); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 1 {
		t.Fatalf("login count = %d, want 1", logins.Load())
	}
	wantPaths := []string{"/gateway/woms/auth/login", productPairingListPath, productPairingCreatePath, productPairingDeletePath}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("paths = %#v, want %#v", paths, wantPaths)
	}
}

func TestNormalizeProductPairingInputRejectsDuplicateAndInvalidQuantity(t *testing.T) {
	_, err := NormalizeProductPairingInput(ProductPairingInput{
		StoreCode: "STORE", PlatformSKU: "20PCS",
		Items: []ProductPairingItem{{SystemSKU: "10PCS", Quantity: 2}, {SystemSKU: " 10PCS ", Quantity: 1}},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate system SKU") {
		t.Fatalf("expected duplicate SKU error, got %v", err)
	}
	_, err = NormalizeProductPairingInput(ProductPairingInput{
		StoreCode: "STORE", PlatformSKU: "20PCS",
		Items: []ProductPairingItem{{SystemSKU: "10PCS", Quantity: 0}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid quantity") {
		t.Fatalf("expected quantity error, got %v", err)
	}
}

func TestMatchProductPairingRecipeRequiresExactApprovedRecord(t *testing.T) {
	records := []ProductPairing{
		{PlatformSKU: "OTHER", Items: []ProductPairingItem{{SystemSKU: "SKU-10", Quantity: 2, ApproveStatus: 2}}},
		{PlatformSKU: "SKU-20", Items: []ProductPairingItem{{SystemSKU: "SKU-10", Quantity: 1, ApproveStatus: 2}}},
		{PlatformSKU: "SKU-20", Items: []ProductPairingItem{{SystemSKU: "SKU-10", Quantity: 2, ApproveStatus: 1}}},
		{PlatformSKU: "SKU-20", Items: []ProductPairingItem{{SystemSKU: "SKU-10", Quantity: 2, ApproveStatus: 2}}},
	}
	match, err := MatchProductPairingRecipe(" SKU-20 ", []ProductPairingItem{{SystemSKU: " SKU-10 ", Quantity: 2}}, records)
	if err != nil {
		t.Fatal(err)
	}
	if match.ExactPlatformRecords != 3 || match.MatchingRecipeRecords != 2 || match.ApprovedRecords != 1 {
		t.Fatalf("unexpected match: %#v", match)
	}
}

func assertPairingAuthorization(t *testing.T, request *http.Request) {
	t.Helper()
	if got := request.Header.Get("Authorization"); got != "Bearer pairing-token" {
		t.Fatalf("authorization = %q", got)
	}
	if got := request.Header.Get("Referer"); got != "http://"+request.Host+"/platform/product/list" {
		t.Fatalf("referer = %q", got)
	}
}
