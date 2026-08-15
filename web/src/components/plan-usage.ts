import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { PlanUsageSnapshot } from "../model/telemetry";
import { NOT_CONNECTED, NOT_REPORTED } from "../presentation/missing-data";

@customElement("am-plan-usage")
export class PlanUsage extends LitElement {
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
      return html`<p class="empty"><strong>${NOT_CONNECTED}.</strong> Connect an account usage adapter to display plan limits; model tokens cannot determine them.</p>`;
    }
    return html`<div class="windows">${this.snapshots.map((snapshot) => html`<article>
      <header>
        <span>${windowLabel(snapshot)}</span>
        <strong>${formatPercent(snapshot.usedPercent)} used · ${formatPercent(100 - snapshot.usedPercent)} remaining</strong>
      </header>
      <div class="track" role="meter" aria-valuemin="0" aria-valuemax="100" aria-valuenow=${snapshot.usedPercent}>
        <div class="bar" style=${`width:${snapshot.usedPercent}%`}></div>
      </div>
      <small>${resetLabel(snapshot)} · observed ${formatLocalTime(snapshot.capturedAt)}</small>
      <small>${metadataLabel(snapshot)}</small>
    </article>`)}</div>`;
  }
}

const windowLabel = (snapshot: PlanUsageSnapshot) => {
  if (snapshot.windowDurationMinutes === undefined) return "Usage window";
  if (snapshot.windowDurationMinutes % 1_440 === 0) return `${snapshot.windowDurationMinutes / 1_440}-day window`;
  if (snapshot.windowDurationMinutes % 60 === 0) return `${snapshot.windowDurationMinutes / 60}-hour window`;
  return `${snapshot.windowDurationMinutes}-minute window`;
};
const resetLabel = (snapshot: PlanUsageSnapshot) => snapshot.resetsAt
  ? `resets ${formatLocalTime(snapshot.resetsAt)}`
  : NOT_REPORTED;
const metadataLabel = (snapshot: PlanUsageSnapshot) => [
  `Source ${snapshot.source}`,
  snapshot.accountId ? `account ${snapshot.accountId}` : undefined,
  snapshot.plan ? `plan ${snapshot.plan}` : undefined,
  `authority ${snapshot.authority}`,
].filter(Boolean).join(" · ");
const formatLocalTime = (value: string) => new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "short",
}).format(new Date(value));
const formatPercent = (value: number) => `${Math.max(0, Math.min(100, value)).toLocaleString(undefined, { maximumFractionDigits: 1 })}%`;

declare global { interface HTMLElementTagNameMap { "am-plan-usage": PlanUsage } }
