import { Archive, Truck, Bell, Boxes, ChartNoAxesCombined, ChevronDown, ClipboardCheck, Database, ExternalLink, Gauge, Link2, ListTodo, Menu, PackageCheck, PanelLeftClose, RefreshCw, ScanBox, Settings, SlidersHorizontal, TriangleAlert, Warehouse as WarehouseIcon } from "lucide-react";
import { useState, type ReactNode } from "react";
import type { Warehouse } from "../types";

const groups = [
  { label: "运营", items: [
    { path: "/", label: "运营总览", icon: ChartNoAxesCombined },
    { path: "/inventory", label: "库存中心", icon: Boxes },
    { path: "/outbound", label: "出库管理", icon: Truck },
    { path: "/platform-orders", label: "平台订单待处理", icon: ListTodo },
    { path: "/product-pairings", label: "组合配对", icon: Link2 },
    { path: "/fulfillment-audits", label: "履约核查", icon: ClipboardCheck },
    { path: "/fulfilled-orders", label: "出库物流跟踪", icon: Archive },
    { path: "/delivery-evaluation", label: "快递评价", icon: Gauge },
    { path: "/costs", label: "费用中心", icon: Database }
  ] },
  { label: "管理", items: [
    { path: "/warehouses", label: "仓库管理", icon: WarehouseIcon },
    { path: "/inventory-alerts", label: "库存警告", icon: TriangleAlert },
    { path: "/sku-specs", label: "SKU 规格", icon: PackageCheck },
    { path: "/packing", label: "包装规划", icon: ScanBox },
    { path: "/inventory-thresholds", label: "库存安全线", icon: SlidersHorizontal },
    { path: "/sync", label: "同步中心", icon: RefreshCw }
  ] }
];
const pageNames: Record<string, string> = { "/": "运营总览", "/inventory": "库存中心", "/outbound": "出库管理", "/platform-orders": "平台订单待处理", "/product-pairings": "组合配对", "/fulfillment-audits": "履约核查", "/fulfilled-orders": "出库物流跟踪", "/delivery-evaluation": "快递评价", "/costs": "费用中心", "/warehouses": "仓库管理", "/inventory-alerts": "库存警告", "/sku-specs": "SKU 规格", "/packing": "包装规划", "/inventory-thresholds": "库存安全线", "/sync": "同步中心", "/settings": "系统设置" };

export default function Layout({ children, warehouses, warehouse, onWarehouseChange, online, path, onNavigate }: { children: ReactNode; warehouses: Warehouse[]; warehouse: string; onWarehouseChange: (value: string) => void; online: boolean | null; path: string; onNavigate: (path: string) => void }) {
  const [open, setOpen] = useState(false);
  const go = (path: string) => { onNavigate(path); setOpen(false); };
  return <div className="app-shell">
    <aside className={`sidebar ${open ? "open" : ""}`}>
      <div className="brand"><div className="brand-mark">X</div><div><strong>XLWMS</strong><span>仓库运营中台</span></div><button className="icon-button sidebar-close" onClick={() => setOpen(false)} title="关闭导航"><PanelLeftClose size={19} /></button></div>
      <nav aria-label="主导航">
        {groups.map((group) => <div className="nav-group" key={group.label}><div className="nav-label">{group.label}</div>{group.items.map((item) => { const Icon = item.icon; return <button key={item.path} className={`nav-item ${path === item.path ? "active" : ""}`} onClick={() => go(item.path)}><Icon size={18} /><span>{item.label}</span></button>; })}</div>)}
      </nav>
      <div className="sidebar-footer"><div className="service-state"><span className={`status-dot ${online === null ? "checking" : online ? "" : "offline"}`} />{online === null ? "连接检测中" : online ? "服务运行中" : "服务未连接"}</div><a className="nav-item app-switch-link" href="/temu/"><ExternalLink size={18} /><span>Temu 履约台</span></a><button className={`nav-item ${path === "/settings" ? "active" : ""}`} onClick={() => go("/settings")}><Settings size={18} /><span>系统设置</span></button></div>
    </aside>
    {open && <button className="backdrop" onClick={() => setOpen(false)} aria-label="关闭导航" />}
    <div className="workspace">
      <header className="topbar"><div className="topbar-title"><button className="icon-button mobile-menu" onClick={() => setOpen(true)} title="打开导航"><Menu size={20} /></button><span>XLWMS</span><b>/</b><strong>{pageNames[path] ?? "运营中台"}</strong></div><div className="topbar-actions">{path !== "/fulfillment-audits" && path !== "/fulfilled-orders" && path !== "/delivery-evaluation" && path !== "/costs" && path !== "/platform-orders" && path !== "/product-pairings" && path !== "/packing" && <label className="warehouse-select"><WarehouseIcon size={16} /><select value={warehouse} onChange={(event) => onWarehouseChange(event.target.value)}><option value="">全部仓库</option>{warehouses.map((item) => <option key={item.wh_code} value={item.wh_code}>{item.name || item.wh_code}</option>)}</select><ChevronDown size={14} /></label>}<button className="icon-button notification" title="通知"><Bell size={19} /><span /></button><div className="profile"><div>WM</div><span>管理员</span></div></div></header>
      <main className="main-content">{children}</main>
    </div>
  </div>;
}
