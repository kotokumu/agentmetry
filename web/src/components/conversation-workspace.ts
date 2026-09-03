import { LitElement, css, html, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import "./activity-table";
import { activityIdentity, type ActivityTable } from "./activity-table";
import type { ReworkSummary } from "./rework-summary";
import type { NavigationViewState } from "../app/navigation";
import "./agent-tree";
import "./kpi-card";
import "./rework-comparison";
import type { ComparisonBaselineSelectedDetail } from "./rework-comparison";
import "./rework-summary";
import "./session-filter";
import "./investigation-filter";
import "./saved-filters";
import { hasSessionConditions, type SessionConditions } from "../model/investigation-conditions";
import "./session-list";
import "./token-chart";
import { agentmetryClient } from "../api/agentmetry-client";
import { ConversationsController } from "../controllers/conversations-controller";
import { SessionComparisonController } from "../controllers/session-comparison-controller";
import { agentDisplayLabel } from "../model/agent-label";
import type { ConversationTarget } from "../model/trace-analysis";
import type { ActivityDirection, Session, TelemetrySource, TimeRange } from "../model/telemetry";
import { NOT_REPORTED } from "../presentation/missing-data";
import { featurePanelStyles } from "./feature-styles";
import { LIVE_UPDATE_EVENT, type LiveUpdateDelivery } from "../controllers/live-update-controller";

export type ConversationSummaryDetail = Readonly<{
  status: "loading" | "ready" | "failed";
  conversationCount?: number;
  activityCount?: number;
}>;

@customElement("am-conversation-workspace")
export class ConversationWorkspace extends LitElement {
  @property() range: TimeRange = "24h";
  @property() sourceId = "";
  @property() search = "";
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
  @state() private selectedActivityId = "";
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
  );
  private readonly comparison = new SessionComparisonController(
    this,
    {
      reader: agentmetryClient,
      current: () => this.conversations.selected,
      sessions: () => this.conversations.sessions,
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
    .workspace { display: grid; grid-template-columns: 264px minmax(0, 1fr); gap: 12px; align-items: start; }
    aside.panel {
      position: sticky;
      top: 16px;
      display: flex;
      flex-direction: column;
      max-height: calc(100dvh - 32px);
      overflow: hidden;
    }
    aside.panel > h2, aside.panel > am-session-filter { flex: 0 0 auto; }
    aside.panel > am-session-list {
      flex: 1 1 auto;
      min-height: 0;
      overflow-y: auto;
      overscroll-behavior: contain;
      scrollbar-gutter: stable;
    }
    .detail { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 12px; min-width: 0; }
    .session-head-panel, .operations-panel, .detail > .empty, .detail > am-rework-summary, .detail > am-rework-comparison { grid-column: 1 / -1; }
    .traffic-panel { grid-column: span 5; padding-bottom: 12px; }
    .topology-panel { grid-column: span 7; padding-bottom: 12px; }
    .session-head-panel { padding-top: 12px; padding-bottom: 12px; }
    .context-return, .list-return { display: inline-flex; margin-bottom: 11px; color: var(--am-accent); font: 700 .72rem/1.3 "SFMono-Regular", "Cascadia Code", monospace; text-decoration: none; }
    .context-return:hover, .context-return:focus-visible, .list-return:hover, .list-return:focus-visible { color: var(--am-text); outline: 2px solid var(--am-accent-soft); outline-offset: 4px; }
    .list-return { display: none; }
    .session-head { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
    .session-id { margin: 2px 0 0; font: .78rem/1.4 "SFMono-Regular", "Cascadia Code", monospace; overflow-wrap: anywhere; }
    .session-metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; margin-top: 10px; }
    .operations-panel { margin: 0; }
    .coverage-note { color: var(--am-muted); font-size: .85rem; line-height: 1.5; overflow-wrap: anywhere; }
    .operations-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
    .operations-heading h2 { margin: 0; }
    .agent-filter { display: flex; align-items: center; flex-wrap: wrap; justify-content: flex-end; gap: 7px; color: var(--am-muted); font-size: .72rem; }
    .agent-filter strong { max-width: 25ch; overflow: hidden; color: var(--am-text); font: 600 .7rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-overflow: ellipsis; white-space: nowrap; }
    .agent-filter button, .retry { border: 1px solid var(--am-border); border-radius: 7px; padding: 5px 9px; background: var(--am-surface-raised); color: var(--am-text); cursor: pointer; font: inherit; }
    .retry { margin-top: 10px; padding: 7px 11px; }
    .agent-filter button:hover, .agent-filter button:focus-visible, .retry:hover, .retry:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    @media (max-width: 1200px) { .workspace { grid-template-columns: 240px minmax(0, 1fr); } }
    @media (max-width: 950px) {
      .workspace { grid-template-columns: 1fr; }
      aside.panel { position: static; display: block; max-height: none; overflow: visible; }
      aside.panel > am-session-list { overflow-y: visible; }
      .workspace[data-view="detail"] aside { display: none; }
      .workspace[data-view="list"] .detail { display: none; }
      .list-return { display: inline-flex; }
      .context-return + .list-return { display: none; }
    }
    @media (max-width: 640px) { .session-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .detail { gap: 12px; } .traffic-panel, .topology-panel { grid-column: 1 / -1; } }
    @media (max-width: 480px) { .session-head { display: block; } }
  `];

  protected willUpdate(changed: PropertyValues<this>) {
    if (changed.has("requestedActivityId")) this.selectedActivityId = this.requestedActivityId;
    const filtersKey = JSON.stringify(this.investigationFilters);
    if (filtersKey !== this.lastFiltersKey) { this.lastFiltersKey = filtersKey; this.conversations.filtersChanged(); }
	  if (changed.has("requestedConversation")) {
		this.selectedActivityId = this.requestedActivityId;
		this.restoredAgentKey = "";
		this.lastReadyKey = "";
		this.lastCanonicalKey = "";
      if (this.requestedConversation) this.conversations.select(this.requestedConversation);
      else this.conversations.clearRoute();
    }
  }

  private readonly liveUpdate = (event: CustomEvent<LiveUpdateDelivery>) => {
    event.detail.waitUntil(Promise.all([
      this.conversations.applyLiveUpdate(event.detail),
      this.comparison.applyLiveUpdate(event.detail),
    ]).then(() => {
      const removed = this.conversations.takeRemovedSession();
      if (removed) this.dispatchEvent(new CustomEvent("conversation-removed", { detail: removed, bubbles: true, composed: true }));
    }));
  };

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
    return html`<section class="workspace" data-view=${this.requestedConversation ? "detail" : "list"}>
      <aside class="panel"><h2 class="list-heading" tabindex="-1">Conversations</h2><am-session-filter
        .sources=${this.sources.length ? this.sources : this.conversations.sources}
        .selectedSource=${this.sourceId}
        .search=${this.search}
      ></am-session-filter><am-investigation-filter .filters=${this.investigationFilters} .pending=${this.filterPending} .confirmed=${!this.conversations.loadingList && !this.conversations.listFailed} .error=${this.filterError || (this.conversations.listFailed ? String(this.conversations.listError ?? "Conversation query unavailable") : "")}></am-investigation-filter><am-saved-filters .filters=${this.investigationFilters} .confirmed=${!this.filterPending && !this.conversations.loadingList && !this.conversations.listFailed} .pending=${this.filterPending || this.conversations.loadingList}></am-saved-filters><am-session-list
        .sessions=${sessions}
        .loading=${this.conversations.loadingList}
        .unavailable=${this.conversations.listFailed}
        .filterActive=${Boolean(this.sourceId || this.search || hasSessionConditions(this.conditions))}
        .selected=${this.conversations.target?.conversationId ?? ""}
        .selectedSource=${this.conversations.target?.sourceId ?? ""}
        .locationForSession=${this.locationForSession}
      ></am-session-list></aside>
      <div class="detail">${selected ? this.renderSelected(selected, selectedAgentId, visibleActivities)
        : this.conversations.loadingList ? html`<section class="panel empty" role="status"><div><h2>Loading conversations</h2><p>Reading the latest bounded conversation list.</p></div></section>`
        : this.conversations.loadingConversation ? html`<section class="panel empty" role="status"><div><h2>Loading requested conversation</h2><p>Resolving the source-qualified conversation and span.</p></div></section>`
        : this.conversations.conversationFailed ? html`<section class="panel empty error" role="alert"><div><h2>Conversation unavailable</h2><p>${String(this.conversations.conversationError ?? "Conversation unavailable")}</p><button class="retry" type="button" @click=${this.retryConversation}>Retry</button></div></section>`
        : this.conversations.listFailed ? html`<section class="panel empty error" role="alert"><div><h2>Conversations unavailable</h2><p>Conversation data could not be loaded.</p></div></section>`
        : html`<section class="panel empty"><div><h2>Waiting for agent telemetry</h2><p>Point an OTLP exporter at this process, then start a conversation.</p></div></section>`}</div>
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
      <section class="panel session-head-panel">${this.returnHref ? html`<a class="context-return" href=${this.returnHref} @click=${this.returnToOrigin}>← ${this.returnLabel}</a>` : null}<a class="list-return" href=${this.listHref} @click=${this.returnToList}>← Conversations</a><div class="session-head"><div><p class="eyebrow">Selected conversation</p><h2 class="session-id" tabindex="-1">${selected.id}</h2></div></div><div class="session-metrics" aria-label="Selected conversation usage">
        <am-kpi-card label="Total tokens" .value=${formatOptionalNumber(selected.tokens.total)} hint="Input + output"></am-kpi-card>
        <am-kpi-card label="Input tokens" .value=${formatOptionalNumber(selected.tokens.input)} hint="Reported by model calls"></am-kpi-card>
        <am-kpi-card label="Output tokens" .value=${formatOptionalNumber(selected.tokens.output)} hint="Reported by model calls"></am-kpi-card>
        <am-kpi-card label="Estimated cost" .value=${formatCost(selected.costUsd)} .hint=${selected.costUsd === undefined ? "Not reported" : "Observed telemetry"}></am-kpi-card>
      </div><p class="coverage-note">Analysis coverage: ${this.conversations.rework?.coverage.activityCoverage === "observed_projection_complete" ? "all retained projected activities" : this.conversations.rework ? "partial projected evidence" : "not yet available"}. This does not establish that every input or body was reported.</p></section>
      <nav class="purpose-nav" aria-label="Investigation view">${([ ["execution", "Execution"], ["rework", "Rework"], ["comparison", "Comparison"] ] as const).map(([purpose, label]) => html`<button type="button" data-purpose=${purpose} aria-pressed=${String(this.purpose === purpose)} @click=${() => this.selectPurpose(purpose)}>${label}</button>`)}</nav>
      <am-rework-summary
        ?hidden=${this.purpose !== "rework"}
        .analysis=${this.conversations.rework}
        .locationForTrace=${this.locationForTrace}
        .legacySessionTotalTokens=${selected.tokens.total}
        .loading=${this.conversations.loadingRework}
        .error=${this.conversations.reworkFailed ? String(this.conversations.reworkError ?? "Rework analysis unavailable") : ""}
        @rework-retry-requested=${this.retryRework}
      ></am-rework-summary>
      <am-rework-comparison
        ?hidden=${this.purpose !== "comparison"}
        .state=${this.comparison.viewState()}
        @comparison-baseline-selected=${this.comparisonBaselineSelected}
        @comparison-retry-requested=${this.retryComparison}
      ></am-rework-comparison>
      <section class="panel traffic-panel" ?hidden=${this.purpose !== "execution"}><h2>Observed model traffic</h2><am-token-chart .usage=${selected.tokens}></am-token-chart></section>
      <section class="panel topology-panel" ?hidden=${this.purpose !== "execution"}><h2>Agent topology</h2><am-agent-tree .agents=${selected.agents} .selectedAgentId=${selectedAgentId} @agent-selected=${this.agentSelected}></am-agent-tree></section>
      ${this.renderOperations(selected, selectedAgentId, activities)}
    `;
  }

  private renderOperations(selected: Session, selectedAgentId: string, activities: Session["activities"]) {
    const activityPage = this.conversations.activityPage;
    const selectedAgent = selected.agents.find(({ agentId }) => agentId === selectedAgentId);
    const agentPage = this.conversations.agentActivityPage?.sessionId === selected.id && this.conversations.agentActivityPage.agentId === selectedAgentId ? this.conversations.agentActivityPage : undefined;
    const retainedSelectedActivity = selected.activities.find((activity) => activityIdentity(activity) === this.selectedActivityId);
    const selectedVisibility = selectedAgentId && retainedSelectedActivity && retainedSelectedActivity.agentId !== selectedAgentId
      ? "outside_agent_filter" : "not_loaded";
    return html`<section class="panel operations-panel" ?hidden=${this.purpose !== "execution"}><div class="operations-heading"><h2>Operations & messages</h2>${selectedAgent ? html`<div class="agent-filter"><span>Filtered by</span><strong>${agentDisplayLabel(selectedAgent)}</strong><button type="button" @click=${this.clearAgentSelection}>All agents</button></div>` : null}</div><am-activity-table
      .selectedActivityId=${this.selectedActivityId}
      .retainedSelectedActivity=${retainedSelectedActivity}
      .selectedVisibility=${selectedVisibility}
      .agentFilterId=${selectedAgentId}
      @activity-selected=${this.activitySelected}
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
  private readonly retryComparison = () => this.comparison.refresh();
  private readonly returnToOrigin = (event: MouseEvent) => this.requestReturn(event, "origin");
  private readonly returnToList = (event: MouseEvent) => this.requestReturn(event, "list");
  private requestReturn(event: MouseEvent, to: "origin" | "list") {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    this.dispatchEvent(new CustomEvent("conversation-return-requested", { detail: { to }, bubbles: true, composed: true }));
  }

  private get investigationFilters() { return { range: this.range, sourceId: this.sourceId, search: this.search, ...this.conditions }; }

  get navigationViewState() {
    return { selectedAgentId: this.conversations.selectedAgentId, selectedActivityId: this.selectedActivityId, purpose: this.purpose, evidenceFocus: this.requestedEvidenceFocus } as const;
  }

  private selectPurpose(purpose: NonNullable<NavigationViewState["purpose"]>) {
    if (purpose === this.purpose) return;
    this.dispatchEvent(new CustomEvent("conversation-purpose-selected", { detail: { purpose }, bubbles: true, composed: true }));
  }

  private activitySelected(event: CustomEvent<{ activityId: string }>) {
    this.selectedActivityId = event.detail.activityId;
    this.reportViewStateChanged();
  }

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

const formatOptionalNumber = (value?: number | null) => value === undefined || value === null ? NOT_REPORTED : value.toLocaleString();
const formatCost = (value?: number) => value === undefined ? NOT_REPORTED : new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: 4 }).format(value);

declare global { interface HTMLElementTagNameMap { "am-conversation-workspace": ConversationWorkspace } }
