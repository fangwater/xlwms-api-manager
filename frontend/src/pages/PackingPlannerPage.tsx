import { AlertTriangle, Boxes, ChevronLeft, ChevronRight, Layers3, Library, LoaderCircle, Minus, PackageCheck, PackagePlus, Pause, Play, Plus, RotateCcw, Save, Search, Trash2, Weight, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, PageHeader, number } from "../components/Common";
import type { PackingPackagePlan, PackingPlan, SKUCombination, WarehouseSKUSpec } from "../types";
import { PackingCombinationsPanel, SKUCombinationEditor } from "./PackingCombinations";
import PackingScene, { packingColorForSKU } from "./PackingScene";

type SelectedSKU = { spec: WarehouseSKUSpec; quantity: number };

const emptyPackage: PackingPackagePlan = {
  index: 1,
  dimensions: { length_cm: 1, width_cm: 1, height_cm: 1 },
  placements: [],
  packed_units: 0,
  used_weight_kg: 0,
  used_volume_cm3: 0,
  volume_utilization_percent: 0,
};

function dimensionsLabel(dimensions: { length_cm: number; width_cm: number; height_cm: number }): string {
  return `${number(dimensions.length_cm, 2)} × ${number(dimensions.width_cm, 2)} × ${number(dimensions.height_cm, 2)} cm`;
}

function prefersReducedMotion(): boolean {
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

export default function PackingPlannerPage() {
  const [search, setSearch] = useState("");
  const [candidates, setCandidates] = useState<WarehouseSKUSpec[]>([]);
  const [searching, setSearching] = useState(true);
  const [searchError, setSearchError] = useState("");
  const [selected, setSelected] = useState<SelectedSKU[]>([]);
  const [plan, setPlan] = useState<PackingPlan | null>(null);
  const [planning, setPlanning] = useState(false);
  const [planError, setPlanError] = useState("");
  const [activePackageIndex, setActivePackageIndex] = useState(0);
  const [visibleCount, setVisibleCount] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);
  const [resetToken, setResetToken] = useState(0);
  const [view, setView] = useState<"planner" | "combinations">("planner");
  const [editingCombination, setEditingCombination] = useState<SKUCombination | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [combinationRefreshToken, setCombinationRefreshToken] = useState(0);
  const [savedMessage, setSavedMessage] = useState("");

  useEffect(() => {
    let cancelled = false;
    const timer = window.setTimeout(async () => {
      setSearching(true);
      setSearchError("");
      try {
        const result = await api.warehouseSKUSpecs({ q: search.trim(), status: "complete", page: 1, pageSize: 30 });
        if (!cancelled) setCandidates(result.records);
      } catch (reason) {
        if (!cancelled) {
          setCandidates([]);
          setSearchError(reason instanceof Error ? reason.message : "无法加载 SKU 规格");
        }
      } finally {
        if (!cancelled) setSearching(false);
      }
    }, 180);
    return () => { cancelled = true; window.clearTimeout(timer); };
  }, [search]);

  const selectedSKUs = useMemo(() => new Set(selected.map((item) => item.spec.warehouse_sku)), [selected]);
  const availableCandidates = candidates.filter((item) => !selectedSKUs.has(item.warehouse_sku));
  const selectedUnits = selected.reduce((total, item) => total + item.quantity, 0);
  const activePackage = plan?.packages[activePackageIndex] ?? emptyPackage;
  const totalSteps = activePackage.placements.length;
  const currentPlacement = visibleCount > 0 ? activePackage.placements[visibleCount - 1] : undefined;

  useEffect(() => {
    if (!playing || visibleCount >= totalSteps) {
      if (visibleCount >= totalSteps) setPlaying(false);
      return;
    }
    const timer = window.setTimeout(() => setVisibleCount((current) => Math.min(current + 1, totalSteps)), 900 / speed);
    return () => window.clearTimeout(timer);
  }, [playing, speed, totalSteps, visibleCount]);

  const clearPlan = () => {
    setPlan(null);
    setPlanError("");
    setPlaying(false);
    setVisibleCount(0);
  };

  const addSKU = (spec: WarehouseSKUSpec) => {
    setSelected((current) => [...current, { spec, quantity: 1 }]);
    clearPlan();
  };

  const setQuantity = (sku: string, quantity: number) => {
    setSelected((current) => current.map((item) => item.spec.warehouse_sku === sku ? { ...item, quantity: Math.max(1, Math.min(99, quantity)) } : item));
    clearPlan();
  };

  const removeSKU = (sku: string) => {
    setSelected((current) => current.filter((item) => item.spec.warehouse_sku !== sku));
    clearPlan();
  };

  const createPlan = async () => {
    setPlanError("");
    if (selected.length === 0) {
      setPlanError("请至少选择一个规格完整的仓库 SKU");
      return;
    }
    setPlanning(true);
    try {
      const result = await api.packingPlan({
        items: selected.map((item) => ({ warehouse_sku: item.spec.warehouse_sku, quantity: item.quantity })),
      });
      setPlan(result);
      setActivePackageIndex(0);
      setVisibleCount(0);
      setPlaying(result.packages[0]?.placements.length > 0 && !prefersReducedMotion());
    } catch (reason) {
      setPlan(null);
      setPlanError(reason instanceof Error ? reason.message : "无法生成包装方案");
    } finally {
      setPlanning(false);
    }
  };

  const switchPackage = (index: number) => {
    setActivePackageIndex(index);
    setVisibleCount(0);
    setPlaying((plan?.packages[index]?.placements.length ?? 0) > 0 && !prefersReducedMotion());
  };

  const editCombination = async (combination: SKUCombination) => {
    const members: SelectedSKU[] = combination.items.map((item) => ({
      quantity: item.quantity,
      spec: {
        warehouse_sku: item.warehouse_sku, product_name: item.product_name ?? "",
        length_cm: item.length_cm, width_cm: item.width_cm, height_cm: item.height_cm, weight_kg: item.weight_kg,
        note: "", enabled: true, source: "combination", complete: true, missing_fields: [],
        first_seen_at: combination.created_at, updated_at: combination.updated_at,
      },
    }));
    setView("planner");
    setEditingCombination(combination);
    setSelected(members);
    setSavedMessage("");
    setPlanError("");
    setPlanning(true);
    try {
      const result = await api.packingPlan({ items: members.map((item) => ({ warehouse_sku: item.spec.warehouse_sku, quantity: item.quantity })) });
      setPlan(result);
      setActivePackageIndex(0);
      setVisibleCount(0);
      setPlaying(false);
    } catch (reason) {
      setPlan(null);
      setPlanError(reason instanceof Error ? reason.message : "无法重新计算组合");
    } finally {
      setPlanning(false);
    }
  };

  const canSaveCombination = selected.length > 0 && Boolean(editingCombination || plan?.summary.requested_units);
  const finishCombinationSave = (item: SKUCombination) => {
    setEditorOpen(false);
    setEditingCombination(item);
    setSavedMessage(`组合“${item.name}”已保存`);
    setCombinationRefreshToken((value) => value + 1);
  };

  return <>
    <PageHeader title="包装规划" subtitle={view === "planner" ? "按仓库 SKU 规格计算包裹，并保存组合与发货替代映射" : "已保存的 SKU 组合、人工包裹修正与发货替代映射"} actions={view === "planner" && editingCombination ? <button type="button" className="secondary-button" onClick={() => setEditingCombination(null)}><X size={15}/>退出修改</button> : undefined} />
    <div className="segmented-tabs packing-mode-tabs" role="tablist" aria-label="包装功能">
      <button type="button" role="tab" aria-selected={view === "planner"} className={view === "planner" ? "active" : ""} onClick={() => setView("planner")}><Layers3 size={15}/>包装规划</button>
      <button type="button" role="tab" aria-selected={view === "combinations"} className={view === "combinations" ? "active" : ""} onClick={() => setView("combinations")}><Library size={15}/>组合库</button>
    </div>
    {view === "planner" ? <>
    {editingCombination && <div className="combination-edit-banner"><span>正在修改</span><strong>{editingCombination.name}</strong><small>{editingCombination.substitute_for_sku ? `替代 ${editingCombination.substitute_for_sku}` : "未绑定发货替代"}</small></div>}
    {savedMessage && <div className="success-banner"><Save size={16}/>{savedMessage}</div>}
    <div className="packing-layout">
      <aside className="packing-config" aria-label="包装选品">
        <section className="packing-config-section">
          <header><div><h2>手工选品</h2><span>{selected.length} SKU · {selectedUnits} 件</span></div></header>
          <label className="packing-search"><Search size={16}/><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索仓库 SKU 或产品名称" /></label>
          <div className="packing-candidates" aria-label="可选 SKU">
            {searching ? <div className="packing-inline-state"><LoaderCircle className="spin" size={16}/>正在加载</div>
              : searchError ? <div className="packing-inline-state error">{searchError}</div>
              : availableCandidates.length ? availableCandidates.slice(0, 8).map((spec) => <button type="button" key={spec.warehouse_sku} onClick={() => addSKU(spec)}>
                <span className="packing-swatch" style={{ backgroundColor: packingColorForSKU(spec.warehouse_sku) }} />
                <span><strong>{spec.warehouse_sku}</strong><small>{spec.product_name || "未命名产品"}</small></span>
                <span className="packing-candidate-spec">{spec.length_cm} × {spec.width_cm} × {spec.height_cm} cm</span>
                <Plus size={15}/>
              </button>) : <div className="packing-inline-state">没有更多匹配 SKU</div>}
          </div>
        </section>

        <section className="packing-config-section selected-section">
          <header><div><h2>待包装 SKU</h2><span>单次最多 300 件</span></div></header>
          <div className="packing-selected-list">
            {selected.length ? selected.map(({ spec, quantity }) => <div className="packing-selected-row" key={spec.warehouse_sku}>
              <span className="packing-swatch" style={{ backgroundColor: packingColorForSKU(spec.warehouse_sku) }} />
              <div className="packing-selected-name"><strong>{spec.warehouse_sku}</strong><small>{spec.length_cm} × {spec.width_cm} × {spec.height_cm} cm · {spec.weight_kg} kg</small></div>
              <div className="packing-stepper" aria-label={`${spec.warehouse_sku} 数量`}>
                <button type="button" onClick={() => setQuantity(spec.warehouse_sku, quantity - 1)} title="减少数量"><Minus size={13}/></button>
                <input type="number" min="1" max="99" value={quantity} onChange={(event) => setQuantity(spec.warehouse_sku, Number(event.target.value) || 1)} />
                <button type="button" onClick={() => setQuantity(spec.warehouse_sku, quantity + 1)} title="增加数量"><Plus size={13}/></button>
              </div>
              <button type="button" className="icon-button packing-remove" onClick={() => removeSKU(spec.warehouse_sku)} title="移除 SKU"><Trash2 size={15}/></button>
            </div>) : <div className="packing-empty-selection"><PackagePlus size={20}/><span>从上方列表添加 SKU</span></div>}
          </div>
          <div className="packing-primary-actions">
            <button className="primary-button packing-generate" type="button" disabled={planning || selected.length === 0} onClick={() => void createPlan()}>{planning ? <LoaderCircle className="spin" size={16}/> : <PackageCheck size={16}/>} {planning ? "规划中" : "生成包装方案"}</button>
            <button className="secondary-button packing-save-combination" type="button" disabled={!canSaveCombination} onClick={() => setEditorOpen(true)}><Save size={15}/>{editingCombination ? "保存组合修改" : "保存为组合"}</button>
          </div>
        </section>
      </aside>

      <section className="packing-workspace" aria-label="包装方案">
        {planError && <ErrorState message={planError} />}
        <div className="packing-summary-bar">
          <div><Boxes size={17}/><span>已规划件数<strong>{plan ? `${plan.summary.packed_units} / ${plan.summary.requested_units}` : "-"}</strong></span></div>
          <div><PackageCheck size={17}/><span>包裹数量<strong>{plan ? plan.summary.packages_used : "-"}</strong></span></div>
          <div><Weight size={17}/><span>总重量<strong>{plan ? `${number(plan.summary.packed_weight_kg, 3)} kg` : "-"}</strong></span></div>
          <div className={plan?.summary.unfit_units ? "warning" : ""}><AlertTriangle size={17}/><span>异常件数<strong>{plan ? plan.summary.unfit_units : "-"}</strong></span></div>
        </div>

        <div className="packing-stage-toolbar">
          <div className="packing-package-tabs" role="tablist" aria-label="包裹选择">
            {plan?.packages.length ? plan.packages.map((packaged, index) => <button type="button" role="tab" aria-selected={index === activePackageIndex} className={index === activePackageIndex ? "active" : ""} key={packaged.index} onClick={() => switchPackage(index)}>包裹 {packaged.index}<span>{packaged.packed_units} 件</span></button>) : <span>尚未规划</span>}
          </div>
          <div className="packing-view-actions">
            <button type="button" className="icon-button bordered" onClick={() => setResetToken((value) => value + 1)} title="复位视角"><RotateCcw size={16}/></button>
          </div>
        </div>

        <div className="packing-stage-grid">
          {plan?.packages.length
            ? <PackingScene packageDimensions={activePackage.dimensions} placements={activePackage.placements} visibleCount={visibleCount} resetToken={resetToken} />
            : <div className="packing-scene packing-scene-empty"><EmptyState label="选择 SKU 并生成包装方案" /></div>}
          <aside className="packing-step-detail" aria-live="polite">
            <header><span>摆放步骤</span><strong>{visibleCount} / {totalSteps}</strong></header>
            {plan?.packages.length ? <div className="packing-package-spec"><span>建议包裹尺寸</span><strong>{dimensionsLabel(activePackage.dimensions)}</strong></div> : null}
            {currentPlacement ? <>
              <div className="packing-current-sku"><span className="packing-swatch" style={{ backgroundColor: packingColorForSKU(currentPlacement.warehouse_sku) }} /><div><strong>{currentPlacement.warehouse_sku}</strong><small>{currentPlacement.product_name || currentPlacement.unit_id}</small></div></div>
              <dl>
                <div><dt>单件尺寸</dt><dd>{dimensionsLabel(currentPlacement.dimensions)}</dd></div>
                <div><dt>放置坐标</dt><dd>{currentPlacement.position.x}, {currentPlacement.position.y}, {currentPlacement.position.z}</dd></div>
                <div><dt>单件重量</dt><dd>{currentPlacement.weight_kg} kg</dd></div>
              </dl>
            </> : plan ? <EmptyState label={totalSteps ? "播放后显示当前放置件" : "该包裹暂无可放置 SKU"} /> : <EmptyState label="尚未生成方案" />}
            <div className="packing-utilization"><span>空间利用率</span><strong>{plan?.packages.length ? `${number(activePackage.volume_utilization_percent, 1)}%` : "-"}</strong><div><i style={{ width: `${Math.min(activePackage.volume_utilization_percent, 100)}%` }} /></div></div>
          </aside>
        </div>

        <div className="packing-playback">
          <button type="button" className="icon-button bordered" disabled={!plan || visibleCount === 0} onClick={() => { setPlaying(false); setVisibleCount((value) => Math.max(value - 1, 0)); }} title="上一步"><ChevronLeft size={18}/></button>
          <button type="button" className="packing-play-button" disabled={!plan || totalSteps === 0} onClick={() => {
            if (visibleCount >= totalSteps) setVisibleCount(0);
            setPlaying((value) => !value);
          }} title={playing ? "暂停" : "播放"}>{playing ? <Pause size={17}/> : <Play size={17}/>}</button>
          <button type="button" className="icon-button bordered" disabled={!plan || visibleCount >= totalSteps} onClick={() => { setPlaying(false); setVisibleCount((value) => Math.min(value + 1, totalSteps)); }} title="下一步"><ChevronRight size={18}/></button>
          <div className="packing-progress"><i style={{ width: totalSteps ? `${visibleCount / totalSteps * 100}%` : "0%" }} /></div>
          <label className="packing-speed"><span>速度</span><select value={speed} onChange={(event) => setSpeed(Number(event.target.value))}><option value={0.5}>0.5×</option><option value={1}>1×</option><option value={1.5}>1.5×</option><option value={2}>2×</option></select></label>
        </div>

        {plan?.unfit_items.length ? <section className="packing-unfit">
          <header><AlertTriangle size={17}/><div><h2>异常 SKU</h2><span>{plan.unfit_items.length} 件未纳入包裹</span></div></header>
          <div>{plan.unfit_items.map((item) => <article key={item.unit_id}><span className="packing-swatch" style={{ backgroundColor: packingColorForSKU(item.warehouse_sku) }} /><strong>{item.unit_id}</strong><small>{item.reason}</small></article>)}</div>
        </section> : null}
      </section>
    </div>
    </> : <PackingCombinationsPanel refreshToken={combinationRefreshToken} onEdit={(item) => void editCombination(item)} />}
    {editorOpen && canSaveCombination && <SKUCombinationEditor selected={selected} plan={plan} existing={editingCombination} targetOptions={candidates} onClose={() => setEditorOpen(false)} onSaved={finishCombinationSave} />}
  </>;
}
