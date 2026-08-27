import { ArrowRightLeft, Check, Edit3, LoaderCircle, PackageOpen, Save, Search, Trash2, X } from "lucide-react";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, dateTime, number } from "../components/Common";
import type { PackingPlan, SKUCombination, SKUCombinationPayload, WarehouseSKUSpec } from "../types";

export type CombinationSelectedSKU = { spec: WarehouseSKUSpec; quantity: number };

export function PackingCombinationsPanel({ refreshToken, onEdit }: { refreshToken: number; onEdit: (item: SKUCombination) => void }) {
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<"all" | "active" | "disabled">("all");
  const [items, setItems] = useState<SKUCombination[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      setItems(await api.skuCombinations({ q: query.trim(), status }));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法加载组合");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 180);
    return () => window.clearTimeout(timer);
  }, [query, status, refreshToken]);

  const remove = async (item: SKUCombination) => {
    if (!window.confirm(`删除组合“${item.name}”？`)) return;
    try {
      await api.deleteSKUCombination(item.id);
      await load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法删除组合");
    }
  };

  return <section className="combination-library">
    <div className="combination-filter-bar">
      <label className="search-field"><Search size={16}/><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索组合、SKU 或替代目标" /></label>
      <div className="segmented-tabs compact-tabs" role="tablist" aria-label="组合状态">
        {(["all", "active", "disabled"] as const).map((value) => <button type="button" role="tab" aria-selected={status === value} className={status === value ? "active" : ""} onClick={() => setStatus(value)} key={value}>{value === "all" ? "全部" : value === "active" ? "启用" : "停用"}</button>)}
      </div>
    </div>
    {error && <ErrorState message={error} onRetry={() => void load()} />}
    {loading ? <LoadingState label="正在加载组合" /> : items.length ? <div className="combination-grid">
      {items.map((item) => <article className="combination-card" key={item.id}>
        <header>
          <div><strong>{item.name}</strong><span className={`status-badge ${item.enabled ? "success" : ""}`}>{item.enabled ? "启用" : "停用"}</span>{item.corrected && <span className="combination-corrected">已修正</span>}</div>
          <small>{dateTime(item.updated_at)}</small>
        </header>
        <div className="combination-expression">{item.items.map((member) => <span key={member.warehouse_sku}><b>{member.warehouse_sku}</b><i>× {member.quantity}</i></span>)}</div>
        <dl>
          <div><dt>发货替代</dt><dd>{item.substitute_for_sku ? <><b>{item.substitute_for_sku}</b><ArrowRightLeft size={13}/></> : "未绑定"}</dd></div>
          <div><dt>修正包裹</dt><dd>{number(item.length_cm, 2)} × {number(item.width_cm, 2)} × {number(item.height_cm, 2)} cm</dd></div>
          <div><dt>修正重量</dt><dd>{number(item.weight_kg, 3)} kg</dd></div>
        </dl>
        {item.note && <p>{item.note}</p>}
        <footer>
          <button type="button" className="secondary-button" onClick={() => onEdit(item)}><Edit3 size={14}/>编辑组合</button>
          <button type="button" className="icon-button danger-button" onClick={() => void remove(item)} title="删除组合"><Trash2 size={15}/></button>
        </footer>
      </article>)}
    </div> : <EmptyState label="暂无已保存组合" />}
  </section>;
}

type CombinationEditorProps = {
  selected: CombinationSelectedSKU[];
  plan: PackingPlan | null;
  existing: SKUCombination | null;
  targetOptions: WarehouseSKUSpec[];
  onClose: () => void;
  onSaved: (item: SKUCombination) => void;
};

export function SKUCombinationEditor({ selected, plan, existing, targetOptions, onClose, onSaved }: CombinationEditorProps) {
  const calculated = useMemo(() => {
    if (plan) return {
      length_cm: plan.packages.reduce((total, item) => total + item.dimensions.length_cm, 0)
        + plan.unfit_items.reduce((total, item) => total + item.dimensions.length_cm, 0),
      width_cm: Math.max(0, ...plan.packages.map((item) => item.dimensions.width_cm), ...plan.unfit_items.map((item) => item.dimensions.width_cm)),
      height_cm: Math.max(0, ...plan.packages.map((item) => item.dimensions.height_cm), ...plan.unfit_items.map((item) => item.dimensions.height_cm)),
      weight_kg: plan.summary.total_weight_kg,
    };
    return {
      length_cm: existing?.calculated_length_cm ?? existing?.length_cm ?? 0,
      width_cm: existing?.calculated_width_cm ?? existing?.width_cm ?? 0,
      height_cm: existing?.calculated_height_cm ?? existing?.height_cm ?? 0,
      weight_kg: existing?.calculated_weight_kg ?? existing?.weight_kg ?? 0,
    };
  }, [existing, plan]);
  const [name, setName] = useState(existing?.name ?? selected.map((item) => `${item.spec.warehouse_sku}×${item.quantity}`).join(" + "));
  const [substituteForSKU, setSubstituteForSKU] = useState(existing?.substitute_for_sku ?? "");
  const [lengthCM, setLengthCM] = useState(existing?.length_cm ?? calculated.length_cm);
  const [widthCM, setWidthCM] = useState(existing?.width_cm ?? calculated.width_cm);
  const [heightCM, setHeightCM] = useState(existing?.height_cm ?? calculated.height_cm);
  const [weightKG, setWeightKG] = useState(existing?.weight_kg ?? calculated.weight_kg);
  const [note, setNote] = useState(existing?.note ?? "");
  const [enabled, setEnabled] = useState(existing?.enabled ?? true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError("");
    const payload: SKUCombinationPayload = {
      name: name.trim(), substitute_for_sku: substituteForSKU.trim() || undefined,
      length_cm: lengthCM, width_cm: widthCM, height_cm: heightCM, weight_kg: weightKG,
      calculated_length_cm: calculated.length_cm, calculated_width_cm: calculated.width_cm,
      calculated_height_cm: calculated.height_cm, calculated_weight_kg: calculated.weight_kg,
      note: note.trim(), enabled,
      items: selected.map((item) => ({ warehouse_sku: item.spec.warehouse_sku, quantity: item.quantity })),
    };
    setSaving(true);
    try {
      onSaved(existing ? await api.updateSKUCombination(existing.id, payload) : await api.createSKUCombination(payload));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法保存组合");
    } finally {
      setSaving(false);
    }
  };

  return <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
    <div className="modal combination-modal" role="dialog" aria-modal="true" aria-labelledby="combination-editor-title">
      <header><div><h2 id="combination-editor-title">{existing ? "修改组合" : "保存组合"}</h2><p>{selected.reduce((total, item) => total + item.quantity, 0)} 件 · {selected.length} 个 SKU</p></div><button className="icon-button" type="button" onClick={onClose} title="关闭"><X size={18}/></button></header>
      <form onSubmit={(event) => void submit(event)}>
        {error && <ErrorState message={error} />}
        <div className="combination-members">{selected.map((item) => <span key={item.spec.warehouse_sku}><PackageOpen size={13}/><b>{item.spec.warehouse_sku}</b><i>× {item.quantity}</i></span>)}</div>
        <div className="form-grid combination-form">
          <label className="full"><span>组合名称</span><input required maxLength={120} value={name} onChange={(event) => setName(event.target.value)} /></label>
          <label className="full"><span>被替代的发货 SKU</span><input list="combination-target-skus" value={substituteForSKU} onChange={(event) => setSubstituteForSKU(event.target.value)} placeholder="可选，输入精确仓库 SKU" /><datalist id="combination-target-skus">{targetOptions.map((spec) => <option value={spec.warehouse_sku} key={spec.warehouse_sku}>{spec.product_name}</option>)}</datalist></label>
        </div>
        <div className="combination-values-header"><div><strong>包裹修正值</strong><small>算法合并值 {number(calculated.length_cm, 2)} × {number(calculated.width_cm, 2)} × {number(calculated.height_cm, 2)} cm · {number(calculated.weight_kg, 3)} kg</small></div><button type="button" className="secondary-button" onClick={() => { setLengthCM(calculated.length_cm); setWidthCM(calculated.width_cm); setHeightCM(calculated.height_cm); setWeightKG(calculated.weight_kg); }}><Check size={14}/>使用算法值</button></div>
        <div className="form-grid combination-value-grid">
          <label><span>长度 (cm)</span><input required type="number" min="0.001" step="0.001" value={lengthCM} onChange={(event) => setLengthCM(Number(event.target.value))} /></label>
          <label><span>宽度 (cm)</span><input required type="number" min="0.001" step="0.001" value={widthCM} onChange={(event) => setWidthCM(Number(event.target.value))} /></label>
          <label><span>高度 (cm)</span><input required type="number" min="0.001" step="0.001" value={heightCM} onChange={(event) => setHeightCM(Number(event.target.value))} /></label>
          <label><span>重量 (kg)</span><input required type="number" min="0.001" step="0.001" value={weightKG} onChange={(event) => setWeightKG(Number(event.target.value))} /></label>
          <label className="full"><span>备注</span><input maxLength={500} value={note} onChange={(event) => setNote(event.target.value)} /></label>
        </div>
        <label className="toggle combination-enabled"><input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} /><span/><b>启用此组合与发货替代映射</b></label>
        <footer><button type="button" className="secondary-button" onClick={onClose}>取消</button><button type="submit" className="primary-button" disabled={saving}>{saving ? <LoaderCircle className="spin" size={16}/> : <Save size={16}/>} {saving ? "保存中" : existing ? "保存修改" : "保存组合"}</button></footer>
      </form>
    </div>
  </div>;
}
