import { AlertTriangle, Download, RefreshCw, Search, ShieldAlert, Truck, Warehouse } from "lucide-react";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, dateTime } from "../components/Common";
import type { FulfillmentAudit, FulfillmentAuditPage } from "../types";

const categoryLabels: Record<string, string> = {
  pending_query: "待查询",
  monitoring: "正常核查",
  manual_required: "转人工",
  warehouse_overdue: "仓库超时",
  sync_error: "查询异常"
};

const omsStatusLabels: Record<string, string> = {
  pending_query: "待查询",
  not_found: "未匹配",
  pending: "待处理",
  processing: "仓库处理中",
  outbound: "已出库",
  exception: "异常订单",
  query_error: "查询失败",
  unknown: "未知状态"
};

const omsStatusCodeLabels: Record<number, string> = {
  0: "新建",
  1: "已取面单",
  2: "仓库处理中",
  3: "已出库",
  4: "已取消",
  5: "异常",
  6: "拦截中",
  7: "面单异常"
};

export default function FulfillmentAuditsPage({ warehouse }: { warehouse: string }) {
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState("");
  const [submittedQuery, setSubmittedQuery] = useState("");
  const [shop, setShop] = useState("");
  const [category, setCategory] = useState("");
  const [omsStatus, setOMSStatus] = useState("");
  const [data, setData] = useState<FulfillmentAuditPage | null>(null);
  const [loading, setLoading] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true); setError("");
    try {
      setData(await api.fulfillmentAudits({ shop, warehouse, category, omsStatus, q: submittedQuery, page, pageSize: 30 }));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法加载履约核查数据");
    } finally { setLoading(false); }
  }, [shop, warehouse, category, omsStatus, submittedQuery, page]);

  useEffect(() => { void load(); }, [load]);
  useEffect(() => {
    const timer = window.setInterval(() => void load(), 60_000);
    return () => window.clearInterval(timer);
  }, [load]);

  const selectCategory = (value: string) => { setCategory(current => current === value ? "" : value); setPage(1); };
  const submitSearch = () => { setSubmittedQuery(query.trim()); setPage(1); };
  const exportManual = async () => {
    setExporting(true); setError("");
    try {
      const file = await api.exportManualFulfillmentAudits({ shop, warehouse, omsStatus, q: submittedQuery });
      const url = URL.createObjectURL(file.blob);
      const link = document.createElement("a");
      link.href = url; link.download = file.filename;
      document.body.appendChild(link); link.click(); link.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法导出人工订单");
    } finally { setExporting(false); }
  };
  const summary = data?.summary;

  return <>
    <PageHeader title="履约核查" subtitle={`Temu 店铺与领星仓库状态${summary?.last_query_at ? ` · 最近查询 ${dateTime(summary.last_query_at)}` : ""}`} actions={<><button className="secondary-button audit-export-button" onClick={() => void exportManual()} disabled={exporting || !summary?.manual_required} title="导出人工订单 CSV"><Download size={16} /><span>{exporting ? "导出中" : "导出人工订单"}</span></button><button className="icon-button bordered" onClick={() => void load()} title="刷新"><RefreshCw size={18} className={loading ? "spin" : ""} /></button></>} />
    <section className="audit-summary">
      <SummaryButton label="核查中" value={summary?.total || 0} icon={<Truck size={17} />} active={!category} onClick={() => { setCategory(""); setPage(1); }} />
      <SummaryButton label="待查询" value={summary?.pending_query || 0} icon={<RefreshCw size={17} />} active={category === "pending_query"} onClick={() => selectCategory("pending_query")} />
      <SummaryButton label="转人工" value={summary?.manual_required || 0} icon={<ShieldAlert size={17} />} tone="danger" active={category === "manual_required"} onClick={() => selectCategory("manual_required")} />
      <SummaryButton label="仓库超时" value={summary?.warehouse_overdue || 0} icon={<Warehouse size={17} />} tone="warning" active={category === "warehouse_overdue"} onClick={() => selectCategory("warehouse_overdue")} />
      <SummaryButton label="查询异常" value={summary?.sync_error || 0} icon={<AlertTriangle size={17} />} tone="danger" active={category === "sync_error"} onClick={() => selectCategory("sync_error")} />
    </section>
    <div className="filter-bar audit-filters">
      <label className="search-field"><Search size={17} /><input value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === "Enter") submitSearch(); }} placeholder="PO 单号、出库单号或跟踪号" /></label>
      <button className="secondary-button" onClick={submitSearch}>查询</button>
      <label className="select-field"><select value={shop} onChange={event => { setShop(event.target.value); setPage(1); }}><option value="">全部店铺</option>{data?.shops.map(item => <option value={item.code} key={item.code}>{item.name || item.code}</option>)}</select></label>
      <label className="select-field"><select value={omsStatus} onChange={event => { setOMSStatus(event.target.value); setPage(1); }}><option value="">全部 OMS 状态</option>{Object.entries(omsStatusLabels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select></label>
    </div>
    {error && <ErrorState message={error} onRetry={() => void load()} />}
    {loading && !data ? <LoadingState label="正在加载履约状态" /> : data?.records.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table audit-table"><thead><tr><th>店铺 / PO</th><th>仓库</th><th>OMS 状态</th><th>核查结果</th><th>状态时长</th><th>出库时间</th><th>跟踪号</th><th>最近核查</th></tr></thead><tbody>{data.records.map(item => <AuditRow item={item} key={item.id} />)}</tbody></table></div></div> : <EmptyState label="当前筛选暂无履约核查订单" />}
    {data && data.total > 0 && <Pagination page={data.page} pages={data.pages} total={data.total} onChange={setPage} />}
  </>;
}

function SummaryButton({ label, value, icon, tone = "", active, onClick }: { label: string; value: number; icon: ReactNode; tone?: string; active: boolean; onClick: () => void }) {
  return <button className={`audit-summary-item ${tone} ${active ? "active" : ""}`} onClick={onClick}><span>{icon}{label}</span><strong>{value.toLocaleString()}</strong></button>;
}

function AuditRow({ item }: { item: FulfillmentAudit }) {
  const since = item.oms_status === "processing" ? item.oms_processing_since : item.oms_status === "outbound" ? item.oms_outbound_at : item.oms_status_since;
  const tracking = item.oms_tracking_number || item.tracking_number;
  const reason = auditReason(item);
  return <tr>
    <td><strong>{item.platform_order_no}</strong><small className="cell-subtitle">{item.shop_name || item.shop_code}</small></td>
    <td><strong>{item.wh_code || "待匹配"}</strong><small className="cell-subtitle">{item.warehouse_key || "-"}</small></td>
    <td><span className={`audit-status oms-${item.oms_status}`}>{omsStatusLabel(item)}</span>{item.outbound_order_no && <small className="cell-subtitle">{item.outbound_order_no}</small>}</td>
    <td><span className={`audit-status category-${item.exception_category}`}>{categoryLabels[item.exception_category] || item.exception_category}</span>{reason && <small className="cell-subtitle audit-reason">{reason}</small>}{item.sync_error && <small className="cell-error" title={item.sync_error}>{item.sync_error}</small>}</td>
    <td>{elapsed(since)}</td>
    <td>{dateTime(item.oms_outbound_at)}</td>
    <td>{tracking || "-"}</td>
    <td>{dateTime(item.last_checked_at)}</td>
  </tr>;
}

function auditReason(item: FulfillmentAudit) {
  if (item.exception_category === "manual_required") {
    switch (item.oms_status_code) {
      case 0: return "领星出库单为新建状态";
      case 1: return "领星已取面单，仍待仓库处理";
      case 4: return "领星出库单已取消";
      case 5: return "领星出库单异常";
      case 6: return "领星出库单拦截中";
      case 7: return "领星出库单面单异常";
    }
    if (item.oms_status === "not_found") return "未在领星找到出库单";
    if (item.oms_status === "pending") return "领星订单待处理";
    if (item.oms_status === "unknown") return "领星订单状态未知";
    return "需要人工核查";
  }
  if (item.exception_category === "warehouse_overdue") return "仓库处理中超过 24 小时仍未出库";
  if (item.exception_category === "pending_query") return "等待查询领星状态";
  return "";
}

function omsStatusLabel(item: FulfillmentAudit) {
  if (item.oms_status_code !== undefined && omsStatusCodeLabels[item.oms_status_code]) return omsStatusCodeLabels[item.oms_status_code];
  return omsStatusLabels[item.oms_status] || item.oms_status;
}

function elapsed(value?: string) {
  if (!value) return "-";
  const milliseconds = Date.now() - new Date(value).getTime();
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return "-";
  const hours = Math.floor(milliseconds / 3_600_000);
  if (hours < 1) return `${Math.max(1, Math.floor(milliseconds / 60_000))} 分钟`;
  return hours < 48 ? `${hours} 小时` : `${Math.floor(hours / 24)} 天 ${hours % 24} 小时`;
}
