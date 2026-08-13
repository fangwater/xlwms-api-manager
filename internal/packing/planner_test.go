package packing

import (
	"encoding/json"
	"reflect"
	"testing"

	"xlwms-api-manager/internal/model"
)

func TestBuildAutomaticallyCalculatesPackageDimensionsWithoutRotation(t *testing.T) {
	request := Request{Items: []model.WarehouseSKUQuantity{{WarehouseSKU: "FIXED", Quantity: 1}, {WarehouseSKU: "SMALL", Quantity: 1}}}
	resolved := []model.WarehouseSKUSpecResolutionItem{
		completeSpec("FIXED", "Fixed item", 8, 5, 5, 2),
		completeSpec("SMALL", "Small item", 5, 4, 5, 2),
	}

	plan, err := Build(request, resolved)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Summary.PackedUnits != 2 || plan.Summary.PackagesUsed != 1 || plan.Summary.UnfitUnits != 0 {
		t.Fatalf("unexpected summary: %+v", plan.Summary)
	}
	if len(plan.Packages) != 1 || plan.Packages[0].Dimensions != (Dimensions{LengthCM: 13, WidthCM: 5, HeightCM: 5}) {
		t.Fatalf("unexpected package: %+v", plan.Packages)
	}
	for _, placement := range plan.Packages[0].Placements {
		if placement.Dimensions != placement.OriginalDimensions {
			t.Fatalf("placement changed orientation: %+v", placement)
		}
	}
}

func TestBuildSplitsPackagesAtInternalWeightLimit(t *testing.T) {
	request := Request{Items: []model.WarehouseSKUQuantity{{WarehouseSKU: "HEAVY", Quantity: 2}}}
	resolved := []model.WarehouseSKUSpecResolutionItem{completeSpec("HEAVY", "Heavy item", 10, 10, 10, 600)}

	plan, err := Build(request, resolved)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Summary.PackedUnits != 2 || plan.Summary.PackagesUsed != 2 || plan.Summary.UnfitUnits != 0 {
		t.Fatalf("unexpected summary: %+v", plan.Summary)
	}
	for _, packaged := range plan.Packages {
		if packaged.PackedUnits != 1 || packaged.UsedWeightKG != 600 {
			t.Fatalf("unexpected package: %+v", packaged)
		}
	}
}

func TestBuildReportsSingleItemOutsideAutomaticLimits(t *testing.T) {
	request := Request{Items: []model.WarehouseSKUQuantity{{WarehouseSKU: "OVERSIZE", Quantity: 1}}}
	resolved := []model.WarehouseSKUSpecResolutionItem{completeSpec("OVERSIZE", "Oversize", maxPackageDimensionCM+1, 10, 10, 1)}

	plan, err := Build(request, resolved)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Summary.PackedUnits != 0 || plan.Summary.PackagesUsed != 0 || plan.Summary.UnfitUnits != 1 {
		t.Fatalf("unexpected summary: %+v", plan.Summary)
	}
	if plan.UnfitItems[0].ReasonCode != "exceeds_auto_package_limits" {
		t.Fatalf("unexpected unfit reason: %+v", plan.UnfitItems[0])
	}
}

func TestBuildPlacementsAreBoundedNonOverlappingAndDeterministic(t *testing.T) {
	request := Request{Items: []model.WarehouseSKUQuantity{{WarehouseSKU: "CUBE", Quantity: 4}}}
	resolved := []model.WarehouseSKUSpecResolutionItem{completeSpec("CUBE", "Cube", 10, 10, 10, 1)}

	first, err := Build(request, resolved)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	second, err := Build(request, resolved)
	if err != nil {
		t.Fatalf("second Build() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		firstJSON, _ := json.Marshal(first)
		secondJSON, _ := json.Marshal(second)
		t.Fatalf("Build() is not deterministic:\n%s\n%s", firstJSON, secondJSON)
	}
	if len(first.Packages) != 1 || len(first.Packages[0].Placements) != 4 {
		t.Fatalf("unexpected packages: %+v", first.Packages)
	}
	packaged := first.Packages[0]
	for _, placement := range packaged.Placements {
		assertPlacementWithinPackage(t, placement, packaged.Dimensions)
	}
	for left := 0; left < len(packaged.Placements); left++ {
		for right := left + 1; right < len(packaged.Placements); right++ {
			if intersects(packaged.Placements[left], packaged.Placements[right]) {
				t.Fatalf("placements overlap: %+v %+v", packaged.Placements[left], packaged.Placements[right])
			}
		}
	}
}

func TestValidateRequestRejectsUnsafeLimits(t *testing.T) {
	base := Request{Items: []model.WarehouseSKUQuantity{{WarehouseSKU: "SKU", Quantity: 1}}}
	cases := []struct {
		name   string
		mutate func(*Request)
	}{
		{name: "empty items", mutate: func(request *Request) { request.Items = nil }},
		{name: "too many units", mutate: func(request *Request) { request.Items[0].Quantity = maxUnits + 1 }},
		{name: "empty SKU", mutate: func(request *Request) { request.Items[0].WarehouseSKU = "" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := base
			request.Items = append([]model.WarehouseSKUQuantity(nil), base.Items...)
			testCase.mutate(&request)
			if err := ValidateRequest(request); err == nil {
				t.Fatal("ValidateRequest() error = nil")
			}
		})
	}
}

func completeSpec(sku, name string, length, width, height, weight float64) model.WarehouseSKUSpecResolutionItem {
	return model.WarehouseSKUSpecResolutionItem{
		WarehouseSKU:        sku,
		ProductName:         name,
		Quantity:            1,
		MatchedWarehouseSKU: sku,
		Matched:             true,
		Enabled:             true,
		Complete:            true,
		LengthCM:            floatPointer(length),
		WidthCM:             floatPointer(width),
		HeightCM:            floatPointer(height),
		WeightKG:            floatPointer(weight),
		MissingFields:       []string{},
	}
}

func floatPointer(value float64) *float64 {
	return &value
}

func assertPlacementWithinPackage(t *testing.T, placement Placement, dimensions Dimensions) {
	t.Helper()
	if placement.Position.X < 0 || placement.Position.Y < 0 || placement.Position.Z < 0 ||
		placement.Position.X+placement.Dimensions.LengthCM > dimensions.LengthCM+0.001 ||
		placement.Position.Y+placement.Dimensions.HeightCM > dimensions.HeightCM+0.001 ||
		placement.Position.Z+placement.Dimensions.WidthCM > dimensions.WidthCM+0.001 {
		t.Fatalf("placement outside package: %+v in %+v", placement, dimensions)
	}
}

func intersects(left, right Placement) bool {
	return left.Position.X < right.Position.X+right.Dimensions.LengthCM &&
		right.Position.X < left.Position.X+left.Dimensions.LengthCM &&
		left.Position.Y < right.Position.Y+right.Dimensions.HeightCM &&
		right.Position.Y < left.Position.Y+left.Dimensions.HeightCM &&
		left.Position.Z < right.Position.Z+right.Dimensions.WidthCM &&
		right.Position.Z < left.Position.Z+left.Dimensions.WidthCM
}
