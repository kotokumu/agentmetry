import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import {
  COMPARISON_DISPLAY_DECIMALS,
  type ComparisonMetricID,
  type ComparisonUnit,
  type ComparisonValue,
  type ReworkComparisonRow,
  type ReworkComparisonViewState,
} from "../model/rework-comparison";
import { NOT_REPORTED } from "../presentation/missing-data";
import { featurePanelStyles } from "./feature-styles";

export type ComparisonBaselineSelectedDetail = Readonly<{ sessionId: string }>;

@customElement("am-rework-comparison")
export class ReworkComparison extends LitElement {
  @property({ attribute: false }) state: ReworkComparisonViewState = { status: "empty" };

  static styles = [featurePanelStyles, css`
    :host { display: block; min-width: 0; }
    .heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 13px; }
    .heading h2 { margin-bottom: 4px; }
    .heading p, .scope, .qualification, .state p, .warnings { margin: 0; color: var(--am-muted); font-size: .72rem; line-height: 1.5; }
    .baseline-control { display: grid; gap: 5px; min-width: min(100%, 260px); color: var(--am-muted); font: 700 .62rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .04em; text-transform: uppercase; }
    select { min-width: 0; border: 1px solid var(--am-border); border-radius: 8px; padding: 7px 28px 7px 9px; background: var(--am-surface-raised); color: var(--am-text); font: 600 .7rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-transform: none; }
    select:hover, select:focus-visible, .retry:hover, .retry:focus-visible { border-color: var(--am-accent); outline: 2px solid var(--am-accent-soft); outline-offset: 2px; }
    .table-wrap { overflow-x: auto; border: 1px solid var(--am-border); border-radius: 10px; }
    table { width: 100%; border-collapse: collapse; min-width: 690px; font-size: .72rem; }
    caption { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
    th, td { padding: 10px 12px; border-bottom: 1px solid var(--am-border); text-align: right; vertical-align: top; }
    thead th { color: var(--am-muted); background: rgba(255,255,255,.02); font: 700 .62rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .05em; text-transform: uppercase; }
    th:first-child, td:first-child { text-align: left; }
    tbody tr:last-child th, tbody tr:last-child td { border-bottom: 0; }
    .metric-label { display: flex; align-items: flex-start; gap: 7px; color: var(--am-text); }
    .value { color: var(--am-text); font: 700 .72rem/1.25 "SFMono-Regular", "Cascadia Code", monospace; }
    .evidence { display: block; margin-top: 3px; color: var(--am-muted); font-size: .62rem; line-height: 1.35; }
    .change { white-space: nowrap; font-weight: 700; }
    .change.improved { color: #7ee2ad; }
    .change.regressed { color: #ff9a91; }
    .change.unchanged, .change.unavailable { color: var(--am-muted); }
    .direction { display: block; margin-top: 3px; font-size: .62rem; }
    .metric-help { position: relative; flex: 0 0 auto; }
    .metric-help summary { display: grid; place-items: center; width: 17px; height: 17px; border: 1px solid var(--am-border); border-radius: 50%; color: var(--am-muted); cursor: help; font: 800 .62rem/1 sans-serif; list-style: none; }
    .metric-help summary::-webkit-details-marker { display: none; }
    .metric-help summary:hover, .metric-help summary:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    .metric-help p { display: none; position: absolute; z-index: 5; top: 22px; left: 0; width: min(300px, 70vw); margin: 0; border: 1px solid var(--am-border); border-radius: 8px; padding: 9px 10px; background: var(--am-surface-raised); box-shadow: 0 12px 30px rgba(0,0,0,.35); color: var(--am-text); font-size: .68rem; font-weight: 400; line-height: 1.45; }
    .metric-help[open] p, .metric-help:hover p, .metric-help:focus-within p { display: block; }
    .warnings { margin-top: 9px; color: #ffc77d; }
    .qualification { margin-top: 9px; }
    .scope { margin-top: 6px; }
    .state { border: 1px dashed var(--am-border); border-radius: 10px; padding: 14px; }
    .state strong { display: block; margin-bottom: 4px; color: var(--am-text); }
    .retry { margin-top: 10px; border: 1px solid var(--am-border); border-radius: 7px; padding: 7px 11px; background: var(--am-surface-raised); color: var(--am-text); cursor: pointer; font: inherit; }
    @media (max-width: 700px) { .heading { display: block; } .baseline-control { margin-top: 12px; width: 100%; } }
  `];

  render() {
    return html`<section class="panel">
      <div class="heading">
        <div><h2>Before / After diagnostics</h2><p>Compare normalized diagnostic signals between two same-source conversations.</p></div>
        ${this.state.status !== "empty" ? html`<label class="baseline-control">Baseline conversation
          <select .value=${this.state.selectedBaselineId} @change=${this.baselineSelected}>
            ${this.state.options.map((option) => html`<option value=${option.sessionId} title=${option.sessionId}>${shortId(option.sessionId)} · ended ${formatTimestamp(option.endedAt)}</option>`)}
          </select>
        </label>` : null}
      </div>
      ${this.renderBody()}
      <p class="scope">Baseline candidates are limited to non-overlapping conversations in the current filtered list.</p>
    </section>`;
  }

  private renderBody() {
    switch (this.state.status) {
      case "empty":
        return html`<div class="state"><strong>No non-overlapping baseline in the current conversation list</strong><p>Broaden the time range or filters, or collect another completed conversation from this source.</p></div>`;
      case "loading":
        return html`<div class="state" role="status"><strong>Loading baseline diagnostics</strong><p>Reading the selected conversation summary and normalized rework evidence.</p></div>`;
      case "failed":
        return html`<div class="state" role="alert"><strong>Comparison unavailable</strong><p>${this.state.message}</p><button class="retry" type="button" @click=${this.retry}>Retry comparison</button></div>`;
      case "waiting":
        return html`<div class="state" role="status"><strong>Waiting for comparable diagnostics</strong><p>${this.state.message}</p></div>`;
      case "invalid":
        return html`<div class="state" role="alert"><strong>${invalidTitle(this.state.code)}</strong><p>${this.state.reason}</p></div>`;
      case "ready":
        return html`
          <div class="table-wrap"><table>
            <caption>Normalized diagnostic comparison between baseline and current conversations</caption>
            <thead><tr><th scope="col">Diagnostic</th><th scope="col">Baseline</th><th scope="col">Current</th><th scope="col">Change</th></tr></thead>
            <tbody>${this.state.rows.map((row) => this.renderRow(row))}</tbody>
          </table></div>
          ${this.state.warnings.length ? html`<p class="warnings">${this.state.warnings.join(" ")}</p>` : null}
          <p class="qualification">Observed differences are diagnostic signals, not causal evidence or a productivity score.</p>
        `;
    }
  }

  private renderRow(row: ReworkComparisonRow) {
    const descriptor = descriptors[row.id];
    const direction = row.availability === "comparable" ? row.direction : "unavailable";
    return html`<tr>
      <th scope="row"><span class="metric-label">${descriptor.label}<details class="metric-help"><summary aria-label=${`About ${descriptor.label}`} @click=${this.toggleHelp}>?</summary><p>${descriptor.description}</p></details></span></th>
      <td>${renderValue(row.baseline, row.unit, descriptor.evidence)}</td>
      <td>${renderValue(row.current, row.unit, descriptor.evidence)}</td>
      <td><span class=${`change ${direction}`}>${formatDelta(row)}<span class="direction">${directionLabel(direction)}</span></span></td>
    </tr>`;
  }

  private readonly baselineSelected = (event: Event) => {
    const detail: ComparisonBaselineSelectedDetail = { sessionId: (event.currentTarget as HTMLSelectElement).value };
    this.dispatchEvent(new CustomEvent<ComparisonBaselineSelectedDetail>("comparison-baseline-selected", { detail, bubbles: true, composed: true }));
  };
  private readonly retry = () => this.dispatchEvent(new CustomEvent("comparison-retry-requested", { bubbles: true, composed: true }));
  private readonly toggleHelp = (event: MouseEvent) => {
    event.preventDefault();
    const details = (event.currentTarget as HTMLElement).closest<HTMLDetailsElement>("details");
    if (details) details.open = !details.open;
  };
}

const descriptors: Record<ComparisonMetricID, Readonly<{ label: string; description: string; evidence: string }>> = {
  initial_validation_success_proxy: { label: "Initial validation success proxy", description: "First observed outcome for each same-agent operation, normalized command, and working-directory identity. It is not task- or change-level first-pass success.", evidence: "eligible identities" },
  rework_token_share: { label: "Rework token share", description: "Tokens attributed to detected closed failure/edit/retry windows divided by all observed session tokens. Lower is generally preferable when coverage is comparable.", evidence: "session tokens" },
  retry_cycle_effort_share: { label: "Detected retry-cycle effort share", description: "Agent-active duration intersecting detected closed retry-cycle windows divided by total observed agent-active duration.", evidence: "ms observed agent-active time" },
  tool_failure_rate: { label: "Tool failure rate", description: "Observed failed tool attempts divided by tool attempts with a known outcome. Missing outcomes are excluded from both sides.", evidence: "outcome-known tool attempts" },
  recurring_loops_per_100_validations: { label: "Recurring failure loops / 100 validations", description: "Confirmed same-command and same-error recurring episodes per 100 logical validation attempts with known outcomes.", evidence: "outcome-known validations" },
};

const renderValue = (value: ComparisonValue, unit: ComparisonUnit, evidence: string) => html`
  <span class="value">${value.availability === "unavailable" ? NOT_REPORTED : unit === "percent" ? `${value.displayValue.toFixed(COMPARISON_DISPLAY_DECIMALS)}%` : value.displayValue.toFixed(COMPARISON_DISPLAY_DECIMALS)}</span>
  <span class="evidence">${value.availability === "unavailable" ? value.reason : `${value.numerator.toLocaleString()} / ${value.denominator.toLocaleString()} ${evidence}`}</span>
`;
const formatDelta = (row: ReworkComparisonRow) => {
  if (row.availability === "unavailable") return NOT_REPORTED;
  const sign = row.delta > 0 ? "+" : "";
  return `${sign}${row.delta.toFixed(COMPARISON_DISPLAY_DECIMALS)} ${row.unit === "percent" ? "pp" : "per 100"}`;
};
const directionLabel = (direction: "improved" | "regressed" | "unchanged" | "unavailable") => ({ improved: "Improved", regressed: "Regressed", unchanged: "No change", unavailable: "Not comparable" })[direction];
const invalidTitle = (code: "identity_mismatch" | "invalid_time" | "baseline_ineligible") => ({
  identity_mismatch: "Comparison identities do not match",
  invalid_time: "Conversation times are invalid",
  baseline_ineligible: "Baseline is no longer eligible",
})[code];
const shortId = (value: string) => value.length > 16 ? `${value.slice(0, 16)}…` : value;
const formatTimestamp = (value: string) => {
  const date = new Date(value);
  return Number.isFinite(date.getTime()) ? date.toLocaleString() : value;
};

declare global { interface HTMLElementTagNameMap { "am-rework-comparison": ReworkComparison } }
