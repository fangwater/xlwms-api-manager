package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"xlwms-api-manager/internal/config"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/xlwms"
)

type warehouseAPICredentialRequest struct {
	Label      string `json:"label"`
	APIBaseURL string `json:"api_base_url"`
	AppKey     string `json:"app_key"`
	AppSecret  string `json:"app_secret"`
}

func (s *Server) listWarehouseAPICredentials(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, err := s.store.ListWarehouseAPICredentialGroups(ctx, request.URL.Query().Get("include_disabled") == "true")
	if err != nil {
		s.internalError(writer, "list warehouse API credentials", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: items})
}

func (s *Server) createWarehouseAPICredential(writer http.ResponseWriter, request *http.Request) {
	var payload warehouseAPICredentialRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if strings.TrimSpace(payload.APIBaseURL) == "" {
		payload.APIBaseURL = config.DefaultAPIBaseURL
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout*4)
	defer cancel()
	client := xlwms.NewClient(payload.APIBaseURL, payload.AppKey, payload.AppSecret, s.requestTimeout)
	records, err := discoverWarehouseAPIInventory(ctx, client)
	if err != nil {
		var apiErr *xlwms.APIError
		if errors.As(err, &apiErr) {
			writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: "仓库 OpenAPI 凭据验证失败"})
			return
		}
		s.internalError(writer, "discover warehouse API inventory", err)
		return
	}
	items := make([]store.WarehouseAPIInventoryItem, 0, len(records))
	for _, record := range records {
		items = append(items, store.WarehouseAPIInventoryItemFromRecord(record))
	}
	item, err := s.store.UpsertWarehouseAPICredentialGroup(
		ctx, payload.Label, payload.APIBaseURL, payload.AppKey, payload.AppSecret, items,
	)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusCreated, response{Success: true, Data: item})
}

func (s *Server) deleteWarehouseAPICredential(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	deleted, err := s.store.DeleteWarehouseAPICredentialGroup(ctx, request.PathValue("credentialKey"))
	if errors.Is(err, store.ErrWarehouseAPICredentialInUse) {
		writeJSON(writer, http.StatusConflict, response{Success: false, Error: "该凭据仍被仓库同步或出库使用，请先迁移仓库后再删除"})
		return
	}
	if err != nil {
		s.internalError(writer, "delete warehouse API credential", err)
		return
	}
	if !deleted {
		writeJSON(writer, http.StatusNotFound, response{Success: false, Error: "仓库 API 凭据不存在"})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]bool{"deleted": true}})
}

func discoverWarehouseAPIInventory(ctx context.Context, client *xlwms.Client) ([]map[string]any, error) {
	records := make([]map[string]any, 0)
	pages := 1
	for page := 1; page <= pages; page++ {
		if page > 1000 {
			return nil, errors.New("warehouse API inventory discovery exceeded 1000 pages")
		}
		result, err := client.PageInventory(ctx, "integrated", map[string]any{}, page, 100)
		if err != nil {
			return nil, err
		}
		data, ok := result["data"].(map[string]any)
		if !ok {
			return nil, errors.New("warehouse API inventory response is missing data")
		}
		batch, ok := data["records"].([]any)
		if !ok && data["records"] != nil {
			return nil, errors.New("warehouse API inventory response has invalid records")
		}
		for _, raw := range batch {
			record, ok := raw.(map[string]any)
			if !ok {
				return nil, errors.New("warehouse API inventory response contains an invalid record")
			}
			records = append(records, record)
		}
		if page == 1 {
			pages = apiInteger(data["pages"])
			if pages < 1 {
				total := apiInteger(data["total"])
				pages = (total + 99) / 100
				if pages < 1 {
					pages = 1
				}
			}
		}
	}
	return records, nil
}

func apiInteger(value any) int {
	var result int
	_, _ = fmt.Sscan(fmt.Sprint(value), &result)
	return result
}
