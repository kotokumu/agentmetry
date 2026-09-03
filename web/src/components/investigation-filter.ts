import { LitElement, css, html, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { parseInvestigationFilters, conditionsKey, type InvestigationFilters } from "../model/investigation-conditions";

@customElement("am-investigation-filter")
export class InvestigationFilter extends LitElement {
  @property({ attribute: false }) filters: InvestigationFilters = { range: "24h", sourceId: "", search: "" };
  @property() error = "";
  @property({ type: Boolean }) confirmed = true;
  @property({ type: Boolean }) pending = false;
  @state() private draftError = "";
  private appliedKey = "";
  static styles = css`
    :host { display: block; margin: 12px 0; font-size: .85rem; }
    details { border-top: 1px solid var(--am-border); padding-top: 10px; }
    summary { cursor: pointer; padding: 5px 0; }
    form { display: grid; gap: 9px; margin-top: 10px; }
    label { display: grid; gap: 4px; color: var(--am-muted); }
    .checkbox { display: flex; align-items: center; gap: 8px; }
    input { min-width: 0; width: 100%; padding: 8px; }
    input[type=checkbox] { width: auto; }
    input, button { font: inherit; color: var(--am-text); background: var(--am-surface); border: 1px solid var(--am-border); border-radius: 5px; }
    button { padding: 9px; cursor: pointer; } button:disabled { opacity: .6; }
    :focus-visible { outline: 2px solid var(--am-accent); outline-offset: 3px; }
    p { line-height: 1.5; overflow-wrap: anywhere; } .applied { color: var(--am-muted); }
    [role=alert] { color: var(--am-danger); }
  `;
  render() {
    return html`<details><summary>Investigation filters</summary>
      <p class="applied">${this.confirmed ? "Applied" : "Requested (not yet applied)"}: ${describeFilters(this.filters)}</p>
      <form @submit=${this.apply}>
        <label class="checkbox"><input type="checkbox" name="observedFailure">Observed failure</label>
        <label>Minimum elapsed time (ms)<input type="number" name="minDurationMs" min="0" step="any"></label>
        <label>Maximum elapsed time (ms)<input type="number" name="maxDurationMs" min="0" step="any"></label>
        <label>Model (exact)<input name="model" maxlength="200"></label>
        <label>Tool (exact)<input name="tool" maxlength="200"></label>
        <button type="submit" ?disabled=${this.pending}>${this.pending ? "Checking conditions…" : "Apply draft conditions"}</button>
      </form>
      <p>All conditions must match the conversation. Unknown outcomes do not count as failures.</p>
    </details>${this.draftError || this.error ? html`<p role="alert">Not applied: ${this.draftError || this.error}</p>` : null}`;
  }
  protected updated(_changed: PropertyValues) {
    const key = conditionsKey(this.filters);
    if (key === this.appliedKey) return;
    this.appliedKey = key;
    for (const name of ["minDurationMs", "maxDurationMs", "model", "tool"] as const) {
      const input = this.shadowRoot!.querySelector<HTMLInputElement>(`[name=${name}]`)!;
      input.value = String(this.filters[name] ?? "");
    }
    this.shadowRoot!.querySelector<HTMLInputElement>("[name=observedFailure]")!.checked = this.filters.observedFailure === true;
    this.draftError = "";
  }
  private apply(event: Event) {
    event.preventDefault(); if (this.pending) return;
    const field = (name: string) => this.shadowRoot!.querySelector<HTMLInputElement>(`[name=${name}]`)!;
    const duration = (name: string) => field(name).value === "" ? undefined : Number(field(name).value);
    try {
      const filters = parseInvestigationFilters({ ...this.filters, observedFailure: field("observedFailure").checked, minDurationMs: duration("minDurationMs"), maxDurationMs: duration("maxDurationMs"), model: field("model").value.trim(), tool: field("tool").value.trim() });
      this.draftError = "";
      this.dispatchEvent(new CustomEvent("investigation-filters-requested", { detail: { filters }, bubbles: true, composed: true }));
    } catch (error) { this.draftError = error instanceof Error ? error.message : "Invalid conditions."; }
  }
}
function describeFilters(value: InvestigationFilters) {
  return [value.range, value.sourceId || "all sources", value.search ? `search ${value.search}` : "", value.observedFailure ? "observed failure" : "", value.minDurationMs !== undefined ? `at least ${value.minDurationMs} ms` : "", value.maxDurationMs !== undefined ? `at most ${value.maxDurationMs} ms` : "", value.model ? `model ${value.model}` : "", value.tool ? `tool ${value.tool}` : ""].filter(Boolean).join(" · ");
}
declare global { interface HTMLElementTagNameMap { "am-investigation-filter": InvestigationFilter } }
