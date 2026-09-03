import { LoaderCircle, RotateCcw, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../../api";
import { EmptyState, ErrorState, LoadingState, Pagination } from "../../components/Common";
import type { OMSAccountSummary, PlatformSKUOMSAccount, PlatformSKUOMSAccountPage } from "../../types";

export default function AccountRoutesView({ platform, platformLabel }: { platform: string; platformLabel: string }) {
  const [accounts, setAccounts] = useState<OMSAccountSummary[]>([]);
  const [routes, setRoutes] = useState<PlatformSKUOMSAccountPage | null>(null);
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState("");

  useEffect(() => {
    const timer = window.setTimeout(() => { setDebouncedQuery(query.trim()); setPage(1); }, 250);
    return () => window.clearTimeout(timer);
  }, [query]);

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const [accountItems, routePage] = await Promise.all([
        api.fulfillmentAccounts(),
        api.platformSKUOMSAccounts({ platform, q: debouncedQuery, page, pageSize: 30 })
      ]);
      setAccounts(accountItems);
      setRoutes(routePage);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "账户路由加载失败");
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { void load(); }, [platform, page, debouncedQuery]);

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
      setRoutes((current) => current ? {
        ...current,
        records: current.records.map((route) => route.warehouse_sku === item.warehouse_sku ? saved : route)
      } : current);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "SKU 账户路由保存失败");
    } finally {
      setSaving("");
    }
  };

  return <div className="policy-view-content account-route-view">
    {error && (
      <ErrorState message={error} onRetry={() => void load()}/>
    )}
    <section className="account-route-section standalone">
      <div className="account-section-heading"><div><h2>{platformLabel} SKU 账户路由</h2><p>平台 SKU 的发货账户归属</p></div></div>
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
  </div>;
}
