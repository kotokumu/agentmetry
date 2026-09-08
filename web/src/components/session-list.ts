import { css, html } from "lit";
import { msg, str } from "@lit/localize";
import { customElement, property, state } from "lit/decorators.js";
import type { SessionListEntry, SessionListView } from "../model/session-catalog";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";
import { notReported } from "../presentation/missing-data";

@customElement("am-session-list")
export class SessionList extends LocalizedElement {
  @property({ attribute: false }) sessions: readonly SessionListEntry[] = [];
  @property() view: SessionListView = "roots";
  @property({ type: Boolean }) hasMore = false;
  @property({ type: Boolean }) loadingMore = false;
  @property({ type: Boolean }) pageFailed = false;
  @property() selected = "";
  @property() selectedSource = "";
  @property({ type: Boolean }) loading = false;
  @property({ type: Boolean }) filterActive = false;
  @property({ type: Boolean }) unavailable = false;
  @property({ attribute: false }) locationForSession: (sourceId: string, sessionId: string) => string = conversationPath;
  @state() private copyStatus?: Readonly<{ id: string; state: "copied" | "failed" }>;

  static styles = css`
    :host { display: block; }
    .view-control { display: flex; align-items: center; gap: 8px; font-size: 14px; padding: 10px 0; cursor: pointer; }
    details { color: var(--am-muted); font-size: 12px; line-height: 1.5; margin-bottom: 12px; }
    summary { cursor: pointer; }
    button { margin-top: 10px; padding: 8px 12px; border: 1px solid var(--am-border); border-radius: 6px; background: var(--am-surface); color: var(--am-text); cursor: pointer; font: inherit; }
    button:focus-visible, input:focus-visible, summary:focus-visible { outline: 2px solid var(--am-accent); outline-offset: 2px; }
    button:disabled { cursor: wait; opacity: .6; }
    .collection-meta { margin: 8px 0; color: var(--am-muted); font-size: 14px; }
    .session-table { width: 100%; min-width: 0; border: 1px solid var(--am-border); border-radius: 8px; overflow: hidden; }
    .table-header, .session-row { display: grid; grid-template-columns: minmax(15rem, 2.2fr) minmax(10rem, 1.2fr) minmax(12rem, 1.1fr) minmax(8rem, .8fr) auto; gap: 16px; align-items: center; }
    .table-header { padding: 10px 14px; color: var(--am-muted); background: var(--am-surface-strong); font-size: 12px; font-weight: 700; }
    .session-row { border-top: 1px solid var(--am-border); border-left: 3px solid transparent; }
    .session-link { display: grid; grid-column: 1 / -2; grid-template-columns: subgrid; gap: 16px; align-items: center; min-width: 0; padding: 14px; color: var(--am-text); text-align: left; text-decoration: none; }
    .session-row:hover { background: var(--am-surface-strong); }
    .session-row[aria-current="true"] { border-left-color: var(--am-accent); background: var(--am-accent-soft); }
    .session-link:focus-visible, .copy-button:focus-visible { position: relative; z-index: 1; outline: 2px solid var(--am-accent); outline-offset: -2px; }
    a:focus-visible { border-color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    .copy-button { margin: 0 10px 0 0; padding: 7px 9px; white-space: nowrap; }
    strong { display: block; min-width: 0; overflow-wrap: anywhere; font: 600 14px/1.45 "SFMono-Regular", "Cascadia Code", monospace; }
    small { color: var(--am-muted); font-size: 12px; }
    .native-id { display: block; margin-top: 4px; overflow-wrap: anywhere; }
    .name-metadata { display: flex; flex-wrap: wrap; align-items: center; gap: 5px; margin-top: 4px; }
    .sources { display: flex; flex-wrap: wrap; gap: 4px; margin-bottom: 5px; }
    .source { display: inline-block; border: 1px solid var(--am-border-strong); border-radius: 4px; padding: 3px 6px; color: var(--am-accent); background: var(--am-accent-soft); font: 700 12px/1.2 "SFMono-Regular", "Cascadia Code", monospace; }
    .cell { min-width: 0; }
    .cell-label { display: block; margin-bottom: 4px; color: var(--am-muted); font-size: 12px; }
    .value { display: block; overflow-wrap: anywhere; }
    time { white-space: nowrap; }
    .empty { color: var(--am-muted); padding: 18px 0; }
    @media (max-width: 950px) {
      .table-header { display: none; }
      .session-table { border: 0; overflow: visible; }
      .session-row { display: block; border: 1px solid var(--am-border); border-radius: 8px; margin-top: 10px; }
      .session-link { display: flex; flex-direction: column; align-items: stretch; gap: 12px; padding: 14px; }
      .copy-button { margin: 0 14px 14px; }
      .cell-label { display: inline; margin: 0 6px 0 0; }
    }
    @media (prefers-reduced-motion: reduce) { a { transition: none; transform: none; } }
  `;

  render() {
    return html`
      <label class="view-control"><input type="checkbox" .checked=${this.view === "all"} @change=${this.viewChanged}>${localization.t("sessions.showAll")}</label>
      <details><summary>${localization.t("sessions.telemetryScope")}</summary><p>${localization.t("sessions.relationshipLimit")}</p><p>${localization.t("sessions.nameLimit")}</p></details>
      <p class="collection-meta" role="status">${this.hasMore
        ? msg(str`Showing ${localization.number(this.sessions.length)} loaded sessions · more available`, { id: "catalogCompletion.loadedMore" })
        : msg(str`Showing ${localization.number(this.sessions.length)} sessions`, { id: "catalogCompletion.loadedComplete" })}</p>
      ${this.renderRows()}
      ${this.pageFailed && !this.unavailable ? html`<p role="alert" class="empty">${localization.t("sessions.unavailable")}</p>` : null}
      ${this.pageFailed || this.unavailable ? html`<button type="button" @click=${() => this.dispatchEvent(new CustomEvent("sessions-retry-requested", { bubbles: true, composed: true }))}>${localization.t("sessions.retry")}</button>` : null}
      ${this.hasMore && !this.unavailable ? html`<button type="button" data-more ?disabled=${this.loading || this.loadingMore} @click=${() => this.dispatchEvent(new CustomEvent("sessions-more-requested", { bubbles: true, composed: true }))}>${localization.t(this.loadingMore ? "common.loading" : "sessions.loadMore")}</button>` : null}
    `;
  }

  private readonly viewChanged = (event: Event) => {
    const view: SessionListView = (event.target as HTMLInputElement).checked ? "all" : "roots";
    this.dispatchEvent(new CustomEvent("session-list-view-selected", { detail: { view }, bubbles: true, composed: true }));
  };

  private renderRows() {
    if (this.loading) return html`<p class="empty" role="status">${localization.t("sessions.loading")}</p>`;
    if (this.unavailable) return html`<p class="empty" role="alert">${localization.t("sessions.unavailable")}</p>`;
    if (this.sessions.length === 0) return html`<p class="empty">${localization.t(this.filterActive ? "sessions.noMatching" : "sessions.none")}</p>`;
    return html`<div class="session-table" role="list" aria-label=${localization.t("sessions.navigation")}>
      <div class="table-header" aria-hidden="true">
        <span>${msg("Session", { id: "catalogCompletion.sessionColumn" })}</span>
        <span>${msg("Source / relationship", { id: "catalogCompletion.sourceColumn" })}</span>
        <span>${msg("Observed time / activity", { id: "catalogCompletion.observedColumn" })}</span>
        <span>${msg("Tokens", { id: "catalogCompletion.tokensColumn" })}</span>
        <span aria-hidden="true"></span>
      </div>
      ${this.sessions.map((session) => this.renderRow(session))}
    </div>`;
  }

  private renderRow(session: SessionListEntry) {
    const selected = session.id === this.selected && session.sourceId === this.selectedSource;
    const title = session.catalog?.name?.text ?? session.id;
    const sourceLabels = session.sources?.length ? session.sources.map((source) => source.label) : [session.sourceId];
    const relationship = session.catalog?.role === "child" ? localization.t("sessions.child") : session.catalog?.role === "root" ? localization.t("sessions.root") : "";
    return html`<div class="session-row" role="listitem" aria-current=${String(selected)}>
      <a class="session-link" href=${this.locationForSession(session.sourceId, session.id)} aria-current=${selected ? "page" : "false"} aria-label=${msg(str`Open session ${title}`, { id: "catalogCompletion.openSession" })} @click=${(event: MouseEvent) => this.select(event, session.sourceId, session.id)}>
        <span class="cell">
          <strong title=${title}>${title}</strong>
          <small class="native-id" title=${session.id}>${session.id}</small>
          ${session.catalog?.name ? html`<span class="name-metadata"><span class="source name-origin" title=${session.catalog.name.origin}>${localization.t("sessions.generatedName")}</span>${session.catalog.name.observedAt ? html`<small>${localization.t("sessions.nameObserved")} <time datetime=${session.catalog.name.observedAt} title=${session.catalog.name.observedAt}>${localization.dateTime(new Date(session.catalog.name.observedAt))}</time></small>` : null}</span>` : null}
        </span>
        <span class="cell"><span class="cell-label">${msg("Source", { id: "catalogCompletion.sourceLabel" })}</span><span class="sources">${sourceLabels.map((label) => html`<span class="source">${label}</span>`)}${relationship ? html`<span class="source" title=${session.catalog?.parentSessionId}>${relationship}</span>` : null}</span>${session.catalog?.role === "child" && session.catalog.parentSessionId ? html`<small class="value">${session.catalog.parentSessionId}</small>` : null}</span>
        <span class="cell"><span class="cell-label">${msg("Observed", { id: "catalogCompletion.observedLabel" })}</span><span class="value">${session.startedAt ? html`<time datetime=${session.startedAt} title=${session.startedAt}>${localization.dateTime(new Date(session.startedAt))}</time>` : html`<span>${notReported()}</span>`}</span><small class="value">${session.activityCount === undefined ? notReported() : localization.t("sessions.counts", { agents: localization.number(session.agentCount ?? session.agents.length), activities: localization.number(session.activityCount) })}</small></span>
        <span class="cell"><span class="cell-label">${msg("Tokens", { id: "catalogCompletion.tokensLabel" })}</span><span class="value">${formatTokens(session.tokens)}</span></span>
      </a>
      <button class="copy-button" type="button" aria-label=${msg(str`Copy source-qualified session ID ${session.sourceId}:${session.id}`, { id: "catalogCompletion.copySessionId" })} title=${msg(str`Copy source-qualified session ID ${session.sourceId}:${session.id}`, { id: "catalogCompletion.copyTitle" })} @click=${(event: MouseEvent) => void this.copyId(event, session.sourceId, session.id)}>${this.copyStatus?.id === `${session.sourceId}:${session.id}` ? this.copyStatus.state === "copied" ? msg("Copied", { id: "catalogCompletion.copySuccess" }) : msg("Copy failed", { id: "catalogCompletion.copyFailure" }) : msg("Copy ID", { id: "catalogCompletion.copyButton" })}</button>
    </div>`;
  }

  private async copyId(event: MouseEvent, sourceId: string, sessionId: string) {
    event.stopPropagation();
    const identity = `${sourceId}:${sessionId}`;
    if (typeof navigator.clipboard?.writeText !== "function") {
      this.copyStatus = { id: identity, state: "failed" };
      return;
    }
    try {
      await navigator.clipboard.writeText(identity);
      this.copyStatus = { id: identity, state: "copied" };
    } catch {
      this.copyStatus = { id: identity, state: "failed" };
    }
  }

  private select(event: MouseEvent, sourceId: string, sessionId: string) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    this.dispatchEvent(new CustomEvent("session-selected", {
      detail: { sourceId, sessionId }, bubbles: true, composed: true,
    }));
  }
}

const formatTokens = (tokens: SessionListEntry["tokens"]) => {
  if (tokens.total !== null) return localization.number(tokens.total);
  const hasReportedPart = [tokens.input, tokens.output, tokens.cacheRead, tokens.cacheWrite, tokens.reasoning].some((value) => value !== null);
  return hasReportedPart ? localization.t("common.partial") : msg("Available in session details", { id: "catalogCompletion.tokensInDetails" });
};

const conversationPath = (sourceId: string, conversationId: string) =>
  `/conversations/${encodeURIComponent(sourceId)}/${encodeURIComponent(conversationId)}`;

declare global { interface HTMLElementTagNameMap { "am-session-list": SessionList } }
