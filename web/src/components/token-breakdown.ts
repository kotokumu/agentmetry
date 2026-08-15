import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { TokenUsage } from "../model/telemetry";
import { NOT_REPORTED } from "../presentation/missing-data";

const emptyUsage: TokenUsage = {
  input: null,
  output: null,
  cacheRead: null,
  cacheWrite: null,
  reasoning: null,
  total: null,
};

@customElement("am-token-breakdown")
export class TokenBreakdown extends LitElement {
  @property({ attribute: false }) usage: TokenUsage = emptyUsage;
  @property({ type: Boolean, reflect: true }) compact = false;

  static styles = css`
    :host { display: block; min-width: 0; }
    .summary { display: flex; align-items: baseline; flex-wrap: wrap; gap: 4px 8px; min-width: 0; }
    .total { color: var(--am-text); font-weight: 700; white-space: nowrap; }
    .unit, .partial, summary { color: var(--am-muted); font-size: .68rem; }
    details { margin-top: 4px; }
    summary { width: fit-content; cursor: pointer; color: var(--am-accent); }
    .grid { display: grid; grid-template-columns: minmax(90px, 1fr) auto; gap: 4px 12px; margin-top: 7px; padding: 9px 11px; border: 1px solid var(--am-border); border-left: 2px solid var(--am-accent); border-radius: 0 7px 7px 0; background: var(--am-surface-strong); }
    .label { color: var(--am-muted); font-size: .68rem; }
    .value { color: var(--am-text); font: .7rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-align: right; }
    :host([compact]) .grid { padding: 6px 8px; }
    :host([compact]) details { margin-top: 2px; }
  `;

  render() {
    const rows = tokenRows(this.usage);
    const hasUsage = rows.length > 0;
    return html`<div class="summary" aria-label="Token usage">
      <strong class="total">${this.usage.total === null ? hasUsage ? "Partial" : NOT_REPORTED : this.usage.total.toLocaleString()}</strong>
      ${this.usage.total !== null ? html`<span class="unit">tokens</span>` : hasUsage ? html`<span class="partial">usage</span>` : null}
      ${hasUsage ? html`<details><summary>Breakdown</summary><div class="grid">${rows.map(([label, value]) => html`<span class="label">${label}</span><strong class="value">${value.toLocaleString()}</strong>`)}</div></details>` : null}
    </div>`;
  }
}

const tokenRows = (usage: TokenUsage): readonly (readonly [string, number])[] => [
  ["Input", usage.input],
  ["Output", usage.output],
  ["Cache read", usage.cacheRead],
  ["Cache write", usage.cacheWrite],
  ["Reasoning", usage.reasoning],
].filter((entry): entry is [string, number] => entry[1] !== null);

declare global { interface HTMLElementTagNameMap { "am-token-breakdown": TokenBreakdown } }
