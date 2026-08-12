import { AlertTriangle, Box, Boxes, ChevronLeft, ChevronRight, LoaderCircle, Minus, PackagePlus, Pause, Play, Plus, RotateCcw, Search, Trash2, Weight } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, PageHeader, number } from "../components/Common";
import type { PackingCartonPlan, PackingPlan, PackingPlanRequest, WarehouseSKUSpec } from "../types";
import PackingScene, { packingColorForSKU, type PackingRendererMode } from "./PackingScene";

type SelectedSKU = { spec: WarehouseSKUSpec; quantity: number };
type CartonForm = { length: string; width: string; height: string; maxWeight: string; count: string };

const defaultCarton: CartonForm = { length: "40", width: "30", height: "25", maxWeight: "20", count: "2" };

function parsePositive(value: string): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

function cartonFromForm(form: CartonForm): PackingPlanRequest["carton"] {
  return {
    length_cm: parsePositive(form.length),
    width_cm: parsePositive(form.width),
    height_cm: parsePositive(form.height),
    max_weight_kg: parsePositive(form.maxWeight),
    count: Math.floor(parsePositive(form.count)),
  };
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
  const [cartonForm, setCartonForm] = useState<CartonForm>(defaultCarton);
  const [plan, setPlan] = useState<PackingPlan | null>(null);
  const [planning, setPlanning] = useState(false);
  const [planError, setPlanError] = useState("");
  const [activeCartonIndex, setActiveCartonIndex] = useState(0);
  const [visibleCount, setVisibleCount] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);
  const [rendererMode, setRendererMode] = useState<PackingRendererMode>("auto");
  const [resetToken, setResetToken] = useState(0);

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
  const draftCarton = useMemo(() => cartonFromForm(cartonForm), [cartonForm]);
  const activeCarton: PackingCartonPlan = plan?.cartons[activeCartonIndex] ?? {
    index: 1,
    placements: [],
    packed_units: 0,
    used_weight_kg: 0,
    used_volume_cm3: 0,
    volume_utilization_percent: 0,
  };
  const totalSteps = activeCarton.placements.length;
  const currentPlacement = visibleCount > 0 ? activeCarton.placements[visibleCount - 1] : undefined;

  useEffect(() => {
    if (!playing || visibleCount >= totalSteps) {
      if (visibleCount >= totalSteps) setPlaying(false);
      return;
    }
    const timer = window.setTimeout(() => setVisibleCount((current) => Math.min(current + 1, totalSteps)), 900 / speed);
    return () => window.clearTimeout(timer);
  }, [playing, speed, totalSteps, visibleCount]);

  const addSKU = (spec: WarehouseSKUSpec) => {
    setSelected((current) => [...current, { spec, quantity: 1 }]);
    setPlan(null);
    setPlanError("");
  };

  const setQuantity = (sku: string, quantity: number) => {
    setSelected((current) => current.map((item) => item.spec.warehouse_sku === sku ? { ...item, quantity: Math.max(1, Math.min(99, quantity)) } : item));
    setPlan(null);
  };

  const removeSKU = (sku: string) => {
    setSelected((current) => current.filter((item) => item.spec.warehouse_sku !== sku));
    setPlan(null);
  };

  const updateCarton = (key: keyof CartonForm, value: string) => {
    setCartonForm((current) => ({ ...current, [key]: value }));
    setPlan(null);
  };

  const createPlan = async () => {
    setPlanError("");
    if (selected.length === 0) {
      setPlanError("请至少选择一个规格完整的仓库 SKU");
      return;
    }
    const carton = cartonFromForm(cartonForm);
    if (!carton.length_cm || !carton.width_cm || !carton.height_cm || !carton.max_weight_kg || carton.count < 1) {
      setPlanError("箱体尺寸、承重和数量必须为正数");
      return;
    }
    setPlanning(true);
    try {
      const result = await api.packingPlan({
        items: selected.map((item) => ({ warehouse_sku: item.spec.warehouse_sku, quantity: item.quantity })),
        carton,
      });
      setPlan(result);
      setActiveCartonIndex(0);
      setVisibleCount(0);
      setPlaying(result.cartons[0]?.placements.length > 0 && !prefersReducedMotion());
    } catch (reason) {
      setPlan(null);
      setPlanError(reason instanceof Error ? reason.message : "无法生成装箱方案");
    } finally {
      setPlanning(false);
    }
  };

  const switchCarton = (index: number) => {
    setActiveCartonIndex(index);
    setVisibleCount(0);
    setPlaying((plan?.cartons[index]?.placements.length ?? 0) > 0 && !prefersReducedMotion());
  };

  return <>
    <PageHeader title="装箱规划" subtitle="按仓库 SKU 规格计算多件组合与逐件装箱顺序" />
    <div className="packing-layout">
      <aside className="packing-config" aria-label="装箱参数">
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
          <header><div><h2>待装 SKU</h2><span>单次最多 300 件</span></div></header>
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
        </section>

        <section className="packing-config-section carton-section">
          <header><div><h2>纸箱参数</h2><span>厘米 / 千克</span></div></header>
          <div className="packing-carton-inputs">
            <label><span>长</span><input aria-label="纸箱长度" type="number" min="0.1" max="500" step="0.1" value={cartonForm.length} onChange={(event) => updateCarton("length", event.target.value)} /></label>
            <label><span>宽</span><input aria-label="纸箱宽度" type="number" min="0.1" max="500" step="0.1" value={cartonForm.width} onChange={(event) => updateCarton("width", event.target.value)} /></label>
            <label><span>高</span><input aria-label="纸箱高度" type="number" min="0.1" max="500" step="0.1" value={cartonForm.height} onChange={(event) => updateCarton("height", event.target.value)} /></label>
            <label><span>承重</span><input aria-label="纸箱承重" type="number" min="0.1" max="1000" step="0.1" value={cartonForm.maxWeight} onChange={(event) => updateCarton("maxWeight", event.target.value)} /></label>
            <label><span>箱数</span><input aria-label="纸箱数量" type="number" min="1" max="20" step="1" value={cartonForm.count} onChange={(event) => updateCarton("count", event.target.value)} /></label>
          </div>
          <button className="primary-button packing-generate" type="button" disabled={planning || selected.length === 0} onClick={() => void createPlan()}>{planning ? <LoaderCircle className="spin" size={16}/> : <Box size={16}/>} {planning ? "规划中" : "生成装箱方案"}</button>
        </section>
      </aside>

      <section className="packing-workspace" aria-label="装箱方案">
        {planError && <ErrorState message={planError} />}
        <div className="packing-summary-bar">
          <div><Boxes size={17}/><span>已装件数<strong>{plan ? `${plan.summary.packed_units} / ${plan.summary.requested_units}` : "-"}</strong></span></div>
          <div><Box size={17}/><span>使用纸箱<strong>{plan ? `${plan.summary.cartons_used} / ${plan.summary.cartons_available}` : "-"}</strong></span></div>
          <div><Weight size={17}/><span>已装重量<strong>{plan ? `${number(plan.summary.packed_weight_kg, 3)} kg` : "-"}</strong></span></div>
          <div className={plan?.summary.unfit_units ? "warning" : ""}><AlertTriangle size={17}/><span>未装件数<strong>{plan ? plan.summary.unfit_units : "-"}</strong></span></div>
        </div>

        <div className="packing-stage-toolbar">
          <div className="packing-carton-tabs" role="tablist" aria-label="纸箱选择">
            {plan?.cartons.length ? plan.cartons.map((carton, index) => <button type="button" role="tab" aria-selected={index === activeCartonIndex} className={index === activeCartonIndex ? "active" : ""} key={carton.index} onClick={() => switchCarton(index)}>箱 {carton.index}<span>{carton.packed_units} 件</span></button>) : <span>箱 1</span>}
          </div>
          <div className="packing-view-actions">
            <div className="packing-view-switch" aria-label="渲染模式">
              <button type="button" className={rendererMode === "auto" ? "active" : ""} onClick={() => setRendererMode("auto")} title="优先使用三维视图">3D</button>
              <button type="button" className={rendererMode === "2d" ? "active" : ""} onClick={() => setRendererMode("2d")} title="使用二维兼容视图">2D</button>
            </div>
            <button type="button" className="icon-button bordered" onClick={() => setResetToken((value) => value + 1)} title="复位视角"><RotateCcw size={16}/></button>
          </div>
        </div>

        <div className="packing-stage-grid">
          <PackingScene carton={plan?.carton ?? draftCarton} placements={activeCarton.placements} visibleCount={visibleCount} rendererMode={rendererMode} resetToken={resetToken} />
          <aside className="packing-step-detail" aria-live="polite">
            <header><span>装箱步骤</span><strong>{visibleCount} / {totalSteps}</strong></header>
            {currentPlacement ? <>
              <div className="packing-current-sku"><span className="packing-swatch" style={{ backgroundColor: packingColorForSKU(currentPlacement.warehouse_sku) }} /><div><strong>{currentPlacement.warehouse_sku}</strong><small>{currentPlacement.product_name || currentPlacement.unit_id}</small></div></div>
              <dl>
                <div><dt>装入尺寸</dt><dd>{currentPlacement.dimensions.length_cm} × {currentPlacement.dimensions.width_cm} × {currentPlacement.dimensions.height_cm} cm</dd></div>
                <div><dt>旋转方向</dt><dd>{currentPlacement.rotation}</dd></div>
                <div><dt>放置坐标</dt><dd>{currentPlacement.position.x}, {currentPlacement.position.y}, {currentPlacement.position.z}</dd></div>
                <div><dt>单件重量</dt><dd>{currentPlacement.weight_kg} kg</dd></div>
              </dl>
            </> : plan ? <EmptyState label={totalSteps ? "播放后显示当前放置件" : "该箱暂无可放置 SKU"} /> : <EmptyState label="选择 SKU 并生成装箱方案" />}
            <div className="packing-utilization"><span>空间利用率</span><strong>{plan ? `${number(activeCarton.volume_utilization_percent, 1)}%` : "-"}</strong><div><i style={{ width: `${Math.min(activeCarton.volume_utilization_percent, 100)}%` }} /></div></div>
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
          <header><AlertTriangle size={17}/><div><h2>未装入 SKU</h2><span>{plan.unfit_items.length} 件需要调整箱型或箱数</span></div></header>
          <div>{plan.unfit_items.map((item) => <article key={item.unit_id}><span className="packing-swatch" style={{ backgroundColor: packingColorForSKU(item.warehouse_sku) }} /><strong>{item.unit_id}</strong><small>{item.reason}</small></article>)}</div>
        </section> : null}
      </section>
    </div>
  </>;
}
