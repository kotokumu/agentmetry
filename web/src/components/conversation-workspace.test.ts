import { afterEach, describe, expect, it, vi } from "vitest";
import "./conversation-workspace";
import { agentmetryClient } from "../api/agentmetry-client";
import { ProjectionTargetKind } from "../gen/agentmetry/v1/agentmetry_pb";
import type { Session } from "../model/telemetry";
import { LIVE_UPDATE_EVENT } from "../controllers/live-update-controller";

const tokens = { input: 10, output: 5, cacheRead: null, cacheWrite: null, reasoning: null, total: 15 };
const session = (id: string, sourceId = "codex", title?: string): Session & { catalog?: { role: "root"; rootSessionId: string; parentSessionId: string; name?: { text: string; origin: "claude_code.generate_session_title" } } } => ({
  id, sourceId, sources: [{ id: sourceId, label: sourceId }], traceIds: ["trace/exact"],
  startedAt: "2026-09-08T00:00:00Z", endedAt: "2026-09-08T00:01:30Z", activityCount: 3, agentCount: 2,
  tokens, costUsd: 0.12, costSummary: { amountMicroUsd: 120_000n, basis: "rate_card_estimate", coverage: "complete", eligibleCalls: 1n, pricedCalls: 1n, unpricedReasons: [] }, agents: [
    { agentId: "agent-1", activityCount: 2, tokens }, { agentId: "agent-2", activityCount: 1, tokens },
  ], activities: [], hasMore: false,
  ...(title ? { catalog: { role: "root", rootSessionId: id, parentSessionId: "", name: { text: title, origin: "claude_code.generate_session_title" } } } : {}),
});

const mount = async (selected = session("one", "codex", "Reported title")) => {
  vi.spyOn(agentmetryClient, "listSessionsPage").mockResolvedValue({ sessions: [selected, session("two", "claude")], nextPageToken: "next" });
  vi.spyOn(agentmetryClient, "getSession").mockResolvedValue(selected);
  vi.spyOn(agentmetryClient, "getSessionRework").mockRejectedValue(new Error("unused"));
  const workspace = document.createElement("am-conversation-workspace") as import("./conversation-workspace").ConversationWorkspace;
  workspace.requestedConversation = { sourceId: selected.sourceId, conversationId: selected.id };
  document.body.append(workspace);
  await workspace.updateComplete;
  await Promise.resolve();
  await workspace.updateComplete;
  return workspace;
};

afterEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("conversation workspace completion", () => {
  it("keeps the session sidebar inside the visible viewport", async () => {
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal("innerHeight", 900);
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      frames.push(callback);
      return frames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", vi.fn());
    vi.spyOn(agentmetryClient, "listSessionsPage").mockResolvedValue({ sessions: [session("one")], nextPageToken: "" });
    const workspace = document.createElement("am-conversation-workspace");
    document.body.append(workspace);
    await workspace.updateComplete;
    const panel = workspace.shadowRoot!.querySelector<HTMLElement>(".list-surface")!;
    vi.spyOn(panel, "getBoundingClientRect").mockReturnValue({ top: 180 } as DOMRect);

    window.dispatchEvent(new Event("resize"));
    while (frames.length) frames.shift()!(0);

    expect(panel.style.maxHeight).toBe("708px");
  });

  it("starts with a compact session sidebar and an unselected detail pane", async () => {
    const listSessionsPage = vi.spyOn(agentmetryClient, "listSessionsPage").mockResolvedValue({ sessions: [session("one")], nextPageToken: "" });
    const workspace = document.createElement("am-conversation-workspace");
    document.body.append(workspace);
    await workspace.updateComplete;
    await vi.waitFor(() => expect(listSessionsPage).toHaveBeenCalled());
    await workspace.updateComplete;

    expect(workspace.shadowRoot!.querySelector(".workspace")!.classList.contains("list-only")).toBe(false);
    expect(workspace.shadowRoot!.querySelector("am-session-list")!.hasAttribute("compact")).toBe(true);
    expect(workspace.shadowRoot!.querySelector<HTMLElement>(".detail")!.hidden).toBe(false);
    expect(workspace.shadowRoot!.querySelector(".detail")!.textContent).toContain("Select a session");
    expect(listSessionsPage.mock.calls[0]![0].pageSize).toBe(20);
  });

  it("keeps the two-pane layout and a list return while detail is loading or unavailable", async () => {
    vi.spyOn(agentmetryClient, "listSessionsPage").mockResolvedValue({ sessions: [session("one")], nextPageToken: "" });
    vi.spyOn(agentmetryClient, "getSessionRework").mockRejectedValue(new Error("unused"));
    let rejectDetail!: (error: Error) => void;
    vi.spyOn(agentmetryClient, "getSession").mockReturnValue(new Promise((_, reject) => { rejectDetail = reject; }));
    const workspace = document.createElement("am-conversation-workspace");
    workspace.requestedConversation = { sourceId: "codex", conversationId: "one" };
    document.body.append(workspace);
    await workspace.updateComplete;
    expect(workspace.shadowRoot!.querySelector(".workspace")!.classList.contains("list-only")).toBe(false);
    expect(workspace.shadowRoot!.querySelector("am-session-list")!.hasAttribute("compact")).toBe(true);
    expect(workspace.shadowRoot!.querySelector(".list-return")).toBeTruthy();
    rejectDetail(new Error("unavailable"));
    await Promise.resolve();
    await workspace.updateComplete;
    expect(workspace.shadowRoot!.querySelector(".list-return")).toBeTruthy();
  });

  it("keeps the session list beside details and switches using source-qualified links", async () => {
    const workspace = await mount();
    const selected = vi.fn();
    workspace.addEventListener("session-selected", selected);

    const list = workspace.shadowRoot!.querySelector("am-session-list")!;
    expect(list).toBeTruthy();
    await list.updateComplete;
    list.shadowRoot!.querySelector<HTMLAnchorElement>('a[href="/conversations/claude/two"]')!.click();
    expect((selected.mock.calls[0]![0] as CustomEvent).detail).toEqual({ sourceId: "claude", sessionId: "two" });
    expect(workspace.shadowRoot!.querySelector("select[data-session-switcher]")).toBeNull();
    expect(workspace.shadowRoot!.querySelector("button[data-session-more]")).toBeNull();
  });

  it("shows agent structure before operations and keeps token details collapsed", async () => {
    const workspace = await mount();
    const root = workspace.shadowRoot!;
    const topology = root.querySelector<HTMLElement>(".topology-panel")!;
    const operations = root.querySelector<HTMLElement>(".operations-panel")!;
    const tokenDetails = root.querySelector<HTMLDetailsElement>(".token-details")!;
    const tokenChart = root.querySelector("am-token-chart")!;

    expect(topology).toBeTruthy();
    expect(topology.hidden).toBe(false);
    expect(topology.closest("details")).toBeNull();
    expect(topology.compareDocumentPosition(operations) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(tokenDetails).toBeTruthy();
    expect(tokenDetails.open).toBe(false);
    expect(tokenDetails.contains(tokenChart)).toBe(true);
  });

  it("defers rework analysis until the rework view is opened", async () => {
    const selected = session("one");
    vi.spyOn(agentmetryClient, "listSessionsPage").mockResolvedValue({ sessions: [selected], nextPageToken: "" });
    vi.spyOn(agentmetryClient, "getSession").mockResolvedValue(selected);
    const getSessionRework = vi.spyOn(agentmetryClient, "getSessionRework").mockResolvedValue(undefined as never);
    const workspace = document.createElement("am-conversation-workspace") as import("./conversation-workspace").ConversationWorkspace;
    workspace.requestedConversation = { sourceId: selected.sourceId, conversationId: selected.id };
    document.body.append(workspace);
    await vi.waitFor(() => expect(workspace.shadowRoot!.querySelector(".session-head-panel")).toBeTruthy());
    expect(getSessionRework).not.toHaveBeenCalled();

    workspace.purpose = "rework";
    await workspace.updateComplete;
    await vi.waitFor(() => expect(getSessionRework).toHaveBeenCalledTimes(1));

    workspace.purpose = "execution";
    await workspace.updateComplete;
    workspace.purpose = "rework";
    await workspace.updateComplete;
    await Promise.resolve();
    expect(getSessionRework).toHaveBeenCalledTimes(1);

    workspace.purpose = "execution";
    await workspace.updateComplete;
    let applied = Promise.resolve();
    window.dispatchEvent(new CustomEvent(LIVE_UPDATE_EVENT, { detail: {
      targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: selected.sourceId, sessionId: selected.id, traceId: "" }],
      resyncRequired: false,
      throughCursor: "updated",
      waitUntil: (promise: Promise<unknown>) => { applied = promise.then(() => undefined); },
    } }));
    await applied;
    expect(getSessionRework).toHaveBeenCalledTimes(1);
    workspace.purpose = "rework";
    await workspace.updateComplete;
    await vi.waitFor(() => expect(getSessionRework).toHaveBeenCalledTimes(2));
  });

  it("defers comparison work until the comparison panel is opened", async () => {
    const selected = session("one");
    vi.spyOn(agentmetryClient, "listSessionsPage").mockResolvedValue({ sessions: [selected, { ...session("older"), startedAt: "2026-09-07T00:00:00Z", endedAt: "2026-09-07T00:01:30Z" }], nextPageToken: "" });
    vi.spyOn(agentmetryClient, "getSession").mockResolvedValue(selected);
    vi.spyOn(agentmetryClient, "getSessionRework").mockResolvedValue(undefined as never);
    const compareRework = vi.spyOn(agentmetryClient, "compareRework").mockReturnValue(new Promise(() => undefined));
    const workspace = document.createElement("am-conversation-workspace") as import("./conversation-workspace").ConversationWorkspace;
    workspace.requestedConversation = { sourceId: selected.sourceId, conversationId: selected.id };
    workspace.purpose = "rework";
    document.body.append(workspace);
    await vi.waitFor(() => expect(workspace.shadowRoot!.querySelector("am-rework-summary")).toBeTruthy());
    expect(compareRework).not.toHaveBeenCalled();

    workspace.shadowRoot!.querySelector("am-rework-summary")!.dispatchEvent(new CustomEvent("comparison-requested", { bubbles: true, composed: true }));
    await workspace.updateComplete;
    await vi.waitFor(() => expect(compareRework).toHaveBeenCalledTimes(1));
  });

  it("defers the root comparison list in the all-sessions view", async () => {
    const selected = session("child");
    const older = { ...session("older"), startedAt: "2026-09-07T00:00:00Z", endedAt: "2026-09-07T00:01:30Z" };
    const listSessionsPage = vi.spyOn(agentmetryClient, "listSessionsPage").mockImplementation(async (query) => ({
      sessions: query.view === "all" ? [selected] : [selected, older], nextPageToken: "",
    }));
    vi.spyOn(agentmetryClient, "getSession").mockResolvedValue(selected);
    vi.spyOn(agentmetryClient, "getSessionRework").mockResolvedValue(undefined as never);
    vi.spyOn(agentmetryClient, "compareRework").mockReturnValue(new Promise(() => undefined));
    const workspace = document.createElement("am-conversation-workspace") as import("./conversation-workspace").ConversationWorkspace;
    workspace.sessionView = "all";
    workspace.purpose = "rework";
    workspace.requestedConversation = { sourceId: selected.sourceId, conversationId: selected.id };
    document.body.append(workspace);
    await vi.waitFor(() => expect(listSessionsPage.mock.calls.some(([query]) => query.view === "all")).toBe(true));
    expect(listSessionsPage.mock.calls.some(([query]) => query.view === "roots")).toBe(false);

    const summary = await vi.waitFor(() => {
      const value = workspace.shadowRoot!.querySelector("am-rework-summary");
      expect(value).toBeTruthy();
      return value!;
    });
    summary.dispatchEvent(new CustomEvent("comparison-requested", { bubbles: true, composed: true }));
    await vi.waitFor(() => expect(listSessionsPage.mock.calls.some(([query]) => query.view === "roots")).toBe(true));
  });

  it("preserves the list element and its scroll position across session navigation", async () => {
    const codex = session("one", "codex");
    const claude = session("two", "claude", "Claude report");
    vi.spyOn(agentmetryClient, "listSessionsPage").mockResolvedValue({ sessions: [codex, claude], nextPageToken: "" });
    vi.spyOn(agentmetryClient, "getSession").mockImplementation(async (sourceId) => sourceId === "claude" ? claude : codex);
    vi.spyOn(agentmetryClient, "getSessionRework").mockRejectedValue(new Error("unused"));
    const workspace = document.createElement("am-conversation-workspace") as import("./conversation-workspace").ConversationWorkspace;
    workspace.requestedConversation = { sourceId: "codex", conversationId: "one" };
    document.body.append(workspace);
    await workspace.updateComplete;
    await Promise.resolve();
    await workspace.updateComplete;
    const list = workspace.shadowRoot!.querySelector("am-session-list")!;
    expect(list).toBeTruthy();
    list.scrollTop = 120;
    expect(list.selected).toBe("one");

    workspace.requestedConversation = { sourceId: "claude", conversationId: "two" };
    await workspace.updateComplete;
    await Promise.resolve();
    await workspace.updateComplete;
    expect(workspace.shadowRoot!.querySelector("am-session-list")).toBe(list);
    expect(list.selected).toBe("two");
    expect(list.selectedSource).toBe("claude");
    expect(list.scrollTop).toBe(120);

    workspace.requestedConversation = { sourceId: "codex", conversationId: "one" };
    await workspace.updateComplete;
    await Promise.resolve();
    await workspace.updateComplete;
    expect(list.selected).toBe("one");
  });

  it("collapses and restores the list without losing the detail", async () => {
    const workspace = await mount();
    const list = workspace.shadowRoot!.querySelector("am-session-list");
    const collapse = workspace.shadowRoot!.querySelector<HTMLButtonElement>("[data-collapse-list]")!;
    expect(collapse.textContent?.trim()).toBe("");
    expect(collapse.getAttribute("aria-label")).toBe("Collapse session list");
    expect(collapse.querySelector("svg")).toBeTruthy();
    collapse.click();
    await workspace.updateComplete;
    const restore = workspace.shadowRoot!.querySelector<HTMLButtonElement>("[data-show-list]")!;
    expect(restore).toBeTruthy();
    expect(restore.textContent?.trim()).toBe("");
    expect(restore.getAttribute("aria-label")).toBe("Expand session list");
    expect(workspace.shadowRoot!.querySelector(".workspace")?.classList.contains("list-collapsed")).toBe(true);
    expect(workspace.shadowRoot!.textContent).toContain("codex:one");
    restore.click();
    await workspace.updateComplete;
    expect(workspace.shadowRoot!.querySelector(".workspace")?.classList.contains("list-collapsed")).toBe(false);
    expect(workspace.shadowRoot!.querySelector("am-session-list")).toBe(list);

    collapse.click();
    await workspace.updateComplete;
    workspace.requestedConversation = undefined;
    await workspace.updateComplete;
    expect(workspace.shadowRoot!.querySelector(".workspace")?.classList.contains("list-collapsed")).toBe(false);
  });

  it("prioritizes a reported title, keeps the full id, and copies the exact id", async () => {
    const workspace = await mount();
    const text = workspace.shadowRoot!.textContent ?? "";
    expect(text).toContain("Reported title");
    expect(text).toContain("codex:one");
    expect(text).not.toContain("Generated from");
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    workspace.shadowRoot!.querySelector<HTMLButtonElement>("button[data-copy-session]")!.click();
    await Promise.resolve();
    expect(writeText).toHaveBeenCalledWith("codex:one");
  });

  it("keeps compact primary metrics visible without exposing a raw trace id list", async () => {
    const workspace = await mount();
    const header = workspace.shadowRoot!.querySelector(".session-head-panel")!;
    const primaryCards = Array.from(header.querySelectorAll(".session-metrics")[0]!.querySelectorAll<HTMLElement>("am-kpi-card"));
    await Promise.all(primaryCards.map((card) => (card as unknown as { updateComplete: Promise<unknown> }).updateComplete));
    expect(primaryCards).toHaveLength(5);
    expect(primaryCards.every((card) => card.hasAttribute("compact"))).toBe(true);
    const primaryText = primaryCards.map((card) => card.shadowRoot?.textContent).join(" ");
    expect(primaryText).toContain("1 min 30 s");
    expect(primaryText).toContain("Estimated cost");
    expect(primaryText).toContain("$0.12");
    expect(header.querySelector(".session-overview")!.textContent).not.toContain("Estimated cost");
    expect(header.textContent).not.toContain("trace/exact");
    expect(header.querySelector('a[href^="/traces/"]')).toBeNull();
    expect(header.textContent).not.toContain("participants");
  });

  it("offers filtered Connections settings from the initial empty state", async () => {
    vi.spyOn(agentmetryClient, "listSessionsPage").mockResolvedValue({ sessions: [], nextPageToken: "" });
    const workspace = document.createElement("am-conversation-workspace") as import("./conversation-workspace").ConversationWorkspace;
    workspace.sourceId = "codex";
    workspace.search = "missing";
    document.body.append(workspace);
    await workspace.updateComplete;
    await Promise.resolve();
    await workspace.updateComplete;
    const link = workspace.shadowRoot!.querySelector<HTMLAnchorElement>("a[data-connections-link]");
    expect(link).toBeTruthy();
    expect(link!.getAttribute("href")).toContain("section=connections");
    expect(workspace.shadowRoot!.textContent).toContain("No matching sessions");
  });
});
