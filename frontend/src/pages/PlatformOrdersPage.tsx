import { AlertTriangle, CheckCircle2, Clock3, Eye, KeyRound, ListTodo, PackageSearch, RefreshCw, Search, ShieldCheck, Store, Truck, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { APIError, api } from "../api";
import { EmptyState, ErrorState, LoadingState, PageHeader, Pagination, dateTime, number } from "../components/Common";
import type { PendingPlatformOrder, PlatformOrderAccountOption, PlatformOrderAssignmentResult, PlatformOrderProduct, PendingPlatformOrderPage, PlatformOrderRoutingPreview } from "../types";
import "./PlatformOrdersPage.css";

const defaultPlatformOrderAccounts: PlatformOrderAccountOption[] = [{
  key: "arp", label: "ARP 账户", warehouse_codes: []
}];

export default function PlatformOrdersPage() {
  const [page, setPage] = useState(1);
  const [account, setAccount] = useState("arp");
  const [accounts, setAccounts] = useState(defaultPlatformOrderAccounts);
  const [accountError, setAccountError] = useState("");
  const [searchInput, setSearchInput] = useState("");
  const [query, setQuery] = useState("");
  const [data, setData] = useState<PendingPlatformOrderPage | null>(null);
  const [detail, setDetail] = useState<PendingPlatformOrder | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [selectedOrderNos, setSelectedOrderNos] = useState<string[]>([]);
  const [routingOpen, setRoutingOpen] = useState(false);
  const [assignmentResult, setAssignmentResult] = useState<PlatformOrderAssignmentResult | null>(null);
  const [accountEditorOpen, setAccountEditorOpen] = useState(false);
  const [accountForm, setAccountForm] = useState({ username: "", password: "", newPassword: "", confirmNewPassword: "" });
  const [accountFormError, setAccountFormError] = useState("");
  const [passwordUpgradeRequired, setPasswordUpgradeRequired] = useState(false);
  const [savingAccount, setSavingAccount] = useState(false);
  const loadSequence = useRef(0);

  const load = useCallback(async () => {
    const sequence = ++loadSequence.current;
    setLoading(true);
    setError("");
    try {
      const next = await api.pendingPlatformOrders({ account, q: query || undefined, page, pageSize: 30 });
      if (sequence !== loadSequence.current) return;
      setData(next);
      setSelectedOrderNos([]);
    } catch (reason) {
      if (sequence !== loadSequence.current) return;
      setError(reason instanceof Error ? reason.message : "无法加载平台订单");
    } finally {
      if (sequence === loadSequence.current) setLoading(false);
    }
  }, [account, page, query]);

  useEffect(() => { void load(); }, [load]);
  const applyAccounts = useCallback((next: PlatformOrderAccountOption[]) => {
    if (next.length === 0) return;
    setAccounts(next);
    const ready = next.filter((item) => item.available !== false);
    setAccount((current) => {
      const selected = next.find((item) => item.key === current);
      if (selected && selected.available !== false) return current;
      return (ready[0] || next[0]).key;
    });
    const offline = next.filter((item) => item.available === false);
    setAccountError(offline.length ? offline.map((item) => item.label + "：" + (item.error || "已掉线")).join("；") : "");
  }, []);
  const loadAccounts = useCallback(async () => {
    try {
      applyAccounts(await api.platformOrderAccounts());
    } catch (reason) {
      setAccountError(reason instanceof Error ? reason.message : "无法读取 OMS 账户列表");
    }
  }, [applyAccounts]);
  useEffect(() => { void loadAccounts(); }, [loadAccounts]);

  const pageOrderNos = data?.records.map((order) => order.platformOrderNo).filter(Boolean) ?? [];
  const allPageSelected = pageOrderNos.length > 0 && pageOrderNos.every((orderNo) => selectedOrderNos.includes(orderNo));
  const selectedAccount = accounts.find((item) => item.key === account) ?? accounts[0];
  const selectedAccountLabel = selectedAccount?.label || "OMS 账户";
  const offlineAccounts = accounts.filter((item) => item.available === false);
  const selectedAccountOffline = selectedAccount?.available === false;
  const selectedAccountStatus = selectedAccountOffline
    ? selectedAccount.label + "已掉线" + (selectedAccount.error ? " · " + selectedAccount.error : "")
    : selectedAccountLabel;
  const accountAlertTitle = offlineAccounts.length === 0
    ? "无法读取领星账户"
    : offlineAccounts.length === accounts.length ? "领星账户已全部掉线" : "部分领星账户已掉线";

  function switchAccount(next: string) {
    if (next === account) return;
    loadSequence.current++;
    setAccount(next);
    setPage(1);
    setData(null);
    setError("");
    setSelectedOrderNos([]);
    setAssignmentResult(null);
    setDetail(null);
  }

  function toggleOrder(orderNo: string) {
    setSelectedOrderNos((selected) => selected.includes(orderNo) ? selected.filter((value) => value !== orderNo) : [...selected, orderNo]);
  }

  function togglePage() {
    setSelectedOrderNos(allPageSelected ? [] : pageOrderNos);
  }

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const next = searchInput.trim();
    if (next === query && page === 1) {
      void load();
      return;
    }
    setPage(1);
    setQuery(next);
  }

  function clearSearch() {
    setSearchInput("");
    setPage(1);
    setQuery("");
  }

  function openAccountEditor() {
    setAccountForm({ username: "", password: "", newPassword: "", confirmNewPassword: "" });
    setAccountFormError("");
    setPasswordUpgradeRequired(false);
    setAccountEditorOpen(true);
  }

  async function saveAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSavingAccount(true);
    setAccountFormError("");
    try {
      const nextAccounts = passwordUpgradeRequired
        ? await api.upgradePlatformOrderAccountPassword(account, {
          username: accountForm.username,
          current_password: accountForm.password,
          new_password: accountForm.newPassword,
          confirm_new_password: accountForm.confirmNewPassword
        })
        : await api.updatePlatformOrderAccount(account, { username: accountForm.username, password: accountForm.password });
      applyAccounts(nextAccounts);
      setAccountEditorOpen(false);
      setAccountForm({ username: "", password: "", newPassword: "", confirmNewPassword: "" });
      setPasswordUpgradeRequired(false);
      setPage(1);
      setData(null);
      void load();
    } catch (reason) {
      if (reason instanceof APIError && reason.code === "OMS_PASSWORD_UPDATE_REQUIRED") {
        setPasswordUpgradeRequired(true);
        setAccountForm((current) => ({ ...current, newPassword: "", confirmNewPassword: "" }));
        setAccountFormError("");
        return;
      }
      setAccountFormError(reason instanceof Error ? reason.message : "无法更新 OMS 账户");
    } finally {
      setSavingAccount(false);
    }
  }

  return <>
    <PageHeader
      title="平台订单待处理"
      subtitle={(selectedAccountOffline ? "领星 OMS 已掉线 · " : "实时读取领星 OMS · ") + selectedAccountStatus + (data?.queried_at ? " · 最近查询 " + dateTime(data.queried_at) : "")}
      actions={<>
        <button className="primary-button platform-order-action" disabled={selectedOrderNos.length === 0} onClick={() => setRoutingOpen(true)} title="分配仓库和物流"><Truck size={16} /><span>分配仓库和物流</span></button>
        <button className="icon-button bordered" onClick={() => void load()} title="刷新"><RefreshCw size={18} className={loading ? "spin" : ""} /></button>
      </>}
    />
    {accountError && <section className="error-banner platform-order-account-alert" role="alert">
      <AlertTriangle size={18} />
      <div>
        <strong>{accountAlertTitle}</strong>
        <span>{accountError}</span>
      </div>
    </section>}
    {data && <section className="platform-order-overview" aria-label="待处理订单概况">
      <div className="platform-order-overview-icon"><ListTodo size={22} /></div>
      <div><span>{query ? "查询结果" : "待处理总数"}</span><strong>{number(data.total)}</strong></div>
      <div><span>当前页</span><strong>{data.page} / {Math.max(data.pages, 1)}</strong></div>
      <div><span>已选择</span><strong>{selectedOrderNos.length}</strong></div>
      <small>{selectedAccountLabel} · {query ? "平台单号精确查询" : "OMS 实时数据"}</small>
    </section>}
    {assignmentResult && <section className={`platform-assignment-result ${assignmentResult.failed ? "partial" : "success"}`} role="status">
      {assignmentResult.failed ? <AlertTriangle size={19} /> : <CheckCircle2 size={19} />}
      <div><strong>物流匹配已完成</strong><span>成功 {assignmentResult.success} 单，失败 {assignmentResult.failed} 单 · {(assignmentResult.warehouse_codes || [assignmentResult.warehouse_code]).filter(Boolean).join("、")}</span>
        {assignmentResult.failures.length > 0 && <small>{assignmentResult.failures.slice(0, 3).map((failure) => `${failure.platform_order_no}: ${failure.error}`).join("；")}</small>}
      </div>
      <button className="icon-button" onClick={() => setAssignmentResult(null)} title="关闭结果"><X size={16} /></button>
    </section>}
    <form className="filter-bar platform-order-filters" onSubmit={submitSearch}>
      <label className="select-field platform-order-account-select" title={selectedAccount?.warehouse_codes.join("、") || selectedAccountLabel}>
        <Store size={16} />
        <select aria-label="OMS 账户" value={account} onChange={(event) => switchAccount(event.target.value)}>
          {accounts.map((item) => <option key={item.key} value={item.key}>{item.available === false ? item.label + "（已掉线）" : item.label}</option>)}
        </select>
      </label>
      <button className="secondary-button platform-order-account-edit" type="button" onClick={openAccountEditor} title="重新登录当前 OMS 账户"><KeyRound size={15} /><span>重新登录</span></button>
      <label className="search-field"><Search size={17} /><input aria-label="平台单号搜索" value={searchInput} onChange={(event) => setSearchInput(event.target.value)} placeholder="输入平台单号精确查询" /></label>
      <button className="secondary-button" type="submit" disabled={loading}>查询</button>
      {query && <button className="icon-button bordered" type="button" onClick={clearSearch} title="清除搜索" aria-label="清除搜索"><X size={16} /></button>}
    </form>
    {error && <ErrorState message={error} onRetry={() => void load()} />}
    {loading && !data ? <LoadingState label="正在读取 OMS 待处理订单" /> : data?.records.length ? <div className="table-panel platform-order-panel"><div className="table-scroll"><table className="data-table platform-order-table">
      <thead><tr><th className="order-select-cell"><input type="checkbox" aria-label="选择当前页订单" checked={allPageSelected} onChange={togglePage} /></th><th>OMS / 平台单号</th><th>平台 / 店铺</th><th>商品明细</th><th>发货仓 / 目的国</th><th>物流</th><th>下单 / 要求送达</th><th>状态</th><th>详情</th></tr></thead>
      <tbody>{data.records.map((order, index) => {
        const items = products(order);
        const first = items[0];
        return <tr key={order.orderNo || order.platformOrderNo || index}>
          <td className="order-select-cell"><input type="checkbox" aria-label={`选择平台订单 ${order.platformOrderNo || order.orderNo}`} disabled={!order.platformOrderNo} checked={Boolean(order.platformOrderNo && selectedOrderNos.includes(order.platformOrderNo))} onChange={() => { if (order.platformOrderNo) toggleOrder(order.platformOrderNo); }} /></td>
          <td><div className="primary-cell"><strong>{order.orderNo || "-"}</strong><small>{order.platformOrderNo || "-"}</small></div></td>
          <td><div className="primary-cell"><strong>{order.platformChannelName || order.platformCode || "-"}</strong><small>{order.storeName || order.storeCode || siteName(order)}</small></div></td>
          <td><div className="order-product-cell"><strong>{first?.sku || "-"}</strong><small>{first ? productLabel(first) : "暂无商品信息"}</small>{items.length > 1 && <span>另 {items.length - 1} 项</span>}</div></td>
          <td><div className="primary-cell"><strong>{order.sendWhName || order.sendWhCode || "-"}</strong><small>{order.receiptCountryName || order.receiptCountryCode || "-"}</small></div></td>
          <td><div className="primary-cell"><strong>{order.logisticsChannelName || order.logisticsCarrierName || "-"}</strong><small>{order.trackNo || "暂无跟踪号"}</small></div></td>
          <td><div className="primary-cell"><strong>{dateTime(order.orderTime || order.createTime)}</strong><small>{dateTime(order.requestDeliveryTime)}</small></div></td>
          <td><span className="audit-status oms-pending"><Clock3 size={14} />待处理</span></td>
          <td><button className="icon-button" onClick={() => setDetail(order)} title="查看平台订单详情"><Eye size={16} /></button></td>
        </tr>;
      })}</tbody>
    </table></div></div> : !error && <EmptyState label={query ? "未找到该待处理平台订单" : "当前没有待处理平台订单"} />}
    {data && data.total > 0 && <Pagination page={data.page} pages={data.pages} total={data.total} onChange={setPage} />}
    {routingOpen && <PlatformOrderRoutingDialog
      platformOrderNos={selectedOrderNos}
      account={account}
      onClose={() => setRoutingOpen(false)}
      onComplete={(result) => {
        setAssignmentResult(result);
        setRoutingOpen(false);
        void load();
        window.setTimeout(() => void load(), 1500);
        window.setTimeout(() => void load(), 5000);
      }}
    />}
    {detail && <OrderDetail order={detail} onClose={() => setDetail(null)} />}
    {accountEditorOpen && <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (!savingAccount && event.target === event.currentTarget) setAccountEditorOpen(false); }}>
      <section className="modal account-modal" role="dialog" aria-modal="true" aria-labelledby="platform-account-modal-title">
        <header><div><h2 id="platform-account-modal-title">{passwordUpgradeRequired ? "更新领星密码" : "重新登录 OMS"}</h2><p>{selectedAccountLabel}{selectedAccount?.username_hint ? " · 当前 " + selectedAccount.username_hint : ""}</p></div><button className="icon-button" type="button" disabled={savingAccount} onClick={() => setAccountEditorOpen(false)} title="关闭"><X size={19} /></button></header>
        <form onSubmit={(event) => void saveAccount(event)}>
          {accountFormError && <div className="error-banner"><span>{accountFormError}</span></div>}
          {passwordUpgradeRequired && <div className="password-upgrade-notice" role="status"><AlertTriangle size={17} /><span>领星要求更新登录密码。设置新密码后，中台会重新验证并保存。</span></div>}
          <div className="form-grid">
            <label className="full"><span>OMS 账号</span><input required disabled={passwordUpgradeRequired} autoComplete="username" value={accountForm.username} onChange={(event) => setAccountForm({ ...accountForm, username: event.target.value })} placeholder="输入登录账号" /></label>
            <label className="full"><span>{passwordUpgradeRequired ? "当前密码" : "OMS 密码"}</span><input required disabled={passwordUpgradeRequired} type="password" autoComplete="current-password" value={accountForm.password} onChange={(event) => setAccountForm({ ...accountForm, password: event.target.value })} placeholder="输入当前登录密码" /></label>
            {passwordUpgradeRequired && <>
              <label className="full"><span>新密码</span><input required minLength={12} maxLength={20} type="password" autoComplete="new-password" value={accountForm.newPassword} onChange={(event) => setAccountForm({ ...accountForm, newPassword: event.target.value })} placeholder="12-20 位，含大小写字母、数字和特殊字符" /></label>
              <label className="full"><span>确认新密码</span><input required minLength={12} maxLength={20} type="password" autoComplete="new-password" value={accountForm.confirmNewPassword} onChange={(event) => setAccountForm({ ...accountForm, confirmNewPassword: event.target.value })} placeholder="再次输入新密码" /></label>
            </>}
          </div>
          <div className="security-note"><ShieldCheck size={17} /><span>{passwordUpgradeRequired ? "更新成功后会重新登录，并加密保存新密码" : "登录验证成功后才会加密保存账号和密码"}</span></div>
          <footer>
            <button type="button" className="secondary-button" disabled={savingAccount} onClick={() => setAccountEditorOpen(false)}>取消</button>
            <button type="submit" className="primary-button" disabled={savingAccount}>{savingAccount ? (passwordUpgradeRequired ? "正在更新" : "正在验证") : (passwordUpgradeRequired ? "更新密码并登录" : "验证并重新登录")}</button>
          </footer>
        </form>
      </section>
    </div>}
  </>;
}

function PlatformOrderRoutingDialog({ platformOrderNos, account, onClose, onComplete }: {
  platformOrderNos: string[];
  account: string;
  onClose: () => void;
  onComplete: (result: PlatformOrderAssignmentResult) => void;
}) {
  const [preview, setPreview] = useState<PlatformOrderRoutingPreview | null>(null);
  const [optionsLoading, setOptionsLoading] = useState(true);
  const [optionsError, setOptionsError] = useState("");
  const [carrier, setCarrier] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");

  const loadOptions = useCallback(async () => {
    setOptionsLoading(true);
    setOptionsError("");
    try {
      const next = await api.platformOrderRoutingPreview(platformOrderNos, account);
      setPreview(next);
      setCarrier(next.carriers.find((item) => item.value === "_AUTO_MATCH_")?.value || next.carriers[0]?.value || "");
    } catch (reason) {
      setOptionsError(reason instanceof Error ? reason.message : "无法加载仓库和物流选项");
    } finally {
      setOptionsLoading(false);
    }
  }, [account, platformOrderNos]);

  useEffect(() => { void loadOptions(); }, [loadOptions]);

  const canSubmit = Boolean(preview?.ready && carrier && confirmed && !submitting);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSubmit) return;
    setSubmitting(true);
    setSubmitError("");
    try {
      const result = await api.assignAndApprovePlatformOrders({
        platform_order_nos: platformOrderNos,
        account,
        logistics_carrier: carrier,
        confirmation: "CONFIRM_AND_APPROVE"
      });
      onComplete(result);
    } catch (reason) {
      setSubmitError(reason instanceof Error ? reason.message : "物流匹配失败");
    } finally {
      setSubmitting(false);
    }
  }

  return <div className="modal-backdrop" onMouseDown={(event) => { if (!submitting && event.target === event.currentTarget) onClose(); }}>
    <section className="modal platform-routing-modal" role="dialog" aria-modal="true" aria-labelledby="platform-routing-title">
      <header><div><h2 id="platform-routing-title">分配仓库和物流</h2><p>已选择 {platformOrderNos.length} 个待处理平台单</p></div><button className="icon-button" type="button" disabled={submitting} onClick={onClose} title="关闭"><X size={18} /></button></header>
      <form onSubmit={(event) => void submit(event)}>
        {optionsLoading ? <LoadingState label="正在根据购面单结果匹配实际发货仓库" /> : !preview ? <div className="routing-options-error"><AlertTriangle size={18} /><span>{optionsError || "无法加载自动仓库匹配"}</span><button className="secondary-button" type="button" onClick={() => void loadOptions()}>重试</button></div> : <>
          <div className="routing-fixed-channel"><span>平台面单渠道</span><strong>{preview.channel_name}</strong><small>{preview.channel_code}</small></div>
          <section className={`routing-auto-routes ${preview.ready ? "ready" : "blocked"}`} aria-label="自动匹配实际发货仓库">
            <div className="routing-auto-title">{preview.ready ? <CheckCircle2 size={17} /> : <AlertTriangle size={17} />}<strong>按购面单结果匹配发货仓库</strong><span>{preview.routes.length} / {platformOrderNos.length} 单</span></div>
            <ul>{preview.routes.map((route) => <li key={route.platform_order_no}>
              <div><strong>{route.platform_order_no}</strong><small>{route.platform_warehouse_name || route.platform_warehouse_id}</small></div>
              <span><Truck size={15} /><b>{route.warehouse_name || route.warehouse_code}</b><small>{route.warehouse_code}</small></span>
            </li>)}
            {preview.unresolved.map((item) => <li className="unresolved" key={item.platform_order_no}><div><strong>{item.platform_order_no}</strong><small>{item.reason}</small></div><span><AlertTriangle size={15} /><b>无法匹配</b></span></li>)}</ul>
          </section>
          <fieldset className="routing-carriers"><legend>物流商</legend><div>
            {preview.carriers.map((item) => <label className={carrier === item.value ? "active" : ""} key={item.value}><input type="radio" name="logistics-carrier" value={item.value} checked={carrier === item.value} onChange={(event) => setCarrier(event.target.value)} /><span>{item.label}</span></label>)}
          </div></fieldset>
          <label className="routing-confirmation"><input type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} /><span>我确认按以上购面单仓库分配订单并立即审核</span></label>
          {submitError && <div className="routing-submit-error" role="alert"><AlertTriangle size={16} /><span>{submitError}</span></div>}
          <footer><button className="secondary-button" type="button" disabled={submitting} onClick={onClose}>取消</button><button className="primary-button" type="submit" disabled={!canSubmit}><ShieldCheck size={16} />{submitting ? "正在审核" : "确定并审核"}</button></footer>
        </>}
      </form>
    </section>
  </div>;
}

function OrderDetail({ order, onClose }: { order: PendingPlatformOrder; onClose: () => void }) {
  const items = products(order);
  const warnings = [
    ["订单备注", order.remark],
    ["异常原因", order.exceptionCause],
    ["审核原因", order.auditCause],
    ["送达时间识别", order.requestDeliveryTimeFailReason],
    ["标记发货", order.markShipmentFailReason],
    ["拆单说明", order.platformSplitReason]
  ].filter((item) => item[1]);
  return <div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
    <section className="modal platform-order-detail" role="dialog" aria-modal="true" aria-labelledby="platform-order-detail-title">
      <header><div><h2 id="platform-order-detail-title">{order.platformOrderNo || order.orderNo}</h2><p>{order.orderNo || "平台订单详情"}</p></div><button className="icon-button" onClick={onClose} title="关闭"><X size={18} /></button></header>
      <div className="detail-body">
        <dl className="detail-grid">
          <Detail label="处理状态" value="待处理" />
          <Detail label="平台" value={order.platformChannelName || order.platformCode} />
          <Detail label="店铺" value={order.storeName || order.storeCode} />
          <Detail label="站点" value={siteName(order)} />
          <Detail label="发货仓" value={order.sendWhName || order.sendWhCode} />
          <Detail label="目的国家" value={order.receiptCountryName || order.receiptCountryCode} />
          <Detail label="物流承运商" value={order.logisticsCarrierName || order.logisticsCarrier} />
          <Detail label="物流渠道" value={order.logisticsChannelName || order.logisticsChannelCode} />
          <Detail label="跟踪号" value={order.trackNo} />
          <Detail label="下单时间" value={dateTime(order.orderTime)} />
          <Detail label="支付时间" value={dateTime(order.payTime)} />
          <Detail label="OMS 创建时间" value={dateTime(order.createTime)} />
          <Detail label="要求送达时间" value={dateTime(order.requestDeliveryTime)} />
          <Detail label="审核时间" value={dateTime(order.auditTime)} />
          <Detail label="平台订单类型" value={order.platformOrderType} />
        </dl>
        <section className="platform-order-products">
          <h3><PackageSearch size={16} />商品明细 <span>{items.length} 项</span></h3>
          {items.length ? <div className="table-scroll"><table className="data-table"><thead><tr><th>SKU</th><th>产品</th><th>数量</th></tr></thead><tbody>{items.map((item, index) => <tr key={item.sku + "-" + index}><td><strong>{item.sku || "-"}</strong></td><td>{item.productName || "-"}</td><td>{number(item.qty)}</td></tr>)}</tbody></table></div> : <EmptyState label="暂无商品信息" />}
        </section>
        <section className="platform-order-routing">
          <div><Store size={16} /><span>{order.storeName || order.storeCode || "未识别店铺"}</span></div>
          <div><Truck size={16} /><span>{order.sendWhName || order.sendWhCode || "未指定发货仓"}</span></div>
        </section>
        {warnings.length > 0 && <dl className="platform-order-notes">{warnings.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>}
      </div>
    </section>
  </div>;
}

function products(order: PendingPlatformOrder): PlatformOrderProduct[] {
  return order.skuList?.length ? order.skuList : order.platformSkuList || [];
}

function productLabel(product: PlatformOrderProduct): string {
  const name = product.productName || "未命名产品";
  return name + " × " + number(product.qty);
}

function siteName(order: PendingPlatformOrder): string {
  return order.siteNameCn || order.siteNameEn || order.site || "-";
}

function Detail({ label, value }: { label: string; value: unknown }) {
  return <div><dt>{label}</dt><dd>{value === undefined || value === null || value === "" ? "-" : String(value)}</dd></div>;
}
