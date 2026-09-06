import { describe, expect, it, vi } from "vitest";
import type { ReactiveControllerHost } from "lit";
import type { SessionListEntry, SessionListPage, SessionListQuery } from "../model/session-catalog";
import { SessionListController } from "./session-list-controller";

const row = (id: string): SessionListEntry => ({ id, sourceId: "codex", sources: [], traceIds: [], startedAt: "", endedAt: "", activityCount: 1, agents: [], activities: [], tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null } });
const deferred = () => { let resolve!: (page: SessionListPage) => void; let reject!: (error: Error) => void; const promise = new Promise<SessionListPage>((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const setup = (initiallyActive = true) => {
  let active = initiallyActive;
  let query: SessionListQuery = { range: "24h", sourceId: "", search: "", conditions: {}, view: "roots" };
  const requests: ReturnType<typeof deferred>[] = [];
  const read = vi.fn((_query: SessionListQuery & { pageToken?: string }, _signal?: AbortSignal) => { const pending = deferred(); requests.push(pending); return pending.promise; });
  const host = { addController: vi.fn(), requestUpdate: vi.fn() } as unknown as ReactiveControllerHost;
  const controller = new SessionListController(host, { listSessionsPage: read }, () => query, () => active);
  controller.hostConnected();
  return { controller, read, requests, activate(value: boolean) { active = value; controller.hostUpdate(); }, change(value: Partial<SessionListQuery>) { query = { ...query, ...value }; controller.hostUpdate(); } };
};

describe("session list request ownership", () => {
  it("only reads while active and ignores replies from a previous activation", async () => {
    const test = setup(false);
    await test.controller.refresh();
    expect(test.read).not.toHaveBeenCalled();
    test.activate(true);
    expect(test.read).toHaveBeenCalledTimes(1);
    test.activate(false);
    expect(test.controller.loading).toBe(false);
    test.activate(true);
    test.requests[0].resolve({ sessions: [row("stale")], nextPageToken: "old" });
    await Promise.resolve();
    expect(test.controller.sessions).toEqual([]);
    test.requests[1].resolve({ sessions: [row("current")], nextPageToken: "" });
    await vi.waitFor(() => expect(test.controller.sessions.map((s) => s.id)).toEqual(["current"]));
  });
  it("ignores an old success after a view change and resets the page", async () => {
    const test = setup();
    expect(test.controller.loading).toBe(true);
    test.change({ view: "all" });
    test.requests[1].resolve({ sessions: [row("child")], nextPageToken: "next" });
    await vi.waitFor(() => expect(test.controller.sessions.map((s) => s.id)).toEqual(["child"]));
    test.requests[0].resolve({ sessions: [row("root")], nextPageToken: "old" });
    await Promise.resolve();
    expect(test.controller.sessions.map((s) => s.id)).toEqual(["child"]);
    expect(test.read.mock.calls[1][0]).toMatchObject({ view: "all", pageToken: "" });
  });
  it("coalesces page loads, deduplicates rows, and retains the next token", async () => {
    const test = setup();
    test.requests[0].resolve({ sessions: [row("one")], nextPageToken: "next" });
    await vi.waitFor(() => expect(test.controller.hasMore).toBe(true));
    const first = test.controller.loadMore();
    const second = test.controller.loadMore();
    expect(first).toBe(second);
    expect(test.read).toHaveBeenCalledTimes(2);
    test.requests[1].resolve({ sessions: [row("one"), row("two")], nextPageToken: "" });
    await first;
    expect(test.controller.sessions.map((s) => s.id)).toEqual(["one", "two"]);
    expect(test.controller.hasMore).toBe(false);
  });
  it("refresh supersedes a page and stale rejection cannot fail the current list", async () => {
    const test = setup();
    test.requests[0].resolve({ sessions: [row("old")], nextPageToken: "next" });
    await vi.waitFor(() => expect(test.controller.hasMore).toBe(true));
    const page = test.controller.loadMore();
    const refresh = test.controller.refresh();
    expect(test.controller.sessions.map((s) => s.id)).toEqual(["old"]);
    test.requests[2].resolve({ sessions: [row("fresh")], nextPageToken: "" });
    await refresh;
    test.requests[1].reject(new Error("private failure"));
    await page;
    expect(test.controller.sessions.map((s) => s.id)).toEqual(["fresh"]);
    expect(test.controller.failed).toBe(false);
  });
  it("clears rows on filter changes and does not append during initial loading", async () => {
    const test = setup();
    test.requests[0].resolve({ sessions: [row("old")], nextPageToken: "next" });
    await vi.waitFor(() => expect(test.controller.hasMore).toBe(true));
    test.change({ search: "new" });
    expect(test.controller.sessions).toEqual([]);
    await test.controller.loadMore();
    expect(test.read).toHaveBeenCalledTimes(2);
    test.requests[1].reject(new Error("sensitive server details"));
    await vi.waitFor(() => expect(test.controller.failed).toBe(true));
    expect(test.controller.error).toBe("Session list unavailable");
  });
  it("ignores disconnected replies and retries on reconnect", async () => {
    const test = setup();
    test.controller.hostDisconnected();
    test.requests[0].resolve({ sessions: [row("stale")], nextPageToken: "" });
    await Promise.resolve();
    expect(test.controller.sessions).toEqual([]);
    test.controller.hostConnected();
    test.requests[1].resolve({ sessions: [row("current")], nextPageToken: "" });
    await vi.waitFor(() => expect(test.controller.sessions.map((s) => s.id)).toEqual(["current"]));
  });
});
