import { afterEach, describe, expect, it, vi } from "vitest";
import { buildReworkComparisonReport, type ReworkComparisonViewState } from "../model/rework-comparison";
import type { ReworkAnalysis, Session, TokenUsage } from "../model/telemetry";
import "./rework-comparison";
import type { ReworkComparison } from "./rework-comparison";

const tokens = (total: number | null): TokenUsage => ({ input: total, output: 0, cacheRead: null, cacheWrite: null, reasoning: null, total });
const session = (id: string, startedAt: string, endedAt: string): Session => ({
  id, sourceId: "codex", sources: [{ id: "codex", label: "Codex" }], traceIds: [], startedAt, endedAt,
  activityCount: 0, tokens: tokens(1_000), agents: [], activities: [],
});
const analysis = (sessionId: string, current = false): ReworkAnalysis => ({
  sourceId: "codex", sessionId,
  metrics: {
    validationFailures: 0, failFixRetryCycles: 0,
    reworkDurationMs: current ? 100 : 400, totalAgentEffortMs: 1_000, reworkAgentEffortRate: current ? 0.1 : 0.4,
    reworkTokens: tokens(current ? 200 : 400), toolAttemptsWithOutcome: 4, toolFailures: current ? 1 : 2,
    toolFailureRate: current ? 0.25 : 0.5, apiRetryWaste: { attempts: 0, durationMs: 0, tokens: tokens(null) },
    repeatedCommands: 0, reeditedFiles: 0, validationAttemptsWithOutcome: current ? 5 : 4,
    firstPassEligibleValidations: 4, firstPassSuccesses: current ? 3 : 1, firstPassSuccessRate: current ? 0.75 : 0.25,
    recurringFailureLoops: current ? 1 : 2, repeatedFailureAttempts: 0, resolvedFailureLoops: 0, unresolvedFailureLoops: 0,
    failureResolutionDurationMs: 0, failureResolutionTokens: tokens(null),
  },
  coverage: {
    activityCoverage: current ? "partial_page" : "observed_projection_complete", canonicalEvents: 1, classifiedEvents: 1, knownOutcomes: 1,
    validationAttempts: 4, fingerprintedFailures: 0, identifiedValidationAttempts: 4, idBackedValidationAttempts: 4,
    mergedValidationAttempts: 0, uncorrelatedValidationObservations: 0, conflictingAttemptObservations: 0, ambiguousFailureAttempts: 0,
  },
  capabilities: {
    changeRevert: { state: "unavailable", reason: "" }, crossAgentOverlap: { state: "unavailable", reason: "" },
  },
  failureEpisodes: [],
});

const readyState = (baseline: Session, current: Session): ReworkComparisonViewState => {
  const report = buildReworkComparisonReport(baseline, analysis(baseline.id), current, analysis(current.id, true));
  if (report.status !== "ready") throw new Error(report.reason);
  return {
    status: "ready",
    options: [{ sessionId: baseline.id, endedAt: baseline.endedAt }],
    selectedBaselineId: baseline.id,
    rows: report.rows,
    warnings: report.warnings,
  };
};

afterEach(() => document.body.replaceChildren());

describe("am-rework-comparison", () => {
  it("renders normalized before/after rows, evidence, explanations, and projection warnings", async () => {
    const baseline = session("baseline-session-123456", "2026-08-17T08:00:00Z", "2026-08-17T09:00:00Z");
    const current = session("current-session-123456", "2026-08-17T10:00:00Z", "2026-08-17T11:00:00Z");
    const panel = document.createElement("am-rework-comparison") as ReworkComparison;
    panel.state = readyState(baseline, current);
    document.body.append(panel);
    await panel.updateComplete;

    const content = panel.shadowRoot?.textContent ?? "";
    expect(content).toContain("Before / After diagnostics");
    expect(content).toContain("Initial validation success proxy");
    expect(content).toContain("25.0%");
    expect(content).toContain("75.0%");
    expect(content).toContain("+50.0 pp");
    expect(content).toContain("Improved");
    expect(content).toContain("1 / 4 eligible identities");
    expect(content).toContain("1 / 5 outcome-known validations");
    expect(content).toContain("Current evidence is a partial retained projection");
    expect(content).toContain("not causal evidence");
    expect(panel.shadowRoot?.querySelector("table caption")?.textContent).toContain("Normalized diagnostic comparison");
    expect(panel.shadowRoot?.querySelectorAll("th[scope='col']")).toHaveLength(4);
    expect(panel.shadowRoot?.querySelectorAll("details.metric-help")).toHaveLength(5);
    const help = panel.shadowRoot?.querySelector<HTMLDetailsElement>("details.metric-help");
    help?.querySelector("summary")?.click();
    expect(help?.open).toBe(true);
    expect(help?.textContent).toContain("not task- or change-level first-pass success");
  });

  it("publishes source-local baseline selection across the shadow boundary", async () => {
    const first = session("first", "2026-08-17T08:00:00Z", "2026-08-17T09:00:00Z");
    const second = session("second", "2026-08-17T07:00:00Z", "2026-08-17T08:00:00Z");
    const panel = document.createElement("am-rework-comparison") as ReworkComparison;
    panel.state = {
      status: "loading",
      options: [first, second].map(({ id, endedAt }) => ({ sessionId: id, endedAt })),
      selectedBaselineId: first.id,
    };
    const selected = vi.fn();
    panel.addEventListener("comparison-baseline-selected", selected);
    document.body.append(panel);
    await panel.updateComplete;

    const select = panel.shadowRoot?.querySelector("select");
    if (!select) throw new Error("baseline select missing");
    select.value = second.id;
    select.dispatchEvent(new Event("change", { bubbles: true }));

    expect(selected).toHaveBeenCalledOnce();
    expect((selected.mock.calls[0]?.[0] as CustomEvent).detail).toEqual({ sessionId: "second" });
    expect((selected.mock.calls[0]?.[0] as CustomEvent).bubbles).toBe(true);
    expect((selected.mock.calls[0]?.[0] as CustomEvent).composed).toBe(true);
  });

  it("isolates empty, loading, and retryable error states", async () => {
    const panel = document.createElement("am-rework-comparison") as ReworkComparison;
    document.body.append(panel);
    await panel.updateComplete;
    expect(panel.shadowRoot?.textContent).toContain("No non-overlapping baseline in the current conversation list");

    const before = session("before", "2026-08-17T08:00:00Z", "2026-08-17T09:00:00Z");
    const context = { options: [{ sessionId: before.id, endedAt: before.endedAt }], selectedBaselineId: before.id } as const;
    panel.state = { ...context, status: "loading" };
    await panel.updateComplete;
    expect(panel.shadowRoot?.querySelector("[role='status']")?.textContent).toContain("Loading baseline diagnostics");

    const retry = vi.fn();
    panel.addEventListener("comparison-retry-requested", retry);
    panel.state = { ...context, status: "failed", message: "Temporary comparison failure" };
    await panel.updateComplete;
    panel.shadowRoot?.querySelector<HTMLButtonElement>("button.retry")?.click();
    expect(panel.shadowRoot?.querySelector("[role='alert']")?.textContent).toContain("Temporary comparison failure");
    expect(retry).toHaveBeenCalledOnce();
  });
});
