import { Check, LoaderCircle, Pencil, RefreshCw, RotateCcw, Save, Search, X } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../../api";
import { EmptyState, ErrorState, LoadingState, Pagination } from "../../components/Common";
import type { CarrierPolicy, PlatformSKUFulfillmentPolicy, PlatformSKUFulfillmentPolicyPage, WarehouseCarrierPolicies } from "../../types";
import { CarrierPriorityList } from "./WarehouseRulesView";

export default function SKUShippingRulesView({ platform, platformLabel }: { platform: string; platformLabel: string }) {
  const [warehouseKeys, setWarehouseKeys] = useState<string[]>([]);
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
    setLoading(true);
    setError("");
    try {
      const [carrierResult, skuResult] = await Promise.all([
        api.carrierPolicies(platform),
        api.skuFulfillmentPolicies({ platform, q: debouncedQuery, page, pageSize: 30 })
      ]);
      setWarehouseKeys(carrierResult.map((group) => group.warehouse_key));
      setData(skuResult);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "SKU 发货规则加载失败"); }
    finally { setLoading(false); }
  };
  useEffect(() => { setEditing(null); void load(); }, [platform, page, debouncedQuery]);

  const openSKU = async (item: PlatformSKUFulfillmentPolicy) => {
    setEditing(item);
    setDisabled(item.disabled_warehouse_keys || []);
    setSavingKey("load-sku");
    setError("");
    try { setSKUCarriers(await api.carrierPolicies(platform, item.warehouse_sku)); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "SKU 快递覆盖加载失败"); setEditing(null); }
    finally { setSavingKey(""); }
  };
  const saveAllowedWarehouses = async () => {
    if (!editing) return;
    setSavingKey("warehouses");
    setError("");
    try {
      const saved = await api.updateSKUFulfillmentPolicy(platform, editing.warehouse_sku, disabled);
      setEditing(saved);
      setData((current) => current ? { ...current, records: current.records.map((item) => item.warehouse_sku === saved.warehouse_sku ? saved : item) } : current);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "SKU 可发仓保存失败"); }
    finally { setSavingKey(""); }
  };
  const updateSKUCarriers = (warehouseKey: string, carriers: CarrierPolicy[]) => setSKUCarriers((items) => items.map((item) => item.warehouse_key === warehouseKey ? { ...item, carriers } : item));
  const saveSKUCarrier = async (group: WarehouseCarrierPolicies) => {
    if (!editing) return;
    setSavingKey(`sku-${group.warehouse_key}`);
    setError("");
    try {
      const saved = await api.updateCarrierPolicies(platform, group.warehouse_key, group.carriers, editing.warehouse_sku);
      setSKUCarriers((items) => items.map((item) => item.warehouse_key === saved.warehouse_key ? saved : item));
    } catch (reason) { setError(reason instanceof Error ? reason.message : "SKU 快递覆盖保存失败"); }
    finally { setSavingKey(""); }
  };
  const resetSKUCarrier = async (group: WarehouseCarrierPolicies) => {
    if (!editing) return;
    setSavingKey(`sku-${group.warehouse_key}`);
    setError("");
    try {
      await api.resetSKUCarrierPolicies(platform, group.warehouse_key, editing.warehouse_sku);
      setSKUCarriers(await api.carrierPolicies(platform, editing.warehouse_sku));
    } catch (reason) { setError(reason instanceof Error ? reason.message : "恢复平台默认失败"); }
    finally { setSavingKey(""); }
  };

  return <div className="policy-view-content sku-policy-view">
    {error && <ErrorState message={error} onRetry={() => void load()}/>} 
    <div className="filter-bar sku-policy-toolbar"><label className="search-field"><Search size={16}/><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索仓库 SKU 或产品名称"/></label><div><span>{data ? `共 ${data.total} 个 SKU` : ""}</span><button className="icon-button bordered" type="button" title="刷新" onClick={() => void load()}><RefreshCw className={loading ? "spin" : ""} size={17}/></button></div></div>
    {loading ? <LoadingState label="正在加载 SKU 发货规则"/> : data?.records.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table sku-policy-table"><thead><tr><th>仓库 SKU / 产品</th><th>可发仓库</th><th>覆盖范围</th><th>规则状态</th><th>操作</th></tr></thead><tbody>{data.records.map((item) => {
      const allowed = warehouseKeys.filter((key) => !(item.disabled_warehouse_keys || []).includes(key));
      return <tr key={item.warehouse_sku}><td><div className="primary-cell"><strong>{item.warehouse_sku}</strong><small>{item.product_name || "-"}</small></div></td><td><div className="warehouse-tags">{allowed.length ? allowed.map((key) => <span key={key}>{key}</span>) : <span className="danger-tag">全部禁用</span>}</div></td><td><span className="warehouse-count"><b>{allowed.length}</b> / {warehouseKeys.length}</span></td><td><span className={`status-badge ${item.customized ? "running" : ""}`}>{item.customized ? "SKU 单独设置" : "使用平台默认"}</span></td><td><button className="secondary-button sku-policy-edit" type="button" onClick={() => void openSKU(item)}><Pencil size={14}/><span>编辑</span></button></td></tr>;
    })}</tbody></table></div></div> : <EmptyState label="当前没有仓库 SKU"/>}
    {data && data.total > 0 && <Pagination page={data.page} pages={data.pages} total={data.total} onChange={setPage}/>} 
    {editing && <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setEditing(null); }}><section className="modal policy-modal" role="dialog" aria-modal="true" aria-labelledby="policy-modal-title"><header><div><h2 id="policy-modal-title">{editing.warehouse_sku}</h2><p>{platformLabel} · SKU 发货规则</p></div><button className="icon-button" type="button" title="关闭" onClick={() => setEditing(null)}><X size={19}/></button></header><div className="policy-modal-body">
      <section className="allowed-warehouses"><div className="policy-section-title"><div><h3>可发仓库</h3><p>{warehouseKeys.filter((key) => !disabled.includes(key)).length} / {warehouseKeys.length} 个仓库允许自动发货</p></div><button className="primary-button" type="button" disabled={savingKey === "warehouses"} onClick={() => void saveAllowedWarehouses()}>{savingKey === "warehouses" ? <LoaderCircle className="spin" size={15}/> : <Save size={15}/>}<span>{savingKey === "warehouses" ? "保存中" : "保存可发仓"}</span></button></div><div className="warehouse-toggle-grid">{warehouseKeys.map((key) => {
        const allowed = !disabled.includes(key);
        return <label className={allowed ? "active" : ""} key={key}><input type="checkbox" checked={allowed} onChange={(event) => setDisabled((current) => event.target.checked ? current.filter((item) => item !== key) : [...current, key])}/><span className="warehouse-toggle-check">{allowed && <Check size={14}/>}</span><strong>{key}</strong><small>{allowed ? "允许发货" : "禁止发货"}</small></label>;
      })}</div></section>
      <section className="sku-carrier-overrides"><div className="policy-section-title"><div><h3>快递优先级覆盖</h3><p>未单独设置的仓库继续使用平台默认策略</p></div></div>{savingKey === "load-sku" ? <LoadingState label="正在加载 SKU 快递覆盖"/> : <div className="sku-carrier-grid">{skuCarriers.map((group) => <article className="sku-carrier-card" key={group.warehouse_key}><header><div><h4>{group.warehouse_key}</h4><span className={`policy-source-badge ${group.customized ? "custom" : ""}`}>{group.customized ? "SKU 单独设置" : "平台默认"}</span></div><div>{group.customized && <button className="secondary-button" type="button" disabled={savingKey === `sku-${group.warehouse_key}`} onClick={() => void resetSKUCarrier(group)}><RotateCcw size={14}/><span>恢复默认</span></button>}<button className="primary-button" type="button" disabled={savingKey === `sku-${group.warehouse_key}`} onClick={() => void saveSKUCarrier(group)}>{savingKey === `sku-${group.warehouse_key}` ? <LoaderCircle className="spin" size={14}/> : <Save size={14}/>}<span>保存</span></button></div></header><CarrierPriorityList carriers={group.carriers} onChange={(carriers) => updateSKUCarriers(group.warehouse_key, carriers)}/></article>)}</div>}</section>
    </div></section></div>}
  </div>;
}
