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

type platformOrderLookupSource interface {
	PlatformOrdersByPlatformOrderNo(context.Context, string) ([]oms.PendingOrder, error)
}

type platformOrderLookupResult struct {
	Account         string             `json:"account"`
	PlatformOrderNo string             `json:"platform_order_no"`
	Found           bool               `json:"found"`
	Records         []oms.PendingOrder `json:"records"`
	QueriedAt       time.Time          `json:"queried_at"`
}

func (s *Server) platformOrder(writer http.ResponseWriter, request *http.Request) {
	platformOrderNos, err := normalizePlatformOrderNos([]string{request.PathValue("platformOrderNo")})
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "平台单号格式无效"})
		return
	}
	accountKey, err := requestedPlatformOrderAccount(request)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "OMS 账户参数冲突"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	account, err := s.selectedPlatformOrderAccount(ctx, accountKey)
	if err != nil {
		writePlatformOrderAccountError(writer, err)
		return
	}
	lookup, ok := account.(platformOrderLookupSource)
	if !ok {
		writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "OMS 全部订单查询暂不可用"})
		return
	}
	records, err := lookup.PlatformOrdersByPlatformOrderNo(ctx, platformOrderNos[0])
	if err != nil {
		s.logger.Warn("query OMS platform order across all statuses", "account", accountKey, "error", err)
		writePlatformOrderSourceError(writer, err, "无法查询 OMS 平台订单")
		return
	}
	if records == nil {
		records = []oms.PendingOrder{}
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, response{Success: true, Data: platformOrderLookupResult{
		Account: accountKey, PlatformOrderNo: platformOrderNos[0], Found: len(records) > 0,
		Records: records, QueriedAt: time.Now().UTC(),
	}})
}

func (s *Server) pendingPlatformOrders(writer http.ResponseWriter, request *http.Request) {
	page := queryInt(request, "page", 1)
	pageSize := queryInt(request, "page_size", 30)
	if pageSize > 100 {
		pageSize = 100
	}
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	accountKey, err := requestedPlatformOrderAccount(request)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "OMS 账户参数冲突"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	account, err := s.selectedPlatformOrderAccount(ctx, accountKey)
	if err != nil {
		writePlatformOrderAccountError(writer, err)
		return
	}
	if query != "" {
		if _, err := normalizePlatformOrderNos([]string{query}); err != nil {
			writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "平台单号格式无效"})
			return
		}
		orders, err := account.PendingOrdersByPlatformOrderNos(ctx, []string{query})
		if err != nil {
			s.logger.Warn("search pending OMS platform order", "account", accountKey, "error", err)
			writePlatformOrderSourceError(writer, err, "无法查询 OMS 平台订单")
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
	result, err := account.PendingOrders(ctx, page, pageSize)
	if err != nil {
		s.logger.Warn("load pending OMS platform orders", "account", accountKey, "error", err)
		writePlatformOrderSourceError(writer, err, "无法加载 OMS 平台订单")
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
	Account          string                        `json:"account"`
	Total            int                           `json:"total"`
	Success          int                           `json:"success"`
	Failed           int                           `json:"failed"`
	Failures         []assignAndApproveFailure     `json:"failures"`
	Routes           []automaticPlatformOrderRoute `json:"routes"`
	WarehouseCode    string                        `json:"warehouse_code"`
	WarehouseCodes   []string                      `json:"warehouse_codes,omitempty"`
	ChannelCode      string                        `json:"channel_code"`
	LogisticsCarrier string                        `json:"logistics_carrier"`
	CompletedAt      time.Time                     `json:"completed_at"`
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
