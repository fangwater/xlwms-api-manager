package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
)

func (s *Server) requireLoopbackInternal(next http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		host, _, err := net.SplitHostPort(request.RemoteAddr)
		address := net.ParseIP(host)
		forwarded := strings.TrimSpace(request.Header.Get("X-Forwarded-For")) != "" || strings.TrimSpace(request.Header.Get("X-Real-IP")) != ""
		if err != nil || address == nil || !address.IsLoopback() || forwarded {
			writeJSON(writer, http.StatusForbidden, response{Success: false, Error: "internal endpoint"})
			return
		}
		next(writer, request)
	}
}

func (s *Server) reserveFulfillmentInventory(writer http.ResponseWriter, request *http.Request) {
	var payload model.FulfillmentInventoryReservationRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	platform, shopCode, err := requestedDecisionShop(request, payload.Platform, payload.ShopCode)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	payload.Platform = platform
	payload.ShopCode = shopCode
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	reservation, err := s.store.ReserveFulfillmentInventory(ctx, payload)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInventoryReservationCapacity):
			writeJSON(writer, http.StatusConflict, response{Success: false, Code: "INVENTORY_CAPACITY_EXHAUSTED", Error: err.Error()})
		case errors.Is(err, store.ErrInvalidInventoryReservation), isShopIdentityError(err):
			writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		default:
			s.internalError(writer, "reserve fulfillment inventory", err)
		}
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: reservation})
}

func (s *Server) releaseFulfillmentInventory(writer http.ResponseWriter, request *http.Request) {
	var payload model.FulfillmentInventoryReservationRelease
	if !decodeJSON(writer, request, &payload) {
		return
	}
	platform, shopCode, err := requestedDecisionShop(request, payload.Platform, payload.ShopCode)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	payload.Platform = platform
	payload.ShopCode = shopCode
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	released, err := s.store.ReleaseFulfillmentInventory(ctx, payload)
	if err != nil {
		if errors.Is(err, store.ErrInvalidInventoryReservation) || isShopIdentityError(err) {
			writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
			return
		}
		s.internalError(writer, "release fulfillment inventory", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]bool{"released": released}})
}
