package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/temu"
)

const maxTemuQuerySKUs = 100

type temuWarehouseQueryRequest struct {
	SKU   string                       `json:"sku"`
	SKUs  []string                     `json:"skus"`
	Items []model.WarehouseSKUQuantity `json:"items"`
}

type temuWarehouseQueryResponse struct {
	Complete             bool                             `json:"complete"`
	RuleVersion          string                           `json:"rule_version"`
	SafetyStockThreshold float64                          `json:"safety_stock_threshold"`
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
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	packageResolution, err := s.store.ResolveWarehouseSKUSpecs(ctx, items)
	if err != nil {
		s.internalError(writer, "resolve warehouse SKU package specs", err)
		return
	}
	warehouses, err := s.store.ActiveWarehouseCredentials(ctx)
	if err != nil {
		s.internalError(writer, "load active warehouses for Temu inventory query", err)
		return
	}
	queriedAt := time.Now()
	inventory := temu.QueryLiveInventory(ctx, warehouses, skus, s.requestTimeout, queriedAt)
	records := make([]temu.SKUDecision, 0, len(skus))
	for _, sku := range skus {
		records = append(records, temu.BuildSKUDecision(sku, inventory.InventoryBySKU[sku]))
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: temuWarehouseQueryResponse{
		Complete:             inventory.Complete,
		RuleVersion:          temu.RuleVersion,
		SafetyStockThreshold: temu.SafetyStockThreshold,
		InventoryBasis:       "XLWMS实时综合库存中的正品产品可用库存（stockType=0, productStockDtl.availableAmount）",
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
