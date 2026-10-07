import { useEffect, useState } from "react";
import { CheckCircle2, CircleAlert, LoaderCircle, RefreshCw, Settings2, ShieldCheck, X } from "lucide-react";
import { api } from "../api";
import { dateTime, EmptyState, ErrorState } from "../components/Common";
import type { ConsoleCredentials, FulfillmentWarehouse, PlatformWarehouseOption, WarehouseAPICredentialGroup, WarehouseBinding, WarehouseConfigurationHistory, WarehouseReadiness } from "../types";
import "./FulfillmentWarehouses.css";

type Tab = "inventory" | "temu" | "shein" | "carriers" | "history";
const tabs: {key: Tab; name: string}[] = [{key:"inventory",name:"库存与发货账号"},{key:"temu",name:"Temu"},{key:"shein",name:"SHEIN"},{key:"carriers",name:"物流规则"},{key:"history",name:"操作记录"}];
const actions: Record<string,string> = {save_or_pause_shop:"保存配置 / 暂停店铺",check:"检查配置",activate_shop:"启用店铺",pause_warehouse:"暂停仓库"};
function stateLabel(b: WarehouseBinding): string {
 if (!b.shop_enabled) return "店铺已停用";
 if (!b.platform_warehouse_id) return "待配置";
 if (b.effective) return "已启用";
 if (b.enabled) return "仓库发货已暂停";
 if (b.verified_at) return "可启用 / 已暂停";
 return "待验证";
}

export default function FulfillmentWarehouses({onChanged}: {onChanged: () => Promise<void>}) {
 const [warehouses,setWarehouses]=useState<FulfillmentWarehouse[]>([]);
 const [selectedKey,setSelectedKey]=useState("");
 const [bindings,setBindings]=useState<WarehouseBinding[]>([]);
 const [credentials,setCredentials]=useState<WarehouseAPICredentialGroup[]>([]);
 const [inProgress,setInProgress]=useState<number | null>(null);
 const [history,setHistory]=useState<WarehouseConfigurationHistory[]>([]);
 const [auth,setAuth]=useState<ConsoleCredentials>({username:"",password:""});
 const [tab,setTab]=useState<Tab>("inventory");
 const [shop,setShop]=useState("");
 const [draft,setDraft]=useState({id:"",name:""});
 const [order,setOrder]=useState("");
 const [options,setOptions]=useState<PlatformWarehouseOption[]>([]);
 const [check,setCheck]=useState<WarehouseReadiness | null>(null);
 const [busy,setBusy]=useState("");
 const [loading,setLoading]=useState(true);
 const [error,setError]=useState("");
 const [message,setMessage]=useState("");
 const selected=warehouses.find(w=>w.warehouse_key===selectedKey);
 const platformBindings=bindings.filter(b=>b.platform===tab);
 const binding=platformBindings.find(b=>b.shop_code===shop);
 const dirty=!!binding && (draft.id!==binding.platform_warehouse_id || draft.name!==binding.platform_warehouse_name);
 const coveredCredentials=credentials.filter(c=>c.warehouse_codes.includes(selected?.wh_code || ""));
 const inventoryCredentials=coveredCredentials.length?coveredCredentials:credentials;
 const authenticated=!!auth.username && !!auth.password;

 async function refresh(key=selectedKey) {
  const list=await api.fulfillmentWarehouses();setWarehouses(list);
  if(key) {
   const [b,c,h]=await Promise.all([api.warehouseBindings(key),api.warehouseAPICredentials(),api.warehouseConfigurationHistory(key)]);
   setBindings(b);setCredentials(c);setHistory(h);
   const activity=await api.warehouseActivity(key).catch(()=>null);setInProgress(activity?.in_progress ?? null);
  }
 }
 useEffect(()=>{let current=true;api.fulfillmentWarehouses().then(items=>{if(current)setWarehouses(items);}).catch(e=>{if(current)setError(e.message);}).finally(()=>{if(current)setLoading(false);});return()=>{current=false;};},[]);
 useEffect(()=>{setOptions([]);setCheck(null);setOrder("");if(tab!=="temu"&&tab!=="shein")return;setShop(bindings.find(b=>b.platform===tab)?.shop_code || "");},[tab,selectedKey]);
 useEffect(()=>{setDraft({id:binding?.platform_warehouse_id || "",name:binding?.platform_warehouse_name || ""});setCheck(null);setOptions([]);},[binding?.shop_code,binding?.platform,binding?.revision,selectedKey]);
 useEffect(()=>{if((tab==="temu"||tab==="shein")&&!shop&&platformBindings.length)setShop(platformBindings[0].shop_code);},[platformBindings.length,shop,tab]);
 async function run(label: string,operation: ()=>Promise<void>) {
  setBusy(label);setError("");setMessage("");try{await operation();}catch(e){setError(e instanceof Error?e.message:"操作失败");}finally{setBusy("");}
 }
 async function open(w: FulfillmentWarehouse) {
  await run("加载配置",async()=>{setSelectedKey(w.warehouse_key);setTab("inventory");setShop("");setBindings([]);setOptions([]);setCheck(null);await refresh(w.warehouse_key);});
 }
 async function save() {
  if(!selected||!binding)return;
  await run("保存配置",async()=>{await api.saveWarehouseBinding(selectedKey,binding,draft.id.trim(),draft.name.trim(),auth);setCheck(null);await refresh();setMessage("配置已保存；该店铺保持暂停，检查通过后可启用。");});
 }
 async function inspect(activate=false) {
  if(!selected||!binding)return;
  await run(activate?"启用店铺":"检查配置",async()=>{const result=await api.warehouseReadiness(selectedKey,binding,order.trim(),auth,activate);await refresh();setCheck(result);setMessage(activate?"已启用仓库和所选店铺，配置立即生效。":result.ready?"配置检查通过，可以启用此店铺。":"检查发现缺项，请按下方提示处理。");});
 }
 async function pauseShop() {
  if(!binding)return;
  await run("暂停店铺",async()=>{await api.saveWarehouseBinding(selectedKey,binding,binding.platform_warehouse_id,binding.platform_warehouse_name,auth);await refresh();setCheck(null);setMessage("已暂停该店铺的新发货，已购面单继续履约。");});
 }
 const canActivate=check?.ready && check.revision===binding?.revision && check.warehouse_revision===binding?.warehouse_revision && !dirty;
 return <section className="warehouse-section fulfillment-warehouses">
  <div className="warehouse-section-heading"><div><h2>发货仓库</h2><p>仓库总开关与平台店铺分别管理；配置保存后动态生效。</p></div><button className="secondary-button" disabled={!!busy} onClick={()=>void run("刷新",async()=>{await refresh();})}><RefreshCw size={15}/>刷新</button></div>
  {!selectedKey&&error&&<ErrorState message={error}/>}
  {loading?<p>正在读取发货仓库…</p>:warehouses.length?<div className="table-panel table-scroll"><table className="data-table"><thead><tr><th>物理仓库</th><th>库存连接</th><th>发货总开关</th><th>已启用店铺</th><th>允许物流</th><th>操作</th></tr></thead><tbody>{warehouses.map(w=><tr key={w.warehouse_key}>
   <td><div className="primary-cell"><strong>{w.display_name}</strong><small>{w.wh_code}</small></div></td><td><span className={`status-badge ${w.inventory_connected?"running":""}`}>{w.inventory_connected?"已连接":"待连接 / 已暂停"}</span></td>
   <td><span className={`status-badge ${w.fulfillment_enabled?"running":""}`}>{w.fulfillment_enabled?"已开启":"已暂停"}</span></td><td>{w.enabled_shop_count} 个</td><td>{w.allowed_carrier_codes?.join(" / ") || "按平台发货策略"}</td>
   <td><button className="secondary-button" disabled={!!busy} onClick={()=>void open(w)}><Settings2 size={15}/>配置与检查</button></td>
  </tr>)}</tbody></table></div>:<EmptyState label="暂无发货仓库"/>}
  {selected&&<div className="modal-backdrop warehouse-management-backdrop"><section className="warehouse-management-panel" role="dialog" aria-modal="true" aria-labelledby="fulfillment-warehouse-title">
   <header><div><h2 id="fulfillment-warehouse-title">{selected.display_name}</h2><p>{selected.wh_code} · 发货总开关{selected.fulfillment_enabled?"已开启":"已暂停"}</p></div><button className="secondary-button" disabled={!!busy} onClick={()=>void run("刷新状态",async()=>{await refresh();await onChanged();setMessage("配置与同步状态已刷新。");})}><RefreshCw size={14}/>刷新状态</button><button className="icon-button" disabled={!!busy} title="关闭配置" onClick={()=>{setSelectedKey("");setAuth({username:"",password:""});setError("");setMessage("");}}><X size={20}/></button></header>
   <div className="warehouse-management-auth"><ShieldCheck size={18}/><span>管理操作认证</span><input aria-label="管理用户名" autoComplete="username" placeholder="管理用户名" value={auth.username} disabled={!!busy} onChange={e=>setAuth({...auth,username:e.target.value})}/><input aria-label="管理密码" type="password" autoComplete="current-password" placeholder="管理密码" value={auth.password} disabled={!!busy} onChange={e=>setAuth({...auth,password:e.target.value})}/></div>
   <nav aria-label="仓库配置模块">{tabs.map(t=><button key={t.key} className={tab===t.key?"active":""} disabled={!!busy} onClick={()=>{setTab(t.key);setError("");setMessage("");}}>{t.name}</button>)}</nav>
   <main>
    {error&&<ErrorState message={error}/>}{message&&<p className="warehouse-operation-message" role="status">{message}</p>}
    {tab==="inventory"&&<>
     <h3>库存与发货账号</h3><p>沿用已有 API 凭据与 OMS 账号绑定。库存为零可以完成配置，实际订单仍按库存规则选仓。</p>
     {!coveredCredentials.length&&<p>尚未发现该仓。可同步以下已有凭据，重新发现它们覆盖的仓库；不需要新建仓库专属 key。</p>}
     {inventoryCredentials.map(c=><div className="warehouse-credential-card" key={c.key}><strong>{c.label}</strong><p>发货账号：{c.oms_account_label || "待绑定"} · SKU {c.sku_count} 个 · {c.inventory_sync_status==="ready"?"库存范围已就绪":"待同步"}</p><small>最近验证：{dateTime(c.last_verified_at || undefined)}</small><button className="secondary-button" disabled={!!busy||!authenticated} onClick={()=>void run("同步数据",async()=>{await api.syncWarehouseAPIInventory(c.key);await refresh();setMessage("已触发库存范围同步，完成后点击上方“刷新状态”。");})}><RefreshCw size={14}/>同步数据范围</button></div>)}
     {!credentials.length&&<EmptyState label="尚未发现覆盖该仓的 API 数据范围，请先同步已有凭据。"/>}
     {!selected.inventory_connected&&selected.inventory_registered&&<button className="secondary-button" disabled={!!busy||!authenticated} onClick={()=>void run("恢复连接",async()=>{await api.setWarehouseActive(selected.wh_code,true);await onChanged();await refresh();setMessage("库存连接已恢复，请重新检查配置。");})}>恢复库存数据连接</button>}
     <a className="secondary-button" href={`${import.meta.env.BASE_URL}accounts`}>打开账号管理</a>
     <h3>平台店铺状态</h3><div className="table-scroll"><table className="data-table"><thead><tr><th>平台 / 店铺</th><th>平台仓</th><th>状态</th></tr></thead><tbody>{bindings.map(b=><tr key={`${b.platform}/${b.shop_code}`}><td>{b.platform.toUpperCase()} · {b.shop_name}</td><td>{b.platform_warehouse_name || b.platform_warehouse_id || "待配置"}</td><td>{stateLabel(b)}</td></tr>)}</tbody></table></div>
    </>}
    {(tab==="temu"||tab==="shein")&&<>
     <div className="warehouse-binding-form"><label>店铺<select aria-label="平台店铺" value={shop} disabled={!!busy} onChange={e=>{setShop(e.target.value);setOrder("");setCheck(null);setOptions([]);}}>{platformBindings.map(b=><option key={b.shop_code} value={b.shop_code}>{b.shop_name}{!b.shop_enabled?"（已停用）":""}</option>)}</select></label>
     {binding&&<><p>当前状态：<strong>{stateLabel(binding)}</strong> · 最近验证 {dateTime(binding.verified_at || undefined)}</p>
      {tab==="shein"&&<label>验证订单（可留空自动查找）<input aria-label="SHEIN 验证订单" placeholder="支持独立履约的订单号" value={order} disabled={!!busy} onChange={e=>{setOrder(e.target.value);setCheck(null);setOptions([]);}}/><small>只查询仓库权限，不购买面单。</small></label>}
      <button className="secondary-button" disabled={!!busy||!authenticated||!binding.shop_enabled} onClick={()=>void run("同步平台仓",async()=>{setOptions(await api.platformWarehouseOptions(selectedKey,binding,order.trim(),auth));setMessage("已同步当前店铺的平台仓，请核对实际物理仓后选择。");})}><RefreshCw size={15}/>同步平台仓库</button>
      {options.length>0&&<label>当前店铺的平台仓<select aria-label="选择平台仓库" value={draft.id} disabled={!!busy} onChange={e=>{const o=options.find(x=>x.id===e.target.value);setDraft({id:e.target.value,name:o?.name || ""});setCheck(null);}}><option value="">请选择对应的实际仓库</option>{options.map(o=><option key={o.id} value={o.id} disabled={!o.can_ship||(!!o.oms_code&&o.oms_code!==selected.wh_code)}>{o.name} · {o.id}{!o.can_ship?"（当前不可发货）":""}</option>)}</select></label>}
      <label>平台仓 ID<input aria-label="平台仓 ID" placeholder="从平台取得的真实仓 ID" value={draft.id} disabled={!!busy} onChange={e=>{setDraft({...draft,id:e.target.value});setCheck(null);}}/></label>
      <label>平台仓名称<input aria-label="平台仓名称" value={draft.name} disabled={!!busy} onChange={e=>{setDraft({...draft,name:e.target.value});setCheck(null);}}/></label>
      <p>保存映射会暂停该店铺的新发货，检查通过后再启用；其他店铺各自管理。</p>
      <div className="warehouse-binding-actions"><button className="secondary-button" disabled={!!busy||!authenticated||!draft.id.trim()} onClick={()=>void save()}>保存草稿</button><button className="secondary-button" disabled={!!busy||!authenticated||!binding.platform_warehouse_id||dirty} onClick={()=>void inspect()}>检查配置</button><button className="primary-button" disabled={!!busy||!authenticated||!canActivate||!binding.shop_enabled} onClick={()=>void inspect(true)}>{selected.fulfillment_enabled?"启用此店铺":"启用仓库和此店铺"}</button>{binding.enabled&&<button className="secondary-button" disabled={!!busy||!authenticated} onClick={()=>void pauseShop()}>暂停此店铺</button>}</div>
     </>}
     </div>
     {check&&<div className="warehouse-check-list" aria-label="配置检查结果">{check.checks.map(c=><div key={c.code} className={c.passed?"passed":"pending"}>{c.passed?<CheckCircle2 size={18}/>:<CircleAlert size={18}/>}<div><strong>{c.label} · {c.passed?"通过":"待处理"}</strong><p>{c.message}</p>{c.code==="oms_permission"&&!c.passed&&<a href={`${import.meta.env.BASE_URL}accounts`}>前往账号管理</a>}{c.code==="carriers"&&!c.passed&&<a href={`${import.meta.env.BASE_URL}shipping-policies/base-rules`}>前往发货策略</a>}</div></div>)}</div>}
    </>}
    {tab==="carriers"&&<><h3>允许物流</h3><p>{selected.allowed_carrier_codes?.join(" / ") || "根据平台与 SKU 发货策略决定"}</p><p>有效范围同时受物理仓能力、平台策略、SKU 规则和订单实时渠道限制。</p><a className="secondary-button" href={`${import.meta.env.BASE_URL}shipping-policies/base-rules`}>打开发货策略</a></>}
    {tab==="history"&&<><h3>操作记录</h3>{history.length?<div className="warehouse-history">{history.map(h=><details key={h.id}><summary>{dateTime(h.created_at)} · {h.actor} · {actions[h.action] || h.action}</summary><pre>{JSON.stringify({修改前:h.before,修改后:h.after},null,2)}</pre></details>)}</div>:<EmptyState label="暂无配置操作记录"/>}</>}
   </main>
   <footer><p>{busy?<><LoaderCircle size={15} className="spin"/>{busy}…</>:`${inProgress===null?"处理中数量暂不可用":`${inProgress} 笔购单处理中`}。暂停阻止新的购单，已提交的购单可能完成，已购面单继续履约。`}</p>{selected.fulfillment_enabled&&<button className="secondary-button" disabled={!!busy||!authenticated} onClick={()=>{if(window.confirm(`暂停 ${selected.display_name} 所有店铺的新发货？`))void run("暂停仓库",async()=>{await api.pauseFulfillmentWarehouse(selected,auth);await refresh();setCheck(null);setMessage("仓库发货已暂停，店铺配置和已购面单保留。");});}}>暂停仓库发货</button>}</footer>
  </section></div>}
 </section>;
}
