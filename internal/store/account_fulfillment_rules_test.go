package store

import (
	"slices"
	"testing"
	"xlwms-api-manager/internal/model"
)

func TestAccountCarrierRestrictions(t *testing.T) {
	rules := AccountFulfillmentRules{SelectionMode: "gofo_over_swiftx_speedx", MaxPriceDelta: .3, CarrierPriority: []string{"GOFO", "SWIFTX", "SPEEDX", "UPS", "USPS", "FEDEX", "YANWEN"}, BlockedCarriers: map[string][]string{"ARP_EAST": {"SWIFTX", "UNIUNI", "YANWEN"}, "ARP_WEST": {"YANWEN"}}}
	groups := []model.WarehouseCarrierPolicies{}
	for _, key := range []string{"ARP_EAST", "ARP_WEST"} {
		group := model.WarehouseCarrierPolicies{WarehouseKey: key, BaseRules: model.WarehouseCarrierRules{AllowedCarrierCodes: append([]string{}, KnownAutomaticCarrierCodes...)}}
		for i, code := range SupportedAutomaticCarrierCodes {
			group.Carriers = append(group.Carriers, model.CarrierPolicy{CarrierCode: code, Priority: i + 1, Enabled: true})
		}
		groups = append(groups, group)
	}
	applyAccountCarrierRules(groups, rules)
	for _, group := range groups {
		if group.Source != "oms_account" || group.BaseRules.MaxPriceDelta != .3 {
			t.Fatal("account policy not applied")
		}
		for _, code := range rules.BlockedCarriers[group.WarehouseKey] {
			if slices.Contains(group.BaseRules.AllowedCarrierCodes, code) {
				t.Fatal("blocked carrier allowed")
			}
			for _, carrier := range group.Carriers {
				if carrier.CarrierCode == code && carrier.Enabled {
					t.Fatal("blocked carrier enabled")
				}
			}
		}
		if group.Carriers[3].CarrierCode != "UPS" {
			t.Fatal("incorrect account priority")
		}
	}
}
