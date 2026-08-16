import { describe, expect, it } from "vitest";
import type { ReworkAnalysis, Session, TokenUsage } from "./telemetry";
import {
  buildReworkComparisonReport,
  compareReworkSnapshots,
  createReworkDiagnosticSnapshot,
  eligibleComparisonBaselines,
} from "./rework-comparison";

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

const analysis = (sessionId: string, values: Partial<ReworkAnalysis["metrics"]> = {}, sourceId = "codex"): ReworkAnalysis => ({
  sourceId,
  sessionId,
  metrics: {
    validationFailures: 0,
    failFixRetryCycles: 0,
    reworkDurationMs: 400,
    totalAgentEffortMs: 1_000,
    reworkAgentEffortRate: 0.4,
    reworkTokens: tokens(400),
    toolAttemptsWithOutcome: 4,
    toolFailures: 2,
    toolFailureRate: 0.5,
    apiRetryWaste: { attempts: 0, durationMs: 0, tokens: tokens(null) },
    repeatedCommands: 0,
    reeditedFiles: 0,
    validationAttemptsWithOutcome: 4,
    firstPassEligibleValidations: 4,
    firstPassSuccesses: 1,
    firstPassSuccessRate: 0.25,
    recurringFailureLoops: 2,
    repeatedFailureAttempts: 0,
    resolvedFailureLoops: 0,
    unresolvedFailureLoops: 0,
    failureResolutionDurationMs: 0,
    failureResolutionTokens: tokens(null),
    ...values,
  },
  coverage: {
    activityCoverage: "observed_projection_complete",
    canonicalEvents: 0,
    classifiedEvents: 0,
    knownOutcomes: 0,
    validationAttempts: 4,
    fingerprintedFailures: 0,
    identifiedValidationAttempts: 4,
    idBackedValidationAttempts: 4,
    mergedValidationAttempts: 0,
    uncorrelatedValidationObservations: 0,
    conflictingAttemptObservations: 0,
    ambiguousFailureAttempts: 0,
  },
  capabilities: {
    changeRevert: { state: "unavailable", reason: "" },
    crossAgentOverlap: { state: "unavailable", reason: "" },
  },
  failureEpisodes: [],
});

describe("rework comparison model", () => {
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

  it("compares all five normalized diagnostics with their desired direction and evidence", () => {
    const baselineSession = session("before", "2026-08-17T08:00:00Z", "2026-08-17T09:00:00Z");
    const currentSession = session("after", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");
    const baseline = createReworkDiagnosticSnapshot(baselineSession, analysis("before"));
    const current = createReworkDiagnosticSnapshot(currentSession, analysis("after", {
      firstPassSuccesses: 3,
      firstPassSuccessRate: 0.75,
      reworkTokens: tokens(200),
      reworkDurationMs: 100,
      reworkAgentEffortRate: 0.1,
      toolFailures: 1,
      toolFailureRate: 0.25,
      validationAttemptsWithOutcome: 5,
      recurringFailureLoops: 1,
    }));

    expect(baseline).toBeDefined();
    expect(current).toBeDefined();
    const rows = compareReworkSnapshots(baseline!, current!);

    expect(rows?.map((row) => ({
      id: row.id,
      baseline: row.baseline.availability === "available" ? row.baseline.displayValue : null,
      current: row.current.availability === "available" ? row.current.displayValue : null,
      delta: row.availability === "comparable" ? row.delta : null,
      direction: row.availability === "comparable" ? row.direction : "unavailable",
    }))).toEqual([
      { id: "initial_validation_success_proxy", baseline: 25, current: 75, delta: 50, direction: "improved" },
      { id: "rework_token_share", baseline: 40, current: 20, delta: -20, direction: "improved" },
      { id: "retry_cycle_effort_share", baseline: 40, current: 10, delta: -30, direction: "improved" },
      { id: "tool_failure_rate", baseline: 50, current: 25, delta: -25, direction: "improved" },
      { id: "recurring_loops_per_100_validations", baseline: 50, current: 20, delta: -30, direction: "improved" },
    ]);
    expect(rows?.[0]?.baseline).toMatchObject({ numerator: 1, denominator: 4 });
    expect(rows?.[4]?.current).toMatchObject({ numerator: 1, denominator: 5 });
  });

  it("marks invalid or missing evidence unavailable while preserving an observed zero", () => {
    const baselineSession = session("before", "2026-08-17T08:00:00Z", "2026-08-17T09:00:00Z");
    const currentSession = session("after", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z", "codex", null);
    const baseline = createReworkDiagnosticSnapshot(baselineSession, analysis("before"));
    const current = createReworkDiagnosticSnapshot(currentSession, analysis("after", {
      firstPassEligibleValidations: 0,
      firstPassSuccesses: 0,
      firstPassSuccessRate: null,
      reworkTokens: tokens(0),
      reworkDurationMs: 2_000,
      totalAgentEffortMs: 1_000,
      reworkAgentEffortRate: 2,
      toolFailures: 0,
      toolFailureRate: 0,
      recurringFailureLoops: 0,
      validationAttemptsWithOutcome: 4,
    }));

    const rows = compareReworkSnapshots(baseline!, current!);
    expect(rows?.find(({ id }) => id === "initial_validation_success_proxy")?.current).toMatchObject({ availability: "unavailable", reason: "No eligible validation identities" });
    expect(rows?.find(({ id }) => id === "rework_token_share")?.current).toMatchObject({ availability: "unavailable", reason: "Session token total unavailable" });
    expect(rows?.find(({ id }) => id === "retry_cycle_effort_share")?.current).toMatchObject({ availability: "unavailable", reason: "Inconsistent retry-cycle effort evidence" });
    expect(rows?.find(({ id }) => id === "tool_failure_rate")?.current).toMatchObject({ availability: "available", displayValue: 0, numerator: 0, denominator: 4 });
    expect(rows?.find(({ id }) => id === "recurring_loops_per_100_validations")?.current).toMatchObject({ availability: "available", displayValue: 0, numerator: 0, denominator: 4 });
  });

  it("rejects mismatched identities, same-session comparisons, and overlapping sessions", () => {
    const baselineSession = session("before", "2026-08-17T08:00:00Z", "2026-08-17T10:30:00Z");
    const currentSession = session("after", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");

    expect(createReworkDiagnosticSnapshot(baselineSession, analysis("different"))).toBeUndefined();
    const baseline = createReworkDiagnosticSnapshot(baselineSession, analysis("before"));
    const current = createReworkDiagnosticSnapshot(currentSession, analysis("after"));
    expect(compareReworkSnapshots(baseline!, current!)).toBeUndefined();
    expect(compareReworkSnapshots(current!, current!)).toBeUndefined();
  });

  it("keeps direction consistent with one-decimal delta display precision", () => {
    const baselineSession = session("before", "2026-08-17T08:00:00Z", "2026-08-17T09:00:00Z");
    const currentSession = session("after", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");
    const baseline = createReworkDiagnosticSnapshot(baselineSession, analysis("before", {
      firstPassEligibleValidations: 10_000,
      firstPassSuccesses: 5_000,
      firstPassSuccessRate: 0.5,
    }));
    const current = createReworkDiagnosticSnapshot(currentSession, analysis("after", {
      firstPassEligibleValidations: 10_000,
      firstPassSuccesses: 5_004,
      firstPassSuccessRate: 0.5004,
    }));

    const row = compareReworkSnapshots(baseline!, current!)?.[0];
    expect(row).toMatchObject({ delta: 0, direction: "unchanged" });
    expect(Object.is(row?.availability === "comparable" ? row.delta : undefined, -0)).toBe(false);
  });

  it("builds an identity-checked report and discloses partial coverage", () => {
    const baselineSession = session("before", "2026-08-17T08:00:00Z", "2026-08-17T09:00:00Z");
    const currentSession = session("after", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");
    const complete = analysis("after");
    const partial: ReworkAnalysis = { ...complete, coverage: { ...complete.coverage, activityCoverage: "partial_page" } };

    expect(buildReworkComparisonReport(baselineSession, analysis("before"), currentSession, partial)).toMatchObject({
      status: "ready",
      warnings: ["Current evidence is a partial retained projection."],
    });
    expect(buildReworkComparisonReport(baselineSession, analysis("different"), currentSession, partial)).toMatchObject({ status: "invalid" });
  });
});
