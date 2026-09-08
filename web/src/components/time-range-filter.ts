import { css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { TimeRange } from "../model/telemetry";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";

export type RangeSelectedDetail = Readonly<{ range: TimeRange }>;

@customElement("am-time-range-filter")
export class TimeRangeFilter extends LocalizedElement {
  @property() selected: TimeRange = "24h";

  static styles = css`
    :host {
      display: inline-flex;
      padding: 4px;
      border: 1px solid var(--am-border, #25314a);
      border-radius: 9px;
      background: var(--am-surface);
    }

    button {
      border: 0;
      border-radius: 6px;
      padding: 8px 13px;
      color: var(--am-muted, #91a0b8);
      background: transparent;
      cursor: pointer;
      font: 700 .875rem/1.2 "SFMono-Regular", "Cascadia Code", monospace;
      transition: color .18s ease, background .18s ease;
    }

    button[aria-pressed="true"] {
      color: var(--am-paper, #07110f);
      background: var(--am-accent, #6df4d6);
    }

    button:hover:not([aria-pressed="true"]), button:focus-visible:not([aria-pressed="true"]) { color: var(--am-text); background: var(--am-surface-strong); outline: none; }
    @media (prefers-reduced-motion: reduce) { button { transition: none; } }
  `;

  render() {
    return html`${(["1h", "24h", "7d"] as const).map(
      (range) => html`
        <button
          type="button"
          data-range=${range}
          aria-pressed=${String(this.selected === range)}
          @click=${() => this.select(range)}
        >
          ${localization.t(`range.${range}`)}
        </button>
      `,
    )}`;
  }

  private select(range: TimeRange) {
    this.dispatchEvent(
      new CustomEvent<RangeSelectedDetail>("range-selected", {
        detail: { range },
        bubbles: true,
        composed: true,
      }),
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "am-time-range-filter": TimeRangeFilter;
  }
}
