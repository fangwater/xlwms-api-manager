package sheinfulfillment

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

const maxResponseBytes = 2 << 20

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type PurchasedLabel struct {
	ShopCode         string    `json:"shop_code"`
	ShopName         string    `json:"shop_name"`
	PlatformOrderNo  string    `json:"platform_order_no"`
	OMSWarehouseKey  string    `json:"oms_warehouse_key"`
	OMSWarehouseCode string    `json:"oms_warehouse_code"`
	TrackingNumber   string    `json:"tracking_number"`
	PurchasedAt      time.Time `json:"purchased_at"`
}

type shop struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type shopsEnvelope struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Shops []shop `json:"shops"`
	} `json:"data"`
}

type purchasedLabelsEnvelope struct {
	Success bool             `json:"success"`
	Data    []PurchasedLabel `json:"data"`
	Error   string           `json:"error"`
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *Client) PurchasedSheinLabelsByPlatformOrderNos(ctx context.Context, orderNos []string) (map[string]PurchasedLabel, error) {
	if c == nil || c.baseURL == "" {
		return nil, errors.New("SHEIN Go purchased-label service is not configured")
	}
	if len(orderNos) == 0 {
		return map[string]PurchasedLabel{}, nil
	}
	if len(orderNos) > 50 {
		return nil, errors.New("at most 50 SHEIN labels may be queried")
	}
	var shops shopsEnvelope
	if err := c.request(ctx, http.MethodGet, "/api/system/shops", "", nil, &shops); err != nil {
		return nil, fmt.Errorf("query SHEIN shops: %w", err)
	}
	if !shops.Success {
		return nil, serviceError("SHEIN shops", shops.Error)
	}
	if len(shops.Data.Shops) == 0 {
		return nil, errors.New("SHEIN Go returned no configured shops")
	}
	body, err := json.Marshal(map[string][]string{"platform_order_nos": orderNos})
	if err != nil {
		return nil, err
	}
	result := make(map[string]PurchasedLabel, len(orderNos))
	for _, currentShop := range shops.Data.Shops {
		var payload purchasedLabelsEnvelope
		if err := c.request(ctx, http.MethodPost, "/api/label-purchases/lookup", currentShop.Code, body, &payload); err != nil {
			return nil, fmt.Errorf("query SHEIN purchased labels for shop %s: %w", currentShop.Code, err)
		}
		if !payload.Success {
			return nil, serviceError("SHEIN purchased-label lookup", payload.Error)
		}
		for _, label := range payload.Data {
			orderNo := strings.ToUpper(strings.TrimSpace(label.PlatformOrderNo))
			if orderNo == "" {
				continue
			}
			if existing, found := result[orderNo]; found {
				return nil, fmt.Errorf("SHEIN order %s has purchased labels in shops %s and %s", orderNo, existing.ShopCode, currentShop.Code)
			}
			label.ShopCode = currentShop.Code
			label.ShopName = currentShop.Name
			result[orderNo] = label
		}
	}
	return result, nil
}

func (c *Client) request(ctx context.Context, method, path, shopCode string, body []byte, destination any) error {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if shopCode != "" {
		request.Header.Set("X-Shein-Shop", shopCode)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("SHEIN Go returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes+1))
	if err := decoder.Decode(destination); err != nil {
		return errors.New("SHEIN Go returned invalid JSON")
	}
	return nil
}

func serviceError(operation, message string) error {
	message = strings.TrimSpace(message)
	if len(message) > 200 {
		message = message[:200]
	}
	if message == "" {
		message = "request failed"
	}
	return fmt.Errorf("%s failed: %s", operation, message)
}
