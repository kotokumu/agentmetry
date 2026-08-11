import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { Trace } from "../model/update";
import { aggregateTraceAgentUsage } from "../model/trace-analysis";
import { agentDisplayLabel } from "../model/agent-label";
import "./token-breakdown";

@customElement("am-trace-participants")
export class TraceParticipants extends LitElement {
  @property({ attribute: false }) trace?: Trace;

  static styles = css`
    :host { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; }
    h3 { margin: 0 0 10px; font: 600 .86rem/1.2 "Iowan Old Style", "Palatino Linotype", serif; }
    ul { display: grid; gap: 7px; list-style: none; margin: 0; padding: 0; }
    li { min-width: 0; border-left: 2px solid var(--am-accent); padding: 6px 9px; background: var(--am-surface-strong); }
    strong, code, small { display: block; overflow-wrap: anywhere; }
    code { font: .72rem/1.45 "SFMono-Regular", "Cascadia Code", monospace; }
    small { color: var(--am-muted); font-size: .7rem; }
    @media (max-width: 720px) { :host { grid-template-columns: 1fr; } }
  `;

  render() {
    const trace = this.trace;
    if (!trace) return null;
    const agents = aggregateTraceAgentUsage(trace);
    return html`<section><h3>Conversations</h3><ul>${trace.conversations.map((conversation) => html`
      <li><small>${conversation.sourceId || "unknown source"}</small><code>${conversation.id}</code></li>`)}
    </ul></section>
    <section><h3>Agents</h3><ul>${agents.map((agent) => html`
      <li><small>${agent.sourceId} · ${agent.conversationId}</small><strong>${agentDisplayLabel(agent)}</strong><code>${agent.agentId || "unavailable"}</code><small>${[agent.agentType, agent.model].filter(Boolean).join(" · ") || "Metadata unavailable"}</small><small>${agent.activityCount.toLocaleString()} activities</small><am-token-breakdown .usage=${agent.tokens} .compact=${true}></am-token-breakdown></li>`)}
    </ul></section>`;
  }
}

declare global { interface HTMLElementTagNameMap { "am-trace-participants": TraceParticipants } }
