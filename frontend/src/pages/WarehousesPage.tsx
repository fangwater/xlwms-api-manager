import { KeyRound, Plus, ShieldCheck, Trash2, Warehouse as WarehouseIcon, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, PageHeader, dateTime } from "../components/Common";
import type { Warehouse } from "../types";

const emptyWarehouseForm = {
  wh_code: "",
  name: "",
  api_base_url: "https://api.xlwms.com/openapi",
  app_key: "",
  app_secret: "",
  active: true
};

export default function WarehousesPage({ warehouses, onChanged }: { warehouses: Warehouse[]; onChanged: () => Promise<void> }) {
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [form, setForm] = useState(emptyWarehouseForm);
  const [accountWarehouse, setAccountWarehouse] = useState<Warehouse | null>(null);
  const [accountForm, setAccountForm] = useState({ username: "", password: "" });
  const [accountError, setAccountError] = useState("");
  const [savingAccount, setSavingAccount] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      await api.saveWarehouse(form);
      await onChanged();
      setOpen(false);
      setForm(emptyWarehouseForm);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "保存失败");
    } finally {
      setSaving(false);
    }
  };

  const toggle = async (item: Warehouse) => {
    setError("");
    try {
      await api.setWarehouseActive(item.wh_code, !item.active);
      await onChanged();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "更新失败");
    }
  };

  const openAccount = (item: Warehouse) => {
    setAccountWarehouse(item);
    setAccountForm({ username: "", password: "" });
    setAccountError("");
  };

  const closeAccount = () => {
    setAccountWarehouse(null);
    setAccountForm({ username: "", password: "" });
    setAccountError("");
  };

  const saveAccount = async (event: FormEvent) => {
    event.preventDefault();
    if (!accountWarehouse) return;
    setSavingAccount(true);
    setAccountError("");
    try {
      await api.setWarehouseOMSAccount(accountWarehouse.wh_code, accountForm);
      await onChanged();
      closeAccount();
    } catch (reason) {
      setAccountError(reason instanceof Error ? reason.message : "保存失败");
    } finally {
      setSavingAccount(false);
    }
  };

  const clearAccount = async () => {
    if (!accountWarehouse || !window.confirm(`确认清除 ${accountWarehouse.wh_code} 的 OMS 发货账号？`)) return;
    setSavingAccount(true);
    setAccountError("");
    try {
      await api.clearWarehouseOMSAccount(accountWarehouse.wh_code);
      await onChanged();
      closeAccount();
    } catch (reason) {
      setAccountError(reason instanceof Error ? reason.message : "清除失败");
    } finally {
      setSavingAccount(false);
    }
  };

  return <>
    <PageHeader
      title="仓库管理"
      subtitle="仓库连接、发货账号与同步状态"
      actions={<button className="primary-button" onClick={() => setOpen(true)}><Plus size={17} />添加仓库</button>}
    />
    {error && <ErrorState message={error} />}
    {warehouses.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table warehouse-table">
      <thead><tr><th>仓库</th><th>连接地址</th><th>OpenAPI App Key</th><th>OMS 发货账号</th><th>更新时间</th><th>状态</th></tr></thead>
      <tbody>{warehouses.map(item => <tr key={item.wh_code}>
        <td><div className="warehouse-cell"><span><WarehouseIcon size={18} /></span><div><strong>{item.name || item.wh_code}</strong><small>{item.wh_code}</small></div></div></td>
        <td>{item.api_base_url}</td>
        <td><span className="key-hint"><KeyRound size={14} />{item.app_key_hint}</span></td>
        <td><div className="warehouse-account-cell"><div><span className={`status-badge ${item.oms_account_configured ? "success" : ""}`}>{item.oms_account_configured ? "已配置" : "未配置"}</span><small>{item.oms_account_hint || "未绑定发货账号"}</small></div><button className="icon-button bordered" onClick={() => openAccount(item)} title={item.oms_account_configured ? "更换 OMS 发货账号" : "配置 OMS 发货账号"}><KeyRound size={15} /></button></div></td>
        <td>{dateTime(item.updated_at)}</td>
        <td><label className="toggle"><input type="checkbox" checked={item.active} onChange={() => void toggle(item)} /><span /><b>{item.active ? "已启用" : "已停用"}</b></label></td>
      </tr>)}</tbody>
    </table></div></div> : <EmptyState label="尚未注册仓库" />}

    {open && <div className="modal-backdrop" role="presentation"><section className="modal" role="dialog" aria-modal="true" aria-labelledby="warehouse-modal-title">
      <header><div><h2 id="warehouse-modal-title">添加仓库</h2><p>连接信息</p></div><button className="icon-button" onClick={() => setOpen(false)} title="关闭"><X size={19} /></button></header>
      <form onSubmit={submit}><div className="form-grid">
        <label><span>仓库编码</span><input required value={form.wh_code} onChange={event => setForm({ ...form, wh_code: event.target.value.toUpperCase() })} /></label>
        <label><span>仓库名称</span><input value={form.name} onChange={event => setForm({ ...form, name: event.target.value })} /></label>
        <label className="full"><span>OpenAPI 地址</span><input required value={form.api_base_url} onChange={event => setForm({ ...form, api_base_url: event.target.value })} /></label>
        <label><span>App Key</span><input required autoComplete="off" value={form.app_key} onChange={event => setForm({ ...form, app_key: event.target.value })} /></label>
        <label><span>App Secret</span><input required type="password" autoComplete="new-password" value={form.app_secret} onChange={event => setForm({ ...form, app_secret: event.target.value })} /></label>
      </div><label className="checkbox-row"><input type="checkbox" checked={form.active} onChange={event => setForm({ ...form, active: event.target.checked })} /><span>立即启用</span></label>
      <div className="security-note"><ShieldCheck size={17} /><span>凭证将加密保存</span></div>
      <footer><button type="button" className="secondary-button" onClick={() => setOpen(false)}>取消</button><button type="submit" className="primary-button" disabled={saving}>{saving ? "保存中" : "保存仓库"}</button></footer></form>
    </section></div>}

    {accountWarehouse && <div className="modal-backdrop" role="presentation"><section className="modal account-modal" role="dialog" aria-modal="true" aria-labelledby="account-modal-title">
      <header><div><h2 id="account-modal-title">OMS 发货账号</h2><p>{accountWarehouse.name || accountWarehouse.wh_code} · {accountWarehouse.wh_code}</p></div><button className="icon-button" onClick={closeAccount} title="关闭"><X size={19} /></button></header>
      <form onSubmit={saveAccount}>
        {accountError && <div className="error-banner"><span>{accountError}</span></div>}
        <div className="form-grid">
          <label className="full"><span>OMS 用户名</span><input required autoComplete="off" value={accountForm.username} onChange={event => setAccountForm({ ...accountForm, username: event.target.value })} /></label>
          <label className="full"><span>OMS 密码</span><input required type="password" autoComplete="new-password" value={accountForm.password} onChange={event => setAccountForm({ ...accountForm, password: event.target.value })} /></label>
        </div>
        <div className="security-note"><ShieldCheck size={17} /><span>该账号仅用于此仓库的权限校验与发货操作，并加密存储</span></div>
        <footer>
          {accountWarehouse.oms_account_configured && <button type="button" className="secondary-button danger-button account-clear-button" onClick={() => void clearAccount()} disabled={savingAccount}><Trash2 size={15} />清除账号</button>}
          <button type="button" className="secondary-button" onClick={closeAccount}>取消</button>
          <button type="submit" className="primary-button" disabled={savingAccount}>{savingAccount ? "保存中" : "保存账号"}</button>
        </footer>
      </form>
    </section></div>}
  </>;
}
