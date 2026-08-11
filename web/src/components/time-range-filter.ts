import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { TimeRange } from "../model/update";

export type RangeSelectedDetail = Readonly<{ range: TimeRange }>;

@customElement("am-time-range-filter")
export class TimeRangeFilter extends LitElement {
  @property() selected: TimeRange = "24h";

  static styles = css`
    :host {
      display: inline-flex;
      padding: 3px;
      border: 1px solid var(--am-border, #25314a);
      border-radius: 10px;
      background: var(--am-surface, #121a2b);
    }

    button {
      border: 0;
      border-radius: 7px;
      padding: 7px 11px;
      color: var(--am-muted, #91a0b8);
      background: transparent;
      cursor: pointer;
      font: inherit;
    }

    button[aria-pressed="true"] {
      color: var(--am-text, #f3f7ff);
      background: var(--am-accent-soft, #243552);
    }
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
          ${range}
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

