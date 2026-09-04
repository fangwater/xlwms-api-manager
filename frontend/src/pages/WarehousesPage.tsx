import { Boxes, Database, KeyRound, LockKeyhole, Plus, ShieldCheck, Trash2, TriangleAlert, Warehouse as WarehouseIcon, X } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { api } from "../api";
import { EmptyState, ErrorState, PageHeader, dateTime } from "../components/Common";
import type { Warehouse, WarehouseAPICredentialGroup } from "../types";

const emptyWarehouseForm = {
  wh_code: "",
  name: "",
  api_base_url: "https://api.xlwms.com/openapi",
  app_key: "",
  app_secret: "",
  active: true
};

const emptyCredentialForm = {
  label: "",
  api_base_url: "https://api.xlwms.com/openapi",
  app_key: "",
  app_secret: ""
};

export default function WarehousesPage({ warehouses, onChanged }: { warehouses: Warehouse[]; onChanged: () => Promise<void> }) {
  const [warehouseOpen, setWarehouseOpen] = useState(false);
  const [credentialOpen, setCredentialOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [form, setForm] = useState(emptyWarehouseForm);
  const [credentialForm, setCredentialForm] = useState(emptyCredentialForm);
  const [credentials, setCredentials] = useState<WarehouseAPICredentialGroup[]>([]);
  const [deleting, setDeleting] = useState<WarehouseAPICredentialGroup | null>(null);

  const loadCredentials = async () => {
    try {
      setCredentials(await api.warehouseAPICredentials());
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法读取仓库 API 凭据");
    }
  };

  useEffect(() => { void loadCredentials(); }, []);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      await api.saveWarehouse(form);
      await onChanged();
      setWarehouseOpen(false);
      setForm(emptyWarehouseForm);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "保存失败");
    } finally {
      setSaving(false);
    }
  };

  const submitCredential = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      await api.saveWarehouseAPICredential(credentialForm);
      await loadCredentials();
      setCredentialOpen(false);
      setCredentialForm(emptyCredentialForm);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "凭据验证失败");
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

  const deleteCredential = async () => {
    if (!deleting) return;
    setSaving(true);
    setError("");
    try {
      await api.deleteWarehouseAPICredential(deleting.key);
      setCredentials(current => current.filter(item => item.key !== deleting.key));
      setDeleting(null);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "删除失败");
    } finally {
      setSaving(false);
    }
  };

  return <>
    <PageHeader
      title="仓库管理"
      subtitle="仓库、OpenAPI 凭据组与数据覆盖范围"
      actions={<div className="warehouse-header-actions"><button className="secondary-button" onClick={() => setWarehouseOpen(true)}><Plus size={17} />添加仓库</button><button className="primary-button" onClick={() => setCredentialOpen(true)}><KeyRound size={17} />新增 API 凭据</button></div>}
    />
    {error && <ErrorState message={error} />}

    <div className="warehouse-summary-bar">
      <div><span><Database size={17} /></span><div><strong>{credentials.length}</strong><small>组 OpenAPI 凭据</small></div></div>
      <div><span><WarehouseIcon size={17} /></span><div><strong>{warehouses.length}</strong><small>个仓库编码</small></div></div>
      <div><span><Boxes size={17} /></span><div><strong>{credentials.reduce((total, item) => total + (item.last_verified_at ? item.sku_count : 0), 0)}</strong><small>条已扫描 SKU 覆盖</small></div></div>
    </div>

    <section className="warehouse-section">
      <div className="warehouse-section-heading"><div><h2>OpenAPI 凭据组</h2><p>同一仓库可由多组凭据覆盖不同货品</p></div><span>{credentials.length} 组</span></div>
      {credentials.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table api-credential-table">
        <thead><tr><th>凭据组</th><th>App Key</th><th>覆盖仓库</th><th>发现 SKU</th><th>验证状态</th><th>操作</th></tr></thead>
        <tbody>{credentials.map(item => <tr key={item.key}>
          <td><div className="primary-cell"><strong>{item.label}</strong><small>{item.key}</small></div></td>
          <td><span className="key-hint"><KeyRound size={14} />{item.app_key_hint}</span></td>
          <td><div className="warehouse-code-list">{item.warehouse_codes.length ? item.warehouse_codes.map(code => <span key={code}>{code}</span>) : <small>待发现</small>}</div></td>
          <td><strong>{item.last_verified_at ? item.sku_count : "-"}</strong></td>
          <td><div className="primary-cell"><strong className={item.active && item.last_verified_at ? "verified-text" : "muted-text"}>{!item.active ? "已停用" : item.last_verified_at ? "已验证" : "旧配置已归并"}</strong><small>{dateTime(item.last_verified_at || item.updated_at)}</small></div></td>
          <td>{item.deletable
            ? <button className="icon-button credential-delete" type="button" title={`删除 ${item.label}`} aria-label={`删除 ${item.label}`} onClick={() => { setError(""); setDeleting(item); }}><Trash2 size={16} /></button>
            : <span className="credential-in-use" title="该凭据仍被仓库同步或出库使用"><LockKeyhole size={14} /><span>使用中</span></span>}
          </td>
        </tr>)}</tbody>
      </table></div></div> : <EmptyState label="尚未登记 OpenAPI 凭据" />}
    </section>

    <section className="warehouse-section">
      <div className="warehouse-section-heading"><div><h2>仓库编码</h2><p>业务请求使用的实际 wh_code</p></div><span>{warehouses.length} 个</span></div>
    {warehouses.length ? <div className="table-panel"><div className="table-scroll"><table className="data-table warehouse-table">
      <thead><tr><th>仓库</th><th>连接地址</th><th>OpenAPI App Key</th><th>更新时间</th><th>状态</th></tr></thead>
      <tbody>{warehouses.map(item => <tr key={item.wh_code}>
        <td><div className="warehouse-cell"><span><WarehouseIcon size={18} /></span><div><strong>{item.name || item.wh_code}</strong><small>{item.wh_code}</small></div></div></td>
        <td>{item.api_base_url}</td>
        <td><span className="key-hint"><KeyRound size={14} />{item.app_key_hint}</span></td>
        <td>{dateTime(item.updated_at)}</td>
        <td><label className="toggle"><input type="checkbox" checked={item.active} onChange={() => void toggle(item)} /><span /><b>{item.active ? "已启用" : "已停用"}</b></label></td>
      </tr>)}</tbody>
    </table></div></div> : <EmptyState label="尚未注册仓库" />}
    </section>

    {warehouseOpen && <div className="modal-backdrop" role="presentation"><section className="modal" role="dialog" aria-modal="true" aria-labelledby="warehouse-modal-title">
      <header><div><h2 id="warehouse-modal-title">添加仓库</h2><p>兼容现有仓库签名路径</p></div><button className="icon-button" onClick={() => setWarehouseOpen(false)} title="关闭"><X size={19} /></button></header>
      <form onSubmit={submit}><div className="form-grid">
        <label><span>仓库编码</span><input required value={form.wh_code} onChange={event => setForm({ ...form, wh_code: event.target.value.toUpperCase() })} /></label>
        <label><span>仓库名称</span><input value={form.name} onChange={event => setForm({ ...form, name: event.target.value })} /></label>
        <label className="full"><span>OpenAPI 地址</span><input required value={form.api_base_url} onChange={event => setForm({ ...form, api_base_url: event.target.value })} /></label>
        <label><span>App Key</span><input required autoComplete="off" value={form.app_key} onChange={event => setForm({ ...form, app_key: event.target.value })} /></label>
        <label><span>App Secret</span><input required type="password" autoComplete="new-password" value={form.app_secret} onChange={event => setForm({ ...form, app_secret: event.target.value })} /></label>
      </div><label className="checkbox-row"><input type="checkbox" checked={form.active} onChange={event => setForm({ ...form, active: event.target.checked })} /><span>立即启用</span></label>
      <div className="security-note"><ShieldCheck size={17} /><span>凭证将加密保存</span></div>
      <footer><button type="button" className="secondary-button" onClick={() => setWarehouseOpen(false)}>取消</button><button type="submit" className="primary-button" disabled={saving}>{saving ? "保存中" : "保存仓库"}</button></footer></form>
    </section></div>}

    {credentialOpen && <div className="modal-backdrop" role="presentation"><section className="modal" role="dialog" aria-modal="true" aria-labelledby="credential-modal-title">
      <header><div><h2 id="credential-modal-title">新增 OpenAPI 凭据</h2><p>验证后自动发现仓库和 SKU</p></div><button className="icon-button" onClick={() => setCredentialOpen(false)} title="关闭"><X size={19} /></button></header>
      <form onSubmit={submitCredential}><div className="form-grid">
        <label className="full"><span>凭据组名称</span><input maxLength={100} placeholder="留空则使用 App Key 提示" value={credentialForm.label} onChange={event => setCredentialForm({ ...credentialForm, label: event.target.value })} /></label>
        <label className="full"><span>OpenAPI 地址</span><input required value={credentialForm.api_base_url} onChange={event => setCredentialForm({ ...credentialForm, api_base_url: event.target.value })} /></label>
        <label><span>App Key</span><input required autoComplete="off" value={credentialForm.app_key} onChange={event => setCredentialForm({ ...credentialForm, app_key: event.target.value })} /></label>
        <label><span>App Secret</span><input required type="password" autoComplete="new-password" value={credentialForm.app_secret} onChange={event => setCredentialForm({ ...credentialForm, app_secret: event.target.value })} /></label>
      </div><div className="security-note"><ShieldCheck size={17} /><span>只在验证成功后加密保存，不覆盖现有仓库凭据</span></div>
      <footer><button type="button" className="secondary-button" onClick={() => setCredentialOpen(false)}>取消</button><button type="submit" className="primary-button" disabled={saving}>{saving ? "正在验证" : "验证并保存"}</button></footer></form>
    </section></div>}

    {deleting && <div className="modal-backdrop" role="presentation" onMouseDown={event => { if (event.target === event.currentTarget && !saving) setDeleting(null); }}><section className="modal credential-delete-modal" role="alertdialog" aria-modal="true" aria-labelledby="credential-delete-title">
      <header><div><h2 id="credential-delete-title">删除 OpenAPI 凭据</h2><p>删除后无法恢复</p></div><button className="icon-button" type="button" disabled={saving} onClick={() => setDeleting(null)} title="关闭"><X size={19} /></button></header>
      <form onSubmit={event => { event.preventDefault(); void deleteCredential(); }}>
        {error && <div className="error-banner"><TriangleAlert size={17} /><span>{error}</span></div>}
        <div className="credential-delete-warning"><TriangleAlert size={20} /><div><strong>{deleting.label}</strong><p>将删除这组加密凭据及已发现的仓库、SKU 覆盖记录。</p></div></div>
        <footer><button type="button" className="secondary-button" disabled={saving} onClick={() => setDeleting(null)}>取消</button><button type="submit" className="secondary-button confirm-delete-button" disabled={saving}><Trash2 size={15} />{saving ? "删除中" : "确认删除"}</button></footer>
      </form>
    </section></div>}
  </>;
}
