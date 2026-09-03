import { describe, expect, it } from "vitest";
import type { Session, TokenUsage } from "./telemetry";
import { compareHarnessContexts, eligibleComparisonBaselines, displayComparisonDirection, roundComparisonDisplay } from "./rework-comparison";

const tokens = (total: number | null): TokenUsage => ({
  input: total, output: 0, cacheRead: null, cacheWrite: null, reasoning: null, total,
});

const session = (
  id: string,
  startedAt: string,
  endedAt: string,
  sourceId = "codex",
  totalTokens: number | null = 1_000,
): Session => ({
  id, sourceId, sources: [{ id: sourceId, label: sourceId }], traceIds: [], startedAt, endedAt,
  activityCount: 0, tokens: tokens(totalTokens), agents: [], activities: [],
});

describe("rework comparison model", () => {
  it.each([0.04, -0.04, 0])("rounds %s only for presentation and suppresses negative zero", (delta) => {
    expect(roundComparisonDisplay(delta)).toBe(0);
    expect(displayComparisonDirection("tool_failure_rate", delta)).toBe("unchanged");
  });

  it("orders only visible, completed, same-source baseline candidates", () => {
    const current = session("current", "2026-08-17T10:00:00Z", "2026-08-17T10:10:00Z");
    const candidates = [
      session("_a", "2026-08-17T08:00:00Z", "2026-08-17T09:59:00Z"),
      session("A", "2026-08-17T08:30:00Z", "2026-08-17T09:59:00Z"),
      session("older", "2026-08-17T07:00:00Z", "2026-08-17T09:00:00Z"),
      session("overlap", "2026-08-17T09:00:00Z", "2026-08-17T10:00:00.001Z"),
      session("other-source", "2026-08-17T08:00:00Z", "2026-08-17T09:58:00Z", "claude"),
      session("invalid", "not-a-time", "also-not-a-time"),
      current,
    ];

    expect(eligibleComparisonBaselines(current, candidates).map(({ id }) => id)).toEqual(["A", "_a", "older"]);
  });

  it("classifies reported fingerprint relationships without implying configuration equality", () => {
    const uniform = { availability: "available", state: "uniform", counts: { eligibleRecords: 2, reportedRecords: 2, unreportedRecords: 0, invalidRecords: 0, distinctIdentities: 1 }, identity: { scope: "project-7f2a", fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", label: "AGENTS v1" } } as const;
    const changed = { ...uniform, identity: { ...uniform.identity, fingerprint: "sha256:dfbc1de58f3b905c7b0c0fd79361699336b5f9da617b1db8f35c76673f95b29d", label: "AGENTS v2" } } as const;
    const otherScope = { ...uniform, identity: { ...uniform.identity, scope: "other-project" } } as const;
    const incomplete = { availability: "available", state: "incomplete", counts: { eligibleRecords: 2, reportedRecords: 1, unreportedRecords: 1, invalidRecords: 0, distinctIdentities: 1 } } as const;
    const unsupported = { availability: "unavailable", reason: "server_unsupported" } as const;

    expect(compareHarnessContexts(uniform, uniform)).toMatchObject({ status: "reported_same" });
    expect(compareHarnessContexts(uniform, changed)).toMatchObject({ status: "reported_changed" });
    expect(compareHarnessContexts(uniform, otherScope)).toMatchObject({ status: "not_comparable", relationshipIssue: "scope_mismatch" });
    expect(compareHarnessContexts(incomplete, unsupported)).toMatchObject({ status: "not_comparable", baselineIssue: "incomplete", currentIssue: "server_unsupported" });
  });
});
