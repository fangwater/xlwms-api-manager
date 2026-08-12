package httpapi

import (
	"context"
	"net/http"

	"xlwms-api-manager/internal/packing"
)

func (s *Server) createPackingPlan(writer http.ResponseWriter, request *http.Request) {
	var payload packing.Request
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if err := packing.ValidateRequest(payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	resolution, err := s.store.ResolveWarehouseSKUSpecs(ctx, payload.Items)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	if len(resolution.MissingSKUs) > 0 {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: resolution.Error})
		return
	}
	plan, err := packing.Build(payload, resolution.Items)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: plan})
}
