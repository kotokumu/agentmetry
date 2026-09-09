import { afterEach, describe, expect, it, vi } from "vitest";
import "./conversation-workspace";
import { agentmetryClient } from "../api/agentmetry-client";
import type { Session } from "../model/telemetry";

const tokens = { input: 10, output: 5, cacheRead: null, cacheWrite: null, reasoning: null, total: 15 };
const session = (id: string, sourceId = "codex", title?: string): Session & { catalog?: { role: "root"; rootSessionId: string; parentSessionId: string; name?: { text: string; origin: "claude_code.generate_session_title" } } } => ({
  id, sourceId, sources: [{ id: sourceId, label: sourceId }], traceIds: ["trace/exact"],
  startedAt: "2026-09-08T00:00:00Z", endedAt: "2026-09-08T00:01:30Z", activityCount: 3, agentCount: 2,
  tokens, costUsd: 0.12, agents: [
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
});

describe("conversation workspace completion", () => {
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
    expect(primaryCards).toHaveLength(4);
    expect(primaryCards.every((card) => card.hasAttribute("compact"))).toBe(true);
    expect(primaryCards.map((card) => card.shadowRoot?.textContent).join(" ")).toContain("1 min 30 s");
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
