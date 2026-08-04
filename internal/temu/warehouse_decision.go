package temu

import (
	"fmt"
	"strconv"
	"time"

	"xlwms-api-manager/internal/model"
)

const (
	RuleVersion    = "2026-08-04-configurable-sku-thresholds"
	RegionEast     = "east"
	RegionWest     = "west"
	QuerySucceeded = "succeeded"
	QueryFailed    = "failed"
	QueryInactive  = "inactive"
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
	Name            string
	Active          bool
	QueryStatus     string
	SKUFound        bool
	AvailableAmount float64
	QueriedAt       *time.Time
}

type WarehouseDecision struct {
	WarehouseKey    string     `json:"warehouse_key"`
	WarehouseCode   string     `json:"wh_code"`
	WarehouseName   string     `json:"warehouse_name"`
	Region          string     `json:"region"`
	Provider        string     `json:"provider"`
	Active          bool       `json:"active"`
	QueryStatus     string     `json:"query_status"`
	SKUFound        bool       `json:"sku_found"`
	AvailableAmount float64    `json:"available_amount"`
	Selectable      bool       `json:"selectable"`
	Recommended     bool       `json:"recommended"`
	InventoryAt     *time.Time `json:"inventory_queried_at,omitempty"`
	ReasonCode      string     `json:"reason_code"`
	Reason          string     `json:"reason"`
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
	regions := []RegionDecision{
		buildRegionDecision(RegionEast, inventory, thresholds.EastThreshold),
		buildRegionDecision(RegionWest, inventory, thresholds.WestThreshold),
	}
	result := SKUDecision{SKU: sku, ManualRegions: make([]string, 0), RegionDecisions: regions, Thresholds: thresholds}
	for _, region := range regions {
		result.TotalAvailableAmount += region.AvailableAmount
		if region.RequiresManual {
			result.RequiresManual = true
			result.ManualRegions = append(result.ManualRegions, region.Region)
		}
	}
	if result.TotalAvailableAmount <= thresholds.TotalThreshold {
		result.RequiresManual = true
		if len(result.ManualRegions) == 0 {
			result.ManualRegions = []string{RegionEast, RegionWest}
		}
		result.DecisionCode = "MANUAL_LOW_TOTAL_STOCK"
		result.Reason = fmt.Sprintf("美东和美西正品产品可用库存合计%s，小于等于该SKU总库存安全线%s，保留库存并转人工处理", formatAmount(result.TotalAvailableAmount), formatAmount(thresholds.TotalThreshold))
		return result
	}
	if result.RequiresManual {
		result.DecisionCode = "MANUAL_REVIEW_REQUIRED"
		result.Reason = "至少一个区域库存不足或查询不完整，该SKU订单需要人工处理"
	} else {
		result.DecisionCode = "AUTO_SELECTION_READY"
		result.Reason = "美东和美西库存均高于安全线，可按区域推荐仓自动选择"
	}
	return result
}

func buildRegionDecision(region string, inventory map[string]WarehouseInventory, safetyStockThreshold float64) RegionDecision {
	rules := rulesForRegion(region)
	result := RegionDecision{Region: region, RegionName: rules[0].RegionName, SafetyStockThreshold: safetyStockThreshold, Warehouses: make([]WarehouseDecision, 0, len(rules))}
	queryIncomplete := false
	for _, rule := range rules {
		current := inventory[rule.Code]
		name := current.Name
		if name == "" {
			name = rule.FallbackName
		}
		warehouse := WarehouseDecision{
			WarehouseKey: rule.Key, WarehouseCode: rule.Code, WarehouseName: name, Region: rule.Region, Provider: rule.Provider,
			Active: current.Active, QueryStatus: current.QueryStatus, SKUFound: current.SKUFound,
			AvailableAmount: current.AvailableAmount, InventoryAt: current.QueriedAt,
		}
		switch {
		case !current.Active || current.QueryStatus == QueryInactive:
			warehouse.ReasonCode = "WAREHOUSE_NOT_ACTIVE"
			warehouse.Reason = "仓库未启用或未配置，不能用于发货"
			queryIncomplete = true
		case current.QueryStatus != QuerySucceeded:
			warehouse.ReasonCode = "INVENTORY_QUERY_FAILED"
			warehouse.Reason = "OMS库存查询失败，不能用于自动发货"
			queryIncomplete = true
		case current.AvailableAmount <= 0:
			warehouse.ReasonCode = "ZERO_AVAILABLE_STOCK"
			if current.SKUFound {
				warehouse.Reason = "正品产品可用库存小于等于0，不能选择该仓发货"
			} else {
				warehouse.Reason = "OMS未返回该SKU的正品产品库存，可用库存按0处理，不能选择该仓发货"
			}
		default:
			warehouse.Selectable = true
			warehouse.ReasonCode = "AVAILABLE_STOCK"
			warehouse.Reason = "正品产品可用库存大于0，可作为候选仓"
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
	if result.AvailableAmount <= safetyStockThreshold {
		result.RequiresManual = true
		result.DecisionCode = "MANUAL_LOW_REGIONAL_STOCK"
		result.Reason = fmt.Sprintf("%s两仓正品产品可用库存合计%s，小于等于该SKU安全线%s，保留库存并转人工处理", result.RegionName, formatAmount(result.AvailableAmount), formatAmount(safetyStockThreshold))
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
