import { LitElement, css, html, type PropertyValues } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { Activity, ActivityDirection } from "../model/telemetry";
import { agentDisplayLabel } from "../model/agent-label";
import { NOT_APPLICABLE, NOT_REPORTED } from "../presentation/missing-data";
import "./token-breakdown";

@customElement("am-activity-table")
export class ActivityTable extends LitElement {
  @property({ attribute: false }) activities: readonly Activity[] = [];
  @property({ type: Boolean }) hasMore = false;
  @property({ type: Boolean }) hasEarlier = false;
  @property({ type: Boolean }) loading = false;
  @property() pageDirection: ActivityDirection | "" = "";
  @property() loadError = "";
  @property() highlightedSpanId = "";
  @property() highlightedTraceId = "";
  @property() pagingContext = "";

  private readonly pagingLatched: Record<ActivityDirection, boolean> = { newer: false, older: false };
  private pagingObserver?: IntersectionObserver;
  private lastScrollTop = 0;
  private revealedTarget = "";

  static styles = css`
    :host { display: block; max-width: 100%; overflow: visible; }
    .table-scroll { max-width: 100%; overflow-x: auto; scrollbar-color: var(--am-border-strong) var(--am-track); }
    table { width: 100%; border-collapse: collapse; min-width: 1160px; }
    thead { position: sticky; top: 0; z-index: 1; background: rgba(9, 14, 20, .96); backdrop-filter: blur(12px); }
    th { color: var(--am-muted); font: 0.68rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-transform: uppercase; letter-spacing: .08em; text-align: left; }
    th, td { padding: 9px 8px; border-bottom: 1px solid var(--am-border); vertical-align: top; }
    tbody tr { transition: background .16s ease; }
    tbody tr:hover td { background: rgba(255, 255, 255, .018); }
    tr[data-highlighted="true"] td { background: var(--am-accent-soft); box-shadow: inset 0 1px 0 rgba(var(--am-accent-rgb), .08), inset 0 -1px 0 rgba(var(--am-accent-rgb), .08); }
    th:nth-child(1) { width: 90px; }
    th:nth-child(2) { width: 180px; }
    th:nth-child(3) { width: 90px; }
    th:nth-child(4) { min-width: 280px; }
    th:nth-child(5) { width: 100px; }
    th:nth-child(6) { width: 150px; }
    th:nth-child(7) { width: 190px; }
    th:nth-child(8) { width: 120px; }
    td { color: var(--am-text); font-size: .78rem; }
    code { color: var(--am-accent); font: .7rem/1.3 "SFMono-Regular", "Cascadia Code", monospace; }
    .content { max-width: 380px; color: var(--am-muted); overflow-wrap: anywhere; }
    .content summary { cursor: pointer; white-space: pre-wrap; }
    .content pre { max-height: 18rem; overflow: auto; white-space: pre-wrap; font: inherit; }
    .kind { display: inline-block; border: 1px solid var(--am-border-strong); border-radius: 4px; padding: 2px 5px; background: var(--am-accent-soft); color: var(--am-accent); font: 700 .58rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .04em; text-transform: uppercase; }
    .tokens { white-space: nowrap; }
    .tokens small { display: block; color: var(--am-muted); white-space: normal; }
    .correlation { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 5px; }
    .correlation small { border: 1px solid var(--am-border); border-radius: 4px; padding: 2px 6px; background: var(--am-surface-strong); color: var(--am-muted); font: .64rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; }
    .rollup { margin-top: 5px; color: var(--am-muted); font-size: .64rem; white-space: normal; }
    .trace { border: 0; border-bottom: 1px solid currentColor; background: transparent; color: var(--am-accent); cursor: pointer; padding: 0; font: .72rem/1.4 "SFMono-Regular", "Cascadia Code", monospace; }
    .trace:hover, .trace:focus-visible { color: var(--am-text); }
    .loading { padding: 16px; color: var(--am-muted); text-align: center; font-size: .78rem; }
    .continuation { position: sticky; left: 0; display: flex; justify-content: center; align-items: center; gap: 8px; padding: 10px; color: var(--am-muted); }
    .continuation button { border: 1px solid var(--am-border); border-radius: 7px; background: var(--am-surface-raised); color: var(--am-text); padding: 8px 14px; cursor: pointer; }
    .continuation button:hover, .continuation button:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    .continuation button:disabled { cursor: wait; opacity: .55; }
  `;

  connectedCallback() {
    super.connectedCallback();
    this.addEventListener("scroll", this.onScroll);
  }

  disconnectedCallback() {
    this.removeEventListener("scroll", this.onScroll);
    this.pagingObserver?.disconnect();
    super.disconnectedCallback();
  }

  protected willUpdate(changed: PropertyValues<this>) {
    if (!changed.has("pagingContext")) return;
    this.pagingLatched.newer = false;
    this.pagingLatched.older = false;
    this.lastScrollTop = this.scrollTop;
    this.revealedTarget = "";
  }

  render() {
    return html`${this.continuation("newer")}<div class="table-scroll"><table>
      <thead><tr><th>Time</th><th>Agent</th><th>Type</th><th>Operation / message</th><th>Source</th><th>Model</th><th>Tokens</th><th>Trace</th></tr></thead>
      <tbody>${this.activities.map((activity) => {
        const highlighted = Boolean(this.highlightedTraceId && this.highlightedSpanId)
          && activity.signal === "trace"
          && activity.traceId === this.highlightedTraceId
          && activity.spanId === this.highlightedSpanId;
        return html`<tr data-highlighted=${String(highlighted)} aria-current=${highlighted ? "location" : "false"}>
        <td>${formatTime(activity.observedAt)}</td>
        <td><small>Agent</small><br><strong>${agentDisplayLabel(activity)}</strong><br><small>Runtime ID: <code>${activity.agentId || "main"}</code></small><br><small>Type: ${activity.agentType || NOT_REPORTED}</small></td>
        <td><span class="kind">${activity.kind}</span></td>
        <td><strong>${operationLabel(activity)}</strong>${contentView(activity)}${activity.targetAgentId || activity.targetAgentType ? html`<small>→ ${activity.targetAgentId || "subagent"}${activity.targetAgentType ? ` · ${activity.targetAgentType}` : ""}</small>` : null}${correlationView(activity)}</td>
        <td>${activity.source || NOT_REPORTED}</td>
        <td>${activity.model || NOT_APPLICABLE}</td>
        <td class="tokens">${tokenView(activity)}</td>
        <td>${this.traceView(activity)}</td>
      </tr>`;})}</tbody>
    </table></div>${this.continuation("older")}`;
  }

  protected updated(changed: PropertyValues<this>) {
    if (changed.has("activities") || changed.has("hasMore") || changed.has("hasEarlier") || changed.has("loading") || changed.has("pageDirection")) this.observePagingSentinel();
    if (!changed.has("highlightedTraceId") && !changed.has("highlightedSpanId") && !changed.has("activities")) return;
    const targetIdentity = this.highlightedTraceId && this.highlightedSpanId
      ? `${this.highlightedTraceId}:${this.highlightedSpanId}`
      : "";
    if (!targetIdentity) {
      this.revealedTarget = "";
      return;
    }
    if (targetIdentity === this.revealedTarget) return;
    const target = this.shadowRoot?.querySelector<HTMLElement>('tr[aria-current="location"]');
    if (!target) return;
    target.scrollIntoView?.({ block: "center", inline: "nearest" });
    this.revealedTarget = targetIdentity;
  }

  private observePagingSentinel() {
    this.pagingObserver?.disconnect();
    if (this.loading || typeof IntersectionObserver === "undefined") return;
    const direction: ActivityDirection = this.hasMore ? "older" : this.hasEarlier ? "newer" : "older";
    const sentinel = this.shadowRoot?.querySelector<HTMLElement>(`[data-paging="${direction}"]`);
    if (!sentinel) return;
    this.pagingObserver = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) this.requestMore(direction);
    }, { rootMargin: "0px 0px 600px" });
    this.pagingObserver.observe(sentinel);
  }

  private readonly onScroll = () => {
    const nearNewer = this.scrollTop < 400;
    const nearOlder = this.scrollHeight - this.scrollTop - this.clientHeight <= 400;
    const scrollMoved = Math.abs(this.scrollTop - this.lastScrollTop) > 1;
    this.lastScrollTop = this.scrollTop;
    if (scrollMoved && !nearNewer) this.pagingLatched.newer = false;
    if (scrollMoved && !nearOlder) this.pagingLatched.older = false;
    if (this.loading) return;
    if (this.hasEarlier && nearNewer && !this.pagingLatched.newer) {
      this.requestMore("newer");
      return;
    }
    if (this.hasMore && nearOlder && !this.pagingLatched.older) this.requestMore("older");
  };

  private requestMore(direction: ActivityDirection) {
    if (this.loading) return;
    this.pagingLatched[direction] = true;
    this.lastScrollTop = this.scrollTop;
    this.dispatchEvent(new CustomEvent("activities-needed", { detail: { direction }, bubbles: true, composed: true }));
  }

  private continuation(direction: ActivityDirection) {
    const available = direction === "newer" ? this.hasEarlier : this.hasMore;
    const current = this.pageDirection === direction;
    if (!available && !(current && (this.loading || this.loadError))) return null;
    if (current && this.loading) return html`<div class="loading" data-paging=${direction} role="status">Loading ${direction} observations…</div>`;
    if (current && this.loadError) return html`<div class="continuation" data-paging=${direction}><span role="alert">${this.loadError}</span><button type="button" data-direction=${direction} @click=${() => this.requestMore(direction)}>Retry loading</button></div>`;
    return html`<div class="continuation" data-paging=${direction} aria-hidden="true"></div>`;
  }

  private traceView(activity: Activity) {
    const traceId = activity.traceId || activity.relatedTraceId;
    return traceId
      ? html`<a class="trace" href=${`/traces/${encodeURIComponent(traceId)}`} aria-label=${`Open trace ${traceId}`}>${activity.traceId ? shortId(traceId) : html`Linked ${shortId(traceId)}`}</a>`
      : html`${NOT_APPLICABLE}`;
  }
}

const formatTime = (value: string) => new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(value));
const shortId = (value?: string) => value ? `${value.slice(0, 8)}…` : NOT_APPLICABLE;
const shortValue = (value: string) => value.length > 18 ? `${value.slice(0, 14)}…` : value;
export const operationLabel = (activity: Activity) => {
  if (activity.toolName) return activity.toolName;
  switch (activity.kind) {
    case "prompt": return "User prompt";
    case "response": return activity.tokens.total === null ? "Model response" : "Model call usage";
    case "tool": return "Tool operation";
    case "delegation": return "Agent delegation";
    case "message": return "Agent message";
    case "reasoning": return "Reasoning";
    default: return "Telemetry event";
  }
};
const contentFallback = (_activity: Activity) => NOT_APPLICABLE;
const contentView = (activity: Activity) => {
  const content = activity.content || contentFallback(activity);
  if (content.length <= 180) return html`<div class="content">${content}</div>`;
  return html`<details class="content"><summary>${content.slice(0, 180)}…</summary><pre>${content}</pre></details>`;
};
const tokenView = (activity: Activity) => {
  return html`<am-token-breakdown .usage=${activity.tokens} .compact=${true}></am-token-breakdown>
    ${activity.contributesToTotal ? null : html`<small class="rollup">Corroborating; excluded from total</small>`}`;
};
const correlationView = (activity: Activity) => {
  const values = [
    activity.promptId ? ["Prompt", activity.promptId] : null,
    activity.usageId ? ["Usage", activity.usageId] : null,
  ].filter((entry): entry is [string, string] => entry !== null);
  return values.length === 0 ? null : html`<div class="correlation">${values.map(([label, value]) => html`<small title=${value}>${label} ${shortValue(value)}</small>`)}</div>`;
};
declare global { interface HTMLElementTagNameMap { "am-activity-table": ActivityTable } }
