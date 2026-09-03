import { ArrowDown, ArrowUp, Check, CircleDollarSign, LoaderCircle, Save, Warehouse as WarehouseIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../../api";
import { EmptyState, ErrorState, LoadingState } from "../../components/Common";
import type { CarrierPolicy, WarehouseCarrierPolicies, WarehouseCarrierRules } from "../../types";

const knownCarriers = ["GOFO", "SWIFTX", "SPEEDX", "YANWEN", "UPS", "USPS", "FEDEX", "UNIUNI"];

export function CarrierPriorityList({ carriers, onChange }: { carriers: CarrierPolicy[]; onChange: (carriers: CarrierPolicy[]) => void }) {
  const move = (index: number, offset: number) => {
    const next = [...carriers];
    const target = index + offset;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target], next[index]];
    onChange(next.map((item, position) => ({ ...item, priority: position + 1 })));
  };

  return <div className="carrier-priority-list">{carriers.map((carrier, index) => <div className="carrier-priority-row" key={carrier.carrier_code}>
    <span className="carrier-rank">{index + 1}</span>
    <strong>{carrier.carrier_code}</strong>
    <label className="policy-toggle"><input type="checkbox" checked={carrier.enabled} onChange={(event) => onChange(carriers.map((item) => item.carrier_code === carrier.carrier_code ? { ...item, enabled: event.target.checked } : item))}/><span/><b>{carrier.enabled ? "启用" : "禁用"}</b></label>
    <div className="carrier-move"><button className="icon-button" type="button" title="上移" disabled={index === 0} onClick={() => move(index, -1)}><ArrowUp size={15}/></button><button className="icon-button" type="button" title="下移" disabled={index === carriers.length - 1} onClick={() => move(index, 1)}><ArrowDown size={15}/></button></div>
  </div>)}</div>;
}

export default function WarehouseRulesView({ platform, mode }: { platform: string; mode: "base-rules" | "selection" }) {
  const [groups, setGroups] = useState<WarehouseCarrierPolicies[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [savingKey, setSavingKey] = useState("");
  const [savedKey, setSavedKey] = useState("");

  const load = async () => {
    setLoading(true);
    setError("");
    try { setGroups(await api.carrierPolicies(platform)); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "仓库快递规则加载失败"); }
    finally { setLoading(false); }
  };
  useEffect(() => { setSavedKey(""); void load(); }, [platform, mode]);

  const updateGroup = (warehouseKey: string, update: (group: WarehouseCarrierPolicies) => WarehouseCarrierPolicies) => {
    setSavedKey((current) => current === warehouseKey ? "" : current);
    setGroups((items) => items.map((item) => item.warehouse_key === warehouseKey ? update(item) : item));
  };
  const updateRules = (warehouseKey: string, update: (rules: WarehouseCarrierRules) => WarehouseCarrierRules) => updateGroup(warehouseKey, (group) => ({ ...group, base_rules: update(group.base_rules) }));
  const save = async (group: WarehouseCarrierPolicies) => {
    setSavingKey(group.warehouse_key);
    setSavedKey("");
    setError("");
    try {
      const saved = await api.updateCarrierPolicies(platform, group.warehouse_key, group.carriers, undefined, group.base_rules);
      setGroups((items) => items.map((item) => item.warehouse_key === saved.warehouse_key ? saved : item));
      setSavedKey(group.warehouse_key);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "仓库快递规则保存失败"); }
    finally { setSavingKey(""); }
  };

  if (loading) return <LoadingState label={mode === "base-rules" ? "正在加载基础快递限制" : "正在加载快递选择算法"}/>;
  return <div className="policy-view-content">
    {error && <ErrorState message={error} onRetry={() => void load()}/>} 
    {!groups.length ? <EmptyState label="当前平台没有仓库快递规则"/> : <div className="warehouse-policy-grid">{groups.map((group) => {
      const allowedCodes = new Set(group.base_rules.allowed_carrier_codes);
      const enabledCount = group.carriers.filter((carrier) => carrier.enabled).length;
      return <article className="warehouse-policy-card" key={group.warehouse_key}>
        <header className="warehouse-policy-header"><div className="warehouse-policy-identity"><span><WarehouseIcon size={17}/></span><div><h2>{group.warehouse_key}</h2><p>平台默认规则</p></div></div><div className="policy-header-summary">{mode === "base-rules" ? <><span>{allowedCodes.size} 家可用</span><span>{group.base_rules.allow_signature ? "允许签名" : "禁止签名"}</span></> : <><span>{group.base_rules.selection_mode === "lowest_price" ? "严格最低价" : `价差 ${group.base_rules.max_price_delta}`}</span><span>{enabledCount} 家参与选择</span></>}</div></header>
        {mode === "base-rules" ? <div className="warehouse-policy-body">
          <section className="policy-field-section"><div className="policy-field-heading"><div><h3>允许快递</h3><p>未勾选的快递不会进入报价候选</p></div><span>{allowedCodes.size} / {knownCarriers.length}</span></div><div className="carrier-allow-grid">{knownCarriers.map((code) => {
            const allowed = allowedCodes.has(code);
            return <label className={allowed ? "active" : ""} key={code}><input type="checkbox" checked={allowed} onChange={(event) => updateRules(group.warehouse_key, (rules) => ({ ...rules, allowed_carrier_codes: event.target.checked ? [...rules.allowed_carrier_codes, code] : rules.allowed_carrier_codes.filter((item) => item !== code) }))}/><span className="carrier-check">{allowed && <Check size={13}/>}</span><strong>{code}</strong></label>;
          })}</div></section>
          <section className="policy-field-section policy-inline-fields"><label><span>签名服务</span><select value={group.base_rules.allow_signature ? "allow" : "deny"} onChange={(event) => updateRules(group.warehouse_key, (rules) => ({ ...rules, allow_signature: event.target.value === "allow" }))}><option value="deny">禁止签名服务</option><option value="allow">允许签名服务</option></select></label><label><span>允许报价币种</span><input value={group.base_rules.allowed_currency_codes.join(", ")} placeholder="不限币种" onChange={(event) => updateRules(group.warehouse_key, (rules) => ({ ...rules, allowed_currency_codes: event.target.value.split(",").map((item) => item.trim().toUpperCase()).filter(Boolean) }))}/></label></section>
        </div> : <div className="warehouse-policy-body">
          <section className="policy-field-section"><div className="policy-field-heading"><div><h3>选价方式</h3><p>当前仓库获取多家有效报价后的选择逻辑</p></div><CircleDollarSign size={18}/></div><div className="policy-mode-control"><button type="button" className={group.base_rules.selection_mode === "lowest_price" ? "active" : ""} onClick={() => updateRules(group.warehouse_key, (rules) => ({ ...rules, selection_mode: "lowest_price" }))}>严格最低价</button><button type="button" className={group.base_rules.selection_mode === "carrier_priority_within_delta" ? "active" : ""} onClick={() => updateRules(group.warehouse_key, (rules) => ({ ...rules, selection_mode: "carrier_priority_within_delta" }))}>价差内按优先级</button></div><div className="selection-number-grid"><label><span>最高价差</span><div><input type="number" min="0" step="0.01" disabled={group.base_rules.selection_mode === "lowest_price"} value={group.base_rules.max_price_delta} onChange={(event) => updateRules(group.warehouse_key, (rules) => ({ ...rules, max_price_delta: Number(event.target.value) }))}/><small>报价币种</small></div></label><label><span>同价仓库优先级</span><div><input type="number" min="1" max="100" step="1" value={group.base_rules.warehouse_tie_priority} onChange={(event) => updateRules(group.warehouse_key, (rules) => ({ ...rules, warehouse_tie_priority: Number(event.target.value) }))}/><small>数字越小越优先</small></div></label></div></section>
          <section className="policy-field-section carrier-priority-section"><div className="policy-field-heading"><div><h3>快递优先级</h3><p>用于价差范围内及同价报价的排序</p></div><span>{enabledCount} 家启用</span></div><CarrierPriorityList carriers={group.carriers} onChange={(carriers) => updateGroup(group.warehouse_key, (item) => ({ ...item, carriers }))}/></section>
        </div>}
        <footer className="warehouse-policy-footer"><span className={savedKey === group.warehouse_key ? "saved visible" : "saved"}><Check size={14}/>已保存</span><button className="primary-button" type="button" disabled={savingKey === group.warehouse_key} onClick={() => void save(group)}>{savingKey === group.warehouse_key ? <LoaderCircle className="spin" size={15}/> : <Save size={15}/>}<span>{savingKey === group.warehouse_key ? "保存中" : "保存规则"}</span></button></footer>
      </article>;
    })}</div>}
  </div>;
}
