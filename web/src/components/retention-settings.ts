import { msg } from "@lit/localize";
import { css, html, nothing } from "lit";
import { customElement, state } from "lit/decorators.js";
import { retentionClient, type ArchiveSegment, type RestoreResult, type RetentionCapacity, type RetentionCycle, type RetentionOperation } from "../api/retention-client";
import { LocalizedElement } from "../localization/localized-element";

@customElement("am-retention-settings")
export class RetentionSettings extends LocalizedElement {
  @state() private loading = true;
  @state() private saving = false;
  @state() private error = "";
  @state() private enabled = false;
  @state() private archiveDays = 30;
  @state() private deleteDays = 365;
  @state() private segments: readonly ArchiveSegment[] = [];
  @state() private operations: readonly RetentionOperation[] = [];
  @state() private cycles: readonly RetentionCycle[] = [];
  @state() private capacity?: RetentionCapacity;
  @state() private restoreSegmentId = "";
  @state() private restoreStart = "";
  @state() private restoreEnd = "";
  @state() private holdDays = 30;
  @state() private restoreResult?: RestoreResult;
  private operationPoll?: number;

  static styles = css`
    :host { display: block; }
    section { display: grid; gap: 14px; border: 1px solid var(--am-border); border-radius: 10px; padding: 18px; background: var(--am-surface-raised); }
    h2, h3, p { margin: 0; } h2 { font-size: 1rem; } h3 { font-size: .9rem; }
    p, .hint { color: var(--am-muted); font-size: 14px; line-height: 1.5; }
    .policy, .period, .capacity { display: flex; flex-wrap: wrap; gap: 12px; align-items: end; }
    label { display: grid; gap: 5px; font-size: 13px; }
    label.toggle { display: flex; align-items: center; gap: 8px; align-self: center; }
    input { min-width: 7rem; border: 1px solid var(--am-border-strong); border-radius: 6px; padding: 7px; color: var(--am-text); background: var(--am-surface); }
    button { border: 1px solid var(--am-border-strong); border-radius: 7px; padding: 8px 11px; color: var(--am-accent); background: var(--am-accent-soft); font-weight: 700; cursor: pointer; }
    button:disabled { opacity: .55; cursor: default; }
    .capacity span { min-width: 9rem; }
    .segments { display: grid; gap: 8px; padding: 0; list-style: none; }
    .segments li { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; align-items: center; border-top: 1px solid var(--am-border); padding-top: 10px; }
    code { overflow-wrap: anywhere; font-size: 11px; }
    dialog { width: min(34rem, calc(100vw - 32px)); border: 1px solid var(--am-border); border-radius: 10px; padding: 18px; color: var(--am-text); background: var(--am-surface-raised); }
    dialog::backdrop { background: rgb(0 0 0 / .45); }
    .dialog-actions { display: flex; justify-content: end; gap: 8px; margin-top: 16px; }
    .error { color: var(--am-danger, #c33); }
    @media (max-width: 620px) { .segments li { grid-template-columns: 1fr; } }
  `;

  connectedCallback() { super.connectedCallback(); void this.load(); }
  disconnectedCallback() { if (this.operationPoll !== undefined) window.clearTimeout(this.operationPoll); super.disconnectedCallback(); }

  render() {
    return html`<section aria-busy=${this.loading ? "true" : "false"}>
      <div><h2>${msg("Data retention", { id: "retention.heading" })}</h2><p>${msg("Archives keep only exact raw OTLP data in cross-export compressed files. Archived data disappears from searches until it is restored; expiry permanently removes the archive.", { id: "retention.intro" })}</p></div>
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : nothing}
      <form class="policy" @submit=${this.savePolicy}>
        <label class="toggle"><input type="checkbox" .checked=${this.enabled} @change=${(event: Event) => { this.enabled = (event.target as HTMLInputElement).checked; }}>${msg("Enable automatic retention", { id: "retention.enable" })}</label>
        <label>${msg("Archive after days", { id: "retention.archiveDays" })}<input name="archiveDays" type="number" min="1" max="36500" required .value=${String(this.archiveDays)} ?disabled=${!this.enabled} @input=${this.numberInput("archiveDays")}></label>
        <label>${msg("Delete after days", { id: "retention.deleteDays" })}<input name="deleteDays" type="number" min="1" max="36500" required .value=${String(this.deleteDays)} ?disabled=${!this.enabled} @input=${this.numberInput("deleteDays")}></label>
        <button type="submit" ?disabled=${this.saving}>${this.saving ? msg("Saving…", { id: "retention.saving" }) : msg("Save retention policy", { id: "retention.save" })}</button>
      </form>
      <div><h3>${msg("Storage capacity", { id: "retention.capacity" })}</h3>${this.capacity ? html`<div class="capacity"><span>${msg("Total allocated", { id: "retention.totalAllocated" })}: ${formatBytes(this.capacity.totalAllocatedBytes)}</span><span>${msg("Active raw", { id: "retention.activeRaw" })}: ${formatBytes(this.capacity.activeRawBytes)}</span><span>${msg("Normalized observations", { id: "retention.observations" })}: ${formatBytes(this.capacity.observationBytes)}</span><span>${msg("Query projections", { id: "retention.projections" })}: ${formatBytes(this.capacity.queryProjectionBytes)}</span><span>${msg("Reusable SQLite pages", { id: "retention.sqliteFree" })}: ${formatBytes(this.capacity.databaseUnusedBytes)}</span><span>${msg("Archive files", { id: "retention.archives" })}: ${formatBytes(this.capacity.archiveAllocatedBytes)}</span><span>${msg("Staging files", { id: "retention.staging" })}: ${formatBytes(this.capacity.stagingAllocatedBytes)}</span><span>${msg("Filesystem available", { id: "retention.free" })}: ${this.capacity.filesystemFreeBytes === undefined ? this.capacity.unavailableReason : formatBytes(this.capacity.filesystemFreeBytes)}</span></div>${this.capacity.warning ? html`<p class="error" role="status">${this.capacity.warning}</p>` : nothing}` : html`<p>${msg("Capacity unavailable", { id: "retention.capacityUnavailable" })}</p>`}</div>
      <div><h3>${msg("Archive inventory", { id: "retention.inventory" })}</h3>
        ${this.segments.length === 0 ? html`<p>${msg("No current archive segments.", { id: "retention.empty" })}</p>` : html`<ul class="segments">${this.segments.map((segment) => html`<li><span><code>${segment.id}</code><br><span class="hint">${formatDate(segment.minReceivedAt)} – ${formatDate(segment.maxReceivedAt)} · ${segment.exportCount} ${msg("exports", { id: "retention.exports" })} · ${formatBytes(segment.storedBytes)} / ${formatBytes(segment.originalBytes)}<br>${msg("Payload", { id: "retention.payloadIntegrity" })}: ${segment.payloadIntegrity} · ${msg("Metadata", { id: "retention.metadataIntegrity" })}: ${segment.metadataIntegrity}${segment.integrityError ? ` — ${segment.integrityError}` : ""}<br>${segment.scheduledDeleteAt ? `${msg("Scheduled deletion", { id: "retention.scheduledDeletion" })}: ${formatDate(segment.scheduledDeleteAt)}` : segment.scheduleUnavailableReason}</span></span><button type="button" @click=${() => this.openSegmentRestore(segment.id)}>${msg("Restore…", { id: "retention.restore" })}</button></li>`)}</ul>`}
      </div>
      <div><h3>${msg("Restore a receive-time period", { id: "retention.period" })}</h3><div class="period"><label>${msg("Start", { id: "retention.start" })}<input type="datetime-local" .value=${this.restoreStart} @input=${(event: Event) => { this.restoreStart = (event.target as HTMLInputElement).value; }}></label><label>${msg("End (exclusive)", { id: "retention.end" })}<input type="datetime-local" .value=${this.restoreEnd} @input=${(event: Event) => { this.restoreEnd = (event.target as HTMLInputElement).value; }}></label><label>${msg("Hold days", { id: "retention.holdDays" })}<input type="number" min="1" max="36500" .value=${String(this.holdDays)} @input=${this.numberInput("holdDays")}></label><button type="button" @click=${this.restorePeriod}>${msg("Restore period", { id: "retention.restorePeriod" })}</button></div></div>
      ${this.restoreResult ? html`<p role="status">${msg("Restore result", { id: "retention.restoreResult" })}: ${this.restoreResult.result} · ${msg("Active", { id: "retention.activeMatches" })} ${this.restoreResult.activeMatches} · ${msg("Archived", { id: "retention.archivedMatches" })} ${this.restoreResult.archivedMatches} · ${msg("Deleted", { id: "retention.deletedMatches" })} ${this.restoreResult.deletedMatches}${this.restoreResult.operationId ? ` · ${this.restoreResult.operationId}` : ""}</p>` : nothing}
      ${this.operations.length > 0 ? html`<div><h3>${msg("Recent operations", { id: "retention.operations" })}</h3><ul>${this.operations.map((operation) => html`<li>${operation.kind}: ${operation.status} · ${operation.affectedExports} ${msg("exports", { id: "retention.exports" })}${operation.evaluatedAt ? ` · ${formatDate(operation.evaluatedAt)}` : ""}${operation.error ? ` — ${operation.error}` : ""}</li>`)}</ul></div>` : nothing}
      ${this.cycles.length > 0 ? html`<div><h3>${msg("Maintenance cycles", { id: "retention.cycles" })}</h3><ul>${this.cycles.map((cycle) => html`<li>${cycle.status} · ${cycle.cohortSize} ${msg("exports", { id: "retention.exports" })}${cycle.evaluatedAt ? ` · ${formatDate(cycle.evaluatedAt)}` : ""} · ${cycle.pendingChildren}/${cycle.runningChildren}/${cycle.completedChildren}/${cycle.failedChildren}/${cycle.cancelledChildren}${cycle.cohortUnavailableReason ? ` — ${cycle.cohortUnavailableReason}` : cycle.error ? ` — ${cycle.error}` : ""}</li>`)}</ul></div>` : nothing}
      <dialog @close=${() => { this.restoreSegmentId = ""; }}><h3>${msg("Restore archive segment", { id: "retention.restoreDialog" })}</h3><p>${msg("The raw data will be normalized again and become searchable. A finite hold prevents immediate re-archiving.", { id: "retention.restoreHelp" })}</p><label>${msg("Hold days", { id: "retention.holdDays" })}<input type="number" min="1" max="36500" .value=${String(this.holdDays)} @input=${this.numberInput("holdDays")}></label><div class="dialog-actions"><button type="button" @click=${this.closeDialog}>${msg("Cancel", { id: "retention.cancel" })}</button><button type="button" @click=${this.restoreSelectedSegment}>${msg("Restore segment", { id: "retention.restoreSegment" })}</button></div></dialog>
    </section>`;
  }

  private numberInput(field: "archiveDays" | "deleteDays" | "holdDays") { return (event: Event) => { this[field] = Number((event.target as HTMLInputElement).value); }; }
  private async load() {
    this.loading = true; this.error = "";
    try {
      const [status, firstInventory, capacity] = await Promise.all([retentionClient.status(), retentionClient.segments(), retentionClient.capacity()]);
      const segments = [...firstInventory.segments];
      let pageToken = firstInventory.nextPageToken;
      while (pageToken) {
        const page = await retentionClient.segments(pageToken);
        segments.push(...page.segments);
        pageToken = page.nextPageToken;
      }
      this.enabled = status.policy.enabled; this.archiveDays = status.policy.archiveDays ?? this.archiveDays; this.deleteDays = status.policy.deleteDays ?? this.deleteDays;
      this.operations = status.operations; this.cycles = status.cycles; this.segments = segments; this.capacity = capacity;
    } catch (error) { this.error = error instanceof Error ? error.message : String(error); }
    finally { this.loading = false; this.scheduleOperationPoll(); }
  }
  private savePolicy = async (event: SubmitEvent) => {
    event.preventDefault(); this.saving = true; this.error = "";
    try { await retentionClient.updatePolicy(this.enabled, this.enabled ? this.archiveDays : undefined, this.enabled ? this.deleteDays : undefined); await this.load(); }
    catch (error) { this.error = error instanceof Error ? error.message : String(error); }
    finally { this.saving = false; }
  };
  private openSegmentRestore(id: string) { this.restoreSegmentId = id; this.renderRoot.querySelector("dialog")?.showModal(); }
  private closeDialog = () => this.renderRoot.querySelector("dialog")?.close();
  private restoreSelectedSegment = async () => { if (!this.restoreSegmentId) return; try { this.restoreResult = await retentionClient.restoreSegment(this.restoreSegmentId, this.holdDays); this.closeDialog(); await this.load(); } catch (error) { this.error = error instanceof Error ? error.message : String(error); } };
  private restorePeriod = async () => { try { const start = new Date(this.restoreStart); const end = new Date(this.restoreEnd); if (!this.restoreStart || !this.restoreEnd || !(start < end)) throw new Error("Start must be earlier than end"); this.restoreResult = await retentionClient.restorePeriod(start, end, this.holdDays); await this.load(); } catch (error) { this.error = error instanceof Error ? error.message : String(error); } };
  private scheduleOperationPoll() {
    if (this.operationPoll !== undefined) window.clearTimeout(this.operationPoll);
    this.operationPoll = undefined;
    if (
      this.operations.some((operation) => operation.status === "pending" || operation.status === "running") ||
      this.cycles.some((cycle) => cycle.status === "running")
    ) {
      this.operationPoll = window.setTimeout(() => { this.operationPoll = undefined; void this.load(); }, 1_000);
    }
  }
}

const formatBytes = (value: bigint) => {
  const bytes = Number(value);
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KiB`;
  if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MiB`;
  return `${(bytes / 1024 ** 3).toFixed(1)} GiB`;
};

const formatDate = (value: Date) => new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(value);

declare global { interface HTMLElementTagNameMap { "am-retention-settings": RetentionSettings } }
