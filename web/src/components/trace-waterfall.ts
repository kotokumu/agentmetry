import { LitElement, css, html, type PropertyValues } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { Activity, Trace } from "../model/telemetry";
import { conversationHref, type ConversationTarget, tokenEvidence } from "../model/trace-analysis";
import { agentDisplayLabel } from "../model/agent-label";
import { NOT_APPLICABLE, NOT_REPORTED } from "../presentation/missing-data";
import "./token-breakdown";

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
  @property({ type: Boolean }) hasMore = false;
  @property({ type: Boolean }) loading = false;
  @property({ attribute: false }) onLoadMore?: () => void;
  @property({ attribute: false }) locationForConversation?: (target: ConversationTarget) => string;
  private loadMoreObserver?: IntersectionObserver;
  private renderOffset = 0;

  static styles = css`
    :host { display: block; overflow: auto; }
    .rows { min-width: 900px; display: grid; gap: 1px; }
    .row { border-bottom: 1px solid var(--am-border); transition: background .18s ease; }
    .row:hover { background: rgba(255, 255, 255, .015); }
    summary { position: relative; display: grid; grid-template-columns: minmax(280px, 36%) minmax(120px, 15%) minmax(360px, 1fr); gap: 10px; align-items: center; min-height: 54px; padding-left: 14px; cursor: pointer; list-style: none; }
    summary::-webkit-details-marker { display: none; }
    summary::before { content: "›"; position: absolute; color: var(--am-accent); transform: translateX(-12px); }
    details[open] summary::before { transform: translateX(-12px) rotate(90deg); }
    .label { min-width: 0; padding-left: calc(var(--depth) * 14px); }
    .label strong, .label small { display: block; overflow-wrap: anywhere; }
    .label small { color: var(--am-muted); font-size: .68rem; }
    .agent { color: var(--am-text) !important; font-weight: 700; }
    .missing { color: var(--am-danger) !important; }
    .usage strong, .usage small { display: block; }
    .usage strong { font-size: .84rem; }
    .usage small { color: var(--am-muted); font-size: .68rem; text-transform: capitalize; }
    .track { position: relative; height: 24px; border: 1px solid rgba(155, 190, 213, .06); border-radius: 4px; background: color-mix(in srgb, var(--am-track) 72%, transparent); }
    .bar { position: absolute; top: 5px; height: 12px; min-width: 4px; border-radius: 2px; background: linear-gradient(90deg, var(--am-accent), var(--am-secondary)); box-shadow: 0 0 10px rgba(var(--am-accent-rgb), .2); }
    .bar.error { background: var(--am-danger); box-shadow: 0 0 10px color-mix(in srgb, var(--am-danger) 35%, transparent); }
    .event { position: absolute; top: 6px; width: 11px; height: 11px; transform: translateX(-50%) rotate(45deg); border: 2px solid var(--am-accent); background: var(--am-surface); }
    .content { margin-top: 3px; color: var(--am-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .evidence { margin: 0 0 10px calc(var(--depth) * 14px); padding: 11px 12px; border: 1px solid var(--am-border); border-left: 2px solid var(--am-accent); border-radius: 0 7px 7px 0; background: linear-gradient(90deg, var(--am-accent-soft), var(--am-surface-strong) 30%); }
    dl { display: grid; grid-template-columns: repeat(4, minmax(130px, 1fr)); gap: 8px 14px; margin: 0; }
    dt { color: var(--am-muted); font: .65rem/1.3 "SFMono-Regular", "Cascadia Code", monospace; text-transform: uppercase; }
    dd { margin: 2px 0 0; overflow-wrap: anywhere; font-size: .78rem; }
    .message { margin: 12px 0 0; white-space: pre-wrap; overflow-wrap: anywhere; color: var(--am-text); font: inherit; }
    .conversation { display: inline-block; margin-top: 12px; color: var(--am-accent); font-weight: 700; }
    .load-status { min-height: 24px; padding: 12px 0 4px; color: var(--am-muted); text-align: center; font-size: .76rem; }
    .window-nav { display: flex; justify-content: center; gap: 8px; padding: 10px; }
    .window-nav button { border: 1px solid var(--am-border); border-radius: 7px; background: var(--am-surface-raised); color: var(--am-text); padding: 8px 14px; cursor: pointer; }
  `;

  render() {
    const trace = this.trace;
    if (!trace) return null;
    const hasPreviousWindow = this.renderOffset > 0;
    const hasNextWindow = this.renderOffset + 200 < trace.activities.length;
    return html`${hasPreviousWindow ? this.windowNavigation("previous") : null}<div class="rows" role="list" aria-label="Trace timeline">${traceRows(trace, this.renderOffset).map((row) => {
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
        ${this.activityEvidence(activity)}
      </details>`;})}
    </div>${hasNextWindow ? this.windowNavigation("next") : null}${this.hasMore && !hasNextWindow ? html`<div class="load-status" role="status" aria-live="polite">${this.loading ? "Loading more trace data…" : ""}</div>` : null}`;
  }

  protected willUpdate(changed: PropertyValues<this>) {
    if (!changed.has("trace")) return;
    const previous = changed.get("trace") as Trace | undefined;
    if (previous?.traceId !== this.trace?.traceId) this.renderOffset = 0;
    if (this.trace && this.renderOffset >= this.trace.activities.length) {
      this.renderOffset = Math.max(0, this.trace.activities.length - 200);
    }
  }

  protected updated(changed: Map<string, unknown>) {
    if (changed.has("trace") || changed.has("hasMore") || changed.has("loading")) this.observeLoadMoreSentinel();
  }

  disconnectedCallback() {
    this.loadMoreObserver?.disconnect();
    super.disconnectedCallback();
  }

  private observeLoadMoreSentinel() {
    this.loadMoreObserver?.disconnect();
    if (!this.hasMore || this.loading) return;
    if (typeof IntersectionObserver === "undefined") return;
    const sentinel = this.renderRoot.querySelector(".load-status");
    if (!sentinel) return;
    this.loadMoreObserver = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) this.requestMore();
    }, { rootMargin: "0px 0px 600px" });
    this.loadMoreObserver.observe(sentinel);
  }

  private windowNavigation(direction: "previous" | "next") {
    return html`<div class="window-nav"><button type="button" @click=${() => this.moveWindow(direction)}>Show ${direction} loaded trace activities</button></div>`;
  }

  private moveWindow(direction: "previous" | "next") {
    const length = this.trace?.activities.length ?? 0;
    this.renderOffset = direction === "previous"
      ? Math.max(0, this.renderOffset - 200)
      : Math.min(Math.max(0, length - 1), this.renderOffset + 200);
    this.requestUpdate();
  }

  private requestMore() {
    if (this.loading) return;
    if (this.onLoadMore) {
      this.onLoadMore();
      return;
    }
    this.dispatchEvent(new CustomEvent("trace-activities-needed", { bubbles: true, composed: true }));
  }

  private activityEvidence(activity: Activity) {
    const target = conversationTarget(activity);
    const href = target && this.locationForConversation
      ? this.locationForConversation(target)
      : conversationHref(activity);
    return activityEvidence(activity, href
      ? (event: MouseEvent) => this.conversationSelected(event, activity)
      : undefined, href);
  }

  private conversationSelected(event: MouseEvent, activity: Activity) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    this.dispatchEvent(new CustomEvent("conversation-selected-from-trace", {
      detail: {
        sourceId: activity.source,
        conversationId: activity.runId,
        traceId: activity.traceId || activity.relatedTraceId,
        spanId: activity.spanId || activity.relatedSpanId,
      },
      bubbles: true,
      composed: true,
    }));
  }
}

export const traceRows = (trace: Trace, offset = 0): readonly TraceRow[] => {
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
  return trace.activities.slice(offset, offset + 200).map((activity) => {
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
  const source = agentDisplayLabel(activity);
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
      : NOT_REPORTED;
  if (evidence.kind !== "none" && !activity.contributesToTotal) return `Corroborating · ${label}`;
  if (evidence.kind !== "none") return label;
  return NOT_REPORTED;
};
const durationLabel = (activity: Activity) => {
  if (activity.signal !== "trace") return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(activity.observedAt));
  const start = new Date(activity.startedAt ?? activity.observedAt).getTime();
  const end = new Date(activity.endedAt ?? activity.observedAt).getTime();
  const milliseconds = Math.max(0, end - start);
  return milliseconds < 1_000 ? `${milliseconds} ms` : `${(milliseconds / 1_000).toFixed(milliseconds < 10_000 ? 2 : 1)} s`;
};
const activityEvidence = (activity: Activity, navigate?: (event: MouseEvent) => void, href = conversationHref(activity)) => {
  const facts = [
    ["Kind", activity.kind],
    ["Tool name", activity.toolName || NOT_APPLICABLE],
    ["Telemetry name", activity.name],
    ["Status", activity.status || NOT_REPORTED],
    ["Source", activity.source || NOT_REPORTED],
    ["Conversation", activity.runId || NOT_APPLICABLE],
    ["Agent", agentDisplayLabel(activity)],
    ["Runtime agent ID", activity.agentId || NOT_REPORTED],
    ["Agent type", activity.agentType || NOT_REPORTED],
    ["Parent agent", activity.parentAgentId || "Root"],
    ["Model", activity.model || NOT_REPORTED],
    ["Observed at", activity.observedAt],
    ["Started at", activity.startedAt || NOT_APPLICABLE],
    ["Ended at", activity.endedAt || NOT_APPLICABLE],
    ["Trace ID", activity.traceId || NOT_APPLICABLE],
    ["Linked trace", activity.relatedTraceId || NOT_APPLICABLE],
    ["Span ID", activity.spanId || NOT_APPLICABLE],
    ["Parent span", activity.parentSpanId || "Root"],
    ["Target agent", activity.targetAgentId || activity.targetAgentType
      ? [activity.targetAgentId, activity.targetAgentType].filter(Boolean).join(" · ")
      : NOT_APPLICABLE],
    ["Rollup", rollupLabel(activity)],
  ];
  return html`<div class="evidence"><dl>${facts.map(([label, value]) => html`<div><dt>${label}</dt><dd>${value}</dd></div>`)}<div><dt>Token breakdown</dt><dd><am-token-breakdown .usage=${activity.tokens}></am-token-breakdown></dd></div></dl>
    <pre class="message">${activity.content || NOT_APPLICABLE}</pre>
    ${href ? html`<a class="conversation" href=${href} @click=${navigate}>Open ${activity.spanId ? "span in" : ""} conversation</a>` : null}
  </div>`;
};

const conversationTarget = (activity: Activity): ConversationTarget | undefined => {
  if (!conversationHref(activity)) return undefined;
  return {
    sourceId: activity.source,
    conversationId: activity.runId,
    traceId: activity.traceId || activity.relatedTraceId,
    spanId: activity.spanId || activity.relatedSpanId,
  };
};

const operationName = (activity: Activity) => activity.toolName || activity.name;

const rollupLabel = (activity: Activity) => {
  const hasUsageEvidence = tokenEvidence(activity.tokens).kind !== "none" || activity.costUsd !== undefined;
  if (!hasUsageEvidence) return NOT_APPLICABLE;
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
