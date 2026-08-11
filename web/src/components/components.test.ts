import { afterEach, describe, expect, it, vi } from "vitest";
import "./kpi-card";
import "./activity-table";
import "./time-range-filter";
import "./plan-usage";
import "./agent-tree";
import "./session-filter";
import "./session-list";
import "./trace-summary";
import "./trace-participants";
import "./trace-waterfall";
import "./token-chart";
import type { ActivityTable } from "./activity-table";
import type { KpiCard } from "./kpi-card";
import type { TimeRangeFilter } from "./time-range-filter";
import type { PlanUsage } from "./plan-usage";
import { buildAgentTree, layoutAgentTree } from "./agent-tree";
import type { AgentTree } from "./agent-tree";
import type { SessionFilter } from "./session-filter";
import type { SessionList } from "./session-list";
import type { TraceSummary } from "./trace-summary";
import type { TraceParticipants } from "./trace-participants";
import type { TraceWaterfall } from "./trace-waterfall";
import type { Trace } from "../model/update";

afterEach(() => vi.useRealTimers());

const traceFixture: Trace = {
  traceId: "trace-123456789",
  startedAt: "2026-08-11T00:00:00Z",
  endedAt: "2026-08-11T00:00:04Z",
  status: "error",
  rootSpanCount: 1,
  missingParentCount: 1,
  conversations: [
    { sourceId: "claude", id: "conversation-a" },
    { sourceId: "codex", id: "conversation-b" },
  ],
  agents: [
    { sourceId: "claude", conversationId: "conversation-a", agentId: "main", agentType: "root", model: "model-a" },
    { sourceId: "codex", conversationId: "conversation-b", agentId: "reviewer", agentDefinition: "repository-review", agentType: "custom", model: "model-b" },
  ],
  activities: [
    {
      source: "claude", signal: "trace", traceId: "trace-123456789", spanId: "root", name: "root operation", kind: "reasoning",
      agentId: "main", runId: "conversation-a", model: "model-a", startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:00:04Z",
      observedAt: "2026-08-11T00:00:04Z", status: "Ok", contributesToTotal: true,
      tokens: { input: 100, output: 20, cacheRead: null, cacheWrite: null, reasoning: null, total: 120 },
    },
    {
      source: "codex", signal: "trace", traceId: "trace-123456789", spanId: "child", parentSpanId: "missing", name: "child operation", kind: "delegation",
      agentId: "reviewer", agentDefinition: "repository-review", targetAgentId: "main", targetAgentType: "root", runId: "conversation-b", model: "model-b", startedAt: "2026-08-11T00:00:01Z", endedAt: "2026-08-11T00:00:02Z",
      observedAt: "2026-08-11T00:00:02Z", status: "Error", contributesToTotal: true,
      tokens: { input: 50, output: 10, cacheRead: 30, cacheWrite: null, reasoning: 5, total: 60 },
    },
    {
      source: "codex", signal: "log", traceId: "trace-123456789", spanId: "child", name: "tool message", kind: "message", content: "Correlated log",
      agentId: "reviewer", runId: "conversation-b", model: "model-b", startedAt: "2026-08-11T00:00:01.500Z", endedAt: "2026-08-11T00:00:01.500Z",
      observedAt: "2026-08-11T00:00:01.500Z", contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    },
  ],
  activityOffset: 0,
  activityCount: 3,
  hasMore: false,
};

describe("dashboard components", () => {
  it("renders canonical operation language instead of a source event name", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = [{
      source: "example",
      signal: "log",
      name: "vendor.response.completed",
      kind: "response",
      agentId: "main",
      runId: "run-1",
      model: "example-model",
      observedAt: "2026-08-11T00:00:00Z",
      contributesToTotal: true,
      tokens: { input: 1, output: 1, cacheRead: 0, cacheWrite: 0, reasoning: 0, total: 2 },
    }];
    document.body.append(table);

    await table.updateComplete;

    expect(table.shadowRoot?.textContent).toContain("Model call usage");
    expect(table.shadowRoot?.textContent).not.toContain("vendor.response.completed");
    expect(table.shadowRoot?.textContent).toContain("Agent");
    expect(table.shadowRoot?.textContent).toContain("N/A");
    expect(table.shadowRoot?.textContent).toContain("Runtime ID: main");
    expect(table.shadowRoot?.querySelector("a.trace")).toBeNull();
  });

  it("emits an activity-page intent when scrolling near the end", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.hasMore = true;
    const listener = vi.fn();
    table.addEventListener("activities-needed", listener);
    document.body.append(table);
    await table.updateComplete;
    Object.defineProperties(table, {
      scrollHeight: { configurable: true, value: 1_000 },
      clientHeight: { configurable: true, value: 500 },
      scrollTop: { configurable: true, value: 200 },
    });

    table.dispatchEvent(new Event("scroll"));
    table.dispatchEvent(new Event("scroll"));

    expect(listener).toHaveBeenCalledOnce();
    expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({ direction: "older" });

    Object.defineProperty(table, "scrollTop", { configurable: true, value: 0 });
    table.dispatchEvent(new Event("scroll"));
    Object.defineProperty(table, "scrollTop", { configurable: true, value: 200 });
    table.dispatchEvent(new Event("scroll"));
    expect(listener).toHaveBeenCalledTimes(2);
  });

  it("resets the scroll-boundary latch when the conversation changes", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.hasMore = true;
    table.pagingContext = "example:conversation-a";
    const listener = vi.fn();
    table.addEventListener("activities-needed", listener);
    document.body.append(table);
    await table.updateComplete;
    Object.defineProperties(table, {
      scrollHeight: { configurable: true, value: 1_000 },
      clientHeight: { configurable: true, value: 500 },
      scrollTop: { configurable: true, value: 200 },
    });

    table.dispatchEvent(new Event("scroll"));
    table.pagingContext = "example:conversation-b";
    await table.updateComplete;
    table.dispatchEvent(new Event("scroll"));

    expect(listener).toHaveBeenCalledTimes(2);
  });

  it("does not request the opposite direction while a page is loading", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.hasEarlier = true;
    table.hasMore = true;
    table.loading = true;
    table.pageDirection = "older";
    const listener = vi.fn();
    table.addEventListener("activities-needed", listener);
    document.body.append(table);
    await table.updateComplete;

    const newer = table.shadowRoot?.querySelector("[data-paging='newer']");

    expect(newer?.querySelector("button")).toBeNull();
    expect(listener).not.toHaveBeenCalled();
  });

  it("emits a newer-page intent when scrolling near the beginning of a target window", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.hasEarlier = true;
    const listener = vi.fn();
    table.addEventListener("activities-needed", listener);
    document.body.append(table);
    await table.updateComplete;
    Object.defineProperties(table, {
      scrollHeight: { configurable: true, value: 2_000 },
      clientHeight: { configurable: true, value: 500 },
      scrollTop: { configurable: true, value: 100 },
    });

    table.dispatchEvent(new Event("scroll"));

    expect(listener).toHaveBeenCalledOnce();
    expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({ direction: "newer" });
  });

  it("offers a keyboard-accessible fallback for loading another page", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.hasMore = true;
    table.loadError = "Temporary failure";
    table.pageDirection = "older";
    const listener = vi.fn();
    table.addEventListener("activities-needed", listener);
    document.body.append(table);
    await table.updateComplete;

    const retry = table.shadowRoot?.querySelector<HTMLButtonElement>("button[data-direction='older']");
    retry?.click();

    expect(retry?.textContent).toContain("Retry");
    expect(table.shadowRoot?.querySelector("[role='alert']")?.textContent).toContain("Temporary failure");
    expect(listener).toHaveBeenCalledOnce();
  });

  it("links a reported trace identity to its reload-safe resource", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = [{
      source: "example",
      signal: "trace",
      traceId: "trace-123456789",
      spanId: "span-1",
      name: "operation",
      kind: "tool",
      agentId: "main",
      runId: "conversation-1",
      model: "",
      startedAt: "2026-08-11T00:00:00Z",
      endedAt: "2026-08-11T00:00:01Z",
      observedAt: "2026-08-11T00:00:01Z",
      contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    }];
    document.body.append(table);
    await table.updateComplete;

    const traceLink = table.shadowRoot?.querySelector<HTMLAnchorElement>("a.trace");

    expect(traceLink?.getAttribute("aria-label")).toContain("trace-123456789");
    expect(traceLink?.getAttribute("href")).toBe("/traces/trace-123456789");
  });

  it("shows prompt and usage correlation next to a linked trace", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = [{
      source: "claude", signal: "log", name: "gen_ai.model.request", kind: "response", agentId: "main", runId: "run-1",
      model: "claude-example", observedAt: "2026-08-11T00:00:00Z", promptId: "prompt-1", usageId: "request-1",
      relatedTraceId: "trace-123456789", relatedSpanId: "span-1", contributesToTotal: true,
      tokens: { input: 100, output: 20, cacheRead: 70, cacheWrite: 4, reasoning: null, total: 120 },
    }];
    document.body.append(table);
    await table.updateComplete;

    expect(table.shadowRoot?.textContent).toContain("Prompt prompt-1");
    expect(table.shadowRoot?.textContent).toContain("Usage request-1");
    expect(table.shadowRoot?.querySelector<HTMLAnchorElement>('a[href="/traces/trace-123456789"]')).not.toBeNull();
  });

  it("reveals a highlighted target only once while adjacent pages are appended", async () => {
    const reveal = vi.fn();
    const original = HTMLElement.prototype.scrollIntoView;
    HTMLElement.prototype.scrollIntoView = reveal;
    try {
      const table = document.createElement("am-activity-table") as ActivityTable;
      table.highlightedTraceId = "trace-123";
      table.highlightedSpanId = "span-1";
      table.activities = [{
        source: "example", signal: "trace", traceId: "trace-123", spanId: "span-1",
        name: "operation", kind: "tool", agentId: "main", runId: "run-1", model: "",
        observedAt: "2026-08-11T00:00:00Z", contributesToTotal: false,
        tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
      }];
      document.body.append(table);
      await table.updateComplete;

      table.activities = [...table.activities, { ...table.activities[0], spanId: "span-2" }];
      await table.updateComplete;

      expect(reveal).toHaveBeenCalledOnce();
    } finally {
      HTMLElement.prototype.scrollIntoView = original;
    }
  });

  it("renders a KPI supplied entirely through properties", async () => {
    const card = document.createElement("am-kpi-card") as KpiCard;
    card.label = "Input tokens";
    card.value = "1,024";
    document.body.append(card);

    await card.updateComplete;

    expect(card.shadowRoot?.textContent).toContain("Input tokens");
    expect(card.shadowRoot?.textContent).toContain("1,024");
  });

  it("emits a typed range intent without fetching data", async () => {
    const filter = document.createElement("am-time-range-filter") as TimeRangeFilter;
    const listener = vi.fn();
    filter.addEventListener("range-selected", listener);
    document.body.append(filter);
    await filter.updateComplete;

    const button = filter.shadowRoot?.querySelector<HTMLButtonElement>("button[data-range='1h']");
    button?.click();

    expect(listener).toHaveBeenCalledOnce();
    expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({ range: "1h" });
  });

  it("keeps authoritative plan limits separate from observed tokens", async () => {
    const limits = document.createElement("am-plan-usage") as PlanUsage;
    limits.snapshots = [{
      source: "example",
      windowId: "window",
      windowDurationMinutes: 300,
      usedPercent: 25,
      capturedAt: "2026-08-11T00:00:00Z",
      authority: "account_api",
    }];
    document.body.append(limits);

    await limits.updateComplete;

    expect(limits.shadowRoot?.textContent).toContain("25% used");
    expect(limits.shadowRoot?.textContent).toContain("75% remaining");
    expect(limits.shadowRoot?.textContent).not.toContain("tokens");
  });

  it("groups token totals and modifiers instead of concatenating them", async () => {
    const chart = document.createElement("am-token-chart");
    (chart as { usage: object }).usage = { input: 100, output: 20, cacheRead: 60, cacheWrite: 4, reasoning: 8, total: 120 };
    document.body.append(chart);
    await (chart as { updateComplete: Promise<unknown> }).updateComplete;

    const content = chart.shadowRoot?.textContent ?? "";
    expect(content).toContain("Observed total");
    expect(content).toContain("Primary usage");
    expect(content).toContain("Modifiers and additional evidence");
    expect(content).toContain("Cache read");
  });

  it("renders agent relationships as a root-first tree", async () => {
    const tree = document.createElement("am-agent-tree") as AgentTree;
    const missing = { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null };
    tree.agents = [
      { agentId: "child", agentDefinition: "repository-review", parentAgentId: "main", agentType: "custom", model: "example-large", activityCount: 1, tokens: { input: 100, output: 20, cacheRead: 60, cacheWrite: 4, reasoning: 8, total: 120 } },
      { agentId: "main", agentType: "root", activityCount: 2, tokens: missing },
    ];
    document.body.append(tree);

    await tree.updateComplete;

    const root = tree.shadowRoot?.querySelector<HTMLElement>("[data-agent-id='main']");
    const child = tree.shadowRoot?.querySelector<HTMLElement>("[data-agent-id='child']");
    expect(root?.textContent).toContain("main");
    expect(child?.textContent).toContain("child");
    expect(child?.textContent).toContain("repository-review");
    expect(child?.textContent).toContain("example-large");
    expect(child?.textContent).toContain("Runtime ID:");
    const childTokens = child?.querySelector("am-token-breakdown");
    await (childTokens as { updateComplete?: Promise<unknown> } | null)?.updateComplete;
    expect(childTokens?.shadowRoot?.textContent).toContain("120");
    expect(childTokens?.shadowRoot?.textContent).toContain("Input");
    expect(childTokens?.shadowRoot?.textContent).toContain("Cache read");
    expect(childTokens?.shadowRoot?.textContent).toContain("Reasoning");
    expect(childTokens?.shadowRoot?.querySelector("details")?.open).toBe(false);
    expect(root?.textContent).toContain("Main agent");
    expect(Number.parseFloat(root?.style.top ?? "NaN")).toBeLessThan(Number.parseFloat(child?.style.top ?? "NaN"));
    expect(Number.parseFloat(root?.style.left ?? "NaN")).toBe(Number.parseFloat(child?.style.left ?? "NaN"));
    expect(tree.shadowRoot?.querySelectorAll(".connector")).toHaveLength(3);
  });

  it("keeps vertical parent-child relationships in separate root lanes", () => {
    const missing = { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null };
    const agents = [
      { agentId: "root-a", activityCount: 1, tokens: missing },
      { agentId: "child-a1", parentAgentId: "root-a", activityCount: 1, tokens: missing },
      { agentId: "child-a2", parentAgentId: "root-a", activityCount: 1, tokens: missing },
      { agentId: "root-b", activityCount: 1, tokens: missing },
      { agentId: "child-b1", parentAgentId: "root-b", activityCount: 1, tokens: missing },
    ];
    const layout = layoutAgentTree(buildAgentTree(agents));
    const byID = new Map(layout.nodes.map((entry) => [entry.node.agent.agentId, entry]));
    const rootA = byID.get("root-a")!;
    const rootB = byID.get("root-b")!;
    const rootANodes = ["root-a", "child-a1", "child-a2"].map((id) => byID.get(id)!);
    const rootBNodes = ["root-b", "child-b1"].map((id) => byID.get(id)!);

    expect(rootA.centerY).toBeLessThan(byID.get("child-a1")!.centerY);
    expect(rootA.centerY).toBeLessThan(byID.get("child-a2")!.centerY);
    expect(rootB.centerY).toBeLessThan(byID.get("child-b1")!.centerY);
    expect(Math.max(...rootANodes.map((entry) => entry.centerX))).toBeLessThan(Math.min(...rootBNodes.map((entry) => entry.centerX)));

    for (let left = 0; left < layout.nodes.length; left += 1) {
      for (let right = left + 1; right < layout.nodes.length; right += 1) {
        const first = layout.nodes[left];
        const second = layout.nodes[right];
        const separated = Math.abs(first.centerX - second.centerX) >= 190 || Math.abs(first.centerY - second.centerY) >= 96;
        expect(separated, `${first.node.agent.agentId} overlaps ${second.node.agent.agentId}`).toBe(true);
      }
    }
  });

  it("lays out a large child set across multiple columns without overlap", () => {
    const missing = { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null };
    const agents = [
      { agentId: "main", activityCount: 1, tokens: missing },
      ...Array.from({ length: 49 }, (_, index) => ({ agentId: `child-${index}`, parentAgentId: "main", activityCount: 1, tokens: missing })),
    ];
    const layout = layoutAgentTree(buildAgentTree(agents));
    const children = layout.nodes.filter(({ depth }) => depth === 1);
    const firstRow = new Set(children.filter(({ centerY }) => centerY === children[0].centerY).map(({ centerX }) => centerX));

    expect(firstRow.size).toBe(4);
    expect(layout.width).toBeGreaterThan(800);
    for (let left = 0; left < layout.nodes.length; left += 1) {
      for (let right = left + 1; right < layout.nodes.length; right += 1) {
        const first = layout.nodes[left];
        const second = layout.nodes[right];
        const separated = Math.abs(first.centerX - second.centerX) >= 190 || Math.abs(first.centerY - second.centerY) >= 96;
        expect(separated, `${first.node.agent.agentId} overlaps ${second.node.agent.agentId}`).toBe(true);
      }
    }
  });

  it("emits and toggles graph-node selection", async () => {
    const tree = document.createElement("am-agent-tree") as AgentTree;
    tree.agents = [{ agentId: "main", agentDefinition: "orchestrator", activityCount: 1, tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null } }];
    const listener = vi.fn();
    tree.addEventListener("agent-selected", listener);
    document.body.append(tree);
    await tree.updateComplete;

    tree.shadowRoot?.querySelector<HTMLElement>("[data-agent-id='main']")?.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    expect(listener.mock.calls[0][0].detail).toEqual({ agentId: "main" });
    tree.selectedAgentId = "main";
    await tree.updateComplete;
    tree.shadowRoot?.querySelector<HTMLElement>("[data-agent-id='main']")?.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    expect(listener.mock.calls[1][0].detail).toEqual({ agentId: "" });
  });

  it("emits source and submitted full-text filter intents", async () => {
    vi.useFakeTimers();
    const filter = document.createElement("am-session-filter") as SessionFilter;
    filter.sources = [{ id: "claude", label: "Claude Code" }, { id: "codex", label: "Codex" }];
    const sourceListener = vi.fn();
    const searchListener = vi.fn();
    filter.addEventListener("source-selected", sourceListener);
    filter.addEventListener("search-submitted", searchListener);
    document.body.append(filter);
    await filter.updateComplete;

    const select = filter.shadowRoot?.querySelector<HTMLSelectElement>("select");
    if (select) select.value = "claude";
    select?.dispatchEvent(new Event("change", { bubbles: true }));
    const input = filter.shadowRoot?.querySelector<HTMLInputElement>("input");
    if (input) input.value = "repository review";
    input?.dispatchEvent(new InputEvent("input", { bubbles: true }));
	vi.advanceTimersByTime(250);

    expect(filter.shadowRoot?.textContent).toContain("Claude Code");
    expect(filter.shadowRoot?.textContent).toContain("Codex");
    expect(sourceListener).toHaveBeenCalledOnce();
    expect(sourceListener.mock.calls[0][0].detail).toEqual({ sourceId: "claude" });
    expect(searchListener.mock.calls[0][0].detail).toEqual({ search: "repository review" });
	vi.useRealTimers();
  });

  it("labels each session with its observed telemetry source", async () => {
    const list = document.createElement("am-session-list") as SessionList;
    const tokens = { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null };
    list.sessions = [
      { id: "session-claude", sourceId: "claude", sources: [{ id: "claude", label: "Claude Code" }], traceIds: [], startedAt: "", endedAt: "", activityCount: 1, tokens, agents: [], activities: [] },
      { id: "session-codex", sourceId: "codex", sources: [{ id: "codex", label: "Codex" }], traceIds: [], startedAt: "", endedAt: "", activityCount: 1, tokens, agents: [], activities: [] },
    ];
    document.body.append(list);
    await list.updateComplete;

    expect(list.shadowRoot?.textContent).toContain("Claude Code");
    expect(list.shadowRoot?.textContent).toContain("Codex");
    expect(list.shadowRoot?.querySelector<HTMLAnchorElement>('a[href="/conversations/claude/session-claude"]')).not.toBeNull();
  });

  it("summarizes cross-conversation trace status and completeness", async () => {
    const summary = document.createElement("am-trace-summary") as TraceSummary;
    summary.trace = traceFixture;
    document.body.append(summary);
    await summary.updateComplete;

    const content = summary.shadowRoot?.textContent ?? "";
    expect(content).toContain("trace-123456789");
    expect(content).toContain("Error");
    expect(content).toContain("2 conversations");
    expect(content).toContain("1 missing parent");
  });

  it("lists trace conversation and agent participants without merging them", async () => {
    const participants = document.createElement("am-trace-participants") as TraceParticipants;
    participants.trace = traceFixture;
    document.body.append(participants);
    await participants.updateComplete;

    const content = participants.shadowRoot?.textContent ?? "";
    expect(content).toContain("claude");
    expect(content).toContain("conversation-a");
    expect(content).toContain("codex");
    expect(content).toContain("conversation-b");
    expect(content).toContain("repository-review");
    expect(content).toContain("model-b");
    const participantTokens = [...participants.shadowRoot?.querySelectorAll("am-token-breakdown") ?? []];
    await Promise.all(participantTokens.map((token) => (token as { updateComplete?: Promise<unknown> }).updateComplete));
    expect(participantTokens.map((token) => token.shadowRoot?.textContent).join(" ")).toContain("120");
    expect(participantTokens.map((token) => token.shadowRoot?.textContent).join(" ")).toContain("60");
  });

  it("renders spans and correlated logs in the trace waterfall", async () => {
    const waterfall = document.createElement("am-trace-waterfall") as TraceWaterfall;
    waterfall.trace = traceFixture;
    document.body.append(waterfall);
    await waterfall.updateComplete;

    expect(waterfall.shadowRoot?.querySelectorAll("[role='listitem']")).toHaveLength(3);
    expect(waterfall.shadowRoot?.textContent).toContain("root operation");
    expect(waterfall.shadowRoot?.textContent).toContain("child operation");
    expect(waterfall.shadowRoot?.textContent).toContain("Correlated log");
    expect(waterfall.shadowRoot?.textContent).toContain("Missing parent");
    expect(waterfall.shadowRoot?.querySelectorAll("details")).toHaveLength(3);
    expect(waterfall.shadowRoot?.querySelectorAll("details")[0]?.open).toBe(false);
    expect(waterfall.shadowRoot?.querySelectorAll("details")[1]?.open).toBe(true);
    const childSummary = waterfall.shadowRoot?.querySelectorAll("summary")[1]?.textContent ?? "";
    expect(childSummary).toContain("repository-review → main");
    expect(childSummary).toContain("60 tokens");
    expect(childSummary).toContain("Error");
    const childTokenBreakdown = waterfall.shadowRoot?.querySelectorAll("am-token-breakdown")[1];
    await (childTokenBreakdown as { updateComplete?: Promise<unknown> } | undefined)?.updateComplete;
    expect(childTokenBreakdown?.shadowRoot?.textContent).toContain("Input");
    expect(childTokenBreakdown?.shadowRoot?.textContent).toContain("50");
    expect(childTokenBreakdown?.shadowRoot?.textContent).toContain("Reasoning");
    expect(waterfall.shadowRoot?.textContent).toContain("Runtime agent IDreviewer");
    expect(waterfall.shadowRoot?.textContent).toContain("Agentrepository-review");
    expect(waterfall.shadowRoot?.textContent).toContain("Agent typecustom");
    expect(waterfall.shadowRoot?.textContent).toContain("StatusError");
    expect(waterfall.shadowRoot?.textContent).toContain("Started at2026-08-11T00:00:01Z");
    expect(waterfall.shadowRoot?.textContent).toContain("Ended at2026-08-11T00:00:02Z");
    const logEvidence = waterfall.shadowRoot?.querySelectorAll("details")[2]?.textContent ?? "";
    const logSummary = waterfall.shadowRoot?.querySelectorAll("summary")[2]?.textContent ?? "";
    expect(logSummary).toContain("N/A");
    expect(logSummary).not.toContain("Tokens not reported");
    expect(logEvidence).toContain("RollupN/A");
    expect(logEvidence).not.toContain("corroborating");
    expect(waterfall.shadowRoot?.querySelector<HTMLAnchorElement>('a[href="/conversations/codex/conversation-b?traceId=trace-123456789&spanId=child"]')).not.toBeNull();
  });

  it("shows partial token evidence without inventing a total or topology edge", async () => {
    const waterfall = document.createElement("am-trace-waterfall") as TraceWaterfall;
    waterfall.trace = {
      ...traceFixture,
      agents: [],
      activities: [{
        ...traceFixture.activities[1],
        name: "gen_ai.tool.call",
        kind: "tool",
        toolName: "read_file",
        targetAgentId: undefined,
        targetAgentType: "planner",
        parentAgentId: "main",
        tokens: { input: 50, output: null, cacheRead: 30, cacheWrite: null, reasoning: 5, total: null },
      }],
    };
    document.body.append(waterfall);
    await waterfall.updateComplete;

    const summary = waterfall.shadowRoot?.querySelector("summary")?.textContent ?? "";
    expect(summary).toContain("repository-review → planner");
    expect(summary).toContain("read_file");
    expect(summary).toContain("Partial · input 50 · cache read 30 · reasoning 5");
    expect(summary).not.toContain("main → repository-review");
    expect(waterfall.shadowRoot?.textContent).toContain("Tool nameread_file");
  });

  it("labels duplicate usage evidence as corroborating without adding it to rollups", async () => {
    const waterfall = document.createElement("am-trace-waterfall") as TraceWaterfall;
    waterfall.trace = { ...traceFixture, activities: [{ ...traceFixture.activities[1], contributesToTotal: false }] };
    document.body.append(waterfall);
    await waterfall.updateComplete;

    expect(waterfall.shadowRoot?.querySelector("summary")?.textContent).toContain("Corroborating · 60 tokens");
    expect(waterfall.shadowRoot?.textContent).toContain("Excluded from rollup as corroborating usage evidence");
  });

  it("highlights a span targeted by conversation navigation", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = traceFixture.activities;
    table.highlightedTraceId = "trace-123456789";
    table.highlightedSpanId = "child";
    document.body.append(table);
    await table.updateComplete;

    expect(table.shadowRoot?.querySelector('tr[data-highlighted="true"]')?.textContent).toContain("Agent delegation");
    expect(table.shadowRoot?.querySelectorAll('tr[data-highlighted="true"]')).toHaveLength(1);
    expect(table.shadowRoot?.querySelector('tr[aria-current="location"]')?.textContent).toContain("Agent delegation");
  });

});
