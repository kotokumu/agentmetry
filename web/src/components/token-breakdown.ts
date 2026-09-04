import { css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { TokenUsage } from "../model/telemetry";
import { notReported } from "../presentation/missing-data";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";

const emptyUsage: TokenUsage = {
  input: null,
  output: null,
  cacheRead: null,
  cacheWrite: null,
  reasoning: null,
  total: null,
};

@customElement("am-token-breakdown")
export class TokenBreakdown extends LocalizedElement {
  @property({ attribute: false }) usage: TokenUsage = emptyUsage;
  @property({ type: Boolean, reflect: true }) compact = false;

  static styles = css`
    :host { display: block; min-width: 0; max-width: 100%; }
    .summary { min-width: 0; }
    .total-line { display: flex; align-items: baseline; flex-wrap: wrap; gap: 4px 8px; min-width: 0; }
    .total { color: var(--am-text); font-weight: 700; white-space: nowrap; }
    .unit, .partial, summary { color: var(--am-muted); font-size: .68rem; }
    details { margin-top: 4px; }
    summary { width: fit-content; cursor: pointer; color: var(--am-accent); }
    .grid { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 4px 12px; box-sizing: border-box; max-width: 100%; min-width: 0; margin-top: 7px; padding: 9px 11px; border: 1px solid var(--am-border); border-left: 2px solid var(--am-accent); border-radius: 0 7px 7px 0; background: var(--am-surface-strong); }
    .label { min-width: 0; color: var(--am-muted); font-size: .68rem; overflow-wrap: anywhere; }
    .value { min-width: 0; color: var(--am-text); font: .7rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; overflow-wrap: anywhere; text-align: right; }
    :host([compact]) .grid { gap-inline: 6px; padding: 6px 8px; }
    :host([compact]) details { margin-top: 2px; }
  `;

  render() {
    const rows = tokenRows(this.usage);
    const hasUsage = rows.length > 0;
    return html`<div class="summary" aria-label=${localization.t("tokens.aria")}>
      <div class="total-line">
        <strong class="total">${this.usage.total === null ? hasUsage ? localization.t("common.partial") : notReported() : localization.number(this.usage.total)}</strong>
        ${this.usage.total !== null ? html`<span class="unit">${localization.t("common.tokens")}</span>` : hasUsage ? html`<span class="partial">${localization.t("common.usage")}</span>` : null}
      </div>
      ${hasUsage ? html`<details @toggle=${this.breakdownToggled}><summary>${localization.t("common.breakdown")}</summary><div class="grid">${rows.map(([label, value]) => html`<span class="label">${localization.t(label)}</span><strong class="value">${localization.number(value)}</strong>`)}</div></details>` : null}
    </div>`;
  }

  private breakdownToggled(event: Event) {
    this.dispatchEvent(new CustomEvent("token-breakdown-toggle", {
      detail: { open: (event.currentTarget as HTMLDetailsElement).open },
      bubbles: true,
      composed: true,
    }));
  }
}

const tokenRows = (usage: TokenUsage): readonly (readonly ["common.input" | "common.output" | "common.cacheRead" | "common.cacheWrite" | "common.reasoning", number])[] => [
  ["common.input", usage.input],
  ["common.output", usage.output],
  ["common.cacheRead", usage.cacheRead],
  ["common.cacheWrite", usage.cacheWrite],
  ["common.reasoning", usage.reasoning],
].filter((entry): entry is ["common.input" | "common.output" | "common.cacheRead" | "common.cacheWrite" | "common.reasoning", number] => entry[1] !== null);

declare global { interface HTMLElementTagNameMap { "am-token-breakdown": TokenBreakdown } }
