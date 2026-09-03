import { LitElement, css, html } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import type { InvestigationFilters } from "../model/investigation-conditions";
import { deleteFilter, loadSavedFilters, replaceFilter, saveFilter, type SavedFilter } from "../model/saved-filters";

@customElement("am-saved-filters")
export class SavedFilters extends LitElement {
  @property({ attribute: false }) filters: InvestigationFilters = { range: "24h", sourceId: "", search: "" };
  @property({ type: Boolean }) confirmed = true;
  @property({ type: Boolean }) pending = false;
  @state() private saved: readonly SavedFilter[] = [];
  @state() private selectedName = "";
  @state() private name = "";
  @state() private error = "";
  @state() private status = "";

  static styles = css`
    :host { display: block; min-width: 0; margin: 12px 0; }
    section { border: 1px solid var(--am-border); border-radius: 8px; padding: 12px; }
    h3 { margin: 0 0 8px; color: var(--am-text); font-size: .85rem; }
    p { margin: 8px 0; color: var(--am-muted); font-size: .75rem; line-height: 1.5; overflow-wrap: anywhere; }
    .controls { display: flex; align-items: end; gap: 8px; flex-wrap: wrap; margin: 8px 0; }
    label { display: grid; gap: 5px; color: var(--am-muted); font-size: .72rem; min-width: 0; flex: 1 1 13rem; }
    input, select, button { box-sizing: border-box; min-width: 0; border: 1px solid var(--am-border); border-radius: 5px; background: var(--am-surface-raised); color: var(--am-text); padding: 8px; font: inherit; font-size: .76rem; }
    input, select { width: 100%; }
    button { cursor: pointer; }
    button:disabled { opacity: .5; cursor: default; }
    input:focus-visible, select:focus-visible, button:focus-visible { outline: 2px solid var(--am-accent); outline-offset: 2px; }
    [role="alert"] { color: var(--am-text); }
  `;

  connectedCallback() {
    super.connectedCallback();
    this.reload();
  }

  protected updated() {
    const select = this.shadowRoot?.querySelector<HTMLSelectElement>("select");
    if (select && select.value !== this.selectedName) select.value = this.selectedName;
  }

  render() {
    return html`<section aria-labelledby="saved-filters-heading">
      <h3 id="saved-filters-heading">Saved filters</h3>
      <p>Save currently applied conditions. Relative time ranges are evaluated when applied.</p>
      <div class="controls"><label>Filter name<input name="filter-name" maxlength="80" .value=${this.name} @input=${(event: Event) => { this.name = (event.target as HTMLInputElement).value; }}></label>
        <button type="button" data-action="save" ?disabled=${!this.confirmed || this.pending} @click=${this.save}>Save applied conditions</button></div>
      <div class="controls"><label>Saved conditions<select .value=${this.selectedName} @change=${(event: Event) => { this.selectedName = (event.target as HTMLSelectElement).value; }}>
        <option value="">Choose saved filters</option>${this.saved.map((item) => html`<option value=${item.name}>${item.name}</option>`)}
      </select></label>
        <button type="button" data-action="apply" ?disabled=${!this.selectedName || this.pending} @click=${this.apply}>Apply</button>
        <button type="button" data-action="replace" ?disabled=${!this.selectedName || !this.confirmed || this.pending} @click=${this.replace}>Replace with applied conditions</button>
        <button type="button" data-action="delete" ?disabled=${!this.selectedName} @click=${this.delete}>Delete</button>
      </div>
      ${!this.confirmed || this.pending ? html`<p>Apply the current conditions successfully before saving or replacing a filter.</p>` : null}
      ${this.error ? html`<p role="alert">${this.error}</p><button type="button" @click=${this.reload}>Reload saved filters</button>` : null}
      ${this.status ? html`<p role="status">${this.status}</p>` : null}
    </section>`;
  }

  private reload() {
    this.status = "";
    try {
      this.saved = loadSavedFilters(window.localStorage);
      if (!this.saved.some((item) => item.name === this.selectedName)) this.selectedName = "";
      this.error = "";
    } catch (error) { this.error = error instanceof Error ? error.message : "Saved filters are unavailable."; }
  }

  private save() {
    if (!this.confirmed || this.pending) return;
    this.persist(() => saveFilter(window.localStorage, this.name, this.filters), `Saved “${this.name.trim()}”.`, this.name.trim());
  }

  private replace() {
    if (!this.confirmed || this.pending) return;
    this.persist(() => replaceFilter(window.localStorage, this.selectedName, this.filters), `Replaced “${this.selectedName}”.`, this.selectedName);
  }

  private delete() {
    this.persist(() => deleteFilter(window.localStorage, this.selectedName), `Deleted “${this.selectedName}”.`, "");
  }

  private persist(operation: () => readonly SavedFilter[], success: string, selection: string) {
    this.status = "";
    this.error = "";
    try {
      const saved = operation();
      this.saved = saved;
      this.selectedName = selection;
      this.status = success;
    } catch (error) { this.error = error instanceof Error ? error.message : "The saved filter change could not be persisted."; }
  }

  private apply() {
    if (this.pending) return;
    this.status = "";
    this.error = "";
    try {
      const selected = loadSavedFilters(window.localStorage).find((item) => item.name === this.selectedName);
      if (!selected) throw new Error("The saved filter no longer exists.");
      this.dispatchEvent(new CustomEvent("investigation-filters-requested", { detail: { filters: selected.filters }, bubbles: true, composed: true }));
    } catch (error) { this.error = error instanceof Error ? error.message : "The saved filter could not be loaded."; }
  }
}

declare global { interface HTMLElementTagNameMap { "am-saved-filters": SavedFilters } }
