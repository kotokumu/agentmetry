import { describe, expect, it } from "vitest";
import {
  aggregateTraceAgentUsage,
  conversationHref,
  conversationTargetFromLocation,
  tokenEvidence,
} from "./trace-analysis";
import type { Activity, Trace } from "./telemetry";

const activity = (overrides: Partial<Activity>): Activity => ({
  source: "example",
  signal: "trace",
  traceId: "trace-1",
  spanId: "span-1",
  name: "operation",
  kind: "tool",
  agentId: "main",
  runId: "conversation-1",
  model: "model-a",
  observedAt: "2026-08-11T00:00:01Z",
  contributesToTotal: false,
  tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
  ...overrides,
});

const trace = (activities: readonly Activity[]): Trace => ({
  traceId: "trace-1",
  startedAt: "2026-08-11T00:00:00Z",
  endedAt: "2026-08-11T00:00:02Z",
  status: "ok",
  rootSpanCount: 1,
  missingParentCount: 0,
  conversations: [],
  agents: [
    { sourceId: "claude", conversationId: "conversation-a", agentId: "main", agentType: "root", model: "model-a" },
    { sourceId: "codex", conversationId: "conversation-b", agentId: "main", agentDefinition: "reviewer", agentType: "custom", model: "model-b" },
  ],
  activities,
  activityOffset: 0,
  activityCount: activities.length,
  hasMore: false,
});

describe("trace analysis", () => {
  it("aggregates contributing tokens by source-qualified conversation agent", () => {
    const usage = aggregateTraceAgentUsage(trace([
      activity({ source: "claude", runId: "conversation-a", contributesToTotal: true, tokens: { input: 100, output: 20, cacheRead: null, cacheWrite: null, reasoning: null, total: 120 } }),
      activity({ source: "claude", runId: "conversation-a", spanId: "span-2", contributesToTotal: false, tokens: { input: 100, output: 20, cacheRead: null, cacheWrite: null, reasoning: null, total: 120 } }),
      activity({ source: "codex", runId: "conversation-b", contributesToTotal: true, tokens: { input: 50, output: 10, cacheRead: 30, cacheWrite: null, reasoning: 5, total: 60 } }),
    ]));

    expect(usage).toHaveLength(2);
    expect(usage[0]).toMatchObject({ sourceId: "claude", conversationId: "conversation-a", agentId: "main", activityCount: 2, tokens: { input: 100, output: 20, cacheRead: null, total: 120 } });
    expect(usage[1]).toMatchObject({ sourceId: "codex", conversationId: "conversation-b", agentId: "main", agentDefinition: "reviewer", activityCount: 1, tokens: { input: 50, output: 10, cacheRead: 30, reasoning: 5, total: 60 } });
  });

  it("distinguishes complete, partial, and absent token evidence", () => {
    expect(tokenEvidence({ input: 50, output: 10, cacheRead: null, cacheWrite: null, reasoning: null, total: 60 })).toEqual({
      kind: "total",
      total: 60,
      components: [["input", 50], ["output", 10]],
    });
    expect(tokenEvidence({ input: 50, output: null, cacheRead: 30, cacheWrite: null, reasoning: 5, total: null })).toEqual({
      kind: "partial",
      components: [["input", 50], ["cache read", 30], ["reasoning", 5]],
    });
    expect(tokenEvidence({ input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null })).toEqual({
      kind: "none",
      components: [],
    });
  });

  it("builds and parses a source-qualified conversation span URL", () => {
    const evidence = activity({ source: "source name", runId: "conversation/a", spanId: "span 1" });

    expect(conversationHref(evidence)).toBe("/conversations/source%20name/conversation%2Fa?traceId=trace-1&spanId=span+1");
    expect(conversationTargetFromLocation("/conversations/source%20name/conversation%2Fa", "?traceId=trace-1&spanId=span%201")).toEqual({
      sourceId: "source name",
      conversationId: "conversation/a",
      traceId: "trace-1",
      spanId: "span 1",
    });
    expect(conversationTargetFromLocation("/conversations/source/conversation", "?spanId=span-1")).toBeUndefined();
    expect(conversationTargetFromLocation("/conversations/source/extra/path", "")).toBeUndefined();
    expect(conversationHref(activity({ runId: "" }))).toBeUndefined();
  });
});
