package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"xlwms-api-manager/internal/oms"
)

type platformOrderSource interface {
	PendingOrders(context.Context, int, int) (oms.PendingOrderPage, error)
}

type platformOrderSearchSource interface {
	PendingOrdersByPlatformOrderNos(context.Context, []string) ([]oms.PendingOrder, error)
}

func (s *Server) pendingPlatformOrders(writer http.ResponseWriter, request *http.Request) {
	if s.platformOrders == nil {
		writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "OMS platform order access is not configured"})
		return
	}
	page := queryInt(request, "page", 1)
	pageSize := queryInt(request, "page_size", 30)
	if pageSize > 100 {
		pageSize = 100
	}
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	if query != "" {
		if _, err := normalizePlatformOrderNos([]string{query}); err != nil {
			writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "平台单号格式无效"})
			return
		}
		searcher, ok := s.platformOrders.(platformOrderSearchSource)
		if !ok {
			writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "OMS platform order search is not configured"})
			return
		}
		orders, err := searcher.PendingOrdersByPlatformOrderNos(ctx, []string{query})
		if err != nil {
			s.logger.Warn("search pending OMS platform order", "error", err)
			writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: "unable to search OMS platform orders"})
			return
		}
		records := make([]oms.PendingOrder, 0, len(orders))
		records = append(records, orders...)
		pages := 0
		if len(records) > 0 {
			pages = 1
		}
		writeJSON(writer, http.StatusOK, response{Success: true, Data: oms.PendingOrderPage{
			Records: records, Total: len(records), Page: 1, PageSize: pageSize, Pages: pages, QueriedAt: time.Now().UTC(),
		}})
		return
	}
	result, err := s.platformOrders.PendingOrders(ctx, page, pageSize)
	if err != nil {
		s.logger.Warn("load pending OMS platform orders", "error", err)
		writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: "unable to load OMS platform orders"})
		return
	}
	if result.QueriedAt.IsZero() {
		result.QueriedAt = time.Now().UTC()
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: result})
}

type platformOrderOperator interface {
	WarehouseOptions(context.Context) ([]oms.WarehouseOption, error)
	LogisticsChannels(context.Context, string) ([]oms.LogisticsChannelOption, error)
	PendingOrdersByPlatformOrderNos(context.Context, []string) ([]oms.PendingOrder, error)
	AssignAndApprove(context.Context, oms.AssignmentRequest) (oms.AssignmentResult, error)
}

type platformOrderCarrierOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type assignAndApproveFailure struct {
	PlatformOrderNo string `json:"platform_order_no"`
	Error           string `json:"error"`
}

type assignAndApproveResult struct {
	Total            int                       `json:"total"`
	Success          int                       `json:"success"`
	Failed           int                       `json:"failed"`
	Failures         []assignAndApproveFailure `json:"failures"`
	WarehouseCode    string                    `json:"warehouse_code"`
	WarehouseCodes   []string                  `json:"warehouse_codes,omitempty"`
	ChannelCode      string                    `json:"channel_code"`
	LogisticsCarrier string                    `json:"logistics_carrier"`
	CompletedAt      time.Time                 `json:"completed_at"`
}

func normalizePlatformOrderNos(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("请至少选择一个待处理订单")
	}
	if len(values) > 50 {
		return nil, errors.New("单次最多处理 50 个订单")
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || len(value) > 100 || strings.ContainsAny(value, "\r\n\t") {
			return nil, errors.New("平台单号格式无效")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	if len(normalized) == 0 {
		return nil, errors.New("请至少选择一个待处理订单")
	}
	return normalized, nil
}

func sanitizeOMSOperationMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "OMS 未返回失败原因"
	}
	if len(message) > 300 {
		return message[:300]
	}
	return message
}
