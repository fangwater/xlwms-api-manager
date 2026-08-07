package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/oms"
	"xlwms-api-manager/internal/temutracking"
)

type platformWarehouseMappingSource interface {
	WarehouseMappings(context.Context) ([]temutracking.WarehouseMapping, error)
}

type platformOrderFulfillmentSource interface {
	FulfillmentAuditsByPlatformOrderNos(context.Context, []string) ([]model.FulfillmentAudit, error)
}

type automaticRoutingRequest struct {
	PlatformOrderNos []string `json:"platform_order_nos"`
}

type automaticAssignmentRequest struct {
	PlatformOrderNos []string `json:"platform_order_nos"`
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
	if _, ok := s.platformOrders.(platformOrderOperator); !ok || s.platformMappings == nil || s.platformFulfillment == nil {
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
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	preview, err := s.resolveAutomaticPlatformOrderRoutes(ctx, platformOrderNos)
	if err != nil {
		s.logger.Warn("preview automatic OMS platform order routes", "error", err)
		writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: "无法读取购面单仓库记录，请稍后重试"})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: preview})
}

func (s *Server) assignAndApprovePlatformOrdersAuto(writer http.ResponseWriter, request *http.Request) {
	operator, ok := s.platformOrders.(platformOrderOperator)
	if !ok || s.platformMappings == nil || s.platformFulfillment == nil {
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

	s.platformOrderMu.Lock()
	defer s.platformOrderMu.Unlock()
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	preview, err := s.resolveAutomaticPlatformOrderRoutes(ctx, platformOrderNos)
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
		result, assignErr := operator.AssignAndApprove(ctx, oms.AssignmentRequest{
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
		groupSuccess := result.SuccessQuantity
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
		WarehouseCode: warehouseCode, WarehouseCodes: warehouseCodes, ChannelCode: oms.PlatformLabelChannelCode,
		LogisticsCarrier: payload.LogisticsCarrier, CompletedAt: time.Now().UTC(),
	}})
}

func (s *Server) resolveAutomaticPlatformOrderRoutes(ctx context.Context, platformOrderNos []string) (automaticRoutingPreview, error) {
	operator := s.platformOrders.(platformOrderOperator)
	preview := automaticRoutingPreview{
		Routes: []automaticPlatformOrderRoute{}, Unresolved: []unresolvedPlatformOrderRoute{},
		ChannelCode: oms.PlatformLabelChannelCode, ChannelName: "上传物流面单",
		Carriers:  []platformOrderCarrierOption{{Value: oms.AutoMatchCarrierValue, Label: "自动匹配"}, {Value: oms.OtherCarrierValue, Label: "Other"}},
		QueriedAt: time.Now().UTC(),
	}
	warehouses, err := operator.WarehouseOptions(ctx)
	if err != nil {
		return preview, fmt.Errorf("query OMS warehouse options: %w", err)
	}
	activeWarehouses := make(map[string]oms.WarehouseOption, len(warehouses))
	for _, warehouse := range warehouses {
		activeWarehouses[strings.ToUpper(strings.TrimSpace(warehouse.WarehouseCode))] = warehouse
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
	fulfillmentAudits, err := s.platformFulfillment.FulfillmentAuditsByPlatformOrderNos(ctx, platformOrderNos)
	if err != nil {
		return preview, fmt.Errorf("query purchased-label fulfillment records: %w", err)
	}
	fulfillmentByPlatformOrder := make(map[string][]model.FulfillmentAudit, len(fulfillmentAudits))
	for _, audit := range fulfillmentAudits {
		if !audit.Active || !strings.EqualFold(strings.TrimSpace(audit.Platform), "temu") {
			continue
		}
		orderNo := strings.ToUpper(strings.TrimSpace(audit.PlatformOrderNo))
		if orderNo != "" {
			fulfillmentByPlatformOrder[orderNo] = append(fulfillmentByPlatformOrder[orderNo], audit)
		}
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

	channelCache := make(map[string]oms.LogisticsChannelOption)
	channelMissing := make(map[string]bool)
	for index, order := range orders {
		platformOrderNo := platformOrderNos[index]
		if order.Status != 0 || order.PlatformOrderNo != platformOrderNo || strings.TrimSpace(order.OrderNo) == "" {
			preview.Unresolved = append(preview.Unresolved, unresolvedPlatformOrderRoute{PlatformOrderNo: platformOrderNo, Reason: "订单状态已变化"})
			continue
		}
		purchasedWarehouses := make(map[string]model.FulfillmentAudit)
		for _, audit := range fulfillmentByPlatformOrder[strings.ToUpper(platformOrderNo)] {
			warehouseKey := strings.ToUpper(strings.TrimSpace(audit.WarehouseKey))
			warehouseCode := strings.ToUpper(strings.TrimSpace(audit.WarehouseCode))
			if warehouseKey == "" || warehouseCode == "" {
				continue
			}
			purchasedWarehouses[warehouseKey+"\x00"+warehouseCode] = audit
		}
		if len(purchasedWarehouses) == 0 {
			preview.Unresolved = append(preview.Unresolved, unresolvedPlatformOrderRoute{PlatformOrderNo: platformOrderNo, Reason: "未找到可靠购面单记录，禁止自动分仓"})
			continue
		}
		if len(purchasedWarehouses) != 1 {
			preview.Unresolved = append(preview.Unresolved, unresolvedPlatformOrderRoute{PlatformOrderNo: platformOrderNo, Reason: "购面单记录包含多个发货仓库，禁止自动分仓"})
			continue
		}
		var purchased model.FulfillmentAudit
		for _, audit := range purchasedWarehouses {
			purchased = audit
		}
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
		warehouse, available := activeWarehouses[warehouseCode]
		if !available {
			preview.Unresolved = append(preview.Unresolved, unresolvedPlatformOrderRoute{PlatformOrderNo: platformOrderNo, Reason: "当前 OMS 发货账号无权使用购面单仓库 " + warehouseCode})
			continue
		}
		channel, checked := channelCache[warehouseCode]
		if !checked && !channelMissing[warehouseCode] {
			channels, channelErr := operator.LogisticsChannels(ctx, warehouseCode)
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
			WarehouseCode: warehouse.WarehouseCode, WarehouseName: warehouse.WarehouseName, internalOrderNo: order.OrderNo,
			logisticsChannelCode: channel.LogisticsChannel, logisticsChannelName: channel.LogisticsChannelName,
			channelGroupFlag: channel.ChannelGroupFlag,
		})
	}
	preview.Ready = len(preview.Routes) == len(platformOrderNos) && len(preview.Unresolved) == 0
	return preview, nil
}
