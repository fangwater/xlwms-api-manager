import { Boxes, CheckCircle2, ChevronRight, CircleDollarSign, Database, GitMerge, RefreshCw, TableProperties, Truck, TriangleAlert } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { fetchEvaluationDataModel } from "../evaluationApi";
import type { CanonicalEvaluationField, EvaluationDataModel, EvaluationEntity, FieldAvailability } from "../evaluationTypes";
import Chart from "../components/Chart";
import { ErrorState, LoadingState, PageHeader, dateTime, number } from "../components/Common";
import "./DeliveryEvaluationPage.css";

type ModelView = "sources" | "canonical" | "relations";

const availabilityLabels: Record<FieldAvailability, string> = {
  persisted: "已持久化",
  raw_payload: "原始载荷",
  realtime: "实时接口",
  derived: "可派生",
  partial: "部分覆盖",
  missing: "当前缺失"
};

const roleLabels: Record<string, string> = {
  key: "关联键",
  dimension: "维度",
  measure: "指标",
  timestamp: "时间",
  status: "状态",
  payload: "载荷"
};

const sourceKindLabels: Record<string, string> = {
  database_table: "数据表",
  json_array: "JSON 列表",
  realtime_api: "实时 API"
};

function Availability({ value }: { value: FieldAvailability }) {
  return <span className={`availability-pill availability-${value}`}>{availabilityLabels[value]}</span>;
}

function entityRecordCount(data: EvaluationDataModel, entity: EvaluationEntity): number {
  return data.snapshot.resources
    .filter((resource) => resource.physical_source.endsWith(`.${entity.id}`))
    .reduce((sum, resource) => sum + resource.record_count, 0);
}

function SourceFields({ data, entity, onEntityChange }: { data: EvaluationDataModel; entity: EvaluationEntity; onEntityChange: (id: string) => void }) {
  const records = entityRecordCount(data, entity);
  return <section className="evaluation-model">
    <div className="evaluation-model-toolbar">
      <label>
        <span>数据实体</span>
        <select value={entity.id} onChange={(event) => onEntityChange(event.target.value)}>
          {data.catalog.entities.map((item) => <option value={item.id} key={item.id}>{item.source_system} · {item.label}</option>)}
        </select>
      </label>
      <div className="model-count"><strong>{entity.fields.length}</strong><span>个字段</span></div>
      <div className="model-count"><strong>{number(records)}</strong><span>条当前记录</span></div>
    </div>
    <div className="evaluation-entity-summary">
      <div><span>{entity.source_system}</span><h2>{entity.label}</h2><p>{entity.description}</p></div>
      <dl>
        <div><dt>物理来源</dt><dd>{entity.physical_source}</dd></div>
        <div><dt>数据粒度</dt><dd>{entity.grain}</dd></div>
        <div><dt>来源类型</dt><dd>{sourceKindLabels[entity.source_kind]}</dd></div>
        <div><dt>关联键</dt><dd>{entity.join_keys.join(" · ") || "-"}</dd></div>
      </dl>
    </div>
    <div className="table-panel evaluation-table-panel"><div className="table-scroll"><table className="data-table evaluation-field-table">
      <thead><tr><th>字段</th><th>业务含义</th><th>类型</th><th>可用状态</th><th>角色</th><th>来源路径</th></tr></thead>
      <tbody>{entity.fields.map((field) => <tr key={field.name}>
        <td><div className="field-name"><strong>{field.name}</strong>{field.sensitive && <small>敏感字段</small>}</div></td>
        <td><div className="field-description"><strong>{field.label}</strong><small>{field.description}</small></div></td>
        <td><code>{field.data_type}</code></td>
        <td><Availability value={field.availability} /></td>
        <td>{roleLabels[field.role] || field.role}{field.nullable ? " · 可空" : ""}</td>
        <td><code className="source-path">{field.source_path}</code></td>
      </tr>)}</tbody>
    </table></div></div>
  </section>;
}

function CanonicalFields({ data }: { data: EvaluationDataModel }) {
  const fields = data.catalog.canonical_model.fields;
  const groups = ["全部", ...Array.from(new Set(fields.map((field) => field.group)))];
  const [group, setGroup] = useState("全部");
  const visible = group === "全部" ? fields : fields.filter((field) => field.group === group);
  return <section className="evaluation-model">
    <div className="canonical-summary">
      <div><span>算法输入模型</span><h2>{data.catalog.canonical_model.label}</h2><p>{data.catalog.canonical_model.description}</p></div>
      <dl><div><dt>数据粒度</dt><dd>{data.catalog.canonical_model.grain}</dd></div><div><dt>字段数量</dt><dd>{fields.length}</dd></div></dl>
    </div>
    <div className="model-group-tabs" role="tablist" aria-label="评价模型字段组">
      {groups.map((item) => <button className={group === item ? "active" : ""} onClick={() => setGroup(item)} key={item}>{item}</button>)}
    </div>
    <CanonicalFieldTable fields={visible} />
  </section>;
}

function CanonicalFieldTable({ fields }: { fields: CanonicalEvaluationField[] }) {
  return <div className="table-panel evaluation-table-panel"><div className="table-scroll"><table className="data-table evaluation-field-table canonical-field-table">
    <thead><tr><th>统一字段</th><th>业务含义</th><th>分组</th><th>可用状态</th><th>来源字段</th></tr></thead>
    <tbody>{fields.map((field) => <tr key={field.name}>
      <td><div className="field-name"><strong>{field.name}</strong><small>{field.data_type}</small></div></td>
      <td><div className="field-description"><strong>{field.label}</strong><small>{field.description}</small></div></td>
      <td>{field.group}</td>
      <td><Availability value={field.availability} /></td>
      <td><div className="source-field-list">{field.source_fields.length ? field.source_fields.map((source) => <code key={source}>{source}</code>) : <span>-</span>}</div></td>
    </tr>)}</tbody>
  </table></div></div>;
}

function Relations({ data }: { data: EvaluationDataModel }) {
  return <section className="evaluation-model">
    <div className="relation-map" aria-label="数据关联链路">
      <div><strong>XLWMS 费用链</strong><div className="relation-flow"><span>履约轨迹</span><ChevronRight /><span>资金流水</span><ChevronRight /><span>费用明细</span><ChevronRight /><span>费用项目</span></div></div>
      <div><strong>Temu 快递链</strong><div className="relation-flow"><span>平台订单</span><ChevronRight /><span>实际快递</span><ChevronRight /><span>实时运费报价</span></div></div>
    </div>
    <div className="table-panel evaluation-table-panel"><div className="table-scroll"><table className="data-table evaluation-relation-table">
      <thead><tr><th>上游实体</th><th>关联字段</th><th>下游实体</th><th>关系</th><th>覆盖</th><th>说明</th></tr></thead>
      <tbody>{data.catalog.relations.map((relation) => <tr key={relation.id}>
        <td><strong>{relation.from_entity}</strong></td>
        <td><code>{relation.from_fields.join(" + ")}</code><ChevronRight size={14} /><code>{relation.to_fields.join(" + ")}</code></td>
        <td><strong>{relation.to_entity}</strong></td>
        <td>{relation.cardinality}</td>
        <td><span className={`relation-coverage ${relation.coverage}`}>{relation.coverage === "stable" ? "稳定" : "部分覆盖"}</span></td>
        <td>{relation.description}</td>
      </tr>)}</tbody>
    </table></div></div>
  </section>;
}

export default function DeliveryEvaluationPage() {
  const [data, setData] = useState<EvaluationDataModel | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [view, setView] = useState<ModelView>("sources");
  const [entityId, setEntityId] = useState("xlwms_fulfillment_audits");
  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try { setData(await fetchEvaluationDataModel()); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "无法加载评价数据模型"); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  const resourceOption = useMemo(() => {
    const resources = [...(data?.snapshot.resources || [])].sort((left, right) => left.record_count - right.record_count);
    return {
      tooltip: { trigger: "axis", axisPointer: { type: "shadow" }, valueFormatter: (value: number) => number(value) },
      grid: { left: 112, right: 28, top: 12, bottom: 28 },
      xAxis: { type: "value", splitLine: { lineStyle: { color: "#eef0f1" } }, axisLabel: { color: "#8b9197" } },
      yAxis: { type: "category", data: resources.map((item) => `${item.scope} · ${item.label}`), axisLine: { show: false }, axisTick: { show: false }, axisLabel: { color: "#687077", width: 104, overflow: "truncate" } },
      series: [{ type: "bar", data: resources.map((item) => ({ value: item.record_count, itemStyle: { color: item.source === "XLWMS" ? "#2f7b63" : "#457e9f", borderRadius: [0, 3, 3, 0] } })), barMaxWidth: 16 }]
    };
  }, [data]);

  const coverageOption = useMemo(() => {
    const metrics = [...(data?.snapshot.coverage || [])].sort((left, right) => left.rate - right.rate);
    return {
      tooltip: { trigger: "axis", axisPointer: { type: "shadow" }, formatter: (items: Array<{ dataIndex: number }>) => { const item = metrics[items[0]?.dataIndex || 0]; return `${item.label}<br/>${number(item.available)} / ${number(item.total)} · ${item.rate}%`; } },
      grid: { left: 150, right: 44, top: 12, bottom: 28 },
      xAxis: { type: "value", min: 0, max: 100, axisLabel: { formatter: "{value}%", color: "#8b9197" }, splitLine: { lineStyle: { color: "#eef0f1" } } },
      yAxis: { type: "category", data: metrics.map((item) => item.label), axisLine: { show: false }, axisTick: { show: false }, axisLabel: { color: "#687077", width: 142, overflow: "truncate" } },
      series: [{ type: "bar", data: metrics.map((item) => ({ value: item.rate, itemStyle: { color: item.rate >= 95 ? "#2f7b63" : item.rate >= 50 ? "#c08b32" : "#c4525a", borderRadius: [0, 3, 3, 0] } })), barMaxWidth: 16, label: { show: true, position: "right", formatter: "{c}%", color: "#5d656b", fontSize: 10 } }]
    };
  }, [data]);

  if (loading && !data) return <LoadingState label="正在读取履约、快递与费用模型" />;
  if (error || !data) return <><PageHeader title="快递评价" subtitle="履约、承运商与运费数据模型" /><ErrorState message={error || "暂无数据模型"} onRetry={() => void load()} /></>;

  const coverage = Object.fromEntries(data.snapshot.coverage.map((item) => [item.id, item]));
  const quoteRecords = data.snapshot.resources.filter((item) => item.id.includes("shipping_quotes")).reduce((sum, item) => sum + item.record_count, 0);
  const shipmentRecords = data.snapshot.resources.filter((item) => item.id.includes("_temu_shipments")).reduce((sum, item) => sum + item.record_count, 0);
  const fieldCount = data.catalog.entities.reduce((sum, entity) => sum + entity.fields.length, 0);
  const metrics = [
    { label: "数据实体", value: data.catalog.entities.length, meta: `${fieldCount} 个可识别源字段`, icon: Database, tone: "green" },
    { label: "履约轨迹", value: coverage.xlwms_tracking?.total || 0, meta: `${coverage.xlwms_tracking?.rate || 0}% 已有轨迹结果`, icon: Truck, tone: "blue" },
    { label: "实时运费报价", value: quoteRecords, meta: `${coverage.temu_quote_amount?.rate || 0}% 含已选报价`, icon: CircleDollarSign, tone: "amber" },
    { label: "快递记录", value: shipmentRecords, meta: `${coverage.temu_carrier?.rate || 0}% 含承运商`, icon: Boxes, tone: "gray" },
    { label: "费用可关联订单", value: coverage.xlwms_funds?.available || 0, meta: `${coverage.xlwms_funds?.rate || 0}% 履约订单覆盖`, icon: GitMerge, tone: "red" }
  ];
  const entity = data.catalog.entities.find((item) => item.id === entityId) || data.catalog.entities[0];

  return <>
    <PageHeader title="快递评价" subtitle={`履约、承运商与运费数据模型 · 更新于 ${dateTime(data.snapshot.as_of)}`} actions={<button className="icon-button bordered" onClick={() => void load()} title="刷新数据目录"><RefreshCw className={loading ? "spin" : ""} size={18} /></button>} />
    <div className="evaluation-source-strip">
      {data.snapshot.sources.map((source) => <div key={source.id}><span className={`source-state-dot ${source.status}`} /> <strong>{source.label}</strong><small>{source.detail}</small></div>)}
      {data.snapshot.sources.some((source) => source.status !== "connected") && <TriangleAlert size={17} />}
      {data.snapshot.sources.every((source) => source.status === "connected") && <CheckCircle2 size={17} />}
    </div>
    <section className="metric-grid">{metrics.map((item) => { const Icon = item.icon; return <article className="metric-card" key={item.label}><div className={`metric-icon ${item.tone}`}><Icon size={20} /></div><div><span>{item.label}</span><strong>{number(item.value)}</strong><small>{item.meta}</small></div></article>; })}</section>
    <section className="evaluation-chart-grid">
      <div className="panel"><div className="panel-header"><div><h2>数据资源规模</h2><p>{data.snapshot.resources.length} 个已定位物理资源</p></div><Database size={18} /></div><Chart option={resourceOption} height={310} /></div>
      <div className="panel"><div className="panel-header"><div><h2>关键字段覆盖率</h2><p>只统计聚合结果，不返回订单明细</p></div><CheckCircle2 size={18} /></div><Chart option={coverageOption} height={310} /></div>
    </section>
    <div className="evaluation-view-tabs" role="tablist" aria-label="数据模型视图">
      <button className={view === "sources" ? "active" : ""} onClick={() => setView("sources")}><TableProperties size={16} />源数据字段</button>
      <button className={view === "canonical" ? "active" : ""} onClick={() => setView("canonical")}><Database size={16} />统一评价模型</button>
      <button className={view === "relations" ? "active" : ""} onClick={() => setView("relations")}><GitMerge size={16} />关联关系</button>
    </div>
    {view === "sources" && <SourceFields data={data} entity={entity} onEntityChange={setEntityId} />}
    {view === "canonical" && <CanonicalFields data={data} />}
    {view === "relations" && <Relations data={data} />}
  </>;
}
