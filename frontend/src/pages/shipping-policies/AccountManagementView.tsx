import { Check, KeyRound, LoaderCircle, Pencil, Plus, RefreshCw, ShieldCheck, Warehouse as WarehouseIcon, X } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { api } from "../../api";
import { EmptyState, ErrorState, LoadingState } from "../../components/Common";
import type { OMSAccountSummary, Warehouse } from "../../types";

type AccountDraft = {
  key: string;
  label: string;
  username: string;
  password: string;
  warehouse_codes: string[];
};

const emptyDraft: AccountDraft = { key: "", label: "", username: "", password: "", warehouse_codes: [] };

export default function AccountManagementView() {
  const [accounts, setAccounts] = useState<OMSAccountSummary[]>([]);
  const [warehouses, setWarehouses] = useState<Warehouse[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState("");
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [draft, setDraft] = useState<AccountDraft>(emptyDraft);
  const [credentialAccount, setCredentialAccount] = useState<OMSAccountSummary | null>(null);
  const [credentials, setCredentials] = useState({ username: "", password: "" });
  const [labelAccount, setLabelAccount] = useState<OMSAccountSummary | null>(null);
  const [label, setLabel] = useState("");

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const [accountItems, warehouseItems] = await Promise.all([
        api.fulfillmentAccounts(true),
        api.warehouses()
      ]);
      setAccounts(accountItems);
      setWarehouses(warehouseItems);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "OMS 账户加载失败");
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { void load(); }, []);

  const replaceAccount = (saved: OMSAccountSummary) => {
    setAccounts((items) => items.map((item) => item.key === saved.key ? saved : item));
  };
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
      replaceAccount(await api.updateFulfillmentAccountWarehouses(account.key, account.warehouse_codes));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "账户仓库范围保存失败");
    } finally {
      setSaving("");
    }
  };
  const setEnabled = async (account: OMSAccountSummary, enabled: boolean) => {
    setSaving(`status:${account.key}`);
    setError("");
    try {
      replaceAccount(await api.updateFulfillmentAccount(account.key, { enabled }));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "账户状态更新失败");
    } finally {
      setSaving("");
    }
  };
  const createAccount = async (event: FormEvent) => {
    event.preventDefault();
    setSaving("create");
    setError("");
    try {
      const created = await api.createFulfillmentAccount(draft);
      setAccounts((items) => [...items, created].sort((left, right) => left.key.localeCompare(right.key)));
      setCreating(false);
      setDraft(emptyDraft);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "OMS 账户新建失败");
    } finally {
      setSaving("");
    }
  };
  const saveLabel = async (event: FormEvent) => {
    event.preventDefault();
    if (!labelAccount) return;
    setSaving(`label:${labelAccount.key}`);
    setError("");
    try {
      replaceAccount(await api.updateFulfillmentAccount(labelAccount.key, { label }));
      setLabelAccount(null);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "账户名称更新失败");
    } finally {
      setSaving("");
    }
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
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "账户凭据更新失败");
    } finally {
      setSaving("");
    }
  };

  return <div className="policy-view-content account-management-view">
    {error && (
      <ErrorState message={error} onRetry={() => void load()}/>
    )}
    <div className="account-management-toolbar">
      <div><strong>{accounts.length}</strong><span>个 OMS 发货账户</span></div>
      <div><button className="icon-button bordered" type="button" title="刷新" onClick={() => void load()}><RefreshCw className={loading ? "spin" : ""} size={17}/></button><button className="primary-button" type="button" onClick={() => { setDraft(emptyDraft); setCreating(true); }}><Plus size={16}/><span>新建账户</span></button></div>
    </div>
    {loading && !accounts.length ? <LoadingState label="正在加载 OMS 账户"/> : accounts.length ? <div className="account-policy-grid">{accounts.map((account) => <article className={`account-policy-card ${account.enabled ? "" : "disabled"}`} key={account.key}>
      <header><div className="account-identity"><span>{account.label.slice(0, 1).toUpperCase()}</span><div><h3>{account.label}</h3><p>{account.key} · {account.username_hint || "未配置登录凭据"}</p></div></div><div className="account-card-actions"><label className="policy-toggle"><input type="checkbox" checked={account.enabled} disabled={saving === `status:${account.key}`} onChange={(event) => void setEnabled(account, event.target.checked)}/><span/><b>{account.enabled ? "启用" : "停用"}</b></label><button className="icon-button bordered" type="button" title="编辑账户名称" onClick={() => { setLabelAccount(account); setLabel(account.label); }}><Pencil size={15}/></button><button className="icon-button bordered" type="button" title="更新账户凭据" disabled={!account.enabled} onClick={() => { setCredentialAccount(account); setCredentials({ username: "", password: "" }); }}><KeyRound size={16}/></button></div></header>
      <div className="account-card-summary"><span><WarehouseIcon size={13}/>{account.warehouse_codes.length} 个仓库</span><span>{account.route_count} 条 SKU 路由</span></div>
      <div className="account-warehouse-grid">{warehouses.map((warehouse) => { const active = account.warehouse_codes.includes(warehouse.wh_code); return <label className={active ? "active" : ""} key={warehouse.wh_code}><input type="checkbox" checked={active} onChange={() => toggleWarehouse(account, warehouse.wh_code)}/><span>{active && <Check size={13}/>}</span><div><strong>{warehouse.wh_code}</strong><small>{warehouse.name || "仓库"}</small></div></label>; })}</div>
      <footer><button className="primary-button" type="button" disabled={saving === `warehouses:${account.key}`} onClick={() => void saveWarehouses(account)}>{saving === `warehouses:${account.key}` ? <LoaderCircle className="spin" size={15}/> : <ShieldCheck size={15}/>}<span>{saving === `warehouses:${account.key}` ? "保存中" : "保存仓库范围"}</span></button></footer>
    </article>)}</div> : <EmptyState label="暂无 OMS 发货账户"/>}

    {creating && <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setCreating(false); }}><section className="modal account-create-modal" role="dialog" aria-modal="true" aria-labelledby="account-create-title"><header><div><h2 id="account-create-title">新建 OMS 发货账户</h2><p>验证登录并自动发现仓库</p></div><button className="icon-button" type="button" title="关闭" onClick={() => setCreating(false)}><X size={19}/></button></header><form onSubmit={createAccount}>{error && <div className="error-banner">{error}</div>}<div className="form-grid"><label><span>账户标识</span><input required autoFocus pattern="[a-z0-9][a-z0-9_-]{0,63}" placeholder="例如 backup" value={draft.key} onChange={(event) => setDraft({ ...draft, key: event.target.value.toLowerCase() })}/></label><label><span>显示名称</span><input required maxLength={100} placeholder="例如 备用账户" value={draft.label} onChange={(event) => setDraft({ ...draft, label: event.target.value })}/></label><label><span>OMS 账号</span><input required autoComplete="off" value={draft.username} onChange={(event) => setDraft({ ...draft, username: event.target.value })}/></label><label><span>OMS 密码</span><input required type="password" autoComplete="new-password" value={draft.password} onChange={(event) => setDraft({ ...draft, password: event.target.value })}/></label></div><div className="security-note"><ShieldCheck size={17}/><span>验证成功后自动读取可见仓库，并加密保存登录凭据</span></div><footer><button className="secondary-button" type="button" onClick={() => setCreating(false)}>取消</button><button className="primary-button" type="submit" disabled={saving === "create"}>{saving === "create" ? "验证中" : "验证并新建"}</button></footer></form></section></div>}

    {labelAccount && <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setLabelAccount(null); }}><section className="modal account-label-modal" role="dialog" aria-modal="true" aria-labelledby="account-label-title"><header><div><h2 id="account-label-title">编辑账户名称</h2><p>{labelAccount.key}</p></div><button className="icon-button" type="button" title="关闭" onClick={() => setLabelAccount(null)}><X size={19}/></button></header><form onSubmit={saveLabel}>{error && <div className="error-banner">{error}</div>}<div className="form-grid"><label className="full"><span>显示名称</span><input required autoFocus maxLength={100} value={label} onChange={(event) => setLabel(event.target.value)}/></label></div><footer><button className="secondary-button" type="button" onClick={() => setLabelAccount(null)}>取消</button><button className="primary-button" type="submit" disabled={saving.startsWith("label:")}>{saving.startsWith("label:") ? "保存中" : "保存"}</button></footer></form></section></div>}

    {credentialAccount && <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setCredentialAccount(null); }}><section className="modal account-credential-modal" role="dialog" aria-modal="true" aria-labelledby="account-credential-title"><header><div><h2 id="account-credential-title">更新 {credentialAccount.label}</h2><p>当前账号 {credentialAccount.username_hint || "未配置"}</p></div><button className="icon-button" type="button" title="关闭" onClick={() => setCredentialAccount(null)}><X size={19}/></button></header><form onSubmit={saveCredentials}>{error && <div className="error-banner">{error}</div>}<div className="form-grid"><label className="full"><span>OMS 账号</span><input required autoComplete="off" value={credentials.username} onChange={(event) => setCredentials({ ...credentials, username: event.target.value })}/></label><label className="full"><span>OMS 密码</span><input required type="password" autoComplete="new-password" value={credentials.password} onChange={(event) => setCredentials({ ...credentials, password: event.target.value })}/></label></div><div className="security-note"><ShieldCheck size={17}/><span>登录凭据验证通过后加密存储</span></div><footer><button className="secondary-button" type="button" onClick={() => setCredentialAccount(null)}>取消</button><button className="primary-button" type="submit" disabled={saving.startsWith("credentials:")}>{saving.startsWith("credentials:") ? "验证中" : "验证并保存"}</button></footer></form></section></div>}
  </div>;
}
