package httpapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"

	"xlwms-api-manager/internal/store"
)

type inventoryAlertDefaultRequest struct {
	Threshold *float64 `json:"threshold"`
}

type inventoryAlertConfigRequest struct {
	WarehouseCode string   `json:"wh_code"`
	WarehouseSKU  string   `json:"warehouse_sku"`
	Threshold     *float64 `json:"threshold,omitempty"`
}

func (s *Server) listInventoryAlerts(writer http.ResponseWriter, request *http.Request) {
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 30
	}
	status := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("status")))
	if status == "" {
		status = "alert"
	}
	if status != "alert" && status != "all" {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "status must be alert or all"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, total, summary, defaultThreshold, err := s.store.ListInventoryAlerts(ctx, store.InventoryAlertFilter{
		WarehouseCode: request.URL.Query().Get("warehouse"),
		Query:         request.URL.Query().Get("q"),
		Status:        status,
		Page:          page,
		PageSize:      pageSize,
	})
	if err != nil {
		s.internalError(writer, "list inventory alerts", err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]any{
		"records": items, "total": total, "page": page, "page_size": pageSize, "pages": pages,
		"summary": summary, "default_threshold": defaultThreshold,
	}})
}

func (s *Server) updateInventoryAlertDefault(writer http.ResponseWriter, request *http.Request) {
	var payload inventoryAlertDefaultRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	threshold, err := validateInventoryAlertThreshold(payload.Threshold)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	result, err := s.store.UpdateInventoryAlertDefault(ctx, threshold)
	if err != nil {
		s.internalError(writer, "update inventory alert default", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]float64{"threshold": result}})
}

func (s *Server) updateInventoryAlertConfig(writer http.ResponseWriter, request *http.Request) {
	var payload inventoryAlertConfigRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	threshold, err := validateInventoryAlertThreshold(payload.Threshold)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	result, err := s.store.UpsertWarehouseSKUInventoryAlertThreshold(ctx, payload.WarehouseCode, payload.WarehouseSKU, threshold)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: result})
}

func (s *Server) resetInventoryAlertConfig(writer http.ResponseWriter, request *http.Request) {
	var payload inventoryAlertConfigRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if strings.TrimSpace(payload.WarehouseCode) == "" || strings.TrimSpace(payload.WarehouseSKU) == "" {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "wh_code and warehouse_sku are required"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	if err := s.store.DeleteWarehouseSKUInventoryAlertThreshold(ctx, payload.WarehouseCode, payload.WarehouseSKU); err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]bool{"deleted": true}})
}

func validateInventoryAlertThreshold(value *float64) (float64, error) {
	if value == nil {
		return 0, errors.New("threshold is required")
	}
	if *value < 0 || *value > 1_000_000_000 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return 0, errors.New("threshold must be between 0 and 1000000000")
	}
	return *value, nil
}
