import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { Trace } from "../model/update";

@customElement("am-trace-summary")
export class TraceSummary extends LitElement {
  @property({ attribute: false }) trace?: Trace;

  static styles = css`
    :host { display: block; }
    .identity { display: flex; justify-content: space-between; gap: 18px; align-items: flex-start; }
    code { display: block; color: var(--am-accent); font: .76rem/1.5 "SFMono-Regular", "Cascadia Code", monospace; overflow-wrap: anywhere; }
    .facts { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 14px; }
    .fact { border: 1px solid var(--am-border); border-radius: 99px; padding: 6px 10px; color: var(--am-muted); font-size: .75rem; }
    .status { font-weight: 700; text-transform: capitalize; }
    .status.error { border-color: #c66a59; color: #9f2f23; background: #f9e6df; }
    .status.ok { border-color: #7d9d83; color: #356442; background: #e6f0e5; }
    p { margin: 0; color: var(--am-muted); font-size: .76rem; }
  `;

  render() {
    const trace = this.trace;
    if (!trace) return null;
    const activityCount = trace.activityCount || trace.activities.length;
    return html`<div class="identity">
      <div><p>OTLP Trace ID</p><code>${trace.traceId}</code></div>
      <span class=${`fact status ${trace.status.toLowerCase()}`}>${title(trace.status)}</span>
    </div>
    <div class="facts" aria-label="Trace summary">
      <span class="fact">${formatDuration(trace.startedAt, trace.endedAt)}</span>
      <span class="fact">${plural(trace.conversations.length, "conversation")}</span>
      <span class="fact">${plural(trace.agents.length, "agent")}</span>
      <span class="fact">${plural(trace.rootSpanCount, "root span")}</span>
      <span class="fact">${plural(trace.missingParentCount, "missing parent")}</span>
      <span class="fact">Showing ${trace.activities.length.toLocaleString()} of ${activityCount.toLocaleString()} activities</span>
    </div>`;
  }
}

const title = (value: string) => value ? `${value[0].toUpperCase()}${value.slice(1)}` : "Unknown";
const plural = (count: number, label: string) => `${count.toLocaleString()} ${label}${count === 1 ? "" : "s"}`;
const formatDuration = (startedAt: string, endedAt: string) => {
  const milliseconds = Math.max(0, new Date(endedAt).getTime() - new Date(startedAt).getTime());
  return milliseconds < 1_000 ? `${milliseconds} ms` : `${(milliseconds / 1_000).toFixed(milliseconds < 10_000 ? 2 : 1)} s`;
};

declare global { interface HTMLElementTagNameMap { "am-trace-summary": TraceSummary } }
