import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import { live } from "lit/directives/live.js";
import type { TraceInvestigationState, TraceOverview } from "../model/trace-investigation";
import type { Activity } from "../model/telemetry";

@customElement("am-trace-overview")
export class TraceOverviewPanel extends LitElement {
  @property({ attribute: false }) overview?: TraceOverview;
  @property({ attribute: false }) investigation: TraceInvestigationState = {};
  @property({ type: Number }) matchingActivities = 0;
  @property() overviewState: "loading" | "available" | "unsupported" | "failed" = "loading";
  @property() windowState: "loading" | "available" | "unsupported" = "loading";
  @property() overviewError = "";
  @property() windowError = "";

  static styles = css`
    :host { display: grid; gap: 12px; }
    .status { margin: 0; color: var(--am-muted); font-size: .78rem; }
    .status.error, .missing { color: var(--am-danger); }
    .controls { display: grid; grid-template-columns: repeat(2, minmax(160px, 1fr)) minmax(130px, auto); gap: 10px 14px; align-items: end; }
    label { display: grid; gap: 5px; color: var(--am-muted); font-size: .72rem; }
    select, button { min-height: 38px; border: 1px solid var(--am-border); border-radius: 7px; background: var(--am-surface-raised); color: var(--am-text); padding: 7px 10px; }
    input[type="range"] { width: 100%; min-height: 28px; accent-color: var(--am-accent); }
    input[type="checkbox"] { accent-color: var(--am-accent); }
    .check { display: flex; align-items: center; gap: 7px; min-height: 38px; }
    .buttons { display: flex; flex-wrap: wrap; gap: 7px; grid-column: 1 / -1; }
    button { cursor: pointer; }
    button:focus-visible, select:focus-visible, input:focus-visible { outline: 2px solid var(--am-accent); outline-offset: 2px; }
    .map { position: relative; min-height: 64px; overflow: hidden; border: 1px solid var(--am-border); border-radius: 7px; background: var(--am-track); }
    .mark { position: absolute; height: 5px; min-width: 2px; border-radius: 3px; background: var(--am-secondary); }
    .mark.error { background: var(--am-danger); }
    .mark.missing-parent { box-shadow: 0 0 0 1px var(--am-danger); }
    @media (max-width: 700px) { .controls { grid-template-columns: 1fr; } .buttons { grid-column: auto; } }
  `;

  render() {
    const overview = this.overview;
    const extent = overview ? extentMilliseconds(overview) : undefined;
    const chosenStart = this.investigation.startedAt ? Date.parse(this.investigation.startedAt) : extent?.start ?? 0;
    const chosenEnd = this.investigation.endedAt ? Date.parse(this.investigation.endedAt) : extent?.end ?? 0;
    const stateMessage = this.overviewState === "unsupported"
      ? this.overviewError || "Trace overview is unsupported by this server. The loaded evidence remains available."
      : this.overviewState === "failed"
        ? this.overviewError || "Trace overview is unavailable. The loaded evidence remains available."
        : this.overviewState === "loading" ? "Loading trace overview…" : "";
    return html`
      ${stateMessage ? html`<p class=${`status ${this.overviewState === "failed" ? "error" : ""}`} role=${this.overviewState === "failed" ? "alert" : "status"}>${stateMessage}</p>` : null}
      ${this.windowState === "unsupported" ? html`<p class="status">${this.windowError || "Bounded trace windows are unsupported by this server."}</p>` : null}
      ${overview ? html`
        <p class="status">Overview ${overview.returnedActivities.toLocaleString()} of ${overview.totalActivities.toLocaleString()} activities · ${overview.coverage === "complete" ? "complete coverage" : "partial retained coverage"} · ${this.matchingActivities.toLocaleString()} match the current window</p>
        <div class="map" role="img" aria-label=${`Trace overview from ${overview.startedAt} to ${overview.endedAt}`}>
          ${overview.activities.map((activity, index) => {
            const position = overviewPosition(activity.startedAt, activity.endedAt, extent!);
            return html`<span class=${`mark ${activity.status?.toLowerCase() === "error" ? "error" : ""} ${activity.missingParent ? "missing-parent" : ""}`}
              title=${`${activity.name}${activity.missingParent ? " · missing parent" : ""}`}
              style=${`left:${position.left}%;width:${position.width}%;top:${6 + (index % 7) * 8}px`}></span>`;
          })}
        </div>
        <div class="controls">
          <label>Window start
            <input aria-label="Trace window start" type="range" min=${extent!.start} max=${extent!.end} step="1" .value=${live(String(chosenStart))} @change=${this.rangeChanged}>
            <span>${new Date(chosenStart).toISOString()}</span>
          </label>
          <label>Window end
            <input aria-label="Trace window end" type="range" min=${extent!.start} max=${extent!.end} step="1" .value=${live(String(chosenEnd))} @change=${this.rangeChanged}>
            <span>${new Date(chosenEnd).toISOString()}</span>
          </label>
          <label>Activity kind
            <select aria-label="Trace activity kind" @change=${this.kindChanged}>
              <option value="">All kinds</option>${kinds.map((kind) => html`<option value=${kind}>${kind}</option>`)}
            </select>
          </label>
          <label class="check"><input type="checkbox" .checked=${live(this.investigation.errorsOnly ?? false)} @change=${this.errorsChanged}> Errors only</label>
          <div class="buttons"><button type="button" @click=${() => this.zoom(.5)}>Zoom in</button><button type="button" @click=${() => this.zoom(2)}>Zoom out</button><button type="button" @click=${this.reset}>Reset window</button></div>
        </div>
      ` : null}
    `;
  }

  protected updated() {
    const select = this.renderRoot.querySelector<HTMLSelectElement>('select[aria-label="Trace activity kind"]');
    if (select && select.value !== (this.investigation.kind ?? "")) select.value = this.investigation.kind ?? "";
  }

  private rangeChanged() {
    const inputs = [...this.renderRoot.querySelectorAll<HTMLInputElement>('input[type="range"]')];
    const start = Number(inputs[0]?.value);
    const end = Number(inputs[1]?.value);
    if (!Number.isFinite(start) || !Number.isFinite(end)) return;
    this.requestInvestigation({ ...this.investigation, startedAt: new Date(Math.min(start, end)).toISOString(), endedAt: new Date(Math.max(start, end)).toISOString() });
  }

  private kindChanged(event: Event) {
    const kind = (event.currentTarget as HTMLSelectElement).value as "" | Activity["kind"];
    this.requestInvestigation({ ...this.investigation, ...(kind ? { kind } : { kind: undefined }) });
  }

  private errorsChanged(event: Event) {
    this.requestInvestigation({ ...this.investigation, errorsOnly: (event.currentTarget as HTMLInputElement).checked || undefined });
  }

  private zoom(factor: number) {
    const overview = this.overview;
    if (!overview) return;
    const extent = extentMilliseconds(overview);
    const start = this.investigation.startedAt ? Date.parse(this.investigation.startedAt) : extent.start;
    const end = this.investigation.endedAt ? Date.parse(this.investigation.endedAt) : extent.end;
    const center = start + (end - start) / 2;
    const half = Math.max(1, Math.min(extent.end - extent.start, (end - start) * factor) / 2);
    const nextStart = Math.max(extent.start, center - half);
    const nextEnd = Math.min(extent.end, center + half);
    this.requestInvestigation({ ...this.investigation, startedAt: new Date(nextStart).toISOString(), endedAt: new Date(nextEnd).toISOString() });
  }

  private reset = () => {
    const { startedAt: _start, endedAt: _end, kind: _kind, errorsOnly: _errors, ...rest } = this.investigation;
    this.requestInvestigation(rest);
  };

  private requestInvestigation(investigation: TraceInvestigationState) {
    this.dispatchEvent(new CustomEvent("trace-investigation-requested", { detail: { investigation }, bubbles: true, composed: true }));
  }
}

const kinds: readonly Activity["kind"][] = ["unknown", "prompt", "response", "tool", "delegation", "message", "reasoning"];
const extentMilliseconds = (overview: TraceOverview) => {
  const start = Date.parse(overview.startedAt);
  return { start, end: Math.max(start + 1, Date.parse(overview.endedAt)) };
};
const overviewPosition = (startedAt: string, endedAt: string, extent: Readonly<{ start: number; end: number }>) => {
  const duration = extent.end - extent.start;
  const start = Math.max(extent.start, Math.min(extent.end, Date.parse(startedAt)));
  const end = Math.max(start, Math.min(extent.end, Date.parse(endedAt)));
  return { left: ((start - extent.start) / duration) * 100, width: Math.max(.15, ((end - start) / duration) * 100) };
};

declare global { interface HTMLElementTagNameMap { "am-trace-overview": TraceOverviewPanel } }
