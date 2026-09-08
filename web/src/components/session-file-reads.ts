import { css, html, type PropertyValues } from "lit";
import { msg } from "@lit/localize";
import { customElement, property, state } from "lit/decorators.js";
import { agentmetryClient } from "../api/agentmetry-client";
import type { SessionFileRead } from "../model/trace-catalog";
import type { Activity, ContentEvidence } from "../model/telemetry";
import { activityIdentity } from "./activity-table";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";
import { notReported } from "../presentation/missing-data";
import { affectsSession, LIVE_UPDATE_EVENT, type LiveUpdateDelivery } from "../controllers/live-update-controller";
import { contentAvailabilityLabel, readableActivityContent } from "./content-evidence";
import "./content-evidence";

type FileReadSelection = Readonly<{ activityId: string; readId: string }>;

@customElement("am-session-file-reads")
export class SessionFileReads extends LocalizedElement {
  @property() sourceId = "";
  @property() sessionId = "";
  @property() requestedReadId = "";
  @property() requestedActivityId = "";
  @property({ attribute: false }) activityContent?: Activity;
  @property({ type: Boolean }) activityContentLoading = false;
  @property({ type: Boolean }) active = false;
  @state() private candidateReads: readonly SessionFileRead[] = [];
  @state() private historyReads: readonly SessionFileRead[] = [];
  @state() private selectedReference = "";
  @state() private selectedReadId = "";
  @state() private candidateNextPageToken = "";
  @state() private historyNextPageToken = "";
  @state() private loading = false;
  @state() private error = "";
  @state() private coverage: SessionFileRead["coverage"] = "unavailable";
  @state() private distinctReferenceCount = 0;
  @state() private failedLoad: "candidates" | "history" = "candidates";
  private request = 0;
  private loadedKey = "";
  private abort?: AbortController;
  private readonly selectedReadByReference = new Map<string, string>();
  private focusDetailHeading = false;

  static styles = css`
    :host { display: block; grid-column: 1 / -1; }
    .panel { padding: 16px; border: 1px solid var(--am-border); border-radius: 8px; background: var(--am-surface-raised); }
    h2, h3 { margin: 0; font-size: 1rem; }
    h3 { font-size: 1rem; }
    .intro, .state, .detail-note { color: var(--am-muted); font-size: 14px; line-height: 1.5; overflow-wrap: anywhere; }
    .intro { margin: 6px 0 14px; }
    .reading-layout { display: grid; grid-template-columns: minmax(180px, .72fr) minmax(300px, 1.28fr); gap: 16px; align-items: start; }
    .file-list { display: grid; gap: 8px; margin: 0; padding: 0; list-style: none; }
    .file-row { min-width: 0; }
    .file-row button { display: block; width: 100%; min-width: 0; border: 1px solid var(--am-border); border-radius: 7px; padding: 11px 12px; background: var(--am-surface); color: var(--am-text); text-align: left; cursor: pointer; font: 14px/1.4 "SFMono-Regular", "Cascadia Code", monospace; overflow-wrap: anywhere; }
    .file-row button[data-selected="true"] { border-color: var(--am-accent); background: var(--am-accent-soft); }
    .file-row button:hover, .file-row button:focus-visible { border-color: var(--am-accent); outline: 2px solid var(--am-accent-soft); outline-offset: 1px; }
    .file-count, .meta { display: block; margin-top: 5px; color: var(--am-muted); font: 12px/1.4 inherit; overflow-wrap: anywhere; }
    .file-count { font-family: system-ui, sans-serif; }
    .file-detail { min-width: 0; position: sticky; top: 16px; border: 1px solid var(--am-border); border-radius: 8px; padding: 16px; background: var(--am-surface); overflow-wrap: anywhere; }
    .file-detail h3 { color: var(--am-accent); font-family: "SFMono-Regular", "Cascadia Code", monospace; word-break: break-word; }
    .detail-note { margin: 6px 0 12px; }
    .read-history { display: grid; gap: 5px; margin: 0 0 14px; color: var(--am-muted); font-size: 12px; }
    .read-history select { min-width: 0; width: 100%; border: 1px solid var(--am-border); border-radius: 6px; padding: 8px; background: var(--am-surface-raised); color: var(--am-text); font: 14px/1.35 inherit; }
    .read-meta { display: flex; flex-wrap: wrap; gap: 7px 14px; margin: 8px 0 14px; color: var(--am-muted); font-size: 14px; }
    .read-body { margin: 0; padding: 14px; border: 1px solid var(--am-border); border-radius: 6px; background: var(--am-surface-raised); white-space: pre-wrap; overflow-wrap: anywhere; font: 13px/1.6 "SFMono-Regular", "Cascadia Code", monospace; }
    .missing { margin: 0; padding: 12px; border-left: 3px solid var(--am-border); color: var(--am-muted); line-height: 1.5; }
    .record-evidence { margin-top: 16px; border-top: 1px solid var(--am-border); padding-top: 10px; }
    .record-evidence summary { color: var(--am-muted); cursor: pointer; font-size: 12px; }
    dl { display: grid; grid-template-columns: minmax(7rem, auto) minmax(0, 1fr); gap: 6px 10px; margin: 12px 0 0; font-size: 12px; }
    dt { color: var(--am-muted); }
    dd { min-width: 0; margin: 0; color: var(--am-text); overflow-wrap: anywhere; }
    code { color: var(--am-accent); font: 12px/1.4 "SFMono-Regular", "Cascadia Code", monospace; word-break: break-all; }
    .detail-actions { display: flex; flex-wrap: wrap; gap: 7px; margin-top: 14px; }
    .detail-actions button, .more { border: 1px solid var(--am-border); border-radius: 7px; padding: 8px 10px; background: var(--am-surface-raised); color: var(--am-text); cursor: pointer; font: 14px/1.3 inherit; }
    .retry { margin-top: 10px; border: 1px solid var(--am-border); border-radius: 7px; padding: 8px 10px; background: var(--am-surface-raised); color: var(--am-text); cursor: pointer; font: 14px/1.3 inherit; }
    .detail-actions button:hover:not(:disabled), .detail-actions button:focus-visible, .more:hover:not(:disabled), .more:focus-visible, .retry:hover, .retry:focus-visible { border-color: var(--am-accent); color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    .detail-actions button:disabled { cursor: not-allowed; opacity: .45; }
    .more { margin-top: 12px; }
    @media (max-width: 800px) { .reading-layout { grid-template-columns: 1fr; } .file-list { max-height: 18rem; overflow-y: auto; padding-right: 4px; } .file-detail { position: static; } }
    @media (max-width: 560px) { .detail-actions button { flex: 1 1 140px; } dl { grid-template-columns: 1fr; gap: 2px; } dd + dt { margin-top: 5px; } }
  `;

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener(LIVE_UPDATE_EVENT, this.liveUpdate as EventListener);
  }

  disconnectedCallback() {
    window.removeEventListener(LIVE_UPDATE_EVENT, this.liveUpdate as EventListener);
    this.abort?.abort();
    super.disconnectedCallback();
  }

  protected willUpdate(changed: PropertyValues<this>) {
    if (changed.has("requestedReadId") || changed.has("requestedActivityId")) {
      this.selectedReadId = this.requestedReadId;
      const requested = this.candidateReads.find((read) => read.id === this.requestedReadId);
      if (requested) this.selectedReference = requested.reference;
      else this.selectedReference = "";
      this.historyReads = [];
      this.historyNextPageToken = "";
      if (this.active && this.sourceId && this.sessionId && this.loadedKey) void this.loadCandidates(true);
    }
    if (changed.has("sourceId") || changed.has("sessionId")) {
      this.abort?.abort();
      this.request += 1;
      this.loadedKey = "";
      this.candidateReads = [];
      this.historyReads = [];
      this.candidateNextPageToken = "";
      this.historyNextPageToken = "";
      this.selectedReference = "";
      this.selectedReadId = this.requestedReadId;
      this.selectedReadByReference.clear();
      this.coverage = "unavailable";
      this.distinctReferenceCount = 0;
      this.error = "";
    }
    if (!this.active) {
      this.abort?.abort();
      this.loadedKey = "";
      return;
    }
    const key = `${this.sourceId}:${this.sessionId}`;
    if (!this.sourceId || !this.sessionId || key === this.loadedKey) return;
    this.loadedKey = key;
    void this.loadCandidates(true);
  }

  protected updated() {
    const select = this.shadowRoot?.querySelector<HTMLSelectElement>(".read-history select");
    if (select && select.value !== this.selectedReadId && this.historyReads.some(({ id }) => id === this.selectedReadId)) {
      select.value = this.selectedReadId;
    }
    if (this.focusDetailHeading) {
      const heading = this.shadowRoot?.querySelector<HTMLElement>(".file-detail h3[tabindex='-1']");
      if (heading) {
        this.focusDetailHeading = false;
        heading.focus({ preventScroll: true });
      }
    }
  }

  render() {
    const selected = this.historyReads.find(({ id }) => id === this.selectedReadId);
    return html`<section class="panel" aria-labelledby="file-reads-heading">
      <h2 id="file-reads-heading">${localization.t("workspace.fileReads")}</h2>
      <p class="intro">${localization.t("workspace.fileReadsIntro", { count: localization.number(this.distinctReferenceCount) })}</p>
      ${this.error ? html`<p class="state" role="alert">${localization.t("workspace.fileReadsUnavailable")} ${this.error}</p><button class="retry" type="button" @click=${this.retry}>${msg("Retry file reads", { id: "filesCompletion.retry" })}</button>`
        : this.loading && !this.candidateReads.length ? html`<p class="state" role="status">${localization.t("workspace.loadingFileReads")}</p>`
          : html`<div class="reading-layout">
            <div><ul class="file-list" aria-label=${localization.t("workspace.fileReads")}>${this.references.map((reference) => this.renderReference(reference))}</ul>
              ${!this.references.length ? html`<p class="state">${localization.t("workspace.noFileReads")}</p>` : null}
              ${this.candidateNextPageToken ? html`<button class="more" type="button" ?disabled=${this.loading} @click=${() => void this.loadCandidates(false)}>${this.loading ? localization.t("workspace.loadingMore") : localization.t("workspace.loadMore")}</button>` : null}
            </div>
            ${this.renderDetail(selected)}
          </div>`}
    </section>`;
  }

  private renderReference(reference: string) {
    const reads = this.candidateReads.filter((read) => read.reference === reference);
    const selected = reference === this.selectedReference;
    const latest = reads[0];
    const count = this.candidateNextPageToken
      ? localization.t("workspace.fileReadLoadedRecords", { count: localization.number(reads.length) })
      : `${reads.length} ${localization.t("workspace.fileReadRecords")}`;
    return html`<li class="file-row"><button type="button" data-selected=${String(selected)} aria-pressed=${String(selected)} @click=${() => this.selectReference(reference)} title=${reference}>
      <span>${reference}</span>
      <span class="file-count">${reads.length ? count : localization.t("workspace.fileReadRecordsUnknown")}${latest?.observedAt ? ` · ${latest.observedAt}` : ""}</span>
    </button></li>`;
  }

  private renderDetail(read?: SessionFileRead) {
    if (!read && this.loading && this.selectedReference) {
      return html`<aside class="file-detail" aria-live="polite"><h3 tabindex="-1">${this.selectedReference}</h3><p class="detail-note" role="status">${localization.t("common.loading")}</p></aside>`;
    }
    if (!read) {
      const missing = (this.requestedReadId || this.requestedActivityId) && !this.loading;
      const note = missing
        ? this.requestedReadId
          ? localization.t("workspace.fileReadSelectionUnavailable", { id: this.requestedReadId })
          : localization.t("workspace.fileReadActivitySelectionUnavailable", { id: this.requestedActivityId })
        : this.selectedReference ? localization.t("workspace.fileReadSelectionHelp") : localization.t("workspace.selectFileRead");
      return html`<aside class="file-detail" aria-live="polite"><h3 tabindex="-1">${this.selectedReference || localization.t("workspace.fileReadDetail")}</h3><p class="detail-note">${note}</p></aside>`;
    }
    const outputState = this.renderOutput(read);
    const activity = this.exactActivityContent(read);
    const index = this.historyReads.findIndex(({ id }) => id === read.id);
    const runtime = read.agentId ? `${read.agentId} (${read.model || notReported()})` : read.model || notReported();
    return html`<aside class="file-detail" aria-labelledby="file-read-detail-heading">
      <h3 id="file-read-detail-heading" tabindex="-1">${read.reference}</h3>
      <p class="read-meta"><span>${read.observedAt || notReported()}</span><span>${runtime}</span></p>
      ${this.historyReads.length > 1 ? html`<label class="read-history">${localization.t("workspace.fileReadHistory")}<select .value=${read.id} @change=${this.historyChanged}>${this.historyReads.map((item) => html`<option value=${item.id} ?selected=${item.id === read.id}>${item.observedAt || notReported()} · ${item.agentId || notReported()} (${item.model || notReported()})</option>`)}</select></label>` : null}
      ${this.historyNextPageToken ? html`<button class="more" type="button" ?disabled=${this.loading} @click=${() => void this.loadHistory(this.selectedReference, false)}>${this.loading ? localization.t("workspace.loadingMore") : localization.t("workspace.loadMore")}</button>` : null}
      ${outputState}
      <details class="record-evidence"><summary>${localization.t("workspace.fileReadRecordEvidence")}</summary><dl>
        <dt>${localization.t("workspace.fileReadSource")}</dt><dd>${read.sourceId}</dd>
        <dt>${localization.t("workspace.fileReadSession")}</dt><dd>${read.sessionId}</dd>
        <dt>${localization.t("workspace.fileReadId")}</dt><dd><code>${read.id}</code></dd>
        <dt>${localization.t("workspace.fileReadActivity")}</dt><dd><code>${read.activityId || notReported()}</code></dd>
        <dt>${localization.t("workspace.fileReadCoverage")}</dt><dd>${coverageLabel(read.coverage)}</dd>
        <dt>${localization.t("workspace.fileReadMapping")}</dt><dd>${read.outputMapping}</dd>
        <dt>${localization.t("content.evidence")}</dt><dd>${read.contentEvidence.evidence}</dd>
        <dt>${localization.t("content.availability")}</dt><dd>${contentAvailabilityLabel(read.contentEvidence, read.outputContent)}</dd>
        <dt>${localization.t("content.receivedFields")}</dt><dd>${read.contentEvidence.fields.length ? read.contentEvidence.fields.join(", ") : notReported()}</dd>
      </dl>${activity ? html`<am-content-evidence .evidence=${activity.contentEvidence} .activityContent=${readableActivityContent(activity.contentEvidence, activity.content)}></am-content-evidence>` : null}</details>
      <div class="detail-actions">
        <button type="button" @click=${() => this.openActivity(read)}>${localization.t("workspace.openActivity")}</button>
        <button type="button" ?disabled=${index <= 0} @click=${() => this.selectNeighbor(index - 1)}>${localization.t("workspace.previousRead")}</button>
        <button type="button" ?disabled=${index < 0 || index >= this.historyReads.length - 1} @click=${() => this.selectNeighbor(index + 1)}>${localization.t("workspace.nextRead")}</button>
      </div>
    </aside>`;
  }

  private renderOutput(read: SessionFileRead) {
    if (read.outputMapping === "confirmed" && read.outputContent && read.outputAvailability === "available") {
      const label = localization.t("workspace.fileReadOutputConfirmed");
      return html`<section aria-label=${label}><p class="detail-note">${label}</p><pre class="read-body">${read.outputContent}</pre>${this.renderContentStatus(read.contentEvidence)}</section>`;
    }
    const activity = this.exactActivityContent(read);
    if (this.activityContentLoading && read.activityId) {
      return html`<p class="missing" role="status">${localization.t("workspace.fileReadActivityOutputLoading")}</p>`;
    }
    const activityContent = readableActivityContent(activity?.contentEvidence, activity?.content);
    if (read.outputMapping !== "confirmed" && activity && this.hasUnavailableContent(activity.contentEvidence)) {
      return html`<p class="missing">${contentAvailabilityLabel(activity.contentEvidence, activity.content)}</p>`;
    }
    if (read.outputMapping !== "confirmed" && activity && this.isReferenceOnly(activity)) {
      return html`<p class="missing">${localization.t("workspace.fileReadReferenceOnly")}</p>`;
    }
    if (read.outputMapping !== "confirmed" && activity && activityContent) {
      const label = localization.t("workspace.fileReadActivityOutput");
      return html`<section aria-label=${label}><p class="detail-note">${label}</p><pre class="read-body">${activityContent}</pre>${this.renderActivityStatus(activity)}</section>`;
    }
    if (read.outputMapping !== "confirmed") {
      const state = activity
        ? [contentAvailabilityLabel(activity.contentEvidence, activity.content), ...this.activityStatusMessages(activity)].join(" · ")
        : localization.t("workspace.fileReadActivityOutputUnavailable");
      return html`<p class="missing">${state}</p>`;
    }
    return html`<p class="missing">${this.outputState(read)}</p>`;
  }

  private renderActivityStatus(activity: Activity) {
    const messages = this.activityStatusMessages(activity);
    return messages.length ? html`<p class="detail-note">${messages.join(" · ")}</p>` : null;
  }

  private activityStatusMessages(activity: Activity) {
    return this.contentStatusMessages(activity.contentEvidence);
  }

  private renderContentStatus(evidence?: ContentEvidence) {
    const messages = this.contentStatusMessages(evidence);
    return messages.length ? html`<p class="detail-note">${messages.join(" · ")}</p>` : null;
  }

  private contentStatusMessages(evidence?: ContentEvidence) {
    return [
      evidence?.truncated ? localization.t("content.truncated") : "",
      evidence?.redactionReason === "encrypted_input" ? localization.t("content.encrypted") : "",
    ].filter(Boolean);
  }

  private hasUnavailableContent(evidence?: ContentEvidence) {
    return evidence?.availability === "redacted" || evidence?.availability === "not_returned";
  }

  private exactActivityContent(read: SessionFileRead) {
    const activity = this.activityContent;
    if (!activity || !read.activityId || activity.source !== read.sourceId || activity.runId !== read.sessionId) return undefined;
    return activity.id === read.activityId || activityIdentity(activity) === read.activityId ? activity : undefined;
  }

  private isReferenceOnly(activity: Activity) {
    const evidence = activity.contentEvidence;
    return evidence?.kind === "reference"
      || (evidence?.evidence === "reference" && evidence.kind !== "tool_input" && evidence.kind !== "tool_output" && evidence.kind !== "tool_input_output");
  }

  private mappingLabel(read: SessionFileRead) {
    return read.outputMapping === "confirmed"
      ? localization.t("workspace.fileReadOutputConfirmed")
      : localization.t("workspace.fileReadOutputNotConfirmed");
  }

  private outputState(read: SessionFileRead) {
    if (read.outputMapping !== "confirmed") return localization.t("workspace.fileReadOutputNotConfirmed");
    return localization.t(`workspace.fileReadAvailability.${read.outputAvailability}` as Parameters<typeof localization.t>[0]);
  }

  private selectReference(reference: string) {
    if (reference === this.selectedReference && this.historyReads.length) return;
    this.selectedReference = reference;
    this.selectedReadId = "";
    this.historyReads = [];
    this.historyNextPageToken = "";
    this.focusDetailHeading = true;
    void this.loadHistory(reference, true);
  }

  private selectRead(read: SessionFileRead) {
    this.selectedReadId = read.id;
    this.selectedReadByReference.set(read.reference, read.id);
    this.focusDetailHeading = true;
    this.dispatchSelection(read, "file-read-selected");
  }

  private retry = () => {
    if (this.failedLoad === "history" && this.selectedReference) void this.loadHistory(this.selectedReference, true);
    else void this.loadCandidates(true);
  };

  private readonly historyChanged = (event: Event) => {
    const read = this.historyReads.find(({ id }) => id === (event.currentTarget as HTMLSelectElement).value);
    if (read) this.selectRead(read);
  };

  private selectNeighbor(index: number) {
    const read = this.historyReads[index];
    if (read) this.selectRead(read);
  }

  private openActivity(read: SessionFileRead) {
    this.dispatchSelection(read, "file-read-open-activity");
  }

  private dispatchSelection(read: SessionFileRead, name: string) {
    const detail: FileReadSelection = { activityId: read.activityId, readId: read.id };
    this.dispatchEvent(new CustomEvent(name, { detail, bubbles: true, composed: true }));
  }

  private async loadCandidates(reset: boolean) {
    const request = ++this.request;
    this.abort?.abort();
    const abort = this.abort = new AbortController();
    this.loading = true;
    this.error = "";
    this.failedLoad = "candidates";
    try {
      let token = reset ? "" : this.candidateNextPageToken;
      let accumulated = reset ? [] as SessionFileRead[] : [...this.candidateReads];
      let page;
      do {
        page = await agentmetryClient.listSessionFileReads(this.sourceId, this.sessionId, token, "", abort.signal);
        if (request !== this.request || abort.signal.aborted) return;
        accumulated = [...accumulated, ...page.reads];
        token = page.nextPageToken ?? "";
      } while (reset && (this.requestedReadId || this.requestedActivityId) && !accumulated.some((read) => this.isRequestedRead(read)) && page.hasMore);
      this.candidateReads = accumulated;
      this.candidateNextPageToken = token;
      this.distinctReferenceCount = page.distinctReferenceCount;
      const requested = accumulated.find((read) => this.isRequestedRead(read));
      const reference = requested?.reference ?? (this.requestedReadId || this.requestedActivityId ? "" : this.selectedReference || this.references[0] || "");
      if (reference && reference !== this.selectedReference) this.selectedReference = reference;
      if (reset && reference) await this.loadHistory(reference, true);
    } catch (error) {
      if (request === this.request && !abort.signal.aborted) this.error = error instanceof Error ? error.message : localization.t("workspace.fileReadsUnavailable");
    } finally {
      if (request === this.request) this.loading = false;
    }
  }

  private async loadHistory(reference: string, reset: boolean) {
    const request = ++this.request;
    this.abort?.abort();
    const abort = this.abort = new AbortController();
    this.loading = true;
    this.error = "";
    this.failedLoad = "history";
    try {
      let token = reset ? "" : this.historyNextPageToken;
      let accumulated = reset ? [] as SessionFileRead[] : [...this.historyReads];
      const requestedForReference = reset && this.candidateReads.some((read) => read.reference === reference && this.isRequestedRead(read));
      const remembered = this.selectedReadByReference.get(reference);
      let page;
      do {
        page = await agentmetryClient.listSessionFileReads(this.sourceId, this.sessionId, token, reference, abort.signal);
        if (request !== this.request || abort.signal.aborted) return;
        accumulated = [...accumulated, ...page.reads];
        token = page.nextPageToken ?? "";
      } while (reset && (requestedForReference || Boolean(remembered))
        && !accumulated.some((read) => requestedForReference ? this.isRequestedRead(read) : read.id === remembered)
        && page.hasMore);
      this.historyReads = accumulated;
      this.historyNextPageToken = token;
      this.coverage = page.coverage;
      const requested = requestedForReference ? this.historyReads.find((read) => this.isRequestedRead(read)) : undefined;
      const rememberedRead = remembered ? this.historyReads.find((read) => read.id === remembered) : undefined;
      if (requested) this.selectedReadId = requested.id;
      else if (rememberedRead) this.selectedReadId = rememberedRead.id;
      else if (reset && page.reads[0]) this.selectedReadId = page.reads[0].id;
      const selected = this.historyReads.find((read) => read.id === this.selectedReadId);
      if (selected) {
        this.selectedReadByReference.set(reference, selected.id);
        this.dispatchSelection(selected, "file-read-selected");
      }
    } catch (error) {
      if (request === this.request && !abort.signal.aborted) this.error = error instanceof Error ? error.message : localization.t("workspace.fileReadsUnavailable");
    } finally {
      if (request === this.request) this.loading = false;
    }
  }

  private readonly liveUpdate = (event: CustomEvent<LiveUpdateDelivery>) => {
    if (!this.active || (!event.detail.resyncRequired && !affectsSession(event.detail.targets, this.sourceId, this.sessionId))) return;
    event.detail.waitUntil(this.reloadAfterLiveUpdate());
  };

  private async reloadAfterLiveUpdate() {
    this.loadedKey = "";
    this.candidateReads = [];
    this.historyReads = [];
    this.selectedReadId = this.requestedReadId;
    if (this.active) await this.loadCandidates(true);
  }

  private get references() {
    return [...new Set(this.candidateReads.map((read) => read.reference))];
  }

  private isRequestedRead(read: SessionFileRead) {
    if (this.requestedReadId) return read.id === this.requestedReadId;
    return Boolean(this.requestedActivityId && read.activityId === this.requestedActivityId);
  }
}

const coverageLabel = (coverage: SessionFileRead["coverage"]) =>
  localization.t(`workspace.fileReadCoverage.${coverage}` as Parameters<typeof localization.t>[0]);

declare global { interface HTMLElementTagNameMap { "am-session-file-reads": SessionFileReads } }
