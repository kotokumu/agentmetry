import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { AgentSession } from "../model/update";
import { agentDisplayLabel } from "../model/agent-label";
import "./token-breakdown";

@customElement("am-agent-tree")
export class AgentTree extends LitElement {
  @property({ attribute: false }) agents: readonly AgentSession[] = [];
  @property() selectedAgentId = "";

  static styles = css`
    :host { display: block; max-width: 100%; }
    .viewport { width: 100%; max-width: 100%; min-height: 220px; max-height: min(560px, 65vh); overflow: auto; overscroll-behavior: contain; padding: 4px 3px 8px; }
    .graph { position: relative; min-width: max(440px, 100%); }
    .connector { position: absolute; z-index: 1; border-radius: 99px; background: var(--am-accent); opacity: .8; pointer-events: none; }
    .node { position: absolute; z-index: 2; height: 96px; overflow: hidden; border: 1px solid var(--am-border); border-left: 3px solid var(--am-accent); border-radius: 8px; padding: 7px 9px; background: var(--am-surface-strong); box-shadow: 0 4px 12px rgba(23, 32, 52, .06); color: var(--am-text); cursor: pointer; text-align: left; }
    .node:hover, .node:focus-visible { border-color: var(--am-accent); outline: 2px solid color-mix(in srgb, var(--am-accent) 35%, transparent); outline-offset: 2px; }
    .node[aria-selected="true"] { background: var(--am-accent-soft); border-color: var(--am-accent); }
    .node-title { display: flex; align-items: baseline; gap: 7px; min-width: 0; }
    .node-title strong { overflow: hidden; color: var(--am-text); font: .76rem/1.3 "SFMono-Regular", "Cascadia Code", monospace; text-overflow: ellipsis; white-space: nowrap; }
    .role { color: var(--am-accent); font: 700 .58rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .08em; text-transform: uppercase; }
    code { display: block; margin-top: 3px; color: var(--am-muted); font-size: .64rem; overflow-wrap: anywhere; }
    .meta { margin-top: 3px; overflow: hidden; color: var(--am-muted); font-size: .68rem; text-overflow: ellipsis; white-space: nowrap; }
    .usage { display: grid; gap: 2px; margin-top: 4px; color: var(--am-muted); font-size: .68rem; }
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
      ${layout.nodes.filter((node) => node.parentID).map((node) => {
        const parent = nodesByID.get(node.parentID!);
        if (!parent) return null;
        return renderConnector(parent, node);
      })}
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

export type AgentNode = Readonly<{ agent: AgentSession; children: readonly AgentNode[] }>;
export type LayoutNode = Readonly<{ node: AgentNode; parentID?: string; depth: number; centerX: number; centerY: number }>;
export type TreeLayout = Readonly<{ nodes: readonly LayoutNode[]; width: number; height: number }>;

const NODE_WIDTH = 190;
const NODE_HEIGHT = 96;
const LEVEL_GAP = 82;
const ROW_GAP = 220;
const SIDE_PADDING = NODE_WIDTH / 2 + 24;
const MAX_COLUMNS = 4;
const ROOT_COLUMNS = 2;
const ROOT_GROUP_WIDTH = SIDE_PADDING * 2 + (MAX_COLUMNS - 1) * ROW_GAP;
const ROOT_ROW_GAP = 24;
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

const renderConnector = (parent: LayoutNode, child: LayoutNode): unknown => {
  const parentBottom = parent.centerY + NODE_HEIGHT / 2;
  const childTop = child.centerY - NODE_HEIGHT / 2;
  const midY = parentBottom + (childTop - parentBottom) / 2;
  const left = Math.min(parent.centerX, child.centerX);
  return html`
    <span class="connector" aria-hidden="true" style=${`left:${parent.centerX - 1.5}px;top:${parentBottom}px;width:3px;height:${Math.max(3, midY - parentBottom)}px`}></span>
    <span class="connector" aria-hidden="true" style=${`left:${left}px;top:${midY - 1.5}px;width:${Math.max(3, Math.abs(child.centerX - parent.centerX))}px;height:3px`}></span>
    <span class="connector" aria-hidden="true" style=${`left:${child.centerX - 1.5}px;top:${midY}px;width:3px;height:${Math.max(3, childTop - midY)}px`}></span>
  `;
};

export const layoutAgentTree = (roots: readonly AgentNode[]): TreeLayout => {
  const groups = [...roots]
    .sort((left, right) => Number(right.children.length > 0) - Number(left.children.length > 0))
    .map((root) => {
      const levels: Array<Array<{ node: AgentNode; parentID?: string; depth: number }>> = [];
      const visit = (node: AgentNode, depth: number, parentID?: string) => {
        (levels[depth] ??= []).push({ node, parentID, depth });
        node.children.forEach((child) => visit(child, depth + 1, node.agent.agentId));
      };
      visit(root, 0);
      const rowHeight = NODE_HEIGHT + LEVEL_GAP;
      const height = levels.reduce((total, level) => total + Math.ceil(level.length / MAX_COLUMNS) * rowHeight, 0) - LEVEL_GAP + 24;
      return { levels, height };
    });

  const rootColumns = Math.min(ROOT_COLUMNS, Math.max(1, groups.length));
  const rootRows = Math.ceil(groups.length / rootColumns);
  const rowHeights = Array.from({ length: rootRows }, (_, row) => Math.max(...groups.slice(row * rootColumns, (row + 1) * rootColumns).map((group) => group.height)));
  const rowTops: number[] = [];
  rowHeights.reduce((top, height, index) => {
    rowTops[index] = top;
    return top + height + ROOT_ROW_GAP;
  }, TOP_PADDING);

  const nodes: LayoutNode[] = [];
  groups.forEach((group, groupIndex) => {
    const groupStartX = (groupIndex % rootColumns) * ROOT_GROUP_WIDTH;
    let top = rowTops[Math.floor(groupIndex / rootColumns)];
    for (const level of group.levels) {
      const columns = Math.min(MAX_COLUMNS, Math.max(1, level.length));
      const firstCenterX = groupStartX + (ROOT_GROUP_WIDTH - (columns - 1) * ROW_GAP) / 2;
      const rowHeight = NODE_HEIGHT + LEVEL_GAP;
      level.forEach((entry, index) => {
        const row = Math.floor(index / MAX_COLUMNS);
        const column = index % MAX_COLUMNS;
        nodes.push({
          ...entry,
          centerX: firstCenterX + column * ROW_GAP,
          centerY: top + row * rowHeight + NODE_HEIGHT / 2,
        });
      });
      top += Math.ceil(level.length / MAX_COLUMNS) * rowHeight;
    }
  });

  const height = rowTops.length === 0 ? 160 : rowTops[rowTops.length - 1] + rowHeights[rowHeights.length - 1];
  return {
    nodes,
    width: Math.max(440, rootColumns * ROOT_GROUP_WIDTH),
    height: Math.max(160, height),
  };
};

const renderGraphNode = (layout: LayoutNode, selectedAgentId: string, select: (event: Event) => void): unknown => html`<div
  class="node"
  role="treeitem"
  aria-level=${layout.depth + 1}
  aria-selected=${String(selectedAgentId === layout.node.agent.agentId)}
  data-agent-id=${layout.node.agent.agentId}
  tabindex="0"
  style=${`left:${layout.centerX - NODE_WIDTH / 2}px;top:${layout.centerY - NODE_HEIGHT / 2}px;width:${NODE_WIDTH}px`}
  @pointerdown=${select}
  @keydown=${(event: KeyboardEvent) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); select(event); } }}
>
  <div class="node-title"><span class="role">${layout.depth === 0 ? "Root" : "Child"}</span><strong title=${agentDisplayLabel(layout.node.agent)}>${agentDisplayLabel(layout.node.agent)}</strong></div>
  <code>Runtime ID: ${layout.node.agent.agentId || "unavailable"}</code>
  <div class="meta">${[layout.node.agent.agentType, layout.node.agent.model].filter(Boolean).join(" · ") || "Metadata unavailable"}</div>
  <div class="usage">
    <p>${layout.node.agent.activityCount} activities</p>
    <am-token-breakdown .usage=${layout.node.agent.tokens} .compact=${true} @pointerdown=${(event: PointerEvent) => event.stopPropagation()}></am-token-breakdown>
  </div>
</div>`;

declare global { interface HTMLElementTagNameMap { "am-agent-tree": AgentTree } }
