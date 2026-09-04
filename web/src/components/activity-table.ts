import { css, html, type PropertyValues } from "lit";
import { customElement, property } from "lit/decorators.js";
import { repeat } from "lit/directives/repeat.js";
import type { Activity, ActivityDirection } from "../model/telemetry";
import { agentDisplayLabel } from "../model/agent-label";
import { NOT_APPLICABLE, notReported } from "../presentation/missing-data";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";
import { contentAvailabilityLabel, readableActivityContent } from "./content-evidence";
import "./token-breakdown";

@customElement("am-activity-table")
export class ActivityTable extends LocalizedElement {
  @property({ attribute: false }) activities: readonly Activity[] = [];
  @property({ type: Boolean }) hasMore = false;
  @property({ type: Boolean }) hasEarlier = false;
  @property({ type: Boolean }) loading = false;
  @property() pageDirection: ActivityDirection | "" = "";
  @property() loadError = "";
  @property() highlightedSpanId = "";
  @property() highlightedTraceId = "";
  @property() pagingContext = "";
  @property() selectionContext = "";
  @property() selectedActivityId = "";
  @property({ attribute: false }) retainedSelectedActivity?: Activity;
  @property() selectedVisibility: "not_loaded" | "outside_agent_filter" = "not_loaded";
  @property() agentFilterId = "";
  @property({ attribute: false }) locationForTrace: (traceId: string, spanId?: string) => string =
    (traceId, spanId) => `/traces/${encodeURIComponent(traceId)}${spanId ? `?spanId=${encodeURIComponent(spanId)}` : ""}`;

  private readonly pagingLatched: Record<ActivityDirection, boolean> = { newer: false, older: false };
  private pagingObserver?: IntersectionObserver;
  private lastScrollTop = 0;
  private revealedTarget = "";
	private selectedTraceTarget = "";
  private notifyHighlightedSelection = false;
	private renderOffset = 0;
  private readingAnchor?: Readonly<{ activityId: string; top: number }>;
  private cachedSelectedActivity?: Activity;

  static styles = css`
    :host { display: block; max-width: 100%; overflow: visible; }
    .table-scroll { max-width: 100%; overflow-x: auto; scrollbar-color: var(--am-border-strong) var(--am-track); }
    .reading-layout { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.15fr); gap: 20px; align-items: start; }
    .activity-list, .activity-detail { min-width: 0; }
    table { width: 100%; border-collapse: collapse; min-width: 530px; }
    thead { position: sticky; top: 0; z-index: 1; background: rgba(9, 14, 20, .96); backdrop-filter: blur(12px); }
    th { color: var(--am-muted); font: 0.68rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-transform: uppercase; letter-spacing: .08em; text-align: left; }
    th, td { padding: 9px 8px; border-bottom: 1px solid var(--am-border); vertical-align: top; }
    tbody tr { transition: background .16s ease; }
    tbody tr:hover td { background: rgba(255, 255, 255, .018); }
    tr[data-highlighted="true"] td { background: var(--am-accent-soft); box-shadow: inset 0 1px 0 rgba(var(--am-accent-rgb), .08), inset 0 -1px 0 rgba(var(--am-accent-rgb), .08); }
    th:nth-child(1) { width: 90px; }
    th:nth-child(2) { min-width: 180px; }
    th:nth-child(3) { width: 130px; }
    th:nth-child(4) { width: 100px; }
    td { color: var(--am-text); font-size: .78rem; }
    code { color: var(--am-accent); font: .7rem/1.3 "SFMono-Regular", "Cascadia Code", monospace; }
    .preview { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; color: var(--am-muted); overflow-wrap: anywhere; margin-top: 4px; }
    .select-activity { border: 1px solid transparent; background: transparent; padding: 4px; text-align: left; color: var(--am-text); cursor: pointer; font: inherit; }
    .select-activity[aria-pressed="true"] { border-color: var(--am-accent); }
    .selected-label, .status { display: block; margin-top: 3px; color: var(--am-muted); font-size: .68rem; }
    .activity-detail { position: sticky; top: 16px; max-height: calc(100dvh - 32px); border: 1px solid var(--am-border); border-radius: 8px; padding: 16px; background: var(--am-surface-raised); overflow: auto; overflow-wrap: anywhere; overscroll-behavior: contain; scrollbar-gutter: stable; }
    .activity-detail h3 { margin: 0 0 12px; font-size: .95rem; }
    .activity-detail h4 { margin: 16px 0 8px; font-size: .78rem; }
    .activity-detail pre { margin: 0; white-space: pre-wrap; overflow-wrap: anywhere; font: .82rem/1.65 "SFMono-Regular", "Cascadia Code", monospace; }
    .activity-detail dl { display: grid; grid-template-columns: minmax(0, 7rem) minmax(0, 1fr); gap: 6px 12px; font-size: .74rem; }
    .activity-detail dt { color: var(--am-muted); }
    .activity-detail dd { margin: 0; min-width: 0; }
    .empty-detail { color: var(--am-muted); font-size: .8rem; line-height: 1.6; }
    .return-to-activity { border: 1px solid var(--am-border); border-radius: 5px; background: transparent; color: var(--am-text); cursor: pointer; padding: 7px 10px; margin-bottom: 12px; }
    button:focus-visible, a:focus-visible, .activity-detail:focus-visible { outline: 2px solid var(--am-accent); outline-offset: 3px; }
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
    @media (max-width: 1100px) {
      .reading-layout { grid-template-columns: minmax(0, 1fr); }
      .activity-detail { position: static; max-height: none; overflow: visible; scrollbar-gutter: auto; }
    }
    @media (prefers-reduced-motion: reduce) { tbody tr { transition: none; } }
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
	if (changed.has("pagingContext")) {
	  this.pagingLatched.newer = false;
	  this.pagingLatched.older = false;
	  this.lastScrollTop = this.scrollTop;
	  this.revealedTarget = "";
	  this.selectedTraceTarget = "";
	  this.renderOffset = 0;
	}
	if (changed.has("selectionContext") && changed.get("selectionContext") !== undefined) {
      this.cachedSelectedActivity = undefined;
      if (!changed.has("selectedActivityId")) this.selectedActivityId = "";
    }
	if (changed.has("activities")) {
      if (!changed.has("pagingContext")) this.captureReadingAnchor();
      else this.readingAnchor = undefined;
      const previous = changed.get("activities") as readonly Activity[] | undefined;
      const windowAnchor = previous?.[this.renderOffset];
      if (this.renderOffset > 0 && windowAnchor) {
        const nextOffset = this.activities.findIndex((activity) => activityIdentity(activity) === activityIdentity(windowAnchor));
        if (nextOffset >= 0) this.renderOffset = nextOffset;
      }
	  if (this.renderOffset >= this.activities.length) this.renderOffset = Math.max(0, this.activities.length - 200);
	}
    const previousActivities = changed.get("activities") as readonly Activity[] | undefined;
    const traceTarget = this.highlightedTraceId && this.highlightedSpanId ? `${this.highlightedTraceId}:${this.highlightedSpanId}` : "";
    if (!traceTarget) this.selectedTraceTarget = "";
    if (traceTarget && traceTarget !== this.selectedTraceTarget) {
      const target = this.activities.find((activity) => activity.signal === "trace" && activity.traceId === this.highlightedTraceId && activity.spanId === this.highlightedSpanId);
      if (target) {
        this.selectedTraceTarget = traceTarget;
        if (!changed.has("selectedActivityId") || !this.selectedActivityId) {
          this.selectedActivityId = activityIdentity(target);
          this.notifyHighlightedSelection = true;
          this.revealSelectedWindow();
        }
      }
    }
    const previousSelection = changed.has("selectionContext") ? undefined
      : previousActivities?.find((activity) => activityIdentity(activity) === this.selectedActivityId);
    const currentSelection = this.activities.find((activity) => activityIdentity(activity) === this.selectedActivityId);
    const retainedSelection = this.retainedSelectedActivity && activityIdentity(this.retainedSelectedActivity) === this.selectedActivityId
      ? this.retainedSelectedActivity : undefined;
    if (currentSelection || previousSelection || retainedSelection) this.cachedSelectedActivity = currentSelection ?? previousSelection ?? retainedSelection;
    else if (!this.selectedActivityId) this.cachedSelectedActivity = undefined;
    const selectionArrived = changed.has("activities") && this.selectedActivityId
      && !previousActivities?.some((activity) => activityIdentity(activity) === this.selectedActivityId);
    if (changed.has("selectedActivityId") || selectionArrived) this.revealSelectedWindow();
  }

  render() {
	const visibleActivities = this.activities.slice(this.renderOffset, this.renderOffset + 200);
    const loadedSelection = this.activities.find((activity) => activityIdentity(activity) === this.selectedActivityId);
    const retainedSelection = this.retainedSelectedActivity && activityIdentity(this.retainedSelectedActivity) === this.selectedActivityId
      ? this.retainedSelectedActivity : undefined;
    const cachedSelection = this.cachedSelectedActivity && activityIdentity(this.cachedSelectedActivity) === this.selectedActivityId
      ? this.cachedSelectedActivity : undefined;
    const selected = loadedSelection ?? retainedSelection ?? cachedSelection;
    const selectedVisibility = loadedSelection ? "loaded"
      : selected && this.agentFilterId && selected.agentId !== this.agentFilterId ? "outside_agent_filter"
        : this.selectedVisibility;
    return html`<div class="reading-layout"><div class="activity-list">${this.continuation("newer")}<div class="table-scroll"><table>
      <thead><tr><th>${localization.t("activity.time")}</th><th>${localization.t("activity.activity")}</th><th>${localization.t("activity.agent")}</th><th>${localization.t("activity.trace")}</th></tr></thead>
	  <tbody>${repeat(visibleActivities, activityIdentity, (activity, index) => {
        const highlighted = Boolean(this.highlightedTraceId && this.highlightedSpanId)
          && activity.signal === "trace"
          && activity.traceId === this.highlightedTraceId
          && activity.spanId === this.highlightedSpanId;
	    const isSelected = activityIdentity(activity) === this.selectedActivityId;
	    const content = readableActivityContent(activity.contentEvidence, activity.content);
	    return html`<tr data-activity-id=${activityIdentity(activity)} data-activity-index=${this.renderOffset + index} data-highlighted=${String(highlighted)} data-selected=${String(isSelected)} aria-current=${highlighted ? "location" : "false"}>
        <td>${formatTime(activity.observedAt)}</td>
        <td><button type="button" class="select-activity" aria-controls="activity-detail" aria-pressed=${String(isSelected)} @click=${() => this.selectActivity(activity)}><strong>${operationLabel(activity)}</strong><span class="preview">${content ? `${content.slice(0, 120)}${content.length > 120 ? "…" : ""}` : contentAvailabilityLabel(activity.contentEvidence, activity.content)}</span>${isSelected ? html`<span class="selected-label">${localization.t("activity.selected")}</span>` : null}</button><br><span class="kind">${activity.kind}</span>${activity.status ? html`<span class="status">${activityStatusLabel(activity.status)}</span>` : null}${correlationView(activity)}</td>
        <td><strong>${agentDisplayLabel(activity)}</strong><br><small>${localization.t("agentTree.runtimeId", { id: activity.agentId || "main" })}</small></td>
        <td>${this.traceView(activity)}</td>
      </tr>`;})}</tbody>
    </table></div>${this.continuation("older")}</div>${this.detailView(selected, selectedVisibility)}</div>`;
  }

  private async selectActivity(activity: Activity) {
    this.selectedActivityId = activityIdentity(activity);
    this.cachedSelectedActivity = activity;
    this.dispatchEvent(new CustomEvent("activity-selected", { detail: { activityId: this.selectedActivityId }, bubbles: true, composed: true }));
    await this.updateComplete;
    const detail = this.shadowRoot?.querySelector<HTMLElement>("#activity-detail");
    if (!detail) return;
    detail.scrollTop = 0;
    detail.focus({ preventScroll: false });
  }

  private revealSelectedWindow() {
    const index = this.activities.findIndex((activity) => activityIdentity(activity) === this.selectedActivityId);
    if (index >= 0 && (index < this.renderOffset || index >= this.renderOffset + 200)) this.renderOffset = Math.floor(index / 200) * 200;
  }

  private async returnToActivity() {
    this.revealSelectedWindow();
    this.requestUpdate();
    await this.updateComplete;
    const row = [...(this.shadowRoot?.querySelectorAll<HTMLElement>("tbody tr") ?? [])]
      .find(({ dataset }) => dataset.activityId === this.selectedActivityId);
    row?.querySelector<HTMLButtonElement>("button.select-activity")?.focus({ preventScroll: true });
    row?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }

  private detailView(activity?: Activity, visibility: "loaded" | "not_loaded" | "outside_agent_filter" = "loaded") {
    const content = activity ? readableActivityContent(activity.contentEvidence, activity.content) : "";
    return html`<section class="activity-detail" id="activity-detail" tabindex="-1" aria-labelledby="activity-detail-heading">
      <h3 id="activity-detail-heading">${activity ? operationLabel(activity) : localization.t("activity.detail")}</h3>
      ${visibility === "outside_agent_filter" ? html`<p class="empty-detail" role="status">${localization.t("activity.outsideFilter")}</p>` : visibility === "not_loaded" && activity ? html`<p class="empty-detail" role="status">${localization.t("activity.notLoadedRetained")}</p>` : null}
      ${activity ? html`${visibility === "loaded" ? html`<button type="button" class="return-to-activity" @click=${this.returnToActivity}>${localization.t("activity.back")}</button>` : null}<h4>${localization.t(activity.contentEvidence?.kind === "reference" ? "activity.receivedReference" : "activity.receivedBody")}</h4>${content ? html`<pre>${content}</pre>` : html`<p class="empty-detail">${localization.t(!activity.contentEvidence || activity.contentEvidence.availability === "not_reported" ? "activity.noBodyReported" : "activity.noReadableBody")}</p>`}
        <am-content-evidence .evidence=${activity.contentEvidence} .activityContent=${activity.content ?? ""}></am-content-evidence>
        <h4>${localization.t("activity.metadata")}</h4><dl>
          <dt>${localization.t("activity.activity")}</dt><dd>${activityIdentity(activity)}</dd>
          <dt>${localization.t("activity.source")}</dt><dd>${activity.source || notReported()}</dd>
          <dt>${localization.t("activity.conversation")}</dt><dd>${activity.runId || notReported()}</dd>
          <dt>${localization.t("activity.signal")}</dt><dd>${activity.signal}</dd>
          <dt>${localization.t("activity.event")}</dt><dd>${activity.name}</dd>
          <dt>${localization.t("activity.agent")}</dt><dd>${agentDisplayLabel(activity)} · ${activity.agentId || "main"}</dd>
          <dt>${localization.t("activity.agentType")}</dt><dd>${activity.agentType || notReported()}</dd>
          <dt>${localization.t("activity.model")}</dt><dd>${activity.model || NOT_APPLICABLE}</dd>
          <dt>${localization.t("activity.status")}</dt><dd>${activityStatusLabel(activity.status)}</dd>
          <dt>${localization.t("activity.observed")}</dt><dd>${activity.observedAt}</dd>
          <dt>${localization.t("activity.trace")}</dt><dd>${activity.traceId || activity.relatedTraceId || NOT_APPLICABLE}</dd>
          <dt>${localization.t("activity.span")}</dt><dd>${activity.spanId || activity.relatedSpanId || NOT_APPLICABLE}</dd>
          <dt>${localization.t("activity.tokens")}</dt><dd>${tokenView(activity)}</dd>
          ${activity.targetAgentId || activity.targetAgentType ? html`<dt>${localization.t("activity.targetAgent")}</dt><dd>${activity.targetAgentId || notReported()} · ${activity.targetAgentType || notReported()}</dd>` : null}
          ${activity.promptId ? html`<dt>${localization.t("activity.prompt")}</dt><dd>${activity.promptId}</dd>` : null}
          ${activity.usageId ? html`<dt>${localization.t("activity.usage")}</dt><dd>${activity.usageId}</dd>` : null}
        </dl>` : this.selectedActivityId
          ? html`<p class="empty-detail" role="status">${localization.t("activity.selectedUnavailable")}</p><code>${this.selectedActivityId}</code>`
          : html`<p class="empty-detail">${localization.t("activity.selectPrompt")}</p>`}
    </section>`;
  }

  protected updated(changed: PropertyValues<this>) {
    if (this.notifyHighlightedSelection) {
      this.notifyHighlightedSelection = false;
      this.dispatchEvent(new CustomEvent("activity-selected", { detail: { activityId: this.selectedActivityId }, bubbles: true, composed: true }));
    }
    if (changed.has("activities") || changed.has("hasMore") || changed.has("hasEarlier") || changed.has("loading") || changed.has("pageDirection")) this.observePagingSentinel();
    if (changed.has("activities")) this.restoreReadingAnchor();
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

  focusTraceEvidence(traceId: string, spanId: string): boolean {
    const index = this.activities.findIndex((activity) => activity.signal === "trace" && activity.traceId === traceId && activity.spanId === spanId);
    const link = this.shadowRoot?.querySelector<HTMLElement>(`tr[data-activity-index="${index}"] a.trace`);
    link?.focus({ preventScroll: true });
    return Boolean(link);
  }

  private captureReadingAnchor() {
    const rows = [...(this.shadowRoot?.querySelectorAll<HTMLElement>("tbody tr") ?? [])];
    const first = rows[0];
    const firstRect = first?.getBoundingClientRect();
    if (!first || !firstRect || firstRect.top >= 0) {
      this.readingAnchor = undefined;
      return;
    }
    for (const row of rows) {
      const rect = row.getBoundingClientRect();
      if (rect.bottom <= 0) continue;
      this.readingAnchor = row.dataset.activityId ? { activityId: row.dataset.activityId, top: rect.top } : undefined;
      return;
    }
    this.readingAnchor = undefined;
  }

  private restoreReadingAnchor() {
    const anchor = this.readingAnchor;
    this.readingAnchor = undefined;
    if (!anchor) return;
    const row = [...(this.shadowRoot?.querySelectorAll<HTMLElement>("tbody tr") ?? [])]
      .find(({ dataset }) => dataset.activityId === anchor.activityId);
    if (!row) return;
    const movement = row.getBoundingClientRect().top - anchor.top;
    if (Math.abs(movement) > 0.5) window.scrollBy({ top: movement, behavior: "instant" });
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
	if (direction === "newer" && this.renderOffset > 0) {
	  this.renderOffset = Math.max(0, this.renderOffset - 200);
	  this.requestUpdate();
	  return;
	}
	if (direction === "older" && this.renderOffset + 200 < this.activities.length) {
	  this.renderOffset = Math.min(this.activities.length - 1, this.renderOffset + 200);
	  this.requestUpdate();
	  return;
	}
    this.pagingLatched[direction] = true;
    this.lastScrollTop = this.scrollTop;
    this.dispatchEvent(new CustomEvent("activities-needed", { detail: { direction }, bubbles: true, composed: true }));
  }

  private continuation(direction: ActivityDirection) {
	const buffered = direction === "newer" ? this.renderOffset > 0 : this.renderOffset + 200 < this.activities.length;
	const available = buffered || (direction === "newer" ? this.hasEarlier : this.hasMore);
    const current = this.pageDirection === direction;
    if (!available && !(current && (this.loading || this.loadError))) return null;
    const localizedDirection = localization.t(direction === "newer" ? "activity.newer" : "activity.older");
    if (current && this.loading) return html`<div class="loading" data-paging=${direction} role="status">${localization.t("activity.loadingDirection", { direction: localizedDirection })}</div>`;
    if (current && this.loadError) return html`<div class="continuation" data-paging=${direction}><span role="alert">${this.loadError}</span><button type="button" data-direction=${direction} @click=${() => this.requestMore(direction)}>${localization.t("activity.retryLoading")}</button></div>`;
	if (buffered) return html`<div class="continuation" data-paging=${direction}><button type="button" @click=${() => this.requestMore(direction)}>${localization.t("activity.showDirection", { direction: localizedDirection })}</button></div>`;
	return html`<div class="continuation" data-paging=${direction} aria-hidden="true"></div>`;
  }

  private traceView(activity: Activity) {
    const traceId = activity.traceId || activity.relatedTraceId;
    return traceId
      ? html`<a class="trace" href=${this.locationForTrace(traceId, traceAnchorSpan(activity))} aria-label=${localization.t("activity.openTrace", { id: traceId })} @click=${(event: MouseEvent) => this.traceSelected(event, activity, traceId)}>${activity.traceId ? shortId(traceId) : localization.t("activity.linked", { id: shortId(traceId) })}</a>`
      : html`${NOT_APPLICABLE}`;
  }

  private traceSelected(event: MouseEvent, activity: Activity, traceId: string) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    this.dispatchEvent(new CustomEvent("trace-selected", {
      detail: {
        traceId,
        sourceId: activity.source,
        conversationId: activity.runId,
        spanId: traceAnchorSpan(activity),
      },
      bubbles: true,
      composed: true,
    }));
  }
}

const formatTime = (value: string) => localization.dateTime(new Date(value), { hour: "2-digit", minute: "2-digit", second: "2-digit" });
const shortId = (value?: string) => value ? `${value.slice(0, 8)}…` : NOT_APPLICABLE;
const shortValue = (value: string) => value.length > 18 ? `${value.slice(0, 14)}…` : value;
const activityStatusLabel = (value?: string) => value?.toLowerCase() === "error" ? localization.t("common.error") : value || notReported();
export const activityIdentity = (activity: Activity) => activity.id ?? JSON.stringify([activity.source, activity.runId, activity.signal, activity.traceId ?? "", activity.spanId ?? "", activity.observedAt, activity.name]);
export const operationLabel = (activity: Activity) => {
  if (activity.toolName) return activity.toolName;
  switch (activity.kind) {
    case "prompt": return localization.t("activity.userPrompt");
    case "response": return localization.t(activity.tokens.total === null ? "activity.modelResponse" : "activity.modelCallUsage");
    case "tool": return localization.t("activity.toolOperation");
    case "delegation": return localization.t("activity.agentDelegation");
    case "message": return localization.t("activity.agentMessage");
    case "reasoning": return localization.t("common.reasoning");
    default: return localization.t("activity.telemetryEvent");
  }
};
const tokenView = (activity: Activity) => {
  return html`<am-token-breakdown .usage=${activity.tokens} .compact=${true}></am-token-breakdown>
    ${activity.contributesToTotal ? null : html`<small class="rollup">${localization.t("activity.corroborating")}</small>`}`;
};
const correlationView = (activity: Activity) => {
  const values = [
    activity.promptId ? [localization.t("activity.prompt"), activity.promptId] : null,
    activity.usageId ? [localization.t("activity.usage"), activity.usageId] : null,
  ].filter((entry): entry is [string, string] => entry !== null);
  return values.length === 0 ? null : html`<div class="correlation">${values.map(([label, value]) => html`<small title=${value}>${label} ${shortValue(value)}</small>`)}</div>`;
};
declare global { interface HTMLElementTagNameMap { "am-activity-table": ActivityTable } }

// A native log correlation does not prove a retained native span exists.
const traceAnchorSpan = (activity: Activity) => activity.traceId
  ? activity.signal === "trace" ? activity.spanId : undefined
  : activity.relatedSpanId;
