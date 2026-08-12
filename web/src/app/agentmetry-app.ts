import { LitElement, css, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import "../components/activity-table";
import "../components/agent-tree";
import "../components/kpi-card";
import "../components/plan-usage";
import "../components/session-list";
import "../components/session-filter";
import "../components/time-range-filter";
import "../components/token-chart";
import "../components/trace-participants";
import "../components/trace-summary";
import "../components/trace-waterfall";
import { agentmetryClient, type ActivityPage } from "../api/agentmetry-client";
import type { RangeSelectedDetail } from "../components/time-range-filter";
import { observedActivityCount, selectedSession } from "../model/selectors";
import { agentDisplayLabel } from "../model/agent-label";
import { conversationTargetFromLocation } from "../model/trace-analysis";
import { NOT_REPORTED, UNAVAILABLE } from "../presentation/missing-data";
import {
  initialModel,
  update,
  type Effect,
  type Message,
  type Model,
  type Overview,
  type Session,
  type Trace,
} from "../model/update";

type AgentActivityPage = ActivityPage & Readonly<{
  sessionId: string;
  sourceId: string;
  agentId: string;
  loading: boolean;
  error?: string;
}>;

@customElement("am-app")
export class AgentmetryApp extends LitElement {
  @state() private model: Model = initialModel();
  @state() private selectedAgentId = "";
  @state() private agentActivityPage?: AgentActivityPage;
  private agentActivityRequest = 0;

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener("popstate", this.popState);
    const conversationTarget = conversationTargetFromLocation(window.location.pathname, window.location.search);
    if (conversationTarget) this.dispatch({ type: "conversation-route-selected", target: conversationTarget });
    this.dispatch({ type: "connected" });
    const traceId = traceIdFromPath(window.location.pathname);
    if (traceId) this.dispatch({ type: "trace-selected", traceId });
  }

  disconnectedCallback() {
    window.removeEventListener("popstate", this.popState);
    super.disconnectedCallback();
  }

  static styles = css`
    :host {
      --am-paper: #070a0f;
      --am-surface: #0d121a;
      --am-surface-raised: #121923;
      --am-surface-strong: #151e29;
      --am-text: #edf5fb;
      --am-muted: #8795a6;
      --am-border: rgba(155, 190, 213, .16);
      --am-border-strong: rgba(109, 244, 214, .38);
      --am-accent: #6df4d6;
      --am-accent-rgb: 109, 244, 214;
      --am-accent-soft: rgba(109, 244, 214, .11);
      --am-secondary: #8ba6ff;
      --am-track: #1a2430;
      --am-danger: #ff7082;
      --am-success: #65e6a5;
      display: block;
      min-height: 100vh;
      color: var(--am-text);
      background:
        radial-gradient(circle at 12% -8%, rgba(109, 244, 214, .12), transparent 28rem),
        radial-gradient(circle at 92% 2%, rgba(139, 166, 255, .10), transparent 32rem),
        linear-gradient(90deg, rgba(154, 190, 214, .035) 1px, transparent 1px) 0 0 / 28px 28px,
        linear-gradient(rgba(154, 190, 214, .035) 1px, transparent 1px) 0 0 / 28px 28px,
        var(--am-paper);
      font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      color-scheme: dark;
    }

    * { box-sizing: border-box; }
    main { width: 100%; min-width: 0; max-width: 1800px; margin: 0 auto; padding: clamp(16px, 1.5vw, 24px); }
    header { position: relative; display: flex; align-items: flex-end; justify-content: space-between; gap: 24px; margin-bottom: 18px; padding: 0 2px 6px; }
    header::after { content: ""; position: absolute; inset: auto 0 -8px; height: 1px; background: linear-gradient(90deg, var(--am-accent), rgba(109, 244, 214, .08) 42%, transparent); }
    .brand { display: flex; width: fit-content; align-items: center; gap: 10px; margin-bottom: 12px; color: var(--am-text); font: 800 .7rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .22em; text-decoration: none; }
    .brand:hover { color: var(--am-accent); }
    .brand:focus-visible { border-radius: 7px; outline: 2px solid var(--am-accent); outline-offset: 4px; }
    .brand-mark { position: relative; width: 24px; height: 24px; border: 1px solid var(--am-border-strong); border-radius: 7px; background: var(--am-accent-soft); box-shadow: inset 0 0 16px rgba(var(--am-accent-rgb), .08), 0 0 20px rgba(var(--am-accent-rgb), .1); }
    .brand-mark::before, .brand-mark::after { content: ""; position: absolute; background: var(--am-accent); }
    .brand-mark::before { width: 9px; height: 2px; top: 7px; left: 7px; box-shadow: 0 7px 0 var(--am-accent); }
    .brand-mark::after { width: 2px; height: 9px; top: 7px; left: 7px; box-shadow: 7px 0 0 var(--am-accent); opacity: .45; }
    .eyebrow { margin: 0 0 6px; color: var(--am-accent); font: 700 .64rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .15em; text-transform: uppercase; }
    h1 { max-width: 720px; margin: 0; font: 650 clamp(2.2rem, 4vw, 4.2rem)/.9 Inter, ui-sans-serif, sans-serif; letter-spacing: -.065em; }
    h1 span { color: var(--am-muted); font-weight: 400; }
    h2 { margin: 0 0 12px; font: 650 .82rem/1.2 Inter, ui-sans-serif, sans-serif; letter-spacing: .01em; }
    .header-controls { display: grid; justify-items: end; flex: 0 1 auto; min-width: min(100%, 520px); }
    .status { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 5px 8px; margin: 11px 0 0; color: var(--am-muted); font: .7rem/1.45 "SFMono-Regular", "Cascadia Code", monospace; text-align: right; }
    .receiver { display: inline-flex; align-items: center; gap: 8px; }
    .state-note::before { content: "·"; margin-right: 8px; color: var(--am-muted); }
    .status-dot { width: 7px; height: 7px; flex: 0 0 auto; border-radius: 50%; background: var(--am-success); box-shadow: 0 0 0 4px rgba(101, 230, 165, .09), 0 0 14px rgba(101, 230, 165, .55); animation: pulse 2.8s ease-in-out infinite; }
    @keyframes pulse { 50% { box-shadow: 0 0 0 6px rgba(101, 230, 165, .03), 0 0 20px rgba(101, 230, 165, .7); } }
    .kpis { display: grid; grid-template-columns: repeat(4, minmax(130px, 1fr)); gap: 10px; margin-bottom: 10px; }
    .workspace { display: grid; grid-template-columns: 264px minmax(0, 1fr); gap: 12px; align-items: start; }
    aside.panel { position: sticky; top: 16px; }
    .operations-panel { margin: 0; }
    .operations-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
    .operations-heading h2 { margin: 0; }
    .agent-filter { display: flex; align-items: center; flex-wrap: wrap; justify-content: flex-end; gap: 7px; color: var(--am-muted); font-size: .72rem; }
    .agent-filter strong { max-width: 25ch; overflow: hidden; color: var(--am-text); font: 600 .7rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-overflow: ellipsis; white-space: nowrap; }
    .agent-filter button { border: 1px solid var(--am-border); border-radius: 7px; padding: 5px 9px; background: var(--am-surface-raised); color: var(--am-text); cursor: pointer; font: inherit; }
    .agent-filter button:hover, .agent-filter button:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    .session-head-panel { padding-top: 12px; padding-bottom: 12px; }
    .session-metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; margin-top: 10px; }
    .trace-view { display: grid; gap: 12px; }
    .trace-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 14px; }
    .trace-close { border: 1px solid var(--am-border); border-radius: 8px; background: var(--am-surface-raised); color: var(--am-text); padding: 8px 13px; cursor: pointer; text-decoration: none; }
    .trace-close:hover, .trace-close:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    .trace-state { min-height: 220px; display: grid; place-items: center; color: var(--am-muted); }
    .panel { position: relative; min-width: 0; max-width: 100%; border: 1px solid var(--am-border); border-radius: 12px; background: linear-gradient(145deg, rgba(18, 25, 35, .94), rgba(10, 15, 22, .94)); padding: 15px; box-shadow: inset 0 1px 0 rgba(255, 255, 255, .025), 0 14px 36px rgba(0, 0, 0, .16); }
    .panel::before { content: ""; position: absolute; inset: 0 auto auto 18px; width: 34px; height: 1px; background: var(--am-accent); box-shadow: 0 0 12px rgba(var(--am-accent-rgb), .55); }
    .plan-panel { display: grid; grid-template-columns: 116px minmax(0, 1fr); gap: 14px; align-items: start; margin-bottom: 10px; padding-top: 11px; padding-bottom: 11px; }
    .plan-panel h2 { margin: 2px 0 0; }
    .detail { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 12px; min-width: 0; }
    .session-head-panel, .operations-panel, .detail > .empty { grid-column: 1 / -1; }
    .traffic-panel { grid-column: span 5; }
    .topology-panel { grid-column: span 7; }
    .session-head { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
    .session-id { margin: 2px 0 0; font: .78rem/1.4 "SFMono-Regular", "Cascadia Code", monospace; overflow-wrap: anywhere; }
    .topology-panel { padding-bottom: 12px; }
    .traffic-panel { padding-bottom: 12px; }
    .empty { min-height: 280px; display: grid; place-items: center; text-align: center; color: var(--am-muted); }
    .empty h2 { color: var(--am-text); }
    .empty p { max-width: 46ch; line-height: 1.6; }
    .error { color: var(--am-danger); }

    @media (max-width: 1200px) {
      .workspace { grid-template-columns: 240px minmax(0, 1fr); }
    }

    @media (max-width: 950px) {
      .workspace { grid-template-columns: 1fr; }
      header { align-items: flex-start; flex-direction: column; }
      .header-controls { justify-items: start; }
      .status { justify-content: flex-start; text-align: left; }
      aside.panel { position: static; }
    }

    @media (max-width: 640px) {
      main { padding: 12px; }
      h1 { font-size: clamp(2.35rem, 12vw, 4rem); }
      .brand { margin-bottom: 14px; }
      .kpis, .session-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .kpis { gap: 8px; }
      .panel { padding: 12px; border-radius: 12px; }
      .detail { gap: 12px; }
      .traffic-panel, .topology-panel { grid-column: 1 / -1; }
      .plan-panel { grid-template-columns: 1fr; gap: 8px; }
    }

    @media (max-width: 480px) {
      .kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .session-head { display: block; }
      .status { align-items: flex-start; flex-direction: column; }
      .state-note::before { content: none; }
    }

    @media (max-width: 340px) { .kpis { grid-template-columns: 1fr; } }

    @media (prefers-reduced-motion: reduce) { .status-dot { animation: none; } }
  `;

  render() {
    const overview = this.model.overview;
    const selected = selectedSession(this.model);
    const selectedAgentId = selected?.agents.some((agent) => agent.agentId === this.selectedAgentId) ? this.selectedAgentId : "";
    const overviewPlaceholder = this.model.status === "failed" ? UNAVAILABLE : "Loading…";
    const visibleActivities = selected
      ? selectedAgentId
        ? this.agentActivityPage?.sessionId === selected.id && this.agentActivityPage.agentId === selectedAgentId
          ? this.agentActivityPage.activities
          : []
        : selected.activities
      : [];
    return html`<main data-density="operator">
      <header>
        <div><a class="brand" href="/" aria-label="Back to Agentmetry dashboard" @click=${this.goHome}><span class="brand-mark" aria-hidden="true"></span><span>AGENTMETRY</span></a><p class="eyebrow">Local trace observatory // Live</p><h1>Agent conversations,<br><span>decoded.</span></h1></div>
        <div class="header-controls"><am-time-range-filter .selected=${this.model.range} @range-selected=${this.rangeSelected}></am-time-range-filter><p class="status">${this.statusText()}</p></div>
      </header>

      <section class="kpis" aria-label="Conversation overview">
        <am-kpi-card label="Conversations" .value=${overview ? String(overview.sessions.length) : overviewPlaceholder}></am-kpi-card>
        <am-kpi-card label="Agents" .value=${overview ? String(overview.agentCount) : overviewPlaceholder}></am-kpi-card>
        <am-kpi-card label="Activities" .value=${overview ? String(observedActivityCount(overview)) : overviewPlaceholder}></am-kpi-card>
        <am-kpi-card label="Observed model traffic" .value=${overview ? formatOptionalNumber(overview.tokens.total) : overviewPlaceholder} hint="Input + output reported by model calls; not a plan quota"></am-kpi-card>
      </section>

      <section class="panel plan-panel"><h2>Plan limits</h2><am-plan-usage .snapshots=${overview?.planUsage ?? []}></am-plan-usage></section>

      ${this.model.selectedTraceId ? this.renderTraceView() : html`<section class="workspace">
        <aside class="panel"><h2>Conversations</h2><am-session-filter
          .sources=${overview?.sources ?? []}
          .selectedSource=${this.model.sourceId}
          .search=${this.model.search}
          @source-selected=${this.sourceSelected}
          @search-submitted=${this.searchSubmitted}
        ></am-session-filter><am-session-list .sessions=${conversationList(this.model)} .loading=${this.model.status === "loading" && !overview} .unavailable=${this.model.status === "failed" && !overview} .filterActive=${Boolean(this.model.sourceId || this.model.search)} .selected=${this.model.selectedSessionId ?? ""} .selectedSource=${this.model.selectedSessionSourceId ?? ""} @session-selected=${this.sessionSelected}></am-session-list></aside>
        <div class="detail">${selected ? html`
          <section class="panel session-head-panel">
            <div class="session-head"><div><p class="eyebrow">Selected conversation</p><p class="session-id">${selected.id}</p></div></div>
            <div class="session-metrics" aria-label="Selected conversation usage">
              <am-kpi-card label="Total tokens" .value=${formatOptionalNumber(selected.tokens.total)} hint="Input + output"></am-kpi-card>
              <am-kpi-card label="Input tokens" .value=${formatOptionalNumber(selected.tokens.input)} hint="Reported by model calls"></am-kpi-card>
              <am-kpi-card label="Output tokens" .value=${formatOptionalNumber(selected.tokens.output)} hint="Reported by model calls"></am-kpi-card>
              <am-kpi-card label="Estimated cost" .value=${formatCost(selected.costUsd)} .hint=${selected.costUsd === undefined ? "Not reported" : "Observed telemetry"}></am-kpi-card>
            </div>
          </section>
          <section class="panel traffic-panel"><h2>Observed model traffic</h2><am-token-chart .usage=${selected.tokens}></am-token-chart></section>
          <section class="panel topology-panel"><h2>Agent topology</h2><am-agent-tree .agents=${selected.agents} .selectedAgentId=${selectedAgentId} @agent-selected=${this.agentSelected}></am-agent-tree></section>
          ${this.renderOperations(selected, selectedAgentId, visibleActivities)}
        ` : this.model.status === "loading" ? html`<section class="panel empty" role="status"><div><h2>Loading conversations</h2><p>Reading the latest bounded dashboard view.</p></div></section>`
          : this.model.conversationStatus === "loading" ? html`<section class="panel empty" role="status"><div><h2>Loading requested conversation</h2><p>Resolving the source-qualified conversation and span.</p></div></section>`
          : this.model.conversationStatus === "failed" ? html`<section class="panel empty error" role="alert"><div><h2>Conversation unavailable</h2><p>${this.model.conversationError}</p></div></section>`
          : this.model.status === "failed" ? html`<section class="panel empty error" role="alert"><div><h2>Dashboard unavailable</h2><p>Conversation data could not be loaded.</p></div></section>`
          : html`<section class="panel empty"><div><h2>Waiting for agent telemetry</h2><p>Point an OTLP exporter at this process, then start a conversation.</p></div></section>`}</div>
      </section>`}
    </main>`;
  }

  private renderOperations(selected: Session, selectedAgentId: string, activities: Session["activities"]) {
    const activityPage = this.model.activityPage?.sessionId === selected.id && this.model.activityPage.sourceId === selected.sourceId
      ? this.model.activityPage
      : undefined;
    const selectedAgent = selected.agents.find((agent) => agent.agentId === selectedAgentId);
    const agentPage = this.agentActivityPage?.sessionId === selected.id && this.agentActivityPage.agentId === selectedAgentId
      ? this.agentActivityPage
      : undefined;
    return html`<section class="panel operations-panel"><div class="operations-heading"><h2>Operations & messages</h2>${selectedAgent ? html`<div class="agent-filter"><span>Filtered by</span><strong>${agentDisplayLabel(selectedAgent)}</strong><button type="button" @click=${this.clearAgentSelection}>All agents</button></div>` : null}</div><am-activity-table
      .activities=${activities}
      .hasEarlier=${selectedAgentId ? agentPage?.hasEarlier ?? false : selected.hasEarlier ?? false}
      .hasMore=${selectedAgentId ? agentPage?.hasMore ?? false : selected.hasMore ?? selected.activities.length < selected.activityCount}
      .loading=${selectedAgentId ? agentPage?.loading ?? true : activityPage?.loading ?? false}
      .pageDirection=${selectedAgentId ? (agentPage?.loading ? "older" : "") : activityPage?.direction ?? ""}
      .loadError=${selectedAgentId ? agentPage?.error ?? "" : activityPage?.error ?? ""}
      .highlightedSpanId=${this.model.highlightedSpanId ?? ""}
      .highlightedTraceId=${this.model.requestedConversation?.traceId ?? ""}
      .pagingContext=${`${selected.sourceId}:${selected.id}:${selectedAgentId}`}
      @activities-needed=${this.activitiesNeeded}
    ></am-activity-table></section>`;
  }

  private renderTraceView() {
    const trace = this.model.trace;
    return html`<section class="trace-view">
      <div class="panel trace-toolbar"><div><p class="eyebrow">Cross-conversation causality</p><h2>Trace explorer</h2></div><a class="trace-close" href="/" @click=${this.closeTrace}>Back to conversation</a></div>
      ${this.model.traceStatus === "loading" ? html`<div class="panel trace-state" role="status">Loading trace evidence…</div>` : null}
      ${this.model.traceStatus === "failed" ? html`<div class="panel trace-state error" role="alert">${this.model.traceError ?? "Trace unavailable"}</div>` : null}
      ${trace ? html`
        <section class="panel"><am-trace-summary .trace=${trace}></am-trace-summary></section>
        <section class="panel"><h2>Participants</h2><am-trace-participants .trace=${trace}></am-trace-participants></section>
        <section class="panel"><h2>Span & event timeline</h2><am-trace-waterfall
          .trace=${trace}
          .hasMore=${trace.hasMore}
          .loading=${this.model.traceStatus === "loading"}
          .onLoadMore=${this.traceActivitiesNeeded}
          @trace-activities-needed=${this.traceActivitiesNeeded}
        ></am-trace-waterfall></section>
      ` : null}
    </section>`;
  }

  private rangeSelected(event: CustomEvent<RangeSelectedDetail>) {
    this.resetAgentSelection();
    this.dispatch({ type: "range-selected", range: event.detail.range });
  }

  private sessionSelected(event: CustomEvent<{ sessionId: string; sourceId: string }>) {
    this.resetAgentSelection();
    history.pushState({}, "", conversationPath(event.detail.sourceId, event.detail.sessionId));
    this.dispatch({ type: "conversation-route-selected", target: { sourceId: event.detail.sourceId, conversationId: event.detail.sessionId } });
  }

  private sourceSelected(event: CustomEvent<{ sourceId: string }>) {
    this.resetAgentSelection();
    this.dispatch({ type: "source-selected", sourceId: event.detail.sourceId });
  }

  private searchSubmitted(event: CustomEvent<{ search: string }>) {
    this.resetAgentSelection();
    this.dispatch({ type: "search-submitted", search: event.detail.search });
  }

  private agentSelected(event: CustomEvent<{ agentId: string }>) {
    this.selectedAgentId = event.detail.agentId;
    this.agentActivityPage = undefined;
    if (event.detail.agentId) void this.loadAgentActivities("older", event.detail.agentId);
  }

  private readonly traceActivitiesNeeded = () => {
    const trace = this.model.trace;
    if (!trace || !trace.hasMore || this.model.traceStatus === "loading" || !trace.nextPageToken) return;
    this.dispatch({
      type: "trace-activities-requested",
      traceId: trace.traceId,
      offset: trace.activityOffset + trace.activities.length,
      pageToken: trace.nextPageToken,
    });
  };

  private readonly clearAgentSelection = () => {
    this.resetAgentSelection();
  };

  private readonly goHome = (event: MouseEvent) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    this.resetAgentSelection();
    if (window.location.pathname !== "/" || window.location.search) history.pushState({}, "", "/");
    this.dispatch({ type: "trace-closed" });
    this.dispatch({ type: "conversation-route-cleared" });
  };

  private activitiesNeeded(event: CustomEvent<{ direction: import("../model/update").ActivityDirection }>) {
    if (this.selectedAgentId) {
      void this.loadAgentActivities(event.detail.direction);
      return;
    }
    const session = selectedSession(this.model);
    if (session) this.dispatch({ type: "activities-requested", sessionId: session.id, sourceId: session.sourceId, direction: event.detail.direction });
  }

  private resetAgentSelection() {
    this.agentActivityRequest += 1;
    this.selectedAgentId = "";
    this.agentActivityPage = undefined;
  }

  private async loadAgentActivities(direction: import("../model/update").ActivityDirection, agentId = this.selectedAgentId) {
    const session = selectedSession(this.model);
    if (!session || !agentId) return;
    const current = this.agentActivityPage?.sessionId === session.id && this.agentActivityPage.agentId === agentId ? this.agentActivityPage : undefined;
    const offset = direction === "older" ? (current ? current.offset + current.activities.length : 0) : Math.max(0, (current?.offset ?? 0) - 100);
    const request = ++this.agentActivityRequest;
    this.agentActivityPage = {
      sessionId: session.id, sourceId: session.sourceId, agentId,
      activities: current?.activities ?? [], total: current?.total ?? 0, offset: current?.offset ?? 0,
      hasEarlier: current?.hasEarlier ?? false, hasMore: current?.hasMore ?? true, loading: true,
    };
    try {
      const page = await agentmetryClient.listSessionActivities(session.sourceId, session.id, direction, offset, 100, direction === "older" ? current?.nextPageToken : current?.previousPageToken, undefined, undefined, agentId);
      if (request !== this.agentActivityRequest || this.selectedAgentId !== agentId || selectedSession(this.model)?.id !== session.id) return;
      this.agentActivityPage = mergeAgentActivityPage(session.id, session.sourceId, agentId, current, page, direction);
    } catch (error) {
      if (request !== this.agentActivityRequest || this.selectedAgentId !== agentId) return;
      this.agentActivityPage = { sessionId: session.id, sourceId: session.sourceId, agentId, activities: current?.activities ?? [], total: current?.total ?? 0, offset: current?.offset ?? 0, hasEarlier: current?.hasEarlier ?? false, hasMore: current?.hasMore ?? false, loading: false, error: error instanceof Error ? error.message : "Agent activities unavailable" };
    }
  }

  private readonly closeTrace = (event: MouseEvent) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    if (shouldReturnThroughHistory(document.referrer, window.location.origin, history.length)) {
      history.back();
      return;
    }
    history.replaceState({}, "", "/");
    this.dispatch({ type: "trace-closed" });
  };

  private readonly popState = () => {
    this.resetAgentSelection();
    const conversationTarget = conversationTargetFromLocation(window.location.pathname, window.location.search);
    if (conversationTarget) {
      this.dispatch({ type: "trace-closed" });
      this.dispatch({ type: "conversation-route-selected", target: conversationTarget });
      return;
    }
    const traceId = traceIdFromPath(window.location.pathname);
    if (traceId) {
      this.dispatch({ type: "trace-selected", traceId });
      return;
    }
    this.dispatch({ type: "trace-closed" });
    this.dispatch({ type: "conversation-route-cleared" });
  };

  private dispatch(message: Message) {
    const [model, effects] = update(this.model, message);
    this.model = model;
    void this.runEffects(effects);
  }

  private async runEffects(effects: readonly Effect[]) {
    await Promise.all(effects.map(async (effect) => {
      try {
        if (effect.type === "fetch-overview") {
          const overview = await agentmetryClient.loadOverview(effect.range, effect.sourceId, effect.search);
          this.dispatch({ type: "overview-received", generation: effect.generation, overview });
          return;
        }
        if (effect.type === "fetch-conversation") {
          const conversation = await agentmetryClient.getSession(effect.target.sourceId, effect.target.conversationId, effect.target.traceId, effect.target.spanId);
          this.dispatch({ type: "conversation-received", generation: effect.generation, target: effect.target, conversation });
          return;
        }
        if (effect.type === "fetch-trace") {
          const trace = await agentmetryClient.getTrace(effect.traceId, effect.offset, effect.limit, effect.pageToken);
          this.dispatch({ type: "trace-received", generation: effect.generation, traceId: effect.traceId, trace });
          return;
        }
        {
          const page = await agentmetryClient.listSessionActivities(effect.sourceId, effect.sessionId, effect.direction, effect.offset, effect.limit, effect.pageToken, effect.traceId, effect.spanId);
          this.dispatch({
            type: "activities-received",
            generation: effect.generation,
            sessionId: effect.sessionId,
            sourceId: effect.sourceId,
            direction: effect.direction,
            exact: effect.exact,
            offset: page.offset,
            activities: page.activities,
            total: page.total,
            hasEarlier: page.hasEarlier,
            hasMore: page.hasMore,
            nextPageToken: page.nextPageToken,
            previousPageToken: page.previousPageToken,
          });
          return;
        }
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        this.dispatch(effect.type === "fetch-overview"
          ? { type: "overview-failed", generation: effect.generation, error: message }
          : effect.type === "fetch-conversation"
            ? { type: "conversation-failed", generation: effect.generation, target: effect.target, error: message }
          : effect.type === "fetch-trace"
            ? { type: "trace-failed", generation: effect.generation, traceId: effect.traceId, error: message }
            : {
              type: "activities-failed", generation: effect.generation,
              sessionId: effect.sessionId, sourceId: effect.sourceId,
              direction: effect.direction, exact: effect.exact, offset: effect.offset, error: message,
            });
      }
    }));
  }

  private statusText() {
    const receiver = html`<span class="receiver"><span class="status-dot" aria-label="Receiving OTLP"></span><span>Receiving OTLP locally · HTTP :4318 · gRPC :4317</span></span>`;
    if (this.model.status === "loading") return html`${receiver}<span class="state-note">Refreshing dashboard…</span>`;
    if (this.model.status === "failed") return html`${receiver}<span class="state-note error">Dashboard data unavailable</span>`;
    return receiver;
  }
}

const formatOptionalNumber = (value?: number | null) => value === undefined || value === null ? NOT_REPORTED : value.toLocaleString();
const formatCost = (value?: number) => value === undefined ? NOT_REPORTED : new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: 4 }).format(value);
const mergeAgentActivityPage = (
  sessionId: string,
  sourceId: string,
  agentId: string,
  current: AgentActivityPage | undefined,
  page: ActivityPage,
  direction: import("../model/update").ActivityDirection,
): AgentActivityPage => ({
  sessionId,
  sourceId,
  agentId,
  activities: direction === "newer" ? [...page.activities, ...(current?.activities ?? [])] : [...(current?.activities ?? []), ...page.activities],
  total: page.total,
  offset: direction === "newer" ? page.offset : current?.offset ?? page.offset,
  hasEarlier: page.hasEarlier,
  hasMore: page.hasMore,
  nextPageToken: page.nextPageToken,
  previousPageToken: page.previousPageToken,
  loading: false,
});
export function traceIdFromPath(pathname: string): string | undefined {
  const match = pathname.match(/^\/traces\/([^/]+)$/);
  if (!match) return undefined;
  try { return decodeURIComponent(match[1]); } catch { return undefined; }
}

export function shouldReturnThroughHistory(referrer: string, origin: string, historyLength: number): boolean {
  if (!referrer || historyLength <= 1) return false;
  try {
    const url = new URL(referrer);
    return url.origin === origin && (url.pathname === "/" || conversationTargetFromLocation(url.pathname, url.search) !== undefined);
  } catch {
    return false;
  }
}

const conversationPath = (sourceId: string, conversationId: string) =>
  `/conversations/${encodeURIComponent(sourceId)}/${encodeURIComponent(conversationId)}`;

const conversationList = (model: Model) => {
  const sessions = model.overview?.sessions ?? [];
  const routed = model.routedConversation;
  if (!routed || sessions.some(({ id, sourceId }) => id === routed.id && sourceId === routed.sourceId)) return sessions;
  return [routed, ...sessions];
};

declare global { interface HTMLElementTagNameMap { "am-app": AgentmetryApp } }
