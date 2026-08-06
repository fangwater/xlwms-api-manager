package temutracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 2 << 20

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type Event struct {
	LogisticsUpdatedAt string `json:"logisticsUpdatedAt"`
	LogisticsStatus    string `json:"logisticsStatus"`
	StatusText         string `json:"statusText"`
}

type Package struct {
	PackageSN    string  `json:"packageSn"`
	TrackingNum  string  `json:"trackingNum"`
	TrackingInfo []Event `json:"trackingInfo"`
}

type OrderTracking struct {
	StoreCode     string    `json:"store_code"`
	StoreName     string    `json:"store_name"`
	ParentOrderSN string    `json:"parent_order_sn"`
	Packages      []Package `json:"packages"`
}

type envelope struct {
	Success bool          `json:"success"`
	Data    OrderTracking `json:"data"`
	Error   string        `json:"error"`
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

func (c *Client) OrderTracking(ctx context.Context, shopCode, parentOrderSN string) (OrderTracking, error) {
	shopCode = strings.TrimSpace(shopCode)
	parentOrderSN = strings.TrimSpace(parentOrderSN)
	if shopCode == "" {
		return OrderTracking{}, errors.New("Temu shop code is required")
	}
	if parentOrderSN == "" {
		return OrderTracking{}, errors.New("Temu parent order number is required")
	}
	if c == nil || c.baseURL == "" {
		return OrderTracking{}, errors.New("Temu Go tracking service is not configured")
	}
	endpoint := c.baseURL + "/api/orders/" + url.PathEscape(parentOrderSN) + "/tracking?language=en"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return OrderTracking{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Temu-Shop", shopCode)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return OrderTracking{}, fmt.Errorf("query Temu tracking service: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return OrderTracking{}, fmt.Errorf("read Temu tracking response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return OrderTracking{}, errors.New("Temu tracking response is too large")
	}
	var payload envelope
	if err := json.Unmarshal(body, &payload); err != nil {
		return OrderTracking{}, errors.New("Temu tracking service returned invalid JSON")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !payload.Success {
		message := strings.TrimSpace(payload.Error)
		if len(message) > 200 {
			message = message[:200]
		}
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		return OrderTracking{}, fmt.Errorf("Temu tracking service failed (HTTP %d): %s", response.StatusCode, message)
	}
	return payload.Data, nil
}
