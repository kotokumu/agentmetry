import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { TelemetrySource } from "../model/update";

@customElement("am-session-filter")
export class SessionFilter extends LitElement {
  @property({ attribute: false }) sources: readonly TelemetrySource[] = [];
  @property() selectedSource = "";
  @property() search = "";
  private searchTimer?: number;

  static styles = css`
    :host { display: block; margin-bottom: 14px; }
    .filters { display: grid; gap: 8px; }
    label { display: grid; gap: 4px; color: var(--am-muted); font-size: .68rem; text-transform: uppercase; letter-spacing: .06em; }
    select, input { min-width: 0; width: 100%; border: 1px solid var(--am-border); border-radius: 8px; background: var(--am-surface); color: var(--am-text); padding: 8px 9px; font: .76rem/1.3 inherit; }
  `;

  render() {
    return html`<div class="filters">
      <label>Source<select .value=${this.selectedSource} @change=${this.selectSource}>
        <option value="">All sources</option>
        ${this.sources.map((source) => html`<option value=${source.id}>${source.label}</option>`)}
      </select></label>
      <label>Search
        <input type="search" .value=${this.search} placeholder="Prompts, messages, tools…" aria-label="Search conversations" @input=${this.searchChanged}>
      </label>
    </div>`;
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
