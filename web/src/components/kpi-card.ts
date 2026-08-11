import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";

@customElement("am-kpi-card")
export class KpiCard extends LitElement {
  @property() label = "";
  @property() value = "0";
  @property() hint = "";

  static styles = css`
    :host {
      display: block;
      min-width: 0;
    }

    article {
      border: 1px solid var(--am-border, #25314a);
      border-radius: 14px;
      background: var(--am-surface, #121a2b);
      padding: 18px;
    }

    p {
      margin: 0;
      color: var(--am-muted, #91a0b8);
      font-size: 0.78rem;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }

    strong {
      display: block;
      margin-top: 8px;
      color: var(--am-text, #f3f7ff);
      font-size: clamp(1.35rem, 3vw, 2rem);
      line-height: 1;
    }

    small {
      display: block;
      margin-top: 8px;
      color: var(--am-muted, #91a0b8);
    }
  `;

  render() {
    return html`
      <article aria-label=${this.label}>
        <p>${this.label}</p>
        <strong>${this.value}</strong>
        ${this.hint ? html`<small>${this.hint}</small>` : null}
      </article>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "am-kpi-card": KpiCard;
  }
}

