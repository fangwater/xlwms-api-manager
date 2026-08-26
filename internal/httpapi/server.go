package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"xlwms-api-manager/internal/auditor"
	"xlwms-api-manager/internal/config"
	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/syncer"
	"xlwms-api-manager/internal/xlwms"
)

type Server struct {
	store                *store.Postgres
	warehouseCredentials warehouseCredentialSource
	syncer               *syncer.Service
	requestTimeout       time.Duration
	logger               *slog.Logger
	fulfillmentAuditor   *auditor.Service
	platformOrders       platformOrderSource
	platformMappings     platformWarehouseMappingSource
	platformShipments    platformOrderShipmentSource
	platformSheinLabels  platformOrderSheinLabelSource
	platformFulfillment  platformOrderFulfillmentSource
	platformAccounts     platformOrderAccountSource
	platformOrderMu      sync.Mutex
}

type response struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

func New(destination *store.Postgres, service *syncer.Service, fulfillmentAuditor *auditor.Service, requestTimeout time.Duration, logger *slog.Logger) http.Handler {
	return NewWithPlatformOrders(destination, service, fulfillmentAuditor, nil, requestTimeout, logger)
}

func NewWithPlatformOrders(destination *store.Postgres, service *syncer.Service, fulfillmentAuditor *auditor.Service, platformOrders platformOrderSource, requestTimeout time.Duration, logger *slog.Logger) http.Handler {
	return NewWithPlatformOrderOperations(destination, service, fulfillmentAuditor, platformOrders, nil, requestTimeout, logger)
}

func NewWithPlatformOrderOperations(destination *store.Postgres, service *syncer.Service, fulfillmentAuditor *auditor.Service, platformOrders platformOrderSource, platformMappings platformWarehouseMappingSource, requestTimeout time.Duration, logger *slog.Logger) http.Handler {
	return newWithPlatformOrderOperations(destination, service, fulfillmentAuditor, platformOrders, platformMappings, destination, requestTimeout, logger)
}

func NewWithWarehousePlatformOrderOperations(destination *store.Postgres, service *syncer.Service, fulfillmentAuditor *auditor.Service, platformOrders platformOrderSource, platformMappings platformWarehouseMappingSource, omsBaseURL, omsUsername, omsPassword string, requestTimeout time.Duration, logger *slog.Logger) http.Handler {
	shared, _ := platformOrders.(platformOrderAccount)
	if !platformOrderAccountAvailable(shared) {
		shared = nil
	}
	accounts := &postgresPlatformOrderAccounts{
		store: destination, baseURL: omsBaseURL, timeout: requestTimeout, shared: shared,
		sharedUsername: omsUsername, sharedPassword: omsPassword,
	}
	return newWithPlatformOrderAccountOperations(destination, service, fulfillmentAuditor, platformOrders, platformMappings, destination, accounts, requestTimeout, logger)
}

func newWithPlatformOrderOperations(destination *store.Postgres, service *syncer.Service, fulfillmentAuditor *auditor.Service, platformOrders platformOrderSource, platformMappings platformWarehouseMappingSource, platformFulfillment platformOrderFulfillmentSource, requestTimeout time.Duration, logger *slog.Logger) http.Handler {
	var accounts platformOrderAccountSource
	if operator, ok := platformOrders.(platformOrderOperator); ok {
		accounts = fixedPlatformOrderAccounts{operator: operator}
	}
	return newWithPlatformOrderAccountOperations(destination, service, fulfillmentAuditor, platformOrders, platformMappings, platformFulfillment, accounts, requestTimeout, logger)
}

func newWithPlatformOrderAccountOperations(destination *store.Postgres, service *syncer.Service, fulfillmentAuditor *auditor.Service, platformOrders platformOrderSource, platformMappings platformWarehouseMappingSource, platformFulfillment platformOrderFulfillmentSource, platformAccounts platformOrderAccountSource, requestTimeout time.Duration, logger *slog.Logger) http.Handler {
	var platformShipments platformOrderShipmentSource
	if source, ok := platformMappings.(platformOrderShipmentSource); ok {
		platformShipments = source
	}
	var platformSheinLabels platformOrderSheinLabelSource
	if source, ok := platformMappings.(platformOrderSheinLabelSource); ok {
		platformSheinLabels = source
	}
	server := &Server{
		store: destination, warehouseCredentials: destination, syncer: service, fulfillmentAuditor: fulfillmentAuditor, platformOrders: platformOrders,
		platformMappings: platformMappings, platformShipments: platformShipments, platformSheinLabels: platformSheinLabels,
		platformFulfillment: platformFulfillment, platformAccounts: platformAccounts,
		requestTimeout: requestTimeout, logger: logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /v1/dashboard/summary", server.dashboard)
	mux.HandleFunc("GET /v1/platform-orders/accounts", server.listPlatformOrderAccounts)
	mux.HandleFunc("PATCH /v1/platform-orders/accounts/{accountKey}", server.updatePlatformOrderAccount)
	mux.HandleFunc("GET /v1/platform-orders/pending", server.pendingPlatformOrders)
	mux.HandleFunc("GET /v1/platform-orders/{platformOrderNo}", server.platformOrder)
	mux.HandleFunc("GET /v1/temu/platform-orders/{platformOrderNo}", server.temuPlatformOrder)
	mux.HandleFunc("POST /v1/platform-orders/routing-preview", server.platformOrderRoutingPreview)
	mux.HandleFunc("POST /v1/platform-orders/warehouse-assignments", server.assignAndApprovePlatformOrdersAuto)
	mux.HandleFunc("POST /v1/platform-orders/assign-and-approve", server.assignAndApprovePlatformOrdersAuto)
	mux.HandleFunc("GET /v1/warehouses", server.listWarehouses)
	mux.HandleFunc("POST /v1/warehouses", server.upsertWarehouse)
	mux.HandleFunc("PATCH /v1/warehouses/{code}/status", server.warehouseStatus)
	mux.HandleFunc("PATCH /v1/warehouses/{code}/oms-account", server.setWarehouseOMSAccount)
	mux.HandleFunc("PUT /v1/warehouses/{code}/oms-account", server.setWarehouseOMSAccount)
	mux.HandleFunc("DELETE /v1/warehouses/{code}/oms-account", server.clearWarehouseOMSAccount)
	mux.HandleFunc("GET /v1/funds-flows", server.fundsFlows)
	mux.HandleFunc("GET /v1/cost-details", server.costDetails)
	mux.HandleFunc("GET /v1/cost-details/{warehouse}/{costNo}/items", server.costItems)
	mux.HandleFunc("GET /v1/inventory", server.inventory)
	mux.HandleFunc("GET /v1/inventory/sku-levels", server.skuStockLevels)
	mux.HandleFunc("GET /v1/inventory-corrections", server.listInventoryCorrections)
	mux.HandleFunc("PATCH /v1/inventory-corrections/{warehouse}/{warehouseSKU}", server.saveInventoryCorrection)
	mux.HandleFunc("POST /v1/inventory-corrections/{warehouse}/{warehouseSKU}/reset", server.deleteInventoryCorrection)
	mux.HandleFunc("GET /v1/inventory-alerts", server.listInventoryAlerts)
	mux.HandleFunc("PATCH /v1/inventory-alerts/default", server.updateInventoryAlertDefault)
	mux.HandleFunc("PATCH /v1/inventory-alerts/config", server.updateInventoryAlertConfig)
	mux.HandleFunc("POST /v1/inventory-alerts/config/reset", server.resetInventoryAlertConfig)
	mux.HandleFunc("GET /v1/warehouse-sku-specs", server.listWarehouseSKUSpecs)
	mux.HandleFunc("POST /v1/warehouse-sku-specs", server.saveWarehouseSKUSpec)
	mux.HandleFunc("PATCH /v1/warehouse-sku-specs/{warehouseSKU}", server.updateWarehouseSKUSpec)
	mux.HandleFunc("PATCH /v1/warehouse-sku-specs/{warehouseSKU}/package", server.updateWarehouseSKUPackageSpec)
	mux.HandleFunc("POST /v1/warehouse-sku-specs/resolve", server.resolveWarehouseSKUSpecs)
	mux.HandleFunc("POST /v1/packing/plans", server.createPackingPlan)
	mux.HandleFunc("GET /v1/fulfillment-shops", server.listFulfillmentShops)
	mux.HandleFunc("GET /v1/inventory-thresholds", server.listInventoryThresholds)
	mux.HandleFunc("GET /v1/inventory-thresholds/defaults", server.inventoryThresholdDefaults)
	mux.HandleFunc("PATCH /v1/inventory-thresholds/defaults", server.updateInventoryThresholdDefaults)
	mux.HandleFunc("POST /v1/inventory-thresholds/defaults/reset", server.resetShopInventoryThresholds)
	mux.HandleFunc("PATCH /v1/inventory-thresholds/{warehouseSKU}", server.updateSKUInventoryThreshold)
	mux.HandleFunc("POST /v1/inventory-thresholds/{warehouseSKU}/reset", server.deleteSKUInventoryThreshold)
	mux.HandleFunc("POST /v1/temu/warehouse-availability/query", server.temuWarehouseAvailability)
	mux.HandleFunc("GET /v1/fulfillment-audits", server.listFulfillmentAudits)
	mux.HandleFunc("GET /v1/fulfillment-audits/archived", server.listArchivedFulfillmentAudits)
	mux.HandleFunc("GET /v1/fulfillment-audits/export-manual", server.exportManualFulfillmentAudits)
	mux.HandleFunc("POST /v1/fulfillment-audits/{id}/resolve", server.resolveFulfillmentAudit)
	mux.HandleFunc("POST /v1/fulfillment-audits/sync", server.syncFulfillmentAudits)
	mux.HandleFunc("GET /v1/outbound-orders", server.listOutboundOrders)
	mux.HandleFunc("POST /v1/inventory/query/{kind}", server.queryInventory)
	mux.HandleFunc("POST /v1/outbound/{operation}", server.outbound)
	mux.HandleFunc("GET /v1/sync/runs", server.syncRuns)
	mux.HandleFunc("POST /v1/sync/funds-flow", server.syncFundsFlow)
	mux.HandleFunc("POST /v1/sync/cost-details", server.syncCostDetails)
	mux.HandleFunc("POST /v1/sync/inventory", server.syncInventory)
	return securityHeaders(mux)
}

func (s *Server) health(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]string{"status": "ok", "service": "xlwms-manager"}})
}

func (s *Server) dashboard(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	warehouse := request.URL.Query().Get("warehouse")
	operations, err := s.store.Dashboard(ctx, warehouse, queryInt(request, "days", 14))
	if err != nil {
		s.internalError(writer, "load dashboard operations", err)
		return
	}
	inventory, err := s.store.InventorySummary(ctx, warehouse)
	if err != nil {
		s.internalError(writer, "load dashboard inventory", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]any{"operations": operations, "inventory": inventory}})
}

func (s *Server) listWarehouses(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	warehouses, err := s.store.ListWarehousesWithOMS(ctx, request.URL.Query().Get("active_only") == "true")
	if err != nil {
		s.internalError(writer, "list warehouses", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: warehouses})
}

type warehouseRequest struct {
	Code       string `json:"wh_code"`
	Name       string `json:"name"`
	APIBaseURL string `json:"api_base_url"`
	AppKey     string `json:"app_key"`
	AppSecret  string `json:"app_secret"`
	Active     *bool  `json:"active,omitempty"`
}

func (s *Server) upsertWarehouse(writer http.ResponseWriter, request *http.Request) {
	var payload warehouseRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if payload.APIBaseURL == "" {
		payload.APIBaseURL = config.DefaultAPIBaseURL
	}
	active := true
	if payload.Active != nil {
		active = *payload.Active
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	warehouse, err := s.store.UpsertWarehouse(ctx, payload.Code, payload.Name, payload.APIBaseURL, payload.AppKey, payload.AppSecret, active)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: warehouse})
}

func (s *Server) warehouseStatus(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Active *bool `json:"active"`
	}
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if payload.Active == nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "active is required"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	warehouse, err := s.store.SetWarehouseActive(ctx, request.PathValue("code"), *payload.Active)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: warehouse})
}

func (s *Server) fundsFlows(writer http.ResponseWriter, request *http.Request) {
	startDate, endDate, err := queryDateRange(request)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, total, err := s.store.ListFundsFlows(ctx, store.FundsFlowFilter{
		WarehouseCode: request.URL.Query().Get("warehouse"), Query: request.URL.Query().Get("q"),
		DetailStatus: request.URL.Query().Get("detail_status"), StartDate: startDate, EndDate: endDate,
		Page: queryInt(request, "page", 1), PageSize: queryInt(request, "page_size", 30),
	})
	if err != nil {
		s.internalError(writer, "list funds flows", err)
		return
	}
	writePage(writer, items, total, queryInt(request, "page", 1), queryInt(request, "page_size", 30))
}

func (s *Server) costDetails(writer http.ResponseWriter, request *http.Request) {
	startDate, endDate, err := queryDateRange(request)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	items, total, err := s.store.ListCostDetails(ctx, store.CostDetailFilter{
		WarehouseCode: request.URL.Query().Get("warehouse"), Query: request.URL.Query().Get("q"),
		StartDate: startDate, EndDate: endDate, Page: page, PageSize: pageSize,
	})
	if err != nil {
		s.internalError(writer, "list cost details", err)
		return
	}
	writePage(writer, items, total, page, pageSize)
}

func (s *Server) costItems(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, err := s.store.CostItems(ctx, request.PathValue("warehouse"), request.PathValue("costNo"))
	if err != nil {
		s.internalError(writer, "list cost items", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: items})
}

func (s *Server) inventory(writer http.ResponseWriter, request *http.Request) {
	var stockType *int
	if value := request.URL.Query().Get("stock_type"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "invalid stock_type"})
			return
		}
		stockType = &parsed
	}
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, total, err := s.store.ListInventory(ctx, store.InventoryFilter{Kind: request.URL.Query().Get("kind"), WarehouseCode: request.URL.Query().Get("warehouse"), Query: request.URL.Query().Get("q"), StockType: stockType, Page: page, PageSize: pageSize})
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	writePage(writer, items, total, page, pageSize)
}
func (s *Server) skuStockLevels(writer http.ResponseWriter, request *http.Request) {
	var stockType *int
	if value := request.URL.Query().Get("stock_type"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "invalid stock_type"})
			return
		}
		stockType = &parsed
	}
	page, pageSize := queryInt(request, "page", 1), queryInt(request, "page_size", 30)
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	items, total, summary, err := s.store.ListSKUStockLevels(ctx, store.InventoryFilter{
		WarehouseCode: request.URL.Query().Get("warehouse"),
		Query:         request.URL.Query().Get("q"),
		StockType:     stockType,
		Page:          page,
		PageSize:      pageSize,
	})
	if err != nil {
		s.internalError(writer, "list SKU stock levels", err)
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]any{
		"records": items, "total": total, "page": page, "page_size": pageSize, "pages": pages, "summary": summary,
	}})
}

type inventoryQueryRequest struct {
	Warehouse  string         `json:"warehouse"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	Parameters map[string]any `json:"parameters"`
}

func (s *Server) queryInventory(writer http.ResponseWriter, request *http.Request) {
	kind := request.PathValue("kind")
	if _, ok := xlwms.InventoryPaths[kind]; !ok {
		writeJSON(writer, http.StatusNotFound, response{Success: false, Error: "unknown inventory kind"})
		return
	}
	var payload inventoryQueryRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if payload.Page < 1 {
		payload.Page = 1
	}
	if payload.PageSize < 1 {
		payload.PageSize = 50
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	warehouse, err := s.store.WarehouseCredentials(ctx, payload.Warehouse, true)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	if payload.Parameters == nil {
		payload.Parameters = map[string]any{}
	}
	if kind == "integrated" {
		payload.Parameters["whCodeList"] = warehouse.Code
	} else {
		payload.Parameters["whCodeList"] = []any{warehouse.Code}
	}
	client := xlwms.NewClient(warehouse.APIBaseURL, warehouse.AppKey, warehouse.AppSecret, s.requestTimeout)
	result, err := client.PageInventory(ctx, kind, payload.Parameters, payload.Page, payload.PageSize)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: result})
}

func (s *Server) syncRuns(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	runs, err := s.store.RecentSyncRuns(ctx, queryInt(request, "limit", 30))
	if err != nil {
		s.internalError(writer, "list sync runs", err)
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: runs})
}

type fundsSyncRequest struct {
	Warehouse  string         `json:"warehouse"`
	AllActive  bool           `json:"all_active"`
	PageSize   int            `json:"page_size"`
	Parameters map[string]any `json:"parameters"`
}

func (s *Server) syncFundsFlow(writer http.ResponseWriter, request *http.Request) {
	var payload fundsSyncRequest
	if !decodeOptionalJSON(writer, request, &payload) {
		return
	}
	if payload.PageSize < 1 {
		payload.PageSize = 100
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	var warehouses []model.WarehouseCredentials
	var err error
	if payload.AllActive {
		warehouses, err = s.store.ActiveWarehouseCredentials(ctx)
	} else {
		var warehouse model.WarehouseCredentials
		warehouse, err = s.store.WarehouseCredentials(ctx, payload.Warehouse, true)
		warehouses = []model.WarehouseCredentials{warehouse}
	}
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	runs, err := s.syncer.TriggerFundsFlow(warehouses, payload.Parameters, payload.PageSize)
	s.writeTrigger(writer, runs, err)
}

type costSyncRequest struct {
	Warehouse         string  `json:"warehouse"`
	Workers           int     `json:"workers"`
	RequestsPerSecond float64 `json:"requests_per_second"`
	MaxAttempts       int     `json:"max_attempts"`
	Limit             int     `json:"limit"`
}

func (s *Server) syncCostDetails(writer http.ResponseWriter, request *http.Request) {
	var payload costSyncRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if payload.Workers < 1 {
		payload.Workers = 4
	}
	if payload.RequestsPerSecond <= 0 {
		payload.RequestsPerSecond = 8
	}
	if payload.MaxAttempts < 1 {
		payload.MaxAttempts = 3
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	warehouse, err := s.store.WarehouseCredentials(ctx, payload.Warehouse, true)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	run, err := s.syncer.TriggerCostDetails(warehouse, syncer.DetailOptions{Workers: payload.Workers, RequestsPerSecond: payload.RequestsPerSecond, MaxAttempts: payload.MaxAttempts, Limit: payload.Limit})
	s.writeTrigger(writer, run, err)
}

type inventorySyncRequest struct {
	Warehouse  string                    `json:"warehouse"`
	Kinds      []string                  `json:"kinds"`
	PageSize   int                       `json:"page_size"`
	Parameters map[string]map[string]any `json:"parameters"`
}

func (s *Server) syncInventory(writer http.ResponseWriter, request *http.Request) {
	var payload inventorySyncRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if len(payload.Kinds) == 0 {
		payload.Kinds = []string{"integrated", "stock_age", "stock_flow", "box_stock", "box_stock_age", "box_segment_age", "box_stock_flow"}
	}
	if payload.PageSize < 1 {
		payload.PageSize = 100
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	warehouse, err := s.store.WarehouseCredentials(ctx, payload.Warehouse, true)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: err.Error()})
		return
	}
	runs, err := s.syncer.TriggerInventory(warehouse, payload.Kinds, payload.Parameters, payload.PageSize)
	s.writeTrigger(writer, runs, err)
}

func (s *Server) writeTrigger(writer http.ResponseWriter, data any, err error) {
	if err == nil {
		writeJSON(writer, http.StatusAccepted, response{Success: true, Data: data})
		return
	}
	status := http.StatusBadRequest
	if errors.Is(err, syncer.ErrAlreadyRunning) {
		status = http.StatusConflict
	}
	writeJSON(writer, status, response{Success: false, Error: err.Error()})
}
func (s *Server) internalError(writer http.ResponseWriter, operation string, err error) {
	s.logger.Error(operation, "error", err)
	writeJSON(writer, http.StatusInternalServerError, response{Success: false, Error: "internal service error"})
}
func queryInt(request *http.Request, name string, fallback int) int {
	value, err := strconv.Atoi(request.URL.Query().Get(name))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func queryDateRange(request *http.Request) (string, string, error) {
	startDate := strings.TrimSpace(request.URL.Query().Get("start_date"))
	endDate := strings.TrimSpace(request.URL.Query().Get("end_date"))
	var start, end time.Time
	var err error
	if startDate != "" {
		start, err = time.Parse(time.DateOnly, startDate)
		if err != nil {
			return "", "", errors.New("start_date must use YYYY-MM-DD")
		}
	}
	if endDate != "" {
		end, err = time.Parse(time.DateOnly, endDate)
		if err != nil {
			return "", "", errors.New("end_date must use YYYY-MM-DD")
		}
	}
	if !start.IsZero() && !end.IsZero() && end.Before(start) {
		return "", "", errors.New("end_date must not be before start_date")
	}
	return startDate, endDate, nil
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	return decode(writer, request, target, false)
}
func decodeOptionalJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	return decode(writer, request, target, true)
}
func decode(writer http.ResponseWriter, request *http.Request, target any, optional bool) bool {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(target)
	if optional && errors.Is(err, io.EOF) {
		return true
	}
	if err != nil {
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
func writePage(writer http.ResponseWriter, items any, total, page, pageSize int) {
	pages := (total + pageSize - 1) / pageSize
	writeJSON(writer, http.StatusOK, response{Success: true, Data: map[string]any{"records": items, "total": total, "page": page, "page_size": pageSize, "pages": pages}})
}
func writeJSON(writer http.ResponseWriter, status int, value response) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(writer, request)
	})
}
