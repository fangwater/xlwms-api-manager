package packing

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"xlwms-api-manager/internal/model"
)

func TestBuildRotatesItemsAndHonorsWeightPerCarton(t *testing.T) {
	request := Request{
		Items:  []model.WarehouseSKUQuantity{{WarehouseSKU: "ROTATED", Quantity: 1}, {WarehouseSKU: "SMALL", Quantity: 1}},
		Carton: CartonSpec{LengthCM: 10, WidthCM: 8, HeightCM: 5, MaxWeightKG: 3, Count: 2},
	}
	resolved := []model.WarehouseSKUSpecResolutionItem{
		completeSpec("ROTATED", "Rotated item", 8, 5, 10, 2),
		completeSpec("SMALL", "Small item", 5, 4, 5, 2),
	}

	plan, err := Build(request, resolved)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Summary.PackedUnits != 2 || plan.Summary.CartonsUsed != 2 || plan.Summary.UnfitUnits != 0 {
		t.Fatalf("unexpected summary: %+v", plan.Summary)
	}
	for _, carton := range plan.Cartons {
		if carton.UsedWeightKG > request.Carton.MaxWeightKG {
			t.Fatalf("carton %d exceeds max weight: %v", carton.Index, carton.UsedWeightKG)
		}
	}
	rotated := findPlacement(t, plan, "ROTATED")
	if rotated.Rotation == "WHD" {
		t.Fatalf("expected ROTATED item to rotate, got %+v", rotated)
	}
	assertPlacementWithinCarton(t, rotated, request.Carton)
}

func TestBuildReportsUnfitReasons(t *testing.T) {
	request := Request{
		Items: []model.WarehouseSKUQuantity{
			{WarehouseSKU: "FULL", Quantity: 2},
			{WarehouseSKU: "OVERSIZE", Quantity: 1},
			{WarehouseSKU: "HEAVY", Quantity: 1},
		},
		Carton: CartonSpec{LengthCM: 10, WidthCM: 10, HeightCM: 10, MaxWeightKG: 3, Count: 1},
	}
	resolved := []model.WarehouseSKUSpecResolutionItem{
		completeSpec("FULL", "Full carton", 10, 10, 10, 1),
		completeSpec("OVERSIZE", "Too large", 11, 11, 11, 1),
		completeSpec("HEAVY", "Too heavy", 1, 1, 1, 4),
	}

	plan, err := Build(request, resolved)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Summary.PackedUnits != 1 || plan.Summary.UnfitUnits != 3 {
		t.Fatalf("unexpected summary: %+v", plan.Summary)
	}
	reasons := make(map[string]int)
	for _, item := range plan.UnfitItems {
		reasons[item.ReasonCode]++
	}
	for _, expected := range []string{"exceeds_carton_dimensions", "exceeds_carton_weight", "packing_capacity_exhausted"} {
		if reasons[expected] != 1 {
			t.Fatalf("expected one %s reason, got %v", expected, reasons)
		}
	}
}

func TestBuildPlacementsAreBoundedNonOverlappingAndDeterministic(t *testing.T) {
	request := Request{
		Items:  []model.WarehouseSKUQuantity{{WarehouseSKU: "CUBE", Quantity: 2}},
		Carton: CartonSpec{LengthCM: 20, WidthCM: 10, HeightCM: 10, MaxWeightKG: 10, Count: 1},
	}
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
	if len(first.Cartons) != 1 || len(first.Cartons[0].Placements) != 2 {
		t.Fatalf("unexpected cartons: %+v", first.Cartons)
	}
	placements := first.Cartons[0].Placements
	for _, placement := range placements {
		assertPlacementWithinCarton(t, placement, request.Carton)
	}
	if intersects(placements[0], placements[1]) {
		t.Fatalf("placements overlap: %+v %+v", placements[0], placements[1])
	}
}

func TestValidateRequestRejectsUnsafeLimits(t *testing.T) {
	base := Request{
		Items:  []model.WarehouseSKUQuantity{{WarehouseSKU: "SKU", Quantity: 1}},
		Carton: CartonSpec{LengthCM: 10, WidthCM: 10, HeightCM: 10, MaxWeightKG: 10, Count: 1},
	}
	cases := []struct {
		name   string
		mutate func(*Request)
	}{
		{name: "empty items", mutate: func(request *Request) { request.Items = nil }},
		{name: "too many units", mutate: func(request *Request) { request.Items[0].Quantity = maxUnits + 1 }},
		{name: "too many cartons", mutate: func(request *Request) { request.Carton.Count = maxCartons + 1 }},
		{name: "nan dimension", mutate: func(request *Request) { request.Carton.LengthCM = math.NaN() }},
		{name: "infinite weight", mutate: func(request *Request) { request.Carton.MaxWeightKG = math.Inf(1) }},
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

func findPlacement(t *testing.T, plan Plan, sku string) Placement {
	t.Helper()
	for _, carton := range plan.Cartons {
		for _, placement := range carton.Placements {
			if placement.WarehouseSKU == sku {
				return placement
			}
		}
	}
	t.Fatalf("placement for %s not found", sku)
	return Placement{}
}

func assertPlacementWithinCarton(t *testing.T, placement Placement, carton CartonSpec) {
	t.Helper()
	if placement.Position.X < 0 || placement.Position.Y < 0 || placement.Position.Z < 0 ||
		placement.Position.X+placement.Dimensions.LengthCM > carton.LengthCM+0.001 ||
		placement.Position.Y+placement.Dimensions.HeightCM > carton.HeightCM+0.001 ||
		placement.Position.Z+placement.Dimensions.WidthCM > carton.WidthCM+0.001 {
		t.Fatalf("placement outside carton: %+v in %+v", placement, carton)
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
