package store

import (
	"strings"
	"testing"

	"xlwms-api-manager/internal/model"
)

func validSKUCombination() model.SKUCombination {
	calculatedLength, calculatedWidth := 30.0, 10.0
	calculatedHeight, calculatedWeight := 8.0, 2.0
	return model.SKUCombination{
		Name: "20pcs replacement", SubstituteForSKU: "ITEM-20PCS",
		LengthCM: 28, WidthCM: 10, HeightCM: 8, WeightKG: 1.9,
		CalculatedLengthCM: &calculatedLength, CalculatedWidthCM: &calculatedWidth,
		CalculatedHeightCM: &calculatedHeight, CalculatedWeightKG: &calculatedWeight,
		Enabled: true,
		Items:   []model.SKUCombinationItem{{WarehouseSKU: "ITEM-10PCS", Quantity: 2}},
	}
}

func TestValidateSKUCombinationAcceptsCorrectedSubstitution(t *testing.T) {
	item, err := validateSKUCombination(validSKUCombination())
	if err != nil {
		t.Fatalf("validateSKUCombination() error = %v", err)
	}
	if item.Name != "20pcs replacement" || item.Items[0].Quantity != 2 {
		t.Fatalf("unexpected normalized combination: %+v", item)
	}
}

func TestValidateSKUCombinationAcceptsArbitraryThreeSKURecipe(t *testing.T) {
	item := validSKUCombination()
	item.Items = []model.SKUCombinationItem{
		{WarehouseSKU: "SKU-A", Quantity: 1},
		{WarehouseSKU: "SKU-B", Quantity: 2},
		{WarehouseSKU: "SKU-C", Quantity: 3},
	}
	validated, err := validateSKUCombination(item)
	if err != nil {
		t.Fatalf("validateSKUCombination() error = %v", err)
	}
	if len(validated.Items) != 3 || validated.Items[2].Quantity != 3 {
		t.Fatalf("unexpected members: %+v", validated.Items)
	}
}

func TestValidateSKUCombinationRejectsAmbiguousOrInvalidMappings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.SKUCombination)
		want   string
	}{
		{name: "no members", mutate: func(item *model.SKUCombination) { item.Items = nil }, want: "items must contain"},
		{name: "target is member", mutate: func(item *model.SKUCombination) { item.SubstituteForSKU = item.Items[0].WarehouseSKU }, want: "cannot also be"},
		{name: "duplicate member", mutate: func(item *model.SKUCombination) { item.Items = append(item.Items, item.Items[0]) }, want: "duplicated"},
		{name: "partial calculated values", mutate: func(item *model.SKUCombination) { item.CalculatedWeightKG = nil }, want: "provided together"},
		{name: "invalid correction", mutate: func(item *model.SKUCombination) { item.WeightKG = 0 }, want: "weight_kg"},
		{name: "too many units", mutate: func(item *model.SKUCombination) { item.Items[0].Quantity = 301 }, want: "cannot exceed 300"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			item := validSKUCombination()
			testCase.mutate(&item)
			_, err := validateSKUCombination(item)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want containing %q", err, testCase.want)
			}
		})
	}
}

func TestSKUCombinationCorrectedComparesAlgorithmAndManualValues(t *testing.T) {
	item := validSKUCombination()
	if !skuCombinationCorrected(item) {
		t.Fatal("expected manually changed values to be marked corrected")
	}
	item.LengthCM = *item.CalculatedLengthCM
	item.WidthCM = *item.CalculatedWidthCM
	item.HeightCM = *item.CalculatedHeightCM
	item.WeightKG = *item.CalculatedWeightKG
	if skuCombinationCorrected(item) {
		t.Fatal("equal calculated and final values must not be marked corrected")
	}
}
