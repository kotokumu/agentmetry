import { css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { Trace } from "../model/telemetry";
import type { ConversationTarget } from "../model/trace-analysis";
import { aggregateTraceAgentUsage } from "../model/trace-analysis";
import { agentDisplayLabel } from "../model/agent-label";
import { notReported } from "../presentation/missing-data";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";
import "./token-breakdown";

@customElement("am-trace-participants")
export class TraceParticipants extends LocalizedElement {
  @property({ attribute: false }) trace?: Trace;
  @property({ attribute: false }) locationForConversation?: (target: ConversationTarget) => string;

  static styles = css`
    :host { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; }
    h3 { margin: 0 0 10px; font: 650 .875rem/1.2 Inter, ui-sans-serif, sans-serif; letter-spacing: .02em; }
    ul { display: grid; gap: 7px; list-style: none; margin: 0; padding: 0; }
    li { min-width: 0; border: 1px solid var(--am-border); border-left: 2px solid var(--am-accent); border-radius: 0 8px 8px 0; padding: 8px 10px; background: var(--am-surface); }
    a { display: block; color: var(--am-text); text-decoration: none; }
    a:hover, a:focus-visible { color: var(--am-accent); outline: 2px solid var(--am-accent-soft); outline-offset: 2px; }
    strong, code, small { display: block; overflow-wrap: anywhere; }
    code { font: .875rem/1.45 "SFMono-Regular", "Cascadia Code", monospace; }
    small { color: var(--am-muted); font-size: .75rem; }
    @media (max-width: 720px) { :host { grid-template-columns: 1fr; } }
  `;

  render() {
    const trace = this.trace;
    if (!trace) return null;
    const agents = aggregateTraceAgentUsage(trace);
    return html`<section><h3>${localization.t("participants.conversations")}</h3><ul>${trace.conversations.map((conversation) => {
      const target: ConversationTarget = { sourceId: conversation.sourceId, conversationId: conversation.id };
      const href = this.locationForConversation?.(target) ?? `/conversations/${encodeURIComponent(conversation.sourceId)}/${encodeURIComponent(conversation.id)}`;
      return html`<li><a data-participant href=${href} @click=${(event: MouseEvent) => this.conversationSelected(event, target)}><small>${conversation.sourceId || notReported()}</small><code>${conversation.id}</code></a></li>`;
    })}
    </ul></section>
    <section><h3>${localization.t("participants.agents")}</h3><ul>${agents.map((agent) => html`
      <li><small>${agent.sourceId} · ${agent.conversationId}</small><strong>${agentDisplayLabel(agent)}</strong><code>${agent.agentId || notReported()}</code><small>${[agent.agentType, agent.model].filter(Boolean).join(" · ") || notReported()}</small><small>${localization.t("participants.activityCount", { count: localization.number(agent.activityCount) })}</small><am-token-breakdown .usage=${agent.tokens} .compact=${true}></am-token-breakdown></li>`)}
    </ul></section>`;
  }

  private conversationSelected(event: MouseEvent, target: ConversationTarget) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    this.dispatchEvent(new CustomEvent("conversation-selected-from-trace", { detail: target, bubbles: true, composed: true }));
  }
}

declare global { interface HTMLElementTagNameMap { "am-trace-participants": TraceParticipants } }
