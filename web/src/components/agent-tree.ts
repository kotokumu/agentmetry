import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { AgentSession } from "../model/update";

@customElement("am-agent-tree")
export class AgentTree extends LitElement {
  @property({ attribute: false }) agents: readonly AgentSession[] = [];
  @property() selectedAgentId = "";

  static styles = css`
    :host { display: block; max-width: 100%; }
    .viewport { max-width: 100%; max-height: 280px; overflow: auto; overscroll-behavior: contain; padding: 4px 3px 8px; }
    .graph { position: relative; min-width: 440px; }
    svg { position: absolute; inset: 0; pointer-events: none; overflow: visible; }
    path { fill: none; stroke: var(--am-accent); stroke-width: 1.5; opacity: .55; }
    circle { fill: var(--am-accent); opacity: .8; }
    .node { position: absolute; min-height: 72px; border: 1px solid var(--am-border); border-left: 3px solid var(--am-accent); border-radius: 8px; padding: 6px 9px; background: var(--am-surface-strong); box-shadow: 0 4px 12px rgba(23, 32, 52, .06); color: var(--am-text); cursor: pointer; text-align: left; }
    .node:hover, .node:focus-visible { border-color: var(--am-accent); outline: 2px solid color-mix(in srgb, var(--am-accent) 35%, transparent); outline-offset: 2px; }
    .node[aria-selected="true"] { background: var(--am-accent-soft); border-color: var(--am-accent); }
    .node-title { display: flex; align-items: baseline; gap: 7px; min-width: 0; }
    .node-title strong { overflow: hidden; color: var(--am-text); font: .76rem/1.3 "SFMono-Regular", "Cascadia Code", monospace; text-overflow: ellipsis; white-space: nowrap; }
    .role { color: var(--am-accent); font: 700 .58rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .08em; text-transform: uppercase; }
    code { display: block; margin-top: 3px; color: var(--am-muted); font-size: .64rem; overflow-wrap: anywhere; }
    .meta { margin-top: 3px; color: var(--am-muted); font-size: .68rem; overflow-wrap: anywhere; }
    .usage { display: flex; flex-wrap: wrap; align-items: baseline; gap: 3px 8px; margin-top: 4px; color: var(--am-muted); font-size: .68rem; }
    .usage p { margin: 0; overflow-wrap: anywhere; }
    .usage details { flex-basis: 100%; }
    .usage summary { width: fit-content; cursor: pointer; color: var(--am-accent); font-size: .66rem; }
    .usage details p { margin-top: 3px; }
    .empty { color: var(--am-muted); font-size: .78rem; }
  `;

  render() {
    const layout = layoutAgentTree(buildAgentTree(this.agents));
    if (layout.nodes.length === 0) return html`<p class="empty">N/A</p>`;
    const nodesByID = new Map(layout.nodes.map((node) => [node.node.agent.agentId, node]));
    return html`<div class="viewport" role="tree" aria-label="Agent delegation topology"><div class="graph" style=${`width:${layout.width}px;height:${layout.height}px`}>
      <svg width=${layout.width} height=${layout.height} aria-hidden="true">
        ${layout.nodes.filter((node) => node.parentID).map((node) => {
          const parent = nodesByID.get(node.parentID!);
          if (!parent) return null;
          const parentX = parent.depth * (NODE_WIDTH + LEVEL_GAP) + NODE_WIDTH;
          const childX = node.depth * (NODE_WIDTH + LEVEL_GAP);
          const control = Math.max(18, (childX - parentX) / 2);
          return html`<path d=${`M ${parentX} ${parent.centerY} C ${parentX + control} ${parent.centerY}, ${childX - control} ${node.centerY}, ${childX} ${node.centerY}`}></path><circle cx=${childX} cy=${node.centerY} r="3"></circle>`;
        })}
      </svg>
      ${layout.nodes.map((node) => renderGraphNode(node, this.selectedAgentId, (event: Event) => this.selectAgent(event, node.node.agent.agentId)))}
    </div></div>`;
  }

  private selectAgent(event: Event, agentId: string) {
    if ((event.target as HTMLElement).closest("details")) return;
    this.dispatchEvent(new CustomEvent("agent-selected", {
      detail: { agentId: this.selectedAgentId === agentId ? "" : agentId }, bubbles: true, composed: true,
    }));
  }
}

type AgentNode = Readonly<{ agent: AgentSession; children: readonly AgentNode[] }>;
type LayoutNode = Readonly<{ node: AgentNode; parentID?: string; depth: number; centerY: number }>;
type TreeLayout = Readonly<{ nodes: readonly LayoutNode[]; width: number; height: number }>;

const NODE_WIDTH = 190;
const LEVEL_GAP = 52;
const ROW_GAP = 94;
const TOP_PADDING = 40;

export const buildAgentTree = (agents: readonly AgentSession[]): readonly AgentNode[] => {
  const byID = new Map(agents.map((agent) => [agent.agentId, { agent, children: [] as AgentNode[] }]));
  const roots: AgentNode[] = [];
  for (const node of byID.values()) {
    const parent = node.agent.parentAgentId ? byID.get(node.agent.parentAgentId) : undefined;
    if (parent && parent !== node) parent.children.push(node);
    else roots.push(node);
  }
  return roots;
};

const layoutAgentTree = (roots: readonly AgentNode[]): TreeLayout => {
  const nodes: LayoutNode[] = [];
  let leafIndex = 0;
  let maxDepth = 0;
  const visit = (node: AgentNode, depth: number, parentID?: string): number => {
    maxDepth = Math.max(maxDepth, depth);
    const childCenters = node.children.map((child) => visit(child, depth + 1, node.agent.agentId));
    const centerY = childCenters.length > 0
      ? (childCenters[0] + childCenters[childCenters.length - 1]) / 2
      : TOP_PADDING + leafIndex++ * ROW_GAP;
    nodes.push({ node, parentID, depth, centerY });
    return centerY;
  };
  roots.forEach((root) => visit(root, 0));
  return {
    nodes,
    width: Math.max(440, (maxDepth + 1) * (NODE_WIDTH + LEVEL_GAP) + 12),
    height: Math.max(100, Math.max(1, leafIndex) * ROW_GAP),
  };
};

const renderGraphNode = (layout: LayoutNode, selectedAgentId: string, select: (event: Event) => void): unknown => html`<div
  class="node"
  role="treeitem"
  aria-level=${layout.depth + 1}
  aria-selected=${String(selectedAgentId === layout.node.agent.agentId)}
  data-agent-id=${layout.node.agent.agentId}
  tabindex="0"
  style=${`left:${layout.depth * (NODE_WIDTH + LEVEL_GAP)}px;top:${layout.centerY - 36}px;width:${NODE_WIDTH}px`}
  @click=${select}
  @keydown=${(event: KeyboardEvent) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); select(event); } }}
>
  <div class="node-title"><span class="role">${layout.depth === 0 ? "Root" : "Child"}</span><strong title=${layout.node.agent.agentDefinition || "N/A"}>${layout.node.agent.agentDefinition || "N/A"}</strong></div>
  <code>Runtime ID: ${layout.node.agent.agentId || "N/A"}</code>
  <div class="meta">${layout.node.agent.agentType || "N/A"} · ${layout.node.agent.model || "N/A"}</div>
  <div class="usage">
    <p>${layout.node.agent.activityCount} activities · ${agentTokenSummary(layout.node.agent.tokens)}</p>
    ${agentTokenDetails(layout.node.agent.tokens) ? html`<details><summary @click=${(event: MouseEvent) => event.stopPropagation()}>Token breakdown</summary><p>${agentTokenDetails(layout.node.agent.tokens)}</p></details>` : null}
  </div>
</div>`;

const agentTokenSummary = (usage: AgentSession["tokens"]) => {
  if (usage.total !== null) return `${usage.total.toLocaleString()} observed tokens`;
  const known = [usage.input, usage.output].filter((value): value is number => value !== null);
  if (known.length === 0) return "N/A";
  return `${known.reduce((total, value) => total + value, 0).toLocaleString()} observed token subtotal`;
};

const agentTokenDetails = (usage: AgentSession["tokens"]) => [
  ["input", usage.input],
  ["output", usage.output],
  ["cache read", usage.cacheRead],
  ["cache write", usage.cacheWrite],
  ["reasoning", usage.reasoning],
].filter((entry): entry is [string, number] => entry[1] !== null)
  .map(([label, value]) => `${label} ${value.toLocaleString()}`)
  .join(" · ");

declare global { interface HTMLElementTagNameMap { "am-agent-tree": AgentTree } }
