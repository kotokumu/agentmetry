import { css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { Session } from "../model/telemetry";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";

@customElement("am-session-list")
export class SessionList extends LocalizedElement {
  @property({ attribute: false }) sessions: readonly Session[] = [];
  @property() selected = "";
  @property() selectedSource = "";
  @property({ type: Boolean }) loading = false;
  @property({ type: Boolean }) filterActive = false;
  @property({ type: Boolean }) unavailable = false;
  @property({ attribute: false }) locationForSession: (sourceId: string, sessionId: string) => string = conversationPath;

  static styles = css`
    :host { display: block; }
    nav { display: grid; gap: 5px; }
    a { position: relative; display: block; width: 100%; overflow: hidden; border: 1px solid transparent; border-radius: 7px; background: transparent; color: var(--am-text); padding: 9px 10px; text-align: left; cursor: pointer; text-decoration: none; transition: border-color .18s ease, background .18s ease, transform .18s ease; }
    a:hover { border-color: var(--am-border); background: rgba(255, 255, 255, .02); transform: translateX(2px); }
    a[aria-current="page"] { border-color: var(--am-border-strong); background: linear-gradient(90deg, var(--am-accent-soft), rgba(109, 244, 214, .02)); box-shadow: inset 2px 0 0 var(--am-accent); }
    a:focus-visible { border-color: var(--am-accent); outline: 2px solid var(--am-accent-soft); }
    strong { display: block; overflow: hidden; text-overflow: ellipsis; font: 0.76rem/1.4 "SFMono-Regular", "Cascadia Code", monospace; }
    small { color: var(--am-muted); font-size: .68rem; }
    .sources { display: flex; flex-wrap: wrap; gap: 4px; margin-bottom: 5px; }
    .source { border: 1px solid var(--am-border-strong); border-radius: 4px; padding: 2px 5px; color: var(--am-accent); background: var(--am-accent-soft); font: 700 .58rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; text-transform: uppercase; letter-spacing: .04em; }
    .empty { color: var(--am-muted); padding: 18px 0; }
    @media (prefers-reduced-motion: reduce) { a { transition: none; transform: none; } }
  `;

  render() {
    if (this.loading) return html`<p class="empty" role="status">${localization.t("sessions.loading")}</p>`;
    if (this.unavailable) return html`<p class="empty" role="alert">${localization.t("sessions.unavailable")}</p>`;
    if (this.sessions.length === 0) return html`<p class="empty">${localization.t(this.filterActive ? "sessions.noMatching" : "sessions.none")}</p>`;
    return html`<nav aria-label=${localization.t("sessions.navigation")}>${this.sessions.map((session) => html`
      <a
        href=${this.locationForSession(session.sourceId, session.id)}
        aria-current=${session.id === this.selected && session.sourceId === this.selectedSource ? "page" : "false"}
        @click=${(event: MouseEvent) => this.select(event, session.sourceId, session.id)}
      >
        <span class="sources">${(session.sources ?? []).map((source) => html`<span class="source">${source.label}</span>`)}</span>
        <strong>${session.id}</strong>
        <small>${localization.t("sessions.counts", { agents: localization.number(session.agentCount ?? session.agents.length), activities: localization.number(session.activityCount) })}</small>
      </a>
    `)}</nav>`;
  }

  private select(event: MouseEvent, sourceId: string, sessionId: string) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    this.dispatchEvent(new CustomEvent("session-selected", {
      detail: { sourceId, sessionId }, bubbles: true, composed: true,
    }));
  }
}

const conversationPath = (sourceId: string, conversationId: string) =>
  `/conversations/${encodeURIComponent(sourceId)}/${encodeURIComponent(conversationId)}`;

declare global { interface HTMLElementTagNameMap { "am-session-list": SessionList } }
