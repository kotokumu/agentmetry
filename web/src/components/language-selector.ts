import { css, html } from "lit";
import { customElement } from "lit/decorators.js";
import { LocalizedElement } from "../localization/localized-element";
import { localization, supportedLocales } from "../localization/localization";

@customElement("am-language-selector")
export class LanguageSelector extends LocalizedElement {
  static styles = css`
    :host { display: block; }
    label { display: flex; min-height: 31px; align-items: center; gap: 7px; border: 1px solid var(--am-border); border-radius: 8px; padding: 0 8px 0 10px; color: var(--am-muted); background: rgba(18, 25, 35, .82); font: 700 .64rem/1 "SFMono-Regular", "Cascadia Code", monospace; }
    select { max-width: 92px; border: 0; padding: 7px 18px 7px 2px; color: var(--am-text); background: transparent; font: inherit; cursor: pointer; }
    label:hover, label:focus-within { border-color: var(--am-border-strong); color: var(--am-accent); background: var(--am-accent-soft); }
    select:focus { outline: none; }
  `;

  render() {
    return html`<label><span>${localization.t("language.label")}</span><select
      aria-label=${localization.t("language.label")}
      @change=${this.localeSelected}
    >${supportedLocales.map(({ code, name }) => html`<option value=${code} ?selected=${code === localization.locale}>${name}</option>`)}</select></label>`;
  }

  private readonly localeSelected = (event: Event) => {
    void localization.select((event.currentTarget as HTMLSelectElement).value);
  };
}

declare global { interface HTMLElementTagNameMap { "am-language-selector": LanguageSelector } }
