import { describe, expect, it } from "vitest";
import { observedActivityCount, selectedSession } from "./selectors";
import { initialModel, type Model, type Overview } from "./update";

const overview = {
  sources: [],
  signalCounts: { traces: 100, logs: 20, metrics: 3 },
  runCount: 1,
  agentCount: 2,
  tokens: { input: 4, output: 1, cacheRead: 0, cacheWrite: 0, reasoning: 0, total: 5 },
  recentActivity: [],
  planUsage: [],
  sessions: [
    { id: "session-a", sourceId: "example", sources: [], traceIds: [], startedAt: "", endedAt: "", activityCount: 7, tokens: { input: 4, output: 1, cacheRead: 0, cacheWrite: 0, reasoning: 0, total: 5 }, agents: [], activities: [] },
  ],
} satisfies Overview;

describe("dashboard selectors", () => {
  it("counts semantic session activities rather than raw OTLP volume", () => {
    expect(observedActivityCount(overview)).toBe(7);
  });

  it("selects a session without mutating UI state", () => {
    const model = { ...initialModel(), status: "ready", requestGeneration: 1, selectedSessionId: "session-a", selectedSessionSourceId: "example", overview } satisfies Model;
    expect(selectedSession(model)?.id).toBe("session-a");
  });

  it("does not fall back to overview while an exact route is loading or failed", () => {
    const requestedConversation = { sourceId: "example", conversationId: "session-a", traceId: "trace-1", spanId: "span-1" };
    const loading = {
      ...initialModel(), overview, selectedSessionId: "session-a", selectedSessionSourceId: "example",
      requestedConversation, conversationStatus: "loading" as const,
    };
    const failed = { ...loading, conversationStatus: "failed" as const, conversationError: "not found" };

    expect(selectedSession(loading)).toBeUndefined();
    expect(selectedSession(failed)).toBeUndefined();
  });
});
