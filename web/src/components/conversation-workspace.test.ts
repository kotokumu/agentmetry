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
  it("keeps source-qualified session switching and partial list paging in the detail header", async () => {
    const workspace = await mount();
    const selected = vi.fn();
    workspace.addEventListener("session-selected", selected);

    const switcher = workspace.shadowRoot!.querySelector<HTMLSelectElement>("select[data-session-switcher]");
    expect(switcher).toBeTruthy();
    switcher!.value = "claude:two";
    switcher!.dispatchEvent(new Event("change", { bubbles: true }));
    expect((selected.mock.calls[0]![0] as CustomEvent).detail).toEqual({ sourceId: "claude", sessionId: "two" });
    expect(workspace.shadowRoot!.textContent).toContain("More sessions available");
    expect(workspace.shadowRoot!.querySelector("button[data-session-more]")).toBeTruthy();
  });

  it("keeps the native dropdown aligned after direct, reload, and source switch renders", async () => {
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
    const switcher = () => workspace.shadowRoot!.querySelector<HTMLSelectElement>("select[data-session-switcher]")!;
    expect(switcher().value).toBe("codex:one");

    workspace.requestedConversation = { sourceId: "claude", conversationId: "two" };
    await workspace.updateComplete;
    await Promise.resolve();
    await workspace.updateComplete;
    expect(switcher().value).toBe("claude:two");
    expect(switcher().selectedOptions[0]?.value).toBe("claude:two");

    workspace.requestedConversation = { sourceId: "codex", conversationId: "one" };
    await workspace.updateComplete;
    await Promise.resolve();
    await workspace.updateComplete;
    expect(switcher().value).toBe("codex:one");
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

  it("keeps compact primary metrics visible and links exact related traces", async () => {
    const workspace = await mount();
    const header = workspace.shadowRoot!.querySelector(".session-head-panel")!;
    const primaryCards = Array.from(header.querySelectorAll(".session-metrics")[0]!.querySelectorAll<HTMLElement>("am-kpi-card"));
    await Promise.all(primaryCards.map((card) => (card as unknown as { updateComplete: Promise<unknown> }).updateComplete));
    expect(primaryCards).toHaveLength(4);
    expect(primaryCards.every((card) => card.hasAttribute("compact"))).toBe(true);
    expect(primaryCards.map((card) => card.shadowRoot?.textContent).join(" ")).toContain("1 min 30 s");
    expect(header.textContent).toContain("trace/exact");
    expect(header.querySelector<HTMLAnchorElement>('a[href="/traces/trace%2Fexact"]')).toBeTruthy();
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
