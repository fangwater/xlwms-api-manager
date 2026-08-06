import { AlertTriangle, Boxes, PackageX, RefreshCw, RotateCcw, Save, Search, SlidersHorizontal, Warehouse } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, dateTime, number } from "../components/Common";
import type { InventoryAlert, InventoryAlertPage } from "../types";
import "./InventoryAlertsPage.css";

type AlertView = "alert" | "all";

export default function InventoryAlertsPage({ warehouse }: { warehouse: string }) {
  const [view, setView] = useState<AlertView>("alert");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [data, setData] = useState<InventoryAlertPage | null>(null);
  const [defaultValue, setDefaultValue] = useState("100");
  const [loading, setLoading] = useState(true);
  const [savingDefault, setSavingDefault] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const result = await api.inventoryAlerts({ warehouse: warehouse || undefined, q: search, status: view, page, pageSize: 50 });
      setData(result);
      setDefaultValue(String(result.default_threshold));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法加载库存警告");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 180);
    return () => window.clearTimeout(timer);
  }, [warehouse, search, view, page]);

  const saveDefault = async (event: FormEvent) => {
    event.preventDefault();
    setSavingDefault(true);
    setError("");
    setNotice("");
    try {
      const result = await api.updateInventoryAlertDefault(Number(defaultValue));
      setDefaultValue(String(result.threshold));
      setNotice(`默认库存告警线已更新为 ${number(result.threshold)}`);
      await load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "默认告警线保存失败");
    } finally {
      setSavingDefault(false);
    }
  };

  const changeView = (next: AlertView) => {
    setView(next);
    setPage(1);
    setNotice("");
  };

  return <>
    <PageHeader title="库存警告" subtitle="按仓库监控正品可用库存" actions={<button className="icon-button bordered" onClick={() => void load()} title="刷新库存警告"><RefreshCw size={17}/></button>} />
    <div className="inventory-alert-default">
      <div className="inventory-alert-default-title"><SlidersHorizontal size={19}/><div><strong>默认告警线</strong><small>未单独配置的仓库 SKU</small></div></div>
      <form onSubmit={saveDefault}>
        <label><span>可用库存低于或等于</span><input aria-label="默认库存告警线" required type="number" min="0" max="1000000000" step="1" value={defaultValue} onChange={(event) => setDefaultValue(event.target.value)}/></label>
        <button className="primary-button" disabled={savingDefault}><Save size={15}/>{savingDefault ? "保存中" : "保存默认值"}</button>
      </form>
    </div>
    {data && <AlertSummary data={data}/>}
    <div className="inventory-alert-toolbar">
      <div className="segmented-tabs compact" role="tablist">
        <button className={view === "alert" ? "active" : ""} onClick={() => changeView("alert")}>告警中</button>
        <button className={view === "all" ? "active" : ""} onClick={() => changeView("all")}>全部 SKU 配置</button>
      </div>
      <label className="search-field"><Search size={17}/><input value={search} onChange={(event) => { setSearch(event.target.value); setPage(1); }} placeholder="搜索仓库、SKU 或产品名称"/></label>
    </div>
    {notice && <div className="success-banner">{notice}</div>}
    {error && <ErrorState message={error} onRetry={() => void load()}/>}
    {loading
      ? <LoadingState label="正在核对各仓库存水位"/>
      : data?.records.length
        ? <InventoryAlertTable records={data.records} defaultThreshold={data.default_threshold} onSaved={async (message) => { setNotice(message); await load(); }}/>
        : <EmptyState label={view === "alert" ? "当前没有低于告警线的仓库 SKU" : "暂无正品库存记录"}/>}
    {data && data.total > 0 && <Pagination page={data.page} pages={data.pages} total={data.total} onChange={setPage}/>}
  </>;
}

function AlertSummary({ data }: { data: InventoryAlertPage }) {
  const items = [
    { label: "告警 SKU", value: data.summary.alert_count, icon: AlertTriangle, tone: "red" },
    { label: "零可用库存", value: data.summary.out_of_stock_count, icon: PackageX, tone: "amber" },
    { label: "覆盖仓库", value: data.summary.warehouse_count, icon: Warehouse, tone: "blue" },
    { label: "监控 SKU", value: data.summary.sku_count, icon: Boxes, tone: "gray" }
  ];
  return <div className="metric-grid inventory-alert-metrics">{items.map((item) => {
    const Icon = item.icon;
    return <div className="metric-card" key={item.label}><div className={`metric-icon ${item.tone}`}><Icon size={18}/></div><div><span>{item.label}</span><strong>{number(item.value)}</strong><small>正品可用库存口径</small></div></div>;
  })}</div>;
}

function InventoryAlertTable({ records, defaultThreshold, onSaved }: { records: InventoryAlert[]; defaultThreshold: number; onSaved: (message: string) => Promise<void> }) {
  return <div className="table-panel inventory-alert-table-panel"><div className="table-scroll"><table className="data-table inventory-alert-table"><thead><tr><th>状态</th><th>仓库</th><th>SKU / 产品</th><th>正品总库存</th><th>可用库存</th><th>锁定</th><th>在途</th><th>告警线配置</th><th>库存更新时间</th></tr></thead><tbody>{records.map((item) => <tr key={`${item.wh_code}:${item.warehouse_sku}`} className={item.alert ? "inventory-alert-row" : ""}>
    <td><span className={`stock-alert-state ${item.alert ? "warning" : "normal"}`}>{item.alert ? "需关注" : "正常"}</span></td>
    <td><div className="primary-cell"><strong>{item.wh_name || item.wh_code}</strong><small>{item.wh_code}</small></div></td>
    <td><div className="primary-cell"><strong>{item.warehouse_sku}</strong><small>{item.product_name || "-"}</small></div></td>
    <td>{number(item.total_amount)}</td>
    <td className={item.alert ? "danger" : "positive"}><strong>{number(item.available_amount)}</strong></td>
    <td>{number(item.lock_amount)}</td><td>{number(item.transport_amount)}</td>
    <td><ThresholdEditor item={item} defaultThreshold={defaultThreshold} onSaved={onSaved}/></td>
    <td>{dateTime(item.inventory_at)}</td>
  </tr>)}</tbody></table></div></div>;
}

function ThresholdEditor({ item, defaultThreshold, onSaved }: { item: InventoryAlert; defaultThreshold: number; onSaved: (message: string) => Promise<void> }) {
  const [value, setValue] = useState(String(item.threshold));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => setValue(String(item.threshold)), [item.threshold]);

  const save = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      await api.updateInventoryAlertConfig({ wh_code: item.wh_code, warehouse_sku: item.warehouse_sku, threshold: Number(value) });
      await onSaved(`${item.wh_code} / ${item.warehouse_sku} 的告警线已保存`);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "保存失败");
    } finally {
      setSaving(false);
    }
  };

  const reset = async () => {
    setSaving(true);
    setError("");
    try {
      await api.resetInventoryAlertConfig({ wh_code: item.wh_code, warehouse_sku: item.warehouse_sku });
      setValue(String(defaultThreshold));
      await onSaved(`${item.wh_code} / ${item.warehouse_sku} 已恢复默认告警线`);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "恢复默认失败");
    } finally {
      setSaving(false);
    }
  };

  return <div className="stock-alert-config"><form onSubmit={save}><input aria-label={`${item.wh_code} ${item.warehouse_sku} 告警线`} required type="number" min="0" max="1000000000" step="1" value={value} onChange={(event) => setValue(event.target.value)}/><button className="icon-button bordered" disabled={saving || Number(value) === item.threshold} title="保存告警线"><Save size={14}/></button>{item.customized && <button type="button" className="icon-button bordered" disabled={saving} onClick={() => void reset()} title="恢复默认值"><RotateCcw size={14}/></button>}</form><small>{item.customized ? "单独配置" : `默认 ${number(defaultThreshold)}`}</small>{error && <em>{error}</em>}</div>;
}
