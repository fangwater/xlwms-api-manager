package store

import (
	"context"
	"xlwms-api-manager/internal/fulfillment"
	"xlwms-api-manager/internal/model"
)

func (p *Postgres) FulfillmentWarehouses(ctx context.Context) ([]fulfillment.Warehouse, error) {
	rows, err := p.pool.Query(ctx, `SELECT f.warehouse_key,f.wh_code,f.display_name,f.provider,f.region,f.inventory_priority,
 f.enabled AND coalesce(w.is_active,false),c.allowed_carrier_codes
 FROM xlwms_fulfillment_warehouses f LEFT JOIN xlwms_warehouses w USING(wh_code)
 LEFT JOIN xlwms_warehouse_carrier_capabilities c USING(wh_code) ORDER BY f.inventory_priority,f.warehouse_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]fulfillment.Warehouse, 0)
	for rows.Next() {
		var w fulfillment.Warehouse
		if err := rows.Scan(&w.Key, &w.Code, &w.Name, &w.Provider, &w.Region, &w.Priority, &w.Enabled, &w.AllowedCarrierCodes); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Apply physical capabilities after platform, SKU and account rules. An empty
// configured capability list intentionally disables every carrier.
func (p *Postgres) applyWarehouseCapabilities(ctx context.Context, groups []model.WarehouseCarrierPolicies) error {
	warehouses, err := p.FulfillmentWarehouses(ctx)
	if err != nil {
		return err
	}
	applyPhysicalWarehouseCapabilities(groups, warehouses)
	return nil
}

func applyPhysicalWarehouseCapabilities(groups []model.WarehouseCarrierPolicies, warehouses []fulfillment.Warehouse) {
	for i := range groups {
		for _, w := range warehouses {
			if groups[i].WarehouseKey != w.Key {
				continue
			}
			groups[i].DisplayName = w.Name
			enabled := w.Enabled
			groups[i].WarehouseEnabled = &enabled
			if w.AllowedCarrierCodes == nil {
				continue
			}
			groups[i].CapabilitySource = "physical_warehouse"
			allowed := map[string]bool{}
			for _, c := range w.AllowedCarrierCodes {
				allowed[c] = true
			}
			codes := make([]string, 0)
			for _, c := range groups[i].BaseRules.AllowedCarrierCodes {
				if allowed[c] && fulfillment.CarrierAllowed(w.Key, c) {
					codes = append(codes, c)
				}
			}
			groups[i].BaseRules.AllowedCarrierCodes = codes
			for j := range groups[i].Carriers {
				c := &groups[i].Carriers[j]
				c.Enabled = c.Enabled && allowed[c.CarrierCode] && fulfillment.CarrierAllowed(w.Key, c.CarrierCode)
			}
		}
	}
}
