import type { CostDetail, DashboardData, FulfilledOrderPage, FulfillmentAuditPage, FundsFlow, InventoryAlertPage, InventoryKind, InventoryRecord, InventoryThresholdPage, InventoryThresholds, PageData, PendingPlatformOrderPage, PlatformOrderAssignmentResult, PlatformOrderRoutingPreview, SKUInventoryThreshold, SKUStockLevelPage, SyncRun, Warehouse, WarehouseSKUInventoryAlertThreshold, WarehouseSKUSpec } from "./types";

type Envelope<T> = { success: boolean; data?: T; error?: string };
const apiBase = `${import.meta.env.BASE_URL}api`;

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${apiBase}${path}`, {
    ...init,
    headers: { ...(init?.body ? { "Content-Type": "application/json" } : {}), ...init?.headers }
  });
  const payload = await response.json() as Envelope<T>;
  if (!response.ok || !payload.success || payload.data === undefined) throw new Error(payload.error || `请求失败 (${response.status})`);
  return payload.data;
}

function query(params: Record<string, string | number | undefined>): string {
  const values = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => { if (value !== undefined && value !== "") values.set(key, String(value)); });
  const rendered = values.toString();
  return rendered ? `?${rendered}` : "";
}

async function downloadFile(path: string): Promise<{ blob: Blob; filename: string }> {
  const response = await fetch(`${apiBase}${path}`);
  if (!response.ok) {
    let message = `导出失败 (${response.status})`;
    try {
      const payload = await response.json() as Envelope<never>;
      if (payload.error) message = payload.error;
    } catch { /* The server may return a non-JSON proxy error. */ }
    throw new Error(message);
  }
  const disposition = response.headers.get("Content-Disposition") || "";
  const filename = disposition.match(/filename="?([^";]+)"?/i)?.[1] || "manual-fulfillment-orders.csv";
  return { blob: await response.blob(), filename };
}

export const api = {
  health: async () => {
    const response = await fetch(`${import.meta.env.BASE_URL}healthz`);
    if (!response.ok) throw new Error("服务不可用");
  },
  dashboard: (warehouse?: string) => request<DashboardData>(`/dashboard/summary${query({ warehouse })}`),
  pendingPlatformOrders: (params: { q?: string; page: number; pageSize: number }) =>
    request<PendingPlatformOrderPage>("/platform-orders/pending" + query({ q: params.q, page: params.page, page_size: params.pageSize })),
  platformOrderRoutingPreview: (platformOrderNos: string[]) => request<PlatformOrderRoutingPreview>("/platform-orders/routing-preview", { method: "POST", body: JSON.stringify({ platform_order_nos: platformOrderNos }) }),
  assignAndApprovePlatformOrders: (payload: { platform_order_nos: string[]; logistics_carrier: string; confirmation: "CONFIRM_AND_APPROVE" }) =>
    request<PlatformOrderAssignmentResult>("/platform-orders/assign-and-approve", {
    method: "POST",
    body: JSON.stringify(payload)
  }),
  skuStockLevels: (params: { warehouse?: string; q?: string; stockType?: string; page: number; pageSize: number }) =>
    request<SKUStockLevelPage>(`/inventory/sku-levels${query({ warehouse: params.warehouse, q: params.q, stock_type: params.stockType, page: params.page, page_size: params.pageSize })}`),
  inventoryAlerts: (params: { warehouse?: string; q?: string; status: "alert" | "all"; page: number; pageSize: number }) =>
    request<InventoryAlertPage>(`/inventory-alerts${query({ warehouse: params.warehouse, q: params.q, status: params.status, page: params.page, page_size: params.pageSize })}`),
  updateInventoryAlertDefault: (threshold: number) => request<{ threshold: number }>("/inventory-alerts/default", { method: "PATCH", body: JSON.stringify({ threshold }) }),
  updateInventoryAlertConfig: (payload: { wh_code: string; warehouse_sku: string; threshold: number }) =>
    request<WarehouseSKUInventoryAlertThreshold>("/inventory-alerts/config", { method: "PATCH", body: JSON.stringify(payload) }),
  resetInventoryAlertConfig: (payload: { wh_code: string; warehouse_sku: string }) =>
    request<{ deleted: boolean }>("/inventory-alerts/config/reset", { method: "POST", body: JSON.stringify(payload) }),
  warehouseSKUSpecs: (params: { q?: string; status?: string; page: number; pageSize: number }) =>
    request<PageData<WarehouseSKUSpec>>(`/warehouse-sku-specs${query({ q: params.q, status: params.status, page: params.page, page_size: params.pageSize })}`),
  saveWarehouseSKUSpec: (payload: Record<string, unknown>) => request<WarehouseSKUSpec>("/warehouse-sku-specs", { method: "POST", body: JSON.stringify(payload) }),
  updateWarehouseSKUSpec: (warehouseSKU: string, payload: Record<string, unknown>) => request<WarehouseSKUSpec>(`/warehouse-sku-specs/${encodeURIComponent(warehouseSKU)}`, { method: "PATCH", body: JSON.stringify(payload) }),
  inventoryThresholds: (params: { q?: string; page: number; pageSize: number }) =>
    request<InventoryThresholdPage>(`/inventory-thresholds${query({ q: params.q, page: params.page, page_size: params.pageSize })}`),
  updateInventoryThresholdDefaults: (payload: InventoryThresholds) => request<InventoryThresholds>("/inventory-thresholds/defaults", { method: "PATCH", body: JSON.stringify(payload) }),
  updateSKUInventoryThreshold: (warehouseSKU: string, payload: InventoryThresholds) => request<SKUInventoryThreshold>(`/inventory-thresholds/${encodeURIComponent(warehouseSKU)}`, { method: "PATCH", body: JSON.stringify(payload) }),
  resetSKUInventoryThreshold: (warehouseSKU: string) => request<{ deleted: boolean }>(`/inventory-thresholds/${encodeURIComponent(warehouseSKU)}/reset`, { method: "POST" }),
  fulfillmentAudits: (params: { shop?: string; warehouse?: string; category?: string; omsStatus?: string; q?: string; page: number; pageSize: number }) =>
    request<FulfillmentAuditPage>(`/fulfillment-audits${query({ shop: params.shop, warehouse: params.warehouse, category: params.category, oms_status: params.omsStatus, q: params.q, page: params.page, page_size: params.pageSize })}`),
  fulfilledOrders: (params: { shop?: string; warehouse?: string; trackingCategory?: string; q?: string; page: number; pageSize: number }) =>
    request<FulfilledOrderPage>(`/fulfillment-audits/archived${query({ shop: params.shop, warehouse: params.warehouse, tracking_category: params.trackingCategory, q: params.q, page: params.page, page_size: params.pageSize })}`),
  outboundOrders: <T>(params: { warehouse?: string; q: string; page: number; pageSize: number }) =>
    request<T>(`/outbound-orders${query({ warehouse: params.warehouse, q: params.q, page: params.page, page_size: params.pageSize })}`),
  exportManualFulfillmentAudits: (params: { shop?: string; warehouse?: string; omsStatus?: string; q?: string; splitByWarehouse?: boolean }) =>
    downloadFile(`/fulfillment-audits/export-manual${query({ shop: params.shop, warehouse: params.warehouse, oms_status: params.omsStatus, q: params.q, split_by_warehouse: params.splitByWarehouse ? "true" : undefined })}`),
  warehouses: () => request<Warehouse[]>("/warehouses"),
  saveWarehouse: (payload: Record<string, unknown>) => request<Warehouse>("/warehouses", { method: "POST", body: JSON.stringify(payload) }),
  setWarehouseActive: (code: string, active: boolean) => request<Warehouse>(`/warehouses/${encodeURIComponent(code)}/status`, { method: "PATCH", body: JSON.stringify({ active }) }),
  setWarehouseOMSAccount: (code: string, payload: { username: string; password: string }) => request<Warehouse>(`/warehouses/${encodeURIComponent(code)}/oms-account`, { method: "PUT", body: JSON.stringify(payload) }),
  clearWarehouseOMSAccount: (code: string) => request<Warehouse>(`/warehouses/${encodeURIComponent(code)}/oms-account`, { method: "DELETE" }),
  inventory: (params: { kind: InventoryKind; warehouse?: string; q?: string; stockType?: string; page: number; pageSize: number }) =>
    request<PageData<InventoryRecord>>(`/inventory${query({ kind: params.kind, warehouse: params.warehouse, q: params.q, stock_type: params.stockType, page: params.page, page_size: params.pageSize })}`),
  fundsFlows: (params: { warehouse?: string; q?: string; detailStatus?: string; startDate?: string; endDate?: string; page: number; pageSize: number }) =>
    request<PageData<FundsFlow>>(`/funds-flows${query({ warehouse: params.warehouse, q: params.q, detail_status: params.detailStatus, start_date: params.startDate, end_date: params.endDate, page: params.page, page_size: params.pageSize })}`),
  costDetails: (params: { warehouse?: string; q?: string; startDate?: string; endDate?: string; page: number; pageSize: number }) =>
    request<PageData<CostDetail>>(`/cost-details${query({ warehouse: params.warehouse, q: params.q, start_date: params.startDate, end_date: params.endDate, page: params.page, page_size: params.pageSize })}`),
  syncRuns: (limit = 40) => request<SyncRun[]>(`/sync/runs?limit=${limit}`),
  syncInventory: (warehouse: string, kinds: InventoryKind[]) => request<SyncRun[]>("/sync/inventory", { method: "POST", body: JSON.stringify({ warehouse, kinds, page_size: 100 }) }),
  syncFundsFlow: (warehouse: string) => request<SyncRun[]>("/sync/funds-flow", { method: "POST", body: JSON.stringify({ warehouse, page_size: 100 }) }),
  syncCostDetails: (warehouse: string) => request<SyncRun>("/sync/cost-details", { method: "POST", body: JSON.stringify({ warehouse, workers: 4, requests_per_second: 8, max_attempts: 3 }) }),
  outbound: <T>(operation: string, warehouse: string, data: unknown) => request<T>(`/outbound/${encodeURIComponent(operation)}`, { method: "POST", body: JSON.stringify({ warehouse, data }) })
};
