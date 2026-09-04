import { css, html } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";

let nextHelpID = 0;

@customElement("am-kpi-card")
export class KpiCard extends LocalizedElement {
  @property() label = "";
  @property() value = "0";
  @property() hint = "";
  @property() description = "";
  @state() private helpOpen = false;
  private helpPinned = false;
  private readonly helpID = `am-kpi-help-${++nextHelpID}`;

  static styles = css`
    :host {
      display: block;
      position: relative;
      min-width: 0;
    }
    :host(:hover), :host(:focus-within) { z-index: 10; }

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
      font: 600 .8rem/1.4 "SFMono-Regular", "Cascadia Code", monospace;
      letter-spacing: .01em;

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
      font-size: .8rem;
      line-height: 1.45;
    }

    .help-wrap { position: absolute; z-index: 3; top: 10px; right: 10px; }
    .help {
      display: grid;
      width: 22px;
      height: 22px;
      place-items: center;
      border: 1px solid var(--am-border, #25314a);
      border-radius: 50%;
      background: rgba(8, 13, 20, .84);
      color: var(--am-muted, #91a0b8);
      cursor: help;
      font: 700 .68rem/1 "SFMono-Regular", "Cascadia Code", monospace;
    }
    .help:hover, .help:focus-visible, .help[aria-expanded="true"] {
      border-color: var(--am-accent, #6df4d6);
      color: var(--am-accent, #6df4d6);
      outline: 2px solid var(--am-accent-soft, rgba(109, 244, 214, .16));
      outline-offset: 2px;
    }
    .tooltip {
      position: absolute;
      top: calc(100% + 8px);
      right: 0;
      width: min(270px, calc(100vw - 56px));
      visibility: hidden;
      border: 1px solid var(--am-border-strong, var(--am-border, #25314a));
      border-radius: 9px;
      padding: 10px 11px;
      background: #0b1119;
      box-shadow: 0 16px 36px rgba(0, 0, 0, .42);
      color: var(--am-text, #f3f7ff);
      font-size: .8rem;
      line-height: 1.5;
      opacity: 0;
      pointer-events: none;
      transform: translateY(-3px);
      transition: opacity .14s ease, transform .14s ease, visibility .14s;
    }
    .tooltip[data-open] { visibility: visible; opacity: 1; transform: translateY(0); }

    @media (prefers-reduced-motion: reduce) { article, .tooltip { transition: none; } article:hover { transform: none; } }
    @media (max-width: 480px) {
      article { min-height: 100px; padding: 14px; }
      strong { font-size: 1.35rem; }
      p { font-size: .8rem; }
    }
  `;

  render() {
    return html`
      <article aria-label=${this.label}>
        <p>${this.label}</p>
        <strong>${this.value}</strong>
        ${this.hint ? html`<small>${this.hint}</small>` : null}
      </article>
      ${this.description ? html`<span class="help-wrap">
        <button
          class="help"
          type="button"
          aria-label=${localization.t("kpi.explain", { label: this.label })}
          aria-controls=${this.helpID}
          aria-expanded=${String(this.helpOpen)}
          @mouseenter=${this.previewHelp}
          @mouseleave=${this.leaveHelp}
          @focus=${this.previewHelp}
          @blur=${this.closeHelp}
          @click=${this.toggleHelp}
          @keydown=${this.helpKeydown}
        >?</button>
        <span id=${this.helpID} class="tooltip" role="tooltip" aria-hidden=${String(!this.helpOpen)} ?data-open=${this.helpOpen}>${this.description}</span>
      </span>` : null}
    `;
  }

  private previewHelp = () => { this.helpOpen = true; };
  private leaveHelp = () => { if (!this.helpPinned) this.helpOpen = false; };
  private toggleHelp = () => {
    this.helpPinned = !this.helpPinned;
    this.helpOpen = this.helpPinned;
  };
  private closeHelp = () => {
    this.helpPinned = false;
    this.helpOpen = false;
  };
  private helpKeydown = (event: KeyboardEvent) => {
    if (event.key !== "Escape") return;
    event.preventDefault();
    this.closeHelp();
  };
}

declare global {
  interface HTMLElementTagNameMap {
    "am-kpi-card": KpiCard;
  }
}
