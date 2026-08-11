import { LitElement, css, html, type PropertyValues } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { Activity, ActivityDirection } from "../model/update";

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
  private lastScrollTop = 0;
  private revealedTarget = "";

  static styles = css`
    :host { display: block; max-width: 100%; max-height: 70vh; overflow: auto; overscroll-behavior: contain; }
    table { width: 100%; border-collapse: collapse; min-width: 1160px; }
    thead { position: sticky; top: 0; z-index: 1; background: var(--am-surface); }
    th { color: var(--am-muted); font: 0.68rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-transform: uppercase; letter-spacing: .08em; text-align: left; }
    th, td { padding: 11px 9px; border-bottom: 1px solid var(--am-border); vertical-align: top; }
    tr[data-highlighted="true"] td { background: var(--am-accent-soft); }
    th:nth-child(1) { width: 90px; }
    th:nth-child(2) { width: 180px; }
    th:nth-child(3) { width: 90px; }
    th:nth-child(4) { min-width: 280px; }
    th:nth-child(5) { width: 100px; }
    th:nth-child(6) { width: 150px; }
    th:nth-child(7) { width: 190px; }
    th:nth-child(8) { width: 120px; }
    td { color: var(--am-text); font-size: .84rem; }
    code { color: var(--am-accent); font-size: .72rem; }
    .content { max-width: 380px; color: var(--am-muted); overflow-wrap: anywhere; }
    .content summary { cursor: pointer; white-space: pre-wrap; }
    .content pre { max-height: 18rem; overflow: auto; white-space: pre-wrap; font: inherit; }
    .kind { display: inline-block; border: 1px solid var(--am-border); border-radius: 99px; padding: 3px 7px; font-size: .68rem; text-transform: uppercase; }
    .tokens { white-space: nowrap; }
    .tokens small { display: block; color: var(--am-muted); white-space: normal; }
    .trace { border: 0; border-bottom: 1px solid currentColor; background: transparent; color: var(--am-accent); cursor: pointer; padding: 0; font: .72rem/1.4 "SFMono-Regular", "Cascadia Code", monospace; }
    .trace:hover, .trace:focus-visible { color: var(--am-text); }
    .loading { padding: 16px; color: var(--am-muted); text-align: center; font-size: .78rem; }
    .continuation { position: sticky; left: 0; display: flex; justify-content: center; align-items: center; gap: 10px; padding: 14px; color: var(--am-muted); }
    .continuation button { border: 1px solid var(--am-border); border-radius: 99px; background: var(--am-surface); color: var(--am-text); padding: 8px 14px; cursor: pointer; }
    .continuation button:disabled { cursor: wait; opacity: .55; }
  `;

  connectedCallback() {
    super.connectedCallback();
    this.addEventListener("scroll", this.onScroll);
  }

  disconnectedCallback() {
    this.removeEventListener("scroll", this.onScroll);
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
    return html`${this.continuation("newer")}<table>
      <thead><tr><th>Time</th><th>Agent</th><th>Type</th><th>Operation / message</th><th>Source</th><th>Model</th><th>Tokens</th><th>Trace</th></tr></thead>
      <tbody>${this.activities.map((activity) => {
        const highlighted = Boolean(this.highlightedTraceId && this.highlightedSpanId)
          && activity.signal === "trace"
          && activity.traceId === this.highlightedTraceId
          && activity.spanId === this.highlightedSpanId;
        return html`<tr data-highlighted=${String(highlighted)} aria-current=${highlighted ? "location" : "false"}>
        <td>${formatTime(activity.observedAt)}</td>
        <td><small>Definition</small><br><strong>${activity.agentDefinition || "N/A"}</strong><br><small>Runtime ID: <code>${activity.agentId || "main"}</code></small><br><small>Type: ${activity.agentType || "N/A"}</small></td>
        <td><span class="kind">${activity.kind}</span></td>
        <td><strong>${operationLabel(activity)}</strong>${contentView(activity)}${activity.targetAgentId || activity.targetAgentType ? html`<small>→ ${activity.targetAgentId || "subagent"}${activity.targetAgentType ? ` · ${activity.targetAgentType}` : ""}</small>` : null}</td>
        <td>${activity.source || "unknown"}</td>
        <td>${activity.model || "N/A"}</td>
        <td class="tokens">${tokenView(activity)}</td>
        <td>${this.traceView(activity.traceId)}</td>
      </tr>`;})}</tbody>
    </table>${this.continuation("older")}`;
  }

  protected updated(changed: PropertyValues<this>) {
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
    if (current && this.loading) return html`<div class="loading" role="status">Loading ${direction} observations…</div>`;
    return html`<div class="continuation">${current && this.loadError ? html`<span role="alert">${this.loadError}</span>` : null}<button type="button" data-direction=${direction} ?disabled=${this.loading} @click=${() => this.requestMore(direction)}>${current && this.loadError ? "Retry loading" : `Load ${direction}`}</button></div>`;
  }

  private traceView(traceId?: string) {
    return traceId
      ? html`<a class="trace" href=${`/traces/${encodeURIComponent(traceId)}`} aria-label=${`Open trace ${traceId}`}>${shortId(traceId)}</a>`
      : html`N/A`;
  }
}

const formatTime = (value: string) => new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(value));
const shortId = (value?: string) => value ? `${value.slice(0, 8)}…` : "N/A";
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
const contentFallback = (activity: Activity) => activity.kind === "response"
  ? "N/A"
  : "N/A";
const contentView = (activity: Activity) => {
  const content = activity.content || contentFallback(activity);
  if (content.length <= 180) return html`<div class="content">${content}</div>`;
  return html`<details class="content"><summary>${content.slice(0, 180)}…</summary><pre>${content}</pre></details>`;
};
const tokenView = (activity: Activity) => {
  const parts = [
    ["in", activity.tokens.input],
    ["out", activity.tokens.output],
    ["cache read", activity.tokens.cacheRead],
    ["cache write", activity.tokens.cacheWrite],
    ["reasoning", activity.tokens.reasoning],
  ].filter((entry): entry is [string, number] => entry[1] !== null);
  if (parts.length === 0) return html`N/A<small>N/A</small>`;
  return html`${activity.tokens.total === null ? "Partial usage" : activity.tokens.total.toLocaleString()}
    <small>${parts.map(([label, value]) => `${label} ${value.toLocaleString()}`).join(" · ")}</small>
    ${activity.contributesToTotal ? null : html`<small>Excluded from rollup to prevent duplicate accounting</small>`}`;
};
declare global { interface HTMLElementTagNameMap { "am-activity-table": ActivityTable } }
