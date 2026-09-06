package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"xlwms-api-manager/internal/model"
)

type AccountFulfillmentRules struct {
	Thresholds      model.InventoryThresholds `json:"thresholds"`
	SelectionMode   string                    `json:"selection_mode"`
	MaxPriceDelta   float64                   `json:"max_price_delta"`
	CarrierPriority []string                  `json:"carrier_priority"`
	BlockedCarriers map[string][]string       `json:"blocked_carriers"`
}

func (p *Postgres) requireSKUManagedRules(ctx context.Context, platform, sku string) error {
	rules, err := p.accountRulesForSKUs(ctx, platform, []string{sku})
	if err != nil {
		return err
	}
	if _, managed := rules[sku]; managed {
		return errors.New("该 SKU 使用 OMS 账户统一规则，不能通过 SKU 覆盖修改")
	}
	return nil
}

// Rules follow the existing API binding; this does not create a second SKU route.
func (p *Postgres) accountRulesForSKUs(ctx context.Context, platform string, skus []string) (map[string]AccountFulfillmentRules, error) {
	rows, err := p.pool.Query(ctx, `SELECT DISTINCT i.warehouse_sku,r.account_key,r.rules
 FROM xlwms_api_credential_inventory i
 JOIN xlwms_oms_account_api_credentials b USING(credential_key)
 JOIN xlwms_oms_account_fulfillment_rules r ON r.account_key=b.account_key AND r.platform=$1
 WHERE i.warehouse_sku=ANY($2)`, platform, skus)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]AccountFulfillmentRules{}
	for rows.Next() {
		var sku, account string
		var raw []byte
		var rules AccountFulfillmentRules
		if err := rows.Scan(&sku, &account, &raw); err != nil {
			return nil, err
		}
		if _, exists := result[sku]; exists {
			return nil, errors.New("SKU has conflicting account fulfillment rules")
		}
		if err := json.Unmarshal(raw, &rules); err != nil {
			return nil, err
		}
		result[sku] = rules
	}
	return result, rows.Err()
}

func applyAccountCarrierRules(groups []model.WarehouseCarrierPolicies, rules AccountFulfillmentRules) {
	for index := range groups {
		group := &groups[index]
		group.Customized = true
		group.Source = "oms_account"
		group.BaseRules.SelectionMode = rules.SelectionMode
		group.BaseRules.MaxPriceDelta = rules.MaxPriceDelta
		blocked := rules.BlockedCarriers[group.WarehouseKey]
		allowed := make([]string, 0, len(group.BaseRules.AllowedCarrierCodes))
		for _, code := range group.BaseRules.AllowedCarrierCodes {
			if !slices.Contains(blocked, code) {
				allowed = append(allowed, code)
			}
		}
		group.BaseRules.AllowedCarrierCodes = allowed
		for index := range group.Carriers {
			carrier := &group.Carriers[index]
			if priority := slices.Index(rules.CarrierPriority, carrier.CarrierCode); priority >= 0 {
				carrier.Priority = priority + 1
			}
			carrier.Enabled = carrier.Enabled && !slices.Contains(blocked, carrier.CarrierCode)
		}
		slices.SortFunc(group.Carriers, func(a, b model.CarrierPolicy) int { return a.Priority - b.Priority })
	}
}
