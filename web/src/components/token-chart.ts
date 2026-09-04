import { css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { TokenUsage } from "../model/telemetry";
import { notReported } from "../presentation/missing-data";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";
import type { MessageKey } from "../localization/messages";

const emptyUsage: TokenUsage = {
  input: null,
  output: null,
  cacheRead: null,
  cacheWrite: null,
  reasoning: null,
  total: null,
};

@customElement("am-token-chart")
export class TokenChart extends LocalizedElement {
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
    const rows: readonly (readonly [MessageKey, number | null])[] = [
      ["common.input", this.usage.input],
      ["common.output", this.usage.output],
      ["common.cacheRead", this.usage.cacheRead],
      ["common.reasoning", this.usage.reasoning],
      ["common.cacheWrite", this.usage.cacheWrite],
    ] as const;
    const maximum = Math.max(1, ...rows.map(([, value]) => value ?? 0));
    const total = this.usage.total === null ? notReported() : localization.number(this.usage.total);
    return html`<div class="chart" aria-label=${localization.t("tokens.chartAria")}>
      <div class="total"><span>${localization.t("tokens.observedTotal")}</span><strong>${total}</strong></div>
      <div class="group">${localization.t("tokens.primaryUsage")}</div>
      ${rows.slice(0, 2).map(([label, value]) => html`
        <div class="row">
          <span class="label">${localization.t(label)}</span>
          <div class="track"><div class="bar" style=${`width:${((value ?? 0) / maximum) * 100}%`}></div></div>
          <span class="value">${value === null ? notReported() : localization.number(value)}</span>
        </div>
      `)}
      <div class="group">${localization.t("tokens.additionalEvidence")}</div>
      ${rows.slice(2).map(([label, value]) => html`
        <div class="row">
          <span class="label modifier">${localization.t(label)}</span>
          <div class="track"><div class="bar" style=${`width:${((value ?? 0) / maximum) * 100}%`}></div></div>
          <span class="value">${value === null ? notReported() : localization.number(value)}</span>
        </div>
      `)}
      <small>${localization.t("tokens.sourceNote")}</small>
    </div>`;
  }
}

declare global { interface HTMLElementTagNameMap { "am-token-chart": TokenChart } }
