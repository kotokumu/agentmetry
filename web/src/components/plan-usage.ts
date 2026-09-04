import { css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { PlanUsageSnapshot } from "../model/telemetry";
import { notConnected, notReported } from "../presentation/missing-data";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";

@customElement("am-plan-usage")
export class PlanUsage extends LocalizedElement {
  @property({ attribute: false }) snapshots: readonly PlanUsageSnapshot[] = [];

  static styles = css`
    :host { display: block; }
    .empty { margin: 0; color: var(--am-muted); font-size: .82rem; }
    .windows { display: grid; gap: 8px; }
    article { display: grid; gap: 6px; }
    header { display: flex; justify-content: space-between; gap: 12px; font-size: .76rem; }
    strong { font-family: "SFMono-Regular", "Cascadia Code", monospace; }
    .track { height: 6px; overflow: hidden; border-radius: 99px; background: var(--am-track); box-shadow: inset 0 1px 2px rgba(0, 0, 0, .3); }
    .bar { height: 100%; border-radius: inherit; background: linear-gradient(90deg, var(--am-accent), var(--am-secondary)); box-shadow: 0 0 12px rgba(var(--am-accent-rgb), .35); }
    small { color: var(--am-muted); }
  `;

  render() {
    if (this.snapshots.length === 0) {
      return html`<p class="empty"><strong>${notConnected()}.</strong> ${localization.t("plan.notConnected")}</p>`;
    }
    return html`<div class="windows">${this.snapshots.map((snapshot) => html`<article>
      <header>
        <span>${windowLabel(snapshot)}</span>
        <strong>${localization.t("plan.usedRemaining", { used: formatPercent(snapshot.usedPercent), remaining: formatPercent(100 - snapshot.usedPercent) })}</strong>
      </header>
      <div class="track" role="meter" aria-valuemin="0" aria-valuemax="100" aria-valuenow=${snapshot.usedPercent}>
        <div class="bar" style=${`width:${snapshot.usedPercent}%`}></div>
      </div>
      <small>${resetLabel(snapshot)} · ${localization.t("plan.observed", { time: formatLocalTime(snapshot.capturedAt) })}</small>
      <small>${metadataLabel(snapshot)}</small>
    </article>`)}</div>`;
  }
}

const windowLabel = (snapshot: PlanUsageSnapshot) => {
  if (snapshot.windowDurationMinutes === undefined) return localization.t("plan.usageWindow");
  if (snapshot.windowDurationMinutes % 1_440 === 0) return localization.t("plan.days", { count: snapshot.windowDurationMinutes / 1_440 });
  if (snapshot.windowDurationMinutes % 60 === 0) return localization.t("plan.hours", { count: snapshot.windowDurationMinutes / 60 });
  return localization.t("plan.minutes", { count: snapshot.windowDurationMinutes });
};
const resetLabel = (snapshot: PlanUsageSnapshot) => snapshot.resetsAt
  ? localization.t("plan.resets", { time: formatLocalTime(snapshot.resetsAt) })
  : notReported();
const metadataLabel = (snapshot: PlanUsageSnapshot) => [
  localization.t("plan.source", { value: snapshot.source }),
  snapshot.accountId ? localization.t("plan.account", { value: snapshot.accountId }) : undefined,
  snapshot.plan ? localization.t("plan.plan", { value: snapshot.plan }) : undefined,
  localization.t("plan.authority", { value: snapshot.authority }),
].filter(Boolean).join(" · ");
const formatLocalTime = (value: string) => localization.dateTime(new Date(value), {
  dateStyle: "medium",
  timeStyle: "short",
});
const formatPercent = (value: number) => `${localization.number(Math.max(0, Math.min(100, value)), { maximumFractionDigits: 1 })}%`;

declare global { interface HTMLElementTagNameMap { "am-plan-usage": PlanUsage } }
