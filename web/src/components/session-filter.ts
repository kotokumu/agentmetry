import { css, html, type PropertyValues } from "lit";
import { customElement, property } from "lit/decorators.js";
import { live } from "lit/directives/live.js";
import type { TelemetrySource } from "../model/telemetry";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";

@customElement("am-session-filter")
export class SessionFilter extends LocalizedElement {
  @property({ attribute: false }) sources: readonly TelemetrySource[] = [];
  @property() selectedSource = "";
  @property() search = "";
  private searchTimer?: number;

  static styles = css`
    :host { display: block; margin-bottom: 14px; }
    .filters { display: grid; gap: 9px; }
    label { display: grid; gap: 6px; color: var(--am-muted); font: 700 .62rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-transform: uppercase; letter-spacing: .1em; }
    select, input { min-width: 0; width: 100%; border: 1px solid var(--am-border); border-radius: 7px; background: rgba(7, 10, 15, .66); color: var(--am-text); padding: 8px 9px; font: .74rem/1.3 inherit; transition: border-color .18s ease, box-shadow .18s ease; }
    select:hover, input:hover { border-color: color-mix(in srgb, var(--am-border) 55%, var(--am-accent)); }
    select:focus, input:focus { border-color: var(--am-accent); box-shadow: 0 0 0 3px var(--am-accent-soft); outline: none; }
    input::placeholder { color: color-mix(in srgb, var(--am-muted) 72%, transparent); }
    @media (prefers-reduced-motion: reduce) { select, input { transition: none; } }
  `;

  render() {
    return html`<div class="filters">
      <label>${localization.t("filter.source")}<select .value=${live(this.selectedSource)} @change=${this.selectSource}>
        <option value="">${localization.t("filter.allSources")}</option>
        ${this.sources.map((source) => html`<option value=${source.id}>${source.label}</option>`)}
      </select></label>
      <label>${localization.t("filter.search")}
        <input type="search" .value=${live(this.search)} placeholder=${localization.t("filter.searchPlaceholder")} aria-label=${localization.t("filter.searchAria")} @input=${this.searchChanged}>
      </label>
    </div>`;
  }

  protected updated(changed: PropertyValues<this>) {
    if (changed.has("selectedSource") || changed.has("sources")) {
      const select = this.shadowRoot?.querySelector<HTMLSelectElement>("select");
      if (select && select.value !== this.selectedSource) select.value = this.selectedSource;
    }
  }

  private selectSource(event: Event) {
    const sourceId = (event.currentTarget as HTMLSelectElement).value;
    this.dispatchEvent(new CustomEvent("source-selected", {
      detail: { sourceId }, bubbles: true, composed: true,
    }));
  }

  private searchChanged(event: InputEvent) {
	const search = (event.currentTarget as HTMLInputElement).value.trim();
	if (this.searchTimer !== undefined) window.clearTimeout(this.searchTimer);
	this.searchTimer = window.setTimeout(() => {
		this.dispatchEvent(new CustomEvent("search-submitted", {
			detail: { search }, bubbles: true, composed: true,
		}));
	}, 250);
  }

  disconnectedCallback() {
	if (this.searchTimer !== undefined) window.clearTimeout(this.searchTimer);
	super.disconnectedCallback();
  }
}

declare global { interface HTMLElementTagNameMap { "am-session-filter": SessionFilter } }
