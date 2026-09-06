package temu

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"xlwms-api-manager/internal/model"
)

const (
	RuleVersion     = "2026-09-06-api-scope-account-rules"
	RegionEast      = "east"
	RegionWest      = "west"
	QuerySucceeded  = "succeeded"
	QueryFailed     = "failed"
	QueryInactive   = "inactive"
	QueryOutOfScope = "out_of_scope"
)

type WarehouseRule struct {
	Key          string
	Code         string
	FallbackName string
	Region       string
	RegionName   string
	Provider     string
	Preferred    bool
}

var warehouseRules = []WarehouseRule{
	{Key: "DPS002", Code: "DPSNY002", FallbackName: "DPS达派思-纽约", Region: RegionEast, RegionName: "美东", Provider: "DPS", Preferred: true},
	{Key: "ARP_EAST", Code: "HYTX30", FallbackName: "ARP1号仓-美东PA", Region: RegionEast, RegionName: "美东", Provider: "ARP"},
	{Key: "DPS004", Code: "DPSCA004", FallbackName: "DPS达派思-加州", Region: RegionWest, RegionName: "美西", Provider: "DPS", Preferred: true},
	{Key: "ARP_WEST", Code: "ARPCA01", FallbackName: "ARP8号仓-美西LA", Region: RegionWest, RegionName: "美西", Provider: "ARP"},
}

type WarehouseInventory struct {
	APIBinding          *WarehouseAPIBinding
	Name                string
	Active              bool
	QueryStatus         string
	SKUFound            bool
	AvailableAmount     float64
	RawAvailableAmount  float64
	Corrected           bool
	CorrectionMode      string
	CorrectionAmount    float64
	CorrectionNote      string
	CorrectionUpdatedAt *time.Time
	QueriedAt           *time.Time
}

type WarehouseDecision struct {
	APIBinding          *WarehouseAPIBinding `json:"api_binding,omitempty"`
	WarehouseKey        string               `json:"warehouse_key"`
	WarehouseCode       string               `json:"wh_code"`
	WarehouseName       string               `json:"warehouse_name"`
	Region              string               `json:"region"`
	Provider            string               `json:"provider"`
	Active              bool                 `json:"active"`
	QueryStatus         string               `json:"query_status"`
	SKUFound            bool                 `json:"sku_found"`
	AvailableAmount     float64              `json:"available_amount"`
	RawAvailableAmount  float64              `json:"raw_available_amount"`
	Corrected           bool                 `json:"corrected"`
	CorrectionMode      string               `json:"correction_mode,omitempty"`
	CorrectionAmount    float64              `json:"correction_amount"`
	CorrectionNote      string               `json:"correction_note,omitempty"`
	CorrectionUpdatedAt *time.Time           `json:"correction_updated_at,omitempty"`
	Selectable          bool                 `json:"selectable"`
	Recommended         bool                 `json:"recommended"`
	InventoryAt         *time.Time           `json:"inventory_queried_at,omitempty"`
	ReasonCode          string               `json:"reason_code"`
	Reason              string               `json:"reason"`
	PlatformSKUDisabled bool                 `json:"platform_sku_disabled,omitempty"`
}

func ApplyPlatformSKUWarehouseRestrictions(decision *SKUDecision, disabled map[string]bool) {
	if decision == nil || len(disabled) == 0 {
		return
	}
	for regionIndex := range decision.RegionDecisions {
		region := &decision.RegionDecisions[regionIndex]
		for warehouseIndex := range region.Warehouses {
			warehouse := &region.Warehouses[warehouseIndex]
			if !disabled[warehouse.WarehouseKey] {
				continue
			}
			warehouse.Selectable = false
			warehouse.Recommended = false
			warehouse.PlatformSKUDisabled = true
			warehouse.ReasonCode = "PLATFORM_SKU_WAREHOUSE_DISABLED"
			warehouse.Reason = fmt.Sprintf("平台已禁止 SKU %s 使用此仓库", decision.SKU)
		}
		recommendRegionAfterRestriction(region)
	}
}

func recommendRegionAfterRestriction(region *RegionDecision) {
	if region == nil {
		return
	}
	region.RecommendedWarehouseKey, region.RecommendedWarehouse, region.RecommendedName = "", "", ""
	for index := range region.Warehouses {
		region.Warehouses[index].Recommended = false
	}
	for index := range region.Warehouses {
		if !region.Warehouses[index].Selectable {
			continue
		}
		region.Warehouses[index].Recommended = true
		setRecommended(region, region.Warehouses[index])
		region.RequiresManual = false
		region.DecisionCode = "PLATFORM_SKU_WAREHOUSE_POLICY_APPLIED"
		region.Reason = region.RegionName + "已按平台 SKU 可发仓规则选择可用仓库"
		return
	}
	region.RequiresManual = true
	region.DecisionCode = "MANUAL_PLATFORM_SKU_WAREHOUSE_DISABLED"
	region.Reason = region.RegionName + "没有符合平台 SKU 可发仓规则的可选仓库"
}

type RegionDecision struct {
	Region                  string              `json:"region"`
	RegionName              string              `json:"region_name"`
	AvailableAmount         float64             `json:"available_amount"`
	SafetyStockThreshold    float64             `json:"safety_stock_threshold"`
	RequiresManual          bool                `json:"requires_manual"`
	RecommendedWarehouseKey string              `json:"recommended_warehouse_key,omitempty"`
	RecommendedWarehouse    string              `json:"recommended_wh_code,omitempty"`
	RecommendedName         string              `json:"recommended_warehouse_name,omitempty"`
	DecisionCode            string              `json:"decision_code"`
	Reason                  string              `json:"reason"`
	Warehouses              []WarehouseDecision `json:"warehouses"`
}

type SKUDecision struct {
	SKU                  string                    `json:"sku"`
	RequiresManual       bool                      `json:"requires_manual"`
	ManualRegions        []string                  `json:"manual_regions"`
	DecisionCode         string                    `json:"decision_code"`
	Reason               string                    `json:"reason"`
	TotalAvailableAmount float64                   `json:"total_available_amount"`
	Thresholds           model.InventoryThresholds `json:"thresholds"`
	RegionDecisions      []RegionDecision          `json:"regions"`
}

func WarehouseRules() []WarehouseRule {
	result := make([]WarehouseRule, len(warehouseRules))
	copy(result, warehouseRules)
	return result
}

func WarehouseCodes(region string) []string {
	rules := rulesForRegion(region)
	result := make([]string, 0, len(rules))
	for _, rule := range rules {
		result = append(result, rule.Code)
	}
	return result
}

func BuildSKUDecision(sku string, inventory map[string]WarehouseInventory, thresholds model.InventoryThresholds) SKUDecision {
	if len(thresholds.WarehouseCodes) > 0 {
		scoped := make(map[string]WarehouseInventory, len(warehouseRules))
		for _, rule := range warehouseRules {
			if slices.Contains(thresholds.WarehouseCodes, rule.Code) {
				scoped[rule.Code] = inventory[rule.Code]
				if scoped[rule.Code].QueryStatus == QueryOutOfScope {
					scoped[rule.Code] = WarehouseInventory{QueryStatus: QueryInactive}
				}
			} else {
				scoped[rule.Code] = WarehouseInventory{QueryStatus: QueryOutOfScope}
			}
		}
		inventory = scoped
	}
	scopeLabel := "可用仓库"
	if len(thresholds.WarehouseCodes) > 0 {
		scopeLabel = fmt.Sprintf("指定%d仓", len(thresholds.WarehouseCodes))
	}
	regions := []RegionDecision{
		buildRegionDecision(RegionEast, inventory),
		buildRegionDecision(RegionWest, inventory),
	}
	result := SKUDecision{SKU: sku, ManualRegions: make([]string, 0), RegionDecisions: regions, Thresholds: thresholds}
	queryIncomplete := false
	for _, region := range regions {
		result.TotalAvailableAmount += region.AvailableAmount
		if region.DecisionCode == "MANUAL_INVENTORY_QUERY_INCOMPLETE" {
			queryIncomplete = true
			result.ManualRegions = append(result.ManualRegions, region.Region)
		}
	}
	if queryIncomplete {
		result.RequiresManual = true
		result.DecisionCode = "MANUAL_INVENTORY_QUERY_INCOMPLETE"
		result.Reason = "至少一个范围内仓库未启用或库存查询失败，无法确认库存总量，转人工处理"
		return result
	}
	if result.TotalAvailableAmount < thresholds.TotalThreshold || (thresholds.TotalInclusive && result.TotalAvailableAmount == thresholds.TotalThreshold) {
		result.RequiresManual = true
		if len(result.ManualRegions) == 0 {
			result.ManualRegions = []string{RegionEast, RegionWest}
		}
		result.DecisionCode = "MANUAL_LOW_TOTAL_STOCK"
		comparison := "小于"
		if thresholds.TotalInclusive {
			comparison = "小于等于"
		}
		result.Reason = fmt.Sprintf("%s正品产品可用库存合计%s，%s该SKU总库存安全线%s，保留库存并转人工处理", scopeLabel, formatAmount(result.TotalAvailableAmount), comparison, formatAmount(thresholds.TotalThreshold))
		return result
	}
	result.DecisionCode = "AUTO_SELECTION_READY"
	result.Reason = scopeLabel + "正品产品可用库存合计通过安全线，可从有货仓自动选择"
	return result
}

func buildRegionDecision(region string, inventory map[string]WarehouseInventory) RegionDecision {
	rules := rulesForRegion(region)
	result := RegionDecision{Region: region, RegionName: rules[0].RegionName, Warehouses: make([]WarehouseDecision, 0, len(rules))}
	queryIncomplete := false
	for _, rule := range rules {
		current := inventory[rule.Code]
		name := current.Name
		if name == "" {
			name = rule.FallbackName
		}
		warehouse := WarehouseDecision{
			APIBinding:   current.APIBinding,
			WarehouseKey: rule.Key, WarehouseCode: rule.Code, WarehouseName: name, Region: rule.Region, Provider: rule.Provider,
			Active: current.Active, QueryStatus: current.QueryStatus, SKUFound: current.SKUFound,
			AvailableAmount: current.AvailableAmount, RawAvailableAmount: current.RawAvailableAmount,
			Corrected: current.Corrected, CorrectionNote: current.CorrectionNote,
			CorrectionMode: current.CorrectionMode, CorrectionAmount: current.CorrectionAmount,
			CorrectionUpdatedAt: current.CorrectionUpdatedAt, InventoryAt: current.QueriedAt,
		}
		switch {
		case current.QueryStatus == QueryOutOfScope:
			warehouse.ReasonCode = "WAREHOUSE_OUT_OF_API_SCOPE"
			warehouse.Reason = "不在该 SKU 的 API 库存范围内"
		case !current.Active || current.QueryStatus == QueryInactive:
			warehouse.ReasonCode = "WAREHOUSE_NOT_ACTIVE"
			warehouse.Reason = "仓库未启用或未配置，不能用于发货"
			queryIncomplete = true
		case current.QueryStatus != QuerySucceeded:
			warehouse.ReasonCode = "INVENTORY_QUERY_FAILED"
			warehouse.Reason = "OMS库存查询失败，不能用于自动发货"
			queryIncomplete = true
		case current.AvailableAmount <= 0:
			if current.Corrected {
				warehouse.ReasonCode = "CORRECTED_ZERO_AVAILABLE_STOCK"
				warehouse.Reason = "库存修正值为0，不能选择该仓发货"
				break
			}
			warehouse.ReasonCode = "ZERO_AVAILABLE_STOCK"
			if current.SKUFound {
				warehouse.Reason = "正品产品可用库存小于等于0，不能选择该仓发货"
			} else {
				warehouse.Reason = "OMS未返回该SKU的正品产品库存，可用库存按0处理，不能选择该仓发货"
			}
		default:
			warehouse.Selectable = true
			if current.Corrected {
				warehouse.ReasonCode = "CORRECTED_AVAILABLE_STOCK"
				warehouse.Reason = "库存修正值大于0，可作为候选仓"
			} else {
				warehouse.ReasonCode = "AVAILABLE_STOCK"
				warehouse.Reason = "正品产品可用库存大于0，可作为候选仓"
			}
			result.AvailableAmount += current.AvailableAmount
		}
		result.Warehouses = append(result.Warehouses, warehouse)
	}
	if queryIncomplete {
		result.RequiresManual = true
		result.DecisionCode = "MANUAL_INVENTORY_QUERY_INCOMPLETE"
		result.Reason = result.RegionName + "存在未启用仓或库存查询失败，无法安全自动选仓，转人工处理"
		return result
	}
	preferredIndex, fallbackIndex := -1, -1
	for index, rule := range rules {
		if rule.Preferred {
			preferredIndex = index
		} else {
			fallbackIndex = index
		}
	}
	switch {
	case preferredIndex >= 0 && result.Warehouses[preferredIndex].Selectable:
		result.Warehouses[preferredIndex].Recommended = true
		setRecommended(&result, result.Warehouses[preferredIndex])
		if fallbackIndex >= 0 && result.Warehouses[fallbackIndex].Selectable {
			result.DecisionCode = "DPS_PRIORITY_CLEAR_STOCK"
			result.Reason = result.RegionName + "两仓均有可用库存且区域库存高于安全线，优先选择DPS仓清理DPS库存"
		} else {
			result.DecisionCode = "DPS_ONLY_AVAILABLE"
			result.Reason = result.RegionName + "区域库存高于安全线，当前仅DPS仓有可用库存，选择DPS仓"
		}
	case fallbackIndex >= 0 && result.Warehouses[fallbackIndex].Selectable:
		result.Warehouses[fallbackIndex].Recommended = true
		setRecommended(&result, result.Warehouses[fallbackIndex])
		result.DecisionCode = "ARP_FALLBACK_DPS_OUT_OF_STOCK"
		result.Reason = result.RegionName + "区域库存高于安全线，但DPS仓可用库存为0，回退选择ARP仓"
	default:
		result.RequiresManual = true
		result.DecisionCode = "MANUAL_NO_SELECTABLE_WAREHOUSE"
		result.Reason = result.RegionName + "没有可选择的发货仓，转人工处理"
	}
	return result
}

func setRecommended(region *RegionDecision, warehouse WarehouseDecision) {
	region.RecommendedWarehouseKey = warehouse.WarehouseKey
	region.RecommendedWarehouse = warehouse.WarehouseCode
	region.RecommendedName = warehouse.WarehouseName
}

func rulesForRegion(region string) []WarehouseRule {
	result := make([]WarehouseRule, 0, 2)
	for _, rule := range warehouseRules {
		if rule.Region == region {
			result = append(result, rule)
		}
	}
	return result
}

func formatAmount(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
