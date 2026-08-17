import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { ReworkAnalysis } from "../model/telemetry";
import { NOT_REPORTED } from "../presentation/missing-data";
import { featurePanelStyles } from "./feature-styles";
import "./kpi-card";

@customElement("am-rework-summary")
export class ReworkSummary extends LitElement {
  @property({ attribute: false }) analysis?: ReworkAnalysis;
  @property({ attribute: false }) legacySessionTotalTokens: number | null = null;
  @property({ type: Boolean }) loading = false;
  @property() error = "";

  static styles = [featurePanelStyles, css`
    :host { display: block; min-width: 0; }
    .heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 13px; }
    .heading h2 { margin-bottom: 4px; }
    .heading p, .state p { margin: 0; color: var(--am-muted); font-size: .72rem; line-height: 1.5; }
    .coverage-badge { flex: 0 0 auto; border: 1px solid var(--am-border); border-radius: 999px; padding: 5px 9px; color: var(--am-muted); font: 700 .62rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .06em; text-transform: uppercase; }
    .coverage-badge.partial { border-color: rgba(255, 190, 99, .42); color: #ffc77d; background: rgba(255, 190, 99, .08); }
    .metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; }
    .metric-group + .metric-group { margin-top: 14px; }
    .metric-group h3 { margin: 0 0 7px; color: var(--am-muted); font: 700 .64rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .08em; text-transform: uppercase; }
    .evidence { display: grid; grid-template-columns: minmax(0, 1.2fr) repeat(2, minmax(0, 1fr)); gap: 8px; margin-top: 10px; }
    .evidence article { border: 1px solid var(--am-border); border-radius: 9px; padding: 10px 12px; background: rgba(7, 10, 15, .25); }
    .evidence strong { display: block; margin-bottom: 4px; color: var(--am-text); font-size: .7rem; }
    .evidence p { margin: 0; color: var(--am-muted); font-size: .68rem; line-height: 1.5; }
    .episodes { margin-top: 14px; }
    .episodes h3 { margin: 0 0 7px; color: var(--am-muted); font: 700 .64rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .08em; text-transform: uppercase; }
    .episode-list { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
    .episode { min-width: 0; border: 1px solid var(--am-border); border-radius: 9px; padding: 11px 12px; background: rgba(7, 10, 15, .25); }
    .episode strong, .episode code { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .episode strong { color: var(--am-text); font-size: .72rem; }
    .episode code { margin: 5px 0; color: var(--am-accent); font-size: .66rem; }
    .episode p { margin: 0; color: var(--am-muted); font-size: .66rem; line-height: 1.5; }
    .episode a { display: inline-block; margin-top: 7px; color: var(--am-accent); font-size: .66rem; text-decoration: none; }
    .episode a:hover, .episode a:focus-visible { text-decoration: underline; outline: 2px solid var(--am-accent-soft); }
    .state { min-height: 140px; display: grid; place-items: center; text-align: center; }
    .state strong { display: block; margin-bottom: 6px; color: var(--am-text); }
    .state button { margin-top: 12px; border: 1px solid var(--am-border); border-radius: 7px; padding: 7px 11px; background: var(--am-surface-raised); color: var(--am-text); cursor: pointer; font: inherit; }
    .state button:hover, .state button:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    @media (max-width: 1050px) { .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .evidence, .episode-list { grid-template-columns: 1fr; } }
    @media (max-width: 520px) { .heading { display: block; } .coverage-badge { display: inline-block; margin-top: 9px; } .metrics { grid-template-columns: 1fr; } }
  `];

  render() {
    if (this.loading) return html`<section class="panel state" role="status"><div><strong>Analyzing development rework</strong><p>Normalizing the complete retained session.</p></div></section>`;
    if (this.error) return html`<section class="panel state error" role="alert"><div><strong>Rework analysis unavailable</strong><p>${this.error}</p><button type="button" @click=${this.retry}>Retry analysis</button></div></section>`;
    if (!this.analysis) return html`<section class="panel state"><div><strong>Waiting for rework analysis</strong><p>No analysis result is available for this session yet.</p></div></section>`;

    const { metrics, coverage, capabilities } = this.analysis;
    const partial = coverage.activityCoverage !== "observed_projection_complete";
    const reworkEffortHint = formatReworkEffortHint(metrics.reworkDurationMs, metrics.totalAgentEffortMs, metrics.reworkAgentEffortRate);
    const sessionTotalTokens = this.analysis.sessionTokens === undefined
      ? this.analysis.harness.availability === "unavailable" && this.analysis.harness.reason === "server_unsupported"
        ? this.legacySessionTotalTokens
        : null
      : this.analysis.sessionTokens.total;
    const reworkTokenRate = calculateRate(metrics.reworkTokens.total, sessionTotalTokens);
    const reworkTokenHint = formatReworkTokenHint(metrics.reworkTokens.total, sessionTotalTokens);
    return html`<section class="panel">
      <div class="heading"><div><h2>Development rework</h2><p>Diagnostic signals from normalized Claude/Codex telemetry—not a productivity score.</p></div><span class=${`coverage-badge ${partial ? "partial" : ""}`}>${partial ? "Partial evidence" : "Complete retained projection"}</span></div>
      <div class="metric-group"><h3>Validation effectiveness</h3><div class="metrics" aria-label="Validation effectiveness indicators">
        <am-kpi-card label="Validation failures" .value=${formatCount(metrics.validationFailures)} hint="Test, build, and lint" description="Known failed test, build, and lint operations. Use this to locate validation friction; zero can also mean no validation ran or outcomes were not reported."></am-kpi-card>
        <am-kpi-card label="Initial validation success" .value=${formatRate(metrics.firstPassSuccessRate)} .hint=${formatFirstPassHint(metrics.firstPassSuccesses, metrics.firstPassEligibleValidations)} description="ID-backed command identities whose first observed known-outcome attempt in this session succeeded. Identity is same agent, operation, normalized command, and working directory when reported—not a change or task—so this is a first-pass proxy. Uncorrelated observations are excluded; missing outcomes can still bias the rate."></am-kpi-card>
      </div></div>
      <div class="metric-group"><h3>Recurring-loop impact</h3><div class="metrics" aria-label="Recurring failure loop indicators">
        <am-kpi-card label="Recurring failure loops" .value=${formatRecurringLoops(metrics.recurringFailureLoops)} .hint=${formatRecurringLoopHint(metrics.recurringFailureLoops, metrics.resolvedFailureLoops, metrics.unresolvedFailureLoops, coverage.fingerprintedFailures, metrics.validationFailures, coverage.uncorrelatedValidationObservations)} description="ID-backed validation episodes where the same agent ran the same normalized test, build, or lint command in the same reported working directory and the same unambiguous error fingerprint failed at least twice. Uncorrelated observations and ambiguous error evidence are excluded from confirmed recurrence."></am-kpi-card>
        <am-kpi-card label="Failure attempts in recurring loops" .value=${formatCount(metrics.repeatedFailureAttempts)} hint="Includes the first failure" description="All failed validation attempts in an episode once any observed error fingerprint recurs, including failures with a different or missing fingerprint inside that episode. FAIL(X) → FAIL(Y) → FAIL(X) → PASS counts as three attempts."></am-kpi-card>
        <am-kpi-card label="Total loop resolution time" .value=${formatResolutionDuration(metrics.failureResolutionDurationMs, metrics.resolvedFailureLoops)} .hint=${formatResolutionHint(metrics.resolvedFailureLoops, metrics.unresolvedFailureLoops)} description="Sum of each resolved recurring loop's elapsed wall time from first failure to matching success. Unresolved loops are excluded, and overlapping loops contribute separately, so the total can exceed session elapsed time."></am-kpi-card>
        <am-kpi-card label="Tokens in resolved loops" .value=${formatResolutionTokens(metrics.failureResolutionTokens.total, metrics.resolvedFailureLoops)} .hint=${formatResolutionHint(metrics.resolvedFailureLoops, metrics.unresolvedFailureLoops)} description="Unique authoritative same-agent token observations inside resolved recurring-loop windows. Unresolved loops are excluded. Missing token telemetry is shown as not reported; this is associated cost, not proven waste."></am-kpi-card>
      </div></div>
      <div class="metric-group"><h3>Other operational signals</h3><div class="metrics" aria-label="Other operational indicators">
        <am-kpi-card label="Detected retry-cycle share" .value=${formatRate(metrics.reworkAgentEffortRate)} .hint=${reworkEffortHint} description="Detected same-agent activity time inside failure, edit, and matching retry windows divided by all observed agent-active time. Matching is heuristic and the retry need not succeed. Activity overlap is merged within each agent; other parallel agents add to the denominator and can dilute the session share. This is not proven waste or a productivity score."></am-kpi-card>
        <am-kpi-card label="Rework token rate" .value=${formatRate(reworkTokenRate)} .hint=${reworkTokenHint} description="Authoritative tokens observed inside detected failure/edit/retry windows divided by total authoritative tokens for this session. It normalizes for session size, but cycle-associated tokens are not necessarily avoidable waste."></am-kpi-card>
        <am-kpi-card label="Tool failure rate" .value=${formatRate(metrics.toolFailureRate)} .hint=${metrics.toolAttemptsWithOutcome ? `${metrics.toolFailures} of ${metrics.toolAttemptsWithOutcome} known outcomes` : "No reported outcomes"} description="Known failed tool outcomes divided by tool attempts with known outcomes. Missing outcomes are excluded, and small samples can make the percentage unstable."></am-kpi-card>
        <am-kpi-card label="Estimated API retry" .value=${formatCount(metrics.apiRetryWaste.attempts)} .hint=${`${formatDuration(metrics.apiRetryWaste.durationMs)} observed`} description="Failed API attempts followed later by a matching API call. Matching is heuristic; the value shows estimated retry count and observed duration, not guaranteed waste."></am-kpi-card>
        <am-kpi-card label="Repeated commands" .value=${formatCount(metrics.repeatedCommands)} hint="Same agent and command" description="Each execution after the first of the same normalized command by one agent. Re-running validation after an edit may be healthy rather than rework."></am-kpi-card>
        <am-kpi-card label="Re-edited files" .value=${formatCount(metrics.reeditedFiles)} hint="Same agent and file" description="Each edit after the first to the same file by one agent. Iterative editing can be normal; this is a concentration signal, not proof of wasted work."></am-kpi-card>
      </div></div>
      ${renderFailureEpisodes(this.analysis.failureEpisodes)}
      <div class="evidence">
        <article><strong>${partial ? "Partial evidence" : "Evidence coverage"}</strong><p>${metrics.validationAttemptsWithOutcome} of ${coverage.validationAttempts} logical validation attempts report outcomes · ${coverage.identifiedValidationAttempts} have command identity · ${coverage.fingerprintedFailures} of ${metrics.validationFailures} failures fingerprinted · ${coverage.idBackedValidationAttempts} ID-backed (${coverage.mergedValidationAttempts} merged) / ${coverage.uncorrelatedValidationObservations} uncorrelated / ${coverage.conflictingAttemptObservations} identity conflicts / ${coverage.ambiguousFailureAttempts} ambiguous errors</p></article>
        ${capability("Change revert", capabilities.changeRevert.state, capabilities.changeRevert.reason)}
        ${capability("Cross-agent overlap", capabilities.crossAgentOverlap.state, capabilities.crossAgentOverlap.reason)}
      </div>
    </section>`;
  }

  private retry = () => this.dispatchEvent(new CustomEvent("rework-retry-requested", { bubbles: true, composed: true }));
}

const capability = (label: string, state: string, reason: string) => html`<article><strong>${label} · ${state === "unavailable" ? "Not available" : state}</strong><p>${reason}</p></article>`;
const renderFailureEpisodes = (episodes: ReworkAnalysis["failureEpisodes"]) => {
  if (episodes.length === 0) return null;
  const visible = [...episodes].sort((first, second) => second.failureAttempts - first.failureAttempts).slice(0, 3);
  return html`<div class="episodes"><h3>Highest-impact recurring loops</h3><div class="episode-list">${visible.map((episode) => html`
    <article class="episode">
      <strong>${episode.operation} · ${shortFingerprint(episode.validationFingerprint).replace("error", "validation")}</strong>
      <code title=${episode.errorFingerprints.join(", ")}>${episode.errorFingerprints.map(shortFingerprint).join(" · ")}</code>
      <p>${episode.failureAttempts} failed attempts · ${episode.resolved ? `resolved in ${formatDuration(episode.resolutionDurationMs)}` : "unresolved"} · agent ${episode.agentId || "main"}</p>
      ${episode.traceId ? html`<a href=${`/traces/${encodeURIComponent(episode.traceId)}`}>Open first failed attempt</a>` : null}
    </article>`)}${episodes.length > visible.length ? html`<article class="episode"><strong>${episodes.length - visible.length} more loops</strong><p>Shown in the analysis API.</p></article>` : null}</div></div>`;
};
const shortFingerprint = (fingerprint: string) => fingerprint.replace("sha256:", "error ").slice(0, 18);
const formatCount = (value: number) => value.toLocaleString();
const formatFirstPassHint = (successes: number, eligible: number) => eligible > 0 ? `${successes} of ${eligible} validation identities` : "No validation identities with known outcomes";
const formatRecurringLoops = (loops: number) => loops === 0 ? "0 detected" : formatCount(loops);
const formatRecurringLoopHint = (loops: number, resolved: number, unresolved: number, fingerprinted: number, failures: number, uncorrelated: number) => {
  if (loops > 0) return `${resolved} resolved · ${unresolved} unresolved`;
  if (uncorrelated > 0) return `${uncorrelated} uncorrelated validation observation${uncorrelated === 1 ? "" : "s"} excluded`;
  if (failures > 0 && fingerprinted === 0) return "Insufficient failure evidence for fingerprints";
  if (fingerprinted < failures) return `${fingerprinted} of ${failures} failed validations fingerprinted`;
  return "No same-error recurrence detected";
};
const formatResolutionHint = (resolved: number, unresolved: number) => resolved > 0 ? `${resolved} resolved loop${resolved === 1 ? "" : "s"} · ${unresolved} unresolved` : `No resolved recurring loops · ${unresolved} unresolved`;
const formatResolutionDuration = (milliseconds: number, resolved: number) => resolved === 0 ? NOT_REPORTED : formatDuration(milliseconds);
const formatResolutionTokens = (tokens: number | null, resolved: number) => resolved === 0 || tokens === null ? NOT_REPORTED : formatCount(tokens);
const calculateRate = (part: number | null, total: number | null) => part === null || total === null || total <= 0 ? null : part / total;
const formatReworkTokenHint = (reworkTokens: number | null, sessionTokens: number | null) => {
  if (reworkTokens === null) return "Rework token usage unavailable";
  if (sessionTokens === null || sessionTokens <= 0) return "Session token total unavailable";
  return `${formatCount(reworkTokens)} of ${formatCount(sessionTokens)} session tokens`;
};
const formatReworkEffortHint = (reworkMs: number, totalMs: number, rate: number | null) => {
  if (rate === null || totalMs <= 0) return "Observed agent-active duration unavailable";
  if (rate === 0) return `No detected closed retry cycles · ${formatDuration(totalMs)} observed agent-active time`;
  return `${formatDuration(reworkMs)} of ${formatDuration(totalMs)} observed agent-active time`;
};
const formatRate = (value: number | null) => value === null ? NOT_REPORTED : `${(value * 100).toFixed(1)}%`;
const formatDuration = (milliseconds: number) => {
  if (milliseconds < 1000) return `${milliseconds.toLocaleString()} ms`;
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(milliseconds % 1000 === 0 ? 0 : 1)} s`;
  return `${(milliseconds / 60_000).toFixed(1)} min`;
};

declare global { interface HTMLElementTagNameMap { "am-rework-summary": ReworkSummary } }
