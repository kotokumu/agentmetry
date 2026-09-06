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
    expect(list.shadowRoot!.textContent).toContain("表示名はテレメトリーから取得できない");
    await localization.select("en");
  });
});
