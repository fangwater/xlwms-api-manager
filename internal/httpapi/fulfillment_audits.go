package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
)

type fulfillmentAuditSnapshotRequest struct {
	Platform string                               `json:"platform"`
	ShopCode string                               `json:"shop_code"`
	ShopName string                               `json:"shop_name"`
	Orders   []model.FulfillmentAuditSnapshotItem `json:"orders"`
}

func (s *Server) syncFulfillmentAudits(writer http.ResponseWriter, request *http.Request) {
	var payload fulfillmentAuditSnapshotRequest
	if !decodeFulfillmentAuditJSON(writer, request, &payload) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	count, err := s.store.ReplaceFulfillmentAuditSnapshot(ctx, payload.Platform, payload.ShopCode, payload.ShopName, payload.Orders)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	matched := 0
	if s.fulfillmentAuditor != nil {
		references := make([]string, 0, len(payload.Orders))
		for _, item := range payload.Orders {
			references = append(references, item.PlatformOrderNo)
		}
		matched, err = s.fulfillmentAuditor.ReconcileReferences(ctx, references)
		if err != nil {
			s.internalError(writer, "reconcile fulfillment snapshot from local index", err)
			return
		}
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]any{
		"platform":  strings.ToLower(strings.TrimSpace(payload.Platform)),
		"shop_code": strings.ToLower(strings.TrimSpace(payload.ShopCode)),
		"orders":    count,
		"matched":   matched,
	}})
}

func (s *Server) listFulfillmentAudits(writer http.ResponseWriter, request *http.Request) {
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	if err := s.store.RefreshFulfillmentAuditCategories(ctx); err != nil {
		s.internalError(writer, "refresh fulfillment audit categories", err)
		return
	}
	items, total, summary, err := s.store.ListFulfillmentAudits(ctx, store.FulfillmentAuditFilter{
		ShopCode: request.URL.Query().Get("shop"), WarehouseCode: request.URL.Query().Get("warehouse"),
		ExceptionCategory: request.URL.Query().Get("category"), OMSStatus: request.URL.Query().Get("oms_status"),
		Query: request.URL.Query().Get("q"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		s.internalError(writer, "list fulfillment audits", err)
		return
	}
	shops, err := s.store.FulfillmentAuditShops(ctx, false)
	if err != nil {
		s.internalError(writer, "list fulfillment audit shops", err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]any{
		"records": items, "total": total, "page": page, "page_size": pageSize, "pages": pages, "summary": summary, "shops": shops,
	}})
}

func (s *Server) listArchivedFulfillmentAudits(writer http.ResponseWriter, request *http.Request) {
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, total, summary, err := s.store.ListFulfillmentAudits(ctx, store.FulfillmentAuditFilter{
		Archived: true, ShopCode: request.URL.Query().Get("shop"),
		WarehouseCode: request.URL.Query().Get("warehouse"), Query: request.URL.Query().Get("q"),
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		s.internalError(writer, "list archived fulfillment audits", err)
		return
	}
	shops, err := s.store.FulfillmentAuditShops(ctx, true)
	if err != nil {
		s.internalError(writer, "list archived fulfillment shops", err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]any{
		"records": items, "total": total, "page": page, "page_size": pageSize,
		"pages": pages, "last_query_at": summary.LastQueryAt, "shops": shops,
	}})
}

func (s *Server) exportManualFulfillmentAudits(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	if err := s.store.RefreshFulfillmentAuditCategories(ctx); err != nil {
		s.internalError(writer, "refresh fulfillment audit categories for export", err)
		return
	}
	items, err := s.store.ExportManualFulfillmentAudits(ctx, store.FulfillmentAuditFilter{
		ShopCode: request.URL.Query().Get("shop"), WarehouseCode: request.URL.Query().Get("warehouse"),
		OMSStatus: request.URL.Query().Get("oms_status"), Query: request.URL.Query().Get("q"),
	})
	if err != nil {
		s.internalError(writer, "export manual fulfillment audits", err)
		return
	}
	contents, err := manualFulfillmentCSV(items)
	if err != nil {
		s.internalError(writer, "encode manual fulfillment audit CSV", err)
		return
	}
	filename := "manual-fulfillment-orders-" + time.Now().Format("20060102-1504") + ".csv"
	writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
	writer.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(contents)
}

func manualFulfillmentCSV(items []model.FulfillmentAudit) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString("\xEF\xBB\xBF")
	output := csv.NewWriter(&buffer)
	if err := output.Write([]string{
		"店铺", "店铺代码", "平台PO单号", "领星仓库", "仓库标识", "OMS状态", "OMS状态码",
		"人工处理原因", "领星出库单号", "平台跟踪号", "OMS跟踪号", "平台发货时间",
		"OMS订单创建时间", "OMS出库时间", "最近核查时间",
	}); err != nil {
		return nil, err
	}
	for _, item := range items {
		if err := output.Write([]string{
			spreadsheetSafe(item.ShopName), spreadsheetSafe(item.ShopCode), spreadsheetSafe(item.PlatformOrderNo),
			spreadsheetSafe(item.WarehouseCode), spreadsheetSafe(item.WarehouseKey), fulfillmentOMSStatusLabel(item.OMSStatus, item.OMSStatusCode),
			optionalInt(item.OMSStatusCode), manualFulfillmentReason(item), spreadsheetSafe(item.OutboundOrderNo),
			spreadsheetSafe(item.TrackingNumber), spreadsheetSafe(item.OMSTrackingNumber), formatCSVTime(item.PlatformShippingAt),
			formatCSVTime(item.OMSOrderCreatedAt), formatCSVTime(item.OMSOutboundAt), formatCSVTime(item.LastCheckedAt),
		}); err != nil {
			return nil, err
		}
	}
	output.Flush()
	if err := output.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func spreadsheetSafe(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	switch value[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func optionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(*value)
}

func formatCSVTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.In(time.Local).Format("2006-01-02 15:04:05")
}

func fulfillmentOMSStatusLabel(status string, statusCode *int) string {
	if statusCode != nil {
		labels := map[int]string{
			0: "新建", 1: "已取面单", 2: "仓库处理中", 3: "已出库",
			4: "已取消", 5: "异常", 6: "拦截中", 7: "面单异常",
		}
		if label := labels[*statusCode]; label != "" {
			return label
		}
	}
	labels := map[string]string{
		"not_found": "未匹配", "pending": "待处理", "processing": "仓库处理中", "outbound": "已出库",
		"exception": "异常订单", "query_error": "查询失败", "unknown": "未知状态", "pending_query": "待查询",
	}
	if label := labels[status]; label != "" {
		return label
	}
	return spreadsheetSafe(status)
}

func manualFulfillmentReason(item model.FulfillmentAudit) string {
	if item.OMSStatusCode != nil {
		switch *item.OMSStatusCode {
		case 0:
			return "领星出库单为新建状态"
		case 1:
			return "领星已取面单，仍待仓库处理"
		case 4:
			return "领星出库单已取消"
		case 5:
			return "领星出库单异常"
		case 6:
			return "领星出库单拦截中"
		case 7:
			return "领星出库单面单异常"
		}
	}
	switch item.OMSStatus {
	case "not_found":
		return "未在领星找到出库单"
	case "pending":
		return "领星订单待处理"
	case "exception":
		return "领星订单异常"
	case "unknown":
		return "领星订单状态未知"
	default:
		return "需要人工核查"
	}
}

func decodeFulfillmentAuditJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 8<<20))
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
