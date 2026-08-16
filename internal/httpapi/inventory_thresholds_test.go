package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestedShopIdentityUsesQueryAndHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/inventory-thresholds?platform=TEMU&shop=Panda-Homes", nil)
	platform, shop, err := requestedShopIdentity(request)
	if err != nil || platform != "temu" || shop != "panda-homes" {
		t.Fatalf("query shop = %s/%s err=%v", platform, shop, err)
	}

	headerRequest := httptest.NewRequest(http.MethodGet, "/v1/inventory-thresholds", nil)
	headerRequest.Header.Set("X-Shein-Shop", "Beauty-Hangers-Home")
	platform, shop, err = requestedShopIdentity(headerRequest)
	if err != nil || platform != "shein" || shop != "beauty-hangers-home" {
		t.Fatalf("header shop = %s/%s err=%v", platform, shop, err)
	}
}

func TestRequestedShopIdentityRejectsConflicts(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/inventory-thresholds?platform=temu&shop=panda-homes", nil)
	request.Header.Set("X-Shein-Shop", "beauty-hangers-home")
	if _, _, err := requestedShopIdentity(request); err == nil {
		t.Fatal("conflicting shop selectors must fail")
	}
}

func TestRequestedDecisionShopAcceptsBodyWhenHeadersMatch(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/temu/warehouse-availability/query", nil)
	request.Header.Set("X-Temu-Shop", "panda-buy")
	platform, shop, err := requestedDecisionShop(request, "temu", "panda-buy")
	if err != nil || platform != "temu" || shop != "panda-buy" {
		t.Fatalf("decision shop = %s/%s err=%v", platform, shop, err)
	}
	if _, _, err := requestedDecisionShop(request, "shein", "beauty-hangers-home"); err == nil {
		t.Fatal("conflicting decision shop must fail")
	}
}
