import { AlertTriangle, ChevronLeft, ChevronRight, Inbox, LoaderCircle, RefreshCw } from "lucide-react";
import type { ReactNode } from "react";

export function PageHeader({ title, subtitle, actions }: { title: string; subtitle: string; actions?: ReactNode }) {
  return <div className="page-header"><div><h1>{title}</h1><p>{subtitle}</p></div>{actions && <div className="page-actions">{actions}</div>}</div>;
}

export function LoadingState({ label = "正在加载" }: { label?: string }) {
  return <div className="state-view"><LoaderCircle className="spin" size={22} /><span>{label}</span></div>;
}

export function EmptyState({ label = "暂无数据" }: { label?: string }) {
  return <div className="state-view"><Inbox size={22} /><span>{label}</span></div>;
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return <div className="error-banner"><AlertTriangle size={18} /><span>{message}</span>{onRetry && <button className="icon-button" onClick={onRetry} title="重新加载"><RefreshCw size={17} /></button>}</div>;
}

export function Pagination({ page, pages, total, onChange }: { page: number; pages: number; total: number; onChange: (page: number) => void }) {
  return <div className="pagination"><span>共 {number(total)} 条</span><div><button className="icon-button bordered" disabled={page <= 1} onClick={() => onChange(page - 1)} title="上一页"><ChevronLeft size={17} /></button><b>{page} / {Math.max(pages, 1)}</b><button className="icon-button bordered" disabled={page >= pages} onClick={() => onChange(page + 1)} title="下一页"><ChevronRight size={17} /></button></div></div>;
}

export function StatusBadge({ status }: { status: string }) {
  const labels: Record<string, string> = { running: "进行中", succeeded: "已完成", failed: "失败", pending: "待同步", success: "已同步", error: "异常" };
  return <span className={`status-badge ${status}`}>{labels[status] ?? status}</span>;
}

export function number(value: number, digits = 0): string {
  return new Intl.NumberFormat("zh-CN", { maximumFractionDigits: digits }).format(value || 0);
}

export function money(value: number, currency = ""): string {
  return `${number(value, 2)}${currency ? ` ${currency}` : ""}`;
}

export function dateTime(value?: string): string {
  if (!value) return "-";
  const parsed = new Date(value.replace(" ", "T"));
  return Number.isNaN(parsed.getTime()) ? value : new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(parsed);
}
