package httpapi

import (
	"context"
	"net/http"
	"strings"

	"xlwms-api-manager/internal/model"
)

type carrierPolicyRequest struct {
	Carriers []model.CarrierPolicy `json:"carriers"`
}

type skuFulfillmentPolicyRequest struct {
	DisabledWarehouseKeys []string `json:"disabled_warehouse_keys"`
}

type skuFulfillmentPolicyQueryRequest struct {
	Platform string   `json:"platform"`
	SKUs     []string `json:"skus"`
}

func (s *Server) listCarrierPolicies(writer http.ResponseWriter, request *http.Request) {
	platform, ok := requiredThresholdPlatform(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, err := s.store.CarrierPolicies(ctx, platform, request.URL.Query().Get("warehouse_sku"))
	if err != nil {
		s.internalError(writer, "list carrier policies", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: items})
}

func (s *Server) updateCarrierPolicies(writer http.ResponseWriter, request *http.Request) {
	platform, ok := requiredThresholdPlatform(writer, request)
	if !ok {
		return
	}
	var payload carrierPolicyRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.ReplaceCarrierPolicies(ctx, platform, request.URL.Query().Get("warehouse_sku"), request.PathValue("warehouseKey"), payload.Carriers)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) resetSKUCarrierPolicies(writer http.ResponseWriter, request *http.Request) {
	platform, ok := requiredThresholdPlatform(writer, request)
	if !ok {
		return
	}
	warehouseSKU := strings.TrimSpace(request.URL.Query().Get("warehouse_sku"))
	if warehouseSKU == "" {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "warehouse_sku is required"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	if err := s.store.ResetSKUCarrierPolicies(ctx, platform, warehouseSKU, request.PathValue("warehouseKey")); err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]bool{"deleted": true}})
}

func (s *Server) listSKUFulfillmentPolicies(writer http.ResponseWriter, request *http.Request) {
	platform, ok := requiredThresholdPlatform(writer, request)
	if !ok {
		return
	}
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, total, err := s.store.ListPlatformSKUFulfillmentPolicies(ctx, platform, request.URL.Query().Get("q"), page, pageSize)
	if err != nil {
		s.internalError(writer, "list SKU fulfillment policies", err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]any{
		"records": items, "total": total, "page": page, "page_size": pageSize, "pages": pages,
		"platform": platform,
	}})
}

func (s *Server) updateSKUFulfillmentPolicy(writer http.ResponseWriter, request *http.Request) {
	platform, ok := requiredThresholdPlatform(writer, request)
	if !ok {
		return
	}
	var payload skuFulfillmentPolicyRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.ReplacePlatformSKUDisabledWarehouses(ctx, platform, request.PathValue("warehouseSKU"), payload.DisabledWarehouseKeys)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) querySKUFulfillmentPolicies(writer http.ResponseWriter, request *http.Request) {
	var payload skuFulfillmentPolicyQueryRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if len(payload.SKUs) > 100 {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "no more than 100 SKUs can be queried at once"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, err := s.store.DisabledWarehousesForPlatformSKUs(ctx, payload.Platform, payload.SKUs)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: items})
}
