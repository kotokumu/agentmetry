import { css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import {
  COMPARISON_DISPLAY_DECIMALS,
  roundComparisonDisplay,
  type ComparisonMetricID,
  type ComparisonUnit,
  type ComparisonValue,
  type HarnessComparisonIssue,
  type HarnessRelationship,
  type ReworkComparisonRow,
  type ReworkComparisonViewState,
} from "../model/rework-comparison";
import { notReported, unavailable } from "../presentation/missing-data";
import { featurePanelStyles } from "./feature-styles";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";
import type { MessageKey } from "../localization/messages";

export type ComparisonBaselineSelectedDetail = Readonly<{ sessionId: string }>;

@customElement("am-rework-comparison")
export class ReworkComparison extends LocalizedElement {
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
    .harness-context { margin-bottom: 12px; border: 1px solid var(--am-border); border-radius: 10px; padding: 11px 12px; background: rgba(255,255,255,.015); }
    .harness-heading { display: flex; align-items: center; gap: 7px; color: var(--am-text); font-size: .72rem; font-weight: 750; }
    .harness-pair { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-top: 8px; }
    .harness-side { min-width: 0; border-left: 2px solid var(--am-border); padding-left: 8px; color: var(--am-muted); font-size: .64rem; line-height: 1.45; }
    .harness-side strong { display: block; overflow: hidden; color: var(--am-text); font: 700 .68rem/1.35 "SFMono-Regular", "Cascadia Code", monospace; text-overflow: ellipsis; white-space: nowrap; }
    .harness-reason { margin: 7px 0 0; color: #ffc77d; font-size: .64rem; line-height: 1.45; }
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
        <div><h2>${localization.t("comparison.title")}</h2><p>${localization.t("comparison.subtitle")}</p></div>
        ${this.state.status !== "empty" ? html`<label class="baseline-control">${localization.t("comparison.baselineConversation")}
          <select .value=${this.state.selectedBaselineId} @change=${this.baselineSelected}>
            ${this.state.options.map((option) => html`<option value=${option.sessionId} title=${option.sessionId}>${shortId(option.sessionId)} · ${localization.t("comparison.ended", { time: formatTimestamp(option.endedAt) })}</option>`)}
          </select>
        </label>` : null}
      </div>
      ${this.renderBody()}
      <p class="scope">${localization.t("comparison.scope")}</p>
    </section>`;
  }

  private renderBody() {
    switch (this.state.status) {
      case "empty":
        return html`<div class="state"><strong>${localization.t("comparison.noBaseline")}</strong><p>${localization.t("comparison.noBaselineHelp")}</p></div>`;
      case "loading":
        return html`<div class="state" role="status"><strong>${localization.t("comparison.loading")}</strong><p>${localization.t("comparison.loadingHelp")}</p></div>`;
      case "failed":
        return html`<div class="state" role="alert"><strong>${localization.t("comparison.unavailable")}</strong><p>${this.state.message}</p><button class="retry" type="button" @click=${this.retry}>${localization.t("comparison.retry")}</button></div>`;
      case "waiting":
        return html`<div class="state" role="status"><strong>${localization.t("comparison.waiting")}</strong><p>${this.state.message}</p></div>`;
      case "invalid":
        return html`<div class="state" role="alert"><strong>${invalidTitle(this.state.code)}</strong><p>${this.state.reason}</p></div>`;
      case "ready":
        return html`
          ${this.renderHarness(this.state.harness)}
          <div class="table-wrap"><table>
            <caption>${localization.t("comparison.caption")}</caption>
            <thead><tr><th scope="col">${localization.t("comparison.diagnostic")}</th><th scope="col">${localization.t("comparison.baseline")}</th><th scope="col">${localization.t("comparison.current")}</th><th scope="col">${localization.t("comparison.change")}</th></tr></thead>
            <tbody>${this.state.rows.map((row) => this.renderRow(row))}</tbody>
          </table></div>
          ${this.state.warnings.length ? html`<p class="warnings">${this.state.warnings.join(" ")}</p>` : null}
          <p class="qualification">${localization.t("comparison.qualification")}</p>
        `;
    }
  }

  private renderHarness(relationship: HarnessRelationship) {
    const title = relationship.status === "reported_changed"
      ? localization.t("comparison.harnessChanged")
      : relationship.status === "reported_same"
        ? localization.t("comparison.harnessSame")
        : localization.t("comparison.harnessNotComparable");
    const issues = relationship.status === "not_comparable"
      ? [
        relationship.baselineIssue ? localization.t("comparison.baselineIssue", { issue: harnessIssueLabel(relationship.baselineIssue) }) : "",
        relationship.currentIssue ? localization.t("comparison.currentIssue", { issue: harnessIssueLabel(relationship.currentIssue) }) : "",
        relationship.relationshipIssue === "scope_mismatch" ? localization.t("comparison.scopeMismatch") : "",
      ].filter(Boolean).join(" ")
      : "";
    return html`<div class="harness-context">
      <div class="harness-heading">${title}<details class="metric-help"><summary aria-label=${localization.t("comparison.aboutHarness")} @click=${this.toggleHelp}>?</summary><p>${localization.t("comparison.harnessHelp")}</p></details></div>
      <div class="harness-pair">
        ${renderHarnessSide(localization.t("comparison.baseline"), relationship.baseline)}
        ${renderHarnessSide(localization.t("comparison.current"), relationship.current)}
      </div>
      ${issues ? html`<p class="harness-reason">${issues}</p>` : null}
    </div>`;
  }

  private renderRow(row: ReworkComparisonRow) {
    const descriptor = descriptors[row.id];
    const label = localization.t(descriptor.label);
    const direction = row.availability === "comparable" ? row.direction : "unavailable";
    return html`<tr>
      <th scope="row"><span class="metric-label">${label}<details class="metric-help"><summary aria-label=${localization.t("comparison.aboutMetric", { label })} @click=${this.toggleHelp}>?</summary><p>${localization.t(descriptor.description)}</p></details></span></th>
      <td>${renderValue(row.baseline, row.unit, localization.t(descriptor.evidence))}</td>
      <td>${renderValue(row.current, row.unit, localization.t(descriptor.evidence))}</td>
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

const descriptors: Record<ComparisonMetricID, Readonly<{ label: MessageKey; description: MessageKey; evidence: MessageKey }>> = {
  initial_validation_success_proxy: { label: "comparison.initialLabel", description: "comparison.initialDescription", evidence: "comparison.initialEvidence" },
  rework_token_share: { label: "comparison.tokenLabel", description: "comparison.tokenDescription", evidence: "comparison.tokenEvidence" },
  retry_cycle_effort_share: { label: "comparison.effortLabel", description: "comparison.effortDescription", evidence: "comparison.effortEvidence" },
  tool_failure_rate: { label: "comparison.toolLabel", description: "comparison.toolDescription", evidence: "comparison.toolEvidence" },
  recurring_loops_per_100_validations: { label: "comparison.loopLabel", description: "comparison.loopDescription", evidence: "comparison.loopEvidence" },
};

const renderValue = (value: ComparisonValue, unit: ComparisonUnit, evidence: string) => html`
  <span class="value">${value.availability === "unavailable" ? notReported() : unit === "percent" ? `${localization.number(value.displayValue, { minimumFractionDigits: COMPARISON_DISPLAY_DECIMALS, maximumFractionDigits: COMPARISON_DISPLAY_DECIMALS })}%` : localization.number(value.displayValue, { minimumFractionDigits: COMPARISON_DISPLAY_DECIMALS, maximumFractionDigits: COMPARISON_DISPLAY_DECIMALS })}</span>
  <span class="evidence">${value.availability === "unavailable" ? value.reason : `${localization.number(value.numerator)} / ${localization.number(value.denominator)} ${evidence}`}</span>
`;
const formatDelta = (row: ReworkComparisonRow) => {
  if (row.availability === "unavailable") return notReported();
  const displayed = roundComparisonDisplay(row.delta);
  const sign = displayed > 0 ? "+" : "";
  return `${sign}${localization.number(displayed, { minimumFractionDigits: COMPARISON_DISPLAY_DECIMALS, maximumFractionDigits: COMPARISON_DISPLAY_DECIMALS })} ${row.unit === "percent" ? "pp" : localization.t("comparison.per100")}`;
};
const directionLabel = (direction: "improved" | "regressed" | "unchanged" | "unavailable") => localization.t(({ improved: "comparison.improved", regressed: "comparison.regressed", unchanged: "comparison.noChange", unavailable: "comparison.notComparable" } as const)[direction]);
const invalidTitle = (code: "identity_mismatch" | "invalid_time" | "baseline_ineligible") => ({
  identity_mismatch: localization.t("comparison.identityMismatch"),
  invalid_time: localization.t("comparison.invalidTime"),
  baseline_ineligible: localization.t("comparison.baselineIneligible"),
})[code];
const shortId = (value: string) => value.length > 16 ? `${value.slice(0, 16)}…` : value;
const formatTimestamp = (value: string) => {
  const date = new Date(value);
  return Number.isFinite(date.getTime()) ? localization.dateTime(date, { dateStyle: "short", timeStyle: "medium" }) : value;
};
const renderHarnessSide = (side: string, context: HarnessRelationship["baseline"]) => {
  if (context.availability === "unavailable") {
    return html`<div class="harness-side"><span>${side}</span><strong>${unavailable()}</strong><span>${harnessIssueLabel(context.reason)}</span></div>`;
  }
  const counts = localization.t("comparison.reportedRecords", { reported: localization.number(context.counts.reportedRecords), eligible: localization.number(context.counts.eligibleRecords) });
  if (context.state !== "uniform") {
    return html`<div class="harness-side"><span>${side}</span><strong>${harnessIssueLabel(context.state)}</strong><span>${counts}</span></div>`;
  }
  const name = context.identity.label || shortFingerprint(context.identity.fingerprint);
  return html`<div class="harness-side"><span>${side}</span><strong title=${context.identity.fingerprint}>${name}</strong><span>${counts} · ${localization.t("comparison.scopeValue", { scope: context.identity.scope })}</span></div>`;
};
const harnessIssueLabel = (issue: HarnessComparisonIssue) => ({
  server_unsupported: localization.t("comparison.serverUnsupported"),
  invalid_server_payload: localization.t("comparison.invalidPayload"),
  no_eligible_records: localization.t("comparison.noEligibleRecords"),
  unreported: localization.t("comparison.unreportedHarness"),
  mixed: localization.t("comparison.mixedHarness"),
  incomplete: localization.t("comparison.incompleteHarness"),
  invalid: localization.t("comparison.invalidHarness"),
})[issue];
const shortFingerprint = (value: string) => value.length > 19 ? `${value.slice(0, 19)}…` : value;

declare global { interface HTMLElementTagNameMap { "am-rework-comparison": ReworkComparison } }
