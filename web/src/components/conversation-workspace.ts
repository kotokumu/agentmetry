import { LitElement, css, html, type PropertyValues } from "lit";
import { customElement, property } from "lit/decorators.js";
import "./activity-table";
import "./agent-tree";
import "./kpi-card";
import "./session-filter";
import "./session-list";
import "./token-chart";
import { agentmetryClient } from "../api/agentmetry-client";
import { ConversationsController } from "../controllers/conversations-controller";
import { agentDisplayLabel } from "../model/agent-label";
import type { ConversationTarget } from "../model/trace-analysis";
import type { ActivityDirection, Session, TelemetrySource, TimeRange } from "../model/telemetry";
import { NOT_REPORTED } from "../presentation/missing-data";
import { featurePanelStyles } from "./feature-styles";

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
  @property({ attribute: false }) sources: readonly TelemetrySource[] = [];
  @property({ attribute: false }) requestedConversation?: ConversationTarget;
  @property({ type: Boolean }) active = true;
  private readonly conversations = new ConversationsController(
    this,
    agentmetryClient,
    () => ({ range: this.range, sourceId: this.sourceId, search: this.search }),
    () => this.active,
  );
  private lastSummaryKey = "";

  static styles = [featurePanelStyles, css`
    :host { display: block; }
    .workspace { display: grid; grid-template-columns: 264px minmax(0, 1fr); gap: 12px; align-items: start; }
    aside.panel { position: sticky; top: 16px; }
    .detail { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 12px; min-width: 0; }
    .session-head-panel, .operations-panel, .detail > .empty { grid-column: 1 / -1; }
    .traffic-panel { grid-column: span 5; padding-bottom: 12px; }
    .topology-panel { grid-column: span 7; padding-bottom: 12px; }
    .session-head-panel { padding-top: 12px; padding-bottom: 12px; }
    .session-head { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
    .session-id { margin: 2px 0 0; font: .78rem/1.4 "SFMono-Regular", "Cascadia Code", monospace; overflow-wrap: anywhere; }
    .session-metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; margin-top: 10px; }
    .operations-panel { margin: 0; }
    .operations-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
    .operations-heading h2 { margin: 0; }
    .agent-filter { display: flex; align-items: center; flex-wrap: wrap; justify-content: flex-end; gap: 7px; color: var(--am-muted); font-size: .72rem; }
    .agent-filter strong { max-width: 25ch; overflow: hidden; color: var(--am-text); font: 600 .7rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-overflow: ellipsis; white-space: nowrap; }
    .agent-filter button, .retry { border: 1px solid var(--am-border); border-radius: 7px; padding: 5px 9px; background: var(--am-surface-raised); color: var(--am-text); cursor: pointer; font: inherit; }
    .retry { margin-top: 10px; padding: 7px 11px; }
    .agent-filter button:hover, .agent-filter button:focus-visible, .retry:hover, .retry:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    @media (max-width: 1200px) { .workspace { grid-template-columns: 240px minmax(0, 1fr); } }
    @media (max-width: 950px) { .workspace { grid-template-columns: 1fr; } aside.panel { position: static; } }
    @media (max-width: 640px) { .session-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .detail { gap: 12px; } .traffic-panel, .topology-panel { grid-column: 1 / -1; } }
    @media (max-width: 480px) { .session-head { display: block; } }
  `];

  protected willUpdate(changed: PropertyValues<this>) {
    if (changed.has("range") || changed.has("sourceId") || changed.has("search")) this.conversations.filtersChanged();
    if (changed.has("requestedConversation")) {
      if (this.requestedConversation) this.conversations.select(this.requestedConversation);
      else this.conversations.clearRoute();
    }
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
    return html`<section class="workspace">
      <aside class="panel"><h2>Conversations</h2><am-session-filter
        .sources=${this.sources.length ? this.sources : this.conversations.sources}
        .selectedSource=${this.sourceId}
        .search=${this.search}
      ></am-session-filter><am-session-list
        .sessions=${sessions}
        .loading=${this.conversations.loadingList}
        .unavailable=${this.conversations.listFailed}
        .filterActive=${Boolean(this.sourceId || this.search)}
        .selected=${this.conversations.target?.conversationId ?? ""}
        .selectedSource=${this.conversations.target?.sourceId ?? ""}
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

  private renderSelected(selected: Session, selectedAgentId: string, activities: Session["activities"]) {
    return html`
      <section class="panel session-head-panel"><div class="session-head"><div><p class="eyebrow">Selected conversation</p><p class="session-id">${selected.id}</p></div></div><div class="session-metrics" aria-label="Selected conversation usage">
        <am-kpi-card label="Total tokens" .value=${formatOptionalNumber(selected.tokens.total)} hint="Input + output"></am-kpi-card>
        <am-kpi-card label="Input tokens" .value=${formatOptionalNumber(selected.tokens.input)} hint="Reported by model calls"></am-kpi-card>
        <am-kpi-card label="Output tokens" .value=${formatOptionalNumber(selected.tokens.output)} hint="Reported by model calls"></am-kpi-card>
        <am-kpi-card label="Estimated cost" .value=${formatCost(selected.costUsd)} .hint=${selected.costUsd === undefined ? "Not reported" : "Observed telemetry"}></am-kpi-card>
      </div></section>
      <section class="panel traffic-panel"><h2>Observed model traffic</h2><am-token-chart .usage=${selected.tokens}></am-token-chart></section>
      <section class="panel topology-panel"><h2>Agent topology</h2><am-agent-tree .agents=${selected.agents} .selectedAgentId=${selectedAgentId} @agent-selected=${this.agentSelected}></am-agent-tree></section>
      ${this.renderOperations(selected, selectedAgentId, activities)}
    `;
  }

  private renderOperations(selected: Session, selectedAgentId: string, activities: Session["activities"]) {
    const activityPage = this.conversations.activityPage;
    const selectedAgent = selected.agents.find(({ agentId }) => agentId === selectedAgentId);
    const agentPage = this.conversations.agentActivityPage?.sessionId === selected.id && this.conversations.agentActivityPage.agentId === selectedAgentId ? this.conversations.agentActivityPage : undefined;
    return html`<section class="panel operations-panel"><div class="operations-heading"><h2>Operations & messages</h2>${selectedAgent ? html`<div class="agent-filter"><span>Filtered by</span><strong>${agentDisplayLabel(selectedAgent)}</strong><button type="button" @click=${this.clearAgentSelection}>All agents</button></div>` : null}</div><am-activity-table
      .activities=${activities}
      .hasEarlier=${selectedAgentId ? agentPage?.hasEarlier ?? false : selected.hasEarlier ?? false}
      .hasMore=${selectedAgentId ? agentPage?.hasMore ?? false : selected.hasMore ?? selected.activities.length < selected.activityCount}
      .loading=${selectedAgentId ? agentPage?.loading ?? true : activityPage?.loading ?? false}
      .pageDirection=${selectedAgentId ? (agentPage?.loading ? "older" : "") : activityPage?.direction ?? ""}
      .loadError=${selectedAgentId ? agentPage?.error ?? "" : activityPage?.error ?? ""}
      .highlightedSpanId=${this.conversations.highlightedSpanId}
      .highlightedTraceId=${this.conversations.highlightedTraceId}
      .pagingContext=${`${selected.sourceId}:${selected.id}:${selectedAgentId}`}
      @activities-needed=${this.activitiesNeeded}
    ></am-activity-table></section>`;
  }

  private agentSelected(event: CustomEvent<{ agentId: string }>) { this.conversations.selectAgent(event.detail.agentId); }
  private readonly clearAgentSelection = () => this.conversations.selectAgent("");
  private readonly retryConversation = () => this.conversations.refreshSelected();
  private activitiesNeeded(event: CustomEvent<{ direction: ActivityDirection }>) {
    if (this.conversations.selectedAgentId) void this.conversations.loadAgentActivities(event.detail.direction);
    else void this.conversations.loadActivities(event.detail.direction);
  }
}

const formatOptionalNumber = (value?: number | null) => value === undefined || value === null ? NOT_REPORTED : value.toLocaleString();
const formatCost = (value?: number) => value === undefined ? NOT_REPORTED : new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: 4 }).format(value);

declare global { interface HTMLElementTagNameMap { "am-conversation-workspace": ConversationWorkspace } }
