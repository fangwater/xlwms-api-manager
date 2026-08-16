package store

import "testing"

func TestNormalizeFulfillmentShopIdentity(t *testing.T) {
	platform, err := NormalizeFulfillmentPlatform(" TEMU ")
	if err != nil || platform != "temu" {
		t.Fatalf("platform = %q err=%v", platform, err)
	}
	shop, err := NormalizeFulfillmentShopCode(" Panda-Homes ")
	if err != nil || shop != "panda-homes" {
		t.Fatalf("shop = %q err=%v", shop, err)
	}
	if _, err := NormalizeFulfillmentPlatform("amazon"); err == nil {
		t.Fatal("unknown platform must fail")
	}
	if _, err := NormalizeFulfillmentShopCode("Panda_Homes"); err == nil {
		t.Fatal("invalid shop code must fail")
	}
}
