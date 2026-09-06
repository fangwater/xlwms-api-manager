package httpapi

import (
	"context"
	"errors"
	"net/http"

	"xlwms-api-manager/internal/oms"
	"xlwms-api-manager/internal/store"
)

type fulfillmentAccountCreateRequest struct {
	Key               string   `json:"key"`
	Label             string   `json:"label"`
	Username          string   `json:"username"`
	Password          string   `json:"password"`
	APICredentialKeys []string `json:"api_credential_keys"`
}

type fulfillmentAccountPatchRequest struct {
	Label   *string `json:"label,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

type omsAccountAPICredentialsRequest struct {
	APICredentialKeys []string `json:"api_credential_keys"`
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
	item, err := creator.CreateAccount(ctx, payload.Key, payload.Label, payload.Username, payload.Password, payload.APICredentialKeys)
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

func (s *Server) updateFulfillmentAccountAPICredentials(writer http.ResponseWriter, request *http.Request) {
	var payload omsAccountAPICredentialsRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.ReplaceOMSAccountAPICredentials(ctx, request.PathValue("accountKey"), payload.APICredentialKeys)
	if err != nil {
		s.writeFulfillmentAccountError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
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
