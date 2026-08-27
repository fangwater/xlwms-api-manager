import { Link2, LoaderCircle, Plus, RefreshCw, Search, Trash2, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, dateTime } from "../components/Common";
import type { PlatformOrderAccountOption, ProductPairing, ProductPairingPage } from "../types";
import "./ProductPairingsPage.css";

type QueryField = "platform_sku" | "system_sku" | "product_name";
type ItemDraft = { id: number; systemSKU: string; quantity: string };
type PairingStatus = { label: string; className: string; count: number };

const pageSize = 30;
const pairingStatuses: Record<number, Omit<PairingStatus, "count">> = {
  0: { label: "新建", className: "new" },
  1: { label: "审核中", className: "reviewing" },
  2: { label: "已审核", className: "approved" },
  3: { label: "已驳回", className: "rejected" },
  4: { label: "废弃", className: "discarded" },
};

function summarizePairingStatuses(item: ProductPairing): PairingStatus[] {
  const counts = new Map<number, number>();
  item.items.forEach((member) => counts.set(member.approve_status, (counts.get(member.approve_status) || 0) + 1));
  return [...counts.entries()].map(([status, count]) => ({
    ...(pairingStatuses[status] || { label: `未知状态 ${status}`, className: "unknown" }), count,
  }));
}

export default function ProductPairingsPage() {
  const [accounts, setAccounts] = useState<PlatformOrderAccountOption[]>([]);
  const [account, setAccount] = useState(() => localStorage.getItem("xlwms-product-pairing-account") || "");
  const [data, setData] = useState<ProductPairingPage | null>(null);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [queryField, setQueryField] = useState<QueryField>("platform_sku");
  const [queryInput, setQueryInput] = useState("");
  const [storeInput, setStoreInput] = useState("");
  const [query, setQuery] = useState("");
  const [storeCode, setStoreCode] = useState("");
  const [editorOpen, setEditorOpen] = useState(false);
  const [deleting, setDeleting] = useState("");
  const loadSequence = useRef(0);

  useEffect(() => {
    let active = true;
    void api.platformOrderAccounts().then((items) => {
      if (!active) return;
      setAccounts(items);
      setAccount((current) => items.some((item) => item.key === current) ? current : items[0]?.key || "");
    }).catch((reason) => {
      if (active) setError(reason instanceof Error ? reason.message : "无法加载 OMS 账户");
    });
    return () => { active = false; };
  }, []);

  const load = useCallback(async () => {
    if (!account) return;
    const sequence = ++loadSequence.current;
    setLoading(true);
    setError("");
    try {
      const result = await api.productPairings({ account, storeCode, q: query, queryField, page, pageSize });
      if (sequence === loadSequence.current) setData(result);
    } catch (reason) {
      if (sequence === loadSequence.current) {
        setData(null);
        setError(reason instanceof Error ? reason.message : "无法加载组合配对");
      }
    } finally {
      if (sequence === loadSequence.current) setLoading(false);
    }
  }, [account, page, query, queryField, storeCode]);

  useEffect(() => { void load(); }, [load]);

  const selectedAccount = useMemo(() => accounts.find((item) => item.key === account), [account, accounts]);
  const changeAccount = (value: string) => {
    setAccount(value);
    localStorage.setItem("xlwms-product-pairing-account", value);
    setPage(1);
    setSuccess("");
  };
  const search = (event: FormEvent) => {
    event.preventDefault();
    const nextQuery = queryInput.trim();
    const nextStore = storeInput.trim();
    setPage(1);
    setQuery(nextQuery);
    setStoreCode(nextStore);
    setSuccess("");
    if (page === 1 && query === nextQuery && storeCode === nextStore) void load();
  };
  const remove = async (item: ProductPairing) => {
    if (!window.confirm(`删除 ${item.store_name || item.store_code} 的组合配对“${item.platform_sku}”？`)) return;
    const key = `${item.store_code}\u0000${item.platform_sku}`;
    setDeleting(key);
    setError("");
    setSuccess("");
    try {
      await api.deleteProductPairing({ account, store_code: item.store_code, platform_sku: item.platform_sku });
      setSuccess(`组合配对“${item.platform_sku}”已删除`);
      if (data?.records.length === 1 && page > 1) setPage(page - 1); else await load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法删除组合配对");
    } finally {
      setDeleting("");
    }
  };

  return <>
    <PageHeader title="组合配对" subtitle="同步维护领星 OMS 的平台 SKU 与系统 SKU 组合映射" actions={<>
      <button className="icon-button bordered" type="button" onClick={() => void load()} disabled={loading || !account} title="刷新组合配对"><RefreshCw className={loading ? "spin" : ""} size={17} /></button>
      <button className="primary-button" type="button" onClick={() => setEditorOpen(true)} disabled={!account || selectedAccount?.available === false}><Plus size={16} />新建配对</button>
    </>} />

    {selectedAccount?.available === false && <div className="pairing-account-alert" role="alert"><strong>{selectedAccount.label}已掉线</strong><span>{selectedAccount.error || "请先在平台订单页重新登录"}</span></div>}
    {error && <ErrorState message={error} onRetry={() => void load()} />}
    {success && <div className="success-banner" role="status"><Link2 size={17} /><span>{success}</span></div>}

    <form className="pairing-toolbar" onSubmit={search}>
      <label><span>OMS 账户</span><select aria-label="OMS 账户" value={account} onChange={(event) => changeAccount(event.target.value)} disabled={!accounts.length}>{accounts.length ? accounts.map((item) => <option key={item.key} value={item.key}>{item.label}{item.available === false ? "（已掉线）" : ""}</option>) : <option value="">暂无账户</option>}</select></label>
      <label><span>店铺编码</span><input aria-label="店铺编码" value={storeInput} onChange={(event) => setStoreInput(event.target.value)} placeholder="全部店铺" /></label>
      <label><span>查询字段</span><select aria-label="查询字段" value={queryField} onChange={(event) => { setQueryField(event.target.value as QueryField); setPage(1); }}><option value="platform_sku">平台 SKU</option><option value="system_sku">系统 SKU</option><option value="product_name">品名</option></select></label>
      <label className="pairing-query"><span>关键词</span><div><Search size={16} /><input aria-label="配对关键词" value={queryInput} onChange={(event) => setQueryInput(event.target.value)} placeholder="输入精确或模糊关键词" /></div></label>
      <button className="secondary-button" type="submit" disabled={loading || !account}>查询</button>
    </form>

    {loading && !data ? <LoadingState label="正在加载组合配对" /> : data?.records.length ? <div className="table-panel pairing-table-panel"><div className="table-scroll"><table className="data-table pairing-table">
      <thead><tr><th>店铺</th><th>平台 SKU</th><th>系统 SKU 组合</th><th>状态</th><th>创建时间</th><th>操作</th></tr></thead>
      <tbody>{data.records.map((item, index) => {
        const key = `${item.store_code}\u0000${item.platform_sku}`;
        const statuses = summarizePairingStatuses(item);
        return <tr key={item.id || `${key}-${index}`}>
          <td><div className="primary-cell"><strong>{item.store_name || item.store_code}</strong><small>{item.store_name ? item.store_code : "-"}</small></div></td>
          <td><strong>{item.platform_sku}</strong></td>
          <td><div className="pairing-members">{item.items.map((member, memberIndex) => <span key={`${member.system_sku}-${memberIndex}`}><b>{member.system_sku}</b><em>× {member.quantity}</em>{member.product_name && <small>{member.product_name}</small>}</span>)}</div></td>
          <td><div className="pairing-status-list">{statuses.length ? statuses.map((status) => <span className={`pairing-status ${status.className}`} key={status.className}>{status.label}{statuses.length > 1 ? ` ${status.count}` : ""}</span>) : <span className="pairing-status unknown">未知</span>}</div></td>
          <td>{dateTime(item.created_at)}</td>
          <td><button className="icon-button danger-button" type="button" onClick={() => void remove(item)} disabled={deleting === key} title="删除组合配对">{deleting === key ? <LoaderCircle className="spin" size={16} /> : <Trash2 size={16} />}</button></td>
        </tr>;
      })}</tbody>
    </table></div></div> : !loading && !error && <EmptyState label={query || storeCode ? "没有符合条件的组合配对" : "当前没有组合配对"} />}
    {data && data.total > 0 && <Pagination page={data.page} pages={data.pages} total={data.total} onChange={setPage} />}

    {editorOpen && <ProductPairingEditor account={account} accountLabel={selectedAccount?.label || account} onClose={() => setEditorOpen(false)} onSaved={async (platformSKU) => { setEditorOpen(false); setSuccess(`组合配对“${platformSKU}”已创建`); await load(); }} />}
  </>;
}

function ProductPairingEditor({ account, accountLabel, onClose, onSaved }: { account: string; accountLabel: string; onClose: () => void; onSaved: (platformSKU: string) => Promise<void> }) {
  const nextItemID = useRef(2);
  const [storeCode, setStoreCode] = useState("");
  const [platformSKU, setPlatformSKU] = useState("");
  const [items, setItems] = useState<ItemDraft[]>([{ id: 1, systemSKU: "", quantity: "1" }]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const updateItem = (id: number, patch: Partial<ItemDraft>) => setItems((current) => current.map((item) => item.id === id ? { ...item, ...patch } : item));
  const addItem = () => setItems((current) => current.length >= 20 ? current : [...current, { id: nextItemID.current++, systemSKU: "", quantity: "1" }]);
  const removeItem = (id: number) => setItems((current) => current.length === 1 ? current : current.filter((item) => item.id !== id));
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      const normalizedItems = items.map((item) => ({ system_sku: item.systemSKU.trim(), quantity: Number(item.quantity) }));
      if (new Set(normalizedItems.map((item) => item.system_sku)).size !== normalizedItems.length) {
        throw new Error("系统 SKU 不能重复");
      }
      await api.createProductPairing({ account, store_code: storeCode.trim(), platform_sku: platformSKU.trim(), items: normalizedItems });
      await onSaved(platformSKU.trim());
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法创建组合配对");
    } finally {
      setSaving(false);
    }
  };

  return <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (!saving && event.target === event.currentTarget) onClose(); }}>
    <section className="modal pairing-editor" role="dialog" aria-modal="true" aria-labelledby="pairing-editor-title">
      <header><div><h2 id="pairing-editor-title">新建组合配对</h2><p>{accountLabel}</p></div><button className="icon-button" type="button" onClick={onClose} disabled={saving} title="关闭"><X size={18} /></button></header>
      <form onSubmit={(event) => void submit(event)}>
        {error && <ErrorState message={error} />}
        <div className="form-grid">
          <label><span>店铺编码</span><input required maxLength={255} autoFocus value={storeCode} onChange={(event) => setStoreCode(event.target.value)} /></label>
          <label><span>平台 SKU</span><input required maxLength={255} value={platformSKU} onChange={(event) => setPlatformSKU(event.target.value)} /></label>
        </div>
        <div className="pairing-item-heading"><div><strong>系统 SKU 组合</strong><span>{items.length} / 20</span></div><button className="secondary-button" type="button" onClick={addItem} disabled={items.length >= 20}><Plus size={15} />添加 SKU</button></div>
        <div className="pairing-item-list">{items.map((item, index) => <div className="pairing-item-row" key={item.id}>
          <span>{index + 1}</span>
          <label><span>系统 SKU</span><input required maxLength={255} aria-label={`系统 SKU ${index + 1}`} value={item.systemSKU} onChange={(event) => updateItem(item.id, { systemSKU: event.target.value })} /></label>
          <label className="pairing-quantity"><span>数量</span><input required type="number" min="1" max="999999" step="1" aria-label={`数量 ${index + 1}`} value={item.quantity} onChange={(event) => updateItem(item.id, { quantity: event.target.value })} /></label>
          <button className="icon-button danger-button" type="button" onClick={() => removeItem(item.id)} disabled={items.length === 1} title="移除 SKU"><Trash2 size={15} /></button>
        </div>)}</div>
        <footer><button className="secondary-button" type="button" onClick={onClose} disabled={saving}>取消</button><button className="primary-button" type="submit" disabled={saving}>{saving ? <LoaderCircle className="spin" size={16} /> : <Link2 size={16} />}{saving ? "正在创建" : "创建配对"}</button></footer>
      </form>
    </section>
  </div>;
}
