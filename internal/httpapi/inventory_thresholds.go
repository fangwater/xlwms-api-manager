package httpapi

import (
	"context"
	"errors"
	"math"
	"net/http"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/temu"
)

type inventoryThresholdRequest struct {
	EastThreshold  *float64 `json:"east_threshold"`
	WestThreshold  *float64 `json:"west_threshold"`
	TotalThreshold *float64 `json:"total_threshold"`
}

func (payload inventoryThresholdRequest) thresholds() (model.InventoryThresholds, error) {
	if payload.EastThreshold == nil || payload.WestThreshold == nil || payload.TotalThreshold == nil {
		return model.InventoryThresholds{}, errors.New("east_threshold, west_threshold and total_threshold are required")
	}
	values := []float64{*payload.EastThreshold, *payload.WestThreshold, *payload.TotalThreshold}
	for _, value := range values {
		if value < 0 || value > 1_000_000_000 || math.IsNaN(value) || math.IsInf(value, 0) {
			return model.InventoryThresholds{}, errors.New("inventory thresholds must be between 0 and 1000000000")
		}
	}
	return model.InventoryThresholds{EastThreshold: values[0], WestThreshold: values[1], TotalThreshold: values[2]}, nil
}

func (s *Server) listInventoryThresholds(writer http.ResponseWriter, request *http.Request) {
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, total, err := s.store.ListInventoryThresholds(ctx, store.InventoryThresholdFilter{
		Query: request.URL.Query().Get("q"), Page: page, PageSize: pageSize,
	}, temu.WarehouseCodes(temu.RegionEast), temu.WarehouseCodes(temu.RegionWest))
	if err != nil {
		s.internalError(writer, "list inventory thresholds", err)
		return
	}
	defaults, err := s.store.InventoryThresholdDefaults(ctx)
	if err != nil {
		s.internalError(writer, "load inventory threshold defaults", err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]any{
		"records": items, "total": total, "page": page, "page_size": pageSize, "pages": pages,
		"default_thresholds": defaults,
	}})
}

func (s *Server) inventoryThresholdDefaults(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.InventoryThresholdDefaults(ctx)
	if err != nil {
		s.internalError(writer, "load inventory threshold defaults", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) updateInventoryThresholdDefaults(writer http.ResponseWriter, request *http.Request) {
	thresholds, ok := decodeInventoryThresholds(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.UpdateInventoryThresholdDefaults(ctx, thresholds)
	if err != nil {
		s.internalError(writer, "update inventory threshold defaults", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) updateSKUInventoryThreshold(writer http.ResponseWriter, request *http.Request) {
	thresholds, ok := decodeInventoryThresholds(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.UpsertSKUInventoryThreshold(ctx, request.PathValue("warehouseSKU"), thresholds)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) deleteSKUInventoryThreshold(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	if err := s.store.DeleteSKUInventoryThreshold(ctx, request.PathValue("warehouseSKU")); err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]bool{"deleted": true}})
}

func decodeInventoryThresholds(writer http.ResponseWriter, request *http.Request) (model.InventoryThresholds, bool) {
	var payload inventoryThresholdRequest
	if !decodeJSON(writer, request, &payload) {
		return model.InventoryThresholds{}, false
	}
	thresholds, err := payload.thresholds()
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return model.InventoryThresholds{}, false
	}
	return thresholds, true
}
