package httpapi

import (
	"context"
	"net/http"
	"strings"

	"xlwms-api-manager/internal/store"
)

type inventoryCorrectionRequest struct {
	CorrectionMode           string   `json:"correction_mode"`
	CorrectionAmount         *float64 `json:"correction_amount"`
	CorrectedAvailableAmount *float64 `json:"corrected_available_amount"`
	Note                     string   `json:"note"`
}

func (s *Server) listInventoryCorrections(writer http.ResponseWriter, request *http.Request) {
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, total, err := s.store.ListInventoryCorrections(ctx, store.InventoryCorrectionFilter{
		WarehouseCode: request.URL.Query().Get("warehouse"),
		Query:         request.URL.Query().Get("q"),
		Page:          page,
		PageSize:      pageSize,
	})
	if err != nil {
		s.internalError(writer, "list inventory corrections", err)
		return
	}
	writePage(writer, items, total, page, pageSize)
}

func (s *Server) saveInventoryCorrection(writer http.ResponseWriter, request *http.Request) {
	warehouseCode := strings.TrimSpace(request.PathValue("warehouse"))
	warehouseSKU := strings.TrimSpace(request.PathValue("warehouseSKU"))
	var payload inventoryCorrectionRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	amount := 0.0
	if payload.CorrectionAmount != nil {
		amount = *payload.CorrectionAmount
	} else if payload.CorrectedAvailableAmount != nil {
		amount = *payload.CorrectedAvailableAmount
	}
	item, err := s.store.SaveInventoryCorrection(ctx, warehouseCode, warehouseSKU, payload.CorrectionMode, amount, payload.Note)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) deleteInventoryCorrection(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	deleted, err := s.store.DeleteInventoryCorrection(ctx, request.PathValue("warehouse"), request.PathValue("warehouseSKU"))
	if err != nil {
		s.internalError(writer, "delete inventory correction", err)
		return
	}
	if !deleted {
		writeJSON(writer, http.StatusNotFound, response{Success: false, Error: "inventory correction not found"})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]bool{"deleted": true}})
}
