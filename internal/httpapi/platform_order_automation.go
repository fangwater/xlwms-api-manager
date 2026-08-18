package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/oms"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/temutracking"
)

type platformWarehouseMappingSource interface {
	WarehouseMappings(context.Context) ([]temutracking.WarehouseMapping, error)
}

type platformOrderShipmentSource interface {
	PurchasedShipmentsByPlatformOrderNos(context.Context, []string) (map[string]temutracking.PurchasedShipment, error)
}

type platformOrderFulfillmentSource interface {
	FulfillmentAuditWarehouseEvidenceByPlatformOrderNos(context.Context, []string) ([]model.FulfillmentAudit, error)
}

type automaticRoutingRequest struct {
	PlatformOrderNos []string `json:"platform_order_nos"`
	Account          string   `json:"account"`
}

type automaticAssignmentRequest struct {
	PlatformOrderNos []string `json:"platform_order_nos"`
	Account          string   `json:"account"`
	LogisticsCarrier string   `json:"logistics_carrier"`
	Confirmation     string   `json:"confirmation"`
}

type automaticPlatformOrderRoute struct {
	PlatformOrderNo      string `json:"platform_order_no"`
	PlatformWarehouseID  string `json:"platform_warehouse_id"`
	PlatformWarehouse    string `json:"platform_warehouse_name"`
	WarehouseCode        string `json:"warehouse_code"`
	WarehouseName        string `json:"warehouse_name"`
	internalOrderNo      string
	logisticsChannelCode string
	logisticsChannelName string
	channelGroupFlag     int
	operator             platformOrderOperator
}

type unresolvedPlatformOrderRoute struct {
	PlatformOrderNo string `json:"platform_order_no"`
	Reason          string `json:"reason"`
}

type automaticRoutingPreview struct {
	Ready       bool                           `json:"ready"`
	Routes      []automaticPlatformOrderRoute  `json:"routes"`
	Unresolved  []unresolvedPlatformOrderRoute `json:"unresolved"`
	ChannelCode string                         `json:"channel_code"`
	ChannelName string                         `json:"channel_name"`
	Carriers    []platformOrderCarrierOption   `json:"carriers"`
	QueriedAt   time.Time                      `json:"queried_at"`
}

func (s *Server) platformOrderRoutingPreview(writer http.ResponseWriter, request *http.Request) {
	if s.platformMappings == nil || s.platformFulfillment == nil || s.platformAccounts == nil {
		writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "平台订单自动仓库映射未配置"})
		return
	}
	var payload automaticRoutingRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	platformOrderNos, err := normalizePlatformOrderNos(payload.PlatformOrderNos)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	accountKey, err := requestedPlatformOrderAccountWithBody(request, payload.Account)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "OMS 账户参数冲突"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	operator, err := s.selectedPlatformOrderAccount(ctx, accountKey)
	if err != nil {
		writePlatformOrderAccountError(writer, err)
		return
	}
	preview, err := s.resolveAutomaticPlatformOrderRoutes(ctx, operator, platformOrderNos)
	if err != nil {
		s.logger.Warn("preview automatic OMS platform order routes", "error", err)
		writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: "无法读取购面单仓库记录，请稍后重试"})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: preview})
}

func (s *Server) assignAndApprovePlatformOrdersAuto(writer http.ResponseWriter, request *http.Request) {
	if s.platformMappings == nil || s.platformFulfillment == nil || s.platformAccounts == nil {
		writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "OMS 平台订单自动操作未配置"})
		return
	}
	var payload automaticAssignmentRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	platformOrderNos, err := normalizePlatformOrderNos(payload.PlatformOrderNos)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	payload.LogisticsCarrier = strings.TrimSpace(payload.LogisticsCarrier)
	if payload.LogisticsCarrier != oms.AutoMatchCarrierValue && payload.LogisticsCarrier != oms.OtherCarrierValue {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "物流商仅支持自动匹配或 Other"})
		return
	}
	if payload.Confirmation != "CONFIRM_AND_APPROVE" {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "请确认分配后立即审核"})
		return
	}
	accountKey, err := requestedPlatformOrderAccountWithBody(request, payload.Account)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "OMS 账户参数冲突"})
		return
	}

	s.platformOrderMu.Lock()
	defer s.platformOrderMu.Unlock()
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	operator, err := s.selectedPlatformOrderAccount(ctx, accountKey)
	if err != nil {
		writePlatformOrderAccountError(writer, err)
		return
	}
	preview, err := s.resolveAutomaticPlatformOrderRoutes(ctx, operator, platformOrderNos)
	if err != nil {
		s.logger.Warn("resolve automatic OMS platform order routes", "error", err)
		writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: "无法读取购面单仓库记录，订单未审核"})
		return
	}
	if !preview.Ready {
		first := preview.Unresolved[0]
		message := fmt.Sprintf("%d 个订单无法自动匹配实际发货仓库：%s %s", len(preview.Unresolved), first.PlatformOrderNo, first.Reason)
		writeJSON(writer, http.StatusConflict, response{Success: false, Data: preview, Error: message})
		return
	}

	groups := make(map[string][]automaticPlatformOrderRoute)
	for _, route := range preview.Routes {
		groups[route.WarehouseCode] = append(groups[route.WarehouseCode], route)
	}
	warehouseCodes := make([]string, 0, len(groups))
	for warehouseCode := range groups {
		warehouseCodes = append(warehouseCodes, warehouseCode)
	}
	sort.Strings(warehouseCodes)

	successCount := 0
	failures := make([]assignAndApproveFailure, 0)
	for _, warehouseCode := range warehouseCodes {
		routes := groups[warehouseCode]
		internalOrderNos := make([]string, 0, len(routes))
		platformByInternal := make(map[string]string, len(routes))
		for _, route := range routes {
			internalOrderNos = append(internalOrderNos, route.internalOrderNo)
			platformByInternal[route.internalOrderNo] = route.PlatformOrderNo
		}
		selected := routes[0]
		result, assignErr := selected.operator.AssignAndApprove(ctx, oms.AssignmentRequest{
			Orders: internalOrderNos, WarehouseCode: warehouseCode,
			LogisticsChannelCode: selected.logisticsChannelCode, LogisticsChannelName: selected.logisticsChannelName,
			LogisticsChannelGroupFlag: selected.channelGroupFlag, LogisticsCarrier: payload.LogisticsCarrier,
		})
		if assignErr != nil {
			s.logger.Warn("assign and approve automatically routed OMS platform orders", "warehouse", warehouseCode, "orders", len(routes), "error", assignErr)
			for _, route := range routes {
				failures = append(failures, assignAndApproveFailure{PlatformOrderNo: route.PlatformOrderNo, Error: "OMS 操作结果未确认，请刷新订单状态"})
			}
			continue
		}
		groupSuccess := int(result.SuccessQuantity)
		if groupSuccess < 0 {
			groupSuccess = 0
		}
		if groupSuccess > len(routes) {
			groupSuccess = len(routes)
		}
		successCount += groupSuccess
		for _, failure := range result.FailList {
			failures = append(failures, assignAndApproveFailure{
				PlatformOrderNo: platformByInternal[failure.OrderNo],
				Error:           sanitizeOMSOperationMessage(failure.ErrorMsg),
			})
		}
	}

	warehouseCode := ""
	if len(warehouseCodes) == 1 {
		warehouseCode = warehouseCodes[0]
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: assignAndApproveResult{
		Total: len(preview.Routes), Success: successCount, Failed: len(preview.Routes) - successCount, Failures: failures,
		Account: accountKey, Routes: preview.Routes,
		WarehouseCode: warehouseCode, WarehouseCodes: warehouseCodes, ChannelCode: oms.PlatformLabelChannelCode,
		LogisticsCarrier: payload.LogisticsCarrier, CompletedAt: time.Now().UTC(),
	}})
}

func (s *Server) resolveAutomaticPlatformOrderRoutes(ctx context.Context, operator platformOrderOperator, platformOrderNos []string) (automaticRoutingPreview, error) {
	preview := automaticRoutingPreview{
		Routes: []automaticPlatformOrderRoute{}, Unresolved: []unresolvedPlatformOrderRoute{},
		ChannelCode: oms.PlatformLabelChannelCode, ChannelName: "上传物流面单",
		Carriers:  []platformOrderCarrierOption{{Value: oms.AutoMatchCarrierValue, Label: "自动匹配"}, {Value: oms.OtherCarrierValue, Label: "Other"}},
		QueriedAt: time.Now().UTC(),
	}
	mappings, err := s.platformMappings.WarehouseMappings(ctx)
	if err != nil {
		return preview, fmt.Errorf("query platform warehouse mappings: %w", err)
	}
	mappingsByOMSWarehouse := make(map[string][]temutracking.WarehouseMapping, len(mappings))
	for _, mapping := range mappings {
		code := strings.ToUpper(strings.TrimSpace(mapping.OMSWarehouseCode))
		if code != "" {
			mappingsByOMSWarehouse[code] = append(mappingsByOMSWarehouse[code], mapping)
		}
	}
	fulfillmentAudits, err := s.platformFulfillment.FulfillmentAuditWarehouseEvidenceByPlatformOrderNos(ctx, platformOrderNos)
	if err != nil {
		return preview, fmt.Errorf("query purchased-label fulfillment records: %w", err)
	}
	fulfillmentByPlatformOrder := make(map[string][]model.FulfillmentAudit, len(fulfillmentAudits))
	for _, audit := range fulfillmentAudits {
		if (!audit.Active && !strings.EqualFold(strings.TrimSpace(audit.OMSStatus), "outbound")) ||
			!purchasedLabelPlatform(audit.Platform) {
			continue
		}
		orderNo := strings.ToUpper(strings.TrimSpace(audit.PlatformOrderNo))
		if orderNo != "" {
			fulfillmentByPlatformOrder[orderNo] = append(fulfillmentByPlatformOrder[orderNo], audit)
		}
	}
	if s.platformShipments != nil {
		shipments, shipmentErr := s.platformShipments.PurchasedShipmentsByPlatformOrderNos(ctx, platformOrderNos)
		if shipmentErr != nil {
			for _, platformOrderNo := range platformOrderNos {
				if _, reason := purchasedLabelWarehouse(fulfillmentByPlatformOrder[strings.ToUpper(platformOrderNo)]); reason != "" {
					return preview, fmt.Errorf("query authoritative Temu purchased-label records: %w", shipmentErr)
				}
			}
			s.logger.Warn("Temu purchased-label lookup failed; using local audit cache", "error", shipmentErr)
		} else {
			fulfillmentByPlatformOrder = mergePurchasedLabelEvidence(fulfillmentByPlatformOrder, fulfillmentAuditsFromTemuShipments(shipments))
		}
	}
	purchasedByPlatformOrder := make(map[string]model.FulfillmentAudit, len(platformOrderNos))
	purchaseReasonByPlatformOrder := make(map[string]string)
	accountOrderNosByWarehouse := make(map[string][]string)
	for _, platformOrderNo := range platformOrderNos {
		normalizedOrderNo := strings.ToUpper(platformOrderNo)
		purchased, reason := purchasedLabelWarehouse(fulfillmentByPlatformOrder[normalizedOrderNo])
		if reason != "" {
			purchaseReasonByPlatformOrder[normalizedOrderNo] = reason
			continue
		}
		purchasedByPlatformOrder[normalizedOrderNo] = purchased
		warehouseCode := strings.ToUpper(strings.TrimSpace(purchased.WarehouseCode))
		accountOrderNosByWarehouse[warehouseCode] = append(accountOrderNosByWarehouse[warehouseCode], platformOrderNo)
	}
	orders, err := operator.PendingOrdersByPlatformOrderNos(ctx, platformOrderNos)
	if err != nil {
		return preview, fmt.Errorf("query pending OMS platform orders: %w", err)
	}
	if len(orders) != len(platformOrderNos) {
		for _, orderNo := range platformOrderNos {
			preview.Unresolved = append(preview.Unresolved, unresolvedPlatformOrderRoute{PlatformOrderNo: orderNo, Reason: "订单已不在待处理状态"})
		}
		return preview, nil
	}

	operatorCache := make(map[string]platformOrderOperator)
	warehouseCache := make(map[string]oms.WarehouseOption)
	accountReasons := make(map[string]string)
	accountOrdersByWarehouse := make(map[string]map[string]oms.PendingOrder)
	channelCache := make(map[string]oms.LogisticsChannelOption)
	channelMissing := make(map[string]bool)
	for index, order := range orders {
		platformOrderNo := platformOrderNos[index]
		if order.Status != 0 || order.PlatformOrderNo != platformOrderNo || strings.TrimSpace(order.OrderNo) == "" {
			preview.Unresolved = append(preview.Unresolved, unresolvedPlatformOrderRoute{PlatformOrderNo: platformOrderNo, Reason: "订单状态已变化"})
			continue
		}
		normalizedOrderNo := strings.ToUpper(platformOrderNo)
		if reason := purchaseReasonByPlatformOrder[normalizedOrderNo]; reason != "" {
			preview.Unresolved = append(preview.Unresolved, unresolvedPlatformOrderRoute{PlatformOrderNo: platformOrderNo, Reason: reason})
			continue
		}
		purchased := purchasedByPlatformOrder[normalizedOrderNo]
		warehouseCode := strings.ToUpper(strings.TrimSpace(purchased.WarehouseCode))
		warehouseKey := strings.ToUpper(strings.TrimSpace(purchased.WarehouseKey))
		platformWarehouseID := ""
		platformWarehouseName := warehouseKey
		for _, mapping := range mappingsByOMSWarehouse[warehouseCode] {
			if !strings.EqualFold(strings.TrimSpace(mapping.OMSKey), warehouseKey) {
				continue
			}
			platformWarehouseID = strings.TrimSpace(mapping.TemuWarehouseID)
			if name := strings.TrimSpace(mapping.TemuName); name != "" {
				platformWarehouseName = name
			}
			break
		}
		if _, checked := operatorCache[warehouseCode]; !checked && accountReasons[warehouseCode] == "" {
			warehouseOperator, accountErr := s.platformAccounts.OperatorForWarehouse(ctx, warehouseCode)
			if accountErr != nil {
				if errors.Is(accountErr, store.ErrWarehouseOMSAccountNotConfigured) {
					accountReasons[warehouseCode] = "购面单仓库 " + warehouseCode + " 尚未配置 OMS 发货账号"
				} else {
					accountReasons[warehouseCode] = "购面单仓库 " + warehouseCode + " 的 OMS 发货账号不可用"
				}
			} else {
				warehouses, warehouseErr := warehouseOperator.WarehouseOptions(ctx)
				if warehouseErr != nil {
					accountReasons[warehouseCode] = "购面单仓库 " + warehouseCode + " 的 OMS 发货账号不可用"
				} else {
					for _, option := range warehouses {
						if strings.EqualFold(strings.TrimSpace(option.WarehouseCode), warehouseCode) {
							operatorCache[warehouseCode] = warehouseOperator
							warehouseCache[warehouseCode] = option
							break
						}
					}
					if operatorCache[warehouseCode] == nil {
						accountReasons[warehouseCode] = "配置的 OMS 发货账号无权使用购面单仓库 " + warehouseCode
					} else {
						accountOrders, lookupErr := warehouseOperator.PendingOrdersByPlatformOrderNos(ctx, accountOrderNosByWarehouse[warehouseCode])
						if lookupErr != nil {
							accountReasons[warehouseCode] = "无法使用购面单仓库的 OMS 发货账号查询待处理订单"
						} else {
							accountOrdersByWarehouse[warehouseCode] = make(map[string]oms.PendingOrder, len(accountOrders))
							for _, accountOrder := range accountOrders {
								orderNo := strings.ToUpper(strings.TrimSpace(accountOrder.PlatformOrderNo))
								if orderNo != "" {
									accountOrdersByWarehouse[warehouseCode][orderNo] = accountOrder
								}
							}
						}
					}
				}
			}
		}
		if reason := accountReasons[warehouseCode]; reason != "" {
			preview.Unresolved = append(preview.Unresolved, unresolvedPlatformOrderRoute{PlatformOrderNo: platformOrderNo, Reason: reason})
			continue
		}
		warehouse := warehouseCache[warehouseCode]
		warehouseOperator := operatorCache[warehouseCode]
		accountOrder, found := accountOrdersByWarehouse[warehouseCode][normalizedOrderNo]
		if !found || accountOrder.Status != 0 || strings.TrimSpace(accountOrder.OrderNo) == "" {
			preview.Unresolved = append(preview.Unresolved, unresolvedPlatformOrderRoute{
				PlatformOrderNo: platformOrderNo,
				Reason:          "配置的 OMS 发货账号无法定位该待处理订单",
			})
			continue
		}
		channel, checked := channelCache[warehouseCode]
		if !checked && !channelMissing[warehouseCode] {
			channels, channelErr := warehouseOperator.LogisticsChannels(ctx, warehouseCode)
			if channelErr != nil {
				return preview, fmt.Errorf("query logistics channels for warehouse %s: %w", warehouseCode, channelErr)
			}
			for _, option := range channels {
				if option.IsActivePlatformLabelUpload() {
					channel = option
					channelCache[warehouseCode] = option
					break
				}
			}
			if channel.LogisticsChannel == "" {
				channelMissing[warehouseCode] = true
			}
		}
		if channelMissing[warehouseCode] {
			preview.Unresolved = append(preview.Unresolved, unresolvedPlatformOrderRoute{PlatformOrderNo: platformOrderNo, Reason: "购面单仓库未启用上传物流面单渠道"})
			continue
		}
		preview.Routes = append(preview.Routes, automaticPlatformOrderRoute{
			PlatformOrderNo: platformOrderNo, PlatformWarehouseID: platformWarehouseID, PlatformWarehouse: platformWarehouseName,
			WarehouseCode: warehouse.WarehouseCode, WarehouseName: warehouse.WarehouseName, internalOrderNo: accountOrder.OrderNo,
			logisticsChannelCode: channel.LogisticsChannel, logisticsChannelName: channel.LogisticsChannelName,
			channelGroupFlag: channel.ChannelGroupFlag, operator: warehouseOperator,
		})
	}
	preview.Ready = len(preview.Routes) == len(platformOrderNos) && len(preview.Unresolved) == 0
	return preview, nil
}

func fulfillmentAuditsFromTemuShipments(shipments map[string]temutracking.PurchasedShipment) map[string][]model.FulfillmentAudit {
	result := make(map[string][]model.FulfillmentAudit, len(shipments))
	for orderNo, shipment := range shipments {
		status := strings.ToLower(strings.TrimSpace(shipment.Status))
		reliable := shipment.ConfirmedAt != nil || strings.TrimSpace(shipment.TrackingNumber) != ""
		switch status {
		case "label_ready", "confirming", "confirm_failed", "shipped":
			reliable = true
		}
		warehouseKey := strings.ToUpper(strings.TrimSpace(shipment.OMSWarehouseKey))
		warehouseCode := strings.ToUpper(strings.TrimSpace(shipment.OMSWarehouseCode))
		if !reliable || warehouseKey == "" || warehouseCode == "" {
			continue
		}
		normalizedOrderNo := strings.ToUpper(strings.TrimSpace(orderNo))
		if normalizedOrderNo == "" {
			continue
		}
		result[normalizedOrderNo] = append(result[normalizedOrderNo], model.FulfillmentAudit{
			Platform: "temu", ShopCode: strings.TrimSpace(shipment.StoreCode),
			PlatformOrderNo: normalizedOrderNo, WarehouseKey: warehouseKey,
			WarehouseCode: warehouseCode, TrackingNumber: strings.TrimSpace(shipment.TrackingNumber), Active: true,
		})
	}
	return result
}

func mergePurchasedLabelEvidence(existing, temuShipments map[string][]model.FulfillmentAudit) map[string][]model.FulfillmentAudit {
	merged := make(map[string][]model.FulfillmentAudit, len(existing)+len(temuShipments))
	for orderNo, audits := range existing {
		kept := make([]model.FulfillmentAudit, 0, len(audits))
		for _, audit := range audits {
			if strings.EqualFold(strings.TrimSpace(audit.Platform), "shein") {
				kept = append(kept, audit)
			}
		}
		if len(kept) > 0 {
			merged[orderNo] = kept
		}
	}
	for orderNo, audits := range temuShipments {
		merged[orderNo] = audits
	}
	return merged
}

func purchasedLabelPlatform(platform string) bool {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "temu", "shein":
		return true
	default:
		return false
	}
}

func purchasedLabelWarehouse(audits []model.FulfillmentAudit) (model.FulfillmentAudit, string) {
	purchasedWarehouses := make(map[string]model.FulfillmentAudit)
	for _, audit := range audits {
		warehouseKey := strings.ToUpper(strings.TrimSpace(audit.WarehouseKey))
		warehouseCode := strings.ToUpper(strings.TrimSpace(audit.WarehouseCode))
		if warehouseKey == "" || warehouseCode == "" {
			continue
		}
		purchasedWarehouses[warehouseKey+"\x00"+warehouseCode] = audit
	}
	if len(purchasedWarehouses) == 0 {
		return model.FulfillmentAudit{}, "未找到可靠购面单记录，禁止自动分仓"
	}
	if len(purchasedWarehouses) != 1 {
		return model.FulfillmentAudit{}, "购面单记录包含多个发货仓库，禁止自动分仓"
	}
	for _, audit := range purchasedWarehouses {
		return audit, ""
	}
	return model.FulfillmentAudit{}, "未找到可靠购面单记录，禁止自动分仓"
}
