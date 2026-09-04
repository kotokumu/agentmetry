import { LitElement, css, html, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import {
  conditionsKey,
  investigationFiltersKey,
  parseInvestigationFilters,
  type InvestigationFilters,
} from "../model/investigation-conditions";
import {
  deleteFilter,
  loadSavedFilters,
  replaceFilter,
  saveFilter,
  type SavedFilter,
} from "../model/saved-filters";

@customElement("am-investigation-filter")
export class InvestigationFilter extends LitElement {
  @property({ attribute: false }) filters: InvestigationFilters = {
    range: "24h",
    sourceId: "",
    search: "",
  };
  @property() error = "";
  @property({ type: Boolean }) confirmed = true;
  @property({ type: Boolean }) pending = false;
  @state() private draftError = "";
  @state() private savedError = "";
  @state() private status = "";
  @state() private saved: readonly SavedFilter[] = [];
  @state() private selectedName = "";
  @state() private selectedApplied = false;
  @state() private saveName = "";
  @state() private naming = false;
  private appliedKey = "";
  private selectedRequest?: { name: string; filters: InvestigationFilters; sawPending: boolean };

  static styles = css`
    :host { display: block; min-width: 0; margin: 12px 0; font-size: .85rem; }
    section { display: grid; gap: 10px; padding: 12px; border: 1px solid var(--am-border); border-radius: 8px; background: var(--am-surface); }
    details.editor { border-top: 1px solid var(--am-border); border-bottom: 1px solid var(--am-border); }
    details.editor > summary { cursor: pointer; padding: 10px 0; color: var(--am-text); font-weight: 600; }
    .applied { margin: 0; color: var(--am-muted); font-size: .75rem; line-height: 1.5; overflow-wrap: anywhere; }
    form, .naming { display: grid; gap: 9px; padding-bottom: 10px; }
    h3 { margin: 0; color: var(--am-text); font-size: .85rem; }
    label { display: grid; gap: 5px; min-width: 0; color: var(--am-muted); font-size: .72rem; }
    input, select, button { box-sizing: border-box; min-width: 0; border: 1px solid var(--am-border); border-radius: 6px; background: var(--am-surface-raised); color: var(--am-text); padding: 8px; font: inherit; font-size: .76rem; }
    input, select { width: 100%; }
    input[type="checkbox"] { width: auto; }
    button { cursor: pointer; }
    button:disabled { cursor: default; opacity: .5; }
    .actions { display: flex; flex-wrap: wrap; gap: 8px; }
    .actions button { flex: 1 1 auto; }
    .primary { border-color: var(--am-accent); color: var(--am-accent); font-weight: 650; }
    .quiet { background: transparent; }
    [role="alert"], [role="status"] { margin: 0; font-size: .72rem; line-height: 1.5; overflow-wrap: anywhere; }
    [role="alert"] { color: var(--am-danger); }
    [role="status"] { color: var(--am-muted); }
    input:focus-visible, select:focus-visible, button:focus-visible, summary:focus-visible { outline: 2px solid var(--am-accent); outline-offset: 2px; }
  `;

  connectedCallback() {
    super.connectedCallback();
    this.reloadSaved();
  }

  protected willUpdate(_changed: PropertyValues) {
    if (!this.selectedRequest) return;
    if (this.pending) {
      this.selectedRequest.sawPending = true;
      return;
    }
    if (!this.selectedRequest.sawPending) return;
    const request = this.selectedRequest;
    if (this.error) {
      this.selectedApplied = false;
      this.selectedRequest = undefined;
      return;
    }
    if (investigationFiltersKey(this.filters) === investigationFiltersKey(request.filters)) {
      this.selectedApplied = true;
      this.selectedRequest = undefined;
    }
  }

  render() {
    return html`<section aria-labelledby="investigation-filters-heading">
        <h3 id="investigation-filters-heading">Investigation filters</h3>
        <p class="applied">Applied: ${describeFilters(this.filters)}</p>
        <label>Saved filter
          <select name="saved-filter" .value=${this.selectedName} ?disabled=${this.pending} @change=${this.selectSaved}>
            <option value="">Choose a saved filter…</option>
            ${this.saved.map((item) => html`<option value=${item.name}>${item.name}</option>`)}
          </select>
        </label>
        ${this.selectedName ? html`<div class="actions">
          <button type="button" data-action="update-saved" ?disabled=${!this.selectedApplied || !this.confirmed || this.pending} @click=${this.updateSaved}>Update saved filter</button>
          <button class="quiet" type="button" data-action="delete-saved" ?disabled=${this.pending} @click=${this.deleteSaved}>Delete</button>
        </div>` : null}
        <button type="button" data-action="save-as" ?disabled=${!this.confirmed || this.pending} @click=${this.beginNaming}>Save current filters as…</button>
        ${this.naming ? html`<form class="naming" @submit=${this.save}>
          <label>Filter name<input name="filter-name" maxlength="80" .value=${this.saveName} @input=${this.changeSaveName}></label>
          <div class="actions">
            <button class="primary" type="submit" data-action="save" ?disabled=${!this.confirmed || this.pending}>Save</button>
            <button class="quiet" type="button" @click=${this.cancelNaming}>Cancel</button>
          </div>
        </form>` : null}
        <details class="editor">
          <summary>Edit conditions</summary>
          <form @submit=${this.applyDraft}>
            <label><span><input type="checkbox" name="observedFailure"> Observed failure</span></label>
            <label>Minimum elapsed time (ms)<input type="number" name="minDurationMs" min="0" step="any"></label>
            <label>Maximum elapsed time (ms)<input type="number" name="maxDurationMs" min="0" step="any"></label>
            <label>Model (exact)<input name="model" maxlength="200"></label>
            <label>Tool (exact)<input name="tool" maxlength="200"></label>
            <button class="primary" type="submit" data-action="apply-draft" ?disabled=${this.pending}>${this.pending ? "Applying…" : "Apply filters"}</button>
          </form>
        </details>
        ${this.draftError ? html`<p role="alert">Not applied: ${this.draftError}</p>` : null}
        ${this.error ? html`<p role="alert">Not applied: ${this.error}</p>` : null}
        ${this.savedError ? html`<p role="alert">${this.savedError}</p><button type="button" data-action="reload-saved" @click=${this.reloadSaved}>Retry saved filters</button>` : null}
        ${this.status ? html`<p role="status">${this.status}</p>` : null}
    </section>`;
  }

  protected updated(changed: PropertyValues) {
    const key = conditionsKey(this.filters);
    if (key !== this.appliedKey) {
      this.appliedKey = key;
      for (const name of ["minDurationMs", "maxDurationMs", "model", "tool"] as const) {
        this.field(name).value = String(this.filters[name] ?? "");
      }
      this.field("observedFailure").checked = this.filters.observedFailure === true;
      this.draftError = "";
    }
    const select = this.shadowRoot?.querySelector<HTMLSelectElement>("select[name='saved-filter']");
    if (select && select.value !== this.selectedName) select.value = this.selectedName;
    if (changed.has("naming") && this.naming) this.field("filter-name").focus();
  }

  private field(name: string) {
    return this.shadowRoot!.querySelector<HTMLInputElement>(`[name=${name}]`)!;
  }

  private applyDraft(event: Event) {
    event.preventDefault();
    if (this.pending) return;
    const duration = (name: string) => this.field(name).value === "" ? undefined : Number(this.field(name).value);
    try {
      const filters = parseInvestigationFilters({
        ...this.filters,
        observedFailure: this.field("observedFailure").checked,
        minDurationMs: duration("minDurationMs"),
        maxDurationMs: duration("maxDurationMs"),
        model: this.field("model").value.trim(),
        tool: this.field("tool").value.trim(),
      });
      this.draftError = "";
      this.requestFilters(filters);
    } catch (error) {
      this.draftError = error instanceof Error ? error.message : "Invalid conditions.";
    }
  }

  private requestFilters(filters: InvestigationFilters) {
    this.dispatchEvent(new CustomEvent("investigation-filters-requested", {
      detail: { filters },
      bubbles: true,
      composed: true,
    }));
  }

  private reloadSaved(event?: Event) {
    this.status = "";
    try {
      this.saved = loadSavedFilters(window.localStorage);
      if (!this.saved.some((item) => item.name === this.selectedName)) {
        this.selectedName = "";
        this.selectedApplied = false;
      }
      this.savedError = "";
      if (event) {
        void this.updateComplete.then(() => {
          this.shadowRoot?.querySelector<HTMLSelectElement>("select[name='saved-filter']")?.focus();
        });
      }
    } catch (error) {
      this.savedError = error instanceof Error ? error.message : "Saved filters are unavailable.";
    }
  }

  private selectSaved(event: Event) {
    this.selectedName = (event.target as HTMLSelectElement).value;
    this.selectedApplied = false;
    this.selectedRequest = undefined;
    this.status = "";
    this.savedError = "";
    if (!this.selectedName || this.pending) return;
    try {
      const selected = loadSavedFilters(window.localStorage).find((item) => item.name === this.selectedName);
      if (!selected) throw new Error("The saved filter no longer exists.");
      this.selectedRequest = { name: selected.name, filters: selected.filters, sawPending: false };
      this.requestFilters(selected.filters);
    } catch (error) {
      this.savedError = error instanceof Error ? error.message : "The saved filter could not be loaded.";
    }
  }

  private beginNaming() {
    if (!this.confirmed || this.pending) return;
    this.saveName = "";
    this.status = "";
    this.savedError = "";
    this.naming = true;
  }

  private cancelNaming() {
    this.naming = false;
    this.saveName = "";
    void this.updateComplete.then(() => {
      const saveAs = this.shadowRoot?.querySelector<HTMLButtonElement>("button[data-action='save-as']");
      if (saveAs && !saveAs.disabled) saveAs.focus();
      else this.shadowRoot?.querySelector<HTMLElement>("details.editor > summary")?.focus();
    });
  }

  private changeSaveName(event: Event) {
    this.saveName = (event.target as HTMLInputElement).value;
  }

  private save(event: Event) {
    event.preventDefault();
    if (!this.confirmed || this.pending) return;
    const saved = this.persist(
      () => saveFilter(window.localStorage, this.saveName, this.filters),
      `Saved “${this.saveName.trim()}”.`,
      this.saveName.trim(),
    );
    if (saved) {
      this.selectedApplied = true;
      this.cancelNaming();
    }
  }

  private updateSaved() {
    if (!this.selectedName || !this.selectedApplied || !this.confirmed || this.pending) return;
    this.persist(
      () => replaceFilter(window.localStorage, this.selectedName, this.filters),
      `Updated “${this.selectedName}”.`,
      this.selectedName,
    );
  }

  private deleteSaved() {
    if (!this.selectedName || this.pending) return;
    const deleted = this.persist(
      () => deleteFilter(window.localStorage, this.selectedName),
      `Deleted “${this.selectedName}”.`,
      "",
    );
    if (deleted) {
      this.selectedApplied = false;
      void this.updateComplete.then(() => {
        this.shadowRoot?.querySelector<HTMLSelectElement>("select[name='saved-filter']")?.focus();
      });
    }
  }

  private persist(operation: () => readonly SavedFilter[], success: string, selection: string) {
    this.status = "";
    this.savedError = "";
    try {
      this.saved = operation();
      this.selectedName = selection;
      this.status = success;
      return true;
    } catch (error) {
      this.savedError = error instanceof Error ? error.message : "The saved filter change could not be persisted.";
      return false;
    }
  }
}

function describeFilters(value: InvestigationFilters) {
  return [
    value.range,
    value.sourceId || "all sources",
    value.search ? `search ${value.search}` : "",
    value.observedFailure ? "observed failure" : "",
    value.minDurationMs !== undefined ? `at least ${value.minDurationMs} ms` : "",
    value.maxDurationMs !== undefined ? `at most ${value.maxDurationMs} ms` : "",
    value.model ? `model ${value.model}` : "",
    value.tool ? `tool ${value.tool}` : "",
  ].filter(Boolean).join(" · ");
}

declare global {
  interface HTMLElementTagNameMap {
    "am-investigation-filter": InvestigationFilter;
  }
}
