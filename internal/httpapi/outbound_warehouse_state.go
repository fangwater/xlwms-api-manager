package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"xlwms-api-manager/internal/fulfillment"
)

func (s *Server) validateOutboundWarehouseState(ctx context.Context, operation, code string, data any) error {
	if s.store == nil {
		return nil
	}
	if operation != "parcel-create" && operation != "bulk-product-create" && operation != "bulk-box-create" {
		return nil
	}
	physical, known := fulfillment.Find(code)
	if !known {
		return nil
	}
	all, err := s.store.FulfillmentWarehouses(ctx)
	if err != nil {
		return errors.New("无法读取发货仓库配置")
	}
	enabled := false
	for _, w := range all {
		if w.Key == physical.Key {
			enabled = w.Enabled
		}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var rows []map[string]any
	if err = json.Unmarshal(raw, &rows); err != nil {
		return err
	}
	bindings, err := s.store.WarehouseBindings(ctx, physical.Key, "", "")
	if err != nil {
		return errors.New("无法读取店铺仓库配置")
	}
	needEvidence := []string{}
	for _, row := range rows {
		platform, _ := row["salesPlatform"].(string)
		platform = strings.ToLower(strings.TrimSpace(platform))
		name, _ := row["storeName"].(string)
		allowed := enabled
		if platform == "temu" || platform == "shein" {
			allowed = false
			for _, b := range bindings {
				if b.Platform == platform && (strings.EqualFold(strings.TrimSpace(name), b.ShopName) || strings.EqualFold(strings.TrimSpace(name), b.ShopCode)) {
					allowed = b.Effective
				}
			}
		}
		if allowed {
			continue
		}
		if operation != "parcel-create" || platform != "shein" {
			return errors.New("仓库或当前店铺已暂停，不能新增出库")
		}
		order, _ := row["platformOrderNo"].(string)
		if order == "" {
			order, _ = row["thirdOrderNo"].(string)
		}
		if order == "" {
			return errors.New("暂停仓库仅允许处理有平台购单凭证的历史订单")
		}
		needEvidence = append(needEvidence, strings.ToUpper(strings.TrimSpace(order)))
	}
	if len(needEvidence) == 0 {
		return nil
	}
	if s.platformSheinLabels == nil {
		return errors.New("无法核实已购面单，请稍后重试")
	}
	for start := 0; start < len(needEvidence); start += 50 {
		end := min(start+50, len(needEvidence))
		labels, err := s.platformSheinLabels.PurchasedSheinLabelsByPlatformOrderNos(ctx, needEvidence[start:end])
		if err != nil {
			return errors.New("无法核实已购面单，请稍后重试")
		}
		for _, no := range needEvidence[start:end] {
			label, ok := labels[no]
			if !ok || !strings.EqualFold(label.OMSWarehouseCode, physical.Code) || !fulfillment.CarrierAllowed(physical.Key, label.CarrierCode) {
				return errors.New("已购面单凭证与该物理仓不符，不能新增出库")
			}
		}
	}
	return nil
}
