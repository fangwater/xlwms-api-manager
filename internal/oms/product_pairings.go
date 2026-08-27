package oms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	productPairingListPath   = "/gateway/woms/platform/product/listMapping"
	productPairingCreatePath = "/gateway/woms/platform/product/addMapping"
	productPairingDeletePath = "/gateway/woms/platform/product/deleteMapping"
	maxProductPairingItems   = 20
)

var ErrInvalidProductPairing = errors.New("invalid OMS product pairing")

type ProductPairingItem struct {
	SystemSKU     string `json:"system_sku"`
	ProductName   string `json:"product_name,omitempty"`
	Quantity      int    `json:"quantity"`
	ApproveStatus int    `json:"approve_status"`
}

type ProductPairing struct {
	ID          string               `json:"id,omitempty"`
	PlatformSKU string               `json:"platform_sku"`
	StoreCode   string               `json:"store_code"`
	StoreName   string               `json:"store_name,omitempty"`
	Items       []ProductPairingItem `json:"items"`
	CreatedAt   string               `json:"created_at,omitempty"`
}

type ProductPairingPage struct {
	Records   []ProductPairing `json:"records"`
	Total     int              `json:"total"`
	Page      int              `json:"page"`
	PageSize  int              `json:"page_size"`
	Pages     int              `json:"pages"`
	QueriedAt time.Time        `json:"queried_at"`
}

type ProductPairingFilter struct {
	Page       int
	PageSize   int
	StoreCode  string
	Query      string
	QueryField string
}

type ProductPairingInput struct {
	StoreCode   string
	PlatformSKU string
	Items       []ProductPairingItem
}

type ProductPairingRecipeMatch struct {
	ExactPlatformRecords  int
	MatchingRecipeRecords int
	ApprovedRecords       int
}

type productPairingListPayload struct {
	Current    int    `json:"current"`
	Size       int    `json:"size"`
	StoreCodes string `json:"storeCodes,omitempty"`
	OrderType  string `json:"orderType,omitempty"`
	OrderNo    string `json:"orderNo,omitempty"`
}

type productPairingWireItem struct {
	SKU           string      `json:"sku"`
	ProductName   string      `json:"productName"`
	Quantity      FlexibleInt `json:"qty"`
	ApproveStatus FlexibleInt `json:"skuApproveStatus"`
}

type productPairingWireRecord struct {
	ID          FlexibleString           `json:"id"`
	PlatformSKU string                   `json:"platformSku"`
	StoreCode   string                   `json:"storeCode"`
	StoreName   string                   `json:"storeName"`
	SKUList     []productPairingWireItem `json:"skuList"`
	CreateTime  string                   `json:"createTime"`
}

type productPairingListData struct {
	Records []productPairingWireRecord `json:"records"`
	Total   int                        `json:"total"`
	Size    int                        `json:"size"`
	Current int                        `json:"current"`
	Pages   int                        `json:"pages"`
}

type productPairingCreatePayload struct {
	StoreCode   string                     `json:"storeCode"`
	PlatformSKU string                     `json:"platformSku"`
	SKUList     []productPairingCreateItem `json:"skuList"`
}

type productPairingCreateItem struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"qty"`
}

type productPairingDeletePayload struct {
	Options []productPairingDeleteOption `json:"optList"`
}

type productPairingDeleteOption struct {
	PlatformSKU string `json:"platformSku"`
	StoreCode   string `json:"storeCode"`
}

func (c *Client) ProductPairings(ctx context.Context, filter ProductPairingFilter) (ProductPairingPage, error) {
	filter.Page, filter.PageSize = normalizeProductPairingPage(filter.Page, filter.PageSize)
	queryField, err := productPairingQueryField(filter.QueryField)
	if err != nil {
		return ProductPairingPage{}, err
	}
	payload := productPairingListPayload{
		Current: filter.Page, Size: filter.PageSize,
		StoreCodes: strings.TrimSpace(filter.StoreCode),
		OrderType:  queryField, OrderNo: strings.TrimSpace(filter.Query),
	}
	return authenticatedRequest(c, ctx, func(token string) (ProductPairingPage, error) {
		envelope, status, requestErr := postJSON[productPairingListData](ctx, c, productPairingListPath, payload, c.productPairingHeaders(token))
		if requestErr != nil {
			return ProductPairingPage{}, fmt.Errorf("query OMS product pairings: %w", requestErr)
		}
		if status == http.StatusUnauthorized || envelope.Code == http.StatusUnauthorized {
			return ProductPairingPage{}, ErrAuthentication
		}
		if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
			return ProductPairingPage{}, remoteError("OMS product pairing query", status, envelope.Code, envelope.Msg)
		}
		return convertProductPairingPage(envelope.Data, filter.Page, filter.PageSize), nil
	})
}

func (c *Client) CreateProductPairing(ctx context.Context, input ProductPairingInput) error {
	normalized, err := NormalizeProductPairingInput(input)
	if err != nil {
		return err
	}
	payload := productPairingCreatePayload{
		StoreCode: normalized.StoreCode, PlatformSKU: normalized.PlatformSKU,
		SKUList: make([]productPairingCreateItem, 0, len(normalized.Items)),
	}
	for _, item := range normalized.Items {
		payload.SKUList = append(payload.SKUList, productPairingCreateItem{SKU: item.SystemSKU, Quantity: item.Quantity})
	}
	_, err = authenticatedRequest(c, ctx, func(token string) (struct{}, error) {
		envelope, status, requestErr := postJSON[any](ctx, c, productPairingCreatePath, payload, c.productPairingHeaders(token))
		if requestErr != nil {
			return struct{}{}, fmt.Errorf("create OMS product pairing: %w", requestErr)
		}
		if status == http.StatusUnauthorized || envelope.Code == http.StatusUnauthorized {
			return struct{}{}, ErrAuthentication
		}
		if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
			return struct{}{}, remoteError("OMS product pairing creation", status, envelope.Code, envelope.Msg)
		}
		return struct{}{}, nil
	})
	return err
}

func (c *Client) DeleteProductPairing(ctx context.Context, storeCode, platformSKU string) error {
	storeCode, platformSKU, err := NormalizeProductPairingKey(storeCode, platformSKU)
	if err != nil {
		return err
	}
	payload := productPairingDeletePayload{Options: []productPairingDeleteOption{{StoreCode: storeCode, PlatformSKU: platformSKU}}}
	_, err = authenticatedRequest(c, ctx, func(token string) (struct{}, error) {
		envelope, status, requestErr := postJSON[any](ctx, c, productPairingDeletePath, payload, c.productPairingHeaders(token))
		if requestErr != nil {
			return struct{}{}, fmt.Errorf("delete OMS product pairing: %w", requestErr)
		}
		if status == http.StatusUnauthorized || envelope.Code == http.StatusUnauthorized {
			return struct{}{}, ErrAuthentication
		}
		if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
			return struct{}{}, remoteError("OMS product pairing deletion", status, envelope.Code, envelope.Msg)
		}
		return struct{}{}, nil
	})
	return err
}

func NormalizeProductPairingInput(input ProductPairingInput) (ProductPairingInput, error) {
	var err error
	input.StoreCode, input.PlatformSKU, err = NormalizeProductPairingKey(input.StoreCode, input.PlatformSKU)
	if err != nil {
		return ProductPairingInput{}, err
	}
	input.PlatformSKU, input.Items, err = NormalizeProductPairingRecipe(input.PlatformSKU, input.Items)
	if err != nil {
		return ProductPairingInput{}, err
	}
	return input, nil
}

func NormalizeProductPairingRecipe(platformSKU string, items []ProductPairingItem) (string, []ProductPairingItem, error) {
	platformSKU = strings.TrimSpace(platformSKU)
	if err := validateProductPairingPlatformSKU(platformSKU); err != nil {
		return "", nil, err
	}
	if len(items) == 0 || len(items) > maxProductPairingItems {
		return "", nil, fmt.Errorf("%w: items must contain between 1 and %d entries", ErrInvalidProductPairing, maxProductPairingItems)
	}
	normalized := append([]ProductPairingItem(nil), items...)
	seen := make(map[string]struct{}, len(normalized))
	for index := range normalized {
		normalized[index].SystemSKU = strings.TrimSpace(normalized[index].SystemSKU)
		item := normalized[index]
		if item.SystemSKU == "" || len(item.SystemSKU) > 255 || containsControlCharacter(item.SystemSKU) {
			return "", nil, fmt.Errorf("%w: item %d has an invalid system SKU", ErrInvalidProductPairing, index+1)
		}
		key := item.SystemSKU
		if _, exists := seen[key]; exists {
			return "", nil, fmt.Errorf("%w: duplicate system SKU", ErrInvalidProductPairing)
		}
		seen[key] = struct{}{}
		if item.Quantity < 1 || item.Quantity > 999999 {
			return "", nil, fmt.Errorf("%w: item %d has an invalid quantity", ErrInvalidProductPairing, index+1)
		}
	}
	return platformSKU, normalized, nil
}

func MatchProductPairingRecipe(platformSKU string, items []ProductPairingItem, records []ProductPairing) (ProductPairingRecipeMatch, error) {
	platformSKU, items, err := NormalizeProductPairingRecipe(platformSKU, items)
	if err != nil {
		return ProductPairingRecipeMatch{}, err
	}
	expected := make(map[string]int, len(items))
	for _, item := range items {
		expected[item.SystemSKU] = item.Quantity
	}
	var result ProductPairingRecipeMatch
	for _, record := range records {
		if strings.TrimSpace(record.PlatformSKU) != platformSKU {
			continue
		}
		result.ExactPlatformRecords++
		actual := make(map[string]int, len(record.Items))
		approved := len(record.Items) > 0
		for _, item := range record.Items {
			actual[strings.TrimSpace(item.SystemSKU)] += item.Quantity
			if item.ApproveStatus != 2 {
				approved = false
			}
		}
		if !sameProductPairingRecipe(expected, actual) {
			continue
		}
		result.MatchingRecipeRecords++
		if approved {
			result.ApprovedRecords++
		}
	}
	return result, nil
}

func sameProductPairingRecipe(expected, actual map[string]int) bool {
	if len(expected) != len(actual) {
		return false
	}
	for sku, quantity := range expected {
		if actual[sku] != quantity {
			return false
		}
	}
	return true
}

func (c *Client) productPairingHeaders(token string) map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + token,
		"Referer":       c.baseURL + "/platform/product/list",
	}
}

func NormalizeProductPairingKey(storeCode, platformSKU string) (string, string, error) {
	storeCode = strings.TrimSpace(storeCode)
	platformSKU = strings.TrimSpace(platformSKU)
	if err := validateProductPairingKey(storeCode, platformSKU); err != nil {
		return "", "", err
	}
	return storeCode, platformSKU, nil
}

func validateProductPairingKey(storeCode, platformSKU string) error {
	if storeCode == "" || len(storeCode) > 255 || containsControlCharacter(storeCode) {
		return fmt.Errorf("%w: invalid store code", ErrInvalidProductPairing)
	}
	return validateProductPairingPlatformSKU(platformSKU)
}

func validateProductPairingPlatformSKU(platformSKU string) error {
	if platformSKU == "" || len(platformSKU) > 255 || containsControlCharacter(platformSKU) {
		return fmt.Errorf("%w: invalid platform SKU", ErrInvalidProductPairing)
	}
	return nil
}

func containsControlCharacter(value string) bool {
	return strings.ContainsAny(value, "\r\n\t")
}

func normalizeProductPairingPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func productPairingQueryField(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "platform_sku":
		return "platformSku", nil
	case "system_sku":
		return "sku", nil
	case "product_name":
		return "productName", nil
	default:
		return "", fmt.Errorf("%w: unsupported query field", ErrInvalidProductPairing)
	}
}

func convertProductPairingPage(data productPairingListData, page, pageSize int) ProductPairingPage {
	if data.Current > 0 {
		page = data.Current
	}
	if data.Size > 0 {
		pageSize = data.Size
	}
	pages := data.Pages
	if pages == 0 && data.Total > 0 {
		pages = (data.Total + pageSize - 1) / pageSize
	}
	records := make([]ProductPairing, 0, len(data.Records))
	for _, record := range data.Records {
		pairing := ProductPairing{
			ID: string(record.ID), PlatformSKU: record.PlatformSKU,
			StoreCode: record.StoreCode, StoreName: record.StoreName,
			Items: make([]ProductPairingItem, 0, len(record.SKUList)), CreatedAt: record.CreateTime,
		}
		for _, item := range record.SKUList {
			pairing.Items = append(pairing.Items, ProductPairingItem{
				SystemSKU: item.SKU, ProductName: item.ProductName,
				Quantity: int(item.Quantity), ApproveStatus: int(item.ApproveStatus),
			})
		}
		records = append(records, pairing)
	}
	return ProductPairingPage{
		Records: records, Total: data.Total, Page: page, PageSize: pageSize,
		Pages: pages, QueriedAt: time.Now().UTC(),
	}
}
