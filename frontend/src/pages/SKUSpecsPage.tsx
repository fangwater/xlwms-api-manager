import { AlertTriangle, PackageCheck, Pencil, Plus, RefreshCw, Search, X } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, dateTime } from "../components/Common";
import type { PageData, WarehouseSKUSpec } from "../types";

type SpecForm = {
  warehouse_sku: string;
  product_name: string;
  length_cm: string;
  width_cm: string;
  height_cm: string;
  weight_kg: string;
  note: string;
  enabled: boolean;
};

const emptyForm: SpecForm = { warehouse_sku: "", product_name: "", length_cm: "", width_cm: "", height_cm: "", weight_kg: "", note: "", enabled: true };
const missingLabels: Record<string, string> = { warehouse_sku: "未建档", enabled: "已停用", length_cm: "缺长", width_cm: "缺宽", height_cm: "缺高", weight_kg: "缺重量" };

function numberOrNull(value: string): number | null {
  const normalized = value.trim();
  return normalized ? Number(normalized) : null;
}

export default function SKUSpecsPage() {
  const [data, setData] = useState<PageData<WarehouseSKUSpec> | null>(null);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("missing");
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState("");
  const [modalError, setModalError] = useState("");
  const [form, setForm] = useState<SpecForm>(emptyForm);

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      setData(await api.warehouseSKUSpecs({ q: search, status, page, pageSize: 50 }));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法加载仓库 SKU 规格");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 180);
    return () => window.clearTimeout(timer);
  }, [search, status, page]);

  const create = () => { setEditing(false); setForm(emptyForm); setModalError(""); setNotice(""); setOpen(true); };
  const edit = (item: WarehouseSKUSpec) => {
    setEditing(true);
    setModalError("");
    setNotice("");
    setForm({ warehouse_sku: item.warehouse_sku, product_name: item.product_name || "", length_cm: item.length_cm?.toString() || "", width_cm: item.width_cm?.toString() || "", height_cm: item.height_cm?.toString() || "", weight_kg: item.weight_kg?.toString() || "", note: item.note || "", enabled: item.enabled });
    setOpen(true);
  };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    setModalError("");
    setNotice("");
    const payload = {
      warehouse_sku: form.warehouse_sku,
      product_name: form.product_name,
      length_cm: numberOrNull(form.length_cm), width_cm: numberOrNull(form.width_cm),
      height_cm: numberOrNull(form.height_cm), weight_kg: numberOrNull(form.weight_kg),
      note: form.note, enabled: form.enabled
    };
    try {
      if (editing) await api.updateWarehouseSKUSpec(form.warehouse_sku, payload);
      else await api.saveWarehouseSKUSpec(payload);
      setOpen(false);
      setNotice(`仓库 SKU ${form.warehouse_sku} 已保存`);
      await load();
    } catch (reason) {
      setModalError(reason instanceof Error ? reason.message : "保存失败");
    } finally {
      setSaving(false);
    }
  };

  return <>
    <PageHeader title="仓库 SKU 规格" subtitle="Temu 发货包裹的唯一重量与尺寸来源" actions={<button className="primary-button" onClick={create}><Plus size={16}/>新建规格</button>} />
    <div className="sku-spec-summary">
      <div><AlertTriangle size={18}/><span>刚性规则</span><strong>缺任一字段即阻断发货</strong></div>
      <div><PackageCheck size={18}/><span>统一单位</span><strong>厘米 / 千克</strong></div>
    </div>
    {notice && <div className="success-banner">{notice}</div>}
    <div className="filter-bar">
      <label className="search-field"><Search size={17}/><input value={search} onChange={(event) => { setSearch(event.target.value); setPage(1); }} placeholder="搜索仓库 SKU 或产品名称" /></label>
      <label className="select-field"><select value={status} onChange={(event) => { setStatus(event.target.value); setPage(1); }}><option value="missing">仅缺失</option><option value="complete">仅完整</option><option value="disabled">已停用</option><option value="all">全部</option></select></label>
      <button className="icon-button bordered" onClick={() => void load()} title="刷新"><RefreshCw size={17}/></button>
    </div>
    {error && <ErrorState message={error} onRetry={() => void load()} />}
    {loading ? <LoadingState label="正在加载 SKU 规格" /> : data?.records.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table sku-spec-table"><thead><tr><th>仓库 SKU / 产品</th><th>完整性</th><th>重量 kg</th><th>长 × 宽 × 高 cm</th><th>来源</th><th>更新时间</th><th>操作</th></tr></thead><tbody>{data.records.map((item) => <tr key={item.warehouse_sku}><td><div className="primary-cell"><strong>{item.warehouse_sku}</strong><small>{item.product_name || "-"}</small></div></td><td>{item.complete ? <span className="stock-state in-stock">完整</span> : <div className="missing-fields">{item.missing_fields.map((field) => <span key={field}>{missingLabels[field] || field}</span>)}</div>}</td><td>{item.weight_kg ?? "-"}</td><td>{item.length_cm ?? "-"} × {item.width_cm ?? "-"} × {item.height_cm ?? "-"}</td><td>{item.source === "manual" ? "人工维护" : item.source === "shein_import" ? "SHEIN 迁入" : "库存发现"}</td><td>{dateTime(item.updated_at)}</td><td><button className="secondary-button sku-edit-button" onClick={() => edit(item)} title="编辑规格"><Pencil size={14}/>编辑</button></td></tr>)}</tbody></table></div></div> : <EmptyState label={status === "missing" ? "没有缺失规格的仓库 SKU" : "当前筛选暂无 SKU 规格"} />}
    {data && data.total > 0 && <Pagination page={data.page} pages={data.pages} total={data.total} onChange={setPage} />}
    {open && <div className="modal-backdrop" role="presentation"><section className="modal" role="dialog" aria-modal="true" aria-labelledby="sku-spec-modal-title"><header><div><h2 id="sku-spec-modal-title">{editing ? "编辑仓库 SKU 规格" : "新建仓库 SKU 规格"}</h2><p>规格必须来自实际单件包装测量</p></div><button className="icon-button" onClick={() => setOpen(false)} title="关闭"><X size={19}/></button></header><form onSubmit={submit}>{modalError && <div className="error-banner">{modalError}</div>}<div className="form-grid"><label className="full"><span>仓库 SKU</span><input required disabled={editing} value={form.warehouse_sku} onChange={(event) => setForm({...form, warehouse_sku: event.target.value})}/></label><label className="full"><span>产品名称</span><input value={form.product_name} onChange={(event) => setForm({...form, product_name: event.target.value})}/></label><label><span>重量 kg</span><input type="number" min="0.001" step="0.001" value={form.weight_kg} onChange={(event) => setForm({...form, weight_kg: event.target.value})}/></label><label><span>长度 cm</span><input type="number" min="0.01" step="0.01" value={form.length_cm} onChange={(event) => setForm({...form, length_cm: event.target.value})}/></label><label><span>宽度 cm</span><input type="number" min="0.01" step="0.01" value={form.width_cm} onChange={(event) => setForm({...form, width_cm: event.target.value})}/></label><label><span>高度 cm</span><input type="number" min="0.01" step="0.01" value={form.height_cm} onChange={(event) => setForm({...form, height_cm: event.target.value})}/></label><label className="full"><span>备注</span><input value={form.note} onChange={(event) => setForm({...form, note: event.target.value})}/></label></div><label className="checkbox-row"><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({...form, enabled: event.target.checked})}/><span>允许用于自动发货</span></label><footer><button type="button" className="secondary-button" onClick={() => setOpen(false)}>取消</button><button type="submit" className="primary-button" disabled={saving}>{saving ? "保存中" : "保存规格"}</button></footer></form></section></div>}
  </>;
}
