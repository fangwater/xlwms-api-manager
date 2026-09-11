package httpapi

import (
	"context"
	"net/http"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
)

type platformSKUMappingRequest struct {
	Platform    string                         `json:"platform"`
	PlatformSKU string                         `json:"platform_sku"`
	Source      string                         `json:"source"`
	Enabled     *bool                          `json:"enabled"`
	Items       []model.PlatformSKUMappingItem `json:"items"`
}

func (payload platformSKUMappingRequest) mapping() model.PlatformSKUMapping {
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	return model.PlatformSKUMapping{
		Platform: payload.Platform, PlatformSKU: payload.PlatformSKU, Source: payload.Source,
		Enabled: enabled, Items: payload.Items,
	}
}

func (s *Server) listPlatformSKUMappings(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	result, err := s.store.ListPlatformSKUMappings(ctx, store.PlatformSKUMappingFilter{
		Platform: request.URL.Query().Get("platform"), Query: request.URL.Query().Get("q"),
		Status: request.URL.Query().Get("status"), Page: queryInt(request, "page", 1),
		PageSize: queryInt(request, "page_size", 50),
	})
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: result})
}

func (s *Server) resolvePlatformSKUMappings(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Platform     string   `json:"platform"`
		PlatformSKUs []string `json:"platform_skus"`
	}
	if !decodeJSON(writer, request, &payload) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	result, err := s.store.ResolvePlatformSKUMappings(ctx, payload.Platform, payload.PlatformSKUs)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: result})
}

func (s *Server) savePlatformSKUMapping(writer http.ResponseWriter, request *http.Request) {
	var payload platformSKUMappingRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	mapping, err := store.NormalizePlatformSKUMapping(payload.mapping())
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	saved, err := s.store.ReplacePlatformSKUMapping(ctx, mapping)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: saved})
}

func (s *Server) importPlatformSKUMappings(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Mappings []platformSKUMappingRequest `json:"mappings"`
	}
	if !decodeJSON(writer, request, &payload) {
		return
	}
	mappings := make([]model.PlatformSKUMapping, 0, len(payload.Mappings))
	for _, item := range payload.Mappings {
		mapping, err := store.NormalizePlatformSKUMapping(item.mapping())
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
			return
		}
		mappings = append(mappings, mapping)
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	count, err := s.store.ImportPlatformSKUMappings(ctx, mappings)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]int{"imported": count}})
}

func (s *Server) deletePlatformSKUMapping(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	deleted, err := s.store.DeletePlatformSKUMapping(ctx, request.PathValue("platform"), request.PathValue("platformSKU"))
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]bool{"deleted": deleted}})
}
