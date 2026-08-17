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
const uniformHarness = (fingerprint: string) => ({
  availability: "available",
  state: "uniform",
  counts: { eligibleRecords: 1, reportedRecords: 1, unreportedRecords: 0, invalidRecords: 0, distinctIdentities: 1 },
  identity: { scope: "project-7f2a", fingerprint },
} as const);

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

describe("SessionComparisonController", () => {
  it("loads the most recently completed eligible visible baseline", async () => {
    current = session("current", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");
    const nearest = session("nearest", "2026-08-17T08:00:00Z", "2026-08-17T09:59:00Z");
    const older = session("older", "2026-08-17T07:00:00Z", "2026-08-17T08:00:00Z");
    sessions = [current, older, nearest, session("overlap", "2026-08-17T09:00:00Z", "2026-08-17T10:01:00Z")];
    const getSessionSummary = vi.fn().mockResolvedValue(nearest);
    const getSessionRework = vi.fn().mockResolvedValue(rework("nearest"));
    comparisonClient = { getSessionSummary, getSessionRework } as SessionComparisonReader;

    const host = document.createElement("test-comparison-host") as ComparisonHost;
    document.body.append(host);

    await vi.waitFor(() => expect(host.comparison.baseline?.session.id).toBe("nearest"));
    expect(host.comparison.candidates.map(({ id }) => id)).toEqual(["nearest", "older"]);
    expect(getSessionSummary).toHaveBeenCalledWith("codex", "nearest", expect.any(AbortSignal));
    expect(getSessionRework).toHaveBeenCalledWith("codex", "nearest", expect.any(AbortSignal));
  });

  it("hides the previous result while an explicitly selected baseline loads", async () => {
    current = session("current", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");
    const nearest = session("nearest", "2026-08-17T08:00:00Z", "2026-08-17T09:59:00Z");
    const older = session("older", "2026-08-17T07:00:00Z", "2026-08-17T08:00:00Z");
    sessions = [current, nearest, older];
    let resolveOlderSummary!: (value: Session) => void;
    const olderSummary = new Promise<Session>((resolve) => { resolveOlderSummary = resolve; });
    comparisonClient = {
      getSessionSummary: vi.fn().mockImplementation((_source: string, id: string) => id === "older" ? olderSummary : Promise.resolve(nearest)),
      getSessionRework: vi.fn().mockImplementation((_source: string, id: string) => Promise.resolve(rework(id))),
    } as SessionComparisonReader;

    const host = document.createElement("test-comparison-host") as ComparisonHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.comparison.baseline?.session.id).toBe("nearest"));

    host.comparison.selectBaseline("older");
    await host.updateComplete;
    await vi.waitFor(() => expect(host.comparison.loading).toBe(true));
    expect(host.comparison.selectedBaselineId).toBe("older");
    expect(host.comparison.baseline).toBeUndefined();

    resolveOlderSummary(older);
    await vi.waitFor(() => expect(host.comparison.baseline?.session.id).toBe("older"));
  });

  it("never exposes a response associated with the previous current conversation", async () => {
    const first = session("current-1", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");
    const firstBaseline = session("before-1", "2026-08-17T08:00:00Z", "2026-08-17T09:00:00Z");
    const second = session("current-2", "2026-08-17T12:00:00Z", "2026-08-17T13:00:00Z");
    const secondBaseline = session("before-2", "2026-08-17T11:01:00Z", "2026-08-17T11:59:00Z");
    current = first;
    sessions = [first, firstBaseline];
    let resolveFirst!: (value: Session) => void;
    const firstRequest = new Promise<Session>((resolve) => { resolveFirst = resolve; });
    comparisonClient = {
      getSessionSummary: vi.fn().mockImplementation((_source: string, id: string) => id === "before-1" ? firstRequest : Promise.resolve(secondBaseline)),
      getSessionRework: vi.fn().mockImplementation((_source: string, id: string) => Promise.resolve(rework(id))),
    } as SessionComparisonReader;
    const host = document.createElement("test-comparison-host") as ComparisonHost;
    document.body.append(host);

    current = second;
    sessions = [second, secondBaseline];
    host.requestUpdate();
    await vi.waitFor(() => expect(host.comparison.baseline?.session.id).toBe("before-2"));
    resolveFirst(firstBaseline);
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(host.comparison.baseline?.session.id).toBe("before-2");
  });

  it("isolates a baseline failure and retries it without changing candidates", async () => {
    current = session("current", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");
    const baseline = session("before", "2026-08-17T08:00:00Z", "2026-08-17T09:00:00Z");
    sessions = [current, baseline];
    const getSessionSummary = vi.fn().mockRejectedValueOnce(new Error("temporary")).mockResolvedValueOnce(baseline);
    comparisonClient = {
      getSessionSummary,
      getSessionRework: vi.fn().mockResolvedValue(rework("before")),
    } as SessionComparisonReader;
    const host = document.createElement("test-comparison-host") as ComparisonHost;
    document.body.append(host);

    await vi.waitFor(() => expect(host.comparison.failed).toBe(true));
    expect(host.comparison.viewState()).toMatchObject({ status: "failed", message: "Baseline diagnostics could not be loaded." });
    expect(host.comparison.candidates.map(({ id }) => id)).toEqual(["before"]);
    host.comparison.refresh();
    await vi.waitFor(() => expect(host.comparison.baseline?.session.id).toBe("before"));
    expect(getSessionSummary).toHaveBeenCalledTimes(2);
  });

  it("resets an explicit baseline whenever the current conversation changes, including A to B to A", async () => {
    const first = session("current-a", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");
    const firstDefault = session("a-default", "2026-08-17T09:00:00Z", "2026-08-17T09:59:00Z");
    const firstAlternate = session("a-alternate", "2026-08-17T08:00:00Z", "2026-08-17T08:59:00Z");
    const second = session("current-b", "2026-08-17T12:00:00Z", "2026-08-17T13:00:00Z");
    const secondDefault = session("b-default", "2026-08-17T11:01:00Z", "2026-08-17T11:59:00Z");
    current = first;
    sessions = [first, firstDefault, firstAlternate];
    comparisonClient = {
      getSessionSummary: vi.fn().mockImplementation((_source: string, id: string) => Promise.resolve(sessions.find((candidate) => candidate.id === id))),
      getSessionRework: vi.fn().mockImplementation((_source: string, id: string) => Promise.resolve(rework(id))),
    } as SessionComparisonReader;
    const host = document.createElement("test-comparison-host") as ComparisonHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.comparison.selectedBaselineId).toBe("a-default"));

    host.comparison.selectBaseline("a-alternate");
    await host.updateComplete;
    expect(host.comparison.selectedBaselineId).toBe("a-alternate");

    current = second;
    sessions = [second, secondDefault, first, firstDefault, firstAlternate];
    host.requestUpdate();
    await host.updateComplete;
    expect(host.comparison.selectedBaselineId).toBe("b-default");

    current = first;
    sessions = [first, firstDefault, firstAlternate];
    host.requestUpdate();
    await host.updateComplete;
    expect(host.comparison.selectedBaselineId).toBe("a-default");
  });

  it("refreshes for baseline live changes but leaves current-only refreshes to the conversation controller", async () => {
    current = session("current", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");
    const baseline = session("before", "2026-08-17T08:00:00Z", "2026-08-17T09:00:00Z");
    sessions = [current, baseline];
    const getSessionSummary = vi.fn().mockResolvedValue(baseline);
    comparisonClient = {
      getSessionSummary,
      getSessionRework: vi.fn().mockResolvedValue(rework("before")),
    } as SessionComparisonReader;
    const host = document.createElement("test-comparison-host") as ComparisonHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.comparison.baseline?.session.id).toBe("before"));
    expect(getSessionSummary).toHaveBeenCalledTimes(1);

    await host.comparison.applyLiveUpdate({
      targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "current", traceId: "" }],
      resyncRequired: false,
      throughCursor: "1",
    });
    expect(getSessionSummary).toHaveBeenCalledTimes(1);

    await host.comparison.applyLiveUpdate({
      targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "before", traceId: "" }],
      resyncRequired: false,
      throughCursor: "2",
    });
    expect(getSessionSummary).toHaveBeenCalledTimes(2);
  });

  it("recomputes harness relationships after late evidence without changing an explicit baseline", async () => {
    current = session("current", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");
    const nearest = session("nearest", "2026-08-17T09:00:00Z", "2026-08-17T09:59:00Z");
    const selected = session("selected", "2026-08-17T08:00:00Z", "2026-08-17T08:59:00Z");
    sessions = [current, nearest, selected];
    const fingerprint = "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db";
    const changedFingerprint = "sha256:dfbc1de58f3b905c7b0c0fd79361699336b5f9da617b1db8f35c76673f95b29d";
    let baselineAnalysis: ReworkAnalysis = { ...rework("selected"), harness: uniformHarness(fingerprint) };
    comparisonClient = {
      getSessionSummary: vi.fn().mockImplementation((_source: string, id: string) => Promise.resolve(id === "selected" ? selected : nearest)),
      getSessionRework: vi.fn().mockImplementation((_source: string, id: string) => Promise.resolve(id === "selected" ? baselineAnalysis : rework(id))),
    } as SessionComparisonReader;
    const host = document.createElement("test-comparison-host") as ComparisonHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.comparison.baseline?.session.id).toBe("nearest"));

    host.comparison.selectBaseline("selected");
    await vi.waitFor(() => expect(host.comparison.baseline?.session.id).toBe("selected"));
    const currentSame = { ...rework("current"), harness: uniformHarness(fingerprint) };
    expect(host.comparison.viewState(currentSame)).toMatchObject({ status: "ready", harness: { status: "reported_same" } });

    baselineAnalysis = {
      ...baselineAnalysis,
      harness: { availability: "available", state: "mixed", counts: { eligibleRecords: 2, reportedRecords: 2, unreportedRecords: 0, invalidRecords: 0, distinctIdentities: 2 } },
    };
    await host.comparison.applyLiveUpdate({
      targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "selected", traceId: "" }],
      resyncRequired: false,
      throughCursor: "3",
    });
    expect(host.comparison.selectedBaselineId).toBe("selected");
    expect(host.comparison.viewState(currentSame)).toMatchObject({ status: "ready", harness: { status: "not_comparable", baselineIssue: "mixed" } });

    baselineAnalysis = { ...baselineAnalysis, harness: uniformHarness(fingerprint) };
    await host.comparison.refresh();
    const currentChanged = { ...currentSame, harness: uniformHarness(changedFingerprint) };
    expect(host.comparison.selectedBaselineId).toBe("selected");
    expect(host.comparison.viewState(currentChanged)).toMatchObject({ status: "ready", harness: { status: "reported_changed" } });
  });
});
