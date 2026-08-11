import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { Session } from "../model/update";

@customElement("am-session-list")
export class SessionList extends LitElement {
  @property({ attribute: false }) sessions: readonly Session[] = [];
  @property() selected = "";
  @property() selectedSource = "";
  @property({ type: Boolean }) loading = false;

  static styles = css`
    :host { display: block; }
    nav { display: grid; gap: 8px; }
    a { display: block; width: 100%; border: 1px solid transparent; border-radius: 10px; background: transparent; color: var(--am-text); padding: 11px; text-align: left; cursor: pointer; text-decoration: none; }
    a:hover, a[aria-current="page"] { border-color: var(--am-border); background: var(--am-surface-strong); }
    strong { display: block; overflow: hidden; text-overflow: ellipsis; font: 0.76rem/1.4 "SFMono-Regular", "Cascadia Code", monospace; }
    small { color: var(--am-muted); }
    .sources { display: flex; flex-wrap: wrap; gap: 4px; margin-bottom: 5px; }
    .source { border: 1px solid var(--am-border); border-radius: 99px; padding: 2px 6px; color: var(--am-accent); font: 700 .62rem/1.2 "SFMono-Regular", "Cascadia Code", monospace; }
    .empty { color: var(--am-muted); padding: 18px 0; }
  `;

  render() {
    if (this.loading) return html`<p class="empty" role="status">Loading conversations…</p>`;
    if (this.sessions.length === 0) return html`<p class="empty">No agent conversations observed yet.</p>`;
    return html`<nav aria-label="Agent conversations">${this.sessions.map((session) => html`
      <a
        href=${conversationPath(session.sourceId, session.id)}
        aria-current=${session.id === this.selected && session.sourceId === this.selectedSource ? "page" : "false"}
        @click=${(event: MouseEvent) => this.select(event, session.sourceId, session.id)}
      >
        <span class="sources">${(session.sources ?? []).map((source) => html`<span class="source">${source.label}</span>`)}</span>
        <strong>${session.id}</strong>
        <small>${session.agentCount ?? session.agents.length} agents · ${session.activityCount} activities</small>
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
