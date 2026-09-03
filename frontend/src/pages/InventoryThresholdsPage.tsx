import { Pencil, RefreshCw, RotateCcw, Save, Search, ShieldAlert, X } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, dateTime } from "../components/Common";
import type { InventoryThresholdPage, InventoryThresholds, SKUInventoryThreshold } from "../types";

type ThresholdForm = { total_threshold: string };

const emptyForm: ThresholdForm = { total_threshold: "" };
const PLATFORM_STORAGE_KEY = "xlwms-threshold-platform";
const platformOptions = ["temu", "shein"];

function platformLabel(platform: string): string {
  return platform === "shein" ? "SHEIN" : "Temu";
}

function formFrom(thresholds: InventoryThresholds): ThresholdForm {
  return { total_threshold: String(thresholds.total_threshold) };
}

function payloadFrom(form: ThresholdForm): InventoryThresholds {
  return { east_threshold: 0, west_threshold: 0, total_threshold: Number(form.total_threshold) };
}

function stockClass(available: number, threshold: number): string {
  return available < threshold ? "danger" : "positive";
}

function sourceLabel(item: SKUInventoryThreshold): string {
  return item.source === "platform_sku" || item.customized ? "平台 SKU 单独设置" : "平台默认";
}

export default function InventoryThresholdsPage() {
  const [platform, setPlatform] = useState(() => localStorage.getItem(PLATFORM_STORAGE_KEY) || "temu");
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [data, setData] = useState<InventoryThresholdPage | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [savingDefaults, setSavingDefaults] = useState(false);
  const [defaultForm, setDefaultForm] = useState<ThresholdForm>(emptyForm);
  const [editing, setEditing] = useState<SKUInventoryThreshold | null>(null);
  const [form, setForm] = useState<ThresholdForm>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [modalError, setModalError] = useState("");

  useEffect(() => {
    const timer = window.setTimeout(() => { setDebouncedQuery(query.trim()); setPage(1); }, 250);
    return () => window.clearTimeout(timer);
  }, [query]);

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const result = await api.inventoryThresholds({ q: debouncedQuery, page, pageSize: 30, platform });
      setData(result);
      setDefaultForm(formFrom(result.default_thresholds));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "库存安全线加载失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, [page, debouncedQuery, platform]);

  const selectPlatform = (value: string) => {
    setPlatform(value);
    setPage(1);
    localStorage.setItem(PLATFORM_STORAGE_KEY, value);
  };

  const saveDefaults = async (event: FormEvent) => {
    event.preventDefault();
    setSavingDefaults(true);
    setError("");
    try {
      await api.updateInventoryThresholdDefaults(platform, payloadFrom(defaultForm));
      await load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "平台默认安全线保存失败");
    } finally {
      setSavingDefaults(false);
    }
  };

  const openEditor = (item: SKUInventoryThreshold) => {
    setEditing(item);
    setForm(formFrom(item));
    setModalError("");
  };

  const saveSKU = async (event: FormEvent) => {
    event.preventDefault();
    if (!editing) return;
    setSaving(true);
    setModalError("");
    try {
      await api.updateSKUInventoryThreshold(platform, editing.warehouse_sku, payloadFrom(form));
      setEditing(null);
      await load();
    } catch (reason) {
      setModalError(reason instanceof Error ? reason.message : "SKU 安全线保存失败");
    } finally {
      setSaving(false);
    }
  };

  const resetSKU = async () => {
    if (!editing) return;
    setSaving(true);
    setModalError("");
    try {
      await api.resetSKUInventoryThreshold(platform, editing.warehouse_sku);
      setEditing(null);
      await load();
    } catch (reason) {
      setModalError(reason instanceof Error ? reason.message : "SKU 安全线恢复失败");
    } finally {
      setSaving(false);
    }
  };

  return <>
    <PageHeader title="库存安全线" subtitle="按平台和仓库 SKU 维护四仓总库存转人工阈值" />
    <div className="filter-bar threshold-shop-bar">
      <label className="select-field"><select aria-label="选择平台" value={platform} onChange={(event) => selectPlatform(event.target.value)}>{platformOptions.map((item) => <option value={item} key={item}>{platformLabel(item)}</option>)}</select></label>
      <span className="table-note">当前正在编辑 {platformLabel(platform)} 全部店铺共用的安全线</span>
    </div>
    <form className="threshold-defaults" onSubmit={saveDefaults}>
      <div className="threshold-default-title"><ShieldAlert size={19}/><div><strong>平台默认</strong><small>{platformLabel(platform)} 未单独设置 SKU 时使用</small></div></div>
      <label><span>四仓总库存低于</span><input required type="number" min="0" step="1" value={defaultForm.total_threshold} onChange={(event) => setDefaultForm({ total_threshold: event.target.value })}/></label>
      <div className="threshold-default-actions"><button className="primary-button" type="submit" disabled={savingDefaults}><Save size={15}/>{savingDefaults ? "保存中" : "保存平台默认"}</button></div>
    </form>
    <div className="filter-bar">
      <label className="search-field"><Search size={16}/><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索仓库 SKU 或产品名称" /></label>
      <button className="icon-button bordered" onClick={() => void load()} title="刷新"><RefreshCw size={17}/></button>
    </div>
    {error && <ErrorState message={error} onRetry={() => void load()} />}
    {loading ? <LoadingState label="正在加载库存安全线" /> : data?.records.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table threshold-table"><thead><tr><th>仓库 SKU / 产品</th><th>美东库存</th><th>美西库存</th><th>四仓总库存</th><th>转人工阈值</th><th>规则来源</th><th>操作</th></tr></thead><tbody>{data.records.map((item) => <tr key={item.warehouse_sku}><td><div className="primary-cell"><strong>{item.warehouse_sku}</strong><small>{item.product_name || "-"}</small></div></td><td>{item.east_available}</td><td>{item.west_available}</td><td className={stockClass(item.total_available, item.total_threshold)}>{item.total_available}</td><td>{item.total_threshold}</td><td><div className="threshold-source"><span className={`status-badge ${item.customized ? "running" : ""}`}>{sourceLabel(item)}</span><small>{item.inventory_at ? `库存 ${dateTime(item.inventory_at)}` : "暂无库存快照"}</small></div></td><td><button className="icon-button bordered" onClick={() => openEditor(item)} title="编辑安全线"><Pencil size={15}/></button></td></tr>)}</tbody></table></div></div> : <EmptyState label="当前没有仓库 SKU" />}
    {data && data.total > 0 && <Pagination page={data.page} pages={data.pages} total={data.total} onChange={setPage} />}
    {editing && <div className="modal-backdrop" role="presentation"><section className="modal threshold-modal" role="dialog" aria-modal="true" aria-labelledby="threshold-modal-title"><header><div><h2 id="threshold-modal-title">编辑库存安全线</h2><p>{editing.warehouse_sku} · {editing.product_name || "未命名产品"} · {platformLabel(platform)}</p></div><button className="icon-button" onClick={() => setEditing(null)} title="关闭"><X size={19}/></button></header><form onSubmit={saveSKU}>{modalError && <div className="error-banner">{modalError}</div>}<div className="form-grid threshold-form"><label className="full"><span>四仓总库存低于</span><input required type="number" min="0" step="1" value={form.total_threshold} onChange={(event) => setForm({ total_threshold: event.target.value })}/></label></div><footer>{editing.customized && <button type="button" className="secondary-button reset-threshold-button" onClick={() => void resetSKU()} disabled={saving}><RotateCcw size={15}/>恢复平台默认</button>}<button type="button" className="secondary-button" onClick={() => setEditing(null)}>取消</button><button type="submit" className="primary-button" disabled={saving}>{saving ? "保存中" : "保存安全线"}</button></footer></form></section></div>}
  </>;
}
