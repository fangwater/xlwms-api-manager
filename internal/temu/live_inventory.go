package temu

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/xlwms"
)

const liveInventoryStartTime = "2020-01-01 00:00:00"

type WarehouseQuery struct {
	WarehouseKey  string     `json:"warehouse_key"`
	WarehouseCode string     `json:"wh_code"`
	WarehouseName string     `json:"warehouse_name"`
	Region        string     `json:"region"`
	Status        string     `json:"status"`
	Pages         int        `json:"pages"`
	Records       int        `json:"records"`
	QueriedAt     *time.Time `json:"queried_at,omitempty"`
	Error         string     `json:"error,omitempty"`
}

type LiveInventoryResult struct {
	Complete         bool
	WindowStart      string
	WindowEnd        string
	WarehouseQueries []WarehouseQuery
	InventoryBySKU   map[string]map[string]WarehouseInventory
}

type queryOutcome struct {
	Code         string
	Query        WarehouseQuery
	Availability map[string]WarehouseInventory
}

func QueryLiveInventory(ctx context.Context, credentials []model.WarehouseCredentials, skus []string, requestTimeout time.Duration, now time.Time) LiveInventoryResult {
	result := LiveInventoryResult{
		Complete: true, WindowStart: liveInventoryStartTime, WindowEnd: now.Format("2006-01-02 15:04:05"),
		WarehouseQueries: make([]WarehouseQuery, 0, len(warehouseRules)),
		InventoryBySKU:   make(map[string]map[string]WarehouseInventory, len(skus)),
	}
	for _, sku := range skus {
		result.InventoryBySKU[sku] = make(map[string]WarehouseInventory, len(warehouseRules))
	}
	credentialsByCode := make(map[string]model.WarehouseCredentials, len(credentials))
	for _, warehouse := range credentials {
		credentialsByCode[strings.ToUpper(warehouse.Code)] = warehouse
	}
	outcomes := make(chan queryOutcome, len(warehouseRules))
	started := 0
	queriesByCode := make(map[string]WarehouseQuery, len(warehouseRules))
	for _, rule := range warehouseRules {
		warehouse, exists := credentialsByCode[rule.Code]
		if !exists {
			queriesByCode[rule.Code] = WarehouseQuery{
				WarehouseKey: rule.Key, WarehouseCode: rule.Code, WarehouseName: rule.FallbackName,
				Region: rule.Region, Status: QueryInactive, Error: "warehouse is not active or configured",
			}
			for _, sku := range skus {
				result.InventoryBySKU[sku][rule.Code] = WarehouseInventory{Name: rule.FallbackName, Active: false, QueryStatus: QueryInactive}
			}
			result.Complete = false
			continue
		}
		started++
		go func(rule WarehouseRule, warehouse model.WarehouseCredentials) {
			outcomes <- queryWarehouse(ctx, rule, warehouse, skus, requestTimeout, result.WindowStart, result.WindowEnd)
		}(rule, warehouse)
	}
	for index := 0; index < started; index++ {
		outcome := <-outcomes
		queriesByCode[outcome.Code] = outcome.Query
		if outcome.Query.Status != QuerySucceeded {
			result.Complete = false
		}
		for sku, inventory := range outcome.Availability {
			result.InventoryBySKU[sku][outcome.Code] = inventory
		}
	}
	for _, rule := range warehouseRules {
		result.WarehouseQueries = append(result.WarehouseQueries, queriesByCode[rule.Code])
	}
	return result
}

func ApplyInventoryCorrections(result *LiveInventoryResult, corrections map[string]map[string]model.InventoryCorrection) {
	if result == nil {
		return
	}
	for sku, byWarehouse := range corrections {
		inventoryByWarehouse, exists := result.InventoryBySKU[sku]
		if !exists {
			continue
		}
		for warehouseCode, correction := range byWarehouse {
			current, exists := inventoryByWarehouse[warehouseCode]
			if !exists || !current.Active || current.QueryStatus != QuerySucceeded {
				continue
			}
			updatedAt := correction.UpdatedAt
			current.RawAvailableAmount = current.AvailableAmount
			current.AvailableAmount = correctedInventoryAmount(current.RawAvailableAmount, correction)
			current.SKUFound = true
			current.Corrected = true
			current.CorrectionMode = correction.CorrectionMode
			current.CorrectionAmount = correction.CorrectionAmount
			current.CorrectionNote = correction.Note
			current.CorrectionUpdatedAt = &updatedAt
			inventoryByWarehouse[warehouseCode] = current
		}
	}
}

func correctedInventoryAmount(rawAmount float64, correction model.InventoryCorrection) float64 {
	if correction.CorrectionMode == "subtract" {
		result := rawAmount - correction.CorrectionAmount
		if result < 0 {
			return 0
		}
		return result
	}
	return correction.CorrectionAmount
}

func queryWarehouse(ctx context.Context, rule WarehouseRule, warehouse model.WarehouseCredentials, skus []string, requestTimeout time.Duration, windowStart, windowEnd string) queryOutcome {
	now := time.Now()
	query := WarehouseQuery{
		WarehouseKey: rule.Key, WarehouseCode: rule.Code, WarehouseName: warehouse.Name,
		Region: rule.Region, Status: QuerySucceeded, QueriedAt: &now,
	}
	if query.WarehouseName == "" {
		query.WarehouseName = rule.FallbackName
	}
	availability := make(map[string]WarehouseInventory, len(skus))
	for _, sku := range skus {
		availability[sku] = WarehouseInventory{Name: query.WarehouseName, Active: true, QueryStatus: QuerySucceeded, QueriedAt: &now}
	}
	client := xlwms.NewClient(warehouse.APIBaseURL, warehouse.AppKey, warehouse.AppSecret, requestTimeout)
	parameters := map[string]any{
		"skuList":    strings.Join(skus, ","),
		"whCodeList": warehouse.Code,
		"stockType":  0,
		"timeType":   "operateTime",
		"startTime":  windowStart,
		"endTime":    windowEnd,
	}
	records, pages, err := fetchIntegratedInventory(ctx, client, parameters)
	query.Pages = pages
	query.Records = len(records)
	if err != nil {
		query.Status = QueryFailed
		query.Error = err.Error()
		for _, sku := range skus {
			availability[sku] = WarehouseInventory{Name: query.WarehouseName, Active: true, QueryStatus: QueryFailed, QueriedAt: &now}
		}
		return queryOutcome{Code: rule.Code, Query: query, Availability: availability}
	}
	requested := make(map[string]struct{}, len(skus))
	for _, sku := range skus {
		requested[sku] = struct{}{}
	}
	for _, record := range records {
		sku := strings.TrimSpace(stringValue(record["sku"]))
		if _, exists := requested[sku]; !exists {
			continue
		}
		returnedWarehouse := strings.ToUpper(strings.TrimSpace(stringValue(record["whCode"])))
		if returnedWarehouse != "" && returnedWarehouse != rule.Code {
			query.Status = QueryFailed
			query.Error = "XLWMS returned inventory for an unexpected warehouse"
			for _, requestedSKU := range skus {
				availability[requestedSKU] = WarehouseInventory{Name: query.WarehouseName, Active: true, QueryStatus: QueryFailed, QueriedAt: &now}
			}
			return queryOutcome{Code: rule.Code, Query: query, Availability: availability}
		}
		current := availability[sku]
		current.SKUFound = true
		amount := nestedNumber(record, "productStockDtl", "availableAmount")
		current.AvailableAmount += amount
		current.RawAvailableAmount += amount
		availability[sku] = current
	}
	return queryOutcome{Code: rule.Code, Query: query, Availability: availability}
}

func fetchIntegratedInventory(ctx context.Context, client *xlwms.Client, parameters map[string]any) ([]map[string]any, int, error) {
	records := make([]map[string]any, 0)
	pages := 1
	for page := 1; page <= pages; page++ {
		response, err := client.PageInventory(ctx, "integrated", parameters, page, 100)
		if err != nil {
			return records, page, err
		}
		data, ok := response["data"].(map[string]any)
		if !ok {
			return records, page, errors.New("XLWMS inventory response is missing data")
		}
		batch, ok := data["records"].([]any)
		if !ok && data["records"] != nil {
			return records, page, errors.New("XLWMS inventory response has invalid records")
		}
		for _, raw := range batch {
			record, ok := raw.(map[string]any)
			if !ok {
				return records, page, errors.New("XLWMS inventory response contains an invalid record")
			}
			records = append(records, record)
		}
		if page == 1 {
			pages = numberAsInt(data["pages"])
			if pages < 1 {
				total := numberAsInt(data["total"])
				pages = (total + 99) / 100
				if pages < 1 {
					pages = 1
				}
			}
		}
	}
	return records, pages, nil
}

func nestedNumber(record map[string]any, object, field string) float64 {
	nested, ok := record[object].(map[string]any)
	if !ok {
		return 0
	}
	return numberAsFloat(nested[field])
}

func numberAsFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case string:
		number, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return number
	default:
		return 0
	}
}

func numberAsInt(value any) int { return int(numberAsFloat(value)) }
func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
