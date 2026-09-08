import { css, html, type PropertyValues } from "lit";
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
import { featurePanelStyles } from "./feature-styles";
import { affectsSessionList, LIVE_UPDATE_EVENT, type LiveUpdateDelivery } from "../controllers/live-update-controller";

export type ConversationSummaryDetail = Readonly<{
  status: "loading" | "ready" | "failed";
  conversationCount?: number;
  activityCount?: number;
}>;

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
  @state() private showComparison = false;
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
  );
  private readonly comparisonRoots = new SessionListController(
    this,
    agentmetryClient,
    () => ({ ...this.investigationFilters, conditions: this.conditions, view: "roots" }),
    () => this.active && this.sessionView === "all",
  );
  private readonly comparison = new SessionComparisonController(
    this,
    {
      reader: agentmetryClient,
      current: () => this.conversations.selected,
      sessions: () => this.sessionView === "all" ? this.comparisonRoots.sessions : this.conversations.sessions,
      isActive: () => this.active,
    },
  );
  private lastSummaryKey = "";
	private restoredAgentKey = "";
	private lastReadyKey = "";
	private lastCanonicalKey = "";

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener(LIVE_UPDATE_EVENT, this.liveUpdate as EventListener);
  }

  disconnectedCallback() {
    window.removeEventListener(LIVE_UPDATE_EVENT, this.liveUpdate as EventListener);
    super.disconnectedCallback();
  }

  static styles = [featurePanelStyles, css`
    :host { display: block; }
    [hidden] { display: none !important; }
    .purpose-nav { grid-column: 1 / -1; display: flex; gap: 8px; flex-wrap: wrap; }
    .purpose-nav button { font: inherit; padding: 9px 15px; border: 1px solid var(--am-border); border-radius: 6px; background: var(--am-surface); color: var(--am-text); cursor: pointer; }
    .purpose-nav button[aria-pressed="true"] { border-color: var(--am-accent); background: var(--am-accent-soft); font-weight: 600; }
    .purpose-nav button:focus-visible { outline: 2px solid var(--am-accent); outline-offset: 3px; }
    .workspace { display: grid; grid-template-columns: minmax(0, 1fr); gap: 12px; align-items: start; }
    .workspace.list-only { grid-template-columns: minmax(0, 1fr); }
    .list-surface { display: flex; flex-direction: column; min-width: 0; }
    .list-surface > h2, .list-surface > am-session-filter, .list-surface > am-investigation-filter { flex: 0 0 auto; }
    .list-surface > am-session-list {
      flex: 1 1 auto;
      min-height: 0;
      overflow-y: auto;
      overscroll-behavior: contain;
      scrollbar-gutter: stable;
    }
    .detail { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 12px; min-width: 0; }
    .session-head-panel, .operations-panel, .detail > .empty, .detail > am-rework-summary, .detail > am-rework-comparison { grid-column: 1 / -1; }
    .traffic-panel, .topology-panel { padding-bottom: 12px; }
    .session-head-panel { padding-top: 12px; padding-bottom: 12px; }
    .context-return, .list-return { display: inline-flex; margin-bottom: 11px; color: var(--am-accent); font: 700 .72rem/1.3 "SFMono-Regular", "Cascadia Code", monospace; text-decoration: none; }
    .context-return:hover, .context-return:focus-visible, .list-return:hover, .list-return:focus-visible { color: var(--am-text); outline: 2px solid var(--am-accent-soft); outline-offset: 4px; }
    .list-return { display: inline-flex; }
    .session-head { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
    .session-id { margin: 2px 0 0; font: .78rem/1.4 "SFMono-Regular", "Cascadia Code", monospace; overflow-wrap: anywhere; }
    .session-metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; margin-top: 10px; }
    .session-overview, .execution-context { margin-top: 10px; border-top: 1px solid var(--am-border); padding-top: 8px; }
    .session-overview summary, .execution-context summary { color: var(--am-muted); cursor: pointer; font-size: 12px; }
    .session-overview .session-metrics { margin-top: 10px; }
    .context-grid { display: grid; grid-template-columns: minmax(0, 5fr) minmax(0, 7fr); gap: 12px; margin-top: 10px; }
    .operations-panel { margin: 0; }
    .coverage-note { color: var(--am-muted); font-size: .85rem; line-height: 1.5; overflow-wrap: anywhere; }
    .operations-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
    .operations-heading h2 { margin: 0; }
    .operation-actions { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 8px; }
    .return-to-file { border: 1px solid var(--am-border); border-radius: 7px; padding: 5px 9px; background: var(--am-surface-raised); color: var(--am-accent); cursor: pointer; font: 12px/1.3 inherit; }
    .agent-filter { display: flex; align-items: center; flex-wrap: wrap; justify-content: flex-end; gap: 7px; color: var(--am-muted); font-size: .72rem; }
    .agent-filter strong { max-width: 25ch; overflow: hidden; color: var(--am-text); font: 600 .7rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-overflow: ellipsis; white-space: nowrap; }
    .agent-filter button, .retry { border: 1px solid var(--am-border); border-radius: 7px; padding: 5px 9px; background: var(--am-surface-raised); color: var(--am-text); cursor: pointer; font: inherit; }
    .retry { margin-top: 10px; padding: 7px 11px; }
    .agent-filter button:hover, .agent-filter button:focus-visible, .retry:hover, .retry:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    @media (max-width: 950px) {
      .context-return + .list-return { display: none; }
    }
    @media (max-width: 640px) { .session-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .detail { gap: 12px; } .context-grid { grid-template-columns: 1fr; } }
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
      else this.conversations.clearRoute();
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
    if (!this.active || this.sessionView !== "all" || (!delivery.resyncRequired && !affectsSessionList(delivery.targets, this.sourceId))) return;
    await this.comparisonRoots.refresh();
    if (this.comparisonRoots.failed) throw new Error("Session list unavailable");
  }

  render() {
    const sessions = this.conversations.sessions;
    const selected = this.conversations.selected;
    const selectedAgentId = selected?.agents.some(({ agentId }) => agentId === this.conversations.selectedAgentId) ? this.conversations.selectedAgentId : "";
    const visibleActivities = selected
      ? selectedAgentId
        ? this.conversations.agentActivityPage?.sessionId === selected.id && this.conversations.agentActivityPage.agentId === selectedAgentId
          ? this.conversations.agentActivityPage.activities : []
        : selected.activities
      : [];
    const listPanel = html`<div class="panel list-surface"><h2 class="list-heading" tabindex="-1">${localization.t("app.conversations")}</h2><am-session-filter
        .sources=${this.sources.length ? this.sources : this.conversations.sources}
        .selectedSource=${this.sourceId}
        .search=${this.search}
      ></am-session-filter><am-investigation-filter .filters=${this.investigationFilters} .pending=${this.filterPending || this.conversations.loadingList} .confirmed=${!this.conversations.loadingList && !this.conversations.listFailed} .error=${this.filterError || (this.conversations.listFailed ? String(this.conversations.listError ?? localization.t("workspace.queryUnavailable")) : "")}></am-investigation-filter><am-session-list
        .sessions=${sessions}
        .view=${this.sessionView}
        .hasMore=${this.conversations.list.hasMore}
        .loadingMore=${this.conversations.list.loadingMore}
        .pageFailed=${this.conversations.list.failed}
        @sessions-more-requested=${() => void this.conversations.list.loadMore()}
        @sessions-retry-requested=${() => this.conversations.refreshList()}
        .loading=${this.conversations.loadingList}
        .unavailable=${this.conversations.listFailed}
        .filterActive=${Boolean(this.sourceId || this.search || hasSessionConditions(this.conditions))}
        .selected=${this.conversations.listSelection?.conversationId ?? ""}
        .selectedSource=${this.conversations.listSelection?.sourceId ?? ""}
        .locationForSession=${this.locationForSession}
      ></am-session-list></div>`;
    if (!selected) return html`<section class="workspace list-only" data-view="list">${listPanel}</section>`;
    return html`<section class="workspace" data-view="detail">
      <div class="detail">${this.renderSelected(selected, selectedAgentId, visibleActivities)}</div>
    </section>`;
  }

	protected updated() {
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
    return html`
      <section class="panel session-head-panel">${this.returnHref ? html`<a class="context-return" href=${this.returnHref} @click=${this.returnToOrigin}>← ${this.returnLabel}</a>` : null}<a class="list-return" href=${this.listHref} @click=${this.returnToList}>← ${localization.t("app.conversations")}</a><div class="session-head"><div><p class="eyebrow">${localization.t("workspace.selected")}</p><h2 class="session-id" tabindex="-1">${selected.id}</h2></div></div><details class="session-overview"><summary>${localization.t("workspace.sessionOverview")}</summary><div class="session-metrics" aria-label=${localization.t("workspace.usageAria")}>
        <am-kpi-card .label=${localization.t("workspace.totalTokens")} .value=${formatOptionalNumber(selected.tokens.total)} .hint=${localization.t("workspace.inputOutput")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("workspace.inputTokens")} .value=${formatOptionalNumber(selected.tokens.input)} .hint=${localization.t("workspace.reportedByModel")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("workspace.outputTokens")} .value=${formatOptionalNumber(selected.tokens.output)} .hint=${localization.t("workspace.reportedByModel")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("workspace.estimatedCost")} .value=${formatCost(selected.costUsd)} .hint=${selected.costUsd === undefined ? notReported() : localization.t("workspace.observedTelemetry")}></am-kpi-card>
      </div></details><p class="coverage-note">${localization.t("workspace.coverage", { state: localization.t(this.conversations.rework?.coverage.activityCoverage === "observed_projection_complete" ? "workspace.coverageComplete" : this.conversations.rework ? "workspace.coveragePartial" : "workspace.coverageUnavailable") })}</p></section>
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
      ${this.renderOperations(selected, selectedAgentId, activities)}
      <details class="execution-context" ?hidden=${this.purpose !== "execution"}><summary>${localization.t("workspace.executionContext")}</summary><div class="context-grid"><section class="panel traffic-panel"><h2>${localization.t("workspace.modelTraffic")}</h2><am-token-chart .usage=${selected.tokens}></am-token-chart></section><section class="panel topology-panel"><h2>${localization.t("workspace.agentTopology")}</h2><am-agent-tree .agents=${selected.agents} .selectedAgentId=${selectedAgentId} @agent-selected=${this.agentSelected}></am-agent-tree></section></div></details>
    `;
  }

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
const formatCost = (value?: number) => value === undefined ? notReported() : localization.number(value, { style: "currency", currency: "USD", maximumFractionDigits: 4 });

declare global { interface HTMLElementTagNameMap { "am-conversation-workspace": ConversationWorkspace } }
