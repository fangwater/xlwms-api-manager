package httpapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/temu"
)

type inventoryThresholdRequest struct {
	EastThreshold  *float64 `json:"east_threshold"`
	WestThreshold  *float64 `json:"west_threshold"`
	TotalThreshold *float64 `json:"total_threshold"`
}

type fulfillmentShopStore interface {
	ListFulfillmentShops(context.Context, bool) ([]model.FulfillmentShop, error)
	UpsertFulfillmentShop(context.Context, string, string, string, bool, string) (model.FulfillmentShop, bool, error)
	UpdateFulfillmentShop(context.Context, string, string, *string, *bool, string) (model.FulfillmentShop, error)
}

type fulfillmentShopRequest struct {
	Platform string `json:"platform"`
	ShopCode string `json:"shop_code"`
	ShopName string `json:"shop_name"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

type fulfillmentShopPatchRequest struct {
	ShopName *string `json:"shop_name,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
}

func (s *Server) registerFulfillmentShopRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/fulfillment-shops", s.listFulfillmentShops)
	mux.HandleFunc("POST /v1/fulfillment-shops", s.requireConsoleAuth(s.upsertFulfillmentShop))
	mux.HandleFunc("PATCH /v1/fulfillment-shops/{platform}/{shopCode}", s.requireConsoleAuth(s.updateFulfillmentShop))
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

func (s *Server) listFulfillmentShops(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, err := s.fulfillmentShops.ListFulfillmentShops(ctx, request.URL.Query().Get("include_disabled") == "true")
	if err != nil {
		s.internalError(writer, "list fulfillment shops", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: items})
}

func (s *Server) upsertFulfillmentShop(writer http.ResponseWriter, request *http.Request) {
	var payload fulfillmentShopRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	actor, _, _ := request.BasicAuth()
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, created, err := s.fulfillmentShops.UpsertFulfillmentShop(ctx, payload.Platform, payload.ShopCode, payload.ShopName, enabled, actor)
	if err != nil {
		s.writeFulfillmentShopError(writer, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(writer, status, response{Success: true, Data: item})
}

func (s *Server) updateFulfillmentShop(writer http.ResponseWriter, request *http.Request) {
	var payload fulfillmentShopPatchRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if payload.ShopName == nil && payload.Enabled == nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "shop_name or enabled is required"})
		return
	}
	actor, _, _ := request.BasicAuth()
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.fulfillmentShops.UpdateFulfillmentShop(ctx, request.PathValue("platform"), request.PathValue("shopCode"), payload.ShopName, payload.Enabled, actor)
	if err != nil {
		s.writeFulfillmentShopError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) writeFulfillmentShopError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInvalidFulfillmentShop):
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
	case errors.Is(err, store.ErrFulfillmentShopNotFound):
		writeJSON(writer, http.StatusNotFound, response{Success: false, Error: err.Error()})
	default:
		s.internalError(writer, "save fulfillment shop", err)
	}
}

func (s *Server) listInventoryThresholds(writer http.ResponseWriter, request *http.Request) {
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	platform, ok := requiredThresholdPlatform(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, total, err := s.store.ListPlatformInventorySKUThresholds(ctx, platform, store.InventoryThresholdFilter{
		Query: request.URL.Query().Get("q"), Page: page, PageSize: pageSize,
	}, temu.WarehouseCodes(temu.RegionEast), temu.WarehouseCodes(temu.RegionWest))
	if err != nil {
		s.internalError(writer, "list inventory thresholds", err)
		return
	}
	defaults, err := s.store.PlatformInventoryThresholds(ctx, platform)
	if err != nil {
		s.internalError(writer, "load inventory threshold defaults", err)
		return
	}
	platforms, err := s.store.ListPlatformInventoryThresholds(ctx)
	if err != nil {
		s.internalError(writer, "list platform inventory thresholds", err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]any{
		"records": items, "total": total, "page": page, "page_size": pageSize, "pages": pages,
		"default_thresholds": defaults, "platforms": platforms, "platform": platform,
	}})
}

func (s *Server) inventoryThresholdDefaults(writer http.ResponseWriter, request *http.Request) {
	platform, ok := requiredThresholdPlatform(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.PlatformInventoryThresholds(ctx, platform)
	if err != nil {
		s.internalError(writer, "load platform inventory thresholds", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) updateInventoryThresholdDefaults(writer http.ResponseWriter, request *http.Request) {
	thresholds, ok := decodeInventoryThresholds(writer, request)
	if !ok {
		return
	}
	platform, ok := requiredThresholdPlatform(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.UpdatePlatformInventoryThresholds(ctx, platform, thresholds)
	if err != nil {
		s.internalError(writer, "update platform inventory thresholds", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) updateSKUInventoryThreshold(writer http.ResponseWriter, request *http.Request) {
	thresholds, ok := decodeInventoryThresholds(writer, request)
	if !ok {
		return
	}
	platform, ok := requiredThresholdPlatform(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	item, err := s.store.UpsertPlatformSKUInventoryThreshold(ctx, platform, request.PathValue("warehouseSKU"), thresholds)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: item})
}

func (s *Server) deleteSKUInventoryThreshold(writer http.ResponseWriter, request *http.Request) {
	platform, ok := requiredThresholdPlatform(writer, request)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	if err := s.store.DeletePlatformSKUInventoryThreshold(ctx, platform, request.PathValue("warehouseSKU")); err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]bool{"deleted": true}})
}

func inventoryThresholdDefaultResetGone(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusGone, response{Success: false, Error: "platform inventory thresholds have no parent default"})
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

func requiredThresholdPlatform(writer http.ResponseWriter, request *http.Request) (string, bool) {
	platform := strings.TrimSpace(request.URL.Query().Get("platform"))
	temuShop := strings.TrimSpace(request.Header.Get("X-Temu-Shop"))
	sheinShop := strings.TrimSpace(request.Header.Get("X-Shein-Shop"))
	if temuShop != "" && sheinShop != "" {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "conflicting shop headers"})
		return "", false
	}
	headerPlatform := ""
	if temuShop != "" {
		headerPlatform = "temu"
	}
	if sheinShop != "" {
		headerPlatform = "shein"
	}
	if platform == "" {
		platform = headerPlatform
	} else if headerPlatform != "" && !strings.EqualFold(platform, headerPlatform) {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "conflicting platform selectors"})
		return "", false
	}
	normalized, err := store.NormalizeFulfillmentPlatform(platform)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return "", false
	}
	return normalized, true
}

func requestedShopIdentity(request *http.Request) (string, string, error) {
	platform := strings.TrimSpace(request.URL.Query().Get("platform"))
	shopCode := strings.TrimSpace(request.URL.Query().Get("shop"))
	if shopCode == "" {
		shopCode = strings.TrimSpace(request.URL.Query().Get("shop_code"))
	}
	temuShop := strings.TrimSpace(request.Header.Get("X-Temu-Shop"))
	sheinShop := strings.TrimSpace(request.Header.Get("X-Shein-Shop"))
	if temuShop != "" && sheinShop != "" {
		return "", "", errors.New("conflicting shop headers")
	}
	if temuShop != "" {
		if platform != "" && !strings.EqualFold(platform, "temu") {
			return "", "", errors.New("conflicting shop selectors")
		}
		if shopCode != "" && !strings.EqualFold(shopCode, temuShop) {
			return "", "", errors.New("conflicting shop selectors")
		}
		platform, shopCode = "temu", temuShop
	}
	if sheinShop != "" {
		if platform != "" && !strings.EqualFold(platform, "shein") {
			return "", "", errors.New("conflicting shop selectors")
		}
		if shopCode != "" && !strings.EqualFold(shopCode, sheinShop) {
			return "", "", errors.New("conflicting shop selectors")
		}
		platform, shopCode = "shein", sheinShop
	}
	if platform == "" && shopCode == "" {
		return "", "", nil
	}
	if platform == "" || shopCode == "" {
		return "", "", errors.New("platform and shop_code are required together")
	}
	normalizedPlatform, err := store.NormalizeFulfillmentPlatform(platform)
	if err != nil {
		return "", "", err
	}
	normalizedShop, err := store.NormalizeFulfillmentShopCode(shopCode)
	if err != nil {
		return "", "", err
	}
	return normalizedPlatform, normalizedShop, nil
}

func optionalShopIdentity(writer http.ResponseWriter, request *http.Request) (string, string, bool) {
	platform, shopCode, err := requestedShopIdentity(request)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return "", "", false
	}
	return platform, shopCode, true
}

func requiredShopIdentity(writer http.ResponseWriter, request *http.Request) (string, string, bool) {
	platform, shopCode, ok := optionalShopIdentity(writer, request)
	if !ok {
		return "", "", false
	}
	if platform == "" || shopCode == "" {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "platform and shop_code are required"})
		return "", "", false
	}
	return platform, shopCode, true
}

func isShopIdentityError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "platform") || strings.Contains(message, "shop_code") || strings.Contains(message, "fulfillment shop") || strings.Contains(message, "shop selectors")
}
