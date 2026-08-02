package xlwms

import "testing"

func TestOutboundPathsCoverOfficialEndpoints(t *testing.T) {
	want := map[string]string{
		"parcel-create": "/v1/outboundOrder/create", "parcel-list": "/v1/outboundOrder/pageList",
		"parcel-detail": "/v1/outboundOrder/detail", "parcel-cancel": "/v1/outboundOrder/cancel",
		"cancel-status": "/v1/outboundOrder/selectBizStatus", "bulk-product-create": "/v1/outboundOrder/big/create",
		"bulk-list": "/v1/outboundOrder/big/pageList", "bulk-detail": "/v1/outboundOrder/big/detail",
		"bulk-cancel": "/v1/outboundOrder/big/cancel", "tracking-label-update": "/v1/outboundOrder/updateTrackNoAndLabel",
		"bulk-box-create": "/v1/outboundOrder/big/bulkBox/create", "message-detail": "/v1/outboundOrder/big/messageBoard/detail",
		"message-reply": "/v1/outboundOrder/big/messageBoard/reply",
	}
	for operation, path := range want {
		if OutboundPaths[operation] != path {
			t.Fatalf("%s path is %q, want %q", operation, OutboundPaths[operation], path)
		}
	}
}

func TestOutboundValidation(t *testing.T) {
	emptyProducts := []any{map[string]any{"whCode": "WH1", "thirdOrderNo": "T1", "subOrderType": 1, "logisticsChannel": "L1", "receiver": "R", "countryRegionCode": "US", "provinceCode": "CA", "provinceName": "California", "cityName": "LA", "postCode": "90001", "addressOne": "A", "productList": []any{}}}
	if err := ValidateOutboundData("parcel-create", emptyProducts); err == nil {
		t.Fatal("empty productList must fail")
	}
	if err := ValidateOutboundData("parcel-list", map[string]any{"page": 1, "pageSize": 100}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOutboundData("parcel-list", map[string]any{"page": 1, "pageSize": 101}); err == nil {
		t.Fatal("pageSize above 100 must fail")
	}
	if err := ValidateOutboundData("tracking-label-update", map[string]any{"outboundOrderNo": "O1"}); err == nil {
		t.Fatal("tracking or label must be required")
	}
	if err := ValidateOutboundData("message-reply", map[string]any{"outboundOrderNo": "O1", "content": "done"}); err != nil {
		t.Fatal(err)
	}
	boxOrder := map[string]any{
		"whCode": "WH1", "thirdOrderNo": "T1", "logisticsChannel": "L1", "receiver": "R",
		"countryRegionCode": "US", "cityName": "LA", "postCode": "90001", "addressOne": "A",
		"boxList": []any{map[string]any{"boxTypeNo": "B1", "quantity": 1}},
	}
	if err := ValidateOutboundData("bulk-box-create", []any{boxOrder}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildAuthCodeSupportsArrayData(t *testing.T) {
	_, err := BuildAuthCode("key", "secret", "1", []any{map[string]any{"whCode": "WH1"}})
	if err != nil {
		t.Fatal(err)
	}
}
