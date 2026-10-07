package sheinfulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"xlwms-api-manager/internal/fulfillment"
)

func (c *Client) ShopWarehouseOptions(ctx context.Context, shop, order string, legacy bool) ([]fulfillment.PlatformWarehouseOption, error) {
	path := "/api/warehouse-management/options"
	method := http.MethodPost
	var body []byte
	if legacy {
		path = "/api/warehouse-management/legacy"
		method = http.MethodGet
	} else {
		body, _ = json.Marshal(map[string]string{"order_no": order})
	}
	var payload struct {
		Success bool                                  `json:"success"`
		Data    []fulfillment.PlatformWarehouseOption `json:"data"`
	}
	if err := c.request(ctx, method, path, shop, body, &payload); err != nil {
		return nil, err
	}
	if !payload.Success {
		return nil, errors.New("SHEIN 平台仓待验证")
	}
	return payload.Data, nil
}

func (c *Client) ShopWarehouseActivity(ctx context.Context, shop string) (map[string]int, error) {
	var payload struct {
		Success bool           `json:"success"`
		Data    map[string]int `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/warehouse-management/activity", shop, nil, &payload); err != nil {
		return nil, err
	}
	if !payload.Success {
		return nil, errors.New("无法查询购单处理中数量")
	}
	return payload.Data, nil
}
