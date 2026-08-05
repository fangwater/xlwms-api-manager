package httpapi

import (
	"context"
	"net/http"

	"xlwms-api-manager/internal/store"
)

func (s *Server) listOutboundOrders(writer http.ResponseWriter, request *http.Request) {
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, total, lastQueryAt, err := s.store.ListOutboundOrders(ctx, store.OutboundOrderFilter{
		WarehouseCode: request.URL.Query().Get("warehouse"),
		Query:         request.URL.Query().Get("q"),
		Page:          page,
		PageSize:      pageSize,
	})
	if err != nil {
		s.internalError(writer, "list indexed outbound orders", err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]any{
		"records": items, "total": total, "page": page, "page_size": pageSize,
		"pages": pages, "last_query_at": lastQueryAt,
	}})
}
