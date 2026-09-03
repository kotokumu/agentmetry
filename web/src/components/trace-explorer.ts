import { LitElement, css, html, type PropertyValues } from "lit";
import { customElement, property } from "lit/decorators.js";
import "./trace-participants";
import "./trace-summary";
import "./trace-waterfall";
import "./trace-overview";
import { agentmetryClient } from "../api/agentmetry-client";
import { TraceController } from "../controllers/trace-controller";
import type { ConversationTarget } from "../model/trace-analysis";
import { featurePanelStyles } from "./feature-styles";
import { LIVE_UPDATE_EVENT, type LiveUpdateDelivery } from "../controllers/live-update-controller";
import { parseTraceInvestigationState, type TraceInvestigationState } from "../model/trace-investigation";
import type { Activity } from "../model/telemetry";

@customElement("am-trace-explorer")
export class TraceExplorer extends LitElement {
  @property() traceId = "";
  @property() anchorSpanId = "";
  @property({ attribute: false }) requestedInvestigation?: TraceInvestigationState;
  @property() returnHref = "/";
  @property() returnLabel = "Conversations";
  @property({ attribute: false }) locationForConversation?: (target: ConversationTarget) => string;
  private readonly trace = new TraceController(this, agentmetryClient);
  private lastReadyTraceId = "";
  private investigation: TraceInvestigationState = {};

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
    .trace-view { display: grid; gap: 12px; }
    .trace-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 14px; }
    .trace-title { margin: 0; font-size: 1rem; }
    .trace-close { border: 1px solid var(--am-border); border-radius: 8px; background: var(--am-surface-raised); color: var(--am-text); padding: 8px 13px; cursor: pointer; text-decoration: none; }
    .trace-close:hover, .trace-close:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    .trace-state { min-height: 220px; display: grid; place-items: center; color: var(--am-muted); }
  `];

  protected willUpdate(changed: PropertyValues<this>) {
    if (!changed.has("traceId") && !changed.has("anchorSpanId") && !changed.has("requestedInvestigation")) return;
    const restored = this.requestedInvestigation === undefined ? undefined : parseTraceInvestigationState(this.requestedInvestigation);
    this.investigation = restored ?? (this.requestedInvestigation === undefined && this.anchorSpanId ? { selectedSpanId: this.anchorSpanId } : {});
    if (this.traceId) this.trace.open(this.traceId, this.anchorSpanId, this.investigation);
    else this.trace.close();
  }

  render() {
    const trace = this.trace.value;
    return html`<section class="trace-view">
      <div class="panel trace-toolbar">
        <div><p class="eyebrow">Cross-conversation causality</p><h1 class="trace-title" tabindex="-1">Trace explorer</h1></div>
        <a class="trace-close" href=${this.returnHref} @click=${this.closeRequested}>← ${this.returnLabel}</a>
      </div>
      ${this.trace.loading ? html`<div class="panel trace-state" role="status">Loading trace evidence…</div>` : null}
      ${this.trace.failed ? html`<div class="panel trace-state error" role="alert">${this.anchorSpanId ? `Requested span ${this.anchorSpanId} is unavailable. ` : ""}${String(this.trace.error ?? "Trace unavailable")}</div>` : null}
      ${trace ? html`
        <section class="panel"><am-trace-summary .trace=${trace}></am-trace-summary></section>
        <section class="panel"><h2>Participants</h2><am-trace-participants .trace=${trace}></am-trace-participants></section>
        <section class="panel"><h2>Investigate trace window</h2><am-trace-overview
          .overview=${this.trace.overview}
          .investigation=${this.investigation}
          .matchingActivities=${this.trace.matchingActivities}
          .overviewState=${this.trace.overviewState}
          .windowState=${this.trace.windowState}
          .overviewError=${this.trace.overviewError ?? ""}
          .windowError=${this.trace.windowError ?? ""}
          @trace-investigation-requested=${this.investigationRequested}
        ></am-trace-overview></section>
        <section class="panel"><h2>Span & event timeline</h2><am-trace-waterfall
          .trace=${trace}
          .overview=${this.trace.overview}
          .selectedSpanId=${this.investigation.selectedSpanId ?? ""}
          .selectedActivity=${this.trace.selectedActivity}
          .selectedAvailability=${this.selectedAvailability(trace.activities)}
          .hasMore=${trace.hasMore}
          .loading=${this.trace.loadingPage}
          .locationForConversation=${this.locationForConversation}
          .onLoadMore=${this.traceActivitiesNeeded}
          @trace-activities-needed=${this.traceActivitiesNeeded}
          @trace-evidence-selected=${this.evidenceSelected}
          @trace-selection-cleared=${this.selectionCleared}
          @trace-selection-show-requested=${this.selectionShowRequested}
        ></am-trace-waterfall></section>
      ` : null}
    </section>`;
  }

  protected updated() {
    const traceId = this.trace.value?.traceId ?? "";
    if (!traceId || traceId === this.lastReadyTraceId) return;
    this.lastReadyTraceId = traceId;
    this.dispatchEvent(new CustomEvent("trace-view-ready", { bubbles: true, composed: true }));
  }

  private readonly traceActivitiesNeeded = () => { void this.trace.loadMore(); };
  private readonly liveUpdate = (event: CustomEvent<LiveUpdateDelivery>) => {
    event.detail.waitUntil(this.trace.applyLiveUpdate(event.detail).then(() => {
      const traceId = this.trace.takeRemovedTrace();
      if (traceId) this.dispatchEvent(new CustomEvent("trace-removed", { detail: { traceId }, bubbles: true, composed: true }));
    }));
  };

  focusRouteHeading() {
    this.shadowRoot?.querySelector<HTMLElement>(".trace-title")?.focus({ preventScroll: true });
  }

  get viewReady() { return Boolean(this.trace.value); }

  get navigationViewState() { return { traceInvestigation: this.investigation }; }

  private readonly investigationRequested = (event: CustomEvent<{ investigation: TraceInvestigationState }>) => {
    this.applyInvestigation(event.detail.investigation);
  };

  private readonly evidenceSelected = (event: CustomEvent<{ spanId: string }>) => {
    this.applyInvestigation({ ...this.investigation, selectedSpanId: event.detail.spanId });
  };

  private readonly selectionCleared = () => {
    const { selectedSpanId: _selected, ...investigation } = this.investigation;
    this.applyInvestigation(investigation);
  };

  private readonly selectionShowRequested = () => {
    const selected = this.trace.selectedActivity;
    if (!selected) {
      const { startedAt: _start, endedAt: _end, kind: _kind, errorsOnly: _errors, ...investigation } = this.investigation;
      this.applyInvestigation(investigation);
      return;
    }
    const startedAt = selected.startedAt ?? selected.observedAt;
    const endedAt = selected.endedAt ?? selected.observedAt;
    this.applyInvestigation({ ...this.investigation, startedAt, endedAt, kind: selected.kind, errorsOnly: undefined });
  };

  private applyInvestigation(investigation: TraceInvestigationState) {
    this.investigation = investigation;
    this.trace.setInvestigation(investigation);
    this.requestUpdate();
    this.dispatchEvent(new CustomEvent("trace-view-state-changed", {
      detail: { traceInvestigation: investigation }, bubbles: true, composed: true,
    }));
  }

  private selectedAvailability(activities: readonly Activity[]): "loaded" | "outside_filters" | "not_loaded" {
    const selectedSpanId = this.investigation.selectedSpanId;
    if (!selectedSpanId || activities.some((activity) => activity.signal === "trace" && activity.spanId === selectedSpanId)) return "loaded";
    const overviewActivity = this.trace.overview?.activities.find((activity) => activity.signal === "trace" && activity.spanId === selectedSpanId);
    const selected = overviewActivity ?? this.trace.selectedActivity;
    return selected && !activityMatchesInvestigation(selected, this.investigation) ? "outside_filters" : "not_loaded";
  }

  private readonly closeRequested = (event: MouseEvent) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    this.dispatchEvent(new CustomEvent("trace-close-requested", { bubbles: true, composed: true }));
  };
}

const activityMatchesInvestigation = (activity: Readonly<{ kind: Activity["kind"]; status?: string; startedAt?: string; endedAt?: string; observedAt?: string }>, investigation: TraceInvestigationState) => {
  if (investigation.kind && activity.kind !== investigation.kind) return false;
  if (investigation.errorsOnly && activity.status?.toLowerCase() !== "error") return false;
  if (!investigation.startedAt || !investigation.endedAt) return true;
  const start = Date.parse(activity.startedAt ?? activity.observedAt ?? "");
  const end = Date.parse(activity.endedAt ?? activity.observedAt ?? "");
  return end >= Date.parse(investigation.startedAt) && start <= Date.parse(investigation.endedAt);
};

declare global { interface HTMLElementTagNameMap { "am-trace-explorer": TraceExplorer } }
