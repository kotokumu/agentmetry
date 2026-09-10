import { css, html, type PropertyValues } from "lit";
import { repeat } from "lit/directives/repeat.js";
import { msg, str } from "@lit/localize";
import { customElement, property, state } from "lit/decorators.js";
import type { SessionListEntry, SessionListView } from "../model/session-catalog";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";
import { notReported } from "../presentation/missing-data";
import { costCoverageHint, formatCostSummary } from "../presentation/cost";

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
  private pageObserver?: IntersectionObserver;
  private pageRequested = false;
  private readingAnchor?: { row: Element; top: number };

  static styles = css`
    :host { display: block; overflow-anchor: none; }
    .view-control { display: flex; align-items: center; gap: 8px; font-size: 14px; padding: 10px 0; cursor: pointer; }
    details { color: var(--am-muted); font-size: 12px; line-height: 1.5; margin-bottom: 12px; }
    summary { cursor: pointer; }
    button { margin-top: 10px; padding: 8px 12px; border: 1px solid var(--am-border); border-radius: 6px; background: var(--am-surface); color: var(--am-text); cursor: pointer; font: inherit; }
    button:focus-visible, input:focus-visible, summary:focus-visible { outline: 2px solid var(--am-accent); outline-offset: 2px; }
    button:disabled { cursor: wait; opacity: .6; }
    .collection-meta { margin: 8px 0; color: var(--am-muted); font-size: 14px; }
    .session-table { width: 100%; min-width: 0; border: 1px solid var(--am-border); border-radius: 8px; overflow: hidden; }
    .table-header, .session-row { display: grid; grid-template-columns: minmax(15rem, 2.2fr) minmax(10rem, 1.2fr) minmax(12rem, 1.1fr) minmax(8rem, .8fr) minmax(9rem, .9fr) auto; gap: 16px; align-items: center; }
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
    .page-end { min-height: 28px; color: var(--am-muted); font-size: 12px; text-align: center; padding: 10px 0; }
    :host([compact]) .table-header { display: none; }
    :host([compact]) .session-table { border: 0; }
    :host([compact]) .session-row { display: block; border-top: 0; border-bottom: 1px solid var(--am-border); border-radius: 5px; margin: 0 0 4px; }
    :host([compact]) .session-link { display: flex; flex-direction: column; align-items: stretch; gap: 6px; padding: 12px 10px; }
    :host([compact]) .cell-label { display: none; }
    :host([compact]) .cell:nth-child(4), :host([compact]) .cell:nth-child(5) { display: none; }
    :host([compact]) .name-metadata { display: none; }
    :host([compact]) .native-id { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    :host([compact]) strong { font-family: inherit; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
    :host([compact]) .source { padding: 2px 5px; font-weight: 500; }
    :host([compact]) .sources { margin: 0; }
    :host([compact]) .copy-button { margin: 0 10px 8px; padding: 3px 6px; font-size: 12px; }
    :host([compact]) .collection-meta { font-size: 12px; }
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
      <div class="page-end" role="status">${this.loadingMore ? localization.t("common.loading") : ""}</div>
    `;
  }

  connectedCallback() {
    super.connectedCallback();
    this.addEventListener("scroll", this.scrollChanged, { passive: true });
    this.requestUpdate();
  }

  protected willUpdate(changed: PropertyValues<this>) {
    if (!changed.has("sessions") || this.scrollTop <= 0) return;
    const top = this.getBoundingClientRect().top;
    const row = [...(this.shadowRoot?.querySelectorAll(".session-row") ?? [])].find((item) => item.getBoundingClientRect().bottom > top);
    this.readingAnchor = row ? { row, top: row.getBoundingClientRect().top } : undefined;
  }

  protected updated(changed: PropertyValues<this>) {
    if (this.readingAnchor?.row.isConnected) this.scrollTop += this.readingAnchor.row.getBoundingClientRect().top - this.readingAnchor.top;
    this.readingAnchor = undefined;
    if (this.pageObserver && !(["sessions", "hasMore", "loading", "loadingMore", "pageFailed", "unavailable"] as const).some((key) => changed.has(key))) return;
    this.pageRequested = false;
    this.pageObserver?.disconnect();
    if (!this.hasMore || this.loading || this.loadingMore || this.pageFailed || this.unavailable || typeof IntersectionObserver === "undefined") return;
    this.pageObserver = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) this.requestNextPage();
    }, { root: this, rootMargin: "0px 0px 200px" });
    const end = this.shadowRoot?.querySelector(".page-end");
    if (end) this.pageObserver.observe(end);
  }

  disconnectedCallback() {
    this.removeEventListener("scroll", this.scrollChanged);
    this.pageObserver?.disconnect();
    this.pageObserver = undefined;
    super.disconnectedCallback();
  }

  private readonly viewChanged = (event: Event) => {
    const view: SessionListView = (event.target as HTMLInputElement).checked ? "all" : "roots";
    this.dispatchEvent(new CustomEvent("session-list-view-selected", { detail: { view }, bubbles: true, composed: true }));
  };

  private readonly scrollChanged = () => {
    if (this.scrollHeight - this.scrollTop - this.clientHeight <= 200) this.requestNextPage();
  };

  private requestNextPage() {
    if (this.pageRequested || !this.isConnected || !this.hasMore || this.loading || this.loadingMore || this.pageFailed || this.unavailable) return;
    this.pageRequested = true;
    this.dispatchEvent(new CustomEvent("sessions-more-requested", { bubbles: true, composed: true }));
  }

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
        <span>${msg("Estimated cost", { id: "catalogCompletion.costColumn" })}</span>
        <span aria-hidden="true"></span>
      </div>
      ${repeat(this.sessions, (session) => JSON.stringify([session.sourceId, session.id]), (session) => this.renderRow(session))}
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
          ${session.catalog?.name ? html`<span class="name-metadata"><span class="source name-origin" title=${session.catalog.name.origin}>${localization.t(session.catalog.name.origin === "codex_app.list_threads" ? "sessions.observedName" : "sessions.generatedName")}</span>${session.catalog.name.observedAt ? html`<small>${localization.t("sessions.nameObserved")} <time datetime=${session.catalog.name.observedAt} title=${session.catalog.name.observedAt}>${localization.dateTime(new Date(session.catalog.name.observedAt))}</time></small>` : null}</span>` : null}
        </span>
        <span class="cell"><span class="cell-label">${msg("Source", { id: "catalogCompletion.sourceLabel" })}</span><span class="sources">${sourceLabels.map((label) => html`<span class="source">${label}</span>`)}${relationship ? html`<span class="source" title=${session.catalog?.parentSessionId}>${relationship}</span>` : null}</span>${session.catalog?.role === "child" && session.catalog.parentSessionId ? html`<small class="value">${session.catalog.parentSessionId}</small>` : null}</span>
        <span class="cell"><span class="cell-label">${msg("Observed", { id: "catalogCompletion.observedLabel" })}</span><span class="value">${session.startedAt ? html`<time datetime=${session.startedAt} title=${session.startedAt}>${localization.dateTime(new Date(session.startedAt))}</time>` : html`<span>${notReported()}</span>`}</span><small class="value">${session.activityCount === undefined ? notReported() : localization.t("sessions.counts", { agents: localization.number(session.agentCount ?? session.agents.length), activities: localization.number(session.activityCount) })}</small></span>
        <span class="cell"><span class="cell-label">${msg("Tokens", { id: "catalogCompletion.tokensLabel" })}</span><span class="value">${formatTokens(session.tokens)}</span></span>
        <span class="cell"><span class="cell-label">${msg("Estimated cost", { id: "catalogCompletion.costLabel" })}</span><span class="value">${formatCostSummary(session.costSummary)}</span><small class="value">${costCoverageHint(session.costSummary)}</small></span>
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
