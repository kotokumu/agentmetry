import { Code, ConnectError } from "@connectrpc/connect";
import type { ReworkComparisonPair, SharedReworkComparison } from "../model/rework-comparison";
import { LitElement } from "lit";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProjectionTargetKind } from "../gen/agentmetry/v1/agentmetry_pb";
import type { ReworkAnalysis, Session, TokenUsage } from "../model/telemetry";
import { SessionComparisonController, type SessionComparisonReader } from "./session-comparison-controller";

const tokens: TokenUsage = { input: 100, output: 0, cacheRead: null, cacheWrite: null, reasoning: null, total: 100 };
const session = (id: string, startedAt: string, endedAt: string, sourceId = "codex"): Session => ({
  id, sourceId, sources: [{ id: sourceId, label: sourceId }], traceIds: [], startedAt, endedAt,
  activityCount: 0, tokens, agents: [], activities: [],
});
const rework = (sessionId: string, sourceId = "codex"): ReworkAnalysis => ({
  sourceId, sessionId,
  sessionTokens: tokens,
  harness: { availability: "available", state: "unreported", counts: { eligibleRecords: 1, reportedRecords: 0, unreportedRecords: 1, invalidRecords: 0, distinctIdentities: 0 } },
  metrics: {
    validationFailures: 0, failFixRetryCycles: 0, reworkDurationMs: 0, totalAgentEffortMs: 100, reworkAgentEffortRate: 0,
    reworkTokens: { ...tokens, total: 0 }, toolAttemptsWithOutcome: 1, toolFailures: 0, toolFailureRate: 0,
    apiRetryWaste: { attempts: 0, durationMs: 0, tokens }, repeatedCommands: 0, reeditedFiles: 0,
    validationAttemptsWithOutcome: 1, firstPassEligibleValidations: 1, firstPassSuccesses: 1, firstPassSuccessRate: 1,
    recurringFailureLoops: 0, repeatedFailureAttempts: 0, resolvedFailureLoops: 0, unresolvedFailureLoops: 0,
    failureResolutionDurationMs: 0, failureResolutionTokens: tokens,
  },
  coverage: {
    activityCoverage: "observed_projection_complete", canonicalEvents: 1, classifiedEvents: 1, knownOutcomes: 1,
    validationAttempts: 1, fingerprintedFailures: 0, identifiedValidationAttempts: 1, idBackedValidationAttempts: 1,
    mergedValidationAttempts: 0, uncorrelatedValidationObservations: 0, conflictingAttemptObservations: 0, ambiguousFailureAttempts: 0,
  },
  capabilities: {
    changeRevert: { state: "unavailable", reason: "" },
    crossAgentOverlap: { state: "unavailable", reason: "" },
  },
  failureEpisodes: [],
});
let comparisonClient: SessionComparisonReader;
let current: Session | undefined;
let sessions: readonly Session[] = [];
let active = true;

class ComparisonHost extends LitElement {
  readonly comparison = new SessionComparisonController(
    this,
    {
      reader: comparisonClient,
      current: () => current,
      sessions: () => sessions,
      isActive: () => active,
    },
  );
}

customElements.define("test-comparison-host", ComparisonHost);
afterEach(() => {
  document.body.replaceChildren();
  current = undefined;
  sessions = [];
  active = true;
});

const pairResult = (pair: ReworkComparisonPair, delta = 17): SharedReworkComparison => {
  const subject = (reference: ReworkComparisonPair["baseline"]) => ({ ...reference,
    startedAt: "2026-08-17T08:00:00Z", endedAt: "2026-08-17T09:00:00Z",
    projectionCoverage: "complete" as const, coverage: rework(reference.sessionId).coverage,
    harness: rework(reference.sessionId).harness,
  });
  return { status: "ready", baseline: subject(pair.baseline), current: subject(pair.current), warnings: [],
    harness: { status: "not_comparable", baseline: rework("before").harness, current: rework("current").harness, baselineIssue: "unreported", currentIssue: "unreported" },
    rows: [{ id: "tool_failure_rate", unit: "percent", availability: "comparable", delta, direction: "regressed",
      baseline: { availability: "available", numerator: 1, denominator: 4, displayValue: 25 },
      current: { availability: "available", numerator: 2, denominator: 4, displayValue: 50 } }],
  };
};
const setupPair = () => {
  current = session("current", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");
  sessions = [current, session("nearest", "2026-08-17T08:00:00Z", "2026-08-17T09:59:00Z"), session("older", "2026-08-17T07:00:00Z", "2026-08-17T08:00:00Z")];
};
const mount = () => {
  const host = document.createElement("test-comparison-host") as ComparisonHost;
  document.body.append(host);
  return host;
};

describe("SessionComparisonController", () => {
  it("uses the returned comparison without recomputing values from separately loaded analyses", async () => {
    setupPair();
    comparisonClient = { compareRework: vi.fn().mockImplementation(async (pair: ReworkComparisonPair) => pairResult(pair)) };
    const host = mount();
    await vi.waitFor(() => expect(host.comparison.viewState()).toMatchObject({ status: "ready", rows: [{ delta: 17 }] }));
    expect(host.comparison.selectedBaselineId).toBe("nearest");
    expect(host.comparison.candidates.map(({ id }) => id)).toEqual(["nearest", "older"]);
  });

  it("hides an old comparison while a different explicit baseline loads", async () => {
    setupPair();
    let release!: (value: SharedReworkComparison) => void;
    const pending = new Promise<SharedReworkComparison>((resolve) => { release = resolve; });
    comparisonClient = { compareRework: vi.fn().mockImplementation(async (pair: ReworkComparisonPair) => pair.baseline.sessionId === "older" ? pending : pairResult(pair)) };
    const host = mount();
    await vi.waitFor(() => expect(host.comparison.viewState().status).toBe("ready"));
    host.comparison.selectBaseline("older");
    await vi.waitFor(() => expect(host.comparison.viewState()).toMatchObject({ status: "loading", selectedBaselineId: "older" }));
    release(pairResult({ baseline: { sourceId: "codex", sessionId: "older" }, current: { sourceId: "codex", sessionId: "current" } }, 9));
    await vi.waitFor(() => expect(host.comparison.viewState()).toMatchObject({ status: "ready", selectedBaselineId: "older", rows: [{ delta: 9 }] }));
  });

  it("rejects a response for another source-qualified pair", async () => {
    setupPair();
    comparisonClient = { compareRework: vi.fn().mockImplementation(async (pair: ReworkComparisonPair) => pairResult({ ...pair, current: { ...pair.current, sourceId: "claude" } })) };
    const host = mount();
    await vi.waitFor(() => expect(host.comparison.viewState()).toMatchObject({ status: "failed" }));
    expect(host.comparison.viewState()).toHaveProperty("message", "Comparison identities do not match the requested conversations.");
  });

  it("reports an unsupported comparison server explicitly and retries operational failures", async () => {
    setupPair();
    comparisonClient = { compareRework: vi.fn().mockRejectedValueOnce(new ConnectError("unimplemented", Code.Unimplemented)).mockImplementation(async (pair: ReworkComparisonPair) => pairResult(pair)) };
    const host = mount();
    await vi.waitFor(() => expect(host.comparison.viewState()).toMatchObject({ status: "failed", message: "This server does not support shared diagnostic comparison." }));
    await host.comparison.refresh();
    expect(host.comparison.viewState().status).toBe("ready");
    expect(host.comparison.selectedBaselineId).toBe("nearest");
  });

  it("resets an explicit baseline when current conversation changes", async () => {
    setupPair();
    comparisonClient = { compareRework: vi.fn().mockImplementation(async (pair: ReworkComparisonPair) => pairResult(pair)) };
    const host = mount();
    await vi.waitFor(() => expect(host.comparison.viewState().status).toBe("ready"));
    host.comparison.selectBaseline("older");
    await host.updateComplete;
    expect(host.comparison.selectedBaselineId).toBe("older");
    const original = current;
    current = session("new-current", "2026-08-17T12:00:00Z", "2026-08-17T13:00:00Z");
    host.requestUpdate();
    await host.updateComplete;
    expect(host.comparison.selectedBaselineId).toBe("current");
    current = original;
    host.requestUpdate();
    await host.updateComplete;
    expect(host.comparison.selectedBaselineId).toBe("nearest");
  });

  it("refreshes the coherent pair for changes to either conversation while keeping the explicit baseline", async () => {
    setupPair();
    let delta = 1;
    comparisonClient = { compareRework: vi.fn().mockImplementation(async (pair: ReworkComparisonPair) => pairResult(pair, delta)) };
    const host = mount();
    await vi.waitFor(() => expect(host.comparison.viewState().status).toBe("ready"));
    host.comparison.selectBaseline("older");
    await vi.waitFor(() => expect(host.comparison.viewState()).toMatchObject({ status: "ready", selectedBaselineId: "older" }));
    for (const sessionId of ["current", "older"]) {
      delta += 1;
      await host.comparison.applyLiveUpdate({ targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId, traceId: "" }], resyncRequired: false, throughCursor: String(delta) });
      expect(host.comparison.viewState()).toMatchObject({ status: "ready", selectedBaselineId: "older", rows: [{ delta }] });
    }
  });
});
