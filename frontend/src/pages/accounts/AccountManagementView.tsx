import { Boxes, Check, Database, KeyRound, Pencil, Plus, RefreshCw, Search, ShieldCheck, X } from "lucide-react";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api } from "../../api";
import { EmptyState, ErrorState, LoadingState, dateTime } from "../../components/Common";
import type { OMSAccountSummary, OMSMFAPrompt, PlatformOrderAccountOption, WarehouseAPICredentialGroup } from "../../types";

type AccountDraft = {
  key: string;
  label: string;
  username: string;
  password: string;
  api_credential_keys: string[];
};

const emptyDraft: AccountDraft = { key: "", label: "", username: "", password: "", api_credential_keys: [] };

export default function AccountManagementView() {
  const [accounts, setAccounts] = useState<OMSAccountSummary[]>([]);
  const [credentials, setCredentials] = useState<WarehouseAPICredentialGroup[]>([]);
  const [healthByKey, setHealthByKey] = useState<Record<string, PlatformOrderAccountOption>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState("");
  const [creating, setCreating] = useState(false);
  const [draft, setDraft] = useState<AccountDraft>(emptyDraft);
  const [credentialAccount, setCredentialAccount] = useState<OMSAccountSummary | null>(null);
  const [credentialsForm, setCredentialsForm] = useState({ username: "", password: "" });
  const [labelAccount, setLabelAccount] = useState<OMSAccountSummary | null>(null);
  const [label, setLabel] = useState("");
  const [bindingAccount, setBindingAccount] = useState<OMSAccountSummary | null>(null);
  const [bindingKeys, setBindingKeys] = useState<string[]>([]);
  const [mfaAccount, setMFAAccount] = useState<OMSAccountSummary | null>(null);
  const [mfaPrompt, setMFAPrompt] = useState<OMSMFAPrompt | null>(null);
  const [mfaCode, setMFACode] = useState("");
  const [search, setSearch] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [nextAccounts, nextCredentials, nextHealth] = await Promise.all([
        api.fulfillmentAccounts(true),
        api.warehouseAPICredentials(),
        api.platformOrderAccounts().catch(() => [] as PlatformOrderAccountOption[])
      ]);
      setAccounts(nextAccounts);
      setCredentials(nextCredentials);
      setHealthByKey(Object.fromEntries(nextHealth.map((item) => [item.key, item])));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "OMS 账户加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  async function syncSKU(account: OMSAccountSummary) {
    setSaving(`sku:${account.key}`);
    setError("");
    const results = await Promise.allSettled(account.api_credential_keys.map((key) => api.syncWarehouseAPIInventory(key)));
    await load();
    if (results.some((result) => result.status === "rejected")) setError("SKU 同步未全部成功，已保留上次成功数据，请稍后重试");
    setSaving("");
  }

  const toggleKey = (values: string[], key: string) => values.includes(key)
    ? values.filter((value) => value !== key)
    : [...values, key].sort();

  async function saveAPIBindings(account: OMSAccountSummary, keys: string[]) {
    setSaving(`bindings:${account.key}`);
    setError("");
    try {
      await api.updateFulfillmentAccountAPICredentials(account.key, keys);
      await load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "OpenAPI 绑定保存失败");
    } finally {
      setSaving("");
    }
  }

  async function setEnabled(account: OMSAccountSummary, enabled: boolean) {
    setSaving(`status:${account.key}`);
    setError("");
    try {
      const saved = await api.updateFulfillmentAccount(account.key, { enabled });
      setAccounts((current) => current.map((item) => item.key === saved.key ? saved : item));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "账户状态保存失败");
    } finally {
      setSaving("");
    }
  }

  async function createAccount(event: FormEvent) {
    event.preventDefault();
    setSaving("create");
    setError("");
    try {
      await api.createFulfillmentAccount(draft);
      setDraft(emptyDraft);
      setCreating(false);
      await load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "OMS 账户新建失败");
    } finally {
      setSaving("");
    }
  }

  async function saveCredentials(event: FormEvent) {
    event.preventDefault();
    if (!credentialAccount) return;
    setSaving(`credentials:${credentialAccount.key}`);
    setError("");
    try {
      await api.updatePlatformOrderAccount(credentialAccount.key, credentialsForm);
      setCredentialAccount(null);
      setCredentialsForm({ username: "", password: "" });
      await load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "登录凭据更新失败");
    } finally {
      setSaving("");
    }
  }

  async function saveLabel(event: FormEvent) {
    event.preventDefault();
    if (!labelAccount) return;
    setSaving(`label:${labelAccount.key}`);
    setError("");
    try {
      const saved = await api.updateFulfillmentAccount(labelAccount.key, { label });
      setAccounts((current) => current.map((item) => item.key === saved.key ? saved : item));
      setLabelAccount(null);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "显示名称保存失败");
    } finally {
      setSaving("");
    }
  }

  async function beginMFA(account: OMSAccountSummary) {
    setSaving(`mfa:${account.key}`);
    setError("");
    try {
      const prompt = await api.beginPlatformOrderAccountMFA(account.key);
      setMFAAccount(account);
      setMFAPrompt(prompt);
      setMFACode("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "二次验证启动失败");
    } finally {
      setSaving("");
    }
  }

  async function completeMFA(event: FormEvent) {
    event.preventDefault();
    if (!mfaAccount) return;
    setSaving(`mfa:${mfaAccount.key}`);
    setError("");
    try {
      await api.completePlatformOrderAccountMFA(mfaAccount.key, mfaCode);
      setMFAAccount(null);
      setMFAPrompt(null);
      setMFACode("");
      await load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "验证码校验失败");
    } finally {
      setSaving("");
    }
  }

  const credentialPicker = (selected: string[], onChange: (keys: string[]) => void, currentAccountKey = "") => <div className="account-warehouse-grid">
    {credentials.map((credential) => {
      const active = selected.includes(credential.key);
      const owner = accounts.find((account) => account.key === credential.oms_account_key);
      return <label className={active ? "active" : ""} key={credential.key}>
        <input type="checkbox" checked={active} onChange={() => onChange(toggleKey(selected, credential.key))}/>
        <span>{active && <Check size={13}/>}</span>
        <div><strong>{credential.label}</strong><small>{skuLabel(credential.inventory_sync_status, credential.sku_count)}{owner && owner.key !== currentAccountKey ? ` · 当前 ${owner.label}` : ""}</small></div>
      </label>;
    })}
  </div>;

  const visibleAccounts = accounts.filter((account) => {
    const boundLabels = credentials.filter((credential) => account.api_credential_keys.includes(credential.key)).map((credential) => credential.label);
    return [account.label, account.key, account.username_hint, ...boundLabels].join(" ").toLowerCase().includes(search.trim().toLowerCase());
  });

  return <div className="account-policy-workspace">
    <div className="account-policy-toolbar">
      <label className="search-field"><Search size={17}/><input aria-label="搜索账户" placeholder="搜索账户或 API 凭据" value={search} onChange={(event) => setSearch(event.target.value)}/></label>
      <button className="primary-button" onClick={() => { setDraft(emptyDraft); setError(""); setCreating(true); }}><Plus size={16}/>新建账户</button>
    </div>
    {error && <ErrorState message={error}/>}
    {loading && !accounts.length ? <LoadingState label="正在加载 OMS 账户"/> : visibleAccounts.length ? <div className="account-policy-grid">{visibleAccounts.map((account) => {
      const bound = credentials.filter((credential) => account.api_credential_keys.includes(credential.key));
      const health = healthByKey[account.key];
      return <article className={`account-policy-card ${account.enabled ? "" : "disabled"}`} key={account.key}>
        <header><div className="account-identity"><span><KeyRound size={19}/></span><div><h3>{account.label}</h3><p>{account.username_hint || "未配置登录"}</p></div></div><button className="icon-button" title="编辑显示名称" onClick={() => { setLabelAccount(account); setLabel(account.label); setError(""); }}><Pencil size={15}/></button></header>
        <div className="account-card-summary"><span><Database size={13}/>{bound.length} 组 OpenAPI</span><span><Boxes size={13}/>{skuLabel(account.sku_status, account.sku_count)}</span>{bound.length > 0 && <button className="icon-button" title="同步 SKU" disabled={!!saving} onClick={() => void syncSKU(account)}><RefreshCw size={14}/></button>}{health && <span className={`account-health ${health.available ? "ready" : "blocked"}`}><ShieldCheck size={13}/>{health.available ? "登录正常" : health.error || "登录不可用"}</span>}</div>
        <div className="account-bindings">{bound.length ? bound.map((credential) => <span key={credential.key}><Database size={14}/><strong>{credential.label}</strong></span>) : <small>尚未绑定 API 凭据</small>}</div>
        <footer><small title="最近更新">{dateTime(account.updated_at)} 更新</small><div>{health?.status === "mfa_required" && <button className="secondary-button" onClick={() => void beginMFA(account)} disabled={saving === `mfa:${account.key}`}><ShieldCheck size={14}/>{saving === `mfa:${account.key}` ? "准备中" : "二次验证"}</button>}<button className="secondary-button" onClick={() => { setBindingAccount(account); setBindingKeys(account.api_credential_keys); setError(""); }} disabled={saving.startsWith("bindings:")}><Database size={14}/>绑定 API</button><button className="secondary-button" onClick={() => { setCredentialAccount(account); setCredentialsForm({ username: "", password: "" }); setError(""); }}><KeyRound size={14}/>登录凭据</button><label className="toggle"><input aria-label={`启用 ${account.label}`} type="checkbox" checked={account.enabled} onChange={(event) => void setEnabled(account, event.target.checked)}/><span/><b>{account.enabled ? "启用" : "停用"}</b></label></div></footer>
      </article>;
    })}</div> : <EmptyState label={search ? "没有匹配的账户" : "暂无 OMS 发货账户"}/>}

    <BindingModal/>
    {creating && <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setCreating(false); }}><section className="modal account-create-modal" role="dialog" aria-modal="true" aria-labelledby="account-create-title"><header><div><h2 id="account-create-title">新建 OMS 发货账户</h2><p>验证登录并绑定 OpenAPI SKU 范围</p></div><button className="icon-button" type="button" title="关闭" onClick={() => setCreating(false)}><X size={19}/></button></header><form onSubmit={createAccount}>{error && <div className="error-banner">{error}</div>}<div className="form-grid"><label><span>账户标识</span><input required autoFocus pattern="[a-z0-9][a-z0-9_-]{0,63}" placeholder="例如 laundry" value={draft.key} onChange={(event) => setDraft({ ...draft, key: event.target.value.toLowerCase() })}/></label><label><span>显示名称</span><input required maxLength={100} placeholder="例如 FHZARP-脏衣篓" value={draft.label} onChange={(event) => setDraft({ ...draft, label: event.target.value })}/></label><label><span>OMS 账号</span><input required autoComplete="off" value={draft.username} onChange={(event) => setDraft({ ...draft, username: event.target.value })}/></label><label><span>OMS 密码</span><input required type="password" autoComplete="new-password" value={draft.password} onChange={(event) => setDraft({ ...draft, password: event.target.value })}/></label></div>{credentialPicker(draft.api_credential_keys, (keys) => setDraft({ ...draft, api_credential_keys: keys }))}<div className="security-note"><ShieldCheck size={17}/><span>验证成功后加密保存，API 凭据决定该账号负责的 SKU 范围</span></div><footer><button className="secondary-button" type="button" onClick={() => setCreating(false)}>取消</button><button className="primary-button" type="submit" disabled={saving === "create"}>{saving === "create" ? "验证中" : "验证并新建"}</button></footer></form></section></div>}

    {labelAccount && <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setLabelAccount(null); }}><section className="modal account-label-modal" role="dialog" aria-modal="true" aria-labelledby="account-label-title"><header><div><h2 id="account-label-title">编辑账户名称</h2><p>仅修改界面显示名称</p></div><button className="icon-button" type="button" title="关闭" onClick={() => setLabelAccount(null)}><X size={19}/></button></header><form onSubmit={saveLabel}><div className="form-grid"><label className="full"><span>显示名称</span><input required autoFocus maxLength={100} value={label} onChange={(event) => setLabel(event.target.value)}/></label></div><footer><button className="secondary-button" type="button" onClick={() => setLabelAccount(null)}>取消</button><button className="primary-button" type="submit" disabled={saving.startsWith("label:")}>{saving.startsWith("label:") ? "保存中" : "保存"}</button></footer></form></section></div>}

    {credentialAccount && <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setCredentialAccount(null); }}><section className="modal account-credential-modal" role="dialog" aria-modal="true" aria-labelledby="account-credential-title"><header><div><h2 id="account-credential-title">更新 {credentialAccount.label}</h2><p>当前账号 {credentialAccount.username_hint || "未配置"}</p></div><button className="icon-button" type="button" title="关闭" onClick={() => setCredentialAccount(null)}><X size={19}/></button></header><form onSubmit={saveCredentials}><div className="form-grid"><label className="full"><span>OMS 账号</span><input required autoComplete="off" value={credentialsForm.username} onChange={(event) => setCredentialsForm({ ...credentialsForm, username: event.target.value })}/></label><label className="full"><span>OMS 密码</span><input required type="password" autoComplete="new-password" value={credentialsForm.password} onChange={(event) => setCredentialsForm({ ...credentialsForm, password: event.target.value })}/></label></div><div className="security-note"><ShieldCheck size={17}/><span>登录凭据验证通过后加密存储</span></div><footer><button className="secondary-button" type="button" onClick={() => setCredentialAccount(null)}>取消</button><button className="primary-button" type="submit" disabled={saving.startsWith("credentials:")}>{saving.startsWith("credentials:") ? "验证中" : "验证并保存"}</button></footer></form></section></div>}

    {mfaAccount && mfaPrompt && <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setMFAAccount(null); }}><section className="modal account-mfa-modal" role="dialog" aria-modal="true" aria-labelledby="account-mfa-title"><header><div><h2 id="account-mfa-title">{mfaAccount.label} 二次验证</h2><p>{mfaPrompt.code_sent ? `验证码已发送至 ${mfaPrompt.masked_target || mfaPrompt.channel}` : `${mfaPrompt.masked_target || "验证器"} · ${mfaPrompt.channel}`}</p></div><button className="icon-button" type="button" title="关闭" onClick={() => setMFAAccount(null)}><X size={19}/></button></header><form onSubmit={completeMFA}><div className="form-grid"><label className="full"><span>6 位验证码</span><input required autoFocus className="mfa-code-input" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" minLength={6} maxLength={6} value={mfaCode} onChange={(event) => setMFACode(event.target.value.replace(/\D/g, "").slice(0, 6))}/></label></div>{error && <div className="error-banner">{error}</div>}<footer><button className="secondary-button" type="button" onClick={() => setMFAAccount(null)}>取消</button><button className="primary-button" type="submit" disabled={saving.startsWith("mfa:") || mfaCode.length !== 6}>{saving.startsWith("mfa:") ? "验证中" : "验证登录"}</button></footer></form></section></div>}
  </div>;

  function BindingModal() {
    if (!bindingAccount) return null;
    return <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setBindingAccount(null); }}><section className="modal account-create-modal" role="dialog" aria-modal="true" aria-labelledby="account-binding-title"><header><div><h2 id="account-binding-title">绑定 OpenAPI 凭据</h2><p>{bindingAccount.label}</p></div><button className="icon-button" type="button" title="关闭" onClick={() => setBindingAccount(null)}><X size={19}/></button></header>{credentialPicker(bindingKeys, setBindingKeys, bindingAccount.key)}<footer><button className="secondary-button" type="button" onClick={() => setBindingAccount(null)}>取消</button><button className="primary-button" type="button" disabled={saving.startsWith("bindings:")} onClick={async () => { await saveAPIBindings(bindingAccount, bindingKeys); setBindingAccount(null); }}>保存绑定</button></footer></section></div>;
  }
}

function skuLabel(status: string, count: number) {
  if (status === "ready") return `${count} 个 SKU`;
  if (status === "failed") return "SKU 同步失败";
  if (status === "unbound") return "未绑定 API";
  return "SKU 待同步";
}
