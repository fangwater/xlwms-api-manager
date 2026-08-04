package model

import (
	"encoding/json"
	"time"
)

type WarehouseSummary struct {
	Code       string    `json:"wh_code"`
	Name       string    `json:"name"`
	APIBaseURL string    `json:"api_base_url"`
	AppKeyHint string    `json:"app_key_hint"`
	Active     bool      `json:"active"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type WarehouseCredentials struct {
	WarehouseSummary
	AppKey    string `json:"-"`
	AppSecret string `json:"-"`
}

type FundsFlow struct {
	ID               int64           `json:"id"`
	WarehouseCode    string          `json:"wh_code"`
	OrderNo          string          `json:"order_no"`
	PlatformOrderNo  string          `json:"platform_order_no"`
	CostTotal        float64         `json:"cost_total"`
	CurrencyCode     string          `json:"currency_code"`
	CostStatus       *int            `json:"cost_status"`
	ModuleType       *int            `json:"module_type"`
	CostTime         *time.Time      `json:"cost_time"`
	BillStatus       *int            `json:"bill_status"`
	RelateBillNo     string          `json:"relate_bill_no"`
	DetailSyncStatus string          `json:"detail_sync_status"`
	DetailAttempts   int             `json:"detail_attempts"`
	DetailError      string          `json:"detail_error,omitempty"`
	RawPayload       json.RawMessage `json:"raw_payload,omitempty"`
}

type CostDetail struct {
	WarehouseCode   string     `json:"wh_code"`
	CostNo          string     `json:"cost_no"`
	QueryOrderNo    string     `json:"query_order_no"`
	CostTotal       float64    `json:"cost_total"`
	CurrencyCode    string     `json:"currency_code"`
	ModuleType      *int       `json:"module_type"`
	CostStatus      *int       `json:"cost_status"`
	BillStatus      *int       `json:"bill_status"`
	CreateTime      *time.Time `json:"create_time"`
	PlatformOrderNo string     `json:"platform_order_no"`
	ItemCount       int        `json:"item_count"`
}

type CostItem struct {
	Name       string     `json:"name"`
	Total      float64    `json:"total"`
	ChargeTime *time.Time `json:"charge_time"`
}

type SyncRun struct {
	ID            int64      `json:"id"`
	WarehouseCode string     `json:"wh_code"`
	Target        string     `json:"target"`
	Status        string     `json:"status"`
	Pages         int        `json:"pages"`
	RecordsSeen   int        `json:"records_seen"`
	RecordsSaved  int        `json:"records_saved"`
	Targets       int        `json:"targets"`
	Succeeded     int        `json:"succeeded"`
	Failed        int        `json:"failed"`
	CostItems     int        `json:"cost_items"`
	Error         string     `json:"error,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

type InventoryRecord struct {
	ID                     int64     `json:"id"`
	Kind                   string    `json:"kind"`
	WarehouseCode          string    `json:"wh_code"`
	WarehouseName          string    `json:"wh_name"`
	SKU                    string    `json:"sku"`
	FNSKU                  string    `json:"fnsku"`
	ProductName            string    `json:"product_name"`
	BoxType                string    `json:"box_type"`
	CustomizeBarcode       string    `json:"customize_barcode"`
	StockType              *int      `json:"stock_type"`
	ProductType            *int      `json:"product_type"`
	TotalAmount            float64   `json:"total_amount"`
	ProductTotalAmount     float64   `json:"product_total_amount"`
	BoxTotalAmount         float64   `json:"box_total_amount"`
	FBAReturnTotalAmount   float64   `json:"fba_return_total_amount"`
	AvailableAmount        float64   `json:"available_amount"`
	LockAmount             float64   `json:"lock_amount"`
	TransportAmount        float64   `json:"transport_amount"`
	ProductAvailableAmount float64   `json:"product_available_amount"`
	ProductLockAmount      float64   `json:"product_lock_amount"`
	ProductTransportAmount float64   `json:"product_transport_amount"`
	ChangeAmount           float64   `json:"change_amount"`
	StockAge               *int      `json:"stock_age"`
	StockAgeStatus         *int      `json:"stock_age_status"`
	StatisticDate          string    `json:"statistic_date"`
	ShelfDate              string    `json:"shelf_date"`
	OperateTime            string    `json:"operate_time"`
	RelateOrderType        *int      `json:"relate_order_type"`
	RelateOrderTypeName    string    `json:"relate_order_type_name"`
	RelateOrderNo          string    `json:"relate_order_no"`
	BatchNo                string    `json:"batch_no"`
	SegmentOneQuantity     float64   `json:"segment_one_quantity"`
	SegmentTwoQuantity     float64   `json:"segment_two_quantity"`
	SegmentThreeQuantity   float64   `json:"segment_three_quantity"`
	SegmentFourQuantity    float64   `json:"segment_four_quantity"`
	SegmentFiveQuantity    float64   `json:"segment_five_quantity"`
	LastSeenAt             time.Time `json:"last_seen_at"`
}

type SKUWarehouseStock struct {
	TotalAmount     float64 `json:"total_amount"`
	AvailableAmount float64 `json:"available_amount"`
	LockAmount      float64 `json:"lock_amount"`
	TransportAmount float64 `json:"transport_amount"`
}

type SKUStockLevel struct {
	SKU             string                       `json:"sku"`
	ProductName     string                       `json:"product_name"`
	StockType       *int                         `json:"stock_type"`
	ProductType     *int                         `json:"product_type"`
	TotalAmount     float64                      `json:"total_amount"`
	AvailableAmount float64                      `json:"available_amount"`
	LockAmount      float64                      `json:"lock_amount"`
	TransportAmount float64                      `json:"transport_amount"`
	WarehouseCount  int                          `json:"warehouse_count"`
	Warehouses      map[string]SKUWarehouseStock `json:"warehouses"`
	LastSeenAt      time.Time                    `json:"last_seen_at"`
}

type SKUStockSummary struct {
	SKUCount        int     `json:"sku_count"`
	RecordCount     int     `json:"record_count"`
	TotalAmount     float64 `json:"total_amount"`
	AvailableAmount float64 `json:"available_amount"`
	LockAmount      float64 `json:"lock_amount"`
	TransportAmount float64 `json:"transport_amount"`
}

type WarehouseSKUSpec struct {
	WarehouseSKU  string    `json:"warehouse_sku"`
	ProductName   string    `json:"product_name"`
	LengthCM      *float64  `json:"length_cm,omitempty"`
	WidthCM       *float64  `json:"width_cm,omitempty"`
	HeightCM      *float64  `json:"height_cm,omitempty"`
	WeightKG      *float64  `json:"weight_kg,omitempty"`
	Note          string    `json:"note"`
	Enabled       bool      `json:"enabled"`
	Source        string    `json:"source"`
	Complete      bool      `json:"complete"`
	MissingFields []string  `json:"missing_fields"`
	FirstSeenAt   time.Time `json:"first_seen_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type InventoryThresholds struct {
	EastThreshold  float64 `json:"east_threshold"`
	WestThreshold  float64 `json:"west_threshold"`
	TotalThreshold float64 `json:"total_threshold"`
}

type SKUInventoryThreshold struct {
	WarehouseSKU   string  `json:"warehouse_sku"`
	ProductName    string  `json:"product_name"`
	EastAvailable  float64 `json:"east_available"`
	WestAvailable  float64 `json:"west_available"`
	TotalAvailable float64 `json:"total_available"`
	InventoryThresholds
	Customized  bool       `json:"customized"`
	InventoryAt *time.Time `json:"inventory_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type WarehouseSKUQuantity struct {
	WarehouseSKU string `json:"warehouse_sku"`
	Quantity     int    `json:"quantity"`
}

type WarehouseSKUSpecResolutionItem struct {
	WarehouseSKU        string   `json:"warehouse_sku"`
	Quantity            int      `json:"quantity"`
	MatchedWarehouseSKU string   `json:"matched_warehouse_sku,omitempty"`
	MatchType           string   `json:"match_type,omitempty"`
	MatchCandidates     []string `json:"match_candidates,omitempty"`
	Matched             bool     `json:"matched"`
	Enabled             bool     `json:"enabled"`
	Complete            bool     `json:"complete"`
	LengthCM            *float64 `json:"length_cm,omitempty"`
	WidthCM             *float64 `json:"width_cm,omitempty"`
	HeightCM            *float64 `json:"height_cm,omitempty"`
	WeightKG            *float64 `json:"weight_kg,omitempty"`
	MissingFields       []string `json:"missing_fields"`
}

type WarehousePackageSpec struct {
	WarehouseSKU  string  `json:"warehouse_sku"`
	Weight        float64 `json:"weight"`
	WeightUnit    string  `json:"weight_unit"`
	Length        float64 `json:"length"`
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
	DimensionUnit string  `json:"dimension_unit"`
}

type WarehouseSKUSpecResolution struct {
	Complete    bool                             `json:"complete"`
	ErrorCode   string                           `json:"error_code,omitempty"`
	Error       string                           `json:"error,omitempty"`
	Items       []WarehouseSKUSpecResolutionItem `json:"items"`
	MissingSKUs []string                         `json:"missing_skus"`
	Package     *WarehousePackageSpec            `json:"package,omitempty"`
}
type InventorySummary struct {
	TotalAmount      float64            `json:"total_amount"`
	AvailableAmount  float64            `json:"available_amount"`
	LockAmount       float64            `json:"lock_amount"`
	TransportAmount  float64            `json:"transport_amount"`
	SKUCount         int                `json:"sku_count"`
	BoxTypeCount     int                `json:"box_type_count"`
	StaleAmount      float64            `json:"stale_amount"`
	StockByWarehouse map[string]float64 `json:"stock_by_warehouse"`
	AgeBuckets       map[string]float64 `json:"age_buckets"`
}
