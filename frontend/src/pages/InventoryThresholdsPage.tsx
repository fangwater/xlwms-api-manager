import { Pencil, RefreshCw, RotateCcw, Save, Search, ShieldAlert, X } from "lucide-react";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, dateTime } from "../components/Common";
import type { InventoryThresholdPage, InventoryThresholds, ShopInventoryThresholds, SKUInventoryThreshold } from "../types";

type ThresholdForm = {
  east_threshold: string;
  west_threshold: string;
  total_threshold: string;
};

const emptyForm: ThresholdForm = { east_threshold: "", west_threshold: "", total_threshold: "" };
const SHOP_STORAGE_KEY = "xlwms-threshold-shop";

function formFrom(thresholds: InventoryThresholds): ThresholdForm {
  return {
    east_threshold: String(thresholds.east_threshold),
    west_threshold: String(thresholds.west_threshold),
    total_threshold: String(thresholds.total_threshold)
  };
}

function payloadFrom(form: ThresholdForm): InventoryThresholds {
  return {
    east_threshold: Number(form.east_threshold),
    west_threshold: Number(form.west_threshold),
    total_threshold: Number(form.total_threshold)
  };
}

function stockClass(available: number, threshold: number): string {
  return available <= threshold ? "danger" : "positive";
}

function shopKey(platform: string, shopCode: string): string {
  return `${platform}:${shopCode}`;
}

function parseShopKey(value: string): { platform?: string; shop?: string } {
  const [platform, shop] = value.split(":");
  if (!platform || !shop) return {};
  return { platform, shop };
}

function shopLabel(item: Pick<ShopInventoryThresholds, "platform" | "shop_name" | "shop_code">): string {
  const platform = item.platform === "shein" ? "SHEIN" : "Temu";
  return `${platform} · ${item.shop_name || item.shop_code}`;
}

function sourceLabel(item: SKUInventoryThreshold, scoped: boolean): string {
  if (item.source === "shop_sku" || (scoped && item.customized)) return "店铺 SKU 单独设置";
  if (item.source === "shop_default") return "店铺默认";
  if (item.source === "sku" || item.customized) return "SKU 单独设置";
  return scoped ? "店铺默认" : "全局默认";
}

export default function InventoryThresholdsPage() {
  const [data, setData] = useState<InventoryThresholdPage | null>(null);
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [shop, setShop] = useState(() => localStorage.getItem(SHOP_STORAGE_KEY) || "");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [defaultForm, setDefaultForm] = useState<ThresholdForm>(emptyForm);
  const [savingDefaults, setSavingDefaults] = useState(false);
  const [editing, setEditing] = useState<SKUInventoryThreshold | null>(null);
  const [form, setForm] = useState<ThresholdForm>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [modalError, setModalError] = useState("");

  const selected = parseShopKey(shop);
  const scoped = Boolean(selected.platform && selected.shop);
  const selectedShop = useMemo(
    () => data?.shops.find((item) => shopKey(item.platform, item.shop_code) === shop) || null,
    [data, shop]
  );

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const result = await api.inventoryThresholds({ q: search, page, pageSize: 50, platform: selected.platform, shop: selected.shop });
      setData(result);
      setDefaultForm(formFrom(result.default_thresholds));
      if (shop && !result.shops.some((item) => shopKey(item.platform, item.shop_code) === shop)) {
        setShop("");
        localStorage.removeItem(SHOP_STORAGE_KEY);
      }
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法加载库存安全线");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 180);
    return () => window.clearTimeout(timer);
  }, [search, page, shop]);

  const saveDefaults = async (event: FormEvent) => {
    event.preventDefault();
    setSavingDefaults(true);
    setError("");
    setNotice("");
    try {
      await api.updateInventoryThresholdDefaults(payloadFrom(defaultForm), selected);
      setNotice(scoped ? `${shopLabel(selectedShop || { platform: selected.platform || "", shop_name: "", shop_code: selected.shop || "" })} 的店铺默认安全线已更新` : "全局默认安全线已更新，未单独设置的店铺和 SKU 将立即使用新值");
      await load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "默认安全线保存失败");
    } finally {
      setSavingDefaults(false);
    }
  };

  const resetShopDefaults = async () => {
    if (!selected.platform || !selected.shop) return;
    setSavingDefaults(true);
    setError("");
    setNotice("");
    try {
      await api.resetShopInventoryThresholds({ platform: selected.platform, shop: selected.shop });
      setNotice(`${shopLabel(selectedShop || { platform: selected.platform, shop_name: "", shop_code: selected.shop })} 已恢复全局默认安全线`);
      await load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "恢复店铺默认失败");
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
      await api.updateSKUInventoryThreshold(editing.warehouse_sku, payloadFrom(form), selected);
      setNotice(`${editing.warehouse_sku} 的库存安全线已保存`);
      setEditing(null);
      await load();
    } catch (reason) {
      setModalError(reason instanceof Error ? reason.message : "SKU 安全线保存失败");
    } finally {
      setSaving(false);
    }
  };

  const resetSKU = async () => {
    if (!editing?.customized) return;
    setSaving(true);
    setModalError("");
    try {
      await api.resetSKUInventoryThreshold(editing.warehouse_sku, selected);
      setNotice(`${editing.warehouse_sku} 已恢复${scoped ? "店铺默认" : "全局默认"}安全线`);
      setEditing(null);
      await load();
    } catch (reason) {
      setModalError(reason instanceof Error ? reason.message : "恢复默认失败");
    } finally {
      setSaving(false);
    }
  };

  return <>
    <PageHeader title="库存安全线" subtitle="仓库为每个店铺维护独立的自动发货转人工阈值，未单独设置的店铺使用全局默认" />
    <div className="filter-bar threshold-shop-bar">
      <label className="select-field"><select aria-label="选择店铺" value={shop} onChange={(event) => { const value = event.target.value; setShop(value); setPage(1); if (value) localStorage.setItem(SHOP_STORAGE_KEY, value); else localStorage.removeItem(SHOP_STORAGE_KEY); }}><option value="">全局默认</option>{data?.shops.map((item) => <option value={shopKey(item.platform, item.shop_code)} key={shopKey(item.platform, item.shop_code)}>{shopLabel(item)}</option>)}</select></label>
      <span className="table-note">{scoped ? "当前正在编辑所选店铺的安全线" : "当前正在编辑所有店铺共用的全局默认"}</span>
    </div>
    <form className="threshold-defaults" onSubmit={saveDefaults}>
      <div className="threshold-default-title"><ShieldAlert size={19}/><div><strong>{scoped ? "店铺默认" : "全局默认"}</strong><small>{scoped ? `${shopLabel(selectedShop || { platform: selected.platform || "", shop_name: "", shop_code: selected.shop || "" })} 未单独设置 SKU 时使用` : "店铺未单独设置时使用"}</small></div></div>
      <label><span>美东低于或等于</span><input required type="number" min="0" step="1" value={defaultForm.east_threshold} onChange={(event) => setDefaultForm({...defaultForm, east_threshold: event.target.value})}/></label>
      <label><span>美西低于或等于</span><input required type="number" min="0" step="1" value={defaultForm.west_threshold} onChange={(event) => setDefaultForm({...defaultForm, west_threshold: event.target.value})}/></label>
      <label><span>总库存低于或等于</span><input required type="number" min="0" step="1" value={defaultForm.total_threshold} onChange={(event) => setDefaultForm({...defaultForm, total_threshold: event.target.value})}/></label>
      <div className="threshold-default-actions">
        {scoped && selectedShop?.customized && <button className="secondary-button reset-threshold-button" type="button" onClick={() => void resetShopDefaults()} disabled={savingDefaults}><RotateCcw size={15}/>恢复全局默认</button>}
        <button className="primary-button" type="submit" disabled={savingDefaults}><Save size={15}/>{savingDefaults ? "保存中" : scoped ? "保存店铺默认" : "保存默认"}</button>
      </div>
    </form>
    {notice && <div className="success-banner">{notice}</div>}
    <div className="filter-bar">
      <label className="search-field"><Search size={17}/><input value={search} onChange={(event) => { setSearch(event.target.value); setPage(1); }} placeholder="搜索仓库 SKU 或产品名称" /></label>
      <button className="icon-button bordered" onClick={() => void load()} title="刷新"><RefreshCw size={17}/></button>
    </div>
    {error && <ErrorState message={error} onRetry={() => void load()} />}
    {loading ? <LoadingState label="正在加载库存安全线" /> : data?.records.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table threshold-table"><thead><tr><th>仓库 SKU / 产品</th><th>美东库存</th><th>美西库存</th><th>总库存</th><th>美东阈值</th><th>美西阈值</th><th>总库存阈值</th><th>规则来源</th><th>操作</th></tr></thead><tbody>{data.records.map((item) => <tr key={item.warehouse_sku}><td><div className="primary-cell"><strong>{item.warehouse_sku}</strong><small>{item.product_name || "-"}</small></div></td><td className={stockClass(item.east_available, item.east_threshold)}>{item.east_available}</td><td className={stockClass(item.west_available, item.west_threshold)}>{item.west_available}</td><td className={stockClass(item.total_available, item.total_threshold)}>{item.total_available}</td><td>{item.east_threshold}</td><td>{item.west_threshold}</td><td>{item.total_threshold}</td><td><div className="threshold-source"><span className={`status-badge ${item.customized ? "running" : ""}`}>{sourceLabel(item, scoped)}</span><small>{item.inventory_at ? `库存 ${dateTime(item.inventory_at)}` : "暂无库存快照"}</small></div></td><td><button className="icon-button bordered" onClick={() => openEditor(item)} title="编辑安全线"><Pencil size={15}/></button></td></tr>)}</tbody></table></div></div> : <EmptyState label="当前没有仓库 SKU" />}
    {data && data.total > 0 && <Pagination page={data.page} pages={data.pages} total={data.total} onChange={setPage} />}
    {editing && <div className="modal-backdrop" role="presentation"><section className="modal threshold-modal" role="dialog" aria-modal="true" aria-labelledby="threshold-modal-title"><header><div><h2 id="threshold-modal-title">编辑库存安全线</h2><p>{editing.warehouse_sku} · {editing.product_name || "未命名产品"}{scoped ? ` · ${shopLabel(selectedShop || { platform: selected.platform || "", shop_name: "", shop_code: selected.shop || "" })}` : ""}</p></div><button className="icon-button" onClick={() => setEditing(null)} title="关闭"><X size={19}/></button></header><form onSubmit={saveSKU}>{modalError && <div className="error-banner">{modalError}</div>}<div className="form-grid threshold-form"><label><span>美东库存低于或等于</span><input required type="number" min="0" step="1" value={form.east_threshold} onChange={(event) => setForm({...form, east_threshold: event.target.value})}/></label><label><span>美西库存低于或等于</span><input required type="number" min="0" step="1" value={form.west_threshold} onChange={(event) => setForm({...form, west_threshold: event.target.value})}/></label><label className="full"><span>美东 + 美西总库存低于或等于</span><input required type="number" min="0" step="1" value={form.total_threshold} onChange={(event) => setForm({...form, total_threshold: event.target.value})}/></label></div><footer>{editing.customized && <button type="button" className="secondary-button reset-threshold-button" onClick={() => void resetSKU()} disabled={saving}><RotateCcw size={15}/>{scoped ? "恢复店铺默认" : "恢复全局默认"}</button>}<button type="button" className="secondary-button" onClick={() => setEditing(null)}>取消</button><button type="submit" className="primary-button" disabled={saving}>{saving ? "保存中" : "保存安全线"}</button></footer></form></section></div>}
  </>;
}
