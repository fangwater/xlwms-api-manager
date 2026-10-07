package temutracking

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"xlwms-api-manager/internal/fulfillment"
)

func (c *Client) ShopWarehouseOptions(ctx context.Context, shop string, legacy bool) ([]fulfillment.PlatformWarehouseOption, error) {
	path := "/api/warehouses/sync"
	method := http.MethodPost
	if legacy {
		path = "/api/warehouses/legacy"
		method = http.MethodGet
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Temu-Shop", shop)
	res, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if legacy && res.StatusCode == 404 {
		var payload warehouseManagementEnvelope
		if err = c.get(ctx, "/api/warehouses", shop, &payload); err != nil {
			return nil, err
		}
		return optionsFromEnvelope(shop, payload, legacy)
	}
	var payload warehouseManagementEnvelope
	if err = json.NewDecoder(io.LimitReader(res.Body, maxResponseBytes)).Decode(&payload); err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, errors.New("Temu 仓库查询失败")
	}
	return optionsFromEnvelope(shop, payload, legacy)
}

type warehouseManagementEnvelope struct {
	Success bool `json:"success"`
	Data    struct {
		Warehouses []struct {
			ID         string   `json:"warehouse_id"`
			Name       string   `json:"warehouse_name"`
			CanShip    bool     `json:"enable_buy_shipping_label"`
			LogicalKey string   `json:"logical_warehouse_key"`
			ShopCodes  []string `json:"shop_codes"`
		} `json:"warehouses"`
		Mappings []struct {
			ID      string `json:"temu_warehouse_id"`
			OMSCode string `json:"oms_warehouse_code"`
			Enabled bool   `json:"enabled"`
		} `json:"mappings"`
	} `json:"data"`
}

func optionsFromEnvelope(shop string, p warehouseManagementEnvelope, legacy bool) ([]fulfillment.PlatformWarehouseOption, error) {
	if !p.Success {
		return nil, errors.New("Temu 仓库查询失败")
	}
	out := []fulfillment.PlatformWarehouseOption{}
	for _, w := range p.Data.Warehouses {
		own := false
		for _, s := range w.ShopCodes {
			own = own || s == shop
		}
		if !own {
			continue
		}
		o := fulfillment.PlatformWarehouseOption{ID: w.ID, Name: w.Name, CanShip: w.CanShip}
		if physical, ok := fulfillment.Find(w.LogicalKey); ok {
			o.OMSCode = physical.Code
		}
		if legacy {
			for _, m := range p.Data.Mappings {
				if m.ID == w.ID {
					o.OMSCode = m.OMSCode
					o.Enabled = m.Enabled
				}
			}
			if o.OMSCode == "" {
				continue
			}
		}
		out = append(out, o)
	}
	return out, nil
}

func (c *Client) ShopWarehouseActivity(ctx context.Context, shop string) (map[string]int, error) {
	var payload struct {
		Success bool           `json:"success"`
		Data    map[string]int `json:"data"`
	}
	if err := c.get(ctx, "/api/warehouse-management/activity", shop, &payload); err != nil {
		return nil, err
	}
	if !payload.Success {
		return nil, errors.New("无法查询购单处理中数量")
	}
	return payload.Data, nil
}
