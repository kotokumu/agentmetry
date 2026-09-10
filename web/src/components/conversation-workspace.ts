import { css, html, type PropertyValues } from "lit";
import { msg, str } from "@lit/localize";
import { customElement, property, state } from "lit/decorators.js";
import "./activity-table";
import { activityIdentity, type ActivityTable } from "./activity-table";
import type { ReworkSummary } from "./rework-summary";
import type { NavigationViewState } from "../app/navigation";
import type { SessionListView } from "../model/session-catalog";
import type { ReworkComparisonViewState } from "../model/rework-comparison";
import "./agent-tree";
import "./kpi-card";
import "./rework-comparison";
import type { ComparisonBaselineSelectedDetail } from "./rework-comparison";
import "./rework-summary";
import "./session-filter";
import "./investigation-filter";
import "./session-file-reads";
import { hasSessionConditions, type SessionConditions } from "../model/investigation-conditions";
import "./session-list";
import "./token-chart";
import { agentmetryClient } from "../api/agentmetry-client";
import { ConversationsController } from "../controllers/conversations-controller";
import { SessionComparisonController } from "../controllers/session-comparison-controller";
import { SessionListController } from "../controllers/session-list-controller";
import { agentDisplayLabel } from "../model/agent-label";
import type { ConversationTarget } from "../model/trace-analysis";
import type { ActivityDirection, Session, TelemetrySource, TimeRange } from "../model/telemetry";
import { notReported } from "../presentation/missing-data";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";
import { costCoverageHint, formatCostSummary } from "../presentation/cost";
import { featurePanelStyles } from "./feature-styles";
import { affectsSessionList, LIVE_UPDATE_EVENT, type LiveUpdateDelivery } from "../controllers/live-update-controller";
import { sectionLocation } from "../app/navigation";

export type ConversationSummaryDetail = Readonly<{
  status: "loading" | "ready" | "failed";
  conversationCount?: number;
  activityCount?: number;
}>;

const LIST_VIEWPORT_GAP = 12;
const MIN_LIST_VIEWPORT_HEIGHT = 240;

@customElement("am-conversation-workspace")
export class ConversationWorkspace extends LocalizedElement {
  @property() range: TimeRange = "24h";
  @property() sourceId = "";
  @property() search = "";
  @property() sessionView: SessionListView = "roots";
  @property({ attribute: false }) conditions: SessionConditions = {};
  @property() filterError = "";
  @property({ type: Boolean }) filterPending = false;
  private lastFiltersKey = "";
  @property({ attribute: false }) sources: readonly TelemetrySource[] = [];
  @property({ attribute: false }) requestedConversation?: ConversationTarget;
  @property() listHref = "/";
  @property() returnHref = "";
  @property() returnLabel = "";
  @property() requestedAgentId = "";
  @property() purpose: NonNullable<NavigationViewState["purpose"]> = "execution";
  @property() requestedActivityId = "";
  @property() requestedFileReadId = "";
  @state() private selectedActivityId = "";
  @state() private selectedFileReadId = "";
  @state() private activityContentLoadingId = "";
  @state() private copySessionIdStatus: "idle" | "copied" | "failed" = "idle";
  @state() private showComparison = false;
  @state() private listCollapsed = false;
  @property({ attribute: false }) requestedEvidenceFocus?: NavigationViewState["evidenceFocus"];
  private restoredEvidenceFocus = "";
  @property({ type: Boolean }) active = true;
  @property({ attribute: false }) locationForSession: (sourceId: string, sessionId: string) => string =
    (sourceId, sessionId) => `/conversations/${encodeURIComponent(sourceId)}/${encodeURIComponent(sessionId)}`;
  @property({ attribute: false }) locationForTrace: (traceId: string, spanId?: string) => string =
    (traceId, spanId) => `/traces/${encodeURIComponent(traceId)}${spanId ? `?spanId=${encodeURIComponent(spanId)}` : ""}`;
  private readonly conversations = new ConversationsController(
    this,
    agentmetryClient,
    () => this.investigationFilters,
    () => this.active,
    () => this.sessionView,
    () => this.purpose === "rework",
  );
  private readonly comparisonRoots = new SessionListController(
    this,
    agentmetryClient,
    () => ({ ...this.investigationFilters, conditions: this.conditions, view: "roots" }),
    () => this.active && this.sessionView === "all" && this.purpose === "rework" && this.showComparison,
  );
  private readonly comparison = new SessionComparisonController(
    this,
    {
      reader: agentmetryClient,
      current: () => this.conversations.selected,
      sessions: () => this.sessionView === "all" ? this.comparisonRoots.sessions : this.conversations.sessions,
      isActive: () => this.active && this.purpose === "rework" && this.showComparison,
    },
  );
  private lastSummaryKey = "";
	private restoredAgentKey = "";
	private lastReadyKey = "";
	private lastCanonicalKey = "";
  private listLayoutFrame?: number;

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener(LIVE_UPDATE_EVENT, this.liveUpdate as EventListener);
    window.addEventListener("resize", this.scheduleListViewportLayout);
    window.addEventListener("scroll", this.scheduleListViewportLayout, { passive: true });
  }

  disconnectedCallback() {
    window.removeEventListener(LIVE_UPDATE_EVENT, this.liveUpdate as EventListener);
    window.removeEventListener("resize", this.scheduleListViewportLayout);
    window.removeEventListener("scroll", this.scheduleListViewportLayout);
    if (this.listLayoutFrame !== undefined) cancelAnimationFrame(this.listLayoutFrame);
    this.listLayoutFrame = undefined;
    super.disconnectedCallback();
  }

  static styles = [featurePanelStyles, css`
    :host { display: block; }
    [hidden] { display: none !important; }
    .purpose-nav { grid-column: 1 / -1; display: flex; gap: 8px; flex-wrap: wrap; }
    .purpose-nav button { font: inherit; padding: 9px 15px; border: 1px solid var(--am-border); border-radius: 6px; background: var(--am-surface); color: var(--am-text); cursor: pointer; }
    .purpose-nav button[aria-pressed="true"] { border-color: var(--am-accent); background: var(--am-accent-soft); font-weight: 600; }
    .purpose-nav button:focus-visible { outline: 2px solid var(--am-accent); outline-offset: 3px; }
    .workspace { display: grid; grid-template-columns: 320px minmax(0, 1fr); gap: 12px; align-items: start; }
    .workspace.list-collapsed { grid-template-columns: minmax(0, 1fr); }
    .list-surface { display: flex; flex-direction: column; min-width: 0; }
    .list-heading { margin: 0; font-size: 16px; }
    .list-toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; gap: 8px; }
    .list-toggle { display: inline-grid; width: 32px; height: 32px; place-items: center; border: 1px solid var(--am-border); border-radius: 6px; padding: 0; background: var(--am-surface-raised); color: var(--am-muted); cursor: pointer; }
    .list-toggle:hover { border-color: var(--am-border-strong); background: var(--am-surface-strong); color: var(--am-text); }
    .list-toggle svg { width: 18px; height: 18px; fill: none; stroke: currentColor; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; }
    .list-toggle:focus-visible { outline: 2px solid var(--am-accent); outline-offset: 2px; }
    .list-collapsed .list-surface { display: none; }
    .list-surface { position: sticky; top: 12px; max-height: calc(100dvh - 24px); }
    .list-conditions { flex: 0 0 auto; margin-bottom: 12px; max-height: 40dvh; overflow: auto; }
    .list-conditions > summary { cursor: pointer; color: var(--am-muted); font-size: 13px; padding: 6px 0; }
    .restore-list { display: none; justify-self: start; grid-column: 1 / -1; }
    .list-collapsed .restore-list { display: inline-grid; }
    .list-surface > h2, .list-surface > am-session-filter, .list-surface > am-investigation-filter { flex: 0 0 auto; }
    .list-surface > am-session-list {
      flex: 1 1 auto;
      min-height: 0;
      overflow-y: auto;
      overscroll-behavior: contain;
      scrollbar-gutter: stable;
    }
    .detail { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 12px; min-width: 0; }
    .session-head-panel, .topology-panel, .operations-panel, .token-details, .detail > .empty, .detail > am-rework-summary, .detail > am-rework-comparison { grid-column: 1 / -1; }
    .traffic-panel, .topology-panel { padding-bottom: 12px; }
    .session-head-panel { padding-top: 12px; padding-bottom: 12px; }
    .context-return, .list-return { display: inline-flex; margin-bottom: 11px; color: var(--am-accent); font: 700 .75rem/1.3 "SFMono-Regular", "Cascadia Code", monospace; text-decoration: none; }
    .context-return:hover, .context-return:focus-visible, .list-return:hover, .list-return:focus-visible { color: var(--am-text); outline: 2px solid var(--am-accent-soft); outline-offset: 4px; }
    .list-return { display: none; }
    .session-head { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
    .session-head > div { min-width: 0; }
    .copy-session-id { flex: 0 0 auto; }
    .session-id { margin: 2px 0 0; font: .78rem/1.4 "SFMono-Regular", "Cascadia Code", monospace; overflow-wrap: anywhere; }
    .session-title { margin: 2px 0 0; font-size: 1.05rem; }
    .copy-session-id { border: 1px solid var(--am-border); border-radius: 6px; padding: 6px 8px; background: var(--am-surface-raised); color: var(--am-text); font: inherit; }
    .copy-session-id { cursor: pointer; }
    .session-partial { color: var(--am-muted); font-size: .78rem; }
    .session-metrics { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 8px; margin-top: 10px; }
    .session-overview, .token-details { margin-top: 10px; border-top: 1px solid var(--am-border); padding-top: 8px; }
    .session-overview summary, .token-details summary { color: var(--am-muted); cursor: pointer; font-size: 12px; }
    .session-overview .session-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); margin-top: 10px; }
    .token-details > .traffic-panel { margin-top: 10px; }
    .operations-panel { margin: 0; }
    .coverage-note { margin: 6px 0 0; color: var(--am-muted); font-size: .85rem; line-height: 1.4; overflow-wrap: anywhere; }
    .empty-settings { grid-column: 1 / -1; color: var(--am-muted); font-size: .85rem; line-height: 1.5; }
    .operations-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
    .operations-heading h2 { margin: 0; }
    .operation-actions { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 8px; }
    .return-to-file { border: 1px solid var(--am-border); border-radius: 7px; padding: 5px 9px; background: var(--am-surface-raised); color: var(--am-accent); cursor: pointer; font: 12px/1.3 inherit; }
    .agent-filter { display: flex; align-items: center; flex-wrap: wrap; justify-content: flex-end; gap: 7px; color: var(--am-muted); font-size: .75rem; }
    .agent-filter strong { max-width: 25ch; overflow: hidden; color: var(--am-text); font: 600 .75rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-overflow: ellipsis; white-space: nowrap; }
    .agent-filter button, .retry { border: 1px solid var(--am-border); border-radius: 7px; padding: 5px 9px; background: var(--am-surface-raised); color: var(--am-text); cursor: pointer; font: inherit; }
    .retry { margin-top: 10px; padding: 7px 11px; }
    .agent-filter button:hover, .agent-filter button:focus-visible, .retry:hover, .retry:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    @media (max-width: 950px) {
      .workspace { grid-template-columns: minmax(0, 1fr); }
      .workspace[data-view="detail"] .list-surface { display: none; }
      .workspace[data-view="list"] .detail { display: none; }
      .list-return { display: inline-flex; }
      .list-toggle, .list-collapsed .restore-list { display: none; }
      .context-return + .list-return { display: none; }
    }
    @media (max-width: 640px) { .session-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .detail { gap: 12px; } }
    @media (max-width: 480px) { .session-head { display: block; } }
  `];

  protected willUpdate(changed: PropertyValues<this>) {
    if (changed.has("requestedActivityId")) this.selectedActivityId = this.requestedActivityId;
    if (changed.has("requestedFileReadId")) this.selectedFileReadId = this.requestedFileReadId;
    const filtersKey = JSON.stringify(this.investigationFilters);
    if (filtersKey !== this.lastFiltersKey) { this.lastFiltersKey = filtersKey; this.conversations.filtersChanged(); }
	  if (changed.has("requestedConversation")) {
		this.selectedActivityId = this.requestedActivityId;
		this.restoredAgentKey = "";
		this.lastReadyKey = "";
		this.lastCanonicalKey = "";
      if (this.requestedConversation) this.conversations.select(this.requestedConversation);
      else {
        this.listCollapsed = false;
        this.conversations.clearRoute();
      }
      this.showComparison = false;
    }
  }

  private readonly liveUpdate = (event: CustomEvent<LiveUpdateDelivery>) => {
    event.detail.waitUntil(Promise.all([
      this.conversations.applyLiveUpdate(event.detail),
      this.refreshComparisonRoots(event.detail),
      this.comparison.applyLiveUpdate(event.detail),
    ]).then(() => {
      const removed = this.conversations.takeRemovedSession();
      if (removed) this.dispatchEvent(new CustomEvent("conversation-removed", { detail: removed, bubbles: true, composed: true }));
    }));
  };

  private async refreshComparisonRoots(delivery: LiveUpdateDelivery) {
    if (!this.active || this.purpose !== "rework" || !this.showComparison || this.sessionView !== "all" || (!delivery.resyncRequired && !affectsSessionList(delivery.targets, this.sourceId))) return;
    await this.comparisonRoots.refresh();
    if (this.comparisonRoots.failed) throw new Error("Session list unavailable");
  }

  render() {
    const sessions = this.conversations.sessions;
    const selected = this.conversations.selected;
    const detailRequested = Boolean(this.requestedConversation);
    const selectedAgentId = selected?.agents.some(({ agentId }) => agentId === this.conversations.selectedAgentId) ? this.conversations.selectedAgentId : "";
    const visibleActivities = selected
      ? selectedAgentId
        ? this.conversations.agentActivityPage?.sessionId === selected.id && this.conversations.agentActivityPage.agentId === selectedAgentId
          ? this.conversations.agentActivityPage.activities : []
        : selected.activities
      : [];
    const listPanel = html`<aside id="session-list-panel" class="panel list-surface" aria-label=${localization.t("app.sessions")}><div class="list-toolbar"><h2 class="list-heading" tabindex="-1">${localization.t("app.conversations")}</h2><button class="list-toggle" type="button" data-collapse-list ?hidden=${!detailRequested} aria-label=${localization.t("workspace.hideList")} title=${localization.t("workspace.hideList")} aria-expanded="true" aria-controls="session-list-panel" @click=${this.toggleList}>${sessionListToggleIcon(true)}</button></div><am-session-filter compact
        .sources=${this.sources.length ? this.sources : this.conversations.sources}
        .selectedSource=${this.sourceId}
        .search=${this.search}
      ></am-session-filter><details class="list-conditions"><summary>${localization.t("investigation.editConditions")}</summary><am-investigation-filter .filters=${this.investigationFilters} .pending=${this.filterPending || this.conversations.loadingList} .confirmed=${!this.conversations.loadingList && !this.conversations.listFailed} .error=${this.filterError || (this.conversations.listFailed ? String(this.conversations.listError ?? localization.t("workspace.queryUnavailable")) : "")}></am-investigation-filter></details><am-session-list compact
        .sessions=${sessions}
        .view=${this.sessionView}
        .hasMore=${this.conversations.list.hasMore}
        .loadingMore=${this.conversations.list.loadingMore}
        .pageFailed=${this.conversations.list.failed}
        @sessions-more-requested=${() => void this.conversations.list.loadMore()}
        @sessions-retry-requested=${() => this.conversations.list.sessions.length && this.conversations.list.hasMore ? void this.conversations.list.loadMore() : this.conversations.refreshList()}
        .loading=${this.conversations.loadingList}
        .unavailable=${this.conversations.listFailed}
        .filterActive=${Boolean(this.sourceId || this.search || hasSessionConditions(this.conditions))}
        .selected=${this.conversations.listSelection?.conversationId ?? ""}
        .selectedSource=${this.conversations.listSelection?.sourceId ?? ""}
        .locationForSession=${this.locationForSession}
      ></am-session-list>${this.renderInitialEmptyState()}</aside>`;
    return html`<section class="workspace ${this.listCollapsed ? "list-collapsed" : ""}" data-view=${detailRequested ? "detail" : "list"}>
      ${listPanel}
      <div class="detail">
        <button class="list-toggle restore-list" type="button" data-show-list aria-label=${localization.t("workspace.showList")} title=${localization.t("workspace.showList")} aria-expanded="false" aria-controls="session-list-panel" @click=${this.toggleList}>${sessionListToggleIcon(false)}</button>
        ${!selected && detailRequested ? html`<a class="list-return" href=${this.listHref} @click=${this.returnToList}>← ${localization.t("app.conversations")}</a>` : null}
        ${selected ? this.renderSelected(selected, selectedAgentId, visibleActivities) : this.renderConversationStatus()}
      </div>
    </section>`;
  }

	protected updated() {
    this.scheduleListViewportLayout();
    if (!this.active) this.restoredEvidenceFocus = "";
    else void this.restoreEvidenceFocus();
	  this.reportCanonicalConversation();
	  this.restoreRequestedAgent();
    this.reportViewReady();
    const status = this.conversations.listFailed ? "failed" : this.conversations.loadingList ? "loading" : "ready";
    const sessions = this.conversations.sessions;
    const detail = {
      status,
      conversationCount: status === "ready" ? sessions.length : undefined,
      activityCount: status === "ready" ? sessions.reduce((total, session) => total + session.activityCount, 0) : undefined,
    } as const;
    const key = `${detail.status}:${detail.conversationCount ?? ""}:${detail.activityCount ?? ""}`;
    if (key === this.lastSummaryKey) return;
    this.lastSummaryKey = key;
    this.dispatchEvent(new CustomEvent<ConversationSummaryDetail>("conversation-summary-changed", { detail, bubbles: true, composed: true }));
	}

  private readonly scheduleListViewportLayout = () => {
    if (this.listLayoutFrame !== undefined) return;
    this.listLayoutFrame = requestAnimationFrame(() => {
      this.listLayoutFrame = undefined;
      const panel = this.shadowRoot?.querySelector<HTMLElement>(".list-surface");
      if (!panel) return;
      const availableHeight = window.innerHeight - panel.getBoundingClientRect().top - LIST_VIEWPORT_GAP;
      panel.style.maxHeight = availableHeight >= MIN_LIST_VIEWPORT_HEIGHT ? `${Math.floor(availableHeight)}px` : "none";
    });
  };

  private readonly toggleList = async () => {
    this.listCollapsed = !this.listCollapsed;
    await this.updateComplete;
    this.shadowRoot?.querySelector<HTMLButtonElement>(this.listCollapsed ? "[data-show-list]" : "[data-collapse-list]")?.focus();
  };

	private reportCanonicalConversation() {
	  if (this.sessionView === "all") return;
	  const requested = this.requestedConversation;
	  const selected = this.conversations.selected;
	  if (!requested || !selected || requested.sourceId !== selected.sourceId || requested.conversationId === selected.id) return;
	  const canonical: ConversationTarget = { ...requested, sourceId: selected.sourceId, conversationId: selected.id };
	  const key = `${canonical.sourceId}:${canonical.conversationId}:${canonical.traceId ?? ""}:${canonical.spanId ?? ""}`;
	  if (this.lastCanonicalKey === key) return;
	  this.lastCanonicalKey = key;
	  this.dispatchEvent(new CustomEvent<ConversationTarget>("conversation-canonicalized", { detail: canonical, bubbles: true, composed: true }));
	}

  private renderSelected(selected: Session, selectedAgentId: string, activities: Session["activities"]) {
    const title = this.selectedCatalogEntry(selected)?.catalog?.name?.text;
    const qualifiedId = `${selected.sourceId}:${selected.id}`;
    return html`
      <section class="panel session-head-panel">${this.returnHref ? html`<a class="context-return" href=${this.returnHref} @click=${this.returnToOrigin}>← ${this.returnLabel}</a>` : null}<a class="list-return" href=${this.listHref} @click=${this.returnToList}>← ${localization.t("app.conversations")}</a><div class="session-head"><div><p class="eyebrow">${localization.t("workspace.selected")}</p>${title ? html`<h2 class="session-title" tabindex="-1">${title}</h2>` : null}<p class="session-id" tabindex="-1">${qualifiedId}</p></div><button class="copy-session-id" type="button" data-copy-session @click=${this.copySessionId}>${copyLabel(this.copySessionIdStatus)}</button></div>
      <div class="session-metrics" aria-label=${localization.t("workspace.usageAria")}>
        <am-kpi-card compact .label=${localization.t("workspace.totalTokens")} .value=${formatOptionalNumber(selected.tokens.total)} .hint=${localization.t("workspace.inputOutput")}></am-kpi-card>
        <am-kpi-card compact .label=${completionMsg("Elapsed time")} .value=${formatDuration(selected.startedAt, selected.endedAt)} .hint=${completionMsg("Reported session interval")}></am-kpi-card>
        <am-kpi-card compact .label=${completionMsg("Activities")} .value=${localization.number(selected.activityCount)} .hint=${completionMsg("Reported activity count")}></am-kpi-card>
        <am-kpi-card compact .label=${completionMsg("Agents")} .value=${localization.number(selected.agentCount ?? selected.agents.length)} .hint=${completionMsg("Reported agent count")}></am-kpi-card>
        <am-kpi-card compact .label=${localization.t("workspace.estimatedCost")} .value=${formatCostSummary(selected.costSummary)} .hint=${costCoverageHint(selected.costSummary)}></am-kpi-card>
      </div><details class="session-overview"><summary>${localization.t("workspace.sessionOverview")}</summary><div class="session-metrics" aria-label=${localization.t("workspace.usageAria")}>
        <am-kpi-card .label=${localization.t("workspace.inputTokens")} .value=${formatOptionalNumber(selected.tokens.input)} .hint=${localization.t("workspace.reportedByModel")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("workspace.outputTokens")} .value=${formatOptionalNumber(selected.tokens.output)} .hint=${localization.t("workspace.reportedByModel")}></am-kpi-card>
      </div></details>${this.conversations.rework ? html`<p class="coverage-note">${localization.t("workspace.coverage", { state: localization.t(this.conversations.rework.coverage.activityCoverage === "observed_projection_complete" ? "workspace.coverageComplete" : "workspace.coveragePartial") })}</p>` : null}</section>
      <nav class="purpose-nav" aria-label=${localization.t("workspace.investigationAria")}>${([ ["execution", "workspace.execution"], ["files", "workspace.fileReads"], ["rework", "workspace.rework"] ] as const).map(([purpose, label]) => html`<button type="button" data-purpose=${purpose} aria-pressed=${String(this.purpose === purpose)} @click=${() => this.selectPurpose(purpose)}>${localization.t(label)}</button>`)}</nav>
      <am-session-file-reads
        ?hidden=${this.purpose !== "files"}
        .sourceId=${selected.sourceId}
        .sessionId=${selected.id}
        .requestedReadId=${this.selectedFileReadId}
        .requestedActivityId=${this.selectedActivityId}
        .activityContent=${selected.activities.find((activity) => activityIdentity(activity) === this.selectedActivityId || activity.id === this.selectedActivityId)}
        .activityContentLoading=${Boolean(this.activityContentLoadingId && this.activityContentLoadingId === this.selectedActivityId)}
        .active=${this.active && this.purpose === "files"}
        @file-read-selected=${this.fileReadSelected}
        @file-read-open-activity=${this.fileReadOpenActivity}
        @file-read-neighbor-requested=${this.fileReadNeighborRequested}
      ></am-session-file-reads>
      <am-rework-summary
        ?hidden=${this.purpose !== "rework"}
        .analysis=${this.conversations.rework}
        .locationForTrace=${this.locationForTrace}
        .legacySessionTotalTokens=${selected.tokens.total}
        .loading=${this.conversations.loadingRework}
        .error=${this.conversations.reworkFailed ? String(this.conversations.reworkError ?? localization.t("workspace.reworkUnavailable")) : ""}
        @rework-retry-requested=${this.retryRework}
        @comparison-requested=${this.comparisonRequested}
      ></am-rework-summary>
      <am-rework-comparison
        ?hidden=${this.purpose !== "rework" || !this.showComparison}
        .state=${this.comparisonViewState()}
        @comparison-baseline-selected=${this.comparisonBaselineSelected}
        @comparison-retry-requested=${this.retryComparison}
      ></am-rework-comparison>
      <section class="panel topology-panel" ?hidden=${this.purpose !== "execution"}><h2>${localization.t("workspace.agentTopology")}</h2><am-agent-tree .agents=${selected.agents} .selectedAgentId=${selectedAgentId} @agent-selected=${this.agentSelected}></am-agent-tree></section>
      ${this.renderOperations(selected, selectedAgentId, activities)}
      <details class="token-details" ?hidden=${this.purpose !== "execution"}><summary>${localization.t("workspace.modelTraffic")}</summary><div class="panel traffic-panel"><am-token-chart .usage=${selected.tokens}></am-token-chart></div></details>
    `;
  }

  private renderInitialEmptyState() {
    if (this.conversations.loadingList || this.conversations.listFailed || this.conversations.list.sessions.length > 0) return null;
    const filterActive = Boolean(this.sourceId || this.search || hasSessionConditions(this.conditions));
    return html`<p class="empty-settings" data-empty-state>${completionMsg(filterActive ? "No matching sessions" : "No sessions yet")}
      <a data-connections-link href=${sectionLocation(this.investigationFilters, "connections")}>${completionMsg("Open Connections settings")}</a></p>`;
  }

  private renderConversationStatus() {
    if (!this.requestedConversation) return html`<section class="panel empty" role="status">${completionMsg("Select a session")}</section>`;
    if (!this.conversations.loadingConversation && !this.conversations.conversationFailed) return null;
    if (this.conversations.loadingConversation) return html`<p class="empty-settings" role="status">${localization.t("workspace.loadingConversationTitle")} — ${localization.t("workspace.loadingConversationBody")}</p>`;
    return html`<p class="empty-settings" role="alert">${localization.t("workspace.conversationUnavailable")} <button class="retry" type="button" @click=${this.retryConversation}>${localization.t("workspace.retry")}</button></p>`;
  }

  private selectedCatalogEntry(selected: Session) {
    return this.conversations.list.sessions.find((candidate) => candidate.sourceId === selected.sourceId && candidate.id === selected.id);
  }

  private copySessionId = async () => {
    try {
      await navigator.clipboard.writeText(`${this.conversations.selected?.sourceId ?? ""}:${this.conversations.selected?.id ?? ""}`);
      this.copySessionIdStatus = "copied";
    } catch {
      this.copySessionIdStatus = "failed";
    }
  };

  private renderOperations(selected: Session, selectedAgentId: string, activities: Session["activities"]) {
    const activityPage = this.conversations.activityPage;
    const selectedAgent = selected.agents.find(({ agentId }) => agentId === selectedAgentId);
    const agentPage = this.conversations.agentActivityPage?.sessionId === selected.id && this.conversations.agentActivityPage.agentId === selectedAgentId ? this.conversations.agentActivityPage : undefined;
    const retainedSelectedActivity = selected.activities.find((activity) => activityIdentity(activity) === this.selectedActivityId);
    const selectedVisibility = selectedAgentId && retainedSelectedActivity && retainedSelectedActivity.agentId !== selectedAgentId
      ? "outside_agent_filter" : "not_loaded";
    return html`<section class="panel operations-panel" ?hidden=${this.purpose !== "execution"}><div class="operations-heading"><h2>${localization.t("workspace.operations")}</h2><div class="operation-actions">${this.selectedFileReadId ? html`<button type="button" class="return-to-file" @click=${this.returnToFileRead}>${localization.t("workspace.returnToFileRead")}</button>` : null}${selectedAgent ? html`<div class="agent-filter"><span>${localization.t("workspace.filteredBy")}</span><strong>${agentDisplayLabel(selectedAgent)}</strong><button type="button" @click=${this.clearAgentSelection}>${localization.t("workspace.allAgents")}</button></div>` : null}</div></div><am-activity-table
      .selectedActivityId=${this.selectedActivityId}
      .retainedSelectedActivity=${retainedSelectedActivity}
      .selectedVisibility=${selectedVisibility}
      .agentFilterId=${selectedAgentId}
      @activity-selected=${this.activitySelected}
      @activity-files-requested=${this.activityFilesRequested}
      .activities=${activities}
      .hasEarlier=${selectedAgentId ? agentPage?.hasEarlier ?? false : selected.hasEarlier ?? false}
      .hasMore=${selectedAgentId ? agentPage?.hasMore ?? false : selected.hasMore ?? selected.activities.length < selected.activityCount}
      .loading=${selectedAgentId ? agentPage?.loading ?? true : activityPage?.loading ?? false}
      .pageDirection=${selectedAgentId ? (agentPage?.loading ? "older" : "") : activityPage?.direction ?? ""}
      .loadError=${selectedAgentId ? agentPage?.error ?? "" : activityPage?.error ?? ""}
      .highlightedSpanId=${this.conversations.highlightedSpanId}
      .highlightedTraceId=${this.conversations.highlightedTraceId}
      .locationForTrace=${this.locationForTrace}
      .pagingContext=${`${selected.sourceId}:${selected.id}:${selectedAgentId}`}
      .selectionContext=${`${selected.sourceId}:${selected.id}`}
      @activities-needed=${this.activitiesNeeded}
    ></am-activity-table></section>`;
  }

  private agentSelected(event: CustomEvent<{ agentId: string }>) {
    this.conversations.selectAgent(event.detail.agentId);
    this.reportViewStateChanged();
  }
  private readonly clearAgentSelection = () => {
    this.conversations.selectAgent("");
    this.reportViewStateChanged();
  };
  private readonly retryConversation = () => this.conversations.refreshSelected();
  private readonly retryRework = () => this.conversations.refreshRework();
  private readonly comparisonBaselineSelected = (event: CustomEvent<ComparisonBaselineSelectedDetail>) => this.comparison.selectBaseline(event.detail.sessionId);
  private comparisonViewState(): ReworkComparisonViewState {
    if (this.sessionView === "all") {
      const context = { options: [], selectedBaselineId: "" };
      if (this.comparisonRoots.loading) return { ...context, status: "loading" };
      if (this.comparisonRoots.failed) return { ...context, status: "failed", message: localization.t("workspace.listUnavailable") };
    }
    return this.comparison.viewState();
  }

  private readonly retryComparison = async () => {
    if (this.sessionView === "all" && this.comparisonRoots.failed) await this.comparisonRoots.refresh();
    if (!this.comparisonRoots.failed) await this.comparison.refresh();
  };
  private readonly returnToOrigin = (event: MouseEvent) => this.requestReturn(event, "origin");
  private readonly returnToList = (event: MouseEvent) => this.requestReturn(event, "list");
  private requestReturn(event: MouseEvent, to: "origin" | "list") {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    this.dispatchEvent(new CustomEvent("conversation-return-requested", { detail: { to }, bubbles: true, composed: true }));
  }

  private get investigationFilters() { return { range: this.range, sourceId: this.sourceId, search: this.search, ...this.conditions }; }

  get navigationViewState() {
    return { selectedAgentId: this.conversations.selectedAgentId, selectedActivityId: this.selectedActivityId, selectedFileReadId: this.selectedFileReadId, purpose: this.purpose, evidenceFocus: this.requestedEvidenceFocus } as const;
  }

  private selectPurpose(purpose: NonNullable<NavigationViewState["purpose"]>) {
    if (purpose === this.purpose) return;
    this.dispatchEvent(new CustomEvent("conversation-purpose-selected", { detail: { purpose }, bubbles: true, composed: true }));
  }

  private activitySelected(event: CustomEvent<{ activityId: string }>) {
    this.selectedActivityId = event.detail.activityId;
    this.selectedFileReadId = "";
    this.activityContentLoadingId = "";
    this.reportViewStateChanged();
  }

  private readonly activityFilesRequested = (event: CustomEvent<{ activityId: string }>) => {
    this.selectedActivityId = event.detail.activityId;
    this.selectedFileReadId = "";
    this.activityContentLoadingId = "";
    if (this.purpose !== "files") this.dispatchEvent(new CustomEvent("conversation-purpose-selected", { detail: { purpose: "files" }, bubbles: true, composed: true }));
    this.reportViewStateChanged();
  };

  private readonly fileReadSelected = (event: CustomEvent<{ activityId: string; readId: string }>) => {
    this.selectedActivityId = event.detail.activityId;
    this.selectedFileReadId = event.detail.readId;
    this.activityContentLoadingId = event.detail.activityId;
    void this.conversations.revealActivity(event.detail.activityId).finally(() => {
      if (this.activityContentLoadingId === event.detail.activityId) this.activityContentLoadingId = "";
    });
    this.reportViewStateChanged();
  };

  private readonly fileReadOpenActivity = (event: CustomEvent<{ activityId: string; readId: string }>) => {
    this.selectedActivityId = event.detail.activityId;
    this.selectedFileReadId = event.detail.readId;
    this.activityContentLoadingId = event.detail.activityId;
    this.dispatchEvent(new CustomEvent("conversation-purpose-selected", { detail: { purpose: "execution" }, bubbles: true, composed: true }));
    void this.conversations.revealActivity(event.detail.activityId).finally(() => {
      if (this.activityContentLoadingId === event.detail.activityId) this.activityContentLoadingId = "";
    });
    this.reportViewStateChanged();
  };

  private readonly fileReadNeighborRequested = (event: CustomEvent<{ activityId: string; readId: string }>) => {
    this.selectedActivityId = event.detail.activityId;
    this.selectedFileReadId = event.detail.readId;
    this.reportViewStateChanged();
  };

  private readonly comparisonRequested = () => {
    this.showComparison = true;
    this.reportViewStateChanged();
  };

  private readonly returnToFileRead = () => {
    this.dispatchEvent(new CustomEvent("conversation-purpose-selected", { detail: { purpose: "files" }, bubbles: true, composed: true }));
    this.reportViewStateChanged();
  };

  focusRouteHeading(view: "detail" | "list") {
	  if (view === "detail" && this.requestedConversation) {
		const selected = this.conversations.selected;
		if (!selected || selected.sourceId !== this.requestedConversation.sourceId) return false;
    }
    const selector = view === "detail" ? ".session-id, .detail h2" : ".list-heading";
    const heading = this.shadowRoot?.querySelector<HTMLElement>(selector);
    heading?.focus({ preventScroll: true });
    return Boolean(heading);
  }

  private async restoreEvidenceFocus() {
    const target = this.requestedEvidenceFocus;
    if (!target || !this.active) return;
    const key = `${target.kind}:${target.traceId}:${target.spanId}`;
    if (key === this.restoredEvidenceFocus) return;
    const focused = target.kind === "episode"
      ? await this.shadowRoot?.querySelector<ReworkSummary>("am-rework-summary")?.focusEvidence(target.traceId, target.spanId)
      : this.shadowRoot?.querySelector<ActivityTable>("am-activity-table")?.focusTraceEvidence(target.traceId, target.spanId);
    if (focused) this.restoredEvidenceFocus = key;
  }

  private restoreRequestedAgent() {
    const selected = this.conversations.selected;
    if (!selected || !this.requestedAgentId || !selected.agents.some(({ agentId }) => agentId === this.requestedAgentId)) return;
    const key = `${selected.sourceId}:${selected.id}:${this.requestedAgentId}`;
    if (this.restoredAgentKey === key) return;
    this.restoredAgentKey = key;
    this.conversations.selectAgent(this.requestedAgentId);
  }

  private reportViewReady() {
    const selected = this.conversations.selected;
    if (!selected) return;
    const key = `${selected.sourceId}:${selected.id}:${this.conversations.highlightedTraceId}:${this.conversations.highlightedSpanId}`;
    if (this.lastReadyKey === key) return;
    this.lastReadyKey = key;
    this.dispatchEvent(new CustomEvent("conversation-view-ready", { bubbles: true, composed: true }));
  }

  private reportViewStateChanged() {
    this.dispatchEvent(new CustomEvent("conversation-view-state-changed", { bubbles: true, composed: true }));
  }
  private activitiesNeeded(event: CustomEvent<{ direction: ActivityDirection }>) {
    if (this.conversations.selectedAgentId) void this.conversations.loadAgentActivities(event.detail.direction);
    else void this.conversations.loadActivities(event.detail.direction);
  }
}

const formatOptionalNumber = (value?: number | null) => value === undefined || value === null ? notReported() : localization.number(value);
const sessionListToggleIcon = (expanded: boolean) => html`<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
  <rect x="3" y="3" width="18" height="18" rx="2"></rect>
  <path d="M9 3v18"></path>
  <path d=${expanded ? "m16 15-3-3 3-3" : "m14 9 3 3-3 3"}></path>
</svg>`;
const completionMsg = (text: string) => {
  switch (text) {
    case "Elapsed time": return msg("Elapsed time", { id: "workspaceCompletion.elapsedTime" });
    case "Reported session interval": return msg("Reported session interval", { id: "workspaceCompletion.reportedSessionInterval" });
    case "Activities": return msg("Activities", { id: "workspaceCompletion.activities" });
    case "Reported activity count": return msg("Reported activity count", { id: "workspaceCompletion.reportedActivityCount" });
    case "Agents": return msg("Agents", { id: "workspaceCompletion.agents" });
    case "Reported agent count": return msg("Reported agent count", { id: "workspaceCompletion.reportedAgentCount" });
    case "No matching sessions": return msg("No matching sessions", { id: "workspaceCompletion.noMatchingSessions" });
    case "No sessions yet": return msg("No sessions yet", { id: "workspaceCompletion.noSessionsYet" });
    case "Open Connections settings": return msg("Open Connections settings", { id: "workspaceCompletion.openConnections" });
    case "Select a session": return msg("Select a session to inspect its operations and cost.", { id: "workspaceCompletion.selectSession" });
    case "Copied": return msg("Copied", { id: "workspaceCompletion.copied" });
    case "Copy failed": return msg("Copy failed", { id: "workspaceCompletion.copyFailed" });
    default: return msg("Copy full id", { id: "workspaceCompletion.copyFullId" });
  }
};
const copyLabel = (status: "idle" | "copied" | "failed") => completionMsg(status === "copied" ? "Copied" : status === "failed" ? "Copy failed" : "Copy full id");
const formatDuration = (startedAt: string, endedAt: string) => {
  const durationMs = Date.parse(endedAt) - Date.parse(startedAt);
  if (!Number.isFinite(durationMs) || durationMs < 0) return notReported();
  const seconds = Math.round(durationMs / 1000);
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  if (minutes === 0) return msg(str`${remainder} s`, { id: "workspaceCompletion.durationSeconds" });
  return msg(str`${minutes} min ${remainder} s`, { id: "workspaceCompletion.durationMinutesSeconds" });
};

declare global { interface HTMLElementTagNameMap { "am-conversation-workspace": ConversationWorkspace } }
