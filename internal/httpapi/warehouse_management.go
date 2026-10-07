package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"xlwms-api-manager/internal/fulfillment"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/xlwms"
)

type platformWarehouseOptionSource interface {
	PlatformWarehouseOptions(context.Context, string, string, string) ([]fulfillment.PlatformWarehouseOption, error)
}
type warehouseManagementRequest struct {
	Platform          string `json:"platform"`
	ShopCode          string `json:"shop_code"`
	OrderNo           string `json:"order_no"`
	ID                string `json:"platform_warehouse_id"`
	Name              string `json:"platform_warehouse_name"`
	Revision          int64  `json:"revision"`
	WarehouseRevision int64  `json:"warehouse_revision"`
	Enabled           *bool  `json:"enabled,omitempty"`
}

func (s *Server) registerWarehouseManagementRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/fulfillment-warehouses/{key}/activity", s.warehouseActivity)
	mux.HandleFunc("GET /v1/fulfillment-warehouse-bindings", s.runtimeWarehouseBindings)
	mux.HandleFunc("GET /v1/fulfillment-warehouses/{key}/platform-bindings", s.listWarehouseBindings)
	mux.HandleFunc("GET /v1/fulfillment-warehouses/{key}/history", s.warehouseConfigurationHistory)
	mux.HandleFunc("PATCH /v1/fulfillment-warehouses/{key}", s.requireConsoleAuth(s.updateFulfillmentWarehouse))
	mux.HandleFunc("PUT /v1/fulfillment-warehouses/{key}/platform-bindings/{platform}/{shop}", s.requireConsoleAuth(s.saveWarehouseBinding))
	mux.HandleFunc("POST /v1/fulfillment-warehouses/{key}/platform-options/query", s.requireConsoleAuth(s.platformWarehouseOptions))
	mux.HandleFunc("POST /v1/fulfillment-warehouses/{key}/readiness", s.requireConsoleAuth(s.warehouseReadiness))
	mux.HandleFunc("POST /v1/fulfillment-warehouses/{key}/activation", s.requireConsoleAuth(s.activateWarehouseBinding))
}
func warehouseIdentity(w http.ResponseWriter, r *http.Request) (fulfillment.Warehouse, bool) {
	item, ok := fulfillment.Find(r.PathValue("key"))
	if !ok {
		writeJSON(w, 404, response{Success: false, Error: "仓库不存在"})
	}
	return item, ok
}
func (s *Server) warehouseManagementError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, store.ErrWarehouseConfigurationConflict) {
		status = http.StatusConflict
	}
	writeJSON(w, status, response{Success: false, Error: err.Error()})
}
func (s *Server) runtimeWarehouseBindings(w http.ResponseWriter, r *http.Request) {
	platform := strings.ToLower(r.URL.Query().Get("platform"))
	shop := r.URL.Query().Get("shop")
	if (platform != "temu" && platform != "shein") || shop == "" {
		writeJSON(w, 400, response{Success: false, Error: "platform 和 shop 必填"})
		return
	}
	s.listWarehouseBindingsFor(w, r, "", platform, shop)
}
func (s *Server) listWarehouseBindings(w http.ResponseWriter, r *http.Request) {
	item, ok := warehouseIdentity(w, r)
	if !ok {
		return
	}
	s.listWarehouseBindingsFor(w, r, item.Key, "", "")
}
func (s *Server) listWarehouseBindingsFor(w http.ResponseWriter, r *http.Request, key, platform, shop string) {
	items, err := s.store.WarehouseBindings(r.Context(), key, platform, shop)
	if err != nil {
		s.internalError(w, "load warehouse bindings", err)
		return
	}
	if len(items) == 0 && shop != "" {
		writeJSON(w, 404, response{Success: false, Error: "店铺不存在"})
		return
	}
	writeJSON(w, 200, response{Success: true, Data: items})
}
func (s *Server) saveWarehouseBinding(w http.ResponseWriter, r *http.Request) {
	item, ok := warehouseIdentity(w, r)
	if !ok {
		return
	}
	var in warehouseManagementRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Enabled != nil && *in.Enabled {
		writeJSON(w, 400, response{Success: false, Error: "请先检查配置，再使用启用操作"})
		return
	}
	in.ID = strings.TrimSpace(in.ID)
	in.Name = strings.TrimSpace(in.Name)
	if len(in.ID) > 200 || len(in.Name) > 200 {
		writeJSON(w, 400, response{Success: false, Error: "平台仓 ID 或名称过长"})
		return
	}
	if in.ID == "" {
		writeJSON(w, 400, response{Success: false, Error: "请填写真实平台仓 ID"})
		return
	}
	actor, _, _ := r.BasicAuth()
	s.warehouseConfigMu.Lock()
	defer s.warehouseConfigMu.Unlock()
	b, err := s.store.SaveWarehouseBinding(r.Context(), item.Key, r.PathValue("platform"), r.PathValue("shop"), in.ID, in.Name, in.Revision, in.WarehouseRevision, actor)
	if err != nil {
		s.warehouseManagementError(w, err)
		return
	}
	writeJSON(w, 200, response{Success: true, Data: b})
}
func (s *Server) queryPlatformWarehouseOptions(ctx context.Context, platform, shop, order string) ([]fulfillment.PlatformWarehouseOption, error) {
	registered, err := s.store.FulfillmentShop(ctx, platform, shop)
	if err != nil || !registered.Enabled {
		return nil, errors.New("店铺不存在或已停用")
	}
	source, ok := s.platformMappings.(platformWarehouseOptionSource)
	if !ok {
		return nil, errors.New("平台仓库查询服务尚未配置")
	}
	return source.PlatformWarehouseOptions(ctx, platform, shop, order)
}
func (s *Server) platformWarehouseOptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := warehouseIdentity(w, r); !ok {
		return
	}
	var in warehouseManagementRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
	defer cancel()
	options, err := s.queryPlatformWarehouseOptions(ctx, in.Platform, in.ShopCode, in.OrderNo)
	if err != nil {
		s.warehouseManagementError(w, errors.New("无法验证当前店铺的平台仓；请同步平台注册，SHEIN 请选择有效的独立履约订单后重试"))
		return
	}
	writeJSON(w, 200, response{Success: true, Data: options})
}
func (s *Server) checkWarehouseSetup(ctx context.Context, item fulfillment.Warehouse, in warehouseManagementRequest) (fulfillment.WarehouseReadiness, error) {
	bindings, err := s.store.WarehouseBindings(ctx, item.Key, in.Platform, in.ShopCode)
	if err != nil {
		return fulfillment.WarehouseReadiness{}, err
	}
	if len(bindings) != 1 {
		return fulfillment.WarehouseReadiness{}, errors.New("店铺不存在")
	}
	b := bindings[0]
	c := fulfillment.WarehouseReadiness{WarehouseKey: item.Key, Platform: in.Platform, ShopCode: in.ShopCode, Revision: b.Revision, WarehouseRevision: b.WarehouseRevision, Ready: true, Checks: []fulfillment.WarehouseCheck{}, CheckedAt: time.Now().UTC()}
	add := func(code, label string, passed bool, message string) {
		c.Checks = append(c.Checks, fulfillment.WarehouseCheck{Code: code, Label: label, Passed: passed, Message: message})
		c.Ready = c.Ready && passed
	}
	add("shop", "店铺状态", b.ShopEnabled, chooseMessage(b.ShopEnabled, "店铺已启用", "请先在店铺管理中启用店铺"))
	all, err := s.store.FulfillmentWarehouses(ctx)
	if err != nil {
		return c, err
	}
	var connection bool
	for _, v := range all {
		if v.Key == item.Key {
			connection = v.InventoryConnected
		}
	}
	add("connection", "库存数据连接", connection, chooseMessage(connection, "库存连接已开启", "请恢复库存数据连接并同步 API 数据范围"))
	credentials, err := s.store.WarehouseSetupCredentials(ctx, item.Code)
	if err != nil {
		return c, err
	}
	add("api_scope", "API 数据范围", len(credentials) > 0, chooseMessage(len(credentials) > 0, "已发现该仓且库存范围同步就绪", "请同步覆盖该仓的 API 凭据，并绑定有效 OMS 发货账号"))
	inventoryOK, accountOK := len(credentials) > 0, len(credentials) > 0
	seen := map[string]bool{}
	for _, credential := range credentials {
		client := xlwms.NewClient(credential.APIBaseURL, credential.AppKey, credential.AppSecret, s.requestTimeout)
		if _, queryErr := client.PageInventory(ctx, "integrated", map[string]any{"whCode": item.Code}, 1, 1); queryErr != nil {
			inventoryOK = false
		}
		if seen[credential.OMSAccountKey] {
			continue
		}
		seen[credential.OMSAccountKey] = true
		if s.platformAccounts == nil {
			accountOK = false
			continue
		}
		operator, opErr := s.platformAccounts.OperatorForAccount(ctx, credential.OMSAccountKey)
		if opErr != nil {
			accountOK = false
			continue
		}
		options, opErr := operator.WarehouseOptions(ctx)
		if opErr != nil {
			accountOK = false
			continue
		}
		found := false
		for _, o := range options {
			found = found || strings.EqualFold(o.WarehouseCode, item.Code)
		}
		accountOK = accountOK && found
	}
	add("inventory", "库存查询", inventoryOK, chooseMessage(inventoryOK, "库存接口可查询；零库存不阻止配置", "库存查询未通过，请检查凭据或恢复同步"))
	add("oms_permission", "OMS 发货权限", accountOK, chooseMessage(accountOK, "已有绑定账号具备该仓权限", "账号尚未授权该仓，或需要在账号管理完成验证"))
	options, optionErr := s.queryPlatformWarehouseOptions(ctx, in.Platform, in.ShopCode, in.OrderNo)
	found := false
	for _, o := range options {
		if o.ID == b.PlatformWarehouseID && o.CanShip {
			found = o.OMSCode == "" || o.OMSCode == item.Code
		}
	}
	add("mapping", "平台仓库映射", found && b.PlatformWarehouseID != "", chooseMessage(found, "已核实当前店铺的平台仓", chooseMessage(optionErr != nil, "需同步平台仓或选择有效的 SHEIN 验证订单", "请保存当前店铺实际可用的平台仓映射")))
	groups, err := s.store.CarrierPolicies(ctx, in.Platform, "")
	if err != nil {
		return c, err
	}
	carrierOK := false
	for _, g := range groups {
		if g.WarehouseKey != item.Key {
			continue
		}
		for _, p := range g.Carriers {
			if !p.Enabled || !fulfillment.CarrierAllowed(item.Key, p.CarrierCode) {
				continue
			}
			for _, allowed := range g.BaseRules.AllowedCarrierCodes {
				carrierOK = carrierOK || allowed == p.CarrierCode
			}
		}
	}
	add("carriers", "有效物流规则", carrierOK, chooseMessage(carrierOK, "有效物流集合非空，实际渠道以订单报价为准", "请在发货策略中启用至少一种允许物流"))
	return c, nil
}
func chooseMessage(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}
func (s *Server) warehouseReadiness(w http.ResponseWriter, r *http.Request) {
	s.performWarehouseCheck(w, r, false)
}
func (s *Server) activateWarehouseBinding(w http.ResponseWriter, r *http.Request) {
	s.performWarehouseCheck(w, r, true)
}
func (s *Server) performWarehouseCheck(w http.ResponseWriter, r *http.Request, activate bool) {
	item, ok := warehouseIdentity(w, r)
	if !ok {
		return
	}
	var in warehouseManagementRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	s.warehouseConfigMu.Lock()
	defer s.warehouseConfigMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
	defer cancel()
	c, err := s.checkWarehouseSetup(ctx, item, in)
	if err != nil {
		s.warehouseManagementError(w, err)
		return
	}
	if in.Revision != c.Revision || in.WarehouseRevision != c.WarehouseRevision {
		s.warehouseManagementError(w, store.ErrWarehouseConfigurationConflict)
		return
	}
	actor, _, _ := r.BasicAuth()
	if activate {
		if !c.Ready {
			writeJSON(w, 409, response{Success: false, Error: "配置检查未通过，请先处理缺项"})
			return
		}
		err = s.store.ActivateWarehouseBinding(ctx, c, actor)
	} else {
		err = s.store.SaveWarehouseVerification(ctx, c, actor)
	}
	if err != nil {
		s.warehouseManagementError(w, err)
		return
	}
	writeJSON(w, 200, response{Success: true, Data: c})
}
func (s *Server) updateFulfillmentWarehouse(w http.ResponseWriter, r *http.Request) {
	item, ok := warehouseIdentity(w, r)
	if !ok {
		return
	}
	var in warehouseManagementRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Enabled == nil {
		writeJSON(w, 400, response{Success: false, Error: "enabled 必填"})
		return
	}
	if *in.Enabled {
		s.performWarehouseActivationRequest(w, r, item, in)
		return
	}
	actor, _, _ := r.BasicAuth()
	s.warehouseConfigMu.Lock()
	defer s.warehouseConfigMu.Unlock()
	if err := s.store.PauseFulfillmentWarehouse(r.Context(), item.Key, in.WarehouseRevision, actor); err != nil {
		s.warehouseManagementError(w, err)
		return
	}
	writeJSON(w, 200, response{Success: true, Data: map[string]bool{"enabled": false}})
}
func (s *Server) performWarehouseActivationRequest(w http.ResponseWriter, r *http.Request, item fulfillment.Warehouse, in warehouseManagementRequest) {
	s.warehouseConfigMu.Lock()
	defer s.warehouseConfigMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
	defer cancel()
	c, err := s.checkWarehouseSetup(ctx, item, in)
	if err != nil {
		s.warehouseManagementError(w, err)
		return
	}
	if in.Revision != c.Revision || in.WarehouseRevision != c.WarehouseRevision {
		s.warehouseManagementError(w, store.ErrWarehouseConfigurationConflict)
		return
	}
	if !c.Ready {
		writeJSON(w, 409, response{Success: false, Error: "请先完成所选店铺的配置检查"})
		return
	}
	actor, _, _ := r.BasicAuth()
	if err = s.store.ActivateWarehouseBinding(ctx, c, actor); err != nil {
		s.warehouseManagementError(w, err)
		return
	}
	writeJSON(w, 200, response{Success: true, Data: c})
}
func (s *Server) warehouseConfigurationHistory(w http.ResponseWriter, r *http.Request) {
	item, ok := warehouseIdentity(w, r)
	if !ok {
		return
	}
	items, err := s.store.WarehouseConfigurationHistory(r.Context(), item.Key)
	if err != nil {
		s.internalError(w, "load warehouse configuration history", err)
		return
	}
	writeJSON(w, 200, response{Success: true, Data: items})
}

func (s *Server) warehouseActivity(w http.ResponseWriter, r *http.Request) {
	item, ok := warehouseIdentity(w, r)
	if !ok {
		return
	}
	source, ok := s.platformMappings.(interface {
		PlatformWarehouseActivity(context.Context, string, string) (map[string]int, error)
	})
	if !ok {
		writeJSON(w, 503, response{Success: false, Error: "购单状态服务不可用"})
		return
	}
	shops, err := s.store.ListFulfillmentShops(r.Context(), false)
	if err != nil {
		s.internalError(w, "load warehouse activity shops", err)
		return
	}
	count := 0
	for _, shop := range shops {
		counts, err := source.PlatformWarehouseActivity(r.Context(), shop.Platform, shop.ShopCode)
		if err != nil {
			writeJSON(w, 503, response{Success: false, Error: "处理中购单数量暂不可用"})
			return
		}
		count += counts[item.Key]
	}
	writeJSON(w, 200, response{Success: true, Data: fulfillment.WarehouseActivity{WarehouseKey: item.Key, InProgress: count}})
}
