package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"xlwms-api-manager/internal/model"
)

func TestRestrictedWarehouseCreateRejectsCarrierBeforeUpstream(t *testing.T) {
	for _, carrier := range []string{"SPEEDX", "YANWEN", "SWIFTX", "CBS", "_AUTO_MATCH_", "other", "unknown"} {
		source := &stubWarehouseCredentialSource{}
		body := strings.ReplaceAll(validParcelCreateBody("ARP06A", "ARP06A"), "CHANNEL-1", carrier)
		response := httptest.NewRecorder()
		parcelHandler(source).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/outbound/parcel-create", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest || len(source.requested) != 0 {
			t.Fatalf("carrier %s reached credential/upstream path: %d", carrier, response.Code)
		}
	}
}

func TestPurchasedLabelApprovalRequiresActualCollectionCarrier(t *testing.T) {
	for _, carrier := range []string{"USPS", "GOFO", "UPS", "FEDEX", "SPEEDX", "YANWEN", "CBS", "", "_AUTO_MATCH_"} {
		audit := model.FulfillmentAudit{WarehouseKey: "ARP_HOUSTON", WarehouseCode: "ARP06A", CarrierCode: carrier}
		_, reason := purchasedLabelWarehouse([]model.FulfillmentAudit{audit})
		allowed := carrier == "USPS" || carrier == "GOFO" || carrier == "UPS" || carrier == "FEDEX"
		if (reason == "") != allowed {
			t.Fatalf("carrier %q approval reason=%q", carrier, reason)
		}
	}
}
