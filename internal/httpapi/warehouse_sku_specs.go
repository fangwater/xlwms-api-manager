package httpapi

import (
	"context"
	"net/http"
	"strings"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
)

type warehouseSKUSpecRequest struct {
	WarehouseSKU string   `json:"warehouse_sku"`
	ProductName  string   `json:"product_name"`
	LengthCM     *float64 `json:"length_cm"`
	WidthCM      *float64 `json:"width_cm"`
	HeightCM     *float64 `json:"height_cm"`
	WeightKG     *float64 `json:"weight_kg"`
	Note         string   `json:"note"`
	Enabled      *bool    `json:"enabled"`
}

func (s *Server) listWarehouseSKUSpecs(writer http.ResponseWriter, request *http.Request) {
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, total, err := s.store.ListWarehouseSKUSpecs(ctx, store.WarehouseSKUSpecFilter{
		Query: request.URL.Query().Get("q"), Status: request.URL.Query().Get("status"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writePage(writer, items, total, page, pageSize)
}
func (s *Server) saveWarehouseSKUSpec(writer http.ResponseWriter, request *http.Request) {
	var payload warehouseSKUSpecRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	s.persistWarehouseSKUSpec(writer, request, payload, enabled)
}

func (s *Server) updateWarehouseSKUSpec(writer http.ResponseWriter, request *http.Request) {
	var payload warehouseSKUSpecRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	warehouseSKU := strings.TrimSpace(request.PathValue("warehouseSKU"))
	if supplied := strings.TrimSpace(payload.WarehouseSKU); supplied != "" && supplied != warehouseSKU {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "warehouse_sku cannot be changed"})
		return
	}
	if payload.Enabled == nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "enabled is required for an update"})
		return
	}
	payload.WarehouseSKU = warehouseSKU
	s.persistWarehouseSKUSpec(writer, request, payload, *payload.Enabled)
}

func (s *Server) updateWarehouseSKUPackageSpec(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		LengthCM *float64 `json:"length_cm"`
		WidthCM  *float64 `json:"width_cm"`
		HeightCM *float64 `json:"height_cm"`
		WeightKG *float64 `json:"weight_kg"`
	}
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if payload.LengthCM == nil || payload.WidthCM == nil || payload.HeightCM == nil || payload.WeightKG == nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "length_cm, width_cm, height_cm and weight_kg are required"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.UpdateWarehouseSKUPackageSpec(ctx, request.PathValue("warehouseSKU"), payload.LengthCM, payload.WidthCM, payload.HeightCM, payload.WeightKG)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) persistWarehouseSKUSpec(writer http.ResponseWriter, request *http.Request, payload warehouseSKUSpecRequest, enabled bool) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.UpsertWarehouseSKUSpec(ctx, model.WarehouseSKUSpec{
		WarehouseSKU: payload.WarehouseSKU, ProductName: payload.ProductName, LengthCM: payload.LengthCM,
		WidthCM: payload.WidthCM, HeightCM: payload.HeightCM, WeightKG: payload.WeightKG,
		Note: payload.Note, Enabled: enabled,
	})
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) resolveWarehouseSKUSpecs(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Items []model.WarehouseSKUQuantity `json:"items"`
	}
	if !decodeJSON(writer, request, &payload) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	result, err := s.store.ResolveWarehouseSKUSpecs(ctx, payload.Items)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: result})
}
