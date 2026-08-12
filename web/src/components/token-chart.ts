import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { TokenUsage } from "../model/update";
import { NOT_REPORTED } from "../presentation/missing-data";

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
    .chart { display: grid; gap: 6px; }
    .total { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; padding: 0 0 9px; border-bottom: 1px solid var(--am-border); }
    .total span { color: var(--am-muted); font-size: .7rem; text-transform: uppercase; letter-spacing: .08em; }
    .total strong { color: var(--am-accent); font: 700 1.05rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-shadow: 0 0 18px rgba(var(--am-accent-rgb), .2); }
    .group { margin-top: 3px; color: var(--am-muted); font-size: .65rem; text-transform: uppercase; letter-spacing: .08em; }
    .row { display: grid; grid-template-columns: 92px 1fr 72px; align-items: center; gap: 10px; }
    .label, .value { font: 0.72rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; }
    .label { color: var(--am-muted); }
    .label.modifier { padding-left: 12px; }
    .value { text-align: right; color: var(--am-text); }
    .track { height: 7px; border-radius: 99px; background: var(--am-track); overflow: hidden; box-shadow: inset 0 1px 2px rgba(0, 0, 0, .32); }
    .bar { height: 100%; min-width: 2px; border-radius: inherit; background: linear-gradient(90deg, var(--am-accent), var(--am-secondary)); box-shadow: 0 0 10px rgba(var(--am-accent-rgb), .28); }
  `;

  render() {
    const rows: readonly (readonly [string, number | null])[] = [
      ["Input", this.usage.input],
      ["Output", this.usage.output],
      ["Cache read", this.usage.cacheRead],
      ["Reasoning", this.usage.reasoning],
      ["Cache write", this.usage.cacheWrite],
    ] as const;
    const maximum = Math.max(1, ...rows.map(([, value]) => value ?? 0));
    const total = this.usage.total === null ? NOT_REPORTED : this.usage.total.toLocaleString();
    return html`<div class="chart" aria-label="Token usage by type">
      <div class="total"><span>Observed total</span><strong>${total}</strong></div>
      <div class="group">Primary usage</div>
      ${rows.slice(0, 2).map(([label, value]) => html`
        <div class="row">
          <span class="label">${label}</span>
          <div class="track"><div class="bar" style=${`width:${((value ?? 0) / maximum) * 100}%`}></div></div>
          <span class="value">${value === null ? NOT_REPORTED : value.toLocaleString()}</span>
        </div>
      `)}
      <div class="group">Modifiers and additional evidence</div>
      ${rows.slice(2).map(([label, value]) => html`
        <div class="row">
          <span class="label modifier">${label}</span>
          <div class="track"><div class="bar" style=${`width:${((value ?? 0) / maximum) * 100}%`}></div></div>
          <span class="value">${value === null ? NOT_REPORTED : value.toLocaleString()}</span>
        </div>
      `)}
      <small>Categories are source-reported. Cache and reasoning values may overlap their parent totals.</small>
    </div>`;
  }
}

declare global { interface HTMLElementTagNameMap { "am-token-chart": TokenChart } }
