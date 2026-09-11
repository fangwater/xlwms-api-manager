import { KeyRound, LoaderCircle, Pencil, Plus, RefreshCw, Search, Tags, Trash2, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, dateTime } from "../components/Common";
import type { ConsoleCredentials, PlatformSKUMapping, PlatformSKUMappingPage } from "../types";
import "./ProductPairingsPage.css";
import "./PlatformSKUMappingsPage.css";

type ItemDraft = { id: number; warehouseSKU: string; quantity: string };
const pageSize = 50;

export default function PlatformSKUMappingsPage() {
  const [platform, setPlatform] = useState("shein");
  const [queryInput, setQueryInput] = useState("");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [data, setData] = useState<PlatformSKUMappingPage | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [editor, setEditor] = useState<PlatformSKUMapping | null | undefined>(undefined);
  const [credentials, setCredentials] = useState<ConsoleCredentials>({ username: "", password: "" });
  const [deleting, setDeleting] = useState("");
  const sequence = useRef(0);

  const load = useCallback(async () => {
    const current = ++sequence.current;
    setLoading(true);
    setError("");
    try {
      const result = await api.platformSKUMappings({ platform, q: query, status: "all", page, pageSize });
      if (sequence.current === current) setData(result);
    } catch (reason) {
      if (sequence.current === current) setError(reason instanceof Error ? reason.message : "无法加载 SKU 映射");
    } finally {
      if (sequence.current === current) setLoading(false);
    }
  }, [page, platform, query]);

  useEffect(() => { void load(); }, [load]);
  const search = (event: FormEvent) => {
    event.preventDefault();
    setPage(1);
    setQuery(queryInput.trim());
    setSuccess("");
  };
  const remove = async (mapping: PlatformSKUMapping) => {
    if (!credentials.username || !credentials.password) {
      setError("删除前请填写维护账号和密码");
      return;
    }
    if (!window.confirm(`删除 ${mapping.platform_sku} 的仓库 SKU 映射？`)) return;
    setDeleting(mapping.platform_sku);
    setError("");
    try {
      await api.deletePlatformSKUMapping(mapping.platform, mapping.platform_sku, credentials);
      setSuccess(`映射“${mapping.platform_sku}”已删除`);
      if (data?.records.length === 1 && page > 1) setPage(page - 1); else await load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法删除 SKU 映射");
    } finally {
      setDeleting("");
    }
  };

  return <>
    <PageHeader title="平台 SKU 映射" actions={<>
      <button className="icon-button bordered" type="button" onClick={() => void load()} disabled={loading} title="刷新映射"><RefreshCw className={loading ? "spin" : ""} size={17} /></button>
      <button className="primary-button" type="button" onClick={() => setEditor(null)}><Plus size={16} />新建映射</button>
    </>} />

    {error && <ErrorState message={error} onRetry={() => void load()} />}
    {success && <div className="success-banner" role="status"><Tags size={17} /><span>{success}</span></div>}
    <form className="mapping-toolbar" onSubmit={search}>
      <label><span>平台</span><select value={platform} onChange={(event) => { setPlatform(event.target.value); setPage(1); }}><option value="shein">SHEIN</option><option value="temu">Temu</option></select></label>
      <label className="mapping-query"><span>平台 SKU / 仓库 SKU</span><div><Search size={16} /><input value={queryInput} onChange={(event) => setQueryInput(event.target.value)} placeholder="输入关键词" /></div></label>
      <button className="secondary-button" type="submit">查询</button>
      <div className="mapping-credentials"><KeyRound size={16} /><input aria-label="维护账号" autoComplete="username" value={credentials.username} onChange={(event) => setCredentials({ ...credentials, username: event.target.value })} placeholder="维护账号" /><input aria-label="维护密码" type="password" autoComplete="current-password" value={credentials.password} onChange={(event) => setCredentials({ ...credentials, password: event.target.value })} placeholder="维护密码" /></div>
    </form>

    {loading && !data ? <LoadingState label="正在加载 SKU 映射" /> : data?.records.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table mapping-table">
      <thead><tr><th>平台</th><th>平台 SKU</th><th>仓库 SKU 配方</th><th>来源</th><th>更新时间</th><th>操作</th></tr></thead>
      <tbody>{data.records.map((mapping) => <tr key={`${mapping.platform}:${mapping.platform_sku}`}>
        <td><span className="mapping-platform">{mapping.platform.toUpperCase()}</span></td>
        <td><strong>{mapping.platform_sku}</strong></td>
        <td><div className="pairing-members">{mapping.items.map((item) => <span key={item.warehouse_sku}><b>{item.warehouse_sku}</b><em>× {item.quantity}</em>{item.product_name && <small>{item.product_name}</small>}{!item.spec_complete && <small className="mapping-spec-warning">规格未完成</small>}</span>)}</div></td>
        <td>{mapping.source || "manual"}</td><td>{dateTime(mapping.updated_at)}</td>
        <td><div className="mapping-actions"><button className="icon-button bordered" type="button" onClick={() => setEditor(mapping)} title="编辑映射"><Pencil size={15} /></button><button className="icon-button danger-button" type="button" onClick={() => void remove(mapping)} disabled={deleting === mapping.platform_sku} title="删除映射">{deleting === mapping.platform_sku ? <LoaderCircle className="spin" size={15} /> : <Trash2 size={15} />}</button></div></td>
      </tr>)}</tbody>
    </table></div></div> : !loading && !error && <EmptyState label={query ? "没有符合条件的 SKU 映射" : "当前平台没有 SKU 映射"} />}
    {data && data.total > 0 && <Pagination page={data.page} pages={data.pages} total={data.total} onChange={setPage} />}
    {editor !== undefined && <MappingEditor platform={platform} mapping={editor} credentials={credentials} onClose={() => setEditor(undefined)} onSaved={async (sku) => { setEditor(undefined); setSuccess(`映射“${sku}”已保存`); await load(); }} />}
  </>;
}

function MappingEditor({ platform, mapping, credentials, onClose, onSaved }: { platform: string; mapping: PlatformSKUMapping | null; credentials: ConsoleCredentials; onClose: () => void; onSaved: (sku: string) => Promise<void> }) {
  const nextID = useRef((mapping?.items.length || 0) + 1);
  const [platformSKU, setPlatformSKU] = useState(mapping?.platform_sku || "");
  const [items, setItems] = useState<ItemDraft[]>(mapping?.items.map((item, index) => ({ id: index + 1, warehouseSKU: item.warehouse_sku, quantity: String(item.quantity) })) || [{ id: 1, warehouseSKU: "", quantity: "1" }]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const updateItem = (id: number, patch: Partial<ItemDraft>) => setItems((current) => current.map((item) => item.id === id ? { ...item, ...patch } : item));
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError("");
    if (!credentials.username || !credentials.password) { setError("请先在列表上方填写维护账号和密码"); return; }
    setSaving(true);
    try {
      const recipe = items.map((item) => ({ warehouse_sku: item.warehouseSKU.trim(), quantity: Number(item.quantity) }));
      if (new Set(recipe.map((item) => item.warehouse_sku)).size !== recipe.length) throw new Error("仓库 SKU 不能重复");
      await api.savePlatformSKUMapping({ platform: mapping?.platform || platform, platform_sku: platformSKU.trim(), enabled: true, items: recipe }, credentials);
      await onSaved(platformSKU.trim());
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法保存 SKU 映射");
    } finally { setSaving(false); }
  };
  return <div className="modal-backdrop" onMouseDown={(event) => { if (!saving && event.target === event.currentTarget) onClose(); }}>
    <section className="modal pairing-editor" role="dialog" aria-modal="true" aria-labelledby="mapping-editor-title">
      <header><div><h2 id="mapping-editor-title">{mapping ? "编辑 SKU 映射" : "新建 SKU 映射"}</h2><p>{(mapping?.platform || platform).toUpperCase()}</p></div><button className="icon-button" type="button" onClick={onClose} disabled={saving} title="关闭"><X size={18} /></button></header>
      <form onSubmit={(event) => void submit(event)}>{error && <ErrorState message={error} />}
        <div className="form-grid"><label><span>平台 SKU</span><input required maxLength={255} autoFocus disabled={Boolean(mapping)} value={platformSKU} onChange={(event) => setPlatformSKU(event.target.value)} /></label></div>
        <div className="pairing-item-heading"><div><strong>仓库 SKU 配方</strong><span>{items.length} / 20</span></div><button className="secondary-button" type="button" onClick={() => setItems((current) => [...current, { id: ++nextID.current, warehouseSKU: "", quantity: "1" }])} disabled={items.length >= 20}><Plus size={15} />添加 SKU</button></div>
        <div className="pairing-item-list">{items.map((item, index) => <div className="pairing-item-row" key={item.id}><span>{index + 1}</span><label><span>仓库 SKU</span><input required maxLength={255} value={item.warehouseSKU} onChange={(event) => updateItem(item.id, { warehouseSKU: event.target.value })} /></label><label className="pairing-quantity"><span>数量</span><input required type="number" min="1" max="999999" step="1" value={item.quantity} onChange={(event) => updateItem(item.id, { quantity: event.target.value })} /></label><button className="icon-button danger-button" type="button" onClick={() => setItems((current) => current.filter((candidate) => candidate.id !== item.id))} disabled={items.length === 1} title="移除 SKU"><Trash2 size={15} /></button></div>)}</div>
        <footer><button className="secondary-button" type="button" onClick={onClose} disabled={saving}>取消</button><button className="primary-button" type="submit" disabled={saving}>{saving ? <LoaderCircle className="spin" size={16} /> : <Tags size={16} />}{saving ? "正在保存" : "保存映射"}</button></footer>
      </form>
    </section>
  </div>;
}
