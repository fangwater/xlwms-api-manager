package httpapi

import (
	"context"
	"errors"
	"net/http"

	"xlwms-api-manager/internal/oms"
	"xlwms-api-manager/internal/store"
)

type fulfillmentAccountCreateRequest struct {
	Key            string   `json:"key"`
	Label          string   `json:"label"`
	Username       string   `json:"username"`
	Password       string   `json:"password"`
	WarehouseCodes []string `json:"warehouse_codes"`
}

type fulfillmentAccountPatchRequest struct {
	Label   *string `json:"label,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

type omsAccountWarehousesRequest struct {
	WarehouseCodes []string `json:"warehouse_codes"`
}

type platformSKUOMSAccountRequest struct {
	AccountKey string `json:"account_key"`
}

func (s *Server) listFulfillmentAccounts(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, err := s.store.ListOMSAccountSummaries(ctx, request.URL.Query().Get("include_disabled") == "true")
	if err != nil {
		s.internalError(writer, "list fulfillment accounts", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: items})
}

func (s *Server) createFulfillmentAccount(writer http.ResponseWriter, request *http.Request) {
	var payload fulfillmentAccountCreateRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	creator, ok := s.platformAccounts.(platformOrderAccountCreator)
	if !ok {
		writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "OMS 账户管理暂不可用"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := creator.CreateAccount(ctx, payload.Key, payload.Label, payload.Username, payload.Password, payload.WarehouseCodes)
	if err != nil {
		if message := oms.AuthErrorMessage(err); message != "" {
			writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: message})
			return
		}
		s.writeFulfillmentAccountError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, response{Success: true, Data: item})
}

func (s *Server) updateFulfillmentAccount(writer http.ResponseWriter, request *http.Request) {
	var payload fulfillmentAccountPatchRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.UpdateOMSAccountMetadata(ctx, request.PathValue("accountKey"), payload.Label, payload.Enabled)
	if err != nil {
		s.writeFulfillmentAccountError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) updateFulfillmentAccountWarehouses(writer http.ResponseWriter, request *http.Request) {
	var payload omsAccountWarehousesRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.ReplaceOMSAccountWarehouses(ctx, request.PathValue("accountKey"), payload.WarehouseCodes)
	if err != nil {
		s.writeFulfillmentAccountError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) listPlatformSKUOMSAccounts(writer http.ResponseWriter, request *http.Request) {
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, total, err := s.store.ListPlatformSKUOMSAccounts(ctx, request.URL.Query().Get("platform"), store.PlatformSKUOMSAccountFilter{
		Query: request.URL.Query().Get("q"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		s.writeFulfillmentAccountError(writer, err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]any{
		"records": items, "total": total, "page": page, "page_size": pageSize, "pages": pages,
	}})
}

func (s *Server) updatePlatformSKUOMSAccount(writer http.ResponseWriter, request *http.Request) {
	var payload platformSKUOMSAccountRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.SetPlatformSKUOMSAccount(ctx, request.URL.Query().Get("platform"), request.PathValue("warehouseSKU"), payload.AccountKey)
	if err != nil {
		s.writeFulfillmentAccountError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) resetPlatformSKUOMSAccount(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	if err := s.store.DeletePlatformSKUOMSAccount(ctx, request.URL.Query().Get("platform"), request.PathValue("warehouseSKU")); err != nil {
		s.writeFulfillmentAccountError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]bool{"deleted": true}})
}

func (s *Server) writeFulfillmentAccountError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrOMSAccountExists):
		writeJSON(writer, http.StatusConflict, response{Success: false, Error: "OMS 账户标识已存在"})
	case errors.Is(err, store.ErrOMSAccountNotFound), errors.Is(err, store.ErrOMSAccountDisabled):
		writeJSON(writer, http.StatusNotFound, response{Success: false, Error: "OMS 账户不存在或已停用"})
	case errors.Is(err, store.ErrInvalidFulfillmentAccount):
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
	default:
		s.internalError(writer, "save fulfillment account policy", err)
	}
}
