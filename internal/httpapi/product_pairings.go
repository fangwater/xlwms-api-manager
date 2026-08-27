package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"xlwms-api-manager/internal/oms"
)

type productPairingOperator interface {
	ProductPairings(context.Context, oms.ProductPairingFilter) (oms.ProductPairingPage, error)
	CreateProductPairing(context.Context, oms.ProductPairingInput) error
	DeleteProductPairing(context.Context, string, string) error
}

type productPairingItemRequest struct {
	SystemSKU string `json:"system_sku"`
	Quantity  int    `json:"quantity"`
}

type productPairingRequest struct {
	Account     string                      `json:"account,omitempty"`
	StoreCode   string                      `json:"store_code"`
	PlatformSKU string                      `json:"platform_sku"`
	Items       []productPairingItemRequest `json:"items"`
}

type productPairingDeleteRequest struct {
	Account     string `json:"account,omitempty"`
	StoreCode   string `json:"store_code"`
	PlatformSKU string `json:"platform_sku"`
}

type productPairingValidationRequest struct {
	Account     string                      `json:"account,omitempty"`
	PlatformSKU string                      `json:"platform_sku"`
	Items       []productPairingItemRequest `json:"items"`
}

type productPairingValidationResult struct {
	Account               string `json:"account"`
	PlatformSKU           string `json:"platform_sku"`
	Ready                 bool   `json:"ready"`
	Reason                string `json:"reason,omitempty"`
	ExactPlatformRecords  int    `json:"exact_platform_records"`
	MatchingRecipeRecords int    `json:"matching_recipe_records"`
	ApprovedRecords       int    `json:"approved_records"`
}

type productPairingMutationResult struct {
	Account     string                   `json:"account"`
	StoreCode   string                   `json:"store_code"`
	PlatformSKU string                   `json:"platform_sku"`
	Items       []oms.ProductPairingItem `json:"items,omitempty"`
}

func (s *Server) listProductPairings(writer http.ResponseWriter, request *http.Request) {
	accountKey, err := requestedPlatformOrderAccount(request)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "OMS 账户参数冲突"})
		return
	}
	pageSize := queryInt(request, "page_size", 20)
	if pageSize > 100 {
		pageSize = 100
	}
	filter := oms.ProductPairingFilter{
		Page: queryInt(request, "page", 1), PageSize: pageSize,
		StoreCode:  strings.TrimSpace(request.URL.Query().Get("store_code")),
		Query:      strings.TrimSpace(request.URL.Query().Get("q")),
		QueryField: strings.TrimSpace(request.URL.Query().Get("query_field")),
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	operator, ok := s.selectedProductPairingOperator(ctx, writer, accountKey)
	if !ok {
		return
	}
	result, err := operator.ProductPairings(ctx, filter)
	if err != nil {
		if errors.Is(err, oms.ErrInvalidProductPairing) {
			writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "产品配对查询参数无效"})
			return
		}
		s.logger.Warn("query OMS product pairings", "account", accountKey, "error", err)
		writePlatformOrderSourceError(writer, err, "无法查询 OMS 产品配对")
		return
	}
	if result.Records == nil {
		result.Records = []oms.ProductPairing{}
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: struct {
		Account string `json:"account"`
		oms.ProductPairingPage
	}{Account: accountKey, ProductPairingPage: result}})
}

func (s *Server) createProductPairing(writer http.ResponseWriter, request *http.Request) {
	var payload productPairingRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	accountKey, err := requestedPlatformOrderAccountWithBody(request, payload.Account)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "OMS 账户参数冲突"})
		return
	}
	input := oms.ProductPairingInput{
		StoreCode: payload.StoreCode, PlatformSKU: payload.PlatformSKU,
		Items: make([]oms.ProductPairingItem, 0, len(payload.Items)),
	}
	for _, item := range payload.Items {
		input.Items = append(input.Items, oms.ProductPairingItem{SystemSKU: item.SystemSKU, Quantity: item.Quantity})
	}
	input, err = oms.NormalizeProductPairingInput(input)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "产品配对参数无效"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	operator, ok := s.selectedProductPairingOperator(ctx, writer, accountKey)
	if !ok {
		return
	}
	s.productPairingMu.Lock()
	defer s.productPairingMu.Unlock()
	if err := operator.CreateProductPairing(ctx, input); err != nil {
		s.logger.Warn("create OMS product pairing", "account", accountKey, "store_code", input.StoreCode, "error", err)
		writePlatformOrderSourceError(writer, err, "无法新建 OMS 产品配对")
		return
	}
	writeJSON(writer, http.StatusCreated, response{Success: true, Data: productPairingMutationResult{
		Account: accountKey, StoreCode: input.StoreCode, PlatformSKU: input.PlatformSKU, Items: input.Items,
	}})
}

func (s *Server) deleteProductPairing(writer http.ResponseWriter, request *http.Request) {
	var payload productPairingDeleteRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	accountKey, err := requestedPlatformOrderAccountWithBody(request, payload.Account)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "OMS 账户参数冲突"})
		return
	}
	storeCode, platformSKU, err := oms.NormalizeProductPairingKey(payload.StoreCode, payload.PlatformSKU)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "产品配对参数无效"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	operator, ok := s.selectedProductPairingOperator(ctx, writer, accountKey)
	if !ok {
		return
	}
	s.productPairingMu.Lock()
	defer s.productPairingMu.Unlock()
	if err := operator.DeleteProductPairing(ctx, storeCode, platformSKU); err != nil {
		s.logger.Warn("delete OMS product pairing", "account", accountKey, "store_code", storeCode, "error", err)
		writePlatformOrderSourceError(writer, err, "无法删除 OMS 产品配对")
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: productPairingMutationResult{
		Account: accountKey, StoreCode: storeCode, PlatformSKU: platformSKU,
	}})
}

func (s *Server) validateProductPairing(writer http.ResponseWriter, request *http.Request) {
	var payload productPairingValidationRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	accountKey, err := requestedPlatformOrderAccountWithBody(request, payload.Account)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "OMS 账户参数冲突"})
		return
	}
	items := make([]oms.ProductPairingItem, 0, len(payload.Items))
	for _, item := range payload.Items {
		items = append(items, oms.ProductPairingItem{SystemSKU: item.SystemSKU, Quantity: item.Quantity})
	}
	platformSKU, items, err := oms.NormalizeProductPairingRecipe(payload.PlatformSKU, items)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "产品配对校验参数无效"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	operator, ok := s.selectedProductPairingOperator(ctx, writer, accountKey)
	if !ok {
		return
	}
	records := make([]oms.ProductPairing, 0)
	for page := 1; ; page++ {
		result, queryErr := operator.ProductPairings(ctx, oms.ProductPairingFilter{
			Page: page, PageSize: 100, Query: platformSKU, QueryField: "platform_sku",
		})
		if queryErr != nil {
			s.logger.Warn("validate OMS product pairing", "account", accountKey, "error", queryErr)
			writePlatformOrderSourceError(writer, queryErr, "无法校验 OMS 产品配对")
			return
		}
		records = append(records, result.Records...)
		if result.Pages <= page || len(result.Records) == 0 {
			break
		}
	}
	match, err := oms.MatchProductPairingRecipe(platformSKU, items, records)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "产品配对校验参数无效"})
		return
	}
	result := productPairingValidationResult{
		Account: accountKey, PlatformSKU: platformSKU,
		ExactPlatformRecords: match.ExactPlatformRecords, MatchingRecipeRecords: match.MatchingRecipeRecords,
		ApprovedRecords: match.ApprovedRecords, Ready: match.ApprovedRecords > 0,
	}
	if !result.Ready {
		switch {
		case match.ExactPlatformRecords == 0:
			result.Reason = "所选仓库所属 OMS 账户没有这个平台 SKU 的产品配对"
		case match.MatchingRecipeRecords == 0:
			result.Reason = "OMS 产品配对与组合成员或数量不一致"
		default:
			result.Reason = "OMS 产品配对尚未全部审核通过"
		}
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: result})
}

func (s *Server) selectedProductPairingOperator(ctx context.Context, writer http.ResponseWriter, accountKey string) (productPairingOperator, bool) {
	account, err := s.selectedPlatformOrderAccount(ctx, accountKey)
	if err != nil {
		writePlatformOrderAccountError(writer, err)
		return nil, false
	}
	operator, ok := account.(productPairingOperator)
	if !ok {
		writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "所选 OMS 账户暂不支持产品配对"})
		return nil, false
	}
	return operator, true
}
