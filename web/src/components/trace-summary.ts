import { css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { Trace } from "../model/telemetry";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";

@customElement("am-trace-summary")
export class TraceSummary extends LocalizedElement {
  @property({ attribute: false }) trace?: Trace;

  static styles = css`
    :host { display: block; }
    .identity { display: flex; justify-content: space-between; gap: 18px; align-items: flex-start; }
    code { display: block; color: var(--am-accent); font: .76rem/1.5 "SFMono-Regular", "Cascadia Code", monospace; overflow-wrap: anywhere; }
    .facts { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 14px; }
    .fact { border: 1px solid var(--am-border); border-radius: 6px; padding: 6px 9px; background: var(--am-surface-strong); color: var(--am-muted); font: .7rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; }
    .status { font-weight: 700; text-transform: capitalize; }
    .status.error { border-color: color-mix(in srgb, var(--am-danger) 55%, transparent); color: var(--am-danger); background: color-mix(in srgb, var(--am-danger) 10%, transparent); }
    .status.ok { border-color: color-mix(in srgb, var(--am-success) 48%, transparent); color: var(--am-success); background: color-mix(in srgb, var(--am-success) 9%, transparent); }
    p { margin: 0; color: var(--am-muted); font-size: .76rem; }
  `;

  render() {
    const trace = this.trace;
    if (!trace) return null;
    const activityCount = trace.activityCount || trace.activities.length;
    return html`<div class="identity">
      <div><p>${localization.t("trace.otlpId")}</p><code>${trace.traceId}</code></div>
      <span class=${`fact status ${trace.status.toLowerCase()}`}>${title(trace.status)}</span>
    </div>
    <div class="facts" aria-label=${localization.t("trace.summaryAria")}>
      <span class="fact">${formatDuration(trace.startedAt, trace.endedAt)}</span>
      <span class="fact">${localization.t("trace.conversationCount", { count: localization.number(trace.conversations.length) })}</span>
      <span class="fact">${localization.t("trace.agentCount", { count: localization.number(trace.agents.length) })}</span>
      <span class="fact">${localization.t("trace.rootSpanCount", { count: localization.number(trace.rootSpanCount) })}</span>
      <span class="fact">${localization.t("trace.missingParentCount", { count: localization.number(trace.missingParentCount) })}</span>
      <span class="fact">${localization.t("trace.showingActivities", { shown: localization.number(trace.activities.length), total: localization.number(activityCount) })}</span>
    </div>`;
  }
}

const title = (value: string) => value ? `${value[0].toUpperCase()}${value.slice(1)}` : localization.t("common.unknown");
const formatDuration = (startedAt: string, endedAt: string) => {
  const milliseconds = Math.max(0, new Date(endedAt).getTime() - new Date(startedAt).getTime());
  return milliseconds < 1_000 ? `${milliseconds} ms` : `${(milliseconds / 1_000).toFixed(milliseconds < 10_000 ? 2 : 1)} s`;
};

declare global { interface HTMLElementTagNameMap { "am-trace-summary": TraceSummary } }
