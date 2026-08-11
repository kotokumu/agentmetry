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
import { agentmetryClient } from "../api/agentmetry-client";
import type { RangeSelectedDetail } from "../components/time-range-filter";
import { observedActivityCount, selectedSession } from "../model/selectors";
import { conversationTargetFromLocation } from "../model/trace-analysis";
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

@customElement("am-app")
export class AgentmetryApp extends LitElement {
  @state() private model: Model = initialModel();

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
      --am-paper: #f2efe5;
      --am-surface: #fffdf7;
      --am-surface-strong: #e9e5d9;
      --am-text: #172034;
      --am-muted: #677083;
      --am-border: #c8c4b8;
      --am-accent: #d6532b;
      --am-accent-soft: #f5d8cc;
      --am-track: #ded9cc;
      display: block;
      min-height: 100vh;
      color: var(--am-text);
      background:
        linear-gradient(90deg, rgba(23, 32, 52, .035) 1px, transparent 1px) 0 0 / 24px 24px,
        linear-gradient(rgba(23, 32, 52, .035) 1px, transparent 1px) 0 0 / 24px 24px,
        var(--am-paper);
      font-family: "Avenir Next", "Segoe UI", sans-serif;
    }

    * { box-sizing: border-box; }
    main { width: 100%; min-width: 0; margin: 0; padding: clamp(12px, 1.25vw, 20px); }
    header { display: flex; align-items: flex-end; justify-content: space-between; gap: 18px; margin-bottom: 16px; }
    .eyebrow { margin: 0 0 4px; color: var(--am-accent); font: 700 .7rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .14em; text-transform: uppercase; }
    h1 { margin: 0; font: 500 clamp(2rem, 4vw, 3.7rem)/.95 "Iowan Old Style", "Palatino Linotype", serif; letter-spacing: -.035em; }
    h2 { margin: 0 0 14px; font: 600 1rem/1.2 "Iowan Old Style", "Palatino Linotype", serif; }
    .status { display: flex; align-items: center; gap: 7px; margin: 8px 0 0; color: var(--am-muted); font-size: .78rem; }
    .status-dot { width: 8px; height: 8px; flex: 0 0 auto; border-radius: 50%; background: #4f8757; box-shadow: 0 0 0 3px rgba(79, 135, 87, .14); }
    .kpis { display: grid; grid-template-columns: repeat(5, minmax(130px, 1fr)); gap: 10px; margin-bottom: 12px; }
    .workspace { display: grid; grid-template-columns: 260px minmax(0, 1fr); gap: 14px; align-items: start; }
    .operations-panel { margin: 0; }
    .session-head-panel { padding-top: 12px; padding-bottom: 12px; }
    .trace-view { display: grid; gap: 18px; }
    .trace-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 14px; }
    .trace-close { border: 1px solid var(--am-border); border-radius: 99px; background: var(--am-surface); color: var(--am-text); padding: 8px 13px; cursor: pointer; text-decoration: none; }
    .trace-state { min-height: 220px; display: grid; place-items: center; color: var(--am-muted); }
    .panel { min-width: 0; max-width: 100%; border: 1px solid var(--am-border); border-radius: 16px; background: color-mix(in srgb, var(--am-surface) 92%, transparent); padding: 17px; box-shadow: 0 14px 45px rgba(23, 32, 52, .06); }
    .plan-panel { margin-bottom: 12px; padding-top: 12px; padding-bottom: 12px; }
    .detail { display: grid; gap: 18px; min-width: 0; }
    .session-head { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
    .session-id { margin: 2px 0 0; font: .78rem/1.4 "SFMono-Regular", "Cascadia Code", monospace; overflow-wrap: anywhere; }
    .split { display: grid; grid-template-columns: minmax(260px, .7fr) minmax(320px, 1.3fr); gap: 12px; align-items: start; }
    .empty { min-height: 280px; display: grid; place-items: center; text-align: center; color: var(--am-muted); }
    .error { color: #9f2f23; }

    @media (max-width: 1200px) {
      .kpis { grid-template-columns: repeat(3, minmax(0, 1fr)); }
      .workspace { grid-template-columns: 220px minmax(0, 1fr); }
      .split { grid-template-columns: minmax(220px, .9fr) minmax(280px, 1.1fr); }
    }

    @media (max-width: 950px) {
      .kpis { grid-template-columns: repeat(2, 1fr); }
      .workspace, .split { grid-template-columns: 1fr; }
      header { align-items: flex-start; flex-direction: column; }
    }

    @media (max-width: 640px) {
      main { padding: 12px; }
      h1 { font-size: clamp(2rem, 10vw, 3rem); }
      .kpis { gap: 8px; }
      .panel { padding: 12px; border-radius: 12px; }
      .detail { gap: 12px; }
    }

    @media (max-width: 480px) {
      .kpis { grid-template-columns: 1fr; }
      .session-head { display: block; }
    }
  `;

  render() {
    const overview = this.model.overview;
    const selected = selectedSession(this.model);
    return html`<main>
      <header>
        <div><p class="eyebrow">Local trace observatory</p><h1>Agent conversations,<br>under glass.</h1></div>
        <div><am-time-range-filter .selected=${this.model.range} @range-selected=${this.rangeSelected}></am-time-range-filter><p class="status">${this.statusText()}</p></div>
      </header>

      <section class="kpis" aria-label="Conversation overview">
        <am-kpi-card label="Conversations" .value=${overview ? String(overview.sessions.length) : "N/A"}></am-kpi-card>
        <am-kpi-card label="Agents" .value=${overview ? String(overview.agentCount) : "N/A"}></am-kpi-card>
        <am-kpi-card label="Activities" .value=${overview ? String(observedActivityCount(overview)) : "N/A"}></am-kpi-card>
        <am-kpi-card label="Observed model traffic" .value=${formatOptionalNumber(overview?.tokens.total)} hint="Input + output reported by model calls; not a plan quota"></am-kpi-card>
        <am-kpi-card label="Estimated cost" .value=${formatCost(selected?.costUsd)} .hint=${selected?.costUsd === undefined ? "N/A" : "Observed telemetry"}></am-kpi-card>
      </section>

      <section class="panel plan-panel"><h2>Plan limits</h2><am-plan-usage .snapshots=${overview?.planUsage ?? []}></am-plan-usage></section>

      ${this.model.selectedTraceId ? this.renderTraceView() : html`<section class="workspace">
        <aside class="panel"><h2>Conversations</h2><am-session-filter
          .sources=${overview?.sources ?? []}
          .selectedSource=${this.model.sourceId}
          .search=${this.model.search}
          @source-selected=${this.sourceSelected}
          @search-submitted=${this.searchSubmitted}
        ></am-session-filter><am-session-list .sessions=${conversationList(this.model)} .loading=${this.model.status === "loading" && !overview} .selected=${this.model.selectedSessionId ?? ""} .selectedSource=${this.model.selectedSessionSourceId ?? ""} @session-selected=${this.sessionSelected}></am-session-list></aside>
        <div class="detail">${selected ? html`
          <section class="panel session-head-panel">
            <div class="session-head"><div><p class="eyebrow">Selected conversation</p><p class="session-id">${selected.id}</p></div></div>
          </section>
          <section class="split">
            <div class="panel"><h2>Agent topology</h2><am-agent-tree .agents=${selected.agents}></am-agent-tree></div>
            <div class="panel"><h2>Observed model traffic</h2><am-token-chart .usage=${selected.tokens}></am-token-chart></div>
          </section>
          ${this.renderOperations(selected)}
        ` : this.model.status === "loading" ? html`<section class="panel empty" role="status"><div><h2>Loading conversations</h2><p>Reading the latest bounded dashboard view.</p></div></section>`
          : this.model.conversationStatus === "loading" ? html`<section class="panel empty" role="status"><div><h2>Loading requested conversation</h2><p>Resolving the source-qualified conversation and span.</p></div></section>`
          : this.model.conversationStatus === "failed" ? html`<section class="panel empty error" role="alert"><div><h2>Conversation unavailable</h2><p>${this.model.conversationError}</p></div></section>`
          : html`<section class="panel empty"><div><h2>Waiting for agent telemetry</h2><p>Point an OTLP exporter at this process, then start a conversation.</p></div></section>`}</div>
      </section>`}
    </main>`;
  }

  private renderOperations(selected: Session) {
    const activityPage = this.model.activityPage?.sessionId === selected.id && this.model.activityPage.sourceId === selected.sourceId
      ? this.model.activityPage
      : undefined;
    return html`<section class="panel operations-panel"><h2>Operations & messages</h2><am-activity-table
      .activities=${selected.activities}
      .hasEarlier=${selected.hasEarlier ?? false}
      .hasMore=${selected.hasMore ?? selected.activities.length < selected.activityCount}
      .loading=${activityPage?.loading ?? false}
      .pageDirection=${activityPage?.direction ?? ""}
      .loadError=${activityPage?.error ?? ""}
      .highlightedSpanId=${this.model.highlightedSpanId ?? ""}
      .highlightedTraceId=${this.model.requestedConversation?.traceId ?? ""}
      .pagingContext=${`${selected.sourceId}:${selected.id}`}
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
        <section class="panel"><h2>Span & event timeline</h2><am-trace-waterfall .trace=${trace}></am-trace-waterfall></section>
      ` : null}
    </section>`;
  }

  private rangeSelected(event: CustomEvent<RangeSelectedDetail>) {
    this.dispatch({ type: "range-selected", range: event.detail.range });
  }

  private sessionSelected(event: CustomEvent<{ sessionId: string; sourceId: string }>) {
    history.pushState({}, "", conversationPath(event.detail.sourceId, event.detail.sessionId));
    this.dispatch({ type: "conversation-route-selected", target: { sourceId: event.detail.sourceId, conversationId: event.detail.sessionId } });
  }

  private sourceSelected(event: CustomEvent<{ sourceId: string }>) {
    this.dispatch({ type: "source-selected", sourceId: event.detail.sourceId });
  }

  private searchSubmitted(event: CustomEvent<{ search: string }>) {
    this.dispatch({ type: "search-submitted", search: event.detail.search });
  }

  private activitiesNeeded(event: CustomEvent<{ direction: import("../model/update").ActivityDirection }>) {
    const session = selectedSession(this.model);
    if (session) this.dispatch({ type: "activities-requested", sessionId: session.id, sourceId: session.sourceId, direction: event.detail.direction });
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
          const trace = await agentmetryClient.getTrace(effect.traceId);
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
    const receiver = html`<span class="status-dot" aria-label="Receiving OTLP"></span>Receiving OTLP locally · HTTP :4318 · gRPC :4317`;
    if (this.model.status === "loading") return html`${receiver} · Refreshing dashboard…`;
    if (this.model.status === "failed") return html`${receiver} · <span class="error">Dashboard refresh failed: ${this.model.error}</span>`;
    return receiver;
  }
}

const formatOptionalNumber = (value?: number | null) => value === undefined || value === null ? "N/A" : value.toLocaleString();
const formatCost = (value?: number) => value === undefined ? "N/A" : new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: 4 }).format(value);
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
