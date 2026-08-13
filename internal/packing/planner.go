package packing

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"xlwms-api-manager/internal/model"
)

const (
	maxDistinctSKUs       = 40
	maxUnits              = 300
	maxPackageDimensionCM = 500
	maxPackageWeightKG    = 1000
	packingEpsilon        = 0.000001
)

type Request struct {
	Items []model.WarehouseSKUQuantity `json:"items"`
}

type Dimensions struct {
	LengthCM float64 `json:"length_cm"`
	WidthCM  float64 `json:"width_cm"`
	HeightCM float64 `json:"height_cm"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type Placement struct {
	Step               int        `json:"step"`
	UnitID             string     `json:"unit_id"`
	WarehouseSKU       string     `json:"warehouse_sku"`
	ProductName        string     `json:"product_name,omitempty"`
	Position           Position   `json:"position"`
	Dimensions         Dimensions `json:"dimensions"`
	OriginalDimensions Dimensions `json:"original_dimensions"`
	WeightKG           float64    `json:"weight_kg"`
}

type PackagePlan struct {
	Index                    int         `json:"index"`
	Dimensions               Dimensions  `json:"dimensions"`
	Placements               []Placement `json:"placements"`
	PackedUnits              int         `json:"packed_units"`
	UsedWeightKG             float64     `json:"used_weight_kg"`
	UsedVolumeCM3            float64     `json:"used_volume_cm3"`
	VolumeUtilizationPercent float64     `json:"volume_utilization_percent"`
}

type UnfitItem struct {
	UnitID       string     `json:"unit_id"`
	WarehouseSKU string     `json:"warehouse_sku"`
	ProductName  string     `json:"product_name,omitempty"`
	Dimensions   Dimensions `json:"dimensions"`
	WeightKG     float64    `json:"weight_kg"`
	ReasonCode   string     `json:"reason_code"`
	Reason       string     `json:"reason"`
}

type Summary struct {
	RequestedUnits  int     `json:"requested_units"`
	PackedUnits     int     `json:"packed_units"`
	UnfitUnits      int     `json:"unfit_units"`
	PackagesUsed    int     `json:"packages_used"`
	TotalWeightKG   float64 `json:"total_weight_kg"`
	PackedWeightKG  float64 `json:"packed_weight_kg"`
	PackedVolumeCM3 float64 `json:"packed_volume_cm3"`
}

type Plan struct {
	Algorithm  string        `json:"algorithm"`
	Heuristic  bool          `json:"heuristic"`
	Packages   []PackagePlan `json:"packages"`
	UnfitItems []UnfitItem   `json:"unfit_items"`
	Summary    Summary       `json:"summary"`
}

type unit struct {
	id          string
	ordinal     int
	sku         string
	productName string
	dimensions  Dimensions
	weightKG    float64
}

type packageState struct {
	index      int
	bounds     Dimensions
	placements []Placement
	usedWeight float64
	usedVolume float64
}

type placementCandidate struct {
	position    Position
	bounds      Dimensions
	addedVolume float64
	totalVolume float64
}

func ValidateRequest(request Request) error {
	if len(request.Items) == 0 {
		return errors.New("items are required")
	}
	if len(request.Items) > maxDistinctSKUs {
		return fmt.Errorf("items cannot contain more than %d SKU entries", maxDistinctSKUs)
	}
	totalUnits := 0
	seen := make(map[string]struct{}, len(request.Items))
	for _, item := range request.Items {
		sku := strings.TrimSpace(item.WarehouseSKU)
		if sku == "" || item.Quantity <= 0 {
			return errors.New("warehouse_sku and a positive quantity are required")
		}
		if len(sku) > 255 {
			return errors.New("warehouse_sku cannot exceed 255 characters")
		}
		if item.Quantity > maxUnits || totalUnits > maxUnits-item.Quantity {
			return fmt.Errorf("total quantity cannot exceed %d", maxUnits)
		}
		seen[sku] = struct{}{}
		totalUnits += item.Quantity
	}
	if len(seen) > maxDistinctSKUs {
		return fmt.Errorf("items cannot contain more than %d distinct SKUs", maxDistinctSKUs)
	}
	return nil
}

func Build(request Request, resolved []model.WarehouseSKUSpecResolutionItem) (Plan, error) {
	plan := Plan{
		Algorithm:  "fixed-orientation-envelope-v1",
		Heuristic:  true,
		Packages:   make([]PackagePlan, 0),
		UnfitItems: make([]UnfitItem, 0),
	}
	if err := ValidateRequest(request); err != nil {
		return plan, err
	}

	units, err := resolveUnits(request, resolved)
	if err != nil {
		return plan, err
	}
	sortUnits(units)
	plan.Summary.RequestedUnits = len(units)
	for _, item := range units {
		plan.Summary.TotalWeightKG += item.weightKG
	}

	packages := make([]packageState, 0)
	for _, item := range units {
		if exceedsAutomaticPackageLimits(item) {
			plan.UnfitItems = append(plan.UnfitItems, toUnfit(item, "exceeds_auto_package_limits", "单件规格超出自动包裹规划上限"))
			continue
		}

		packageIndex, candidate, found := bestExistingPackage(packages, item)
		if !found {
			packages = append(packages, packageState{index: len(packages) + 1, placements: make([]Placement, 0)})
			packageIndex = len(packages) - 1
			candidate = placementCandidate{position: Position{}, bounds: item.dimensions, totalVolume: volume(item.dimensions)}
		}
		addPlacement(&packages[packageIndex], item, candidate)
	}

	plan.Packages = make([]PackagePlan, 0, len(packages))
	for _, state := range packages {
		packaged := finalizePackage(state)
		plan.Packages = append(plan.Packages, packaged)
		plan.Summary.PackedUnits += packaged.PackedUnits
		plan.Summary.PackedWeightKG += packaged.UsedWeightKG
		plan.Summary.PackedVolumeCM3 += packaged.UsedVolumeCM3
	}
	plan.Summary.UnfitUnits = len(plan.UnfitItems)
	plan.Summary.PackagesUsed = len(plan.Packages)
	plan.Summary.TotalWeightKG = round(plan.Summary.TotalWeightKG)
	plan.Summary.PackedWeightKG = round(plan.Summary.PackedWeightKG)
	plan.Summary.PackedVolumeCM3 = round(plan.Summary.PackedVolumeCM3)
	return plan, nil
}

func resolveUnits(request Request, resolved []model.WarehouseSKUSpecResolutionItem) ([]unit, error) {
	requestOrder := make([]string, 0, len(request.Items))
	quantities := make(map[string]int, len(request.Items))
	for _, item := range request.Items {
		sku := strings.TrimSpace(item.WarehouseSKU)
		if _, exists := quantities[sku]; !exists {
			requestOrder = append(requestOrder, sku)
		}
		quantities[sku] += item.Quantity
	}
	resolvedBySKU := make(map[string]model.WarehouseSKUSpecResolutionItem, len(resolved))
	for _, item := range resolved {
		resolvedBySKU[item.WarehouseSKU] = item
	}

	units := make([]unit, 0)
	for _, sku := range requestOrder {
		item, ok := resolvedBySKU[sku]
		if !ok || !item.Complete || item.LengthCM == nil || item.WidthCM == nil || item.HeightCM == nil || item.WeightKG == nil {
			return nil, fmt.Errorf("warehouse SKU %s has no complete enabled package specification", sku)
		}
		values := []float64{*item.LengthCM, *item.WidthCM, *item.HeightCM, *item.WeightKG}
		for _, value := range values {
			if !positiveFinite(value) {
				return nil, fmt.Errorf("warehouse SKU %s has an invalid package specification", sku)
			}
		}
		for index := 1; index <= quantities[sku]; index++ {
			units = append(units, unit{
				id:          fmt.Sprintf("%s#%d", sku, index),
				ordinal:     index,
				sku:         sku,
				productName: item.ProductName,
				dimensions:  Dimensions{LengthCM: *item.LengthCM, WidthCM: *item.WidthCM, HeightCM: *item.HeightCM},
				weightKG:    *item.WeightKG,
			})
		}
	}
	return units, nil
}

func bestExistingPackage(packages []packageState, item unit) (int, placementCandidate, bool) {
	bestIndex := -1
	best := placementCandidate{}
	found := false
	for index := range packages {
		candidate, fits := bestPlacement(packages[index], item)
		if !fits || found && !betterCandidate(candidate, best) {
			continue
		}
		bestIndex, best, found = index, candidate, true
	}
	return bestIndex, best, found
}

func bestPlacement(packaged packageState, item unit) (placementCandidate, bool) {
	if packaged.usedWeight+item.weightKG > maxPackageWeightKG+packingEpsilon {
		return placementCandidate{}, false
	}

	positions := make([]Position, 0, len(packaged.placements)*3)
	seen := make(map[Position]struct{}, len(packaged.placements)*3)
	addPosition := func(position Position) {
		if _, exists := seen[position]; exists {
			return
		}
		seen[position] = struct{}{}
		positions = append(positions, position)
	}
	for _, placed := range packaged.placements {
		addPosition(Position{X: placed.Position.X + placed.Dimensions.LengthCM, Y: placed.Position.Y, Z: placed.Position.Z})
		addPosition(Position{X: placed.Position.X, Y: placed.Position.Y + placed.Dimensions.HeightCM, Z: placed.Position.Z})
		addPosition(Position{X: placed.Position.X, Y: placed.Position.Y, Z: placed.Position.Z + placed.Dimensions.WidthCM})
	}

	currentVolume := volume(packaged.bounds)
	best := placementCandidate{}
	found := false
	for _, position := range positions {
		if intersectsAny(packaged.placements, position, item.dimensions) {
			continue
		}
		bounds := expandedBounds(packaged.bounds, position, item.dimensions)
		if bounds.LengthCM > maxPackageDimensionCM+packingEpsilon ||
			bounds.WidthCM > maxPackageDimensionCM+packingEpsilon ||
			bounds.HeightCM > maxPackageDimensionCM+packingEpsilon {
			continue
		}
		candidate := placementCandidate{
			position:    position,
			bounds:      bounds,
			addedVolume: volume(bounds) - currentVolume,
			totalVolume: volume(bounds),
		}
		if !found || betterCandidate(candidate, best) {
			best, found = candidate, true
		}
	}
	return best, found
}

func betterCandidate(left, right placementCandidate) bool {
	if !nearlyEqual(left.addedVolume, right.addedVolume) {
		return left.addedVolume < right.addedVolume
	}
	if !nearlyEqual(left.totalVolume, right.totalVolume) {
		return left.totalVolume < right.totalVolume
	}
	leftSpan := left.bounds.LengthCM + left.bounds.WidthCM + left.bounds.HeightCM
	rightSpan := right.bounds.LengthCM + right.bounds.WidthCM + right.bounds.HeightCM
	if !nearlyEqual(leftSpan, rightSpan) {
		return leftSpan < rightSpan
	}
	if !nearlyEqual(left.position.Y, right.position.Y) {
		return left.position.Y < right.position.Y
	}
	if !nearlyEqual(left.position.X, right.position.X) {
		return left.position.X < right.position.X
	}
	return left.position.Z < right.position.Z-packingEpsilon
}

func addPlacement(packaged *packageState, item unit, candidate placementCandidate) {
	packaged.placements = append(packaged.placements, Placement{
		Step:               len(packaged.placements) + 1,
		UnitID:             item.id,
		WarehouseSKU:       item.sku,
		ProductName:        item.productName,
		Position:           candidate.position,
		Dimensions:         item.dimensions,
		OriginalDimensions: item.dimensions,
		WeightKG:           item.weightKG,
	})
	packaged.bounds = candidate.bounds
	packaged.usedWeight += item.weightKG
	packaged.usedVolume += volume(item.dimensions)
}

func finalizePackage(state packageState) PackagePlan {
	placements := make([]Placement, len(state.placements))
	copy(placements, state.placements)
	for index := range placements {
		placements[index].Position = Position{X: round(placements[index].Position.X), Y: round(placements[index].Position.Y), Z: round(placements[index].Position.Z)}
		placements[index].Dimensions = roundDimensions(placements[index].Dimensions)
		placements[index].OriginalDimensions = roundDimensions(placements[index].OriginalDimensions)
		placements[index].WeightKG = round(placements[index].WeightKG)
	}
	packageVolume := volume(state.bounds)
	utilization := 0.0
	if packageVolume > 0 {
		utilization = state.usedVolume * 100 / packageVolume
	}
	return PackagePlan{
		Index:                    state.index,
		Dimensions:               roundDimensions(state.bounds),
		Placements:               placements,
		PackedUnits:              len(placements),
		UsedWeightKG:             round(state.usedWeight),
		UsedVolumeCM3:            round(state.usedVolume),
		VolumeUtilizationPercent: round(utilization),
	}
}

func intersectsAny(placements []Placement, position Position, dimensions Dimensions) bool {
	for _, placed := range placements {
		if position.X < placed.Position.X+placed.Dimensions.LengthCM-packingEpsilon &&
			placed.Position.X < position.X+dimensions.LengthCM-packingEpsilon &&
			position.Y < placed.Position.Y+placed.Dimensions.HeightCM-packingEpsilon &&
			placed.Position.Y < position.Y+dimensions.HeightCM-packingEpsilon &&
			position.Z < placed.Position.Z+placed.Dimensions.WidthCM-packingEpsilon &&
			placed.Position.Z < position.Z+dimensions.WidthCM-packingEpsilon {
			return true
		}
	}
	return false
}

func expandedBounds(current Dimensions, position Position, dimensions Dimensions) Dimensions {
	return Dimensions{
		LengthCM: math.Max(current.LengthCM, position.X+dimensions.LengthCM),
		WidthCM:  math.Max(current.WidthCM, position.Z+dimensions.WidthCM),
		HeightCM: math.Max(current.HeightCM, position.Y+dimensions.HeightCM),
	}
}

func exceedsAutomaticPackageLimits(item unit) bool {
	return item.dimensions.LengthCM > maxPackageDimensionCM ||
		item.dimensions.WidthCM > maxPackageDimensionCM ||
		item.dimensions.HeightCM > maxPackageDimensionCM ||
		item.weightKG > maxPackageWeightKG
}

func sortUnits(items []unit) {
	sort.SliceStable(items, func(i, j int) bool {
		leftVolume := volume(items[i].dimensions)
		rightVolume := volume(items[j].dimensions)
		if leftVolume != rightVolume {
			return leftVolume > rightVolume
		}
		if items[i].weightKG != items[j].weightKG {
			return items[i].weightKG > items[j].weightKG
		}
		if items[i].sku != items[j].sku {
			return items[i].sku < items[j].sku
		}
		return items[i].ordinal < items[j].ordinal
	})
}

func toUnfit(item unit, code, reason string) UnfitItem {
	return UnfitItem{
		UnitID:       item.id,
		WarehouseSKU: item.sku,
		ProductName:  item.productName,
		Dimensions:   roundDimensions(item.dimensions),
		WeightKG:     round(item.weightKG),
		ReasonCode:   code,
		Reason:       reason,
	}
}

func volume(dimensions Dimensions) float64 {
	return dimensions.LengthCM * dimensions.WidthCM * dimensions.HeightCM
}

func roundDimensions(dimensions Dimensions) Dimensions {
	return Dimensions{LengthCM: round(dimensions.LengthCM), WidthCM: round(dimensions.WidthCM), HeightCM: round(dimensions.HeightCM)}
}

func nearlyEqual(left, right float64) bool {
	return math.Abs(left-right) <= packingEpsilon
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func round(value float64) float64 {
	return math.Round(value*1000) / 1000
}
