package store

import (
	"testing"
	"xlwms-api-manager/internal/fulfillment"
	"xlwms-api-manager/internal/model"
)

func TestCollectionWarehousePolicyCannotExpandPhysicalCapabilities(t *testing.T) {
	for _, key := range []string{"ARP_HOUSTON", "ARP_ATLANTA"} {
		policies := []model.CarrierPolicy{}
		for i, code := range SupportedAutomaticCarrierCodes {
			policies = append(policies, model.CarrierPolicy{CarrierCode: code, Priority: i + 1, Enabled: fulfillment.CarrierAllowed(key, code)})
		}
		if _, err := ValidateCarrierPolicies(key, policies); err != nil {
			t.Fatal(err)
		}
		for i := range policies {
			if policies[i].CarrierCode == "SPEEDX" {
				policies[i].Enabled = true
			}
		}
		if _, err := ValidateCarrierPolicies(key, policies); err == nil {
			t.Fatal("SPEEDX enable must be rejected")
		}
		rules := model.WarehouseCarrierRules{AllowedCarrierCodes: []string{"USPS", "YANWEN"}, SelectionMode: "lowest_price", WarehouseTiePriority: 1}
		if _, err := ValidateWarehouseCarrierRules(key, rules); err == nil {
			t.Fatal("YANWEN whitelist expansion must be rejected")
		}
	}
}

func TestPhysicalCapabilitiesApplyAfterAccountRulesAndHonorEmptyList(t *testing.T) {
	groups := []model.WarehouseCarrierPolicies{{WarehouseKey: "ARP_HOUSTON", BaseRules: model.WarehouseCarrierRules{AllowedCarrierCodes: []string{"GOFO", "SPEEDX", "CBS"}}, Carriers: []model.CarrierPolicy{{CarrierCode: "GOFO", Enabled: true}, {CarrierCode: "SPEEDX", Enabled: true}, {CarrierCode: "CBS", Enabled: true}}}}
	applyAccountCarrierRules(groups, AccountFulfillmentRules{BlockedCarriers: map[string][]string{}})
	applyPhysicalWarehouseCapabilities(groups, []fulfillment.Warehouse{{Key: "ARP_HOUSTON", Enabled: true, AllowedCarrierCodes: []string{"USPS", "GOFO", "UPS", "FEDEX"}}})
	if len(groups[0].BaseRules.AllowedCarrierCodes) != 1 || groups[0].BaseRules.AllowedCarrierCodes[0] != "GOFO" || groups[0].Carriers[1].Enabled || groups[0].Carriers[2].Enabled {
		t.Fatal("account rules expanded physical capability")
	}
	applyPhysicalWarehouseCapabilities(groups, []fulfillment.Warehouse{{Key: "ARP_HOUSTON", AllowedCarrierCodes: []string{}}})
	if len(groups[0].BaseRules.AllowedCarrierCodes) != 0 || groups[0].Carriers[0].Enabled {
		t.Fatal("empty physical capability must disable all carriers")
	}
}
