export type Warehouse = {
  wh_code: string;
  name: string;
  api_base_url: string;
  app_key_hint: string;
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

export type InventoryThresholds = {
  east_threshold: number;
  west_threshold: number;
  total_threshold: number;
};

export type SKUInventoryThreshold = InventoryThresholds & {
  warehouse_sku: string;
  product_name: string;
  east_available: number;
  west_available: number;
  total_available: number;
  customized: boolean;
  inventory_at?: string;
  updated_at: string;
};

export type InventoryThresholdPage = PageData<SKUInventoryThreshold> & {
  default_thresholds: InventoryThresholds;
};
