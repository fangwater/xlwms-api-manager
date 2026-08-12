package packing

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/tvanriper/bp3d"

	"xlwms-api-manager/internal/model"
)

const (
	maxDistinctSKUs = 40
	maxUnits        = 300
	maxCartons      = 20
	maxDimensionCM  = 500
	maxWeightKG     = 1000
)

type CartonSpec struct {
	LengthCM    float64 `json:"length_cm"`
	WidthCM     float64 `json:"width_cm"`
	HeightCM    float64 `json:"height_cm"`
	MaxWeightKG float64 `json:"max_weight_kg"`
	Count       int     `json:"count"`
}

type Request struct {
	Items  []model.WarehouseSKUQuantity `json:"items"`
	Carton CartonSpec                   `json:"carton"`
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
	Rotation           string     `json:"rotation"`
}

type CartonPlan struct {
	Index                    int         `json:"index"`
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
	RequestedUnits   int     `json:"requested_units"`
	PackedUnits      int     `json:"packed_units"`
	UnfitUnits       int     `json:"unfit_units"`
	CartonsUsed      int     `json:"cartons_used"`
	CartonsAvailable int     `json:"cartons_available"`
	TotalWeightKG    float64 `json:"total_weight_kg"`
	PackedWeightKG   float64 `json:"packed_weight_kg"`
	PackedVolumeCM3  float64 `json:"packed_volume_cm3"`
}

type Plan struct {
	Algorithm  string       `json:"algorithm"`
	Heuristic  bool         `json:"heuristic"`
	Carton     CartonSpec   `json:"carton"`
	Cartons    []CartonPlan `json:"cartons"`
	UnfitItems []UnfitItem  `json:"unfit_items"`
	Summary    Summary      `json:"summary"`
}

type unit struct {
	id          string
	ordinal     int
	sku         string
	productName string
	dimensions  Dimensions
	weightKG    float64
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
	if request.Carton.Count < 1 || request.Carton.Count > maxCartons {
		return fmt.Errorf("carton count must be between 1 and %d", maxCartons)
	}
	for name, value := range map[string]float64{
		"carton length_cm": request.Carton.LengthCM,
		"carton width_cm":  request.Carton.WidthCM,
		"carton height_cm": request.Carton.HeightCM,
	} {
		if !positiveFinite(value) || value > maxDimensionCM {
			return fmt.Errorf("%s must be positive and no greater than %.0f", name, float64(maxDimensionCM))
		}
	}
	if !positiveFinite(request.Carton.MaxWeightKG) || request.Carton.MaxWeightKG > maxWeightKG {
		return fmt.Errorf("carton max_weight_kg must be positive and no greater than %.0f", float64(maxWeightKG))
	}
	return nil
}

func Build(request Request, resolved []model.WarehouseSKUSpecResolutionItem) (Plan, error) {
	plan := Plan{
		Algorithm:  "bp3d-pivot-v1",
		Heuristic:  true,
		Carton:     request.Carton,
		Cartons:    make([]CartonPlan, 0, request.Carton.Count),
		UnfitItems: make([]UnfitItem, 0),
	}
	if err := ValidateRequest(request); err != nil {
		return plan, err
	}

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
			return plan, fmt.Errorf("warehouse SKU %s has no complete enabled package specification", sku)
		}
		values := []float64{*item.LengthCM, *item.WidthCM, *item.HeightCM, *item.WeightKG}
		for _, value := range values {
			if !positiveFinite(value) {
				return plan, fmt.Errorf("warehouse SKU %s has an invalid package specification", sku)
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
	sortUnits(units)
	plan.Summary.RequestedUnits = len(units)
	plan.Summary.CartonsAvailable = request.Carton.Count
	for _, item := range units {
		plan.Summary.TotalWeightKG += item.weightKG
	}

	remaining := make([]unit, 0, len(units))
	for _, item := range units {
		switch {
		case !fitsCarton(item.dimensions, request.Carton):
			plan.UnfitItems = append(plan.UnfitItems, toUnfit(item, "exceeds_carton_dimensions", "SKU 任意旋转后仍超出箱体尺寸"))
		case item.weightKG > request.Carton.MaxWeightKG:
			plan.UnfitItems = append(plan.UnfitItems, toUnfit(item, "exceeds_carton_weight", "单件重量超过箱体承重"))
		default:
			remaining = append(remaining, item)
		}
	}

	for cartonIndex := 1; cartonIndex <= request.Carton.Count && len(remaining) > 0; cartonIndex++ {
		candidates := selectCartonCandidates(remaining, request.Carton)
		if len(candidates) == 0 {
			break
		}
		packed, err := packCarton(cartonIndex, candidates, request.Carton)
		if err != nil {
			return plan, err
		}
		if len(packed.Placements) == 0 {
			break
		}
		plan.Cartons = append(plan.Cartons, packed)
		packedIDs := make(map[string]struct{}, len(packed.Placements))
		for _, placement := range packed.Placements {
			packedIDs[placement.UnitID] = struct{}{}
		}
		next := remaining[:0]
		for _, item := range remaining {
			if _, packed := packedIDs[item.id]; !packed {
				next = append(next, item)
			}
		}
		remaining = next
	}

	for _, item := range remaining {
		plan.UnfitItems = append(plan.UnfitItems, toUnfit(item, "packing_capacity_exhausted", "现有箱数或启发式布局无法继续容纳该件"))
	}
	for _, carton := range plan.Cartons {
		plan.Summary.PackedUnits += carton.PackedUnits
		plan.Summary.PackedWeightKG += carton.UsedWeightKG
		plan.Summary.PackedVolumeCM3 += carton.UsedVolumeCM3
	}
	plan.Summary.UnfitUnits = len(plan.UnfitItems)
	plan.Summary.CartonsUsed = len(plan.Cartons)
	plan.Summary.TotalWeightKG = round(plan.Summary.TotalWeightKG)
	plan.Summary.PackedWeightKG = round(plan.Summary.PackedWeightKG)
	plan.Summary.PackedVolumeCM3 = round(plan.Summary.PackedVolumeCM3)
	return plan, nil
}

func selectCartonCandidates(items []unit, carton CartonSpec) []unit {
	capacityVolume := carton.LengthCM * carton.WidthCM * carton.HeightCM
	usedVolume, usedWeight := 0.0, 0.0
	result := make([]unit, 0, len(items))
	for _, item := range items {
		volume := item.dimensions.LengthCM * item.dimensions.WidthCM * item.dimensions.HeightCM
		if usedVolume+volume > capacityVolume+0.000001 || usedWeight+item.weightKG > carton.MaxWeightKG+0.000001 {
			continue
		}
		result = append(result, item)
		usedVolume += volume
		usedWeight += item.weightKG
	}
	return result
}

func packCarton(index int, candidates []unit, carton CartonSpec) (CartonPlan, error) {
	result := CartonPlan{Index: index, Placements: make([]Placement, 0, len(candidates))}
	packer := bp3d.NewPacker()
	bin := bp3d.NewBin(fmt.Sprintf("carton-%d", index), carton.LengthCM, carton.HeightCM, carton.WidthCM, carton.MaxWeightKG)
	packer.AddBin(bin)
	byID := make(map[string]unit, len(candidates))
	for _, item := range candidates {
		byID[item.id] = item
		packer.AddItem(bp3d.NewItem(item.id, item.dimensions.LengthCM, item.dimensions.HeightCM, item.dimensions.WidthCM, item.weightKG))
	}
	err := packer.Pack()
	if err != nil && !errors.Is(err, bp3d.ErrUnfitItemsExist) {
		return result, fmt.Errorf("calculate carton %d: %w", index, err)
	}
	for step, packed := range bin.Items {
		item, ok := byID[packed.Name]
		if !ok {
			return result, fmt.Errorf("calculate carton %d: unknown packed item %s", index, packed.Name)
		}
		dimension := packed.GetDimension()
		result.Placements = append(result.Placements, Placement{
			Step:               step + 1,
			UnitID:             item.id,
			WarehouseSKU:       item.sku,
			ProductName:        item.productName,
			Position:           Position{X: round(packed.Position[0]), Y: round(packed.Position[1]), Z: round(packed.Position[2])},
			Dimensions:         Dimensions{LengthCM: round(dimension[0]), WidthCM: round(dimension[2]), HeightCM: round(dimension[1])},
			OriginalDimensions: item.dimensions,
			WeightKG:           round(item.weightKG),
			Rotation:           rotationName(packed.RotationType),
		})
		result.UsedWeightKG += item.weightKG
		result.UsedVolumeCM3 += item.dimensions.LengthCM * item.dimensions.WidthCM * item.dimensions.HeightCM
	}
	result.PackedUnits = len(result.Placements)
	result.UsedWeightKG = round(result.UsedWeightKG)
	result.UsedVolumeCM3 = round(result.UsedVolumeCM3)
	result.VolumeUtilizationPercent = round(result.UsedVolumeCM3 * 100 / (carton.LengthCM * carton.WidthCM * carton.HeightCM))
	return result, nil
}

func fitsCarton(item Dimensions, carton CartonSpec) bool {
	dimensions := [3]float64{item.LengthCM, item.WidthCM, item.HeightCM}
	container := [3]float64{carton.LengthCM, carton.WidthCM, carton.HeightCM}
	permutations := [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	for _, permutation := range permutations {
		if dimensions[permutation[0]] <= container[0]+0.000001 && dimensions[permutation[1]] <= container[1]+0.000001 && dimensions[permutation[2]] <= container[2]+0.000001 {
			return true
		}
	}
	return false
}

func sortUnits(items []unit) {
	sort.SliceStable(items, func(i, j int) bool {
		leftVolume := items[i].dimensions.LengthCM * items[i].dimensions.WidthCM * items[i].dimensions.HeightCM
		rightVolume := items[j].dimensions.LengthCM * items[j].dimensions.WidthCM * items[j].dimensions.HeightCM
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
		Dimensions:   item.dimensions,
		WeightKG:     round(item.weightKG),
		ReasonCode:   code,
		Reason:       reason,
	}
}

func rotationName(rotation bp3d.RotationType) string {
	names := [...]string{"WHD", "HWD", "HDW", "DHW", "DWH", "WDH"}
	if int(rotation) < 0 || int(rotation) >= len(names) {
		return "UNKNOWN"
	}
	return names[rotation]
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func round(value float64) float64 {
	return math.Round(value*1000) / 1000
}
