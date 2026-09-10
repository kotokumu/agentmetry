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
  private readonly knownSources = new Map<string, TelemetrySource>();
  private searchTimer?: number;

  static styles = css`
    :host { display: block; margin-bottom: 14px; }
    .filters { display: grid; grid-template-columns: minmax(180px, .72fr) minmax(260px, 1.28fr); gap: 12px; align-items: end; }
    :host([compact]) .filters { grid-template-columns: minmax(0, 1fr); gap: 9px; }
    label, .source-filter { display: grid; min-width: 0; gap: 6px; }
    .filter-label { color: var(--am-muted); font: 700 12px/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-transform: uppercase; letter-spacing: .1em; }
    .source-options { display: flex; min-width: 0; flex-wrap: wrap; gap: 3px; padding: 3px; border: 1px solid var(--am-border); border-radius: 9px; background: var(--am-surface-raised); }
    .source-option { display: inline-flex; min-width: 0; flex: 1 1 auto; align-items: center; justify-content: center; gap: 7px; margin: 0; padding: 6px 9px; border: 1px solid transparent; border-radius: 6px; color: var(--am-muted); background: transparent; cursor: pointer; font: 650 12px/1.35 inherit; white-space: nowrap; transition: color .18s ease, border-color .18s ease, background .18s ease, box-shadow .18s ease; }
    .source-option::before { width: 6px; height: 6px; flex: 0 0 auto; border: 1px solid currentColor; border-radius: 50%; content: ""; opacity: .7; }
    .source-option:hover { color: var(--am-text); background: var(--am-surface-strong); }
    .source-option[aria-pressed="true"] { border-color: var(--am-border-strong); color: var(--am-text); background: var(--am-accent-soft); box-shadow: inset 0 0 0 1px rgba(var(--am-accent-rgb), .08); }
    .source-option[aria-pressed="true"]::before { border-color: var(--am-accent); background: var(--am-accent); box-shadow: 0 0 0 3px var(--am-accent-soft); opacity: 1; }
    input { min-width: 0; width: 100%; border: 1px solid var(--am-border); border-radius: 7px; background: var(--am-surface-raised); color: var(--am-text); padding: 9px 10px; font: 14px/1.35 inherit; transition: border-color .18s ease, box-shadow .18s ease; }
    input:hover { border-color: color-mix(in srgb, var(--am-border) 55%, var(--am-accent)); }
    .source-option:focus-visible, input:focus { border-color: var(--am-accent); box-shadow: 0 0 0 3px var(--am-accent-soft); outline: none; }
    input::placeholder { color: color-mix(in srgb, var(--am-muted) 72%, transparent); }
    @media (max-width: 560px) { .filters { grid-template-columns: 1fr; gap: 9px; } }
    @media (prefers-reduced-motion: reduce) { .source-option, input { transition: none; } }
  `;

  render() {
    return html`<div class="filters">
      <div class="source-filter"><span class="filter-label">${localization.t("filter.source")}</span><div class="source-options" role="group" aria-label=${localization.t("filter.source")}>
        ${[{ id: "", label: localization.t("filter.allSources") }, ...this.knownSources.values()].map((source) => html`<button class="source-option" data-source=${source.id} type="button" aria-pressed=${String(source.id === this.selectedSource)} @click=${() => this.selectSource(source.id)}>${source.label}</button>`)}
      </div></div>
      <label><span class="filter-label">${localization.t("filter.search")}</span>
        <input type="search" .value=${live(this.search)} placeholder=${localization.t("filter.searchPlaceholder")} aria-label=${localization.t("filter.searchAria")} @input=${this.searchChanged}>
      </label>
    </div>`;
  }

  protected willUpdate(changed: PropertyValues<this>) {
    if (changed.has("sources")) for (const source of this.sources) this.knownSources.set(source.id, source);
    if (this.selectedSource && !this.knownSources.has(this.selectedSource)) {
      this.knownSources.set(this.selectedSource, { id: this.selectedSource, label: this.selectedSource });
    }
  }

  private selectSource(sourceId: string) {
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
