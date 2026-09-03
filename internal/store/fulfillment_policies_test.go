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
