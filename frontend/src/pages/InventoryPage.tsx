import { Boxes, LockKeyhole, PackageCheck, Pencil, Plus, RefreshCw, RotateCcw, Search, SlidersHorizontal, Truck, X } from "lucide-react";
import { useEffect, useState, type ComponentType, type FormEvent } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, StatusBadge, dateTime, number } from "../components/Common";
import type { InventoryCorrection, InventoryKind, InventoryRecord, PageData, SKUStockLevel, SKUStockLevelPage, Warehouse } from "../types";

type InventoryView = "sku_levels" | "corrections" | InventoryKind;

const views: { key: InventoryView; label: string }[] = [
  { key: "sku_levels", label: "SKU 综合库存" },
  { key: "corrections", label: "库存修正" },
  { key: "integrated", label: "综合库存" },
  { key: "stock_age", label: "产品库龄" },
  { key: "stock_flow", label: "产品流水" },
  { key: "box_stock", label: "箱库存" },
  { key: "box_stock_age", label: "箱库龄" },
  { key: "box_segment_age", label: "分段库龄" },
  { key: "box_stock_flow", label: "箱库存流水" }
];

export default function InventoryPage({ warehouse, warehouses }: { warehouse: string; warehouses: Warehouse[] }) {
  const [view, setView] = useState<InventoryView>("sku_levels");
  const [search, setSearch] = useState("");
  const [stockType, setStockType] = useState("");
  const [page, setPage] = useState(1);
  const [rawData, setRawData] = useState<PageData<InventoryRecord> | null>(null);
  const [levels, setLevels] = useState<SKUStockLevelPage | null>(null);
  const [corrections, setCorrections] = useState<PageData<InventoryCorrection> | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [syncing, setSyncing] = useState(false);
  const [editing, setEditing] = useState<CorrectionEditor | null>(null);
  const [savingCorrection, setSavingCorrection] = useState(false);
  const [correctionError, setCorrectionError] = useState("");
  const activeWarehouses = warehouses.filter((item) => item.active);
  const displayedWarehouses = activeWarehouses.filter((item) => !warehouse || item.wh_code === warehouse);

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      if (view === "sku_levels") {
        setLevels(await api.skuStockLevels({ warehouse: warehouse || undefined, q: search, stockType, page, pageSize: 30 }));
      } else if (view === "corrections") {
        setCorrections(await api.inventoryCorrections({ warehouse: warehouse || undefined, q: search, page, pageSize: 30 }));
      } else {
        setRawData(await api.inventory({ kind: view, warehouse: warehouse || undefined, q: search, stockType, page, pageSize: 30 }));
      }
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法加载库存");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, [view, warehouse, search, stockType, page]);

  const changeView = (value: InventoryView) => {
    setView(value);
    setPage(1);
    setMessage("");
  };

  const sync = async () => {
    const kind: InventoryKind = view === "sku_levels" || view === "corrections" ? "integrated" : view;
    const targets = activeWarehouses.filter((item) => !warehouse || item.wh_code === warehouse);
    if (!targets.length) return;
    setSyncing(true);
    setError("");
    setMessage("");
    try {
      await Promise.all(targets.map((item) => api.syncInventory(item.wh_code, [kind])));
      setMessage(targets.length === 1 ? "库存同步任务已提交" : targets.length + " 个仓库的库存同步任务已提交");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "同步失败");
    } finally {
      setSyncing(false);
    }
  };

  const syncDisabled = syncing || displayedWarehouses.length === 0;
  const pageData = view === "sku_levels" ? levels : view === "corrections" ? corrections : rawData;

  const openNewCorrection = () => {
    setCorrectionError("");
    setEditing({ wh_code: warehouse || displayedWarehouses[0]?.wh_code || "", warehouse_sku: "", product_name: "", raw_available_amount: 0, correction_mode: "absolute", correction_amount: "0", note: "", exists: false, lockIdentity: false });
  };

  const openStockCorrection = (record: SKUStockLevel, target: Warehouse) => {
    const stock = record.warehouses[target.wh_code];
    setCorrectionError("");
    setEditing({
      wh_code: target.wh_code,
      warehouse_sku: record.sku,
      product_name: record.product_name,
      raw_available_amount: stock?.raw_fulfillment_available_amount || 0,
      correction_mode: stock?.correction_mode || "absolute",
      correction_amount: String(stock?.corrected ? stock.correction_amount || 0 : 0),
      note: stock?.correction_note || "",
      exists: Boolean(stock?.corrected),
      lockIdentity: true
    });
  };

  const openExistingCorrection = (item: InventoryCorrection) => {
    setCorrectionError("");
    setEditing({ wh_code: item.wh_code, warehouse_sku: item.warehouse_sku, product_name: item.product_name, raw_available_amount: item.raw_available_amount, correction_mode: item.correction_mode, correction_amount: String(item.correction_amount), note: item.note, exists: true, lockIdentity: true });
  };

  const saveCorrection = async (event: FormEvent) => {
    event.preventDefault();
    if (!editing) return;
    setSavingCorrection(true);
    setCorrectionError("");
    try {
      await api.saveInventoryCorrection(editing.wh_code, editing.warehouse_sku, { correction_mode: editing.correction_mode, correction_amount: Number(editing.correction_amount), note: editing.note });
      setEditing(null);
      setMessage("库存修正已生效，后续 SHEIN 和 Temu 发货查询将使用修正值");
      await load();
    } catch (reason) {
      setCorrectionError(reason instanceof Error ? reason.message : "保存库存修正失败");
    } finally {
      setSavingCorrection(false);
    }
  };

  const resetCorrection = async (item = editing) => {
    if (!item || !item.exists || !window.confirm(`确认撤销 ${item.wh_code} / ${item.warehouse_sku} 的库存修正？`)) return;
    setSavingCorrection(true);
    setCorrectionError("");
    try {
      await api.deleteInventoryCorrection(item.wh_code, item.warehouse_sku);
      setEditing(null);
      setMessage("库存修正已撤销，后续发货查询恢复使用领星实时库存");
      await load();
    } catch (reason) {
      setCorrectionError(reason instanceof Error ? reason.message : "撤销库存修正失败");
    } finally {
      setSavingCorrection(false);
    }
  };
  return <>
    <PageHeader
      title="库存中心"
      subtitle={view === "sku_levels" ? "启用仓 SKU 综合库存水位" : view === "corrections" ? "按仓库和 SKU 覆盖发货可用库存，不回写领星" : "综合库存、库龄与库存流水"}
      actions={<>{view === "corrections" && <button className="secondary-button" onClick={openNewCorrection}><Plus size={16} />新增修正</button>}<button className="primary-button" disabled={syncDisabled} onClick={() => void sync()}><RefreshCw size={16} className={syncing ? "spin" : ""} />{syncing ? "提交中" : !warehouse ? "同步全部启用仓" : "同步当前视图"}</button></>}
    />
    <div className="segmented-tabs" role="tablist">{views.map((item) => <button key={item.key} className={view === item.key ? "active" : ""} onClick={() => changeView(item.key)}>{item.label}</button>)}</div>
    {view === "sku_levels" && levels && <StockSummary data={levels} />}
    <div className="filter-bar">
      <label className="search-field"><Search size={17} /><input value={search} onChange={(event) => { setSearch(event.target.value); setPage(1); }} placeholder={view === "sku_levels" ? "搜索 SKU 或产品名称" : "SKU、产品、箱型或单据号"} /></label>
      {view !== "corrections" && <label className="select-field"><SlidersHorizontal size={16} /><select value={stockType} onChange={(event) => { setStockType(event.target.value); setPage(1); }}><option value="">全部库存属性</option><option value="0">正品</option><option value="1">次品</option></select></label>}
      <button className="icon-button bordered" onClick={() => void load()} title="刷新"><RefreshCw size={17} /></button>
    </div>
    {message && <div className="success-banner">{message}</div>}
    {error && <ErrorState message={error} onRetry={() => void load()} />}
    {loading
      ? <LoadingState />
      : view === "sku_levels"
        ? levels && levels.records.length
          ? <SKUStockTable records={levels.records} warehouses={displayedWarehouses} onEdit={openStockCorrection} />
          : <EmptyState label="暂无 SKU 综合库存，请先同步启用仓" />
        : view === "corrections"
          ? corrections && corrections.records.length
            ? <CorrectionTable records={corrections.records} onEdit={openExistingCorrection} onReset={(item) => void resetCorrection({ wh_code: item.wh_code, warehouse_sku: item.warehouse_sku, product_name: item.product_name, raw_available_amount: item.raw_available_amount, correction_mode: item.correction_mode, correction_amount: String(item.correction_amount), note: item.note, exists: true, lockIdentity: true })} />
            : <EmptyState label="暂无库存修正，发货查询当前全部使用领星实时库存" />
        : rawData && rawData.records.length
          ? <InventoryTable kind={view} records={rawData.records} warehouses={warehouses} />
          : <EmptyState label={warehouse ? "当前筛选暂无库存数据" : "暂无库存数据，请执行全部启用仓同步"} />}
    {pageData && pageData.total > 0 && <Pagination page={pageData.page} pages={pageData.pages} total={pageData.total} onChange={setPage} />}
    {editing && <CorrectionModal editor={editing} warehouses={activeWarehouses} saving={savingCorrection} error={correctionError} onChange={setEditing} onClose={() => setEditing(null)} onSave={saveCorrection} onReset={() => void resetCorrection()} />}
  </>;
}

type CorrectionEditor = {
  wh_code: string;
  warehouse_sku: string;
  product_name: string;
  raw_available_amount: number;
  correction_mode: "absolute" | "subtract";
  correction_amount: string;
  note: string;
  exists: boolean;
  lockIdentity: boolean;
};

function StockSummary({ data }: { data: SKUStockLevelPage }) {
  const items: { label: string; value: number; icon: ComponentType<{ size?: number }>; tone: string }[] = [
    { label: "全局 SKU", value: data.summary.sku_count, icon: Boxes, tone: "gray" },
    { label: "综合总库存", value: data.summary.total_amount, icon: PackageCheck, tone: "green" },
    { label: "发货有效库存", value: data.summary.fulfillment_available_amount, icon: PackageCheck, tone: "blue" },
    { label: "锁定库存", value: data.summary.lock_amount, icon: LockKeyhole, tone: "amber" },
    { label: "在途库存", value: data.summary.transport_amount, icon: Truck, tone: "gray" }
  ];
  return <div className="metric-grid inventory-metrics">{items.map((item) => {
    const Icon = item.icon;
    const detail = item.label === "发货有效库存" && data.summary.correction_count > 0
      ? `领星原值 ${number(data.summary.raw_fulfillment_available_amount)} · ${number(data.summary.correction_count)} 项修正`
      : "综合库存口径";
    return <div className="metric-card" key={item.label}><div className={"metric-icon " + item.tone}><Icon size={18} /></div><div><span>{item.label}</span><strong>{number(item.value)}</strong><small>{detail}</small></div></div>;
  })}</div>;
}

function SKUStockTable({ records, warehouses, onEdit }: { records: SKUStockLevel[]; warehouses: Warehouse[]; onEdit: (record: SKUStockLevel, warehouse: Warehouse) => void }) {
  return <div className="table-panel"><div className="table-scroll"><table className="data-table sku-level-table"><thead><tr>
    <th>SKU / 产品</th><th>状态</th><th>全局综合库存</th><th>发货有效库存</th><th>锁定</th><th>在途</th>
    {warehouses.map((item) => <th key={item.wh_code}>{item.name || item.wh_code}<small>{item.wh_code}</small></th>)}
    <th>更新时间</th>
  </tr></thead><tbody>{records.map((record) => <tr key={record.sku}>
    <td><div className="primary-cell"><strong>{record.sku}</strong><small>{record.product_name || "-"}</small></div></td>
    <td><span className={"stock-state " + (record.fulfillment_available_amount > 0 ? "in-stock" : "empty")}>{record.fulfillment_available_amount > 0 ? "有货" : "无可用"}</span></td>
    <td>{number(record.total_amount)}</td><td className={record.fulfillment_available_amount > 0 ? "positive" : "danger"}>{number(record.fulfillment_available_amount)}</td>
    <td>{number(record.lock_amount)}</td><td>{number(record.transport_amount)}</td>
    {warehouses.map((item) => {
      const stock = record.warehouses[item.wh_code];
      const effective = stock?.fulfillment_available_amount || 0;
      return <td key={item.wh_code}><div className="warehouse-level corrected-level"><div><strong className={effective > 0 ? "positive" : "danger"}>{number(effective)}</strong>{stock?.corrected && <span className="correction-badge">已修正</span>}</div><small>{stock?.corrected ? `领星原值 ${number(stock.raw_fulfillment_available_amount)}` : "发货可用"}</small><button className="icon-button correction-edit" onClick={() => onEdit(record, item)} title={`修正 ${item.wh_code} 库存`}><Pencil size={14} /></button></div></td>;
    })}
    <td>{dateTime(record.last_seen_at)}</td>
  </tr>)}</tbody></table></div></div>;
}

function CorrectionTable({ records, onEdit, onReset }: { records: InventoryCorrection[]; onEdit: (item: InventoryCorrection) => void; onReset: (item: InventoryCorrection) => void }) {
  return <div className="table-panel"><div className="table-scroll"><table className="data-table"><thead><tr><th>仓库</th><th>SKU / 产品</th><th>领星原值</th><th>修正规则</th><th>发货有效值</th><th>备注</th><th>更新时间</th><th>操作</th></tr></thead><tbody>{records.map((item) => <tr key={`${item.wh_code}:${item.warehouse_sku}`}>
    <td><div className="primary-cell"><strong>{item.warehouse_name || item.wh_code}</strong><small>{item.wh_code}</small></div></td>
    <td><div className="primary-cell"><strong>{item.warehouse_sku}</strong><small>{item.product_name || "-"}</small></div></td>
    <td>{number(item.raw_available_amount)}</td><td>{item.correction_mode === "subtract" ? `比 OMS 少 ${number(item.correction_amount)}` : `直接设为 ${number(item.correction_amount)}`}</td><td><span className="correction-value">{number(item.corrected_available_amount)}</span></td><td>{item.note || "-"}</td><td>{dateTime(item.updated_at)}</td>
    <td><div className="row-actions"><button className="icon-button" onClick={() => onEdit(item)} title="编辑库存修正"><Pencil size={15}/></button><button className="icon-button danger-button" onClick={() => onReset(item)} title="撤销库存修正"><RotateCcw size={15}/></button></div></td>
  </tr>)}</tbody></table></div></div>;
}

function CorrectionModal({ editor, warehouses, saving, error, onChange, onClose, onSave, onReset }: { editor: CorrectionEditor; warehouses: Warehouse[]; saving: boolean; error: string; onChange: (value: CorrectionEditor) => void; onClose: () => void; onSave: (event: FormEvent) => void; onReset: () => void }) {
  const amount = Number(editor.correction_amount) || 0;
  const effective = editor.correction_mode === "subtract" ? Math.max(editor.raw_available_amount - amount, 0) : amount;
  return <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (!saving && event.target === event.currentTarget) onClose(); }}><section className="modal correction-modal" role="dialog" aria-modal="true" aria-labelledby="correction-modal-title">
    <header><div><h2 id="correction-modal-title">{editor.exists ? "编辑库存修正" : "新增库存修正"}</h2><p>修正只影响 SHEIN 和 Temu 发货查询，不回写领星 WMS</p></div><button className="icon-button" type="button" disabled={saving} onClick={onClose} title="关闭"><X size={19}/></button></header>
    <form onSubmit={onSave}>{error && <div className="error-banner"><span>{error}</span></div>}<div className="correction-mode-control" role="group" aria-label="修正方式"><button type="button" className={editor.correction_mode === "absolute" ? "active" : ""} onClick={() => onChange({ ...editor, correction_mode: "absolute" })}>直接设为</button><button type="button" className={editor.correction_mode === "subtract" ? "active" : ""} onClick={() => onChange({ ...editor, correction_mode: "subtract" })}>比 OMS 少</button></div><div className="form-grid">
      <label><span>仓库</span><select required disabled={editor.lockIdentity} value={editor.wh_code} onChange={(event) => onChange({ ...editor, wh_code: event.target.value })}><option value="">选择仓库</option>{warehouses.map((item) => <option key={item.wh_code} value={item.wh_code}>{item.name || item.wh_code} ({item.wh_code})</option>)}</select></label>
      <label><span>{editor.correction_mode === "subtract" ? "从 OMS 库存扣减" : "修正后的发货可用库存"}</span><input required aria-label={editor.correction_mode === "subtract" ? "从 OMS 库存扣减" : "修正后的发货可用库存"} type="number" min="0" step="1" value={editor.correction_amount} onChange={(event) => onChange({ ...editor, correction_amount: event.target.value })}/></label>
      <label className="full"><span>仓库 SKU</span><input required disabled={editor.lockIdentity} value={editor.warehouse_sku} onChange={(event) => onChange({ ...editor, warehouse_sku: event.target.value })}/></label>
      <label className="full"><span>备注</span><input value={editor.note} maxLength={500} onChange={(event) => onChange({ ...editor, note: event.target.value })} placeholder="例如：仓库盘点找不到货"/></label>
    </div>{editor.lockIdentity && <div className="correction-source"><span>领星当前原值 <b>{number(editor.raw_available_amount)}</b></span><span>发货有效值 <strong>{number(effective)}</strong></span></div>}<footer>{editor.exists && <button type="button" className="secondary-button danger-button correction-reset" disabled={saving} onClick={onReset}><RotateCcw size={15}/>撤销修正</button>}<button type="button" className="secondary-button" disabled={saving} onClick={onClose}>取消</button><button type="submit" className="primary-button" disabled={saving}>{saving ? "保存中" : "保存并立即生效"}</button></footer></form>
  </section></div>;
}

function InventoryTable({ kind, records, warehouses }: { kind: InventoryKind; records: InventoryRecord[]; warehouses: Warehouse[] }) {
  const names = Object.fromEntries(warehouses.map((item) => [item.wh_code, item.name || item.wh_code]));
  const base = (record: InventoryRecord) => <><td><div className="primary-cell"><strong>{names[record.wh_code] || record.wh_name || record.wh_code}</strong><small>{record.wh_code}</small></div></td><td><div className="primary-cell"><strong>{record.sku || record.box_type || "-"}</strong><small>{record.product_name || record.customize_barcode || record.fnsku}</small></div></td></>;
  return <div className="table-panel"><div className="table-scroll"><table className="data-table"><thead><tr><th>仓库</th><th>{kind.startsWith("box_") ? "箱型 / 条码" : "SKU / 产品"}</th>{headers(kind).map((item) => <th key={item}>{item}</th>)}</tr></thead><tbody>{records.map((record) => <tr key={record.id}>{base(record)}{cells(kind, record)}</tr>)}</tbody></table></div></div>;
}

function headers(kind: InventoryKind): string[] {
  if (kind === "integrated") return ["综合库存", "可用", "锁定", "在途", "产品 / 箱 / 退货", "属性"];
  if (kind === "stock_age") return ["FNSKU", "库存", "库龄", "上架日期", "统计日期", "属性"];
  if (kind === "stock_flow") return ["变化量", "剩余库存", "单据类型", "关联单号", "批次", "操作时间"];
  if (kind === "box_stock") return ["总库存", "可用", "锁定", "在途", "属性"];
  if (kind === "box_stock_age") return ["库存", "库龄", "上架日期", "统计日期", "状态"];
  if (kind === "box_segment_age") return ["0-30天", "31-60天", "61-90天", "91-180天", "180天以上", "统计日期"];
  return ["变化量", "剩余库存", "单据类型", "关联单号", "批次", "操作时间"];
}

function cells(kind: InventoryKind, record: InventoryRecord) {
  if (kind === "integrated") return <><td>{number(record.total_amount)}</td><td className="positive">{number(record.available_amount)}</td><td>{number(record.lock_amount)}</td><td>{number(record.transport_amount)}</td><td>{number(record.product_total_amount)} / {number(record.box_total_amount)} / {number(record.fba_return_total_amount)}</td><td>{record.stock_type === 1 ? "次品" : "正品"}</td></>;
  if (kind === "stock_age") return <><td>{record.fnsku || "-"}</td><td>{number(record.total_amount)}</td><td className={(record.stock_age ?? 0) > 180 ? "danger" : ""}>{number(record.stock_age ?? 0)} 天</td><td>{record.shelf_date || "-"}</td><td>{record.statistic_date || "-"}</td><td>{record.stock_type === 1 ? "次品" : "正品"}</td></>;
  if (kind === "stock_flow" || kind === "box_stock_flow") return <><td className={record.change_amount >= 0 ? "positive" : "danger"}>{record.change_amount > 0 ? "+" : ""}{number(record.change_amount)}</td><td>{number(record.total_amount)}</td><td>{record.relate_order_type_name || "-"}</td><td>{record.relate_order_no || "-"}</td><td>{record.batch_no || "-"}</td><td>{dateTime(record.operate_time)}</td></>;
  if (kind === "box_stock") return <><td>{number(record.total_amount)}</td><td className="positive">{number(record.available_amount)}</td><td>{number(record.lock_amount)}</td><td>{number(record.transport_amount)}</td><td>{record.stock_type === 1 ? "次品" : "正品"}</td></>;
  if (kind === "box_stock_age") return <><td>{number(record.total_amount)}</td><td>{number(record.stock_age ?? 0)} 天</td><td>{record.shelf_date || "-"}</td><td>{record.statistic_date || "-"}</td><td><StatusBadge status={record.stock_age_status === 1 ? "error" : "success"} /></td></>;
  return <><td>{number(record.segment_one_quantity)}</td><td>{number(record.segment_two_quantity)}</td><td>{number(record.segment_three_quantity)}</td><td>{number(record.segment_four_quantity)}</td><td className="danger">{number(record.segment_five_quantity)}</td><td>{record.statistic_date || "-"}</td></>;
}
