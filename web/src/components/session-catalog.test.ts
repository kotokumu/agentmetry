import { afterEach, describe, expect, it, vi } from "vitest";
import "./session-list";
import type { SessionList } from "./session-list";
import { localization } from "../localization/localization";

afterEach(() => document.body.replaceChildren());

describe("session catalog controls", () => {
  it("exposes a labelled native control, child role, paging and fixed failure", async () => {
    await localization.select("en");
    const list = document.createElement("am-session-list") as SessionList;
    list.view = "all";
    list.hasMore = true;
    list.sessions = [{ id: "child", sourceId: "codex", sources: [], traceIds: [], startedAt: "", endedAt: "", activityCount: 1, agents: [], activities: [], tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null }, catalog: { role: "child", rootSessionId: "root", parentSessionId: "root" } }];
    const change = vi.fn(); const more = vi.fn();
    list.addEventListener("session-list-view-selected", change);
    list.addEventListener("sessions-more-requested", more);
    document.body.append(list);
    await list.updateComplete;
    const toggle = list.shadowRoot!.querySelector<HTMLInputElement>("input[type=checkbox]")!;
    expect(toggle.checked).toBe(true);
    expect(toggle.labels?.[0]?.textContent).toContain("Show all");
    toggle.click();
    expect(change.mock.calls[0][0].detail).toEqual({ view: "roots" });
    expect(list.shadowRoot!.textContent).toContain("Child session");
    expect(list.shadowRoot!.querySelector("strong")!.textContent).toBe("child");
    list.shadowRoot!.querySelector<HTMLButtonElement>("button[data-more]")!.click();
    expect(more).toHaveBeenCalledTimes(1);
  });
  it("explains telemetry limits in Japanese without claiming human creation", async () => {
    await localization.select("ja");
    const list = document.createElement("am-session-list") as SessionList;
    document.body.append(list); await list.updateComplete;
    expect(list.shadowRoot!.textContent).toContain("すべてを表示");
    expect(list.shadowRoot!.textContent).toContain("人が作成したことを保証しません");
    expect(list.shadowRoot!.textContent).toContain("Claude Codeの自動生成名");
    expect(list.shadowRoot!.textContent).toContain("手動変更後");
    await localization.select("en");
  });
  it.each(["en", "ja"])("shows the generated text safely with its identity, origin and observed time (%s)", async (locale) => {
    await localization.select(locale);
    const list = document.createElement("am-session-list");
    const text = '<img src=x onerror="alert(1)">';
    list.sessions = [{ id: "native/id", sourceId: "claude", sources: [], traceIds: [], startedAt: "", endedAt: "", activityCount: 1, agents: [], activities: [], tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null }, catalog: { role: "root", rootSessionId: "native/id", parentSessionId: "", name: { text, origin: "claude_code.generate_session_title", observedAt: "2026-09-01T12:00:00.000Z" } } }];
    list.selected = "native/id";
    list.selectedSource = "claude";
    const selected = vi.fn();
    list.addEventListener("session-selected", selected);
    document.body.append(list); await list.updateComplete;
    const root = list.shadowRoot!;
    expect(root.querySelector("strong")!.textContent).toBe(text);
    expect(root.querySelector("img")).toBeNull();
    expect(root.querySelector(".native-id")!.textContent).toBe("native/id");
    expect(root.querySelector(".name-origin")!.textContent).toBe(locale === "ja" ? "自動生成名" : "Generated name");
    expect(root.querySelector("time")!.getAttribute("datetime")).toBe("2026-09-01T12:00:00.000Z");
    const link = root.querySelector("a")!;
    expect(link.getAttribute("href")).toBe("/conversations/claude/native%2Fid");
    expect(link.getAttribute("aria-current")).toBe("page");
    link.click();
    expect(selected.mock.calls[0][0].detail).toEqual({ sourceId: "claude", sessionId: "native/id" });
    list.sessions = [{ ...list.sessions[0], catalog: { ...list.sessions[0].catalog!, name: { text: "Next generated name", origin: "claude_code.generate_session_title" } } }];
    await list.updateComplete;
    expect(root.querySelector("time")).toBeNull();
    expect(root.querySelector("a")!.getAttribute("aria-current")).toBe("page");
    expect(root.querySelector("strong")!.textContent).toBe("Next generated name");
    await localization.select("en");
  });
});
