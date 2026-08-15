import { LitElement, css, html, type PropertyValues } from "lit";
import { customElement, property } from "lit/decorators.js";
import "./trace-participants";
import "./trace-summary";
import "./trace-waterfall";
import { agentmetryClient } from "../api/agentmetry-client";
import { TraceController } from "../controllers/trace-controller";
import type { ConversationTarget } from "../model/trace-analysis";
import { featurePanelStyles } from "./feature-styles";
import { LIVE_UPDATE_EVENT, type LiveUpdateWindow } from "../controllers/live-update-controller";

@customElement("am-trace-explorer")
export class TraceExplorer extends LitElement {
  @property() traceId = "";
  @property() returnHref = "/";
  @property() returnLabel = "Conversations";
  @property({ attribute: false }) locationForConversation?: (target: ConversationTarget) => string;
  private readonly trace = new TraceController(this, agentmetryClient);
  private lastReadyTraceId = "";

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
    if (!changed.has("traceId")) return;
    if (this.traceId) this.trace.open(this.traceId);
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
      ${this.trace.failed ? html`<div class="panel trace-state error" role="alert">${String(this.trace.error ?? "Trace unavailable")}</div>` : null}
      ${trace ? html`
        <section class="panel"><am-trace-summary .trace=${trace}></am-trace-summary></section>
        <section class="panel"><h2>Participants</h2><am-trace-participants .trace=${trace}></am-trace-participants></section>
        <section class="panel"><h2>Span & event timeline</h2><am-trace-waterfall
          .trace=${trace}
          .hasMore=${trace.hasMore}
          .loading=${this.trace.loadingPage}
          .locationForConversation=${this.locationForConversation}
          .onLoadMore=${this.traceActivitiesNeeded}
          @trace-activities-needed=${this.traceActivitiesNeeded}
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
  private readonly liveUpdate = (event: CustomEvent<LiveUpdateWindow>) => { void this.trace.applyLiveUpdate(event.detail); };

  focusRouteHeading() {
    this.shadowRoot?.querySelector<HTMLElement>(".trace-title")?.focus({ preventScroll: true });
  }

  get viewReady() { return Boolean(this.trace.value); }

  private readonly closeRequested = (event: MouseEvent) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    this.dispatchEvent(new CustomEvent("trace-close-requested", { bubbles: true, composed: true }));
  };
}

declare global { interface HTMLElementTagNameMap { "am-trace-explorer": TraceExplorer } }
