import { ArrowDown, ArrowUp, Pencil, RefreshCw, RotateCcw, Save, Search, X } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination } from "../components/Common";
import type { CarrierPolicy, PlatformSKUFulfillmentPolicy, PlatformSKUFulfillmentPolicyPage, WarehouseCarrierPolicies, WarehouseCarrierRules } from "../types";
import "./ShippingPoliciesPage.css";

const platforms = ["temu", "shein"];
const warehouses = ["DPS002", "ARP_EAST", "DPS004", "ARP_WEST"];
const knownCarriers = ["GOFO", "SWIFTX", "SPEEDX", "YANWEN", "UPS", "USPS", "FEDEX", "UNIUNI"];
const platformLabel = (value: string) => value === "shein" ? "SHEIN" : "Temu";

function CarrierEditor({ group, saving, onChange, onRulesChange, onSave, onReset }: {
  group: WarehouseCarrierPolicies;
  saving: boolean;
  onChange: (carriers: CarrierPolicy[]) => void;
  onRulesChange?: (rules: WarehouseCarrierRules) => void;
  onSave: () => void;
  onReset?: () => void;
}) {
  const move = (index: number, offset: number) => {
    const next = [...group.carriers];
    const target = index + offset;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target], next[index]];
    onChange(next.map((item, position) => ({ ...item, priority: position + 1 })));
  };
  return <section className="carrier-editor">
    <header>
      <div><strong>{group.warehouse_key}</strong><small>{group.customized ? "SKU 单独设置" : "平台默认"}</small></div>
      <div className="carrier-actions">
        {onReset && group.customized && <button className="icon-button bordered" type="button" title="恢复平台默认" onClick={onReset}><RotateCcw size={15}/></button>}
        <button className="icon-button bordered" type="button" title="保存快递策略" disabled={saving} onClick={onSave}><Save size={15}/></button>
      </div>
    </header>
    {onRulesChange && <div className="base-rule-editor">
      <div className="base-rule-heading"><strong>基础限制</strong><small>平台 + 仓库生效，SKU 不可绕过</small></div>
      <div className="base-carrier-toggles">{knownCarriers.map((code) => {
        const allowed = group.base_rules.allowed_carrier_codes.includes(code);
        return <label key={code}><input type="checkbox" checked={allowed} onChange={(event) => onRulesChange({ ...group.base_rules, allowed_carrier_codes: event.target.checked ? [...group.base_rules.allowed_carrier_codes, code] : group.base_rules.allowed_carrier_codes.filter((item) => item !== code) })}/><span>{code}</span></label>;
      })}</div>
      <div className="base-rule-grid">
        <label><span>签名服务</span><select value={group.base_rules.allow_signature ? "allow" : "deny"} onChange={(event) => onRulesChange({ ...group.base_rules, allow_signature: event.target.value === "allow" })}><option value="deny">禁止</option><option value="allow">允许</option></select></label>
        <label><span>允许报价币种</span><input value={group.base_rules.allowed_currency_codes.join(", ")} placeholder="留空表示不限" onChange={(event) => onRulesChange({ ...group.base_rules, allowed_currency_codes: event.target.value.split(",").map((item) => item.trim().toUpperCase()).filter(Boolean) })}/></label>
        <label><span>选价算法</span><select value={group.base_rules.selection_mode} onChange={(event) => onRulesChange({ ...group.base_rules, selection_mode: event.target.value as WarehouseCarrierRules["selection_mode"] })}><option value="lowest_price">严格最低价</option><option value="carrier_priority_within_delta">价差内按快递优先级</option></select></label>
        <label><span>最高价差（报价币种）</span><input type="number" min="0" step="0.01" disabled={group.base_rules.selection_mode === "lowest_price"} value={group.base_rules.max_price_delta} onChange={(event) => onRulesChange({ ...group.base_rules, max_price_delta: Number(event.target.value) })}/></label>
        <label><span>同价仓库优先级</span><input type="number" min="1" max="100" step="1" value={group.base_rules.warehouse_tie_priority} onChange={(event) => onRulesChange({ ...group.base_rules, warehouse_tie_priority: Number(event.target.value) })}/></label>
      </div>
    </div>}
    <div className="carrier-list">{group.carriers.map((carrier, index) => <div className="carrier-row" key={carrier.carrier_code}>
      <span className="carrier-rank">{carrier.priority}</span><strong>{carrier.carrier_code}</strong>
      <label className="switch-line"><input type="checkbox" checked={carrier.enabled} onChange={(event) => onChange(group.carriers.map((item) => item.carrier_code === carrier.carrier_code ? { ...item, enabled: event.target.checked } : item))}/><span>{carrier.enabled ? "启用" : "禁用"}</span></label>
      <div className="carrier-move"><button className="icon-button" type="button" title="上移" disabled={index === 0} onClick={() => move(index, -1)}><ArrowUp size={14}/></button><button className="icon-button" type="button" title="下移" disabled={index === group.carriers.length - 1} onClick={() => move(index, 1)}><ArrowDown size={14}/></button></div>
    </div>)}</div>
  </section>;
}

export default function ShippingPoliciesPage() {
  const [platform, setPlatform] = useState(() => localStorage.getItem("xlwms-policy-platform") || "temu");
  const [defaults, setDefaults] = useState<WarehouseCarrierPolicies[]>([]);
  const [data, setData] = useState<PlatformSKUFulfillmentPolicyPage | null>(null);
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [savingKey, setSavingKey] = useState("");
  const [editing, setEditing] = useState<PlatformSKUFulfillmentPolicy | null>(null);
  const [skuCarriers, setSKUCarriers] = useState<WarehouseCarrierPolicies[]>([]);
  const [disabled, setDisabled] = useState<string[]>([]);

  useEffect(() => {
    const timer = window.setTimeout(() => { setDebouncedQuery(query.trim()); setPage(1); }, 250);
    return () => window.clearTimeout(timer);
  }, [query]);

  const load = async () => {
    setLoading(true); setError("");
    try {
      const [carrierResult, skuResult] = await Promise.all([
        api.carrierPolicies(platform),
        api.skuFulfillmentPolicies({ platform, q: debouncedQuery, page, pageSize: 30 })
      ]);
      setDefaults(carrierResult); setData(skuResult);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "发货策略加载失败"); }
    finally { setLoading(false); }
  };
  useEffect(() => { void load(); }, [platform, page, debouncedQuery]);

  const choosePlatform = (value: string) => {
    setPlatform(value); setPage(1); setEditing(null); localStorage.setItem("xlwms-policy-platform", value);
  };
  const updateGroup = (groups: WarehouseCarrierPolicies[], key: string, carriers: CarrierPolicy[]) => groups.map((group) => group.warehouse_key === key ? { ...group, carriers } : group);
  const updateRules = (groups: WarehouseCarrierPolicies[], key: string, baseRules: WarehouseCarrierRules) => groups.map((group) => group.warehouse_key === key ? { ...group, base_rules: baseRules } : group);
  const saveDefault = async (group: WarehouseCarrierPolicies) => {
    setSavingKey(`default-${group.warehouse_key}`); setError("");
    try {
      const saved = await api.updateCarrierPolicies(platform, group.warehouse_key, group.carriers, undefined, group.base_rules);
      setDefaults((items) => items.map((item) => item.warehouse_key === saved.warehouse_key ? saved : item));
    } catch (reason) { setError(reason instanceof Error ? reason.message : "平台默认快递策略保存失败"); }
    finally { setSavingKey(""); }
  };
  const openSKU = async (item: PlatformSKUFulfillmentPolicy) => {
    setEditing(item); setDisabled(item.disabled_warehouse_keys || []); setSavingKey("load-sku");
    try { setSKUCarriers(await api.carrierPolicies(platform, item.warehouse_sku)); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "SKU 快递策略加载失败"); setEditing(null); }
    finally { setSavingKey(""); }
  };
  const saveAllowedWarehouses = async () => {
    if (!editing) return;
    setSavingKey("warehouses");
    try {
      const saved = await api.updateSKUFulfillmentPolicy(platform, editing.warehouse_sku, disabled);
      setEditing(saved);
      setData((current) => current ? { ...current, records: current.records.map((item) => item.warehouse_sku === saved.warehouse_sku ? saved : item) } : current);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "SKU 可发仓保存失败"); }
    finally { setSavingKey(""); }
  };
  const saveSKUCarrier = async (group: WarehouseCarrierPolicies) => {
    if (!editing) return;
    setSavingKey(`sku-${group.warehouse_key}`);
    try {
      const saved = await api.updateCarrierPolicies(platform, group.warehouse_key, group.carriers, editing.warehouse_sku);
      setSKUCarriers((items) => items.map((item) => item.warehouse_key === saved.warehouse_key ? saved : item));
    } catch (reason) { setError(reason instanceof Error ? reason.message : "SKU 快递策略保存失败"); }
    finally { setSavingKey(""); }
  };
  const resetSKUCarrier = async (group: WarehouseCarrierPolicies) => {
    if (!editing) return;
    setSavingKey(`sku-${group.warehouse_key}`);
    try {
      await api.resetSKUCarrierPolicies(platform, group.warehouse_key, editing.warehouse_sku);
      setSKUCarriers(await api.carrierPolicies(platform, editing.warehouse_sku));
    } catch (reason) { setError(reason instanceof Error ? reason.message : "恢复平台默认失败"); }
    finally { setSavingKey(""); }
  };

  return <>
    <PageHeader title="发货策略" subtitle="按平台、仓库和 SKU 维护发货限制与快递选择" />
    <div className="filter-bar policy-platform-bar"><div className="segmented-control" aria-label="选择平台">{platforms.map((item) => <button key={item} className={platform === item ? "active" : ""} onClick={() => choosePlatform(item)}>{platformLabel(item)}</button>)}</div><span className="table-note">规则对 {platformLabel(platform)} 全部店铺生效</span></div>
    {error && <ErrorState message={error} onRetry={() => void load()} />}
    <div className="policy-section-title"><div><h2>平台 + 仓库基础快递规则</h2><p>控制基础白名单、签名/币种限制和自动选价算法；SKU 可覆盖优先级，但不能绕过基础限制</p></div></div>
    {loading ? <LoadingState label="正在加载发货策略" /> : <div className="carrier-grid">{defaults.map((group) => <CarrierEditor key={group.warehouse_key} group={group} saving={savingKey === `default-${group.warehouse_key}`} onChange={(carriers) => setDefaults((items) => updateGroup(items, group.warehouse_key, carriers))} onRulesChange={(rules) => setDefaults((items) => updateRules(items, group.warehouse_key, rules))} onSave={() => void saveDefault(group)} />)}</div>}
    <div className="policy-section-title sku-policy-heading"><div><h2>SKU 发货策略</h2><p>设置可发仓，并按仓覆盖快递策略</p></div></div>
    <div className="filter-bar"><label className="search-field"><Search size={16}/><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索仓库 SKU 或产品名称"/></label><button className="icon-button bordered" title="刷新" onClick={() => void load()}><RefreshCw size={17}/></button></div>
    {!loading && data?.records.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table"><thead><tr><th>仓库 SKU / 产品</th><th>可发仓</th><th>规则状态</th><th>操作</th></tr></thead><tbody>{data.records.map((item) => {
      const allowed = warehouses.filter((key) => !(item.disabled_warehouse_keys || []).includes(key));
      return <tr key={item.warehouse_sku}><td><div className="primary-cell"><strong>{item.warehouse_sku}</strong><small>{item.product_name || "-"}</small></div></td><td><div className="warehouse-tags">{allowed.length ? allowed.map((key) => <span key={key}>{key}</span>) : <span className="danger-tag">全部禁用</span>}</div></td><td><span className={`status-badge ${item.customized ? "running" : ""}`}>{item.customized ? "SKU 单独设置" : "全部仓可发"}</span></td><td><button className="icon-button bordered" title="编辑 SKU 发货策略" onClick={() => void openSKU(item)}><Pencil size={15}/></button></td></tr>;
    })}</tbody></table></div></div> : !loading && <EmptyState label="当前没有仓库 SKU" />}
    {data && data.total > 0 && <Pagination page={data.page} pages={data.pages} total={data.total} onChange={setPage}/>}
    {editing && <div className="modal-backdrop" role="presentation"><section className="modal policy-modal" role="dialog" aria-modal="true" aria-labelledby="policy-modal-title"><header><div><h2 id="policy-modal-title">SKU 发货策略</h2><p>{editing.warehouse_sku} · {platformLabel(platform)}</p></div><button className="icon-button" title="关闭" onClick={() => setEditing(null)}><X size={19}/></button></header><div className="policy-modal-body">
      <section className="allowed-warehouses"><div className="policy-section-title"><div><h3>可发仓库</h3><p>关闭后，该 SKU 不会从对应仓自动发货</p></div><button className="primary-button" disabled={savingKey === "warehouses"} onClick={() => void saveAllowedWarehouses()}><Save size={15}/>保存可发仓</button></div><div className="warehouse-toggle-grid">{warehouses.map((key) => {
        const allowed = !disabled.includes(key);
        return <label key={key}><input type="checkbox" checked={allowed} onChange={(event) => setDisabled((current) => event.target.checked ? current.filter((item) => item !== key) : [...current, key])}/><span>{key}</span><small>{allowed ? "允许发货" : "禁止发货"}</small></label>;
      })}</div></section>
      <section><div className="policy-section-title"><div><h3>SKU 快递覆盖</h3><p>每个发货仓可独立排序和启停</p></div></div>{savingKey === "load-sku" ? <LoadingState label="正在加载 SKU 快递策略"/> : <div className="carrier-grid">{skuCarriers.map((group) => <CarrierEditor key={group.warehouse_key} group={group} saving={savingKey === `sku-${group.warehouse_key}`} onChange={(carriers) => setSKUCarriers((items) => updateGroup(items, group.warehouse_key, carriers))} onSave={() => void saveSKUCarrier(group)} onReset={() => void resetSKUCarrier(group)}/>)}</div>}</section>
    </div></section></div>}
  </>;
}
