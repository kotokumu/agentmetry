import { msg } from "@lit/localize";
import { css, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import { LocalizedElement } from "../localization/localized-element";
import {
  getThemePreference, initializeThemePreference, setThemePreference, THEME_PREFERENCE_EVENT,
  type ThemePreference,
} from "./theme-preference";

const themeLabel = (preference: ThemePreference) => {
  switch (preference) {
    case "system": return msg("System", { id: "settingsCompletion.themeSystem" });
    case "light": return msg("Light", { id: "settingsCompletion.themeLight" });
    case "dark": return msg("Dark", { id: "settingsCompletion.themeDark" });
  }
};

@customElement("am-appearance-settings")
export class AppearanceSettings extends LocalizedElement {
  @state() private preference: ThemePreference = "system";

  connectedCallback() {
    super.connectedCallback();
    initializeThemePreference();
    this.preference = getThemePreference();
    window.addEventListener(THEME_PREFERENCE_EVENT, this.preferenceChanged);
  }

  disconnectedCallback() {
    window.removeEventListener(THEME_PREFERENCE_EVENT, this.preferenceChanged);
    super.disconnectedCallback();
  }

  static styles = css`
    :host { display: block; }
    fieldset { display: grid; gap: 8px; margin: 0; border: 0; padding: 0; }
    legend { margin-bottom: 4px; color: var(--am-text); font-weight: 650; }
    .options { display: flex; flex-wrap: wrap; gap: 8px; }
    label { display: inline-flex; align-items: center; gap: 7px; border: 1px solid var(--am-border); border-radius: 7px; padding: 8px 11px; color: var(--am-muted); background: var(--am-surface); font-size: 14px; cursor: pointer; }
    label:has(input:checked) { border-color: var(--am-border-strong); color: var(--am-text); background: var(--am-accent-soft); }
    input { accent-color: var(--am-accent); }
  `;

  render() {
    return html`<fieldset>
      <legend>${msg("Appearance", { id: "settingsCompletion.appearance" })}</legend>
      <div class="options" role="radiogroup" aria-label=${msg("Appearance", { id: "settingsCompletion.appearanceAria" })}>
        ${(Object.keys({ system: true, light: true, dark: true }) as ThemePreference[]).map((preference) => html`<label>
          <input type="radio" name="theme" value=${preference} .checked=${this.preference === preference} @change=${this.selected}>
          ${themeLabel(preference)}
        </label>`)}
      </div>
    </fieldset>`;
  }

  private readonly selected = (event: Event) => {
    const preference = (event.currentTarget as HTMLInputElement).value;
    if (preference !== "system" && preference !== "light" && preference !== "dark") return;
    this.preference = preference;
    setThemePreference(preference);
  };

  private readonly preferenceChanged = (event: Event) => {
    const preference = (event as CustomEvent<{ preference?: ThemePreference }>).detail?.preference;
    if (!preference || preference === this.preference) return;
    this.preference = preference;
  };
}

declare global { interface HTMLElementTagNameMap { "am-appearance-settings": AppearanceSettings } }
