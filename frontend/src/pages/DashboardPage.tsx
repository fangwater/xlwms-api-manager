import { AlertTriangle, ArrowRight, Boxes, Box, CheckCircle2, CircleDollarSign, Clock3, LockKeyhole, PackageCheck, RefreshCw, Warehouse as WarehouseIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { api } from "../api";
import Chart from "../components/Chart";
import { ErrorState, LoadingState, PageHeader, number } from "../components/Common";
import type { DashboardData, Warehouse } from "../types";

export default function DashboardPage({ warehouse, warehouses }: { warehouse: string; warehouses: Warehouse[] }) {
  const navigate = (path: string) => { const base=import.meta.env.BASE_URL.replace(/\/$/, ""); window.history.pushState({}, "", base+path); window.dispatchEvent(new PopStateEvent("popstate")); };
  const [data, setData] = useState<DashboardData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = async () => { setLoading(true); setError(""); try { setData(await api.dashboard(warehouse || undefined)); } catch (reason) { setError(reason instanceof Error ? reason.message : "无法加载看板"); } finally { setLoading(false); } };
  useEffect(() => { void load(); }, [warehouse]);
  const warehouseNames = useMemo(() => Object.fromEntries(warehouses.map((item) => [item.wh_code, item.name || item.wh_code])), [warehouses]);
  const selectedWarehouseName = warehouseNames[warehouse] || warehouse;
  const stockOption = useMemo(() => ({ tooltip: { trigger: "axis" }, grid: { left: 44, right: 18, top: 20, bottom: 34 }, xAxis: { type: "category", data: Object.keys(data?.inventory.stock_by_warehouse ?? {}).map((code) => warehouseNames[code] || code), axisLine: { lineStyle: { color: "#dfe3e6" } }, axisLabel: { color: "#747b82" } }, yAxis: { type: "value", splitLine: { lineStyle: { color: "#eef0f1" } }, axisLabel: { color: "#8b9197" } }, series: [{ type: "bar", data: Object.values(data?.inventory.stock_by_warehouse ?? {}), barMaxWidth: 34, itemStyle: { color: "#26785f", borderRadius: [3, 3, 0, 0] } }] }), [data, warehouseNames]);
  const ageOption = useMemo(() => { const buckets = data?.inventory.age_buckets ?? {}; return ({ tooltip: { trigger: "item" }, legend: { bottom: 0, itemWidth: 10, itemHeight: 10, textStyle: { color: "#687077", fontSize: 11 } }, series: [{ type: "pie", radius: [54, 82], center: ["50%", "43%"], label: { formatter: "{d}%", color: "#565d63" }, data: [{ name: "0-30天", value: buckets["0_30"] ?? 0, itemStyle: { color: "#2d8468" } }, { name: "31-60天", value: buckets["31_60"] ?? 0, itemStyle: { color: "#4c86a8" } }, { name: "61-90天", value: buckets["61_90"] ?? 0, itemStyle: { color: "#d5a13c" } }, { name: "91-180天", value: buckets["91_180"] ?? 0, itemStyle: { color: "#ca7041" } }, { name: "180天以上", value: buckets["181_plus"] ?? 0, itemStyle: { color: "#c64c54" } }] }] }); }, [data]);
  const flowOption = useMemo(() => ({ tooltip: { trigger: "axis" }, grid: { left: 44, right: 18, top: 20, bottom: 34 }, xAxis: { type: "category", data: data?.operations.trend.map((item) => item.date.slice(5)) ?? [], axisLine: { lineStyle: { color: "#dfe3e6" } }, axisLabel: { color: "#747b82" } }, yAxis: { type: "value", splitLine: { lineStyle: { color: "#eef0f1" } }, axisLabel: { color: "#8b9197" } }, series: [{ type: "line", smooth: true, showSymbol: false, data: data?.operations.trend.map((item) => item.amount) ?? [], lineStyle: { color: "#3976a8", width: 2 }, areaStyle: { color: "rgba(57,118,168,.10)" } }] }), [data]);
  if (loading) return <LoadingState label="正在汇总仓库数据" />;
  if (error || !data) return <><PageHeader title="运营总览" subtitle="仓库库存与费用运行态势" /><ErrorState message={error || "暂无看板数据"} onRetry={() => void load()} /></>;
  const metrics = [
    { label: "综合库存", value: number(data.inventory.total_amount), meta: `${data.inventory.sku_count} 个 SKU`, icon: Boxes, tone: "green" },
    { label: "可用库存", value: number(data.inventory.available_amount), meta: "可分配数量", icon: PackageCheck, tone: "blue" },
    { label: "锁定库存", value: number(data.inventory.lock_amount), meta: "待释放数量", icon: LockKeyhole, tone: "amber" },
    { label: "180天以上", value: number(data.inventory.stale_amount), meta: "长库龄库存", icon: Clock3, tone: "red" },
    { label: "箱型数量", value: number(data.inventory.box_type_count), meta: "在库箱型", icon: Box, tone: "gray" }
  ];
  return <>
    <PageHeader title="运营总览" subtitle={warehouse ? `${selectedWarehouseName} · 库存与费用运行态势` : "全仓库 · 库存与费用运行态势"} actions={<button className="icon-button bordered" onClick={() => void load()} title="刷新"><RefreshCw size={18} /></button>} />
    <section className="metric-grid">{metrics.map((item) => { const Icon=item.icon; return <article className="metric-card" key={item.label}><div className={`metric-icon ${item.tone}`}><Icon size={20} /></div><div><span>{item.label}</span><strong>{item.value}</strong><small>{item.meta}</small></div></article>; })}</section>
    <section className="chart-grid"><div className="panel"><div className="panel-header"><div><h2>仓库库存分布</h2><p>{Object.keys(data.inventory.stock_by_warehouse).length} 个仓库</p></div><WarehouseIcon size={18} /></div><Chart option={stockOption} /></div><div className="panel"><div className="panel-header"><div><h2>库龄结构</h2><p>产品、退货与箱库存</p></div><Clock3 size={18} /></div><Chart option={ageOption} /></div></section>
    <section className="dashboard-lower"><div className="panel"><div className="panel-header"><div><h2>费用流水趋势</h2><p>最近 14 天流水条数</p></div><CircleDollarSign size={18} /></div><Chart option={flowOption} height={244} /></div><div className="operations-panel"><div className="panel-header"><div><h2>同步概况</h2><p>费用明细处理状态</p></div></div><dl className="operation-stats"><div><dt><CheckCircle2 size={16} />已同步明细</dt><dd>{number(data.operations.cost_details)}</dd></div><div><dt><Clock3 size={16} />待同步</dt><dd>{number(data.operations.pending_details)}</dd></div><div><dt><AlertTriangle size={16} />异常记录</dt><dd>{number(data.operations.failed_details)}</dd></div><div><dt><WarehouseIcon size={16} />启用仓库</dt><dd>{data.operations.active_warehouses} / {data.operations.total_warehouses}</dd></div></dl><button className="panel-link" onClick={() => navigate("/sync")}>进入同步中心<ArrowRight size={16} /></button></div></section>
    {warehouses.length === 0 && <div className="notice-strip"><WarehouseIcon size={18} /><span>尚未注册仓库</span><button onClick={() => navigate("/warehouses")}>添加仓库</button></div>}
  </>;
}
