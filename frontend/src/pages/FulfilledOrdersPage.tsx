import { Archive, RefreshCw, Search } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, dateTime } from "../components/Common";
import type { FulfilledOrderPage } from "../types";

export default function FulfilledOrdersPage({ warehouse }: { warehouse: string }) {
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState("");
  const [submittedQuery, setSubmittedQuery] = useState("");
  const [shop, setShop] = useState("");
  const [data, setData] = useState<FulfilledOrderPage | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true); setError("");
    try {
      setData(await api.fulfilledOrders({ warehouse, shop, q: submittedQuery, page, pageSize: 30 }));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法加载已出库平台单");
    } finally { setLoading(false); }
  }, [warehouse, shop, submittedQuery, page]);

  useEffect(() => { void load(); }, [load]);
  const submitSearch = () => { setSubmittedQuery(query.trim()); setPage(1); };

  return <>
    <PageHeader title="已出库平台单" subtitle={`已从履约核查归档${data?.last_query_at ? ` · 最近查询 ${dateTime(data.last_query_at)}` : ""}`} actions={<button className="icon-button bordered" onClick={() => void load()} title="刷新"><RefreshCw size={18} className={loading ? "spin" : ""} /></button>} />
    <div className="filter-bar audit-filters">
      <label className="search-field"><Search size={17} /><input value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === "Enter") submitSearch(); }} placeholder="PO 单号、出库单号或跟踪号" /></label>
      <button className="secondary-button" onClick={submitSearch}>查询</button>
      <label className="select-field"><select value={shop} onChange={event => { setShop(event.target.value); setPage(1); }}><option value="">全部店铺</option>{data?.shops.map(item => <option value={item.code} key={item.code}>{item.name || item.code}</option>)}</select></label>
    </div>
    {error && <ErrorState message={error} onRetry={() => void load()} />}
    {loading && !data ? <LoadingState label="正在加载已出库平台单" /> : data?.records.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table"><thead><tr><th>店铺 / PO</th><th>仓库</th><th>出库单号</th><th>状态</th><th>出库时间</th><th>跟踪号</th><th>归档时间</th></tr></thead><tbody>{data.records.map(item => <tr key={item.id}><td><strong>{item.platform_order_no}</strong><small className="cell-subtitle">{item.shop_name || item.shop_code}</small></td><td>{item.wh_code || "-"}</td><td><strong>{item.outbound_order_no || "-"}</strong></td><td><span className="audit-status oms-outbound"><Archive size={14} />已出库</span></td><td>{dateTime(item.oms_outbound_at)}</td><td>{item.oms_tracking_number || item.tracking_number || "-"}</td><td>{dateTime(item.updated_at)}</td></tr>)}</tbody></table></div></div> : <EmptyState label="当前筛选暂无已出库平台单" />}
    {data && data.total > 0 && <Pagination page={data.page} pages={data.pages} total={data.total} onChange={setPage} />}
  </>;
}
