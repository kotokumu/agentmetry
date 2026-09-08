import { css, html } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { agentmetryClient } from "../api/agentmetry-client";
import { ProjectionTargetKind } from "../gen/agentmetry/v1/agentmetry_pb";
import { LIVE_UPDATE_EVENT, type LiveUpdateDelivery } from "../controllers/live-update-controller";
import type { TraceCatalogConditions, TraceCatalogEntry, TraceCatalogFailure } from "../model/trace-catalog";
import type { TimeRange } from "../model/telemetry";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";
import { notReported } from "../presentation/missing-data";
import { costCoverageHint, formatCostSummary } from "../presentation/cost";

@customElement("am-trace-catalog")
export class TraceCatalog extends LocalizedElement {
  @property() range: TimeRange = "24h";
  @property() sourceId = "";
  @property({ type: Boolean }) active = false;
  @property({ attribute: false }) conditions: TraceCatalogConditions = {};
  @property({ attribute: false }) locationForTrace: (traceId: string) => string = (traceId) => `/traces/${encodeURIComponent(traceId)}`;
  @state() private traces: readonly TraceCatalogEntry[] = [];
  @state() private nextPageToken = "";
  @state() private loading = false;
  @state() private error = "";
  @state() private appliedConditions: TraceCatalogConditions = {};
  private loadedKey = "";
  private request = 0;
  private abort?: AbortController;

  static styles = css`
    :host { display: block; }
    .panel { padding: 16px; border: 1px solid var(--am-border); border-radius: 8px; background: var(--am-surface-raised); }
    h2 { margin: 0; font-size: 1rem; }
    .state { color: var(--am-muted); font-size: 14px; line-height: 1.5; overflow-wrap: anywhere; }
    .filters { display: grid; grid-template-columns: minmax(160px, 1fr) minmax(160px, 1fr) auto; gap: 10px; margin-top: 14px; }
    label { display: grid; gap: 5px; color: var(--am-muted); font-size: 12px; }
    select, input, button { min-height: 36px; border: 1px solid var(--am-border); border-radius: 7px; background: var(--am-surface); color: var(--am-text); padding: 7px 9px; font: inherit; }
    select:focus-visible, input:focus-visible, button:focus-visible { border-color: var(--am-accent); outline: 2px solid var(--am-accent-soft); outline-offset: 2px; }
    .scope { margin: 10px 0 0; color: var(--am-muted); font-size: 12px; line-height: 1.4; overflow-wrap: anywhere; }
    .trace-list { display: grid; gap: 8px; margin: 14px 0 0; padding: 0; list-style: none; }
    .trace-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 16px; padding: 12px; border: 1px solid var(--am-border); border-radius: 7px; background: var(--am-surface); }
    .trace-link { color: var(--am-accent); font: 14px/1.4 "SFMono-Regular", "Cascadia Code", monospace; overflow-wrap: anywhere; }
    .trace-link:hover, .trace-link:focus-visible { color: var(--am-text); }
    .meta { display: flex; flex-wrap: wrap; gap: 6px 14px; margin-top: 5px; color: var(--am-muted); font-size: 12px; }
    .status { color: var(--am-text); font-size: 14px; }
    .more { margin-top: 12px; border: 1px solid var(--am-border); border-radius: 7px; padding: 8px 12px; background: var(--am-surface); color: var(--am-text); cursor: pointer; }
    .more:hover, .more:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    @media (max-width: 640px) { .filters { grid-template-columns: 1fr; } .trace-row { grid-template-columns: 1fr; } }
  `;

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener(LIVE_UPDATE_EVENT, this.liveUpdate as EventListener);
    if (this.active) this.requestUpdate();
  }

  disconnectedCallback() {
    window.removeEventListener(LIVE_UPDATE_EVENT, this.liveUpdate as EventListener);
    this.abort?.abort();
    this.abort = undefined;
    this.request += 1;
    this.loadedKey = "";
    this.loading = false;
    super.disconnectedCallback();
  }

  protected willUpdate() {
    const key = `${this.range}:${this.sourceId}:${JSON.stringify(this.conditions)}`;
    if (!this.active) {
      this.abort?.abort();
      this.abort = undefined;
      this.request += 1;
      this.loadedKey = "";
      this.loading = false;
      return;
    }
    if (key === this.loadedKey) return;
    this.loadedKey = key;
    this.abort?.abort();
    this.abort = new AbortController();
    this.request += 1;
    this.traces = [];
    this.nextPageToken = "";
    this.appliedConditions = {};
    void this.load(true);
  }

  private async load(reset: boolean) {
    const request = ++this.request;
    const abort = this.abort;
    this.loading = true;
    this.error = "";
    try {
      const page = await agentmetryClient.listTraces(this.range, this.sourceId, reset ? "" : this.nextPageToken, abort?.signal, this.conditions);
      if (request !== this.request || abort?.signal.aborted || !this.active) return;
      this.traces = reset ? page.traces : [...this.traces, ...page.traces];
      this.nextPageToken = page.nextPageToken ?? "";
      this.appliedConditions = page.appliedConditions;
    } catch (error) {
      if (request === this.request && !abort?.signal.aborted && this.active) this.error = error instanceof Error ? error.message : localization.t("app.tracesUnavailable");
    } finally {
      if (request === this.request && this.active) this.loading = false;
    }
  }

  render() {
    return html`<section class="panel" aria-labelledby="trace-catalog-heading"><h2 id="trace-catalog-heading">${localization.t("app.traces")}</h2>
      <div class="filters">
        <label>${localization.t("traceCatalog.failureCondition")}<select data-filter="failure" .value=${this.conditions.failureObservation ?? ""} @change=${this.conditionChanged}>
          <option value="">${localization.t("traceCatalog.allFailures")}</option>
          <option value="observed">${localization.t("traceCatalog.failureObserved")}</option>
          <option value="not_observed">${localization.t("traceCatalog.failureNotObserved")}</option>
          <option value="not_reported">${localization.t("traceCatalog.failureNotReported")}</option>
        </select></label>
        <label>${localization.t("traceCatalog.minimumDuration")}<input data-filter="duration" type="number" min="0" step="any" inputmode="decimal" .value=${this.conditions.minDurationMs === undefined ? "" : String(this.conditions.minDurationMs)} @change=${this.conditionChanged}></label>
      </div>
      ${hasTraceConditions(this.appliedConditions) ? html`<p class="scope">${localization.t("traceCatalog.conditionsApplied")}</p>` : null}
      ${this.error ? html`<p class="state" role="alert">${localization.t("app.tracesUnavailable")} ${this.error}</p>`
        : this.loading && !this.traces.length ? html`<p class="state" role="status">${localization.t("app.loadingTraces")}</p>`
          : this.traces.length ? html`<ul class="trace-list">${this.traces.map((trace) => this.renderTrace(trace))}</ul>`
            : html`<p class="state">${localization.t("app.noTraces")}</p>`}
      ${this.nextPageToken ? html`<button class="more" type="button" ?disabled=${this.loading} @click=${() => void this.load(false)}>${this.loading ? localization.t("workspace.loadingMore") : localization.t("workspace.loadMore")}</button>` : null}
    </section>`;
  }

  private renderTrace(trace: TraceCatalogEntry) {
    const duration = trace.durationMs === undefined ? notReported() : `${localization.number(trace.durationMs)} ms`;
    const sessions = trace.conversations.length ? trace.conversations.map(({ sourceId, id }) => `${sourceId}/${id}`).join(", ") : notReported();
    return html`<li class="trace-row"><div><a class="trace-link" href=${this.locationForTrace(trace.traceId)} @click=${(event: MouseEvent) => this.traceSelected(event, trace)}>${trace.traceId}</a><div class="meta"><span>${trace.startedAt || notReported()}</span><span>${duration}</span><span>${localization.t("traceCatalog.activities", { count: localization.number(trace.activityCount) })}</span><span>${sessions}</span><span>${formatCostSummary(trace.costSummary)} · ${costCoverageHint(trace.costSummary)}</span></div></div><span class="status">${trace.status}</span></li>`;
  }

  private readonly conditionChanged = (event: Event) => {
    const control = event.currentTarget as HTMLInputElement | HTMLSelectElement;
    const next: { failureObservation?: TraceCatalogFailure; minDurationMs?: number } = { ...this.conditions };
    if (control.dataset.filter === "failure") {
      const value = control.value as TraceCatalogFailure | "";
      if (value) next.failureObservation = value;
      else delete next.failureObservation;
    } else {
      if (control instanceof HTMLInputElement && !control.checkValidity()) {
        control.reportValidity();
        return;
      }
      const value = control.value.trim();
      if (value) {
        const number = Number(value);
        if (!Number.isFinite(number) || number < 0) return;
        next.minDurationMs = number;
      } else delete next.minDurationMs;
    }
    this.dispatchEvent(new CustomEvent("trace-conditions-requested", { detail: { conditions: next }, bubbles: true, composed: true }));
  };

  private readonly traceSelected = (event: MouseEvent, trace: TraceCatalogEntry) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    this.dispatchEvent(new CustomEvent("trace-catalog-selected", { detail: { traceId: trace.traceId }, bubbles: true, composed: true }));
  };

  private readonly liveUpdate = (event: CustomEvent<LiveUpdateDelivery>) => {
    if (!this.active || (!event.detail.resyncRequired && !affectsTraceCatalog(event.detail.targets))) return;
    event.detail.waitUntil(this.reloadAfterLiveUpdate());
  };

  private async reloadAfterLiveUpdate() {
    this.abort?.abort();
    this.abort = undefined;
    this.request += 1;
    this.loadedKey = "";
    this.traces = [];
    this.nextPageToken = "";
    this.appliedConditions = {};
    this.error = "";
    if (!this.active) return;
    this.loadedKey = `${this.range}:${this.sourceId}:${JSON.stringify(this.conditions)}`;
    this.abort = new AbortController();
    await this.load(true);
  };
}

const affectsTraceCatalog = (targets: LiveUpdateDelivery["targets"]) => targets.some(({ kind }) =>
  kind === ProjectionTargetKind.ALL_TRACES || kind === ProjectionTargetKind.TRACE
    || kind === ProjectionTargetKind.ALL_SOURCES || kind === ProjectionTargetKind.SOURCE,
);

const hasTraceConditions = (conditions: TraceCatalogConditions) => conditions.failureObservation !== undefined || conditions.minDurationMs !== undefined;

declare global { interface HTMLElementTagNameMap { "am-trace-catalog": TraceCatalog } }
