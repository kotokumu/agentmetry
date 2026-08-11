import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { Activity, Trace } from "../model/update";
import { conversationHref, tokenEvidence } from "../model/trace-analysis";

type TraceRow = Readonly<{
  activity: Activity;
  offsetPercent: number;
  widthPercent: number;
  depth: number;
  missingParent: boolean;
}>;

@customElement("am-trace-waterfall")
export class TraceWaterfall extends LitElement {
  @property({ attribute: false }) trace?: Trace;

  static styles = css`
    :host { display: block; overflow: auto; }
    .rows { min-width: 900px; display: grid; gap: 3px; }
    .row { border-bottom: 1px solid var(--am-border); }
    summary { position: relative; display: grid; grid-template-columns: minmax(280px, 36%) minmax(120px, 15%) minmax(360px, 1fr); gap: 12px; align-items: center; min-height: 64px; padding-left: 14px; cursor: pointer; list-style: none; }
    summary::-webkit-details-marker { display: none; }
    summary::before { content: "›"; position: absolute; color: var(--am-accent); transform: translateX(-12px); }
    details[open] summary::before { transform: translateX(-12px) rotate(90deg); }
    .label { min-width: 0; padding-left: calc(var(--depth) * 14px); }
    .label strong, .label small { display: block; overflow-wrap: anywhere; }
    .label small { color: var(--am-muted); font-size: .68rem; }
    .agent { color: var(--am-text) !important; font-weight: 700; }
    .missing { color: #9f2f23 !important; }
    .usage strong, .usage small { display: block; }
    .usage strong { font-size: .84rem; }
    .usage small { color: var(--am-muted); font-size: .68rem; text-transform: capitalize; }
    .track { position: relative; height: 28px; border-radius: 5px; background: color-mix(in srgb, var(--am-track) 58%, transparent); }
    .bar { position: absolute; top: 6px; height: 16px; min-width: 4px; border-radius: 3px; background: var(--am-accent); }
    .bar.error { background: #9f2f23; }
    .event { position: absolute; top: 7px; width: 14px; height: 14px; transform: translateX(-50%) rotate(45deg); border: 2px solid var(--am-accent); background: var(--am-surface); }
    .content { margin-top: 3px; color: var(--am-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .evidence { margin: 0 0 12px calc(var(--depth) * 14px); padding: 14px; border-left: 2px solid var(--am-accent); background: var(--am-surface-strong); }
    dl { display: grid; grid-template-columns: repeat(4, minmax(130px, 1fr)); gap: 10px 16px; margin: 0; }
    dt { color: var(--am-muted); font: .65rem/1.3 "SFMono-Regular", "Cascadia Code", monospace; text-transform: uppercase; }
    dd { margin: 2px 0 0; overflow-wrap: anywhere; font-size: .78rem; }
    .message { margin: 12px 0 0; white-space: pre-wrap; overflow-wrap: anywhere; color: var(--am-text); font: inherit; }
    .conversation { display: inline-block; margin-top: 12px; color: var(--am-accent); font-weight: 700; }
  `;

  render() {
    const trace = this.trace;
    if (!trace) return null;
    return html`<div class="rows" role="list" aria-label="Trace timeline">${traceRows(trace).map((row) => {
      const activity = withAgentEvidence(trace, row.activity);
      return html`
      <details class="row" role="listitem" style=${`--depth:${row.depth}`} .open=${activity.status?.toLowerCase() === "error"}>
        <summary>
        <div class="label"><small>${activity.signal} · ${activity.source}</small><small class="agent">${agentLabel(activity)}</small><strong>${operationName(activity)}</strong>${activity.toolName && activity.toolName !== activity.name ? html`<small>${activity.name}</small>` : null}
          ${row.missingParent ? html`<small class="missing">Missing parent ${row.activity.parentSpanId}</small>` : null}
        </div>
        <div class="usage"><strong>${tokenTotal(activity)}</strong><small>${activity.status || activity.kind}</small><small>${durationLabel(activity)}</small></div>
        <div class="track" aria-label=${timingLabel(activity)}>${activity.signal === "trace"
          ? html`<span class=${`bar ${activity.status?.toLowerCase() === "error" ? "error" : ""}`} style=${`left:${row.offsetPercent}%;width:${row.widthPercent}%`}></span>`
          : html`<span class="event" style=${`left:${row.offsetPercent}%`}></span>`}
        </div>
        </summary>
        ${activityEvidence(activity)}
      </details>`;})}
    </div>`;
  }
}

export const traceRows = (trace: Trace): readonly TraceRow[] => {
  const start = new Date(trace.startedAt).getTime();
  const total = Math.max(1, new Date(trace.endedAt).getTime() - start);
  const spans = new Map(trace.activities.filter(({ signal, spanId }) => signal === "trace" && spanId).map((activity) => [activity.spanId!, activity]));
  const depthOf = (activity: Activity, seen = new Set<string>()): number => {
    const parentID = activity.parentSpanId;
    if (!parentID || seen.has(parentID)) return 0;
    const parent = spans.get(parentID);
    if (!parent) return 0;
    seen.add(parentID);
    return 1 + depthOf(parent, seen);
  };
  return trace.activities.map((activity) => {
    const activityStart = new Date(activity.startedAt ?? activity.observedAt).getTime();
    const activityEnd = new Date(activity.endedAt ?? activity.observedAt).getTime();
    const missingParent = activity.signal === "trace" && Boolean(activity.parentSpanId) && !spans.has(activity.parentSpanId!);
    return {
      activity,
      offsetPercent: clamp(((activityStart - start) / total) * 100),
      widthPercent: Math.max(.6, clamp(((Math.max(activityStart, activityEnd) - activityStart) / total) * 100)),
      depth: activity.signal === "trace" ? depthOf(activity) : 0,
      missingParent,
    };
  });
};

const clamp = (value: number) => Math.max(0, Math.min(100, value));
const agentLabel = (activity: Activity) => {
  const source = activity.agentDefinition || activity.agentId || "N/A";
  const target = activity.targetAgentId || activity.targetAgentType;
  if (target) return `${source} → ${target}`;
  return source;
};
const tokenTotal = (activity: Activity) => {
  const evidence = tokenEvidence(activity.tokens);
  const label = evidence.kind === "total"
    ? `${evidence.total?.toLocaleString()} tokens`
    : evidence.kind === "partial"
      ? `Partial · ${evidence.components.map(([name, value]) => `${name} ${value.toLocaleString()}`).join(" · ")}`
      : "N/A";
  if (evidence.kind !== "none" && !activity.contributesToTotal) return `Corroborating · ${label}`;
  if (evidence.kind !== "none") return label;
  return "N/A";
};
const durationLabel = (activity: Activity) => {
  if (activity.signal !== "trace") return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(activity.observedAt));
  const start = new Date(activity.startedAt ?? activity.observedAt).getTime();
  const end = new Date(activity.endedAt ?? activity.observedAt).getTime();
  const milliseconds = Math.max(0, end - start);
  return milliseconds < 1_000 ? `${milliseconds} ms` : `${(milliseconds / 1_000).toFixed(milliseconds < 10_000 ? 2 : 1)} s`;
};
const tokenBreakdown = (activity: Activity) => {
  const { components } = tokenEvidence(activity.tokens);
  return components.length === 0 ? "N/A" : components.map(([label, value]) => `${label} ${value.toLocaleString()}`).join(" · ");
};
const activityEvidence = (activity: Activity) => {
  const href = conversationHref(activity);
  const facts = [
    ["Kind", activity.kind],
    ["Tool name", activity.toolName || "N/A"],
    ["Telemetry name", activity.name],
    ["Status", activity.status || "N/A"],
    ["Source", activity.source || "N/A"],
    ["Conversation", activity.runId || "N/A"],
    ["Agent definition", activity.agentDefinition || "N/A"],
    ["Runtime agent ID", activity.agentId || "N/A"],
    ["Agent type", activity.agentType || "N/A"],
    ["Parent agent", activity.parentAgentId || "Root"],
    ["Model", activity.model || "N/A"],
    ["Observed at", activity.observedAt],
    ["Started at", activity.startedAt || "N/A"],
    ["Ended at", activity.endedAt || "N/A"],
    ["Trace ID", activity.traceId || "N/A"],
    ["Span ID", activity.spanId || "N/A"],
    ["Parent span", activity.parentSpanId || "Root"],
    ["Target agent", activity.targetAgentId || activity.targetAgentType
      ? [activity.targetAgentId, activity.targetAgentType].filter(Boolean).join(" · ")
      : "N/A"],
    ["Token components", tokenBreakdown(activity)],
    ["Rollup", rollupLabel(activity)],
  ];
  return html`<div class="evidence"><dl>${facts.map(([label, value]) => html`<div><dt>${label}</dt><dd>${value}</dd></div>`)}</dl>
    <pre class="message">${activity.content || "N/A"}</pre>
    ${href ? html`<a class="conversation" href=${href}>Open ${activity.spanId ? "span in" : ""} conversation</a>` : null}
  </div>`;
};

const operationName = (activity: Activity) => activity.toolName || activity.name;

const rollupLabel = (activity: Activity) => {
  const hasUsageEvidence = tokenEvidence(activity.tokens).kind !== "none" || activity.costUsd !== undefined;
  if (!hasUsageEvidence) return "N/A";
  return activity.contributesToTotal
    ? "Included in observed total"
    : "Excluded from rollup as corroborating usage evidence";
};

const withAgentEvidence = (trace: Trace, activity: Activity): Activity => {
  const agent = trace.agents.find(({ sourceId, conversationId, agentId }) =>
    sourceId === activity.source && conversationId === activity.runId && agentId === activity.agentId);
  if (!agent) return activity;
  return {
    ...activity,
    agentDefinition: activity.agentDefinition || agent.agentDefinition,
    agentType: activity.agentType || agent.agentType,
    parentAgentId: activity.parentAgentId || agent.parentAgentId,
    model: activity.model || agent.model || "",
  };
};
const timingLabel = (activity: Activity) => activity.signal === "trace"
  ? `${activity.startedAt ?? activity.observedAt} to ${activity.endedAt ?? activity.observedAt}`
  : `Event at ${activity.observedAt}`;

declare global { interface HTMLElementTagNameMap { "am-trace-waterfall": TraceWaterfall } }
