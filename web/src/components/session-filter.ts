import { css, html, nothing, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { live } from "lit/directives/live.js";
import type { TelemetrySource } from "../model/telemetry";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";

@customElement("am-session-filter")
export class SessionFilter extends LocalizedElement {
  @property({ attribute: false }) sources: readonly TelemetrySource[] = [];
  @property() selectedSource = "";
  @property() search = "";
  @state() private sourceMenuOpen = false;
  private readonly knownSources = new Map<string, TelemetrySource>();
  private searchTimer?: number;

  static styles = css`
    :host { display: block; margin-bottom: 14px; }
    .filters { display: grid; grid-template-columns: minmax(180px, .72fr) minmax(260px, 1.28fr); gap: 12px; align-items: end; }
    :host([compact]) .filters { grid-template-columns: minmax(0, 1fr); gap: 9px; }
    label, .source-filter { display: grid; min-width: 0; gap: 6px; }
    .filter-label { color: var(--am-muted); font: 700 12px/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-transform: uppercase; letter-spacing: .1em; }
    .source-picker { position: relative; min-width: 0; }
    .source-trigger { display: grid; width: 100%; min-height: 38px; grid-template-columns: 20px minmax(0, 1fr) 16px; align-items: center; gap: 9px; border: 1px solid var(--am-border); border-radius: 8px; padding: 7px 10px; color: var(--am-text); background: var(--am-surface-raised); cursor: pointer; font: 650 13px/1.35 inherit; text-align: left; transition: border-color .18s ease, background .18s ease, box-shadow .18s ease; }
    .source-trigger:hover, .source-trigger[aria-expanded="true"] { border-color: var(--am-border-strong); background: var(--am-surface-strong); }
    .source-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .source-indicator { position: relative; display: grid; width: 18px; height: 18px; place-items: center; border: 1px solid color-mix(in srgb, var(--am-accent) 36%, var(--am-border)); border-radius: 6px; background: var(--am-accent-soft); }
    .source-indicator::before { width: 6px; height: 6px; border-radius: 50%; background: var(--am-accent); content: ""; box-shadow: 0 0 0 3px color-mix(in srgb, var(--am-accent) 14%, transparent); }
    .source-indicator[data-source="claude"] { border-color: color-mix(in srgb, #d97745 44%, var(--am-border)); background: color-mix(in srgb, #d97745 12%, var(--am-surface-raised)); }
    .source-indicator[data-source="claude"]::before { background: #d97745; box-shadow: 0 0 0 3px color-mix(in srgb, #d97745 14%, transparent); }
    .source-indicator[data-source=""]::before { width: 3px; height: 3px; background: var(--am-accent); box-shadow: 6px 0 var(--am-muted), 0 6px var(--am-muted), 6px 6px var(--am-accent); transform: translate(-3px, -3px); }
    .source-chevron { width: 15px; height: 15px; color: var(--am-muted); transition: transform .18s ease; }
    .source-trigger[aria-expanded="true"] .source-chevron { transform: rotate(180deg); }
    .source-menu { position: absolute; z-index: 20; top: calc(100% + 6px); left: 0; display: grid; width: 100%; min-width: 180px; max-height: min(280px, calc(100dvh - 120px)); gap: 2px; box-sizing: border-box; margin: 0; padding: 6px; overflow-y: auto; overscroll-behavior: contain; border: 1px solid var(--am-border-strong); border-radius: 10px; background: var(--am-surface-raised); box-shadow: 0 14px 32px rgba(0, 0, 0, .20), 0 2px 8px rgba(0, 0, 0, .12); }
    .source-option { display: grid; width: 100%; min-width: 0; grid-template-columns: 20px minmax(0, 1fr) 18px; align-items: center; gap: 9px; margin: 0; padding: 8px 9px; border: 0; border-radius: 7px; color: var(--am-muted); background: transparent; cursor: pointer; font: 650 13px/1.35 inherit; text-align: left; transition: color .14s ease, background .14s ease; }
    .source-option:hover, .source-option:focus-visible { color: var(--am-text); background: var(--am-surface-strong); outline: none; }
    .source-option:focus-visible { box-shadow: inset 0 0 0 1px var(--am-accent); }
    .source-option[aria-checked="true"] { color: var(--am-text); background: var(--am-accent-soft); }
    .source-check { width: 16px; height: 16px; color: var(--am-accent); opacity: 0; }
    .source-option[aria-checked="true"] .source-check { opacity: 1; }
    input { min-width: 0; width: 100%; border: 1px solid var(--am-border); border-radius: 7px; background: var(--am-surface-raised); color: var(--am-text); padding: 9px 10px; font: 14px/1.35 inherit; transition: border-color .18s ease, box-shadow .18s ease; }
    input:hover { border-color: color-mix(in srgb, var(--am-border) 55%, var(--am-accent)); }
    .source-trigger:focus-visible, input:focus { border-color: var(--am-accent); box-shadow: 0 0 0 3px var(--am-accent-soft); outline: none; }
    input::placeholder { color: color-mix(in srgb, var(--am-muted) 72%, transparent); }
    @media (max-width: 560px) { .filters { grid-template-columns: 1fr; gap: 9px; } }
    @media (prefers-reduced-motion: reduce) { .source-option, .source-trigger, .source-chevron, input { transition: none; } }
  `;

  render() {
    const sources = [{ id: "", label: localization.t("filter.allSources") }, ...this.knownSources.values()];
    const selected = sources.find(({ id }) => id === this.selectedSource) ?? sources[0];
    return html`<div class="filters">
      <div class="source-filter"><span class="filter-label">${localization.t("filter.source")}</span><div class="source-picker" @keydown=${this.sourceKeydown}>
        <button class="source-trigger" type="button" aria-haspopup="menu" aria-expanded=${String(this.sourceMenuOpen)} aria-controls="source-menu" @click=${this.toggleSourceMenu}>
          ${this.sourceIndicator(selected.id)}<span class="source-name">${selected.label}</span>${this.chevronIcon()}
        </button>
        ${this.sourceMenuOpen ? html`<div id="source-menu" class="source-menu" role="menu" aria-label=${localization.t("filter.source")}>
          ${sources.map((source) => html`<button class="source-option" data-source=${source.id} type="button" role="menuitemradio" aria-checked=${String(source.id === this.selectedSource)} @click=${() => this.selectSource(source.id)}>
            ${this.sourceIndicator(source.id)}<span class="source-name">${source.label}</span>${this.checkIcon()}
          </button>`)}
        </div>` : nothing}
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

  connectedCallback() {
    super.connectedCallback();
    document.addEventListener("pointerdown", this.documentPointerDown);
  }

  private selectSource(sourceId: string) {
    this.sourceMenuOpen = false;
    this.dispatchEvent(new CustomEvent("source-selected", {
      detail: { sourceId }, bubbles: true, composed: true,
    }));
    void this.updateComplete.then(() => this.shadowRoot?.querySelector<HTMLButtonElement>(".source-trigger")?.focus());
  }

  private readonly toggleSourceMenu = () => {
    this.sourceMenuOpen = !this.sourceMenuOpen;
  };

  private readonly documentPointerDown = (event: PointerEvent) => {
    if (this.sourceMenuOpen && !event.composedPath().includes(this)) this.sourceMenuOpen = false;
  };

  private readonly sourceKeydown = (event: KeyboardEvent) => {
    if (event.key === "Escape" && this.sourceMenuOpen) {
      event.preventDefault();
      this.sourceMenuOpen = false;
      void this.updateComplete.then(() => this.shadowRoot?.querySelector<HTMLButtonElement>(".source-trigger")?.focus());
      return;
    }
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    event.preventDefault();
    if (!this.sourceMenuOpen) {
      this.sourceMenuOpen = true;
      void this.updateComplete.then(() => this.focusSourceOption(event.key === "ArrowUp" ? -1 : 0));
      return;
    }
    const options = [...(this.shadowRoot?.querySelectorAll<HTMLButtonElement>(".source-option") ?? [])];
    const activeIndex = options.indexOf(this.shadowRoot?.activeElement as HTMLButtonElement);
    if (activeIndex < 0) this.focusSourceOption(event.key === "ArrowDown" ? 0 : -1);
    else this.focusSourceOption(event.key === "ArrowDown" ? activeIndex + 1 : activeIndex - 1);
  };

  private focusSourceOption(index: number) {
    const options = [...(this.shadowRoot?.querySelectorAll<HTMLButtonElement>(".source-option") ?? [])];
    if (!options.length) return;
    options[(index + options.length) % options.length].focus();
  }

  private sourceIndicator(sourceId: string) {
    return html`<span class="source-indicator" data-source=${sourceId} aria-hidden="true"></span>`;
  }

  private chevronIcon() {
    return html`<svg class="source-chevron" aria-hidden="true" viewBox="0 0 16 16" fill="none"><path d="m4 6 4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
  }

  private checkIcon() {
    return html`<svg class="source-check" aria-hidden="true" viewBox="0 0 16 16" fill="none"><path d="m3.5 8.25 2.75 2.75 6.25-6.25" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
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
	document.removeEventListener("pointerdown", this.documentPointerDown);
	super.disconnectedCallback();
  }
}

declare global { interface HTMLElementTagNameMap { "am-session-filter": SessionFilter } }
