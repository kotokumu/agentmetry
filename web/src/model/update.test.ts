import { describe, expect, it } from "vitest";
import { initialModel, update, type Overview, type Trace } from "./update";

const overview: Overview = {
  sources: [],
  signalCounts: { traces: 3, logs: 2, metrics: 1 },
  runCount: 1,
  agentCount: 2,
  tokens: {
    input: 100,
    output: 20,
    cacheRead: 5,
    cacheWrite: 0,
    reasoning: 4,
    total: 129,
  },
  recentActivity: [],
  sessions: [],
  planUsage: [],
};

describe("dashboard update", () => {
  it("describes loading as state and an effect", () => {
    const before = Object.freeze(initialModel());

    const [after, effects] = update(before, { type: "connected" });

    expect(after).toMatchObject({ status: "loading", requestGeneration: 1 });
    expect(effects).toEqual([{ type: "fetch-overview", generation: 1, range: "24h", sourceId: "", search: "" }]);
    expect(before).toEqual(initialModel());
  });

  it("accepts only the current request generation", () => {
    const loading = update(initialModel(), { type: "connected" })[0];

    const [stale] = update(loading, {
      type: "overview-received",
      generation: 0,
      overview,
    });
    const [current, effects] = update(loading, {
      type: "overview-received",
      generation: 1,
      overview,
    });

    expect(stale).toBe(loading);
    expect(current).toMatchObject({ status: "ready", overview });
    expect(effects).toEqual([]);
  });

  it("changing the time range starts a new query without mutating the prior model", () => {
    const before = Object.freeze(initialModel());

    const [after, effects] = update(before, { type: "range-selected", range: "1h" });

    expect(before.range).toBe("24h");
    expect(after).toMatchObject({ range: "1h", status: "loading", requestGeneration: 1 });
    expect(effects).toEqual([{ type: "fetch-overview", generation: 1, range: "1h", sourceId: "", search: "" }]);
  });

  it("treats source and full-text filters as state that produces a new query", () => {
	const [sourceFiltered, sourceEffects] = update(initialModel(), { type: "source-selected", sourceId: "claude" });
	const [searched, searchEffects] = update(sourceFiltered, { type: "search-submitted", search: "repository review" });

	expect(sourceFiltered).toMatchObject({ sourceId: "claude", search: "" });
	expect(sourceEffects).toEqual([{ type: "fetch-overview", generation: 1, range: "24h", sourceId: "claude", search: "" }]);
	expect(searched).toMatchObject({ sourceId: "claude", search: "repository review", requestGeneration: 2 });
	expect(searchEffects).toEqual([{ type: "fetch-overview", generation: 2, range: "24h", sourceId: "claude", search: "repository review" }]);
  });

  it("requests and appends the next activity page without mutating the overview", () => {
    const activity = {
      source: "example",
      signal: "log" as const,
      name: "first",
      kind: "message" as const,
      agentId: "main",
      runId: "session-1",
      model: "",
      observedAt: "2026-08-11T00:00:00Z",
      contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    };
    const ready: ReturnType<typeof initialModel> & { overview: Overview; selectedSessionId: string } = {
      ...initialModel(),
      status: "ready",
      selectedSessionId: "session-1",
      overview: {
        ...overview,
        sessions: [{
          id: "session-1",
          sourceId: "example",
          sources: [],
          traceIds: [],
          startedAt: activity.observedAt,
          endedAt: activity.observedAt,
          activityCount: 2,
          tokens: activity.tokens,
          agents: [],
          activities: [activity],
        }],
      },
    };

    const [loading, effects] = update(ready, { type: "activities-requested", sessionId: "session-1", sourceId: "example", direction: "older" });
    expect(effects).toEqual([{
      type: "fetch-activities",
      generation: 0,
      range: "24h",
      sourceId: "example",
      search: "",
      sessionId: "session-1",
      direction: "older",
      exact: false,
      offset: 1,
      limit: 100,
      pageToken: "",
    }]);
    expect(loading.activityPage).toEqual({ sessionId: "session-1", sourceId: "example", direction: "older", exact: false, offset: 1, loading: true });

    const second = { ...activity, name: "second", observedAt: "2026-08-10T23:59:59Z" };
    const [received] = update(loading, {
      type: "activities-received",
      generation: 0,
      sessionId: "session-1",
      sourceId: "example",
      direction: "older",
      exact: false,
      offset: 1,
      activities: [second],
      total: 2,
      hasEarlier: false,
      hasMore: false,
    });
    expect(received.overview?.sessions[0].activities.map(({ name }) => name)).toEqual(["first", "second"]);
    expect(ready.overview.sessions[0].activities).toHaveLength(1);
    expect(received.activityPage).toEqual({ sessionId: "session-1", sourceId: "example", direction: "older", exact: false, offset: 1, loading: false });
    expect(received.overview?.sessions[0]).toMatchObject({ activityCount: 2, hasMore: false });
  });

  it("retains loaded rows and exposes a retryable page error", () => {
    const model = { ...initialModel(), activityPage: { sessionId: "session-1", sourceId: "example", direction: "older" as const, exact: false, offset: 100, loading: true } };

    const [after] = update(model, {
      type: "activities-failed",
      generation: 0,
      sessionId: "session-1",
      sourceId: "example",
      direction: "older",
      exact: false,
      offset: 100,
      error: "Temporary failure",
    });

    expect(after.activityPage).toEqual({ sessionId: "session-1", sourceId: "example", direction: "older", exact: false, offset: 100, loading: false, error: "Temporary failure" });
  });

  it("does not request another page while one is already loading", () => {
    const model = { ...initialModel(), activityPage: { sessionId: "session-1", sourceId: "example", direction: "older" as const, exact: false, offset: 100, loading: true } };

    const [after, effects] = update(model, { type: "activities-requested", sessionId: "session-1", sourceId: "example", direction: "older" });

    expect(after).toBe(model);
    expect(effects).toEqual([]);
  });

  it("prepends and appends exact conversation pages using absolute window offsets", () => {
    const target = { sourceId: "claude", conversationId: "session-1", traceId: "trace-1", spanId: "span-1" };
    const activity = (name: string) => ({
      source: "claude", signal: "log" as const, name, kind: "message" as const,
      agentId: "main", runId: "session-1", model: "", observedAt: "2026-08-11T00:00:00Z",
      contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    });
    const conversation = {
      id: "session-1", sourceId: "claude", sources: [], traceIds: [], startedAt: "", endedAt: "",
      activityCount: 300, activityOffset: 100, hasEarlier: true, hasMore: true,
      tokens: overview.tokens, agents: [], activities: [activity("current")],
    };
    const model = {
      ...initialModel(), requestedConversation: target, routedConversation: conversation,
      conversationStatus: "ready" as const, conversationRequestGeneration: 1,
      selectedSessionId: "session-1", selectedSessionSourceId: "claude",
    };

    const [newerLoading, newerEffects] = update(model, {
      type: "activities-requested", sessionId: "session-1", sourceId: "claude", direction: "newer",
    });
    expect(newerEffects).toEqual([{
      type: "fetch-activities", generation: 1, range: "24h", sourceId: "claude", search: "",
      sessionId: "session-1", direction: "newer", exact: true, offset: 0, limit: 100,
      pageToken: "",
      traceId: "trace-1", spanId: "span-1",
    }]);
    const [newer] = update(newerLoading, {
      type: "activities-received", generation: 1, sessionId: "session-1", sourceId: "claude",
      direction: "newer", exact: true, offset: 0, activities: [activity("newer")], total: 300,
      hasEarlier: false, hasMore: true,
    });
    expect(newer.routedConversation).toMatchObject({ activityOffset: 0, hasEarlier: false, hasMore: true });
    expect(newer.routedConversation?.activities.map(({ name }) => name)).toEqual(["newer", "current"]);

    const olderBase = { ...model, routedConversation: { ...conversation, activityOffset: 100, activities: [activity("current")] } };
    const [olderLoading, olderEffects] = update(olderBase, {
      type: "activities-requested", sessionId: "session-1", sourceId: "claude", direction: "older",
    });
    expect(olderEffects[0]).toMatchObject({ exact: true, direction: "older", offset: 101, limit: 100, traceId: "trace-1", spanId: "span-1" });
    const [older] = update(olderLoading, {
      type: "activities-received", generation: 1, sessionId: "session-1", sourceId: "claude",
      direction: "older", exact: true, offset: 101, activities: [activity("older")], total: 300,
      hasEarlier: true, hasMore: true,
    });
    expect(older.routedConversation?.activities.map(({ name }) => name)).toEqual(["current", "older"]);
    expect(older.routedConversation?.activityOffset).toBe(100);
  });

  it("selects a source-qualified conversation requested by a deep link", () => {
    const target = { sourceId: "codex", conversationId: "conversation-1", traceId: "trace-2", spanId: "span-2" };
    const sessions: Overview["sessions"] = [
      { id: "conversation-1", sourceId: "claude", sources: [], traceIds: [], startedAt: "", endedAt: "", activityCount: 0, tokens: overview.tokens, agents: [], activities: [] },
    ];
    const [routed, routeEffects] = update(initialModel(), {
      type: "conversation-route-selected",
      target,
    });
    const loading = update(routed, { type: "connected" })[0];

    const [ready] = update(loading, { type: "overview-received", generation: 1, overview: { ...overview, sessions } });

    expect(routeEffects).toEqual([{ type: "fetch-conversation", generation: 1, target }]);
    expect(ready).toMatchObject({
      selectedSessionId: "conversation-1",
      selectedSessionSourceId: "codex",
      highlightedSpanId: "span-2",
      conversationStatus: "loading",
    });
  });

  it("keeps an exact route failure instead of falling back to another conversation", () => {
    const target = { sourceId: "codex", conversationId: "missing", traceId: "trace-2", spanId: "span-2" };
    const routed = update(initialModel(), { type: "conversation-route-selected", target })[0];
    const overviewLoaded = update(routed, { type: "overview-received", generation: 0, overview })[0];

    const [failed] = update(overviewLoaded, {
      type: "conversation-failed", generation: 1, target, error: "Conversation request failed (404)",
    });

    expect(failed).toMatchObject({
      selectedSessionId: "missing",
      selectedSessionSourceId: "codex",
      conversationStatus: "failed",
      conversationError: "Conversation request failed (404)",
    });
  });

  it("loads a selected trace without changing the selected conversation", () => {
    const before = {
      ...initialModel(),
      selectedSessionId: "conversation-1",
      selectedSessionSourceId: "claude",
    };

    const [after, effects] = update(before, { type: "trace-selected", traceId: "trace-1" });

    expect(after).toMatchObject({
      selectedSessionId: "conversation-1",
      selectedSessionSourceId: "claude",
      selectedTraceId: "trace-1",
      traceStatus: "loading",
      traceRequestGeneration: 1,
    });
    expect(effects).toEqual([{
      type: "fetch-trace",
      generation: 1,
      traceId: "trace-1",
      offset: 0,
      limit: 100,
      pageToken: "",
    }]);
  });

  it("ignores stale trace responses and closes trace state independently", () => {
    const trace: Trace = {
      traceId: "trace-2",
      startedAt: "2026-08-11T00:00:00Z",
      endedAt: "2026-08-11T00:00:01Z",
      status: "ok",
      rootSpanCount: 1,
      missingParentCount: 0,
      conversations: [],
      agents: [],
      activities: [],
      activityOffset: 0,
      activityCount: 0,
      hasMore: false,
    };
    const first = update(initialModel(), { type: "trace-selected", traceId: "trace-1" })[0];
    const second = update(first, { type: "trace-selected", traceId: "trace-2" })[0];

    const [stale] = update(second, { type: "trace-received", generation: 1, traceId: "trace-1", trace });
    const [current] = update(second, { type: "trace-received", generation: 2, traceId: "trace-2", trace });
    const [closed, effects] = update(current, { type: "trace-closed" });

    expect(stale).toBe(second);
    expect(current).toMatchObject({ traceStatus: "ready", trace });
    expect(closed.selectedTraceId).toBeUndefined();
    expect(closed.trace).toBeUndefined();
    expect(closed.traceStatus).toBe("idle");
    expect(effects).toEqual([]);
  });

  it("requests and appends the next trace activity page", () => {
    const firstPage: Trace = {
      traceId: "trace-1", startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:00:01Z", status: "ok",
      rootSpanCount: 1, missingParentCount: 0, conversations: [], agents: [],
      activities: [{ name: "first", source: "test", signal: "trace", kind: "tool", agentId: "", runId: "", model: "", observedAt: "2026-08-11T00:00:00Z", contributesToTotal: false, tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null } }],
      activityOffset: 0, activityCount: 2, hasMore: true, nextPageToken: "page-1",
    };
    const secondPage = { ...firstPage, activities: [{ ...firstPage.activities[0], name: "second" }], activityOffset: 1, hasMore: false, nextPageToken: undefined };
    const selected = update(initialModel(), { type: "trace-selected", traceId: "trace-1" })[0];
    const ready = update(selected, { type: "trace-received", generation: 1, traceId: "trace-1", trace: firstPage })[0];
    const [loading, effects] = update(ready, { type: "trace-activities-requested", traceId: "trace-1", offset: 1, pageToken: "page-1" });
    expect(loading.traceStatus).toBe("loading");
    expect(effects).toEqual([{ type: "fetch-trace", generation: 2, traceId: "trace-1", offset: 1, limit: 100, pageToken: "page-1" }]);
    const [merged] = update(loading, { type: "trace-received", generation: 2, traceId: "trace-1", trace: secondPage });
    expect(merged.trace?.activities.map(({ name }) => name)).toEqual(["first", "second"]);
    expect(merged.trace?.hasMore).toBe(false);
  });
});
