import { ListOrdered, PackageSearch, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { PageHeader } from "../components/Common";
import SKUShippingRulesView from "./shipping-policies/SKUShippingRulesView";
import WarehouseRulesView from "./shipping-policies/WarehouseRulesView";
import "./ShippingPoliciesPage.css";

const platforms = ["temu", "shein"];
const platformLabel = (value: string) => value === "shein" ? "SHEIN" : "Temu";

export type ShippingPolicyView = "base-rules" | "selection" | "sku-rules";

const views = [
  { key: "base-rules", path: "/shipping-policies/base-rules", label: "基础快递限制", title: "基础快递限制", subtitle: "按平台和仓库维护可用快递、签名服务及报价币种", scope: "平台 + 仓库", icon: ShieldCheck },
  { key: "selection", path: "/shipping-policies/selection", label: "快递选择算法", title: "快递选择算法", subtitle: "按平台和仓库维护选价方式、价差范围与快递优先级", scope: "平台 + 仓库", icon: ListOrdered },
  { key: "sku-rules", path: "/shipping-policies/sku-rules", label: "SKU 发货规则", title: "SKU 发货规则", subtitle: "按平台和 SKU 维护可发仓库与快递覆盖", scope: "平台 + SKU", icon: PackageSearch }
] as const;

export default function ShippingPoliciesPage({ view, onNavigate }: { view: ShippingPolicyView; onNavigate: (path: string) => void }) {
  const [platform, setPlatform] = useState(() => localStorage.getItem("xlwms-policy-platform") || "temu");
  const choosePlatform = (value: string) => {
    setPlatform(value);
    localStorage.setItem("xlwms-policy-platform", value);
  };
  const current = views.find((item) => item.key === view) ?? views[0];

  return <>
    <PageHeader title={current.title} subtitle={current.subtitle} />
    <div className="policy-workspace-bar">
      <nav className="policy-view-nav" aria-label="发货策略目录">{views.map((item) => { const Icon = item.icon; return <button key={item.key} className={view === item.key ? "active" : ""} onClick={() => onNavigate(item.path)}><Icon size={17}/><span>{item.label}</span></button>; })}</nav>
      <div className="policy-context"><span className="policy-scope">{current.scope}</span><div className="segmented-control" aria-label="选择平台">{platforms.map((item) => <button key={item} className={platform === item ? "active" : ""} onClick={() => choosePlatform(item)}>{platformLabel(item)}</button>)}</div></div>
    </div>
    {view === "sku-rules" ? <SKUShippingRulesView platform={platform} platformLabel={platformLabel(platform)} /> : <WarehouseRulesView platform={platform} mode={view} />}
  </>;
}
