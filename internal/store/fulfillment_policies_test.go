package store

import (
	"testing"

	"xlwms-api-manager/internal/model"
)

func TestValidateCarrierPoliciesAcceptsCompleteUniquePolicy(t *testing.T) {
	policies := make([]model.CarrierPolicy, 0, len(SupportedAutomaticCarrierCodes))
	for index, code := range SupportedAutomaticCarrierCodes {
		policies = append(policies, model.CarrierPolicy{CarrierCode: code, Priority: index + 1, Enabled: true})
	}
	normalized, err := ValidateCarrierPolicies("dps002", policies)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized) != len(SupportedAutomaticCarrierCodes) || normalized[0].WarehouseKey != "DPS002" {
		t.Fatalf("unexpected normalized policies: %#v", normalized)
	}
}

func TestValidateCarrierPoliciesRejectsDuplicatePriority(t *testing.T) {
	policies := make([]model.CarrierPolicy, 0, len(SupportedAutomaticCarrierCodes))
	for index, code := range SupportedAutomaticCarrierCodes {
		policies = append(policies, model.CarrierPolicy{CarrierCode: code, Priority: index + 1, Enabled: true})
	}
	policies[1].Priority = policies[0].Priority
	if _, err := ValidateCarrierPolicies("DPS002", policies); err == nil {
		t.Fatal("duplicate priorities must be rejected")
	}
}

func TestValidateWarehouseCarrierRulesNormalizesCurrentTemuRules(t *testing.T) {
	rules, err := ValidateWarehouseCarrierRules("dps002", model.WarehouseCarrierRules{
		AllowedCarrierCodes: []string{"ups", "gofo"}, AllowedCurrencyCodes: []string{"usd"},
		SelectionMode: "carrier_priority_within_delta", MaxPriceDelta: 0.5, WarehouseTiePriority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rules.WarehouseKey != "DPS002" || rules.AllowedCarrierCodes[0] != "GOFO" || rules.AllowedCurrencyCodes[0] != "USD" {
		t.Fatalf("unexpected normalized base rules: %#v", rules)
	}
}

func TestValidateWarehouseCarrierRulesRejectsUnknownCarrier(t *testing.T) {
	_, err := ValidateWarehouseCarrierRules("DPS002", model.WarehouseCarrierRules{
		AllowedCarrierCodes: []string{"UNKNOWN"}, SelectionMode: "lowest_price", WarehouseTiePriority: 1,
	})
	if err == nil {
		t.Fatal("unknown base carrier must be rejected")
	}
}
