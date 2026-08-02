import type { CostDetail, DashboardData, FundsFlow, InventoryKind, InventoryRecord, PageData, SKUStockLevelPage, SyncRun, Warehouse } from "./types";

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

export const api = {
  health: async () => {
    const response = await fetch(`${import.meta.env.BASE_URL}healthz`);
    if (!response.ok) throw new Error("服务不可用");
  },
  dashboard: (warehouse?: string) => request<DashboardData>(`/dashboard/summary${query({ warehouse })}`),
  skuStockLevels: (params: { warehouse?: string; q?: string; stockType?: string; page: number; pageSize: number }) =>
    request<SKUStockLevelPage>(`/inventory/sku-levels${query({ warehouse: params.warehouse, q: params.q, stock_type: params.stockType, page: params.page, page_size: params.pageSize })}`),
  warehouses: () => request<Warehouse[]>("/warehouses"),
  saveWarehouse: (payload: Record<string, unknown>) => request<Warehouse>("/warehouses", { method: "POST", body: JSON.stringify(payload) }),
  setWarehouseActive: (code: string, active: boolean) => request<Warehouse>(`/warehouses/${encodeURIComponent(code)}/status`, { method: "PATCH", body: JSON.stringify({ active }) }),
  inventory: (params: { kind: InventoryKind; warehouse?: string; q?: string; stockType?: string; page: number; pageSize: number }) =>
    request<PageData<InventoryRecord>>(`/inventory${query({ kind: params.kind, warehouse: params.warehouse, q: params.q, stock_type: params.stockType, page: params.page, page_size: params.pageSize })}`),
  fundsFlows: (params: { warehouse?: string; q?: string; detailStatus?: string; page: number; pageSize: number }) =>
    request<PageData<FundsFlow>>(`/funds-flows${query({ warehouse: params.warehouse, q: params.q, detail_status: params.detailStatus, page: params.page, page_size: params.pageSize })}`),
  costDetails: (params: { warehouse?: string; q?: string; page: number; pageSize: number }) =>
    request<PageData<CostDetail>>(`/cost-details${query({ warehouse: params.warehouse, q: params.q, page: params.page, page_size: params.pageSize })}`),
  syncRuns: (limit = 40) => request<SyncRun[]>(`/sync/runs?limit=${limit}`),
  syncInventory: (warehouse: string, kinds: InventoryKind[]) => request<SyncRun[]>("/sync/inventory", { method: "POST", body: JSON.stringify({ warehouse, kinds, page_size: 100 }) }),
  syncFundsFlow: (warehouse: string) => request<SyncRun[]>("/sync/funds-flow", { method: "POST", body: JSON.stringify({ warehouse, page_size: 100 }) }),
  syncCostDetails: (warehouse: string) => request<SyncRun>("/sync/cost-details", { method: "POST", body: JSON.stringify({ warehouse, workers: 4, requests_per_second: 8, max_attempts: 3 }) }),
  outbound: <T>(operation: string, warehouse: string, data: unknown) => request<T>(`/outbound/${encodeURIComponent(operation)}`, { method: "POST", body: JSON.stringify({ warehouse, data }) })
};
