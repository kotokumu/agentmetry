import { afterEach, describe, expect, it, vi } from "vitest";
import "./session-list";
import type { SessionList } from "./session-list";
import type { SessionListEntry } from "../model/session-catalog";

afterEach(() => document.body.replaceChildren());

const tokens = (total: number | null = null) => ({ input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total });
const session = (id: string, sourceId = "codex"): SessionListEntry => ({
  id, sourceId, sources: [{ id: sourceId, label: sourceId === "codex" ? "Codex" : "Claude Code" }], traceIds: [],
  startedAt: "2026-09-01T12:00:00.000Z", endedAt: "2026-09-01T12:01:00.000Z", activityCount: 3,
  agents: [], activities: [], tokens: tokens(120),
});

describe("session list presentation", () => {
  it("renders a loaded paged collection as a comparative table", async () => {
    const list = document.createElement("am-session-list") as SessionList;
    list.sessions = [session("codex-session")];
    list.hasMore = true;
    document.body.append(list);
    await list.updateComplete;

    const root = list.shadowRoot!;
    expect(root.querySelector('[role="list"]')).not.toBeNull();
    expect(root.querySelectorAll('[role="listitem"]')).toHaveLength(1);
    expect(root.textContent).toContain("Showing 1 loaded sessions");
    expect(root.textContent).toContain("codex-session");
    expect(root.textContent).toContain("120");
    expect(root.querySelector("button[data-more]")).not.toBeNull();
  });

  it("keeps full identity and missing values visible with generated titles", async () => {
    const list = document.createElement("am-session-list") as SessionList;
    list.sessions = [{ ...session("native/session"), startedAt: "", tokens: tokens(), catalog: { role: "root", rootSessionId: "native/session", parentSessionId: "", name: { text: "Review title", origin: "claude_code.generate_session_title" } } }];
    document.body.append(list);
    await list.updateComplete;

    const root = list.shadowRoot!;
    expect(root.querySelector("strong")?.textContent).toBe("Review title");
    expect(root.querySelector(".native-id")?.textContent).toBe("native/session");
    expect(root.textContent).toContain("Available in session details");
    expect(root.querySelectorAll("button")).toHaveLength(1);
  });

  it("keeps same IDs distinct by source for selection and links", async () => {
    const list = document.createElement("am-session-list") as SessionList;
    list.sessions = [session("same", "codex"), session("same", "claude")];
    list.selected = "same";
    list.selectedSource = "claude";
    document.body.append(list);
    await list.updateComplete;

    const links = [...list.shadowRoot!.querySelectorAll<HTMLAnchorElement>("a")];
    expect(links.map((link) => link.getAttribute("href"))).toEqual(["/conversations/codex/same", "/conversations/claude/same"]);
    expect(links.map((link) => link.getAttribute("aria-current"))).toEqual(["false", "page"]);
    expect(list.shadowRoot!.querySelectorAll('[aria-current="true"][role="listitem"]')).toHaveLength(1);
  });

  it("copies from a sibling control without selecting the session", async () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const list = document.createElement("am-session-list") as SessionList;
    list.sessions = [session("copy-me")];
    const selected = vi.fn();
    list.addEventListener("session-selected", selected);
    document.body.append(list);
    await list.updateComplete;

    const row = list.shadowRoot!.querySelector('[role="listitem"]')!;
    expect(row.querySelector("a button")).toBeNull();
    (row.querySelector(".copy-button") as HTMLButtonElement).click();
    await Promise.resolve();
    await list.updateComplete;
    expect(writeText).toHaveBeenCalledWith("codex:copy-me");
    expect(row.querySelector(".copy-button")?.textContent).toContain("Copied");
    expect(row.querySelector(".copy-button")?.getAttribute("aria-label")).toContain("codex:copy-me");
    expect(selected).not.toHaveBeenCalled();
  });

  it("reports a clipboard failure without changing the source-qualified identity", async () => {
    const writeText = vi.fn(() => Promise.reject(new Error("denied")));
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const list = document.createElement("am-session-list") as SessionList;
    list.sessions = [session("copy-failure", "claude")];
    document.body.append(list);
    await list.updateComplete;

    (list.shadowRoot!.querySelector(".copy-button") as HTMLButtonElement).click();
    await vi.waitFor(async () => {
      await list.updateComplete;
      expect(list.shadowRoot!.querySelector(".copy-button")?.textContent).toContain("Copy failed");
    });
    expect(writeText).toHaveBeenCalledWith("claude:copy-failure");
  });
});
