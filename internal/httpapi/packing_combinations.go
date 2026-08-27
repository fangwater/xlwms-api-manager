package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
)

func (s *Server) listSKUCombinations(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, err := s.store.ListSKUCombinations(ctx, store.SKUCombinationFilter{
		Query: request.URL.Query().Get("q"), Status: request.URL.Query().Get("status"),
	})
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: items})
}

func (s *Server) getSKUCombination(writer http.ResponseWriter, request *http.Request) {
	id, ok := skuCombinationID(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.SKUCombination(ctx, id)
	if err != nil {
		writeJSON(writer, http.StatusNotFound, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) getSKUCombinationSubstitution(writer http.ResponseWriter, request *http.Request) {
	warehouseSKU := strings.TrimSpace(request.PathValue("warehouseSKU"))
	if warehouseSKU == "" {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "warehouse_sku is required"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.SKUCombinationForSubstitution(ctx, warehouseSKU)
	if err != nil {
		writeJSON(writer, http.StatusNotFound, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) createSKUCombination(writer http.ResponseWriter, request *http.Request) {
	var payload model.SKUCombination
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if _, err := store.ValidateSKUCombinationForAPI(payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.CreateSKUCombination(ctx, payload)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusCreated, response{Success: true, Data: item})
}

func (s *Server) updateSKUCombination(writer http.ResponseWriter, request *http.Request) {
	id, ok := skuCombinationID(writer, request)
	if !ok {
		return
	}
	var payload model.SKUCombination
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if _, err := store.ValidateSKUCombinationForAPI(payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.UpdateSKUCombination(ctx, id, payload)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) deleteSKUCombination(writer http.ResponseWriter, request *http.Request) {
	id, ok := skuCombinationID(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	if err := s.store.DeleteSKUCombination(ctx, id); err != nil {
		writeJSON(writer, http.StatusNotFound, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]bool{"deleted": true}})
}

func skuCombinationID(writer http.ResponseWriter, request *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "invalid SKU combination id"})
		return 0, false
	}
	return id, true
}
