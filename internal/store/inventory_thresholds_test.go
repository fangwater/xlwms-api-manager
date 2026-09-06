package store

import (
	"errors"
	"strings"
	"testing"
)

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

func TestRuntimeFulfillmentShopSeedMatchesTemuShops(t *testing.T) {
	for _, fragment := range []string{
		"('temu', 'panda-homes', 'PANDA HOMES')",
		"('temu', 'panda-buy', 'PANDA BUY')",
		"('temu', 'hans-living', 'Hans Living')",
		"('temu', 'woven-whispers', 'WovenWhispers')",
	} {
		if !strings.Contains(fulfillmentShopSeedSQL, fragment) {
			t.Fatalf("runtime fulfillment shop seed missing %q", fragment)
		}
	}
	if !strings.Contains(fulfillmentShopSeedSQL, "ON CONFLICT (platform, shop_code) DO NOTHING") {
		t.Fatal("runtime seed must not overwrite API-managed shop settings")
	}
}

func TestNormalizeFulfillmentShopMutation(t *testing.T) {
	platform, shopCode, shopName, actor, err := normalizeFulfillmentShopMutation(" TEMU ", " New-Shop ", " New Shop ", " operator ")
	if err != nil {
		t.Fatal(err)
	}
	if platform != "temu" || shopCode != "new-shop" || shopName != "New Shop" || actor != "operator" {
		t.Fatalf("unexpected normalized shop: %q/%q %q actor=%q", platform, shopCode, shopName, actor)
	}
	if _, _, _, _, err := normalizeFulfillmentShopMutation("temu", "bad_shop", "Bad", "operator"); !errors.Is(err, ErrInvalidFulfillmentShop) {
		t.Fatalf("invalid shop code error = %v", err)
	}
	if _, _, _, _, err := normalizeFulfillmentShopMutation("temu", "valid-shop", " ", "operator"); !errors.Is(err, ErrInvalidFulfillmentShop) {
		t.Fatalf("empty shop name error = %v", err)
	}
}
