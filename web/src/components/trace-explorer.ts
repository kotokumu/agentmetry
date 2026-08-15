import { LitElement, css, html, type PropertyValues } from "lit";
import { customElement, property } from "lit/decorators.js";
import "./trace-participants";
import "./trace-summary";
import "./trace-waterfall";
import { agentmetryClient } from "../api/agentmetry-client";
import { TraceController } from "../controllers/trace-controller";
import { featurePanelStyles } from "./feature-styles";

@customElement("am-trace-explorer")
export class TraceExplorer extends LitElement {
  @property() traceId = "";
  private readonly trace = new TraceController(this, agentmetryClient);

  static styles = [featurePanelStyles, css`
    :host { display: block; }
    .trace-view { display: grid; gap: 12px; }
    .trace-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 14px; }
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
        <div><p class="eyebrow">Cross-conversation causality</p><h2>Trace explorer</h2></div>
        <a class="trace-close" href="/" @click=${this.closeRequested}>Back to conversation</a>
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
          .onLoadMore=${this.traceActivitiesNeeded}
          @trace-activities-needed=${this.traceActivitiesNeeded}
        ></am-trace-waterfall></section>
      ` : null}
    </section>`;
  }

  private readonly traceActivitiesNeeded = () => { void this.trace.loadMore(); };

  private readonly closeRequested = (event: MouseEvent) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    this.dispatchEvent(new CustomEvent("trace-close-requested", { bubbles: true, composed: true }));
  };
}

declare global { interface HTMLElementTagNameMap { "am-trace-explorer": TraceExplorer } }
