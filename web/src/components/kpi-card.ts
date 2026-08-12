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
      position: relative;
      min-height: 104px;
      overflow: hidden;
      border: 1px solid var(--am-border, #25314a);
      border-radius: 10px;
      background: linear-gradient(145deg, var(--am-surface-raised, #121a2b), var(--am-surface, #0d121a));
      padding: 15px 16px;
      box-shadow: inset 0 1px 0 rgba(255, 255, 255, .025), 0 16px 36px rgba(0, 0, 0, .12);
      transition: border-color .2s ease, transform .2s ease, box-shadow .2s ease;
    }

    article::before { content: ""; position: absolute; inset: 0 auto 0 0; width: 2px; background: linear-gradient(180deg, var(--am-accent), transparent 72%); }
    article::after { content: ""; position: absolute; width: 100px; height: 100px; top: -70px; right: -30px; border-radius: 50%; background: rgba(var(--am-accent-rgb, 109, 244, 214), .08); filter: blur(14px); }
    article:hover { border-color: var(--am-border-strong, var(--am-accent)); transform: translateY(-2px); box-shadow: 0 18px 42px rgba(0, 0, 0, .2), 0 0 24px rgba(var(--am-accent-rgb, 109, 244, 214), .04); }

    p {
      margin: 0;
      color: var(--am-muted, #91a0b8);
      font: 700 .66rem/1.2 "SFMono-Regular", "Cascadia Code", monospace;
      letter-spacing: 0.11em;
      text-transform: uppercase;
    }

    strong {
      display: block;
      margin-top: 8px;
      color: var(--am-text, #f3f7ff);
      font: 650 clamp(1.35rem, 2.4vw, 1.9rem)/1 "SFMono-Regular", "Cascadia Code", monospace;
      letter-spacing: -.06em;
      line-height: 1;
    }

    small {
      display: block;
      margin-top: 6px;
      color: var(--am-muted, #91a0b8);
      font-size: .7rem;
      line-height: 1.45;
    }

    @media (max-width: 480px) {
      article { min-height: 100px; padding: 14px; }
      strong { font-size: 1.35rem; }
      p { font-size: .6rem; }
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
