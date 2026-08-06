package temutracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Shop struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type WarehouseMapping struct {
	OMSKey           string `json:"oms_warehouse_key"`
	OMSWarehouseCode string `json:"oms_warehouse_code"`
	TemuWarehouseID  string `json:"temu_warehouse_id"`
	TemuName         string `json:"temu_warehouse_name"`
}

type shopsEnvelope struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Shops []Shop `json:"shops"`
	} `json:"data"`
}

type warehouseMappingsEnvelope struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Mappings []WarehouseMapping `json:"mappings"`
	} `json:"data"`
}

func (c *Client) WarehouseMappings(ctx context.Context) ([]WarehouseMapping, error) {
	if c == nil || c.baseURL == "" {
		return nil, errors.New("Temu Go warehouse mapping service is not configured")
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

	byPlatformWarehouseID := make(map[string]WarehouseMapping)
	for _, shop := range shops.Data.Shops {
		var payload warehouseMappingsEnvelope
		if err := c.get(ctx, "/api/warehouses", shop.Code, &payload); err != nil {
			return nil, fmt.Errorf("query Temu warehouse mappings for shop %s: %w", shop.Code, err)
		}
		if !payload.Success {
			return nil, serviceError("Temu warehouse mappings", payload.Error)
		}
		for _, mapping := range payload.Data.Mappings {
			mapping.TemuWarehouseID = strings.TrimSpace(mapping.TemuWarehouseID)
			mapping.OMSWarehouseCode = strings.TrimSpace(mapping.OMSWarehouseCode)
			if mapping.TemuWarehouseID == "" || mapping.OMSWarehouseCode == "" {
				continue
			}
			if existing, ok := byPlatformWarehouseID[mapping.TemuWarehouseID]; ok && !strings.EqualFold(existing.OMSWarehouseCode, mapping.OMSWarehouseCode) {
				return nil, fmt.Errorf("platform warehouse %s has conflicting OMS mappings", mapping.TemuWarehouseID)
			}
			byPlatformWarehouseID[mapping.TemuWarehouseID] = mapping
		}
	}

	result := make([]WarehouseMapping, 0, len(byPlatformWarehouseID))
	for _, mapping := range byPlatformWarehouseID {
		result = append(result, mapping)
	}
	return result, nil
}

func (c *Client) get(ctx context.Context, path, shopCode string, destination any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
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
