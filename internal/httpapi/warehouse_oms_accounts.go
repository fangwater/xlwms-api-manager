package httpapi

import (
	"context"
	"net/http"
)

type warehouseOMSAccountRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) setWarehouseOMSAccount(writer http.ResponseWriter, request *http.Request) {
	var payload warehouseOMSAccountRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	warehouse, err := s.store.SetWarehouseOMSAccount(ctx, request.PathValue("code"), payload.Username, payload.Password)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: warehouse})
}

func (s *Server) clearWarehouseOMSAccount(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	warehouse, err := s.store.ClearWarehouseOMSAccount(ctx, request.PathValue("code"))
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: warehouse})
}
