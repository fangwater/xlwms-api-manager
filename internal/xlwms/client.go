package xlwms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	FundsFlowPath           = "/v1/cost/pageFundsFlow"
	CostDetailPath          = "/v1/cost/costDetail"
	IntegratedInventoryPath = "/v1/integratedInventory/pageOpen"
	StockAgePath            = "/v1/integratedInventory/pageStockAge"
	StockFlowPath           = "/v1/integratedInventory/pageStockFlow"
	BoxStockPath            = "/v1/boxStock/page"
	BoxStockAgePath         = "/v1/boxStock/pageStockAge"
	BoxSegmentStockAgePath  = "/v1/boxStock/pageSegmentStockAge"
	BoxStockFlowPath        = "/v1/boxStock/pageStockFlow"
)

var InventoryPaths = map[string]string{
	"integrated":      IntegratedInventoryPath,
	"stock_age":       StockAgePath,
	"stock_flow":      StockFlowPath,
	"box_stock":       BoxStockPath,
	"box_stock_age":   BoxStockAgePath,
	"box_segment_age": BoxSegmentStockAgePath,
	"box_stock_flow":  BoxStockFlowPath,
}

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("XLWMS API error code=%s msg=%s", e.Code, e.Message)
	}
	return e.Message
}

type Client struct {
	baseURL   string
	appKey    string
	appSecret string
	http      *http.Client
	now       func() time.Time
}

func NewClient(baseURL, appKey, appSecret string, timeout time.Duration) *Client {
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		appKey:    appKey,
		appSecret: appSecret,
		http:      &http.Client{Timeout: timeout},
		now:       time.Now,
	}
}

func CanonicalJSON(value any) ([]byte, error) {
	return json.Marshal(canonicalize(value))
}

func canonicalize(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			left, right := strings.ToLower(keys[i]), strings.ToLower(keys[j])
			if left == right {
				return keys[i] < keys[j]
			}
			return left < right
		})
		ordered := orderedObject{keys: keys, values: typed}
		return ordered
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = canonicalize(item)
		}
		return items
	default:
		return value
	}
}

type orderedObject struct {
	keys   []string
	values map[string]any
}

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, key := range o.keys {
		if index > 0 {
			buffer.WriteByte(',')
		}
		encodedKey, _ := json.Marshal(key)
		encodedValue, err := json.Marshal(canonicalize(o.values[key]))
		if err != nil {
			return nil, err
		}
		buffer.Write(encodedKey)
		buffer.WriteByte(':')
		buffer.Write(encodedValue)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func BuildAuthCode(appKey, appSecret, requestTime string, data any) (string, error) {
	serialized, err := CanonicalJSON(data)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	_, _ = mac.Write([]byte(appKey))
	_, _ = mac.Write(serialized)
	_, _ = mac.Write([]byte(requestTime))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (c *Client) Request(ctx context.Context, path string, data any) (map[string]any, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, errors.New("XLWMS API path must start with '/'")
	}
	requestTime := strconv.FormatInt(c.now().Unix(), 10)
	authCode, err := BuildAuthCode(c.appKey, c.appSecret, requestTime, data)
	if err != nil {
		return nil, fmt.Errorf("build XLWMS authcode: %w", err)
	}
	query := url.Values{"authcode": []string{authCode}}
	payload := map[string]any{"appKey": c.appKey, "reqTime": requestTime, "data": data}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode XLWMS request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path+"?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create XLWMS request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("XLWMS request failed: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 160<<20))
	if err != nil {
		return nil, fmt.Errorf("read XLWMS response: %w", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, &APIError{Status: response.StatusCode, Message: "XLWMS returned invalid JSON"}
	}
	code := fmt.Sprint(parsed["code"])
	if response.StatusCode < 200 || response.StatusCode >= 300 || code != "200" {
		return nil, &APIError{Status: response.StatusCode, Code: code, Message: fmt.Sprint(parsed["msg"])}
	}
	return parsed, nil
}

func (c *Client) PageFundsFlow(ctx context.Context, parameters map[string]any, page, pageSize int) (map[string]any, error) {
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, errors.New("page must be positive and pageSize must be between 1 and 100")
	}
	data := cloneMap(parameters)
	data["page"] = page
	data["pageSize"] = pageSize
	return c.Request(ctx, FundsFlowPath, data)
}

func (c *Client) CostDetail(ctx context.Context, orderNo string, orderType int, moduleType *int) (map[string]any, error) {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" {
		return nil, errors.New("queryOrderNo is required")
	}
	if orderType != 1 && orderType != 2 {
		return nil, errors.New("queryOrderType must be 1 or 2")
	}
	data := map[string]any{"queryOrderNo": orderNo, "queryOrderType": orderType}
	if moduleType != nil {
		if *moduleType < 1 {
			return nil, errors.New("moduleType must be positive")
		}
		data["moduleType"] = *moduleType
	}
	return c.Request(ctx, CostDetailPath, data)
}

func (c *Client) PageInventory(ctx context.Context, kind string, parameters map[string]any, page, pageSize int) (map[string]any, error) {
	path, ok := InventoryPaths[kind]
	if !ok {
		return nil, errors.New("unknown inventory kind")
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, errors.New("page must be positive and pageSize must be between 1 and 100")
	}
	data := cloneMap(parameters)
	data["page"] = page
	data["pageSize"] = pageSize
	if err := validateInventoryParameters(kind, data); err != nil {
		return nil, err
	}
	return c.Request(ctx, path, data)
}

func validateInventoryParameters(kind string, data map[string]any) error {
	switch kind {
	case "stock_age":
		value, ok := integerParameter(data["stockItemType"])
		if !ok || value != 0 && value != 2 {
			return errors.New("stockItemType is required and must be 0 or 2")
		}
	case "box_stock_age", "box_segment_age":
		if countProvided(data, "boxType", "customizeBarcode", "sku") > 1 {
			return errors.New("boxType, customizeBarcode and sku are mutually exclusive")
		}
	case "stock_flow":
		if hasParameter(data["startTime"]) != hasParameter(data["endTime"]) {
			return errors.New("startTime and endTime must be provided together")
		}
	case "box_stock_flow":
		if countProvided(data, "boxType", "customizeBarcode", "relateOrderNo", "batchNo") > 1 {
			return errors.New("boxType, customizeBarcode, relateOrderNo and batchNo are mutually exclusive")
		}
		if hasParameter(data["startTime"]) != hasParameter(data["endTime"]) {
			return errors.New("startTime and endTime must be provided together")
		}
	case "box_stock":
		for _, field := range []string{"boxTypeList", "skuList", "customizeBarcodeList", "otherCodeList"} {
			if length := parameterListLength(data[field]); length > 200 {
				return fmt.Errorf("%s cannot contain more than 200 items", field)
			}
		}
	}
	return nil
}

func integerParameter(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case float64:
		return int(typed), typed == float64(int(typed))
	default:
		return 0, false
	}
}

func hasParameter(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

func countProvided(data map[string]any, fields ...string) int {
	count := 0
	for _, field := range fields {
		if hasParameter(data[field]) {
			count++
		}
	}
	return count
}

func parameterListLength(value any) int {
	if items, ok := value.([]any); ok {
		return len(items)
	}
	if items, ok := value.([]string); ok {
		return len(items)
	}
	return 0
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+2)
	for key, value := range source {
		result[key] = value
	}
	return result
}
