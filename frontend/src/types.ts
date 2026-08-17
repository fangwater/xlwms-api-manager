export type Warehouse = {
  wh_code: string;
  name: string;
  api_base_url: string;
  app_key_hint: string;
  oms_account_configured: boolean;
  oms_account_hint: string;
  active: boolean;
  updated_at: string;
};

export type PageData<T> = {
  records: T[];
  total: number;
  page: number;
  page_size: number;
  pages: number;
};

export type InventoryKind = "integrated" | "stock_age" | "stock_flow" | "box_stock" | "box_stock_age" | "box_segment_age" | "box_stock_flow";

export type InventoryRecord = {
  id: number;
  kind: InventoryKind;
  wh_code: string;
  wh_name: string;
  sku: string;
  fnsku: string;
  product_name: string;
  box_type: string;
  customize_barcode: string;
  stock_type?: number;
  product_type?: number;
  total_amount: number;
  product_total_amount: number;
  box_total_amount: number;
  fba_return_total_amount: number;
  available_amount: number;
  lock_amount: number;
  transport_amount: number;
  product_available_amount: number;
  product_lock_amount: number;
  product_transport_amount: number;
  change_amount: number;
  stock_age?: number;
  stock_age_status?: number;
  statistic_date: string;
  shelf_date: string;
  operate_time: string;
  relate_order_type?: number;
  relate_order_type_name: string;
  relate_order_no: string;
  batch_no: string;
  segment_one_quantity: number;
  segment_two_quantity: number;
  segment_three_quantity: number;
  segment_four_quantity: number;
  segment_five_quantity: number;
  last_seen_at: string;
};

export type SKUWarehouseStock = {
  total_amount: number;
  available_amount: number;
  lock_amount: number;
  transport_amount: number;
};

export type SKUStockLevel = {
  sku: string;
  product_name: string;
  stock_type?: number;
  product_type?: number;
  total_amount: number;
  available_amount: number;
  lock_amount: number;
  transport_amount: number;
  warehouse_count: number;
  warehouses: Record<string, SKUWarehouseStock>;
  last_seen_at: string;
};

export type SKUStockLevelPage = PageData<SKUStockLevel> & {
  summary: {
    sku_count: number;
    record_count: number;
    total_amount: number;
    available_amount: number;
    lock_amount: number;
    transport_amount: number;
  };
};

export type FundsFlow = {
  id: number;
  wh_code: string;
  order_no: string;
  platform_order_no: string;
  cost_total: number;
  currency_code: string;
  cost_status?: number;
  module_type?: number;
  cost_time?: string;
  bill_status?: number;
  relate_bill_no: string;
  detail_sync_status: "pending" | "success" | "error";
  detail_attempts: number;
  detail_error?: string;
};

export type CostDetail = {
  wh_code: string;
  cost_no: string;
  query_order_no: string;
  cost_total: number;
  currency_code: string;
  module_type?: number;
  cost_status?: number;
  bill_status?: number;
  create_time?: string;
  platform_order_no: string;
  item_count: number;
};

export type SyncRun = {
  id: number;
  wh_code: string;
  target: string;
  status: "running" | "succeeded" | "failed";
  pages: number;
  records_seen: number;
  records_saved: number;
  targets: number;
  succeeded: number;
  failed: number;
  cost_items: number;
  error?: string;
  started_at: string;
  finished_at?: string;
};

export type DashboardData = {
  operations: {
    active_warehouses: number;
    total_warehouses: number;
    funds_flows: number;
    cost_details: number;
    pending_details: number;
    failed_details: number;
    cost_by_currency: Record<string, number>;
    trend: { date: string; amount: number }[];
  };
  inventory: {
    total_amount: number;
    available_amount: number;
    lock_amount: number;
    transport_amount: number;
    sku_count: number;
    box_type_count: number;
    stale_amount: number;
    stock_by_warehouse: Record<string, number>;
    age_buckets: Record<string, number>;
  };
};

export type WarehouseSKUSpec = {
  warehouse_sku: string;
  product_name: string;
  length_cm?: number;
  width_cm?: number;
  height_cm?: number;
  weight_kg?: number;
  note: string;
  enabled: boolean;
  source: string;
  complete: boolean;
  missing_fields: string[];
  first_seen_at: string;
  updated_at: string;
};

export type PackingPlanRequest = {
  items: Array<{ warehouse_sku: string; quantity: number }>;
};

export type PackingDimensions = {
  length_cm: number;
  width_cm: number;
  height_cm: number;
};

export type PackingPosition = { x: number; y: number; z: number };

export type PackingPlacement = {
  step: number;
  unit_id: string;
  warehouse_sku: string;
  product_name?: string;
  position: PackingPosition;
  dimensions: PackingDimensions;
  original_dimensions: PackingDimensions;
  weight_kg: number;
};

export type PackingPackagePlan = {
  index: number;
  dimensions: PackingDimensions;
  placements: PackingPlacement[];
  packed_units: number;
  used_weight_kg: number;
  used_volume_cm3: number;
  volume_utilization_percent: number;
};

export type PackingUnfitItem = {
  unit_id: string;
  warehouse_sku: string;
  product_name?: string;
  dimensions: PackingDimensions;
  weight_kg: number;
  reason_code: string;
  reason: string;
};

export type PackingPlan = {
  algorithm: string;
  heuristic: boolean;
  packages: PackingPackagePlan[];
  unfit_items: PackingUnfitItem[];
  summary: {
    requested_units: number;
    packed_units: number;
    unfit_units: number;
    packages_used: number;
    total_weight_kg: number;
    packed_weight_kg: number;
    packed_volume_cm3: number;
  };
};

export type InventoryThresholds = {
  east_threshold: number;
  west_threshold: number;
  total_threshold: number;
};

export type FulfillmentShop = {
  platform: string;
  shop_code: string;
  shop_name: string;
  enabled: boolean;
};

export type ShopInventoryThresholds = FulfillmentShop & InventoryThresholds & {
  customized: boolean;
  updated_at: string;
};

export type SKUInventoryThreshold = InventoryThresholds & {
  warehouse_sku: string;
  product_name: string;
  east_available: number;
  west_available: number;
  total_available: number;
  customized: boolean;
  source?: string;
  inventory_at?: string;
  updated_at: string;
};

export type InventoryThresholdPage = PageData<SKUInventoryThreshold> & {
  default_thresholds: InventoryThresholds | ShopInventoryThresholds;
  shops: ShopInventoryThresholds[];
  platform?: string;
  shop_code?: string;
};

export type InventoryAlert = {
  wh_code: string;
  wh_name: string;
  warehouse_sku: string;
  product_name: string;
  total_amount: number;
  available_amount: number;
  lock_amount: number;
  transport_amount: number;
  threshold: number;
  customized: boolean;
  alert: boolean;
  inventory_at: string;
  config_updated_at: string;
};

export type InventoryAlertPage = PageData<InventoryAlert> & {
  default_threshold: number;
  summary: {
    alert_count: number;
    out_of_stock_count: number;
    warehouse_count: number;
    sku_count: number;
  };
};

export type WarehouseSKUInventoryAlertThreshold = {
  wh_code: string;
  warehouse_sku: string;
  threshold: number;
  updated_at: string;
};

export type FulfillmentAudit = {
  id: number;
  platform: string;
  shop_code: string;
  shop_name: string;
  platform_order_no: string;
  platform_status: string;
  platform_status_code?: number;
  platform_shipping_at?: string;
  warehouse_key: string;
  wh_code: string;
  tracking_number: string;
  oms_status: string;
  oms_status_code?: number;
  oms_status_since: string;
  oms_processing_since?: string;
  oms_order_created_at?: string;
  oms_outbound_at?: string;
  outbound_order_no: string;
  oms_tracking_number: string;
  exception_category: string;
  last_mile_tracking_number: string;
  tracking_status: string;
  tracking_status_text: string;
  tracking_updated_at?: string;
  tracking_checked_at?: string;
  tracking_error?: string;
  tracking_category: string;
  tracking_package_count: number;
  picked_up_package_count: number;
  pickup_exception_reason?: string;
  pickup_confirmed_at?: string;
  sync_error?: string;
  last_checked_at?: string;
  updated_at: string;
};

export type FulfillmentAuditPage = PageData<FulfillmentAudit> & {
  summary: {
    total: number;
    pending_query: number;
    manual_required: number;
    warehouse_overdue: number;
    sync_error: number;
    monitoring: number;
    last_query_at?: string;
  };
  shops: Array<{ code: string; name: string }>;
};

export type FulfilledOrderPage = PageData<FulfillmentAudit> & {
  last_query_at?: string;
  last_tracking_at?: string;
  summary: {
    total: number;
    awaiting_pickup: number;
    picked_up: number;
    pickup_exception: number;
    tracking_error: number;
    last_tracking_at?: string;
  };
  shops: Array<{ code: string; name: string }>;
};

export type PlatformOrderAccountOption = {
  key: string;
  label: string;
  warehouse_codes: string[];
  username_hint?: string;
  available?: boolean;
  status?: string;
  error?: string;
};

export type PlatformOrderProduct = {
  sku: string;
  qty: number;
  productName: string;
};

export type PlatformWarehouseDetail = {
  platformSku: string;
  warehouseId: string;
  warehouseName: string;
  qty: number;
};

export type PendingPlatformOrder = {
  orderNo: string;
  platformOrderNo: string;
  platformCode: string;
  platformSkuList: PlatformOrderProduct[];
  platformWarehouseDetails: PlatformWarehouseDetail[];
  skuList: PlatformOrderProduct[];
  storeCode: string;
  storeName: string;
  site: string;
  siteNameCn: string;
  siteNameEn: string;
  remark: string;
  sendWhCode: string;
  sendWhName: string;
  receiptCountryCode: string;
  receiptCountryName: string;
  trackNo: string;
  requestDeliveryTime: string;
  requestDeliveryTimeRecognizeStatus: number;
  requestDeliveryTimeFailReason: string;
  logisticsCarrier: string;
  logisticsCarrierName: string;
  logisticsChannelCode: string;
  logisticsChannelName: string;
  orderTime: string;
  payTime: string;
  source: string;
  createTime: string;
  auditTime: string;
  status: number;
  exceptionCause: string;
  auditCause: string;
  subStatus: number;
  markShipmentStatus: number;
  markShipmentTime: string;
  markShipmentFailReason: string;
  deliveryOptionType: number;
  platformOrderType: string;
  platformSplitRequired: string;
  platformSplitReason: string;
  splitStatus: number;
  printingStatus: number;
  directMailOrder: boolean;
  platformChannelCode: string;
  platformChannelName: string;
};

export type PendingPlatformOrderPage = PageData<PendingPlatformOrder> & {
  queried_at: string;
};

export type PlatformOrderAutomaticRoute = {
  platform_order_no: string;
  platform_warehouse_id: string;
  platform_warehouse_name: string;
  warehouse_code: string;
  warehouse_name: string;
};

export type PlatformOrderRoutingPreview = {
  ready: boolean;
  routes: PlatformOrderAutomaticRoute[];
  unresolved: Array<{ platform_order_no: string; reason: string }>;
  channel_code: string;
  channel_name: string;
  carriers: Array<{ value: string; label: string }>;
  queried_at: string;
};

export type PlatformOrderAssignmentResult = {
  account: string;
  total: number;
  success: number;
  failed: number;
  failures: Array<{ platform_order_no: string; error: string }>;
  routes: PlatformOrderAutomaticRoute[];
  warehouse_code: string;
  warehouse_codes: string[];
  channel_code: string;
  logistics_carrier: string;
  completed_at: string;
};
