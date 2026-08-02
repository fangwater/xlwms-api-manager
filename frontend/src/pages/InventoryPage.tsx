import { Boxes, LockKeyhole, PackageCheck, RefreshCw, Search, SlidersHorizontal, Truck } from "lucide-react";
import { useEffect, useState, type ComponentType } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, StatusBadge, dateTime, number } from "../components/Common";
import type { InventoryKind, InventoryRecord, PageData, SKUStockLevel, SKUStockLevelPage, Warehouse } from "../types";

type InventoryView = "sku_levels" | InventoryKind;

const views: { key: InventoryView; label: string }[] = [
  { key: "sku_levels", label: "SKU 综合库存" },
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
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [syncing, setSyncing] = useState(false);
  const activeWarehouses = warehouses.filter((item) => item.active);
  const displayedWarehouses = activeWarehouses.filter((item) => !warehouse || item.wh_code === warehouse);

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      if (view === "sku_levels") {
        setLevels(await api.skuStockLevels({ warehouse: warehouse || undefined, q: search, stockType, page, pageSize: 30 }));
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
    const kind: InventoryKind = view === "sku_levels" ? "integrated" : view;
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
  const pageData = view === "sku_levels" ? levels : rawData;
  return <>
    <PageHeader
      title="库存中心"
      subtitle={view === "sku_levels" ? "启用仓 SKU 综合库存水位" : "综合库存、库龄与库存流水"}
      actions={<button className="primary-button" disabled={syncDisabled} onClick={() => void sync()}><RefreshCw size={16} className={syncing ? "spin" : ""} />{syncing ? "提交中" : !warehouse ? "同步全部启用仓" : "同步当前视图"}</button>}
    />
    <div className="segmented-tabs" role="tablist">{views.map((item) => <button key={item.key} className={view === item.key ? "active" : ""} onClick={() => changeView(item.key)}>{item.label}</button>)}</div>
    {view === "sku_levels" && levels && <StockSummary data={levels} />}
    <div className="filter-bar">
      <label className="search-field"><Search size={17} /><input value={search} onChange={(event) => { setSearch(event.target.value); setPage(1); }} placeholder={view === "sku_levels" ? "搜索 SKU 或产品名称" : "SKU、产品、箱型或单据号"} /></label>
      <label className="select-field"><SlidersHorizontal size={16} /><select value={stockType} onChange={(event) => { setStockType(event.target.value); setPage(1); }}><option value="">全部库存属性</option><option value="0">正品</option><option value="1">次品</option></select></label>
      <button className="icon-button bordered" onClick={() => void load()} title="刷新"><RefreshCw size={17} /></button>
    </div>
    {message && <div className="success-banner">{message}</div>}
    {error && <ErrorState message={error} onRetry={() => void load()} />}
    {loading
      ? <LoadingState />
      : view === "sku_levels"
        ? levels && levels.records.length
          ? <SKUStockTable records={levels.records} warehouses={displayedWarehouses} />
          : <EmptyState label="暂无 SKU 综合库存，请先同步启用仓" />
        : rawData && rawData.records.length
          ? <InventoryTable kind={view} records={rawData.records} warehouses={warehouses} />
          : <EmptyState label={warehouse ? "当前筛选暂无库存数据" : "暂无库存数据，请执行全部启用仓同步"} />}
    {pageData && pageData.total > 0 && <Pagination page={pageData.page} pages={pageData.pages} total={pageData.total} onChange={setPage} />}
  </>;
}

function StockSummary({ data }: { data: SKUStockLevelPage }) {
  const items: { label: string; value: number; icon: ComponentType<{ size?: number }>; tone: string }[] = [
    { label: "全局 SKU", value: data.summary.sku_count, icon: Boxes, tone: "gray" },
    { label: "综合总库存", value: data.summary.total_amount, icon: PackageCheck, tone: "green" },
    { label: "可用库存", value: data.summary.available_amount, icon: PackageCheck, tone: "blue" },
    { label: "锁定库存", value: data.summary.lock_amount, icon: LockKeyhole, tone: "amber" },
    { label: "在途库存", value: data.summary.transport_amount, icon: Truck, tone: "gray" }
  ];
  return <div className="metric-grid inventory-metrics">{items.map((item) => {
    const Icon = item.icon;
    return <div className="metric-card" key={item.label}><div className={"metric-icon " + item.tone}><Icon size={18} /></div><div><span>{item.label}</span><strong>{number(item.value)}</strong><small>综合库存口径</small></div></div>;
  })}</div>;
}

function SKUStockTable({ records, warehouses }: { records: SKUStockLevel[]; warehouses: Warehouse[] }) {
  return <div className="table-panel"><div className="table-scroll"><table className="data-table sku-level-table"><thead><tr>
    <th>SKU / 产品</th><th>状态</th><th>全局综合库存</th><th>全局可用</th><th>锁定</th><th>在途</th>
    {warehouses.map((item) => <th key={item.wh_code}>{item.name || item.wh_code}<small>{item.wh_code}</small></th>)}
    <th>更新时间</th>
  </tr></thead><tbody>{records.map((record) => <tr key={record.sku}>
    <td><div className="primary-cell"><strong>{record.sku}</strong><small>{record.product_name || "-"}</small></div></td>
    <td><span className={"stock-state " + (record.available_amount > 0 ? "in-stock" : "empty")}>{record.available_amount > 0 ? "有货" : "无可用"}</span></td>
    <td>{number(record.total_amount)}</td><td className={record.available_amount > 0 ? "positive" : "danger"}>{number(record.available_amount)}</td>
    <td>{number(record.lock_amount)}</td><td>{number(record.transport_amount)}</td>
    {warehouses.map((item) => {
      const stock = record.warehouses[item.wh_code];
      return <td key={item.wh_code}><div className="warehouse-level"><strong className={(stock?.total_amount || 0) > 0 ? "positive" : "danger"}>{number(stock?.total_amount || 0)}</strong><small>可用 {number(stock?.available_amount || 0)}</small></div></td>;
    })}
    <td>{dateTime(record.last_seen_at)}</td>
  </tr>)}</tbody></table></div></div>;
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
