import { CircleCheckBig, Clock3, PackageCheck, RefreshCw, Search, ShieldAlert, TriangleAlert, Truck } from "lucide-react";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, dateTime } from "../components/Common";
import type { FulfilledOrderPage, FulfillmentAudit, Warehouse } from "../types";

const categoryLabels: Record<string, string> = {
  awaiting_pickup: "待承运商揽收",
  picked_up: "已揽收",
  pickup_exception: "揽收异常订单",
  tracking_error: "轨迹查询异常"
};

const trackingStatusLabels: Record<string, string> = {
  "last mile manifest": "待承运商揽收",
  "last mile carrier pick up failed": "揽收异常",
  "last mile carrier picked up": "已揽收",
  "in transit": "运输中",
  "arrived at post office": "已到达投递网点",
  "out for delivery": "派送中",
  "delivery exception": "派送异常",
  "delivery failure non carrier": "非承运商原因派送失败",
  "delivered": "已签收",
  "carrier accepted lost package claim": "承运商已受理丢件索赔"
};

export default function FulfilledOrdersPage({ warehouse, warehouses, onWarehouseChange }: { warehouse: string; warehouses: Warehouse[]; onWarehouseChange: (value: string) => void }) {
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState("");
  const [submittedQuery, setSubmittedQuery] = useState("");
  const [shop, setShop] = useState("");
  const [trackingCategory, setTrackingCategory] = useState("");
  const [data, setData] = useState<FulfilledOrderPage | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      setData(await api.fulfilledOrders({ warehouse, shop, trackingCategory, q: submittedQuery, page, pageSize: 30 }));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法加载出库物流跟踪");
    } finally {
      setLoading(false);
    }
  }, [warehouse, shop, trackingCategory, submittedQuery, page]);

  useEffect(() => { void load(); }, [load]);
  useEffect(() => {
    const timer = window.setInterval(() => void load(), 60_000);
    return () => window.clearInterval(timer);
  }, [load]);

  const selectCategory = (value: string) => {
    setTrackingCategory(current => current === value ? "" : value);
    setPage(1);
  };
  const submitSearch = () => {
    setSubmittedQuery(query.trim());
    setPage(1);
  };
  const summary = data?.summary;

  return <>
    <PageHeader
      title="出库物流跟踪"
      subtitle={`Temu 已出库订单的承运商揽收状态${data?.last_tracking_at ? ` · 最近追踪 ${dateTime(data.last_tracking_at)}` : ""}`}
      actions={<button className="icon-button bordered" onClick={() => void load()} title="刷新"><RefreshCw size={18} className={loading ? "spin" : ""} /></button>}
    />
    <section className="audit-summary">
      <SummaryButton label="全部已出库" value={summary?.total || 0} icon={<Truck size={17} />} active={!trackingCategory} onClick={() => { setTrackingCategory(""); setPage(1); }} />
      <SummaryButton label="揽收异常订单" value={summary?.pickup_exception || 0} icon={<ShieldAlert size={17} />} tone="danger" active={trackingCategory === "pickup_exception"} onClick={() => selectCategory("pickup_exception")} />
      <SummaryButton label="待承运商揽收" value={summary?.awaiting_pickup || 0} icon={<Clock3 size={17} />} tone="warning" active={trackingCategory === "awaiting_pickup"} onClick={() => selectCategory("awaiting_pickup")} />
      <SummaryButton label="已揽收" value={summary?.picked_up || 0} icon={<CircleCheckBig size={17} />} active={trackingCategory === "picked_up"} onClick={() => selectCategory("picked_up")} />
      <SummaryButton label="轨迹查询异常" value={summary?.tracking_error || 0} icon={<TriangleAlert size={17} />} tone="danger" active={trackingCategory === "tracking_error"} onClick={() => selectCategory("tracking_error")} />
    </section>
    <div className="filter-bar audit-filters">
      <label className="search-field"><Search size={17} /><input value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === "Enter") submitSearch(); }} placeholder="PO 单号、出库单号或跟踪号" /></label>
      <button className="secondary-button" onClick={submitSearch}>查询</button>
      <label className="select-field"><select aria-label="选择店铺" value={shop} onChange={event => { setShop(event.target.value); setPage(1); }}><option value="">全部店铺</option>{data?.shops.map(item => <option value={item.code} key={item.code}>{item.name || item.code}</option>)}</select></label>
      <label className="select-field"><select aria-label="选择仓库" value={warehouse} onChange={event => { onWarehouseChange(event.target.value); setPage(1); }}><option value="">全部仓库</option>{warehouses.map(item => <option value={item.wh_code} key={item.wh_code}>{item.name || item.wh_code}</option>)}</select></label>
    </div>
    {error && <ErrorState message={error} onRetry={() => void load()} />}
    {loading && !data ? <LoadingState label="正在查询出库物流" /> : data?.records.length
      ? <div className="table-panel"><div className="table-scroll"><table className="data-table tracking-table"><thead><tr><th>店铺 / PO</th><th>仓库 / 出库单</th><th>揽收分类</th><th>Temu 最新轨迹</th><th>出库时长</th><th>包裹进度</th><th>最后一公里单号</th><th>最近追踪</th></tr></thead><tbody>{data.records.map(item => <TrackingRow item={item} key={item.id} />)}</tbody></table></div></div>
      : <EmptyState label="当前筛选暂无出库物流订单" />}
    {data && data.total > 0 && <Pagination page={data.page} pages={data.pages} total={data.total} onChange={setPage} />}
  </>;
}

function SummaryButton({ label, value, icon, tone = "", active, onClick }: { label: string; value: number; icon: ReactNode; tone?: string; active: boolean; onClick: () => void }) {
  return <button className={`audit-summary-item ${tone} ${active ? "active" : ""}`} onClick={onClick}><span>{icon}{label}</span><strong>{value.toLocaleString()}</strong></button>;
}

function TrackingRow({ item }: { item: FulfillmentAudit }) {
  const trackingNumber = item.last_mile_tracking_number || item.oms_tracking_number || item.tracking_number;
  const statusLabel = trackingStatusLabel(item.tracking_status);
  return <tr>
    <td><strong>{item.platform_order_no}</strong><small className="cell-subtitle">{item.shop_name || item.shop_code}</small></td>
    <td><strong>{item.wh_code || "-"}</strong><small className="cell-subtitle">{item.outbound_order_no || "-"}</small></td>
    <td><span className={`audit-status category-${item.tracking_category || "awaiting_pickup"}`}>{categoryLabels[item.tracking_category] || "待承运商揽收"}</span><small className="cell-subtitle audit-reason">{trackingReason(item)}</small>{item.tracking_error && <small className="cell-error" title={item.tracking_error}>{item.tracking_error}</small>}</td>
    <td><strong>{statusLabel}</strong>{item.tracking_status && <small className="cell-subtitle">{item.tracking_status}</small>}{item.tracking_status_text && item.tracking_status_text !== item.tracking_status && <small className="cell-subtitle audit-reason">{item.tracking_status_text}</small>}</td>
    <td>{elapsed(item.oms_outbound_at)}<small className="cell-subtitle">{dateTime(item.oms_outbound_at)}</small></td>
    <td><span className="tracking-progress"><PackageCheck size={14} /> {item.picked_up_package_count || 0} / {item.tracking_package_count || 0}</span></td>
    <td>{trackingNumber || "-"}</td>
    <td>{dateTime(item.tracking_checked_at)}{item.tracking_updated_at && <small className="cell-subtitle">轨迹 {dateTime(item.tracking_updated_at)}</small>}</td>
  </tr>;
}

function trackingReason(item: FulfillmentAudit) {
  if (item.pickup_exception_reason === "pickup_failed") return "承运商返回揽收失败";
  if (item.pickup_exception_reason === "pickup_overdue") return "出库满 24 小时仍未显示已揽收";
  if (item.tracking_category === "awaiting_pickup") return "出库未满 24 小时，等待承运商揽收";
  if (item.tracking_category === "picked_up") return item.pickup_confirmed_at ? `已于 ${dateTime(item.pickup_confirmed_at)} 揽收` : "全部包裹已揽收";
  if (item.tracking_category === "tracking_error") return "Temu 轨迹暂时无法查询";
  return "";
}

function trackingStatusLabel(value: string) {
  if (!value) return "暂无轨迹";
  const normalized = value.toLowerCase().replace(/[-_]/g, " ").replace(/\s+/g, " ").trim();
  return trackingStatusLabels[normalized] || value;
}

function elapsed(value?: string) {
  if (!value) return "-";
  const milliseconds = Date.now() - new Date(value).getTime();
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return "-";
  const hours = Math.floor(milliseconds / 3_600_000);
  if (hours < 1) return `${Math.max(1, Math.floor(milliseconds / 60_000))} 分钟`;
  return hours < 48 ? `${hours} 小时` : `${Math.floor(hours / 24)} 天 ${hours % 24} 小时`;
}
