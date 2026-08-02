package xlwms

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	ParcelCreatePath        = "/v1/outboundOrder/create"
	ParcelPagePath          = "/v1/outboundOrder/pageList"
	ParcelDetailPath        = "/v1/outboundOrder/detail"
	ParcelCancelPath        = "/v1/outboundOrder/cancel"
	OutboundStatusPath      = "/v1/outboundOrder/selectBizStatus"
	BulkProductCreatePath   = "/v1/outboundOrder/big/create"
	BulkPagePath            = "/v1/outboundOrder/big/pageList"
	BulkDetailPath          = "/v1/outboundOrder/big/detail"
	BulkCancelPath          = "/v1/outboundOrder/big/cancel"
	TrackingLabelUpdatePath = "/v1/outboundOrder/updateTrackNoAndLabel"
	BulkBoxCreatePath       = "/v1/outboundOrder/big/bulkBox/create"
	MessageBoardDetailPath  = "/v1/outboundOrder/big/messageBoard/detail"
	MessageBoardReplyPath   = "/v1/outboundOrder/big/messageBoard/reply"
)

var OutboundPaths = map[string]string{
	"parcel-create":         ParcelCreatePath,
	"parcel-list":           ParcelPagePath,
	"parcel-detail":         ParcelDetailPath,
	"parcel-cancel":         ParcelCancelPath,
	"cancel-status":         OutboundStatusPath,
	"bulk-product-create":   BulkProductCreatePath,
	"bulk-list":             BulkPagePath,
	"bulk-detail":           BulkDetailPath,
	"bulk-cancel":           BulkCancelPath,
	"tracking-label-update": TrackingLabelUpdatePath,
	"bulk-box-create":       BulkBoxCreatePath,
	"message-detail":        MessageBoardDetailPath,
	"message-reply":         MessageBoardReplyPath,
}

func (c *Client) Outbound(ctx context.Context, operation string, data any) (map[string]any, error) {
	path, ok := OutboundPaths[operation]
	if !ok {
		return nil, errors.New("unknown outbound operation")
	}
	if err := ValidateOutboundData(operation, data); err != nil {
		return nil, err
	}
	return c.Request(ctx, path, data)
}

func ValidateOutboundData(operation string, data any) error {
	if _, ok := OutboundPaths[operation]; !ok {
		return errors.New("unknown outbound operation")
	}
	switch operation {
	case "parcel-create":
		if err := validateCreateOrders(data, 100, []string{"whCode", "thirdOrderNo", "subOrderType", "logisticsChannel", "receiver", "countryRegionCode", "provinceCode", "provinceName", "cityName", "postCode", "addressOne", "productList"}); err != nil {
			return err
		}
		return validateProductLines(data)
	case "bulk-product-create":
		if err := validateCreateOrders(data, 0, []string{"whCode", "thirdOrderNo", "logisticsChannel", "needRelabel", "receiver", "countryRegionCode", "cityName", "postCode", "addressOne", "productList", "fileList", "packageList"}); err != nil {
			return err
		}
		return validateProductLines(data)
	case "bulk-box-create":
		if err := validateCreateOrders(data, 100, []string{"whCode", "thirdOrderNo", "logisticsChannel", "receiver", "countryRegionCode", "cityName", "postCode", "addressOne", "boxList"}); err != nil {
			return err
		}
		return validateBoxCreateOrders(data)
	case "parcel-list", "bulk-list":
		return validateOutboundPage(data)
	case "parcel-detail":
		value, err := outboundObject(data)
		if err != nil {
			return err
		}
		if countProvided(value, "outboundOrderNoList", "referOrderNoList", "thirdOrderNoList") == 0 {
			return errors.New("one parcel order number filter is required")
		}
	case "parcel-cancel", "cancel-status", "bulk-detail", "bulk-cancel":
		value, err := outboundObject(data)
		if err != nil {
			return err
		}
		if err := requireStringList(value, "outboundOrderNoList"); err != nil {
			return err
		}
		if operation == "bulk-cancel" && hasParameter(value["orderThirdBindType"]) {
			kind, ok := integerParameter(value["orderThirdBindType"])
			if !ok || kind != 4 && kind != 5 {
				return errors.New("orderThirdBindType must be 4 or 5")
			}
		}
	case "tracking-label-update":
		value, err := outboundObject(data)
		if err != nil {
			return err
		}
		if !nonEmptyString(value["outboundOrderNo"]) {
			return errors.New("outboundOrderNo is required")
		}
		if countProvided(value, "trackingNumber", "labelUrl", "labelBase64") == 0 {
			return errors.New("trackingNumber, labelUrl or labelBase64 is required")
		}
	case "message-detail":
		value, err := outboundObject(data)
		if err != nil {
			return err
		}
		if !nonEmptyString(value["outboundOrderNo"]) {
			return errors.New("outboundOrderNo is required")
		}
	case "message-reply":
		return validateMessageReply(data)
	}
	return nil
}

func OutboundWarehouseCodes(data any) []string {
	items, ok := data.([]any)
	if !ok {
		return nil
	}
	codes := make([]string, 0, len(items))
	for _, item := range items {
		if order, ok := item.(map[string]any); ok {
			codes = append(codes, strings.ToUpper(strings.TrimSpace(fmt.Sprint(order["whCode"]))))
		}
	}
	return codes
}

func validateCreateOrders(data any, maximum int, required []string) error {
	items, ok := data.([]any)
	if !ok || len(items) == 0 {
		return errors.New("data must be a non-empty order array")
	}
	if maximum > 0 && len(items) > maximum {
		return fmt.Errorf("data cannot contain more than %d orders", maximum)
	}
	for index, item := range items {
		order, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("order %d must be an object", index+1)
		}
		for _, field := range required {
			if !hasParameter(order[field]) {
				return fmt.Errorf("order %d requires %s", index+1, field)
			}
			if strings.HasSuffix(field, "List") {
				values, ok := order[field].([]any)
				if !ok || len(values) == 0 {
					return fmt.Errorf("order %d requires a non-empty %s array", index+1, field)
				}
			}
		}
	}
	return nil
}

func validateProductLines(data any) error {
	for orderIndex, rawOrder := range data.([]any) {
		order := rawOrder.(map[string]any)
		products := order["productList"].([]any)
		for productIndex, rawProduct := range products {
			product, ok := rawProduct.(map[string]any)
			if !ok {
				return fmt.Errorf("order %d product %d must be an object", orderIndex+1, productIndex+1)
			}
			quantity, quantityOK := integerParameter(product["quantity"])
			if !nonEmptyString(product["sku"]) || !quantityOK || quantity < 1 {
				return fmt.Errorf("order %d product %d requires sku and a positive integer quantity", orderIndex+1, productIndex+1)
			}
		}
	}
	return nil
}

func validateBoxCreateOrders(data any) error {
	for orderIndex, rawOrder := range data.([]any) {
		order := rawOrder.(map[string]any)
		boxes := order["boxList"].([]any)
		seen := make(map[string]bool, len(boxes))
		for boxIndex, rawBox := range boxes {
			box, ok := rawBox.(map[string]any)
			if !ok {
				return fmt.Errorf("order %d box %d must be an object", orderIndex+1, boxIndex+1)
			}
			quantity, quantityOK := integerParameter(box["quantity"])
			if !nonEmptyString(box["boxTypeNo"]) || !quantityOK || quantity < 1 {
				return fmt.Errorf("order %d box %d requires boxTypeNo and a positive integer quantity", orderIndex+1, boxIndex+1)
			}
			boxType := strings.TrimSpace(fmt.Sprint(box["boxTypeNo"]))
			if seen[boxType] {
				return fmt.Errorf("order %d contains duplicate boxTypeNo %s", orderIndex+1, boxType)
			}
			seen[boxType] = true
		}
	}
	return nil
}
func validateOutboundPage(data any) error {
	value, err := outboundObject(data)
	if err != nil {
		return err
	}
	page, pageOK := integerParameter(value["page"])
	pageSize, pageSizeOK := integerParameter(value["pageSize"])
	if !pageOK || page < 1 || !pageSizeOK || pageSize < 1 || pageSize > 100 {
		return errors.New("page must be positive and pageSize must be between 1 and 100")
	}
	if hasParameter(value["startTime"]) != hasParameter(value["endTime"]) {
		return errors.New("startTime and endTime must be provided together")
	}
	if hasParameter(value["startTime"]) && strings.TrimSpace(fmt.Sprint(value["timeType"])) != "orderCreateTime" {
		return errors.New("timeType must be orderCreateTime when a time range is provided")
	}
	return nil
}

func validateMessageReply(data any) error {
	value, err := outboundObject(data)
	if err != nil {
		return err
	}
	if !nonEmptyString(value["outboundOrderNo"]) {
		return errors.New("outboundOrderNo is required")
	}
	attachments, hasAttachments := value["appendixList"].([]any)
	if !nonEmptyString(value["content"]) && (!hasAttachments || len(attachments) == 0) {
		return errors.New("content or appendixList is required")
	}
	if len(attachments) > 10 {
		return errors.New("appendixList cannot contain more than 10 files")
	}
	return nil
}

func outboundObject(data any) (map[string]any, error) {
	value, ok := data.(map[string]any)
	if !ok {
		return nil, errors.New("data must be an object")
	}
	return value, nil
}

func requireStringList(value map[string]any, field string) error {
	items, ok := value[field].([]any)
	if !ok || len(items) == 0 {
		return fmt.Errorf("%s must be a non-empty array", field)
	}
	for _, item := range items {
		if !nonEmptyString(item) {
			return fmt.Errorf("%s must contain only non-empty strings", field)
		}
	}
	return nil
}

func nonEmptyString(value any) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) != ""
}
