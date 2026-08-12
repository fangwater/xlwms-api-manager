package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"xlwms-api-manager/internal/oms"
)

const (
	temuPlatformOrderStatusPending               = "pending"
	temuPlatformOrderStatusAwaitingPlatformLabel = "awaiting_platform_label"
	temuPlatformOrderStatusProcessing            = "processing"
	temuPlatformOrderStatusShipped               = "shipped"
	temuPlatformOrderStatusCanceled              = "canceled"
	temuPlatformOrderStatusException             = "exception"
	temuPlatformOrderStatusAwaitingInvoice       = "awaiting_invoice"
	temuPlatformOrderStatusUnknown               = "unknown"
)

var errTemuPlatformOrderAccountRequired = errors.New("OMS account is required")

type temuPlatformOrderRecord struct {
	OMSOrderNo         string `json:"oms_order_no"`
	PlatformOrderNo    string `json:"platform_order_no"`
	PlatformCode       string `json:"platform_code,omitempty"`
	Status             int    `json:"status"`
	StatusKey          string `json:"status_key"`
	StatusText         string `json:"status_text,omitempty"`
	SubStatus          int    `json:"sub_status"`
	SendWarehouseCode  string `json:"send_warehouse_code,omitempty"`
	TrackingNumber     string `json:"tracking_number,omitempty"`
	OrderTime          string `json:"order_time,omitempty"`
	CreateTime         string `json:"create_time,omitempty"`
	AuditTime          string `json:"audit_time,omitempty"`
	MarkShipmentStatus int    `json:"mark_shipment_status"`
	MarkShipmentTime   string `json:"mark_shipment_time,omitempty"`
}

type temuPlatformOrderLookupResult struct {
	Account         string                    `json:"account"`
	PlatformOrderNo string                    `json:"platform_order_no"`
	Found           bool                      `json:"found"`
	MatchCount      int                       `json:"match_count"`
	Orders          []temuPlatformOrderRecord `json:"orders"`
	QueriedAt       time.Time                 `json:"queried_at"`
}

func (s *Server) temuPlatformOrder(writer http.ResponseWriter, request *http.Request) {
	platformOrderNos, err := normalizePlatformOrderNos([]string{request.PathValue("platformOrderNo")})
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "invalid platform order number"})
		return
	}
	accountKey, err := requiredTemuPlatformOrderAccount(request)
	if err != nil {
		message := "conflicting OMS account selectors"
		if errors.Is(err, errTemuPlatformOrderAccountRequired) {
			message = "X-OMS-Account or account is required"
		}
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: message})
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
		writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "OMS all-orders lookup is unavailable"})
		return
	}
	records, err := lookup.PlatformOrdersByPlatformOrderNo(ctx, platformOrderNos[0])
	if err != nil {
		s.logger.Warn("query OMS platform order for Temu", "account", accountKey, "error", err)
		writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: "unable to query OMS platform order"})
		return
	}

	orders := make([]temuPlatformOrderRecord, 0, len(records))
	for _, record := range records {
		orders = append(orders, newTemuPlatformOrderRecord(record))
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, response{Success: true, Data: temuPlatformOrderLookupResult{
		Account: accountKey, PlatformOrderNo: platformOrderNos[0], Found: len(orders) > 0,
		MatchCount: len(orders), Orders: orders, QueriedAt: time.Now().UTC(),
	}})
}

func requiredTemuPlatformOrderAccount(request *http.Request) (string, error) {
	if strings.TrimSpace(request.Header.Get(platformOrderAccountHeader)) == "" &&
		strings.TrimSpace(request.URL.Query().Get("account")) == "" {
		return "", errTemuPlatformOrderAccountRequired
	}
	return requestedPlatformOrderAccount(request)
}

func newTemuPlatformOrderRecord(order oms.PendingOrder) temuPlatformOrderRecord {
	statusKey, statusText := temuPlatformOrderStatus(order.Status)
	return temuPlatformOrderRecord{
		OMSOrderNo: order.OrderNo, PlatformOrderNo: order.PlatformOrderNo,
		PlatformCode: order.PlatformCode, Status: order.Status, StatusKey: statusKey,
		StatusText: statusText, SubStatus: order.SubStatus,
		SendWarehouseCode: order.SendWarehouseCode, TrackingNumber: order.TrackNo,
		OrderTime: order.OrderTime, CreateTime: order.CreateTime, AuditTime: order.AuditTime,
		MarkShipmentStatus: order.MarkShipmentStatus, MarkShipmentTime: order.MarkShipmentTime,
	}
}

func temuPlatformOrderStatus(status int) (string, string) {
	switch status {
	case 0:
		return temuPlatformOrderStatusPending, "待处理"
	case 1:
		return temuPlatformOrderStatusAwaitingPlatformLabel, "待获取平台面单"
	case 2:
		return temuPlatformOrderStatusProcessing, "处理中"
	case 3:
		return temuPlatformOrderStatusShipped, "已发货"
	case 4:
		return temuPlatformOrderStatusCanceled, "已取消"
	case 5:
		return temuPlatformOrderStatusException, "异常"
	case 6:
		return temuPlatformOrderStatusAwaitingInvoice, "待开票"
	default:
		return temuPlatformOrderStatusUnknown, ""
	}
}
