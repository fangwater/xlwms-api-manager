import { Check, KeyRound, LoaderCircle, RefreshCw, RotateCcw, Search, ShieldCheck, Warehouse as WarehouseIcon, X } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { api } from "../../api";
import { EmptyState, ErrorState, LoadingState, Pagination } from "../../components/Common";
import type { OMSAccountSummary, PlatformSKUOMSAccount, PlatformSKUOMSAccountPage, Warehouse } from "../../types";

export default function AccountRoutesView({ platform, platformLabel }: { platform: string; platformLabel: string }) {
  const [accounts, setAccounts] = useState<OMSAccountSummary[]>([]);
  const [warehouses, setWarehouses] = useState<Warehouse[]>([]);
  const [routes, setRoutes] = useState<PlatformSKUOMSAccountPage | null>(null);
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState("");
  const [credentialAccount, setCredentialAccount] = useState<OMSAccountSummary | null>(null);
  const [credentials, setCredentials] = useState({ username: "", password: "" });

  useEffect(() => {
    const timer = window.setTimeout(() => { setDebouncedQuery(query.trim()); setPage(1); }, 250);
    return () => window.clearTimeout(timer);
  }, [query]);

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const [accountItems, warehouseItems, routePage] = await Promise.all([
        api.fulfillmentAccounts(), api.warehouses(), api.platformSKUOMSAccounts({ platform, q: debouncedQuery, page, pageSize: 30 })
      ]);
      setAccounts(accountItems);
      setWarehouses(warehouseItems);
      setRoutes(routePage);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "账户路由加载失败"); }
    finally { setLoading(false); }
  };
  useEffect(() => { void load(); }, [platform, page, debouncedQuery]);

  const toggleWarehouse = (account: OMSAccountSummary, warehouseCode: string) => {
    setAccounts((items) => items.map((item) => item.key !== account.key ? item : {
      ...item,
      warehouse_codes: item.warehouse_codes.includes(warehouseCode)
        ? item.warehouse_codes.filter((code) => code !== warehouseCode)
        : [...item.warehouse_codes, warehouseCode].sort()
    }));
  };
  const saveWarehouses = async (account: OMSAccountSummary) => {
    setSaving(`warehouses:${account.key}`);
    setError("");
    try {
      const saved = await api.updateFulfillmentAccountWarehouses(account.key, account.warehouse_codes);
      setAccounts((items) => items.map((item) => item.key === saved.key ? saved : item));
    } catch (reason) { setError(reason instanceof Error ? reason.message : "账户仓库范围保存失败"); }
    finally { setSaving(""); }
  };
  const saveRoute = async (item: PlatformSKUOMSAccount, accountKey: string) => {
    setSaving(`route:${item.warehouse_sku}`);
    setError("");
    try {
      let saved: PlatformSKUOMSAccount;
      if (accountKey) {
        saved = await api.updatePlatformSKUOMSAccount(platform, item.warehouse_sku, accountKey);
      } else {
        await api.resetPlatformSKUOMSAccount(platform, item.warehouse_sku);
        saved = { ...item, account_key: "", account_label: "", configured: false };
      }
      setRoutes((current) => current ? { ...current, records: current.records.map((route) => route.warehouse_sku === item.warehouse_sku ? saved : route) } : current);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "SKU 账户路由保存失败"); }
    finally { setSaving(""); }
  };
  const saveCredentials = async (event: FormEvent) => {
    event.preventDefault();
    if (!credentialAccount) return;
    setSaving(`credentials:${credentialAccount.key}`);
    setError("");
    try {
      await api.updatePlatformOrderAccount(credentialAccount.key, credentials);
      setCredentialAccount(null);
      setCredentials({ username: "", password: "" });
      await load();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "账户凭证更新失败"); }
    finally { setSaving(""); }
  };

  return <div className="policy-view-content account-route-view">
    {error && (
      <ErrorState message={error} onRetry={() => void load()}/>
    )}
    <section className="account-policy-section">
      <div className="account-section-heading"><div><h2>OMS 发货账户</h2><p>仓库范围可重叠，同一仓库可以由多个账户操作</p></div><button className="icon-button bordered" type="button" title="刷新" onClick={() => void load()}><RefreshCw className={loading ? "spin" : ""} size={17}/></button></div>
      {loading && !accounts.length ? <LoadingState label="正在加载 OMS 账户"/> : <div className="account-policy-grid">{accounts.map((account) => <article className="account-policy-card" key={account.key}>
        <header><div className="account-identity"><span>{account.label.slice(0, 1)}</span><div><h3>{account.label}</h3><p>{account.username_hint || "未配置登录凭证"}</p></div></div><button className="icon-button bordered" type="button" title="更新账户凭证" onClick={() => { setCredentialAccount(account); setCredentials({ username: "", password: "" }); }}><KeyRound size={16}/></button></header>
        <div className="account-card-summary"><span><WarehouseIcon size={13}/>{account.warehouse_codes.length} 个仓库</span><span>{account.route_count} 条 SKU 路由</span></div>
        <div className="account-warehouse-grid">{warehouses.map((warehouse) => { const active = account.warehouse_codes.includes(warehouse.wh_code); return <label className={active ? "active" : ""} key={warehouse.wh_code}><input type="checkbox" checked={active} onChange={() => toggleWarehouse(account, warehouse.wh_code)}/><span>{active && <Check size={13}/>}</span><div><strong>{warehouse.wh_code}</strong><small>{warehouse.name || "仓库"}</small></div></label>; })}</div>
        <footer><button className="primary-button" type="button" disabled={saving === `warehouses:${account.key}`} onClick={() => void saveWarehouses(account)}>{saving === `warehouses:${account.key}` ? <LoaderCircle className="spin" size={15}/> : <ShieldCheck size={15}/>}<span>{saving === `warehouses:${account.key}` ? "保存中" : "保存仓库范围"}</span></button></footer>
      </article>)}</div>}
    </section>

    <section className="account-route-section">
      <div className="account-section-heading"><div><h2>{platformLabel} SKU 账户路由</h2><p>每个仓库 SKU 必须明确选择一个 OMS 发货账户</p></div></div>
      <div className="filter-bar sku-policy-toolbar"><label className="search-field"><Search size={16}/><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索仓库 SKU 或产品名称"/></label><div><span>{routes ? `已配置 ${routes.records.filter((item) => item.configured).length} / 当前 ${routes.records.length}` : ""}</span></div></div>
      {loading ? <LoadingState label="正在加载 SKU 账户路由"/> : routes?.records.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table account-route-table"><thead><tr><th>仓库 SKU / 产品</th><th>OMS 发货账户</th><th>账户可操作仓库</th><th>状态</th><th>操作</th></tr></thead><tbody>{routes.records.map((item) => {
        const account = accounts.find((candidate) => candidate.key === item.account_key);
        const busy = saving === `route:${item.warehouse_sku}`;
        return <tr key={item.warehouse_sku}><td><div className="primary-cell"><strong>{item.warehouse_sku}</strong><small>{item.product_name || "-"}</small></div></td><td><select aria-label={`${item.warehouse_sku} OMS 发货账户`} value={item.account_key || ""} disabled={busy} onChange={(event) => void saveRoute(item, event.target.value)}><option value="">待配置</option>{accounts.map((candidate) => <option key={candidate.key} value={candidate.key}>{candidate.label}</option>)}</select></td><td><div className="warehouse-tags">{account?.warehouse_codes.length ? account.warehouse_codes.map((code) => <span key={code}>{code}</span>) : <span className="danger-tag">无可操作仓库</span>}</div></td><td><span className={`status-badge ${item.configured ? "success" : ""}`}>{item.configured ? "已配置" : "转人工"}</span></td><td>{busy ? <LoaderCircle className="spin route-saving" size={17}/> : item.configured ? <button className="icon-button bordered" type="button" title="清除账户路由" onClick={() => void saveRoute(item, "")}><RotateCcw size={15}/></button> : <span className="muted-action">-</span>}</td></tr>;
      })}</tbody></table></div></div> : <EmptyState label="当前没有仓库 SKU"/>}
      {routes && routes.total > 0 && (
        <Pagination page={routes.page} pages={routes.pages} total={routes.total} onChange={setPage}/>
      )}
    </section>

    {credentialAccount && <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setCredentialAccount(null); }}><section className="modal account-credential-modal" role="dialog" aria-modal="true" aria-labelledby="account-credential-title"><header><div><h2 id="account-credential-title">更新 {credentialAccount.label}</h2><p>当前账号 {credentialAccount.username_hint || "未配置"}</p></div><button className="icon-button" type="button" title="关闭" onClick={() => setCredentialAccount(null)}><X size={19}/></button></header><form onSubmit={saveCredentials}><div className="form-grid"><label className="full"><span>OMS 账号</span><input required autoComplete="off" value={credentials.username} onChange={(event) => setCredentials({ ...credentials, username: event.target.value })}/></label><label className="full"><span>OMS 密码</span><input required type="password" autoComplete="new-password" value={credentials.password} onChange={(event) => setCredentials({ ...credentials, password: event.target.value })}/></label></div><div className="security-note"><ShieldCheck size={17}/><span>保存前会验证 OMS 登录，凭证通过验证后加密存储</span></div><footer><button className="secondary-button" type="button" onClick={() => setCredentialAccount(null)}>取消</button><button className="primary-button" type="submit" disabled={saving.startsWith("credentials:")}>{saving.startsWith("credentials:") ? "验证中" : "验证并保存"}</button></footer></form></section></div>}
  </div>;
}
