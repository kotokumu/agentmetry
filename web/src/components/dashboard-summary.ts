import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import "./kpi-card";
import "./plan-usage";
import { DashboardController } from "../controllers/dashboard-controller";
import { agentmetryClient } from "../api/agentmetry-client";
import type { TelemetrySource, TimeRange } from "../model/telemetry";
import { NOT_REPORTED, UNAVAILABLE } from "../presentation/missing-data";
import { featurePanelStyles } from "./feature-styles";
import { affectsOverview, LIVE_UPDATE_EVENT, type LiveUpdateWindow } from "../controllers/live-update-controller";

export type DashboardStateDetail = Readonly<{
  status: "loading" | "ready" | "failed";
  sources: readonly TelemetrySource[];
}>;

@customElement("am-dashboard-summary")
export class DashboardSummary extends LitElement {
  @property() range: TimeRange = "24h";
  @property() sourceId = "";
  @property() search = "";
  @property() conversationStatus: "loading" | "ready" | "failed" = "loading";
  @property({ type: Number }) conversationCount?: number;
  @property({ type: Number }) activityCount?: number;
  private readonly dashboard = new DashboardController(this, agentmetryClient, () => ({ range: this.range, sourceId: this.sourceId, search: this.search }));
  private lastStateKey = "";

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
    .kpis { display: grid; grid-template-columns: repeat(4, minmax(130px, 1fr)); gap: 10px; margin-bottom: 10px; }
    .plan-panel { display: grid; grid-template-columns: 116px minmax(0, 1fr); gap: 14px; align-items: start; margin-bottom: 10px; padding-top: 11px; padding-bottom: 11px; }
    .plan-panel h2 { margin: 2px 0 0; }
    @media (max-width: 640px) { .kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; } .plan-panel { grid-template-columns: 1fr; gap: 8px; } }
    @media (max-width: 340px) { .kpis { grid-template-columns: 1fr; } }
  `];

  render() {
    const value = this.dashboard.value;
    const placeholder = this.dashboard.failed ? UNAVAILABLE : "Loading…";
    const conversationPlaceholder = this.conversationStatus === "failed" ? UNAVAILABLE : "Loading…";
    return html`
      <section class="kpis" aria-label="Conversation overview">
        <am-kpi-card label="Conversations" .value=${this.conversationStatus === "ready" && this.conversationCount !== undefined ? String(this.conversationCount) : conversationPlaceholder}></am-kpi-card>
        <am-kpi-card label="Agents" .value=${value ? String(value.agentCount) : placeholder}></am-kpi-card>
        <am-kpi-card label="Activities" .value=${this.conversationStatus === "ready" && this.activityCount !== undefined ? String(this.activityCount) : conversationPlaceholder}></am-kpi-card>
        <am-kpi-card label="Observed model traffic" .value=${value ? formatOptionalNumber(value.tokens.total) : placeholder} hint="Input + output reported by model calls; not a plan quota"></am-kpi-card>
      </section>
      <section class="panel plan-panel"><h2>Plan limits</h2><am-plan-usage .snapshots=${value?.planUsage ?? []}></am-plan-usage></section>
    `;
  }

  protected updated() {
    const status = this.dashboard.failed ? "failed" : this.dashboard.loading ? "loading" : "ready";
    const sources = this.dashboard.value?.sources ?? this.dashboard.lastValue?.sources ?? [];
    const key = `${status}:${sources.map(({ id, label }) => `${id}\u0000${label}`).join("\u0001")}`;
    if (key === this.lastStateKey) return;
    this.lastStateKey = key;
    this.dispatchEvent(new CustomEvent<DashboardStateDetail>("dashboard-state-changed", { detail: { status, sources }, bubbles: true, composed: true }));
  }

  private readonly liveUpdate = (event: CustomEvent<LiveUpdateWindow>) => {
    if (event.detail.resyncRequired || affectsOverview(event.detail.targets)) this.dashboard.refresh();
  };
}

const formatOptionalNumber = (value?: number | null) => value === undefined || value === null ? NOT_REPORTED : value.toLocaleString();

declare global { interface HTMLElementTagNameMap { "am-dashboard-summary": DashboardSummary } }
