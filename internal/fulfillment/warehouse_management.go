package fulfillment

import "time"

type PlatformWarehouseOption struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	CanShip bool   `json:"can_ship"`
	OMSCode string `json:"oms_code,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}
type WarehouseBinding struct {
	WarehouseKey          string     `json:"warehouse_key"`
	OMSCode               string     `json:"oms_code"`
	Platform              string     `json:"platform"`
	ShopCode              string     `json:"shop_code"`
	ShopName              string     `json:"shop_name"`
	ShopEnabled           bool       `json:"shop_enabled"`
	PlatformWarehouseID   string     `json:"platform_warehouse_id"`
	PlatformWarehouseName string     `json:"platform_warehouse_name"`
	Enabled               bool       `json:"enabled"`
	Effective             bool       `json:"effective"`
	Revision              int64      `json:"revision"`
	WarehouseRevision     int64      `json:"warehouse_revision"`
	VerifiedAt            *time.Time `json:"verified_at"`
	Verification          any        `json:"verification"`
	UpdatedAt             *time.Time `json:"updated_at"`
}
type WarehouseCheck struct {
	Code    string `json:"code"`
	Label   string `json:"label"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}
type WarehouseReadiness struct {
	WarehouseKey      string           `json:"warehouse_key"`
	Platform          string           `json:"platform"`
	ShopCode          string           `json:"shop_code"`
	Revision          int64            `json:"revision"`
	WarehouseRevision int64            `json:"warehouse_revision"`
	Ready             bool             `json:"ready"`
	Checks            []WarehouseCheck `json:"checks"`
	CheckedAt         time.Time        `json:"checked_at"`
}

type WarehouseActivity struct {
	WarehouseKey string `json:"warehouse_key"`
	InProgress   int    `json:"in_progress"`
}
