package oms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL = "https://oms.xlwms.com"
	userAgent      = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/149.0.7827.55 Safari/537.36"
	screenSize     = "1280x720"
	timezone       = "Asia/Shanghai"

	// This public request-signing key and algorithm come from the current OMS web client.
	trackKeySecret = "inUekb1OG+WeSjLAo+iZ8hwqiPq5a/vss0cwIxQ8aK4="

	platformOrderQueryConcurrency = 2
	platformOrderQueryMinInterval = 500 * time.Millisecond
)

var ErrAuthentication = errors.New("OMS authentication failed")

const (
	PlatformLabelChannelCode = "Upload_Shipping_Label"
	AutoMatchCarrierValue    = "_AUTO_MATCH_"
	OtherCarrierValue        = "other"
)

type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client

	tokenMu sync.Mutex
	token   string

	platformOrderGate *platformOrderQueryGate
}

type platformOrderQueryGate struct {
	slots    chan struct{}
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func newPlatformOrderQueryGate(concurrency int, interval time.Duration) *platformOrderQueryGate {
	return &platformOrderQueryGate{slots: make(chan struct{}, concurrency), interval: interval}
}

func (g *platformOrderQueryGate) acquire(ctx context.Context) (func(), error) {
	select {
	case g.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := func() { <-g.slots }

	g.mu.Lock()
	readyAt := time.Now()
	if g.next.After(readyAt) {
		readyAt = g.next
	}
	g.next = readyAt.Add(g.interval)
	g.mu.Unlock()

	delay := time.Until(readyAt)
	if delay <= 0 {
		return release, nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return release, nil
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
}

type OrderProduct struct {
	SKU         string `json:"sku"`
	Quantity    int    `json:"qty"`
	ProductName string `json:"productName"`
}

type PlatformWarehouseDetail struct {
	PlatformSKU   string `json:"platformSku"`
	WarehouseID   string `json:"warehouseId"`
	WarehouseName string `json:"warehouseName"`
	Quantity      int    `json:"qty"`
}

type PendingOrder struct {
	OrderNo                        string                    `json:"orderNo"`
	PlatformOrderNo                string                    `json:"platformOrderNo"`
	PlatformCode                   string                    `json:"platformCode"`
	PlatformSKUList                []OrderProduct            `json:"platformSkuList"`
	PlatformWarehouseDetails       []PlatformWarehouseDetail `json:"platformWarehouseDetails"`
	SKUList                        []OrderProduct            `json:"skuList"`
	StoreCode                      string                    `json:"storeCode"`
	StoreName                      string                    `json:"storeName"`
	Site                           string                    `json:"site"`
	SiteNameCN                     string                    `json:"siteNameCn"`
	SiteNameEN                     string                    `json:"siteNameEn"`
	Remark                         string                    `json:"remark"`
	SendWarehouseCode              string                    `json:"sendWhCode"`
	SendWarehouseName              string                    `json:"sendWhName"`
	ReceiptCountryCode             string                    `json:"receiptCountryCode"`
	ReceiptCountryName             string                    `json:"receiptCountryName"`
	TrackNo                        string                    `json:"trackNo"`
	RequestDeliveryTime            string                    `json:"requestDeliveryTime"`
	RequestDeliveryRecognizeStatus int                       `json:"requestDeliveryTimeRecognizeStatus"`
	RequestDeliveryTimeFailReason  string                    `json:"requestDeliveryTimeFailReason"`
	LogisticsCarrier               string                    `json:"logisticsCarrier"`
	LogisticsCarrierName           string                    `json:"logisticsCarrierName"`
	LogisticsChannelCode           string                    `json:"logisticsChannelCode"`
	LogisticsChannelName           string                    `json:"logisticsChannelName"`
	OrderTime                      string                    `json:"orderTime"`
	PayTime                        string                    `json:"payTime"`
	Source                         string                    `json:"source"`
	CreateTime                     string                    `json:"createTime"`
	AuditTime                      string                    `json:"auditTime"`
	Status                         int                       `json:"status"`
	ExceptionCause                 string                    `json:"exceptionCause"`
	AuditCause                     string                    `json:"auditCause"`
	SubStatus                      int                       `json:"subStatus"`
	MarkShipmentStatus             int                       `json:"markShipmentStatus"`
	MarkShipmentTime               string                    `json:"markShipmentTime"`
	MarkShipmentFailReason         string                    `json:"markShipmentFailReason"`
	DeliveryOptionType             int                       `json:"deliveryOptionType"`
	PlatformOrderType              FlexibleString            `json:"platformOrderType"`
	PlatformSplitRequired          string                    `json:"platformSplitRequired"`
	PlatformSplitReason            string                    `json:"platformSplitReason"`
	SplitStatus                    int                       `json:"splitStatus"`
	PrintingStatus                 int                       `json:"printingStatus"`
	DirectMailOrder                bool                      `json:"directMailOrder"`
	PlatformChannelCode            string                    `json:"platformChannelCode"`
	PlatformChannelName            string                    `json:"platformChannelName"`
}

type PendingOrderPage struct {
	Records   []PendingOrder `json:"records"`
	Total     int            `json:"total"`
	Page      int            `json:"page"`
	PageSize  int            `json:"page_size"`
	Pages     int            `json:"pages"`
	QueriedAt time.Time      `json:"queried_at"`
}

type WarehouseOption struct {
	WarehouseCode string `json:"whCode"`
	WarehouseName string `json:"whNameCn"`
}

type FlexibleInt int

func (value *FlexibleInt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" || raw == `""` {
		*value = -1
		return nil
	}
	raw = strings.Trim(raw, `"`)
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("decode flexible integer %q: %w", raw, err)
	}
	*value = FlexibleInt(parsed)
	return nil
}

type FlexibleString string

func (value *FlexibleString) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("decode flexible string: %w", err)
	}
	switch parsed := decoded.(type) {
	case nil:
		*value = ""
	case string:
		*value = FlexibleString(parsed)
	case json.Number:
		*value = FlexibleString(parsed.String())
	default:
		return fmt.Errorf("decode flexible string from %T", decoded)
	}
	return nil
}

type LogisticsChannelOption struct {
	LogisticsChannel     string      `json:"logisticsChannel"`
	LogisticsChannelName string      `json:"logisticsChannelName"`
	LogisticsCarrier     string      `json:"logisticsCarrier"`
	ChannelGroupFlag     int         `json:"channelGroupFlag"`
	ChannelStatus        int         `json:"channelStatus"`
	CarrierStatus        int         `json:"carrierStatus"`
	ChannelType          FlexibleInt `json:"channelType"`
	GetSheetType         FlexibleInt `json:"getSheetType"`
}

func (option LogisticsChannelOption) IsActivePlatformLabelUpload() bool {
	return option.LogisticsChannel == PlatformLabelChannelCode && option.ChannelStatus == 0 && option.CarrierStatus == 0 &&
		option.ChannelType == 3 && option.GetSheetType == 1
}

type AssignmentRequest struct {
	Orders                    []string
	WarehouseCode             string
	LogisticsChannelCode      string
	LogisticsChannelName      string
	LogisticsChannelGroupFlag int
	LogisticsCarrier          string
}

type AssignmentFailure struct {
	OrderNo  string `json:"orderNo"`
	ErrorMsg string `json:"errorMsg"`
}

type AssignmentResult struct {
	TotalQuantity   FlexibleInt         `json:"totalQuantity"`
	SuccessQuantity FlexibleInt         `json:"successQuantity"`
	FailQuantity    FlexibleInt         `json:"failQuantity"`
	FailList        []AssignmentFailure `json:"failList"`
}

type apiEnvelope[T any] struct {
	Code int    `json:"code"`
	Data T      `json:"data"`
	Msg  string `json:"msg"`
}

type loginPayload struct {
	BusinessType      string `json:"businessType"`
	DeviceFingerprint string `json:"deviceFingerprint"`
	DeviceInfo        string `json:"deviceInfo"`
	LoginAccount      string `json:"loginAccount"`
	LoginFlowID       string `json:"loginFlowId"`
	Password          string `json:"password"`
}

type loginData struct {
	Token                   string `json:"token"`
	NeedVerify              bool   `json:"needVerify"`
	NeedSetupMFA            bool   `json:"needSetupMfa"`
	NeedUpdatePassword      bool   `json:"needUpdatePassword"`
	NeedForceUpdatePassword bool   `json:"needForceUpdatePassword"`
}

type listPayload struct {
	CancelWay           string `json:"cancelWay"`
	CountKind           string `json:"countKind"`
	Current             int    `json:"current"`
	DeliveryOptionTypes string `json:"deliveryOptionTypes"`
	LogisticsChannels   string `json:"logisticsChannels"`
	MarkShipmentStatus  string `json:"markShipmentStatus"`
	PlatformCodes       string `json:"platformCodes"`
	PlatformOrderType   string `json:"platformOrderType"`
	PlatformOrderNo     string `json:"platformOrderNo,omitempty"`
	PlatformWarehouses  string `json:"platformWarehouses"`
	PrintingStatus      string `json:"printingStatus"`
	ReceiptCountries    string `json:"receiptCountries"`
	SendWarehouses      string `json:"sendWarehouses"`
	SiteCodes           string `json:"siteCodes"`
	Size                int    `json:"size"`
	Status              string `json:"status"`
	StoreCodes          string `json:"storeCodes"`
}

type listData struct {
	Records []PendingOrder `json:"records"`
	Total   int            `json:"total"`
	Size    int            `json:"size"`
	Current int            `json:"current"`
	Pages   int            `json:"pages"`
}

type assignmentPayload struct {
	Orders                    []string `json:"orders"`
	WarehouseCode             string   `json:"whCode"`
	LogisticsChannelCode      string   `json:"logisticsChannelCode"`
	LogisticsChannelName      string   `json:"logisticsChannelName"`
	LogisticsChannelGroupFlag int      `json:"channelGroupFlag"`
	LogisticsCarrier          string   `json:"logisticsCarrier"`
	HasApprove                int      `json:"hasApprove"`
}

type pendingOrderLookup struct {
	order PendingOrder
	found bool
}

func NewClient(baseURL, username, password string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL:           strings.TrimRight(baseURL, "/"),
		username:          strings.TrimSpace(username),
		password:          password,
		httpClient:        &http.Client{Timeout: timeout},
		platformOrderGate: newPlatformOrderQueryGate(platformOrderQueryConcurrency, platformOrderQueryMinInterval),
	}
}

func (c *Client) PendingOrders(ctx context.Context, page, pageSize int) (PendingOrderPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	token, err := c.accessToken(ctx)
	if err != nil {
		return PendingOrderPage{}, err
	}
	result, err := c.pendingOrders(ctx, token, page, pageSize)
	if !errors.Is(err, ErrAuthentication) {
		return result, err
	}
	c.invalidateToken(token)
	token, err = c.accessToken(ctx)
	if err != nil {
		return PendingOrderPage{}, err
	}
	return c.pendingOrders(ctx, token, page, pageSize)
}

// PlatformOrdersByPlatformOrderNo searches the OMS "all orders" view without
// applying a workflow-status filter. The platform order number remains an
// exact-match guard because the upstream search may return related records.
func (c *Client) PlatformOrdersByPlatformOrderNo(ctx context.Context, platformOrderNo string) ([]PendingOrder, error) {
	platformOrderNo = strings.TrimSpace(platformOrderNo)
	if platformOrderNo == "" {
		return nil, nil
	}
	release, err := c.platformOrderGate.acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("wait for OMS platform order query slot: %w", err)
	}
	defer release()
	return authenticatedRequest(c, ctx, func(token string) ([]PendingOrder, error) {
		payload := listPayload{
			CountKind: "orderCount", Current: 1, Size: 100,
			Status: "", PlatformOrderNo: platformOrderNo,
		}
		envelope, status, err := postJSON[listData](ctx, c, "/gateway/woms/platform/order/list", payload, map[string]string{"Authorization": "Bearer " + token})
		if err != nil {
			return nil, fmt.Errorf("query OMS platform order: %w", err)
		}
		if status == http.StatusUnauthorized || envelope.Code == http.StatusUnauthorized {
			return nil, ErrAuthentication
		}
		if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
			return nil, remoteError("OMS platform order lookup", status, envelope.Code, envelope.Msg)
		}
		matches := make([]PendingOrder, 0, len(envelope.Data.Records))
		for _, order := range envelope.Data.Records {
			if strings.EqualFold(strings.TrimSpace(order.PlatformOrderNo), platformOrderNo) {
				matches = append(matches, order)
			}
		}
		return matches, nil
	})
}

func (c *Client) WarehouseOptions(ctx context.Context) ([]WarehouseOption, error) {
	return authenticatedRequest(c, ctx, func(token string) ([]WarehouseOption, error) {
		query := url.Values{"status": {"0"}}
		envelope, status, err := getJSON[[]WarehouseOption](ctx, c, "/gateway/woms/warehouse/options", query, token)
		if err != nil {
			return nil, fmt.Errorf("query OMS warehouse options: %w", err)
		}
		if status == http.StatusUnauthorized || envelope.Code == http.StatusUnauthorized {
			return nil, ErrAuthentication
		}
		if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
			return nil, remoteError("OMS warehouse options", status, envelope.Code, envelope.Msg)
		}
		return envelope.Data, nil
	})
}

func (c *Client) LogisticsChannels(ctx context.Context, warehouseCode string) ([]LogisticsChannelOption, error) {
	warehouseCode = strings.TrimSpace(warehouseCode)
	if warehouseCode == "" {
		return nil, errors.New("warehouse code is required")
	}
	return authenticatedRequest(c, ctx, func(token string) ([]LogisticsChannelOption, error) {
		query := url.Values{
			"whCode":           {warehouseCode},
			"channelGroupFlag": {"1"},
			"lowPriceFlag":     {"1"},
		}
		envelope, status, err := getJSON[[]LogisticsChannelOption](ctx, c, "/gateway/woms/logistics/channel/options", query, token)
		if err != nil {
			return nil, fmt.Errorf("query OMS logistics channels: %w", err)
		}
		if status == http.StatusUnauthorized || envelope.Code == http.StatusUnauthorized {
			return nil, ErrAuthentication
		}
		if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
			return nil, remoteError("OMS logistics channels", status, envelope.Code, envelope.Msg)
		}
		return envelope.Data, nil
	})
}

func (c *Client) PendingOrdersByPlatformOrderNos(ctx context.Context, platformOrderNos []string) ([]PendingOrder, error) {
	type lookupResult struct {
		index  int
		lookup pendingOrderLookup
		err    error
	}

	results := make([]PendingOrder, len(platformOrderNos))
	jobs := make(chan int)
	completed := make(chan lookupResult, len(platformOrderNos))
	workers := 6
	if len(platformOrderNos) < workers {
		workers = len(platformOrderNos)
	}
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				lookup, err := c.pendingOrderByPlatformOrderNo(ctx, platformOrderNos[index])
				completed <- lookupResult{index: index, lookup: lookup, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range platformOrderNos {
			select {
			case jobs <- index:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		group.Wait()
		close(completed)
	}()

	foundCount := 0
	for result := range completed {
		if result.err != nil {
			return nil, result.err
		}
		if result.lookup.found {
			results[result.index] = result.lookup.order
			foundCount++
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if foundCount != len(platformOrderNos) {
		return nil, nil
	}
	return results, nil
}

func (c *Client) AssignAndApprove(ctx context.Context, input AssignmentRequest) (AssignmentResult, error) {
	return authenticatedRequest(c, ctx, func(token string) (AssignmentResult, error) {
		payload := assignmentPayload{
			Orders: input.Orders, WarehouseCode: input.WarehouseCode,
			LogisticsChannelCode: input.LogisticsChannelCode, LogisticsChannelName: input.LogisticsChannelName,
			LogisticsChannelGroupFlag: input.LogisticsChannelGroupFlag, LogisticsCarrier: input.LogisticsCarrier,
			HasApprove: 1,
		}
		envelope, status, err := postJSON[AssignmentResult](ctx, c, "/gateway/woms/platform/order/batchAllotWarehouse", payload, map[string]string{"Authorization": "Bearer " + token})
		if err != nil {
			return AssignmentResult{}, fmt.Errorf("assign and approve OMS platform orders: %w", err)
		}
		if status == http.StatusUnauthorized || envelope.Code == http.StatusUnauthorized {
			return AssignmentResult{}, ErrAuthentication
		}
		if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
			return AssignmentResult{}, remoteError("OMS assign and approve platform orders", status, envelope.Code, envelope.Msg)
		}
		result := envelope.Data
		if result.TotalQuantity == 0 && result.SuccessQuantity == 0 && result.FailQuantity == 0 && len(result.FailList) == 0 {
			result.TotalQuantity = FlexibleInt(len(input.Orders))
			result.SuccessQuantity = FlexibleInt(len(input.Orders))
		}
		return result, nil
	})
}

func (c *Client) pendingOrderByPlatformOrderNo(ctx context.Context, platformOrderNo string) (pendingOrderLookup, error) {
	platformOrderNo = strings.TrimSpace(platformOrderNo)
	if platformOrderNo == "" {
		return pendingOrderLookup{}, nil
	}
	return authenticatedRequest(c, ctx, func(token string) (pendingOrderLookup, error) {
		payload := listPayload{CountKind: "orderCount", Current: 1, Size: 100, Status: "0", PlatformOrderNo: platformOrderNo}
		envelope, status, err := postJSON[listData](ctx, c, "/gateway/woms/platform/order/list", payload, map[string]string{"Authorization": "Bearer " + token})
		if err != nil {
			return pendingOrderLookup{}, fmt.Errorf("query pending OMS platform order: %w", err)
		}
		if status == http.StatusUnauthorized || envelope.Code == http.StatusUnauthorized {
			return pendingOrderLookup{}, ErrAuthentication
		}
		if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
			return pendingOrderLookup{}, remoteError("OMS pending platform order lookup", status, envelope.Code, envelope.Msg)
		}
		for _, order := range envelope.Data.Records {
			if order.PlatformOrderNo == platformOrderNo && order.Status == 0 {
				return pendingOrderLookup{order: order, found: true}, nil
			}
		}
		return pendingOrderLookup{}, nil
	})
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.token != "" {
		return c.token, nil
	}
	token, err := c.login(ctx)
	if err != nil {
		return "", err
	}
	c.token = token
	return token, nil
}

func (c *Client) CheckAccess(ctx context.Context) error {
	_, err := c.accessToken(ctx)
	return err
}

func PublicAuthError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "账号已锁定"):
		return "账号已锁定，请联系超管修改或重置密码"
	case strings.Contains(message, "请更新登录密码"):
		return "请更新登录密码"
	case strings.Contains(message, "用户名或密码错误"):
		return "用户名或密码错误"
	case strings.Contains(message, "password update"), strings.Contains(message, "verification"):
		return "需要更新登录密码或完成验证"
	default:
		return "领星登录失败"
	}
}

func (c *Client) invalidateToken(token string) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.token == token {
		c.token = ""
	}
}

func (c *Client) login(ctx context.Context) (string, error) {
	if c.username == "" || c.password == "" {
		return "", errors.New("OMS username and password are not configured")
	}
	flowID, err := randomHex(16)
	if err != nil {
		return "", fmt.Errorf("generate OMS login flow ID: %w", err)
	}
	fingerprint := deviceFingerprint()
	payload := loginPayload{
		BusinessType: "oms", DeviceFingerprint: fingerprint, DeviceInfo: deviceInfo(),
		LoginAccount: c.username, LoginFlowID: flowID, Password: c.password,
	}
	headers := map[string]string{
		"X-Client-Type": "web", "X-Device-Fingerprint": fingerprint, "X-Login-Flow-Id": flowID,
	}
	envelope, status, err := postJSON[loginData](ctx, c, "/gateway/woms/auth/login", payload, headers)
	if err != nil {
		return "", fmt.Errorf("login to OMS: %w", err)
	}
	if status == http.StatusUnauthorized || envelope.Code == http.StatusUnauthorized {
		return "", ErrAuthentication
	}
	if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
		return "", remoteError("OMS login", status, envelope.Code, envelope.Msg)
	}
	if envelope.Data.NeedVerify || envelope.Data.NeedSetupMFA || envelope.Data.NeedUpdatePassword || envelope.Data.NeedForceUpdatePassword {
		return "", errors.New("OMS login requires interactive verification or a password update")
	}
	if envelope.Data.Token == "" {
		return "", errors.New("OMS login returned no access token")
	}
	return envelope.Data.Token, nil
}

func (c *Client) pendingOrders(ctx context.Context, token string, page, pageSize int) (PendingOrderPage, error) {
	payload := listPayload{CountKind: "orderCount", Current: page, Size: pageSize, Status: "0"}
	headers := map[string]string{"Authorization": "Bearer " + token}
	envelope, status, err := postJSON[listData](ctx, c, "/gateway/woms/platform/order/list", payload, headers)
	if err != nil {
		return PendingOrderPage{}, fmt.Errorf("query OMS pending platform orders: %w", err)
	}
	if status == http.StatusUnauthorized || envelope.Code == http.StatusUnauthorized {
		return PendingOrderPage{}, ErrAuthentication
	}
	if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
		return PendingOrderPage{}, remoteError("OMS pending platform orders", status, envelope.Code, envelope.Msg)
	}
	return PendingOrderPage{
		Records: envelope.Data.Records, Total: envelope.Data.Total, Page: envelope.Data.Current,
		PageSize: envelope.Data.Size, Pages: envelope.Data.Pages, QueriedAt: time.Now().UTC(),
	}, nil
}

func authenticatedRequest[T any](client *Client, ctx context.Context, operation func(string) (T, error)) (T, error) {
	var zero T
	token, err := client.accessToken(ctx)
	if err != nil {
		return zero, err
	}
	result, err := operation(token)
	if !errors.Is(err, ErrAuthentication) {
		return result, err
	}
	client.invalidateToken(token)
	token, err = client.accessToken(ctx)
	if err != nil {
		return zero, err
	}
	return operation(token)
}

func getJSON[T any](ctx context.Context, client *Client, path string, query url.Values, token string) (apiEnvelope[T], int, error) {
	var envelope apiEnvelope[T]
	endpoint := client.baseURL + path
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return envelope, 0, err
	}
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Origin", client.baseURL)
	request.Header.Set("Referer", client.baseURL+"/platform/order/list")
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Track-Key", buildTrackKey(http.MethodGet, nil))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return envelope, 0, err
	}
	defer response.Body.Close()
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&envelope); err != nil {
		return envelope, response.StatusCode, fmt.Errorf("decode OMS response: %w", err)
	}
	return envelope, response.StatusCode, nil
}

func postJSON[T any](ctx context.Context, client *Client, path string, payload any, headers map[string]string) (apiEnvelope[T], int, error) {
	var envelope apiEnvelope[T]
	body, err := json.Marshal(payload)
	if err != nil {
		return envelope, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return envelope, 0, err
	}
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Content-Type", "application/json;charset=UTF-8")
	request.Header.Set("Origin", client.baseURL)
	referer := client.baseURL + "/platform/order/list"
	if strings.HasSuffix(path, "/auth/login") {
		referer = client.baseURL + "/login"
	}
	request.Header.Set("Referer", referer)
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Track-Key", buildTrackKey(http.MethodPost, body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return envelope, 0, err
	}
	defer response.Body.Close()
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&envelope); err != nil {
		return envelope, response.StatusCode, fmt.Errorf("decode OMS response: %w", err)
	}
	return envelope, response.StatusCode, nil
}

func buildTrackKey(method string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	message := strings.ToUpper(method) + hex.EncodeToString(bodyHash[:])
	mac := hmac.New(sha256.New, []byte(trackKeySecret))
	_, _ = mac.Write([]byte(message))
	return "v2:" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func deviceFingerprint() string {
	sum := sha256.Sum256([]byte(userAgent + "|" + screenSize + "|" + timezone))
	return hex.EncodeToString(sum[:])[:8]
}

func deviceInfo() string {
	return strings.Join([]string{userAgent, screenSize, timezone}, " | ")
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func remoteError(operation string, status, code int, message string) error {
	message = strings.TrimSpace(message)
	if len(message) > 200 {
		message = message[:200]
	}
	if message == "" {
		return fmt.Errorf("%s failed (HTTP %d, code %d)", operation, status, code)
	}
	return fmt.Errorf("%s failed (HTTP %d, code %d): %s", operation, status, code, message)
}
