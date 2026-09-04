import { css, html, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import type { ReworkAnalysis } from "../model/telemetry";
import { notReported } from "../presentation/missing-data";
import { featurePanelStyles } from "./feature-styles";
import "./kpi-card";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";

@customElement("am-rework-summary")
export class ReworkSummary extends LocalizedElement {
  @property({ attribute: false }) analysis?: ReworkAnalysis;
  @property({ attribute: false }) legacySessionTotalTokens: number | null = null;
  @property({ attribute: false }) locationForTrace: (traceId: string, spanId?: string) => string =
    (traceId, spanId) => `/traces/${encodeURIComponent(traceId)}${spanId ? `?spanId=${encodeURIComponent(spanId)}` : ""}`;
  @property({ type: Boolean }) loading = false;
  @property() error = "";
  @state() private visibleEpisodeCount = 3;
  private episodeSourceId?: string;
  private episodeSessionId?: string;

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
    .episode-count { margin: 8px 0 0; color: var(--am-muted); font-size: .68rem; }
    .state { min-height: 140px; display: grid; place-items: center; text-align: center; }
    .state strong { display: block; margin-bottom: 6px; color: var(--am-text); }
    .state button, .show-more { margin-top: 12px; border: 1px solid var(--am-border); border-radius: 7px; padding: 7px 11px; background: var(--am-surface-raised); color: var(--am-text); cursor: pointer; font: inherit; }
    .state button:hover, .state button:focus-visible, .show-more:hover, .show-more:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    @media (max-width: 1050px) { .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .evidence, .episode-list { grid-template-columns: 1fr; } }
    @media (max-width: 520px) { .heading { display: block; } .coverage-badge { display: inline-block; margin-top: 9px; } .metrics { grid-template-columns: 1fr; } }
  `];

  protected willUpdate(changed: PropertyValues<this>) {
    if (changed.has("analysis") && this.analysis
      && (this.analysis.sourceId !== this.episodeSourceId || this.analysis.sessionId !== this.episodeSessionId)) {
      this.visibleEpisodeCount = 3;
      this.episodeSourceId = this.analysis.sourceId;
      this.episodeSessionId = this.analysis.sessionId;
    }
  }

  async focusEvidence(traceId: string, spanId: string): Promise<boolean> {
    await this.updateComplete;
    if (!spanId || !this.analysis || this.loading || this.error) return false;
    const { sourceId, sessionId } = this.analysis;
    const index = this.sortedEpisodes.findIndex((episode) => episode.spanId === spanId && episode.traceId === traceId);
    if (index < 0) return false;
    this.visibleEpisodeCount = Math.max(this.visibleEpisodeCount, index + 1);
    await this.updateComplete;
    if (this.analysis?.sourceId !== sourceId || this.analysis.sessionId !== sessionId) return false;
    const link = Array.from(this.shadowRoot?.querySelectorAll<HTMLAnchorElement>(".episode a") ?? [])
      .find((candidate) => candidate.dataset.spanId === spanId && candidate.dataset.traceId === traceId);
    if (!link) return false;
    link.focus({ preventScroll: true });
    return this.shadowRoot?.activeElement === link;
  }

  render() {
    if (this.loading) return html`<section class="panel state" role="status"><div><strong>${localization.t("rework.analyzing")}</strong><p>${localization.t("rework.normalizing")}</p></div></section>`;
    if (this.error) return html`<section class="panel state error" role="alert"><div><strong>${localization.t("rework.unavailable")}</strong><p>${this.error}</p><button type="button" @click=${this.retry}>${localization.t("rework.retry")}</button></div></section>`;
    if (!this.analysis) return html`<section class="panel state"><div><strong>${localization.t("rework.waiting")}</strong><p>${localization.t("rework.noResult")}</p></div></section>`;

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
      <div class="heading"><div><h2>${localization.t("rework.title")}</h2><p>${localization.t("rework.subtitle")}</p></div><span class=${`coverage-badge ${partial ? "partial" : ""}`}>${localization.t(partial ? "rework.partialEvidence" : "rework.completeProjection")}</span></div>
      <div class="metric-group"><h3>${localization.t("rework.validationEffectiveness")}</h3><div class="metrics" aria-label=${localization.t("rework.validationAria")}>
        <am-kpi-card .label=${localization.t("rework.validationFailures")} .value=${formatCount(metrics.validationFailures)} .hint=${localization.t("rework.validationHint")} .description=${localization.t("rework.validationDescription")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("rework.initialSuccess")} .value=${formatRate(metrics.firstPassSuccessRate)} .hint=${formatFirstPassHint(metrics.firstPassSuccesses, metrics.firstPassEligibleValidations)} .description=${localization.t("rework.initialSuccessDescription")}></am-kpi-card>
      </div></div>
      <div class="metric-group"><h3>${localization.t("rework.recurringImpact")}</h3><div class="metrics" aria-label=${localization.t("rework.recurringAria")}>
        <am-kpi-card .label=${localization.t("rework.recurringLoops")} .value=${formatRecurringLoops(metrics.recurringFailureLoops)} .hint=${formatRecurringLoopHint(metrics.recurringFailureLoops, metrics.resolvedFailureLoops, metrics.unresolvedFailureLoops, coverage.fingerprintedFailures, metrics.validationFailures, coverage.uncorrelatedValidationObservations)} .description=${localization.t("rework.recurringDescription")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("rework.failureAttempts")} .value=${formatCount(metrics.repeatedFailureAttempts)} .hint=${localization.t("rework.includesFirst")} .description=${localization.t("rework.failureAttemptsDescription")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("rework.resolutionTime")} .value=${formatResolutionDuration(metrics.failureResolutionDurationMs, metrics.resolvedFailureLoops)} .hint=${formatResolutionHint(metrics.resolvedFailureLoops, metrics.unresolvedFailureLoops)} .description=${localization.t("rework.resolutionDescription")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("rework.resolutionTokens")} .value=${formatResolutionTokens(metrics.failureResolutionTokens.total, metrics.resolvedFailureLoops)} .hint=${formatResolutionHint(metrics.resolvedFailureLoops, metrics.unresolvedFailureLoops)} .description=${localization.t("rework.resolutionTokensDescription")}></am-kpi-card>
      </div></div>
      <div class="metric-group"><h3>${localization.t("rework.otherSignals")}</h3><div class="metrics" aria-label=${localization.t("rework.otherAria")}>
        <am-kpi-card .label=${localization.t("rework.effortShare")} .value=${formatRate(metrics.reworkAgentEffortRate)} .hint=${reworkEffortHint} .description=${localization.t("rework.effortDescription")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("rework.tokenRate")} .value=${formatRate(reworkTokenRate)} .hint=${reworkTokenHint} .description=${localization.t("rework.tokenDescription")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("rework.toolFailureRate")} .value=${formatRate(metrics.toolFailureRate)} .hint=${metrics.toolAttemptsWithOutcome ? localization.t("rework.knownOutcomes", { failures: metrics.toolFailures, total: metrics.toolAttemptsWithOutcome }) : localization.t("rework.noOutcomes")} .description=${localization.t("rework.toolFailureDescription")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("rework.apiRetry")} .value=${formatCount(metrics.apiRetryWaste.attempts)} .hint=${localization.t("rework.observedDuration", { duration: formatDuration(metrics.apiRetryWaste.durationMs) })} .description=${localization.t("rework.apiRetryDescription")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("rework.repeatedCommands")} .value=${formatCount(metrics.repeatedCommands)} .hint=${localization.t("rework.sameAgentCommand")} .description=${localization.t("rework.repeatedCommandsDescription")}></am-kpi-card>
        <am-kpi-card .label=${localization.t("rework.reeditedFiles")} .value=${formatCount(metrics.reeditedFiles)} .hint=${localization.t("rework.sameAgentFile")} .description=${localization.t("rework.reeditedFilesDescription")}></am-kpi-card>
      </div></div>
      ${this.renderFailureEpisodes()}
      <div class="evidence">
        <article><strong>${localization.t(partial ? "rework.partialEvidence" : "rework.evidenceCoverage")}</strong><p>${localization.t("rework.coverageDetail", { outcomes: metrics.validationAttemptsWithOutcome, validations: coverage.validationAttempts, identified: coverage.identifiedValidationAttempts, fingerprinted: coverage.fingerprintedFailures, failures: metrics.validationFailures, backed: coverage.idBackedValidationAttempts, merged: coverage.mergedValidationAttempts, uncorrelated: coverage.uncorrelatedValidationObservations, conflicts: coverage.conflictingAttemptObservations, ambiguous: coverage.ambiguousFailureAttempts })}</p></article>
        ${capability(localization.t("rework.changeRevert"), capabilities.changeRevert.state, capabilities.changeRevert.reason)}
        ${capability(localization.t("rework.crossAgentOverlap"), capabilities.crossAgentOverlap.state, capabilities.crossAgentOverlap.reason)}
      </div>
    </section>`;
  }

  private renderFailureEpisodes() {
    const episodes = this.sortedEpisodes;
    if (episodes.length === 0) return null;
    const visible = episodes.slice(0, this.visibleEpisodeCount);
    return html`<div class="episodes"><h3>${localization.t("rework.highestImpact")}</h3><div class="episode-list" id="failure-episodes">${visible.map((episode) => html`
      <article class="episode">
        <strong>${episode.operation} · ${shortFingerprint(episode.validationFingerprint).replace("error", "validation")}</strong>
        <code title=${episode.errorFingerprints.join(", ")}>${episode.errorFingerprints.map(shortFingerprint).join(" · ")}</code>
        <p>${localization.t("rework.episode", { attempts: episode.failureAttempts, resolution: episode.resolved ? localization.t("rework.resolvedIn", { duration: formatDuration(episode.resolutionDurationMs) }) : localization.t("rework.unresolved"), agent: episode.agentId || "main" })}</p>
        ${episode.traceId ? html`<a href=${this.locationForTrace(episode.traceId, episode.spanId)} data-span-id=${episode.spanId} data-trace-id=${episode.traceId} @click=${(event: MouseEvent) => this.openEvidence(event, episode)}>${localization.t("rework.openFirst")}</a>` : null}
      </article>`)}</div>
      <p class="episode-count">${localization.t("rework.showingEpisodes", { shown: visible.length, total: episodes.length })}</p>
      ${episodes.length > visible.length ? html`<button class="show-more" type="button" aria-controls="failure-episodes" @click=${this.showMoreEpisodes}>${localization.t("rework.showMore", { count: episodes.length - visible.length })}</button>` : null}
    </div>`;
  }

  private get sortedEpisodes() {
    return [...this.analysis?.failureEpisodes ?? []].sort((first, second) => second.failureAttempts - first.failureAttempts);
  }

  private openEvidence(event: MouseEvent, episode: ReworkAnalysis["failureEpisodes"][number]) {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || !this.analysis) return;
    event.preventDefault();
    this.dispatchEvent(new CustomEvent("trace-selected", {
      detail: {
        sourceId: this.analysis.sourceId, conversationId: this.analysis.sessionId,
        traceId: episode.traceId, spanId: episode.spanId, evidenceOrigin: "episode",
      },
      bubbles: true,
      composed: true,
    }));
  }

  private showMoreEpisodes = () => { this.visibleEpisodeCount += 3; };
  private retry = () => this.dispatchEvent(new CustomEvent("rework-retry-requested", { bubbles: true, composed: true }));
}

const capability = (label: string, state: string, reason: string) => html`<article><strong>${label} · ${state === "unavailable" ? localization.t("rework.notAvailable") : state}</strong><p>${reason}</p></article>`;
const shortFingerprint = (fingerprint: string) => fingerprint.replace("sha256:", "error ").slice(0, 18);
const formatCount = (value: number) => localization.number(value);
const formatFirstPassHint = (successes: number, eligible: number) => eligible > 0 ? localization.t("rework.validationIdentities", { successes, eligible }) : localization.t("rework.noValidationIdentities");
const formatRecurringLoops = (loops: number) => loops === 0 ? localization.t("rework.detected", { count: 0 }) : formatCount(loops);
const formatRecurringLoopHint = (loops: number, resolved: number, unresolved: number, fingerprinted: number, failures: number, uncorrelated: number) => {
  if (loops > 0) return localization.t("rework.resolvedUnresolved", { resolved, unresolved });
  if (uncorrelated > 0) return localization.t("rework.uncorrelatedExcluded", { count: uncorrelated });
  if (failures > 0 && fingerprinted === 0) return localization.t("rework.insufficientFingerprints");
  if (fingerprinted < failures) return localization.t("rework.fingerprinted", { count: fingerprinted, total: failures });
  return localization.t("rework.noRecurrence");
};
const formatResolutionHint = (resolved: number, unresolved: number) => resolved > 0 ? localization.t("rework.resolvedLoops", { resolved, unresolved }) : localization.t("rework.noResolvedLoops", { unresolved });
const formatResolutionDuration = (milliseconds: number, resolved: number) => resolved === 0 ? notReported() : formatDuration(milliseconds);
const formatResolutionTokens = (tokens: number | null, resolved: number) => resolved === 0 || tokens === null ? notReported() : formatCount(tokens);
const calculateRate = (part: number | null, total: number | null) => part === null || total === null || total <= 0 ? null : part / total;
const formatReworkTokenHint = (reworkTokens: number | null, sessionTokens: number | null) => {
  if (reworkTokens === null) return localization.t("rework.tokenUnavailable");
  if (sessionTokens === null || sessionTokens <= 0) return localization.t("rework.sessionTokenUnavailable");
  return localization.t("rework.sessionTokens", { part: formatCount(reworkTokens), total: formatCount(sessionTokens) });
};
const formatReworkEffortHint = (reworkMs: number, totalMs: number, rate: number | null) => {
  if (rate === null || totalMs <= 0) return localization.t("rework.effortUnavailable");
  if (rate === 0) return localization.t("rework.noClosedCycles", { total: formatDuration(totalMs) });
  return localization.t("rework.effortTime", { part: formatDuration(reworkMs), total: formatDuration(totalMs) });
};
const formatRate = (value: number | null) => value === null ? notReported() : `${localization.number(value * 100, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%`;
const formatDuration = (milliseconds: number) => {
  if (milliseconds < 1000) return `${localization.number(milliseconds)} ms`;
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(milliseconds % 1000 === 0 ? 0 : 1)} s`;
  return `${(milliseconds / 60_000).toFixed(1)} min`;
};

declare global { interface HTMLElementTagNameMap { "am-rework-summary": ReworkSummary } }
