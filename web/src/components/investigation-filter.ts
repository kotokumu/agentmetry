import { css, html, type PropertyValues } from "lit";
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
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";

@customElement("am-investigation-filter")
export class InvestigationFilter extends LocalizedElement {
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
        <h3 id="investigation-filters-heading">${localization.t("investigation.title")}</h3>
        <p class="applied">${localization.t("investigation.applied", { filters: describeFilters(this.filters) })}</p>
        <label>${localization.t("saved.filter")}
          <select name="saved-filter" .value=${this.selectedName} ?disabled=${this.pending} @change=${this.selectSaved}>
            <option value="">${localization.t("saved.chooseOne")}</option>
            ${this.saved.map((item) => html`<option value=${item.name}>${item.name}</option>`)}
          </select>
        </label>
        ${this.selectedName ? html`<div class="actions">
          <button type="button" data-action="update-saved" ?disabled=${!this.selectedApplied || !this.confirmed || this.pending} @click=${this.updateSaved}>${localization.t("saved.update")}</button>
          <button class="quiet" type="button" data-action="delete-saved" ?disabled=${this.pending} @click=${this.deleteSaved}>${localization.t("saved.delete")}</button>
        </div>` : null}
        <button type="button" data-action="save-as" ?disabled=${!this.confirmed || this.pending} @click=${this.beginNaming}>${localization.t("saved.saveAs")}</button>
        ${this.naming ? html`<form class="naming" @submit=${this.save}>
          <label>${localization.t("saved.filterName")}<input name="filter-name" maxlength="80" .value=${this.saveName} @input=${this.changeSaveName}></label>
          <div class="actions">
            <button class="primary" type="submit" data-action="save" ?disabled=${!this.confirmed || this.pending}>${localization.t("common.save")}</button>
            <button class="quiet" type="button" @click=${this.cancelNaming}>${localization.t("common.cancel")}</button>
          </div>
        </form>` : null}
        <details class="editor">
          <summary>${localization.t("investigation.editConditions")}</summary>
          <form @submit=${this.applyDraft}>
            <label><span><input type="checkbox" name="observedFailure"> ${localization.t("investigation.observedFailure")}</span></label>
            <label>${localization.t("investigation.minElapsed")}<input type="number" name="minDurationMs" min="0" step="any"></label>
            <label>${localization.t("investigation.maxElapsed")}<input type="number" name="maxDurationMs" min="0" step="any"></label>
            <label>${localization.t("investigation.modelExact")}<input name="model" maxlength="200"></label>
            <label>${localization.t("investigation.toolExact")}<input name="tool" maxlength="200"></label>
            <button class="primary" type="submit" data-action="apply-draft" ?disabled=${this.pending}>${localization.t(this.pending ? "investigation.applying" : "investigation.apply")}</button>
          </form>
        </details>
        ${this.draftError ? html`<p role="alert">${localization.t("investigation.notApplied", { reason: this.draftError })}</p>` : null}
        ${this.error ? html`<p role="alert">${localization.t("investigation.notApplied", { reason: this.error })}</p>` : null}
        ${this.savedError ? html`<p role="alert">${this.savedError}</p><button type="button" data-action="reload-saved" @click=${this.reloadSaved}>${localization.t("saved.retry")}</button>` : null}
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
      this.draftError = error instanceof Error ? error.message : localization.t("investigation.invalid");
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
      this.savedError = error instanceof Error ? error.message : localization.t("saved.unavailable");
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
      if (!selected) throw new Error(localization.t("saved.noLongerExists"));
      this.selectedRequest = { name: selected.name, filters: selected.filters, sawPending: false };
      this.requestFilters(selected.filters);
    } catch (error) {
      this.savedError = error instanceof Error ? error.message : localization.t("saved.loadFailed");
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
      localization.t("saved.saved", { name: this.saveName.trim() }),
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
      localization.t("saved.updated", { name: this.selectedName }),
      this.selectedName,
    );
  }

  private deleteSaved() {
    if (!this.selectedName || this.pending) return;
    const deleted = this.persist(
      () => deleteFilter(window.localStorage, this.selectedName),
      localization.t("saved.deleted", { name: this.selectedName }),
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
      this.savedError = error instanceof Error ? error.message : localization.t("saved.persistFailed");
      return false;
    }
  }
}

function describeFilters(value: InvestigationFilters) {
  return [
    value.range,
    value.sourceId || localization.t("investigation.allSources"),
    value.search ? localization.t("investigation.search", { value: value.search }) : "",
    value.observedFailure ? localization.t("investigation.failure") : "",
    value.minDurationMs !== undefined ? localization.t("investigation.atLeast", { value: value.minDurationMs }) : "",
    value.maxDurationMs !== undefined ? localization.t("investigation.atMost", { value: value.maxDurationMs }) : "",
    value.model ? localization.t("investigation.model", { value: value.model }) : "",
    value.tool ? localization.t("investigation.tool", { value: value.tool }) : "",
  ].filter(Boolean).join(" · ");
}

declare global {
  interface HTMLElementTagNameMap {
    "am-investigation-filter": InvestigationFilter;
  }
}
