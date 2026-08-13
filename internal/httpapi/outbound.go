package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/xlwms"
)

const outboundRequestLimit = 145 << 20

type outboundRequest struct {
	Warehouse string `json:"warehouse"`
	Data      any    `json:"data"`
}

type warehouseCredentialSource interface {
	WarehouseCredentials(context.Context, string, bool) (model.WarehouseCredentials, error)
}

func (s *Server) outbound(writer http.ResponseWriter, request *http.Request) {
	operation := request.PathValue("operation")
	if _, ok := xlwms.OutboundPaths[operation]; !ok {
		writeJSON(writer, http.StatusNotFound, response{Success: false, Error: "unknown outbound operation"})
		return
	}
	var payload outboundRequest
	if !decodeOutboundJSON(writer, request, &payload) {
		return
	}
	if err := xlwms.ValidateOutboundData(operation, payload.Data); err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	warehouse, err := s.warehouseCredentials.WarehouseCredentials(ctx, payload.Warehouse, true)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	for _, code := range xlwms.OutboundWarehouseCodes(payload.Data) {
		if code != strings.ToUpper(warehouse.Code) {
			writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "request warehouse does not match the selected warehouse"})
			return
		}
	}
	client := xlwms.NewClient(warehouse.APIBaseURL, warehouse.AppKey, warehouse.AppSecret, s.requestTimeout)
	result, err := client.Outbound(ctx, operation, payload.Data)
	if err != nil {
		var apiErr *xlwms.APIError
		if errors.As(err, &apiErr) {
			writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: apiErr.Error()})
			return
		}
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: result})
}

func decodeOutboundJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(request.Body, outboundRequestLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "invalid JSON request"})
		return false
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "request body must contain one JSON object"})
		return false
	}
	return true
}
