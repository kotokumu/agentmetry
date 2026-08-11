import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { TokenUsage } from "../model/update";

const emptyUsage: TokenUsage = {
  input: null,
  output: null,
  cacheRead: null,
  cacheWrite: null,
  reasoning: null,
  total: null,
};

@customElement("am-token-chart")
export class TokenChart extends LitElement {
  @property({ attribute: false }) usage: TokenUsage = emptyUsage;

  static styles = css`
    :host { display: block; }
    .chart { display: grid; gap: 8px; }
    .row { display: grid; grid-template-columns: 92px 1fr 72px; align-items: center; gap: 10px; }
    .label, .value { font: 0.72rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; }
    .label { color: var(--am-muted); text-transform: uppercase; }
    .value { text-align: right; color: var(--am-text); }
    .track { height: 8px; border-radius: 99px; background: var(--am-track); overflow: hidden; }
    .bar { height: 100%; min-width: 2px; border-radius: inherit; background: var(--am-accent); }
  `;

  render() {
    const rows: readonly (readonly [string, number | null])[] = [
      ["request input", this.usage.input],
      ["↳ cache read", this.usage.cacheRead],
      ["generated output", this.usage.output],
      ["↳ reasoning", this.usage.reasoning],
      ["cache write", this.usage.cacheWrite],
    ] as const;
    const maximum = Math.max(1, ...rows.map(([, value]) => value ?? 0));
    return html`<div class="chart" aria-label="Token usage by type">
      ${rows.map(([label, value]) => html`
        <div class="row">
          <span class="label">${label}</span>
          <div class="track"><div class="bar" style=${`width:${((value ?? 0) / maximum) * 100}%`}></div></div>
          <span class="value">${value === null ? "N/A" : value.toLocaleString()}</span>
        </div>
      `)}
      <small>Categories are source-reported. Cache and reasoning values may overlap their parent totals.</small>
    </div>`;
  }
}

declare global { interface HTMLElementTagNameMap { "am-token-chart": TokenChart } }
