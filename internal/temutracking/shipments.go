package temutracking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type PurchasedShipment struct {
	StoreCode        string     `json:"store_code"`
	StoreName        string     `json:"store_name"`
	ParentOrderSN    string     `json:"parent_order_sn"`
	Status           string     `json:"status"`
	OMSWarehouseKey  string     `json:"oms_warehouse_key"`
	OMSWarehouseCode string     `json:"oms_warehouse_code"`
	PackageSNList    []string   `json:"package_sn_list"`
	TrackingNumber   string     `json:"tracking_number"`
	ConfirmedAt      *time.Time `json:"confirmed_at,omitempty"`
}

type shipmentLookupEnvelope struct {
	Success bool                `json:"success"`
	Data    []PurchasedShipment `json:"data"`
	Error   string              `json:"error"`
}

func (c *Client) PurchasedShipmentsByPlatformOrderNos(ctx context.Context, parentOrderSNs []string) (map[string]PurchasedShipment, error) {
	if c == nil || c.baseURL == "" {
		return nil, errors.New("Temu Go shipment service is not configured")
	}
	if len(parentOrderSNs) == 0 {
		return map[string]PurchasedShipment{}, nil
	}
	if len(parentOrderSNs) > 50 {
		return nil, errors.New("at most 50 Temu shipments may be queried")
	}
	var shops shopsEnvelope
	if err := c.get(ctx, "/api/system/shops", "", &shops); err != nil {
		return nil, fmt.Errorf("query Temu shops: %w", err)
	}
	if !shops.Success {
		return nil, serviceError("Temu shops", shops.Error)
	}
	if len(shops.Data.Shops) == 0 {
		return nil, errors.New("Temu Go returned no configured shops")
	}

	requestBody, err := json.Marshal(map[string][]string{"parent_order_sns": parentOrderSNs})
	if err != nil {
		return nil, err
	}
	result := make(map[string]PurchasedShipment, len(parentOrderSNs))
	for _, shop := range shops.Data.Shops {
		var payload shipmentLookupEnvelope
		if err := c.post(ctx, "/api/shipments/lookup", shop.Code, requestBody, &payload); err != nil {
			return nil, fmt.Errorf("query Temu shipments for shop %s: %w", shop.Code, err)
		}
		if !payload.Success {
			return nil, serviceError("Temu shipment lookup", payload.Error)
		}
		for _, shipment := range payload.Data {
			orderNo := strings.ToUpper(strings.TrimSpace(shipment.ParentOrderSN))
			if orderNo == "" {
				continue
			}
			if existing, found := result[orderNo]; found {
				return nil, fmt.Errorf("Temu order %s has shipments in shops %s and %s", orderNo, existing.StoreCode, shop.Code)
			}
			shipment.StoreCode = shop.Code
			shipment.StoreName = shop.Name
			result[orderNo] = shipment
		}
	}
	return result, nil
}

func (c *Client) post(ctx context.Context, path, shopCode string, body []byte, destination any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if shopCode != "" {
		request.Header.Set("X-Temu-Shop", shopCode)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Temu Go returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes+1))
	if err := decoder.Decode(destination); err != nil {
		return errors.New("Temu Go returned invalid JSON")
	}
	return nil
}
