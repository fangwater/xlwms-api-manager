package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/temu"
)

const maxTemuQuerySKUs = 100

type temuWarehouseQueryRequest struct {
	SKU      string                       `json:"sku"`
	SKUs     []string                     `json:"skus"`
	Items    []model.WarehouseSKUQuantity `json:"items"`
	Platform string                       `json:"platform"`
	ShopCode string                       `json:"shop_code"`
}

type temuWarehouseQueryResponse struct {
	Complete             bool                             `json:"complete"`
	RuleVersion          string                           `json:"rule_version"`
	SafetyStockThreshold float64                          `json:"safety_stock_threshold"`
	DefaultThresholds    model.InventoryThresholds        `json:"default_thresholds"`
	InventoryBasis       string                           `json:"inventory_basis"`
	InventoryWindowStart string                           `json:"inventory_window_start"`
	InventoryWindowEnd   string                           `json:"inventory_window_end"`
	QueriedAt            time.Time                        `json:"queried_at"`
	WarehouseQueries     []temu.WarehouseQuery            `json:"warehouse_queries"`
	Records              []temu.SKUDecision               `json:"records"`
	PackageResolution    model.WarehouseSKUSpecResolution `json:"package_resolution"`
}

func (s *Server) temuWarehouseAvailability(writer http.ResponseWriter, request *http.Request) {
	var payload temuWarehouseQueryRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	skus, items, err := normalizeTemuRequest(payload)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	platform, _, err := requestedDecisionShop(request, payload.Platform, payload.ShopCode)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	packageResolution, err := s.store.ResolveWarehouseSKUSpecs(ctx, items)
	if err != nil {
		s.internalError(writer, "resolve warehouse SKU package specs", err)
		return
	}
	resolvedBySKU := make(map[string]string, len(skus))
	for _, sku := range skus {
		resolvedBySKU[sku] = sku
	}
	for _, item := range packageResolution.Items {
		if item.Complete && item.MatchedWarehouseSKU != "" {
			resolvedBySKU[item.WarehouseSKU] = item.MatchedWarehouseSKU
		}
	}
	inventorySKUs := make([]string, 0, len(skus))
	seenInventorySKU := make(map[string]struct{}, len(skus))
	for _, sku := range skus {
		resolved := resolvedBySKU[sku]
		if _, exists := seenInventorySKU[resolved]; exists {
			continue
		}
		seenInventorySKU[resolved] = struct{}{}
		inventorySKUs = append(inventorySKUs, resolved)
	}
	warehouses, err := s.store.ActiveWarehouseCredentials(ctx)
	if err != nil {
		s.internalError(writer, "load active warehouses for Temu inventory query", err)
		return
	}
	queriedAt := time.Now()
	inventory := temu.QueryLiveInventory(ctx, warehouses, inventorySKUs, s.requestTimeout, queriedAt)
	corrections, err := s.store.InventoryCorrectionsForSKUs(ctx, inventorySKUs)
	if err != nil {
		s.internalError(writer, "load inventory corrections", err)
		return
	}
	temu.ApplyInventoryCorrections(&inventory, corrections)
	thresholdsBySKU, defaultThresholds, err := s.store.InventoryThresholdsForPlatformSKUs(ctx, platform, inventorySKUs)
	if err != nil {
		if isShopIdentityError(err) {
			writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
			return
		}
		s.internalError(writer, "resolve SKU inventory thresholds", err)
		return
	}
	disabledBySKU, err := s.store.DisabledWarehousesForPlatformSKUs(ctx, platform, inventorySKUs)
	if err != nil {
		s.internalError(writer, "resolve platform SKU warehouse policies", err)
		return
	}
	records := make([]temu.SKUDecision, 0, len(skus))
	for _, sku := range skus {
		resolvedSKU := resolvedBySKU[sku]
		record := temu.BuildSKUDecision(sku, inventory.InventoryBySKU[resolvedSKU], thresholdsBySKU[resolvedSKU])
		temu.ApplyPlatformSKUWarehouseRestrictions(&record, disabledBySKU[resolvedSKU])
		records = append(records, record)
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: temuWarehouseQueryResponse{
		Complete:             inventory.Complete,
		RuleVersion:          temu.RuleVersion,
		SafetyStockThreshold: defaultThresholds.TotalThreshold,
		DefaultThresholds:    defaultThresholds,
		InventoryBasis:       "XLWMS实时综合库存中的正品产品可用库存；存在仓库+SKU修正时使用修正后的可用库存",
		InventoryWindowStart: inventory.WindowStart,
		InventoryWindowEnd:   inventory.WindowEnd,
		QueriedAt:            queriedAt,
		WarehouseQueries:     inventory.WarehouseQueries,
		Records:              records,
		PackageResolution:    packageResolution,
	}})
}

func normalizeTemuRequest(payload temuWarehouseQueryRequest) ([]string, []model.WarehouseSKUQuantity, error) {
	if len(payload.Items) == 0 {
		skus, err := normalizeTemuSKUs(payload.SKU, payload.SKUs)
		if err != nil {
			return nil, nil, err
		}
		items := make([]model.WarehouseSKUQuantity, 0, len(skus))
		for _, sku := range skus {
			items = append(items, model.WarehouseSKUQuantity{WarehouseSKU: sku, Quantity: 1})
		}
		return skus, items, nil
	}
	rawSKUs := make([]string, 0, len(payload.Items))
	quantities := make(map[string]int, len(payload.Items))
	for _, item := range payload.Items {
		sku := strings.TrimSpace(item.WarehouseSKU)
		if item.Quantity <= 0 {
			return nil, nil, errors.New("item quantity must be positive")
		}
		rawSKUs = append(rawSKUs, sku)
		quantities[sku] += item.Quantity
	}
	skus, err := normalizeTemuSKUs("", rawSKUs)
	if err != nil {
		return nil, nil, err
	}
	items := make([]model.WarehouseSKUQuantity, 0, len(skus))
	for _, sku := range skus {
		items = append(items, model.WarehouseSKUQuantity{WarehouseSKU: sku, Quantity: quantities[sku]})
	}
	return skus, items, nil
}

func normalizeTemuSKUs(single string, values []string) ([]string, error) {
	all := make([]string, 0, len(values)+1)
	if strings.TrimSpace(single) != "" {
		all = append(all, single)
	}
	all = append(all, values...)
	result := make([]string, 0, len(all))
	seen := make(map[string]struct{}, len(all))
	for _, raw := range all {
		sku := strings.TrimSpace(raw)
		if sku == "" {
			continue
		}
		if strings.Contains(sku, ",") {
			return nil, errors.New("SKU cannot contain commas")
		}
		if len(sku) > 255 {
			return nil, errors.New("SKU cannot exceed 255 characters")
		}
		if _, exists := seen[sku]; exists {
			continue
		}
		seen[sku] = struct{}{}
		result = append(result, sku)
	}
	if len(result) == 0 {
		return nil, errors.New("sku or skus is required")
	}
	if len(result) > maxTemuQuerySKUs {
		return nil, errors.New("no more than 100 SKUs can be queried at once")
	}
	return result, nil
}

func requestedDecisionShop(request *http.Request, bodyPlatform, bodyShop string) (string, string, error) {
	platform, shopCode, err := requestedShopIdentity(request)
	if err != nil {
		return "", "", err
	}
	bodyPlatform = strings.TrimSpace(bodyPlatform)
	bodyShop = strings.TrimSpace(bodyShop)
	if bodyPlatform == "" && bodyShop == "" {
		return platform, shopCode, nil
	}
	if bodyPlatform == "" || bodyShop == "" {
		return "", "", errors.New("platform and shop_code are required together")
	}
	normalizedPlatform, err := store.NormalizeFulfillmentPlatform(bodyPlatform)
	if err != nil {
		return "", "", err
	}
	normalizedShop, err := store.NormalizeFulfillmentShopCode(bodyShop)
	if err != nil {
		return "", "", err
	}
	if platform != "" && (platform != normalizedPlatform || shopCode != normalizedShop) {
		return "", "", errors.New("conflicting shop selectors")
	}
	return normalizedPlatform, normalizedShop, nil
}
