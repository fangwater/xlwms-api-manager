import { ClipboardList, Eye, MessageSquareText, PackagePlus, RefreshCw, Search, Send, Truck, X } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, dateTime } from "../components/Common";

type OrderMode = "parcel" | "bulk";
type OutboundOrder = {
  whCode?: string; outboundOrderNo?: string; thirdOrderNo?: string; status?: number; orderType?: string;
  logisticsChannel?: string; logisticsTrackNo?: string; salesPlatform?: string; referOrderNo?: string;
  platformOrderNo?: string; orderCreateTime?: string; outboundTime?: string; canceledTime?: string;
  exceptionTime?: string; productList?: Array<Record<string, unknown>>; [key: string]: unknown;
};
type OfficialResponse<T> = { code: number; msg: string; success?: boolean; data: T };
type OfficialPage = { records: OutboundOrder[]; total: number; page: number; pageSize: number; pages?: number };
type CancelState = { outboundOrderNo?: string; status?: number; msg?: string };

const statusLabels: Record<number, string> = { 0: "新建", 1: "已取面单", 2: "仓库处理中", 3: "已出库", 4: "已取消", 5: "异常", 6: "拦截中", 7: "面单异常" };

export default function OutboundPage({ warehouse }: { warehouse: string }) {
  const [mode, setMode] = useState<OrderMode>("parcel");
  const [query, setQuery] = useState("");
  const [submittedQuery, setSubmittedQuery] = useState("");
  const [page, setPage] = useState(1);
  const [data, setData] = useState<OfficialPage | null>(null);
  const [detail, setDetail] = useState<OutboundOrder | null>(null);
  const [messages, setMessages] = useState<Array<Record<string, unknown>>>([]);
  const [reply, setReply] = useState("");
  const [tracking, setTracking] = useState("");
  const [loading, setLoading] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const load = useCallback(async () => {
    if (!warehouse) { setData(null); return; }
    setLoading(true); setError("");
    try {
      const result = await api.outbound<OfficialResponse<OfficialPage>>(`${mode}-list`, warehouse, { page, pageSize: 30, ...(submittedQuery ? { outboundOrderNos: submittedQuery } : {}) });
      setData(result.data);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法加载出库单");
    } finally { setLoading(false); }
  }, [mode, page, submittedQuery, warehouse]);

  useEffect(() => { void load(); }, [load]);

  const changeMode = (next: OrderMode) => { setMode(next); setPage(1); setDetail(null); setNotice(""); };
  const submitSearch = () => { setSubmittedQuery(query.trim()); setPage(1); };
  const openDetail = async (order: OutboundOrder) => {
    if (!order.outboundOrderNo || !warehouse) return;
    setActionLoading(true); setError(""); setMessages([]); setTracking(order.logisticsTrackNo || "");
    try {
      const requestData = mode === "parcel" ? { outboundOrderNoList: order.outboundOrderNo } : { outboundOrderNoList: [order.outboundOrderNo] };
      const result = await api.outbound<OfficialResponse<OutboundOrder[]>>(`${mode}-detail`, warehouse, requestData);
      setDetail(result.data?.[0] || order);
      if (mode === "bulk") {
        const board = await api.outbound<OfficialResponse<Array<Record<string, unknown>>>>("message-detail", warehouse, { outboundOrderNo: order.outboundOrderNo });
        setMessages(board.data || []);
      }
    } catch (reason) { setError(reason instanceof Error ? reason.message : "无法加载出库单详情"); }
    finally { setActionLoading(false); }
  };
  const cancelOrder = async (order: OutboundOrder) => {
    if (!order.outboundOrderNo || !warehouse || !window.confirm(`确认取消出库单 ${order.outboundOrderNo}？`)) return;
    setActionLoading(true); setError(""); setNotice("");
    try {
      await api.outbound<OfficialResponse<CancelState[]>>(`${mode}-cancel`, warehouse, { outboundOrderNoList: [order.outboundOrderNo], ...(mode === "bulk" ? { orderThirdBindType: order.orderType?.includes("箱") ? 5 : 4 } : {}) });
      const result = await api.outbound<OfficialResponse<CancelState[]>>("cancel-status", warehouse, { outboundOrderNoList: [order.outboundOrderNo] });
      const state = result.data?.[0];
      setNotice(state?.status === 1 ? "取消成功" : state?.status === 2 ? `取消失败：${state.msg || "未知原因"}` : "取消请求处理中");
      void load();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "取消请求失败"); }
    finally { setActionLoading(false); }
  };
  const updateTracking = async () => {
    if (!detail?.outboundOrderNo || !tracking.trim()) return;
    setActionLoading(true); setError("");
    try {
      await api.outbound("tracking-label-update", warehouse, { outboundOrderNo: detail.outboundOrderNo, trackingNumber: tracking.trim() });
      setNotice("跟踪号已更新"); setDetail({ ...detail, logisticsTrackNo: tracking.trim() });
    } catch (reason) { setError(reason instanceof Error ? reason.message : "跟踪号更新失败"); }
    finally { setActionLoading(false); }
  };
  const sendReply = async () => {
    if (!detail?.outboundOrderNo || !reply.trim()) return;
    setActionLoading(true); setError("");
    try {
      await api.outbound("message-reply", warehouse, { outboundOrderNo: detail.outboundOrderNo, content: reply.trim() });
      setReply(""); setNotice("留言已添加");
      const board = await api.outbound<OfficialResponse<Array<Record<string, unknown>>>>("message-detail", warehouse, { outboundOrderNo: detail.outboundOrderNo });
      setMessages(board.data || []);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "留言发送失败"); }
    finally { setActionLoading(false); }
  };

  const pages = data?.pages || Math.max(1, Math.ceil((data?.total || 0) / (data?.pageSize || 30)));
  return <>
    <PageHeader title="出库管理" subtitle="小包与备货中转出库单" actions={<button className="icon-button bordered" onClick={() => void load()} title="刷新"><RefreshCw size={18} className={loading ? "spin" : ""} /></button>} />
    <div className="segmented-tabs compact-tabs" role="tablist"><button className={mode === "parcel" ? "active" : ""} onClick={() => changeMode("parcel")}><PackagePlus size={15} />小包出库</button><button className={mode === "bulk" ? "active" : ""} onClick={() => changeMode("bulk")}><Truck size={15} />备货中转</button></div>
    <div className="filter-bar"><label className="search-field"><Search size={17} /><input value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") submitSearch(); }} placeholder="出库单号，多个使用英文逗号分隔" /></label><button className="secondary-button" onClick={submitSearch}>查询</button></div>
    {notice && <div className="success-banner">{notice}</div>}{error && <ErrorState message={error} onRetry={() => void load()} />}
    {!warehouse ? <EmptyState label="请选择仓库" /> : loading && !data ? <LoadingState label="正在查询领星出库单" /> : data?.records?.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table"><thead><tr><th>出库单号</th><th>外部单号</th><th>仓库</th><th>状态</th><th>物流渠道</th><th>跟踪号</th><th>创建时间</th><th>操作</th></tr></thead><tbody>{data.records.map((order, index) => <tr key={order.outboundOrderNo || index}><td><strong>{order.outboundOrderNo || "-"}</strong></td><td>{order.thirdOrderNo || "-"}</td><td>{order.whCode || warehouse}</td><td><span className={`order-status status-${order.status ?? 0}`}>{statusLabels[order.status ?? 0] || `状态 ${order.status}`}</span></td><td>{order.logisticsChannel || "-"}</td><td>{order.logisticsTrackNo || "-"}</td><td>{dateTime(order.orderCreateTime)}</td><td><div className="row-actions"><button className="icon-button" onClick={() => void openDetail(order)} title="查看详情"><Eye size={16} /></button>{order.status !== 3 && order.status !== 4 && <button className="icon-button danger-button" disabled={actionLoading} onClick={() => void cancelOrder(order)} title="取消出库单"><X size={16} /></button>}</div></td></tr>)}</tbody></table></div></div> : <EmptyState label="当前筛选暂无出库单" />}
    {data && data.total > 0 && <Pagination page={data.page || page} pages={pages} total={data.total} onChange={setPage} />}
    {detail && <div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) setDetail(null); }}><section className="modal outbound-detail"><header><div><h2>{detail.outboundOrderNo}</h2><p>{detail.thirdOrderNo || detail.referOrderNo || "出库单详情"}</p></div><button className="icon-button" onClick={() => setDetail(null)} title="关闭"><X size={18} /></button></header><div className="detail-body"><dl className="detail-grid"><Detail label="仓库" value={detail.whCode} /><Detail label="状态" value={statusLabels[detail.status ?? 0]} /><Detail label="物流渠道" value={detail.logisticsChannel} /><Detail label="销售平台" value={detail.salesPlatform} /><Detail label="创建时间" value={dateTime(detail.orderCreateTime)} /><Detail label="出库时间" value={dateTime(detail.outboundTime)} /></dl><div className="tracking-editor"><label><ClipboardList size={16} />物流跟踪号</label><input value={tracking} onChange={(event) => setTracking(event.target.value)} /><button className="secondary-button" disabled={!tracking.trim() || actionLoading} onClick={() => void updateTracking()}>更新</button></div>{mode === "bulk" && <section className="message-board"><h3><MessageSquareText size={16} />留言板</h3><div className="message-list">{messages.length ? messages.map((message, index) => <article key={index}><strong>{String(message.creatorName || message.createBy || "系统")}</strong><p>{String(message.content || message.message || "")}</p><small>{String(message.createTime || "")}</small></article>) : <span>暂无留言</span>}</div><div className="message-compose"><textarea value={reply} onChange={(event) => setReply(event.target.value)} rows={3} /><button className="primary-button" disabled={!reply.trim() || actionLoading} onClick={() => void sendReply()}><Send size={15} />发送</button></div></section>}</div></section></div>}
  </>;
}

function Detail({ label, value }: { label: string; value: unknown }) { return <div><dt>{label}</dt><dd>{value === undefined || value === null || value === "" ? "-" : String(value)}</dd></div>; }
