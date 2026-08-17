import { afterEach, describe, expect, it, vi } from "vitest";
import "./kpi-card";
import "./activity-table";
import "./time-range-filter";
import "./plan-usage";
import "./agent-tree";
import "./session-filter";
import "./session-list";
import "./trace-summary";
import "./trace-participants";
import "./trace-waterfall";
import "./token-chart";
import "./rework-summary";
import type { ActivityTable } from "./activity-table";
import type { KpiCard } from "./kpi-card";
import type { TimeRangeFilter } from "./time-range-filter";
import type { PlanUsage } from "./plan-usage";
import { buildAgentTree, layoutAgentTree } from "./agent-tree";
import type { AgentTree } from "./agent-tree";
import type { SessionFilter } from "./session-filter";
import type { SessionList } from "./session-list";
import type { TraceSummary } from "./trace-summary";
import type { TraceParticipants } from "./trace-participants";
import type { TraceWaterfall } from "./trace-waterfall";
import type { Trace } from "../model/telemetry";
import type { ReworkAnalysis } from "../model/telemetry";
import type { ReworkSummary } from "./rework-summary";

afterEach(() => vi.useRealTimers());

const traceFixture: Trace = {
  traceId: "trace-123456789",
  startedAt: "2026-08-11T00:00:00Z",
  endedAt: "2026-08-11T00:00:04Z",
  status: "error",
  rootSpanCount: 1,
  missingParentCount: 1,
  conversations: [
    { sourceId: "claude", id: "conversation-a" },
    { sourceId: "codex", id: "conversation-b" },
  ],
  agents: [
    { sourceId: "claude", conversationId: "conversation-a", agentId: "main", agentType: "root", model: "model-a" },
    { sourceId: "codex", conversationId: "conversation-b", agentId: "reviewer", agentDefinition: "repository-review", agentType: "custom", model: "model-b" },
  ],
  activities: [
    {
      source: "claude", signal: "trace", traceId: "trace-123456789", spanId: "root", name: "root operation", kind: "reasoning",
      agentId: "main", runId: "conversation-a", model: "model-a", startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:00:04Z",
      observedAt: "2026-08-11T00:00:04Z", status: "Ok", contributesToTotal: true,
      tokens: { input: 100, output: 20, cacheRead: null, cacheWrite: null, reasoning: null, total: 120 },
    },
    {
      source: "codex", signal: "trace", traceId: "trace-123456789", spanId: "child", parentSpanId: "missing", name: "child operation", kind: "delegation",
      agentId: "reviewer", agentDefinition: "repository-review", targetAgentId: "main", targetAgentType: "root", runId: "conversation-b", model: "model-b", startedAt: "2026-08-11T00:00:01Z", endedAt: "2026-08-11T00:00:02Z",
      observedAt: "2026-08-11T00:00:02Z", status: "Error", contributesToTotal: true,
      tokens: { input: 50, output: 10, cacheRead: 30, cacheWrite: null, reasoning: 5, total: 60 },
    },
    {
      source: "codex", signal: "log", traceId: "trace-123456789", spanId: "child", name: "tool message", kind: "message", content: "Correlated log",
      agentId: "reviewer", runId: "conversation-b", model: "model-b", startedAt: "2026-08-11T00:00:01.500Z", endedAt: "2026-08-11T00:00:01.500Z",
      observedAt: "2026-08-11T00:00:01.500Z", contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    },
  ],
  activityOffset: 0,
  activityCount: 3,
  hasMore: false,
};

const reworkFixture: ReworkAnalysis = {
  sourceId: "codex",
  sessionId: "session-1",
  sessionTokens: { input: 800, output: 200, cacheRead: null, cacheWrite: null, reasoning: null, total: 1_000 },
  harness: { availability: "available", state: "unreported", counts: { eligibleRecords: 8, reportedRecords: 0, unreportedRecords: 8, invalidRecords: 0, distinctIdentities: 0 } },
  metrics: {
    validationFailures: 2, failFixRetryCycles: 1, reworkDurationMs: 3500,
    totalAgentEffortMs: 10_000, reworkAgentEffortRate: 0.35,
    reworkTokens: { input: 100, output: 20, cacheRead: null, cacheWrite: null, reasoning: null, total: 120 },
    toolAttemptsWithOutcome: 4, toolFailures: 1, toolFailureRate: 0.25,
    apiRetryWaste: { attempts: 1, durationMs: 500, tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null } },
    repeatedCommands: 3, reeditedFiles: 2,
    validationAttemptsWithOutcome: 4, firstPassEligibleValidations: 2, firstPassSuccesses: 1, firstPassSuccessRate: 0.5,
    recurringFailureLoops: 1, repeatedFailureAttempts: 3, resolvedFailureLoops: 1, unresolvedFailureLoops: 0,
    failureResolutionDurationMs: 6_500,
    failureResolutionTokens: { input: 30, output: 8, cacheRead: null, cacheWrite: null, reasoning: null, total: 38 },
  },
  coverage: { activityCoverage: "partial_page", canonicalEvents: 8, classifiedEvents: 7, knownOutcomes: 4, validationAttempts: 4, fingerprintedFailures: 2, identifiedValidationAttempts: 4, idBackedValidationAttempts: 3, mergedValidationAttempts: 1, uncorrelatedValidationObservations: 1, conflictingAttemptObservations: 0, ambiguousFailureAttempts: 0 },
  capabilities: {
    changeRevert: { state: "unavailable", reason: "needs diffs" },
    crossAgentOverlap: { state: "unavailable", reason: "needs identities" },
  },
  failureEpisodes: [{
    agentId: "agent-1", operation: "test", validationFingerprint: "sha256:abcdef1234567890", errorFingerprints: ["sha256:1234567890abcdef"],
    failureAttempts: 3, resolved: true, resolutionDurationMs: 6_500,
    resolutionTokens: { input: 30, output: 8, cacheRead: null, cacheWrite: null, reasoning: null, total: 38 },
    traceId: "trace-1", spanId: "span-1",
  }],
};

describe("dashboard components", () => {
  it("renders session rework metrics with coverage and capability limits", async () => {
    const panel = document.createElement("am-rework-summary") as ReworkSummary;
    panel.analysis = reworkFixture;
    document.body.append(panel);

    await panel.updateComplete;

    const content = panel.shadowRoot?.textContent ?? "";
    const cards = Array.from(panel.shadowRoot?.querySelectorAll<KpiCard>("am-kpi-card") ?? []);
    await Promise.all(cards.map((card) => card.updateComplete));
    const cardContent = cards.map((card) => card.shadowRoot?.textContent ?? "").join(" ");
    expect(content).toContain("Development rework");
    expect(cardContent).toContain("Validation failures");
    expect(cardContent).toContain("Initial validation success");
    expect(cardContent).toContain("50.0%");
    expect(cardContent).toContain("Recurring failure loops");
    expect(cardContent).toContain("Failure attempts in recurring loops");
    expect(cardContent).toContain("Total loop resolution time");
    expect(cardContent).toContain("6.5 s");
    expect(cardContent).toContain("Tokens in resolved loops");
    expect(cardContent).toContain("Detected retry-cycle share");
    expect(cardContent).toContain("35.0%");
    expect(cardContent).toContain("3.5 s of 10 s observed agent-active time");
    expect(cardContent).toContain("25.0%");
    expect(cardContent).toContain("Rework token rate");
    expect(cardContent).toContain("12.0%");
    expect(cardContent).toContain("120 of 1,000 session tokens");
    expect(content).toContain("Partial evidence");
    expect(content).toContain("4 of 4 logical validation attempts report outcomes");
    expect(content).toContain("Change revert");
    expect(content).toContain("Highest-impact recurring loops");
    expect(content).toContain("validation abcdef1234");
    expect(content).toContain("3 failed attempts");
    expect(panel.shadowRoot?.querySelector<HTMLAnchorElement>('.episode a')?.getAttribute("href")).toBe("/traces/trace-1");
    expect(content).toContain("Not available");
    expect(cards).toHaveLength(12);
    expect(cards.every((card) => card.description.length > 0)).toBe(true);
    expect(cards.every((card) => card.shadowRoot?.querySelector("button.help"))).toBe(true);
  });

  it("does not report a rework effort rate without observed agent duration", async () => {
    const panel = document.createElement("am-rework-summary") as ReworkSummary;
    panel.analysis = {
      ...reworkFixture,
      metrics: {
        ...reworkFixture.metrics,
        totalAgentEffortMs: 0,
        reworkAgentEffortRate: null,
      },
    };
    document.body.append(panel);

    await panel.updateComplete;

    const card = Array.from(panel.shadowRoot?.querySelectorAll<KpiCard>("am-kpi-card") ?? [])
      .find((candidate) => candidate.label === "Detected retry-cycle share");
    await card?.updateComplete;
    expect(card?.value).toBe("Not reported");
    expect(card?.hint).toBe("Observed agent-active duration unavailable");
  });

  it("labels an observed zero as no detected closed retry cycles", async () => {
    const panel = document.createElement("am-rework-summary") as ReworkSummary;
    panel.analysis = {
      ...reworkFixture,
      metrics: {
        ...reworkFixture.metrics,
        reworkDurationMs: 0,
        reworkAgentEffortRate: 0,
      },
    };
    document.body.append(panel);

    await panel.updateComplete;

    const card = Array.from(panel.shadowRoot?.querySelectorAll<KpiCard>("am-kpi-card") ?? [])
      .find((candidate) => candidate.label === "Detected retry-cycle share");
    await card?.updateComplete;
    expect(card?.value).toBe("0.0%");
    expect(card?.hint).toBe("No detected closed retry cycles · 10 s observed agent-active time");
  });

  it("does not report a rework token rate without a positive session token total", async () => {
    const panel = document.createElement("am-rework-summary") as ReworkSummary;
    panel.analysis = { ...reworkFixture, sessionTokens: { ...reworkFixture.sessionTokens!, total: 0 } };
    document.body.append(panel);

    await panel.updateComplete;

    const card = Array.from(panel.shadowRoot?.querySelectorAll<KpiCard>("am-kpi-card") ?? [])
      .find((candidate) => candidate.label === "Rework token rate");
    await card?.updateComplete;
    expect(card?.value).toBe("Not reported");
    expect(card?.hint).toBe("Session token total unavailable");
  });

  it("uses the session summary denominator only for an old server response", async () => {
    const panel = document.createElement("am-rework-summary") as ReworkSummary;
    panel.analysis = {
      ...reworkFixture,
      sessionTokens: undefined,
      harness: { availability: "unavailable", reason: "server_unsupported" },
    };
    panel.legacySessionTotalTokens = 2_000;
    document.body.append(panel);

    await panel.updateComplete;

    const card = Array.from(panel.shadowRoot?.querySelectorAll<KpiCard>("am-kpi-card") ?? [])
      .find((candidate) => candidate.label === "Rework token rate");
    await card?.updateComplete;
    expect(card?.value).toBe("6.0%");
    expect(card?.hint).toBe("120 of 2,000 session tokens");
  });

  it("does not render unresolved recurring loops as zero-time resolutions", async () => {
    const panel = document.createElement("am-rework-summary") as ReworkSummary;
    panel.analysis = {
      ...reworkFixture,
      metrics: {
        ...reworkFixture.metrics,
        resolvedFailureLoops: 0,
        unresolvedFailureLoops: 1,
        failureResolutionDurationMs: 0,
        failureResolutionTokens: { ...reworkFixture.metrics.failureResolutionTokens, total: null },
      },
    };
    document.body.append(panel);
    await panel.updateComplete;

    const cards = Array.from(panel.shadowRoot?.querySelectorAll<KpiCard>("am-kpi-card") ?? []);
    const duration = cards.find((candidate) => candidate.label === "Total loop resolution time");
    const tokens = cards.find((candidate) => candidate.label === "Tokens in resolved loops");
    await Promise.all([duration?.updateComplete, tokens?.updateComplete]);
    expect(duration?.value).toBe("Not reported");
    expect(tokens?.value).toBe("Not reported");
    expect(duration?.hint).toBe("No resolved recurring loops · 1 unresolved");
  });

  it("labels zero loops as undetected when failure fingerprints are unavailable", async () => {
    const panel = document.createElement("am-rework-summary") as ReworkSummary;
    panel.analysis = {
      ...reworkFixture,
      metrics: { ...reworkFixture.metrics, recurringFailureLoops: 0, resolvedFailureLoops: 0, unresolvedFailureLoops: 0 },
      coverage: { ...reworkFixture.coverage, fingerprintedFailures: 0, uncorrelatedValidationObservations: 0 },
    };
    document.body.append(panel);
    await panel.updateComplete;

    const card = Array.from(panel.shadowRoot?.querySelectorAll<KpiCard>("am-kpi-card") ?? [])
      .find((candidate) => candidate.label === "Recurring failure loops");
    await card?.updateComplete;
    expect(card?.value).toBe("0 detected");
    expect(card?.hint).toBe("Insufficient failure evidence for fingerprints");
  });

  it("distinguishes missing rework token usage from a missing session total", async () => {
    const panel = document.createElement("am-rework-summary") as ReworkSummary;
    panel.analysis = {
      ...reworkFixture,
      metrics: {
        ...reworkFixture.metrics,
        reworkTokens: { ...reworkFixture.metrics.reworkTokens, total: null },
      },
    };
    document.body.append(panel);

    await panel.updateComplete;

    const card = Array.from(panel.shadowRoot?.querySelectorAll<KpiCard>("am-kpi-card") ?? [])
      .find((candidate) => candidate.label === "Rework token rate");
    await card?.updateComplete;
    expect(card?.value).toBe("Not reported");
    expect(card?.hint).toBe("Rework token usage unavailable");
  });

  it("isolates rework loading and retry states", async () => {
    const panel = document.createElement("am-rework-summary") as ReworkSummary;
    panel.loading = true;
    document.body.append(panel);
    await panel.updateComplete;
    expect(panel.shadowRoot?.querySelector("[role='status']")?.textContent).toContain("Analyzing");

    const retry = vi.fn();
    panel.addEventListener("rework-retry-requested", retry);
    panel.loading = false;
    panel.error = "Temporary analysis failure";
    await panel.updateComplete;
    panel.shadowRoot?.querySelector<HTMLButtonElement>("button")?.click();

    expect(panel.shadowRoot?.querySelector("[role='alert']")?.textContent).toContain("Temporary analysis failure");
    expect(retry).toHaveBeenCalledOnce();
  });

  it("renders canonical operation language instead of a source event name", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = [{
      source: "example",
      signal: "log",
      name: "vendor.response.completed",
      kind: "response",
      agentId: "main",
      runId: "run-1",
      model: "example-model",
      observedAt: "2026-08-11T00:00:00Z",
      contributesToTotal: true,
      tokens: { input: 1, output: 1, cacheRead: 0, cacheWrite: 0, reasoning: 0, total: 2 },
    }];
    document.body.append(table);

    await table.updateComplete;

    expect(table.shadowRoot?.textContent).toContain("Model call usage");
    expect(table.shadowRoot?.textContent).not.toContain("vendor.response.completed");
    expect(table.shadowRoot?.textContent).toContain("Agent");
    expect(table.shadowRoot?.textContent).toContain("—");
    expect(table.shadowRoot?.textContent).not.toContain("N/A");
    expect(table.shadowRoot?.textContent).toContain("Runtime ID: main");
    expect(table.shadowRoot?.querySelector("a.trace")).toBeNull();
  });

  it("keeps the activity DOM window bounded when thousands are resident", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = Array.from({ length: 2_000 }, (_, index) => ({
      id: `activity-${index}`,
      source: "example",
      signal: "log" as const,
      name: `activity ${index}`,
      kind: "message" as const,
      agentId: "main",
      runId: "run-1",
      model: "",
      observedAt: new Date(1_700_000_000_000 + index).toISOString(),
      contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    }));
    document.body.append(table);

    await table.updateComplete;

    expect(table.shadowRoot?.querySelectorAll("tbody tr")).toHaveLength(200);
    expect(table.shadowRoot?.textContent).toContain("Show older loaded activities");
  });

  it("keeps the same loaded activity window visible when live activities are prepended", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = Array.from({ length: 400 }, (_, index) => ({
      id: `activity-${index}`,
      source: "example", signal: "log" as const, name: `activity ${index}`, kind: "message" as const,
      agentId: "main", runId: "run-1", model: "", observedAt: new Date(1_700_000_000_000 - index).toISOString(),
      contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    }));
    document.body.append(table);
    await table.updateComplete;
    table.shadowRoot?.querySelector<HTMLButtonElement>(".continuation button")?.click();
    await table.updateComplete;
    expect(table.shadowRoot?.querySelector("tbody tr")?.getAttribute("data-activity-id")).toBe("activity-200");

    table.activities = [
      ...Array.from({ length: 5 }, (_, index) => ({ ...table.activities[0], id: `new-${index}`, name: `new ${index}` })),
      ...table.activities,
    ];
    await table.updateComplete;

    expect(table.shadowRoot?.querySelector("tbody tr")?.getAttribute("data-activity-id")).toBe("activity-200");
  });

  it("keeps the first visible activity at the same viewport position during a live refresh", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = Array.from({ length: 20 }, (_, index) => ({
      id: `activity-${index}`,
      source: "example", signal: "log" as const, name: `activity ${index}`, kind: "message" as const,
      agentId: "main", runId: "run-1", model: "", observedAt: new Date(1_700_000_000_000 - index).toISOString(),
      contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    }));
    document.body.append(table);
    await table.updateComplete;
    let anchorReads = 0;
    const rect = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      const index = Number(this.dataset.activityIndex ?? -1);
      const top = this.dataset.activityId === "activity-5" ? (++anchorReads >= 2 ? 30 : -10) : index < 5 ? -100 : 100;
      return { x: 0, y: top, top, bottom: top + 20, left: 0, right: 100, width: 100, height: 20, toJSON: () => ({}) };
    });
    const scrollBy = vi.spyOn(window, "scrollBy").mockImplementation(() => undefined);

    table.activities = [{ ...table.activities[0], id: "new", name: "new" }, ...table.activities];
    await table.updateComplete;

    expect(scrollBy).toHaveBeenCalledWith({ top: 40, behavior: "instant" });
    rect.mockRestore();
    scrollBy.mockRestore();
  });

  it("keeps expanded message content open when live activities are prepended", async () => {
    const longContent = "expanded content ".repeat(20);
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = [{
      id: "existing",
      source: "example", signal: "log", name: "existing", kind: "message", content: longContent,
      agentId: "main", runId: "run-1", model: "", observedAt: "2026-08-11T00:00:00Z",
      contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    }];
    document.body.append(table);
    await table.updateComplete;
    const expanded = table.shadowRoot?.querySelector<HTMLDetailsElement>("details");
    if (expanded) expanded.open = true;

    table.activities = [{ ...table.activities[0], id: "new", name: "new", content: "new message" }, ...table.activities];
    await table.updateComplete;

    expect(table.shadowRoot?.querySelector<HTMLDetailsElement>('tr[data-activity-id="existing"] details')?.open).toBe(true);
  });

  it("navigates every loaded trace activity while keeping the DOM at 200 rows", async () => {
    const waterfall = document.createElement("am-trace-waterfall") as TraceWaterfall;
    waterfall.trace = {
      ...traceFixture,
      activities: Array.from({ length: 300 }, (_, index) => ({
        ...traceFixture.activities[0],
        id: `trace-activity-${index}`,
        name: `trace activity ${index}`,
        spanId: `span-${index}`,
        observedAt: new Date(Date.UTC(2026, 7, 11, 0, 0, 0, index)).toISOString(),
      })),
      activityCount: 300,
    };
    document.body.append(waterfall);
    await waterfall.updateComplete;

    expect(waterfall.shadowRoot?.querySelectorAll("details.row")).toHaveLength(200);
    waterfall.shadowRoot?.querySelector<HTMLButtonElement>(".window-nav button")?.click();
    await waterfall.updateComplete;

    expect(waterfall.shadowRoot?.querySelectorAll("details.row")).toHaveLength(100);
    expect(waterfall.shadowRoot?.textContent).toContain("trace activity 299");
    expect(waterfall.shadowRoot?.textContent).toContain("Show previous loaded trace activities");
  });

  it("emits an activity-page intent when scrolling near the end", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.hasMore = true;
    const listener = vi.fn();
    table.addEventListener("activities-needed", listener);
    document.body.append(table);
    await table.updateComplete;
    Object.defineProperties(table, {
      scrollHeight: { configurable: true, value: 1_000 },
      clientHeight: { configurable: true, value: 500 },
      scrollTop: { configurable: true, value: 200 },
    });

    table.dispatchEvent(new Event("scroll"));
    table.dispatchEvent(new Event("scroll"));

    expect(listener).toHaveBeenCalledOnce();
    expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({ direction: "older" });

    Object.defineProperty(table, "scrollTop", { configurable: true, value: 0 });
    table.dispatchEvent(new Event("scroll"));
    Object.defineProperty(table, "scrollTop", { configurable: true, value: 200 });
    table.dispatchEvent(new Event("scroll"));
    expect(listener).toHaveBeenCalledTimes(2);
  });

  it("resets the scroll-boundary latch when the conversation changes", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.hasMore = true;
    table.pagingContext = "example:conversation-a";
    const listener = vi.fn();
    table.addEventListener("activities-needed", listener);
    document.body.append(table);
    await table.updateComplete;
    Object.defineProperties(table, {
      scrollHeight: { configurable: true, value: 1_000 },
      clientHeight: { configurable: true, value: 500 },
      scrollTop: { configurable: true, value: 200 },
    });

    table.dispatchEvent(new Event("scroll"));
    table.pagingContext = "example:conversation-b";
    await table.updateComplete;
    table.dispatchEvent(new Event("scroll"));

    expect(listener).toHaveBeenCalledTimes(2);
  });

  it("does not request the opposite direction while a page is loading", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.hasEarlier = true;
    table.hasMore = true;
    table.loading = true;
    table.pageDirection = "older";
    const listener = vi.fn();
    table.addEventListener("activities-needed", listener);
    document.body.append(table);
    await table.updateComplete;

    const newer = table.shadowRoot?.querySelector("[data-paging='newer']");

    expect(newer?.querySelector("button")).toBeNull();
    expect(listener).not.toHaveBeenCalled();
  });

  it("emits a newer-page intent when scrolling near the beginning of a target window", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.hasEarlier = true;
    const listener = vi.fn();
    table.addEventListener("activities-needed", listener);
    document.body.append(table);
    await table.updateComplete;
    Object.defineProperties(table, {
      scrollHeight: { configurable: true, value: 2_000 },
      clientHeight: { configurable: true, value: 500 },
      scrollTop: { configurable: true, value: 100 },
    });

    table.dispatchEvent(new Event("scroll"));

    expect(listener).toHaveBeenCalledOnce();
    expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({ direction: "newer" });
  });

  it("offers a keyboard-accessible fallback for loading another page", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.hasMore = true;
    table.loadError = "Temporary failure";
    table.pageDirection = "older";
    const listener = vi.fn();
    table.addEventListener("activities-needed", listener);
    document.body.append(table);
    await table.updateComplete;

    const retry = table.shadowRoot?.querySelector<HTMLButtonElement>("button[data-direction='older']");
    retry?.click();

    expect(retry?.textContent).toContain("Retry");
    expect(table.shadowRoot?.querySelector("[role='alert']")?.textContent).toContain("Temporary failure");
    expect(listener).toHaveBeenCalledOnce();
  });

  it("links a reported trace identity to its reload-safe resource", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = [{
      source: "example",
      signal: "trace",
      traceId: "trace-123456789",
      spanId: "span-1",
      name: "operation",
      kind: "tool",
      agentId: "main",
      runId: "conversation-1",
      model: "",
      startedAt: "2026-08-11T00:00:00Z",
      endedAt: "2026-08-11T00:00:01Z",
      observedAt: "2026-08-11T00:00:01Z",
      contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    }];
    document.body.append(table);
    await table.updateComplete;

    const traceLink = table.shadowRoot?.querySelector<HTMLAnchorElement>("a.trace");

    expect(traceLink?.getAttribute("aria-label")).toContain("trace-123456789");
    expect(traceLink?.getAttribute("href")).toBe("/traces/trace-123456789");
  });

  it("emits the conversation context when a trace link is followed in place", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = [{
      source: "example", signal: "trace", traceId: "trace-123456789", spanId: "span-1",
      name: "operation", kind: "tool", agentId: "main", runId: "conversation-1", model: "",
      observedAt: "2026-08-11T00:00:01Z", contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    }];
    const listener = vi.fn();
    table.addEventListener("trace-selected", listener);
    document.body.append(table);
    await table.updateComplete;

    table.shadowRoot?.querySelector<HTMLAnchorElement>("a.trace")?.click();

    expect(listener).toHaveBeenCalledOnce();
    expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({
      traceId: "trace-123456789",
      sourceId: "example",
      conversationId: "conversation-1",
      spanId: "span-1",
    });
  });

  it("shows prompt and usage correlation next to a linked trace", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = [{
      source: "claude", signal: "log", name: "gen_ai.model.request", kind: "response", agentId: "main", runId: "run-1",
      model: "claude-example", observedAt: "2026-08-11T00:00:00Z", promptId: "prompt-1", usageId: "request-1",
      relatedTraceId: "trace-123456789", relatedSpanId: "span-1", contributesToTotal: true,
      tokens: { input: 100, output: 20, cacheRead: 70, cacheWrite: 4, reasoning: null, total: 120 },
    }];
    document.body.append(table);
    await table.updateComplete;

    expect(table.shadowRoot?.textContent).toContain("Prompt prompt-1");
    expect(table.shadowRoot?.textContent).toContain("Usage request-1");
    expect(table.shadowRoot?.querySelector<HTMLAnchorElement>('a[href="/traces/trace-123456789"]')).not.toBeNull();
  });

  it("reveals a highlighted target only once while adjacent pages are appended", async () => {
    const reveal = vi.fn();
    const original = HTMLElement.prototype.scrollIntoView;
    HTMLElement.prototype.scrollIntoView = reveal;
    try {
      const table = document.createElement("am-activity-table") as ActivityTable;
      table.highlightedTraceId = "trace-123";
      table.highlightedSpanId = "span-1";
      table.activities = [{
        source: "example", signal: "trace", traceId: "trace-123", spanId: "span-1",
        name: "operation", kind: "tool", agentId: "main", runId: "run-1", model: "",
        observedAt: "2026-08-11T00:00:00Z", contributesToTotal: false,
        tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
      }];
      document.body.append(table);
      await table.updateComplete;

      table.activities = [...table.activities, { ...table.activities[0], spanId: "span-2" }];
      await table.updateComplete;

      expect(reveal).toHaveBeenCalledOnce();
    } finally {
      HTMLElement.prototype.scrollIntoView = original;
    }
  });

  it("renders a KPI supplied entirely through properties", async () => {
    const card = document.createElement("am-kpi-card") as KpiCard;
    card.label = "Input tokens";
    card.value = "1,024";
    document.body.append(card);

    await card.updateComplete;

    expect(card.shadowRoot?.textContent).toContain("Input tokens");
    expect(card.shadowRoot?.textContent).toContain("1,024");
    expect(card.shadowRoot?.querySelector("button.help")).toBeNull();
  });

  it("explains a KPI through hover, click, focus, and Escape", async () => {
    const card = document.createElement("am-kpi-card") as KpiCard;
    card.label = "Rework time";
    card.value = "3.5 s";
    card.description = "Observed duration from a failed validation through its corrected retry.";
    document.body.append(card);
    await card.updateComplete;

    const help = card.shadowRoot?.querySelector<HTMLButtonElement>("button.help");
    const tooltip = card.shadowRoot?.querySelector<HTMLElement>("[role='tooltip']");
    expect(help?.getAttribute("aria-label")).toBe("Explain Rework time");
    expect(help?.getAttribute("aria-controls")).toBe(tooltip?.id);
    expect(help?.getAttribute("aria-expanded")).toBe("false");
    expect(tooltip?.getAttribute("aria-hidden")).toBe("true");

    help?.dispatchEvent(new MouseEvent("mouseenter"));
    await card.updateComplete;
    expect(help?.getAttribute("aria-expanded")).toBe("true");
    expect(tooltip?.getAttribute("aria-hidden")).toBe("false");

    help?.click();
    help?.dispatchEvent(new MouseEvent("mouseleave"));
    await card.updateComplete;
    expect(help?.getAttribute("aria-expanded")).toBe("true");

    help?.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await card.updateComplete;
    expect(help?.getAttribute("aria-expanded")).toBe("false");

    help?.dispatchEvent(new FocusEvent("focus"));
    await card.updateComplete;
    expect(help?.getAttribute("aria-expanded")).toBe("true");
    help?.dispatchEvent(new FocusEvent("blur"));
    await card.updateComplete;
    expect(help?.getAttribute("aria-expanded")).toBe("false");
  });

  it("emits a typed range intent without fetching data", async () => {
    const filter = document.createElement("am-time-range-filter") as TimeRangeFilter;
    const listener = vi.fn();
    filter.addEventListener("range-selected", listener);
    document.body.append(filter);
    await filter.updateComplete;

    const button = filter.shadowRoot?.querySelector<HTMLButtonElement>("button[data-range='1h']");
    button?.click();

    expect(listener).toHaveBeenCalledOnce();
    expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({ range: "1h" });
  });

  it("keeps authoritative plan limits separate from observed tokens", async () => {
    const limits = document.createElement("am-plan-usage") as PlanUsage;
    limits.snapshots = [{
      source: "example",
      windowId: "window",
      windowDurationMinutes: 300,
      usedPercent: 25,
      capturedAt: "2026-08-11T00:00:00Z",
      authority: "account_api",
    }];
    document.body.append(limits);

    await limits.updateComplete;

    expect(limits.shadowRoot?.textContent).toContain("25% used");
    expect(limits.shadowRoot?.textContent).toContain("75% remaining");
    expect(limits.shadowRoot?.textContent).not.toContain("tokens");
  });

  it("distinguishes an unconnected plan source from unavailable data", async () => {
    const limits = document.createElement("am-plan-usage") as PlanUsage;
    document.body.append(limits);
    await limits.updateComplete;

    expect(limits.shadowRoot?.textContent).toContain("Not connected");
    expect(limits.shadowRoot?.textContent).not.toContain("N/A");
    expect(limits.shadowRoot?.textContent).not.toContain("Unavailable");
  });

  it("groups token totals and modifiers instead of concatenating them", async () => {
    const chart = document.createElement("am-token-chart");
    (chart as { usage: object }).usage = { input: 100, output: 20, cacheRead: 60, cacheWrite: 4, reasoning: 8, total: 120 };
    document.body.append(chart);
    await (chart as { updateComplete: Promise<unknown> }).updateComplete;

    const content = chart.shadowRoot?.textContent ?? "";
    expect(content).toContain("Observed total");
    expect(content).toContain("Primary usage");
    expect(content).toContain("Modifiers and additional evidence");
    expect(content).toContain("Cache read");
  });

  it("labels missing token evidence as not reported", async () => {
    const chart = document.createElement("am-token-chart");
    const breakdown = document.createElement("am-token-breakdown");
    document.body.append(chart, breakdown);
    await Promise.all([
      (chart as { updateComplete: Promise<unknown> }).updateComplete,
      (breakdown as { updateComplete: Promise<unknown> }).updateComplete,
    ]);

    expect(chart.shadowRoot?.textContent).toContain("Not reported");
    expect(breakdown.shadowRoot?.textContent).toContain("Not reported");
    expect(`${chart.shadowRoot?.textContent}${breakdown.shadowRoot?.textContent}`).not.toContain("N/A");
  });

  it("renders agent relationships as a root-first tree", async () => {
    const tree = document.createElement("am-agent-tree") as AgentTree;
    const missing = { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null };
    tree.agents = [
      { agentId: "child", agentDefinition: "repository-review", parentAgentId: "main", agentType: "custom", model: "example-large", activityCount: 1, tokens: { input: 100, output: 20, cacheRead: 60, cacheWrite: 4, reasoning: 8, total: 120 } },
      { agentId: "main", agentType: "root", activityCount: 2, tokens: missing },
    ];
    document.body.append(tree);

    await tree.updateComplete;

    const root = tree.shadowRoot?.querySelector<HTMLElement>("[data-agent-id='main']");
    const child = tree.shadowRoot?.querySelector<HTMLElement>("[data-agent-id='child']");
    expect(root?.textContent).toContain("main");
    expect(child?.textContent).toContain("child");
    expect(child?.textContent).toContain("repository-review");
    expect(child?.textContent).toContain("example-large");
    expect(child?.textContent).toContain("Runtime ID:");
    const childTokens = child?.querySelector("am-token-breakdown");
    await (childTokens as { updateComplete?: Promise<unknown> } | null)?.updateComplete;
    expect(childTokens?.shadowRoot?.textContent).toContain("120");
    expect(childTokens?.shadowRoot?.textContent).toContain("Input");
    expect(childTokens?.shadowRoot?.textContent).toContain("Cache read");
    expect(childTokens?.shadowRoot?.textContent).toContain("Reasoning");
    expect(childTokens?.shadowRoot?.querySelector("details")?.open).toBe(false);
    expect(root?.textContent).toContain("Main agent");
    expect(Number.parseFloat(root?.style.top ?? "NaN")).toBeLessThan(Number.parseFloat(child?.style.top ?? "NaN"));
    expect(Number.parseFloat(root?.style.left ?? "NaN")).toBe(Number.parseFloat(child?.style.left ?? "NaN"));
    expect(tree.shadowRoot?.querySelectorAll(".connector")).toHaveLength(3);
  });

  it("explains when no agent relationships were reported", async () => {
    const tree = document.createElement("am-agent-tree") as AgentTree;
    document.body.append(tree);
    await tree.updateComplete;

    expect(tree.shadowRoot?.textContent).toContain("No agent relationships reported");
    expect(tree.shadowRoot?.textContent).not.toContain("N/A");
  });

  it("keeps vertical parent-child relationships in separate root lanes", () => {
    const missing = { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null };
    const agents = [
      { agentId: "root-a", activityCount: 1, tokens: missing },
      { agentId: "child-a1", parentAgentId: "root-a", activityCount: 1, tokens: missing },
      { agentId: "child-a2", parentAgentId: "root-a", activityCount: 1, tokens: missing },
      { agentId: "root-b", activityCount: 1, tokens: missing },
      { agentId: "child-b1", parentAgentId: "root-b", activityCount: 1, tokens: missing },
    ];
    const layout = layoutAgentTree(buildAgentTree(agents));
    const byID = new Map(layout.nodes.map((entry) => [entry.node.agent.agentId, entry]));
    const rootA = byID.get("root-a")!;
    const rootB = byID.get("root-b")!;
    const rootANodes = ["root-a", "child-a1", "child-a2"].map((id) => byID.get(id)!);
    const rootBNodes = ["root-b", "child-b1"].map((id) => byID.get(id)!);

    expect(rootA.centerY).toBeLessThan(byID.get("child-a1")!.centerY);
    expect(rootA.centerY).toBeLessThan(byID.get("child-a2")!.centerY);
    expect(rootB.centerY).toBeLessThan(byID.get("child-b1")!.centerY);
    expect(Math.max(...rootANodes.map((entry) => entry.centerX))).toBeLessThan(Math.min(...rootBNodes.map((entry) => entry.centerX)));

    for (let left = 0; left < layout.nodes.length; left += 1) {
      for (let right = left + 1; right < layout.nodes.length; right += 1) {
        const first = layout.nodes[left];
        const second = layout.nodes[right];
        const separated = Math.abs(first.centerX - second.centerX) >= 190 || Math.abs(first.centerY - second.centerY) >= 96;
        expect(separated, `${first.node.agent.agentId} overlaps ${second.node.agent.agentId}`).toBe(true);
      }
    }
  });

  it("keeps each hierarchy level on one horizontal lane without overlap", () => {
    const missing = { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null };
    const agents = [
      { agentId: "main", activityCount: 1, tokens: missing },
      ...Array.from({ length: 49 }, (_, index) => ({ agentId: `child-${index}`, parentAgentId: "main", activityCount: 1, tokens: missing })),
      ...Array.from({ length: 5 }, (_, index) => ({ agentId: `grandchild-${index}`, parentAgentId: `child-${index}`, activityCount: 1, tokens: missing })),
    ];
    const layout = layoutAgentTree(buildAgentTree(agents));
    const root = layout.nodes.find(({ depth }) => depth === 0)!;
    const children = layout.nodes.filter(({ depth }) => depth === 1);
    const grandchildren = layout.nodes.filter(({ depth }) => depth === 2);

    expect(root.centerX).toBe(children[0].centerX);
    expect(root.centerX).toBeLessThan(250);
    expect(new Set(children.map(({ centerY }) => centerY)).size).toBe(1);
    expect(new Set(grandchildren.map(({ centerY }) => centerY)).size).toBe(1);
    expect(children[0].centerY).toBeLessThan(grandchildren[0].centerY);
    expect(layout.width).toBeGreaterThan(10_000);
    for (let left = 0; left < layout.nodes.length; left += 1) {
      for (let right = left + 1; right < layout.nodes.length; right += 1) {
        const first = layout.nodes[left];
        const second = layout.nodes[right];
        const separated = Math.abs(first.centerX - second.centerX) >= 190 || Math.abs(first.centerY - second.centerY) >= 96;
        expect(separated, `${first.node.agent.agentId} overlaps ${second.node.agent.agentId}`).toBe(true);
      }
    }
  });

  it("emits and toggles graph-node selection", async () => {
    const tree = document.createElement("am-agent-tree") as AgentTree;
    tree.agents = [{ agentId: "main", agentDefinition: "orchestrator", activityCount: 1, tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null } }];
    const listener = vi.fn();
    tree.addEventListener("agent-selected", listener);
    document.body.append(tree);
    await tree.updateComplete;

    tree.shadowRoot?.querySelector<HTMLElement>("[data-agent-id='main']")?.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    expect(listener.mock.calls[0][0].detail).toEqual({ agentId: "main" });
    tree.selectedAgentId = "main";
    await tree.updateComplete;
    tree.shadowRoot?.querySelector<HTMLElement>("[data-agent-id='main']")?.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    expect(listener.mock.calls[1][0].detail).toEqual({ agentId: "" });
  });

  it("emits source and submitted full-text filter intents", async () => {
    vi.useFakeTimers();
    const filter = document.createElement("am-session-filter") as SessionFilter;
    filter.sources = [{ id: "claude", label: "Claude Code" }, { id: "codex", label: "Codex" }];
    const sourceListener = vi.fn();
    const searchListener = vi.fn();
    filter.addEventListener("source-selected", sourceListener);
    filter.addEventListener("search-submitted", searchListener);
    document.body.append(filter);
    await filter.updateComplete;

    const select = filter.shadowRoot?.querySelector<HTMLSelectElement>("select");
    if (select) select.value = "claude";
    select?.dispatchEvent(new Event("change", { bubbles: true }));
    const input = filter.shadowRoot?.querySelector<HTMLInputElement>("input");
    if (input) input.value = "repository review";
    input?.dispatchEvent(new InputEvent("input", { bubbles: true }));
	vi.advanceTimersByTime(250);

    expect(filter.shadowRoot?.textContent).toContain("Claude Code");
    expect(filter.shadowRoot?.textContent).toContain("Codex");
    expect(input?.placeholder).toContain("Session ID");
    expect(sourceListener).toHaveBeenCalledOnce();
    expect(sourceListener.mock.calls[0][0].detail).toEqual({ sourceId: "claude" });
    expect(searchListener.mock.calls[0][0].detail).toEqual({ search: "repository review" });
	vi.useRealTimers();
  });

  it("restores the controlled source value when options return after loading", async () => {
    const filter = document.createElement("am-session-filter") as SessionFilter;
    filter.selectedSource = "claude";
    filter.sources = [{ id: "claude", label: "Claude Code" }];
    document.body.append(filter);
    await filter.updateComplete;
    expect(filter.shadowRoot?.querySelector<HTMLSelectElement>("select")?.value).toBe("claude");

    filter.sources = [];
    await filter.updateComplete;
    expect(filter.shadowRoot?.querySelector<HTMLSelectElement>("select")?.value).toBe("");

    filter.sources = [{ id: "claude", label: "Claude Code" }];
    await filter.updateComplete;
    expect(filter.shadowRoot?.querySelector<HTMLSelectElement>("select")?.value).toBe("claude");
  });

  it("labels each session with its observed telemetry source", async () => {
    const list = document.createElement("am-session-list") as SessionList;
    const tokens = { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null };
    list.sessions = [
      { id: "session-claude", sourceId: "claude", sources: [{ id: "claude", label: "Claude Code" }], traceIds: [], startedAt: "", endedAt: "", activityCount: 1, tokens, agents: [], activities: [] },
      { id: "session-codex", sourceId: "codex", sources: [{ id: "codex", label: "Codex" }], traceIds: [], startedAt: "", endedAt: "", activityCount: 1, tokens, agents: [], activities: [] },
    ];
    document.body.append(list);
    await list.updateComplete;

    expect(list.shadowRoot?.textContent).toContain("Claude Code");
    expect(list.shadowRoot?.textContent).toContain("Codex");
    expect(list.shadowRoot?.querySelector<HTMLAnchorElement>('a[href="/conversations/claude/session-claude"]')).not.toBeNull();
  });

  it("distinguishes an empty history from an empty filtered result", async () => {
    const list = document.createElement("am-session-list") as SessionList;
    document.body.append(list);
    await list.updateComplete;
    expect(list.shadowRoot?.textContent).toContain("No conversations yet");

    list.filterActive = true;
    await list.updateComplete;
    expect(list.shadowRoot?.textContent).toContain("No matching conversations");
  });

  it("distinguishes an unavailable conversation list from an empty history", async () => {
    const list = document.createElement("am-session-list") as SessionList;
    list.unavailable = true;
    document.body.append(list);
    await list.updateComplete;

    expect(list.shadowRoot?.textContent).toContain("Conversations unavailable");
    expect(list.shadowRoot?.textContent).not.toContain("No conversations yet");
  });

  it("summarizes cross-conversation trace status and completeness", async () => {
    const summary = document.createElement("am-trace-summary") as TraceSummary;
    summary.trace = traceFixture;
    document.body.append(summary);
    await summary.updateComplete;

    const content = summary.shadowRoot?.textContent ?? "";
    expect(content).toContain("trace-123456789");
    expect(content).toContain("Error");
    expect(content).toContain("2 conversations");
    expect(content).toContain("1 missing parent");
  });

  it("lists trace conversation and agent participants without merging them", async () => {
    const participants = document.createElement("am-trace-participants") as TraceParticipants;
    participants.trace = traceFixture;
    document.body.append(participants);
    await participants.updateComplete;

    const content = participants.shadowRoot?.textContent ?? "";
    expect(content).toContain("claude");
    expect(content).toContain("conversation-a");
    expect(content).toContain("codex");
    expect(content).toContain("conversation-b");
    expect(content).toContain("repository-review");
    expect(content).toContain("model-b");
    const participantTokens = [...participants.shadowRoot?.querySelectorAll("am-token-breakdown") ?? []];
    await Promise.all(participantTokens.map((token) => (token as { updateComplete?: Promise<unknown> }).updateComplete));
    expect(participantTokens.map((token) => token.shadowRoot?.textContent).join(" ")).toContain("120");
    expect(participantTokens.map((token) => token.shadowRoot?.textContent).join(" ")).toContain("60");
  });

  it("renders spans and correlated logs in the trace waterfall", async () => {
    const waterfall = document.createElement("am-trace-waterfall") as TraceWaterfall;
    waterfall.trace = traceFixture;
    document.body.append(waterfall);
    await waterfall.updateComplete;

    expect(waterfall.shadowRoot?.querySelectorAll("[role='listitem']")).toHaveLength(3);
    expect(waterfall.shadowRoot?.textContent).toContain("root operation");
    expect(waterfall.shadowRoot?.textContent).toContain("child operation");
    expect(waterfall.shadowRoot?.textContent).toContain("Correlated log");
    expect(waterfall.shadowRoot?.textContent).toContain("Missing parent");
    expect(waterfall.shadowRoot?.querySelectorAll("details")).toHaveLength(3);
    expect(waterfall.shadowRoot?.querySelectorAll("details")[0]?.open).toBe(false);
    expect(waterfall.shadowRoot?.querySelectorAll("details")[1]?.open).toBe(true);
    const childSummary = waterfall.shadowRoot?.querySelectorAll("summary")[1]?.textContent ?? "";
    expect(childSummary).toContain("repository-review → main");
    expect(childSummary).toContain("60 tokens");
    expect(childSummary).toContain("Error");
    const childTokenBreakdown = waterfall.shadowRoot?.querySelectorAll("am-token-breakdown")[1];
    await (childTokenBreakdown as { updateComplete?: Promise<unknown> } | undefined)?.updateComplete;
    expect(childTokenBreakdown?.shadowRoot?.textContent).toContain("Input");
    expect(childTokenBreakdown?.shadowRoot?.textContent).toContain("50");
    expect(childTokenBreakdown?.shadowRoot?.textContent).toContain("Reasoning");
    expect(waterfall.shadowRoot?.textContent).toContain("Runtime agent IDreviewer");
    expect(waterfall.shadowRoot?.textContent).toContain("Agentrepository-review");
    expect(waterfall.shadowRoot?.textContent).toContain("Agent typecustom");
    expect(waterfall.shadowRoot?.textContent).toContain("StatusError");
    expect(waterfall.shadowRoot?.textContent).toContain("Started at2026-08-11T00:00:01Z");
    expect(waterfall.shadowRoot?.textContent).toContain("Ended at2026-08-11T00:00:02Z");
    const logEvidence = waterfall.shadowRoot?.querySelectorAll("details")[2]?.textContent ?? "";
    const logSummary = waterfall.shadowRoot?.querySelectorAll("summary")[2]?.textContent ?? "";
    expect(logSummary).toContain("Not reported");
    expect(logEvidence).toContain("Rollup—");
    expect(logEvidence).not.toContain("N/A");
    expect(logEvidence).not.toContain("corroborating");
    expect(waterfall.shadowRoot?.querySelector<HTMLAnchorElement>('a[href="/conversations/codex/conversation-b?traceId=trace-123456789&spanId=child"]')).not.toBeNull();
  });

  it("emits a span-qualified conversation target from trace evidence", async () => {
    const waterfall = document.createElement("am-trace-waterfall") as TraceWaterfall;
    waterfall.trace = traceFixture;
    const listener = vi.fn();
    waterfall.addEventListener("conversation-selected-from-trace", listener);
    document.body.append(waterfall);
    await waterfall.updateComplete;

    waterfall.shadowRoot?.querySelectorAll<HTMLAnchorElement>("a.conversation")[1]?.click();

    expect(listener).toHaveBeenCalledOnce();
    expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({
      sourceId: "codex",
      conversationId: "conversation-b",
      traceId: "trace-123456789",
      spanId: "child",
    });
  });

  it("uses the shared navigation location for reload-safe trace evidence links", async () => {
    const waterfall = document.createElement("am-trace-waterfall") as TraceWaterfall;
    waterfall.trace = traceFixture;
    waterfall.locationForConversation = (target) =>
      `/filtered/${encodeURIComponent(target.conversationId)}?traceId=${encodeURIComponent(target.traceId ?? "")}`;
    document.body.append(waterfall);
    await waterfall.updateComplete;

    expect(waterfall.shadowRoot?.querySelectorAll<HTMLAnchorElement>("a.conversation")[1]?.getAttribute("href")).toBe(
      "/filtered/conversation-b?traceId=trace-123456789",
    );
  });

  it("shows partial token evidence without inventing a total or topology edge", async () => {
    const waterfall = document.createElement("am-trace-waterfall") as TraceWaterfall;
    waterfall.trace = {
      ...traceFixture,
      agents: [],
      activities: [{
        ...traceFixture.activities[1],
        name: "gen_ai.tool.call",
        kind: "tool",
        toolName: "read_file",
        targetAgentId: undefined,
        targetAgentType: "planner",
        parentAgentId: "main",
        tokens: { input: 50, output: null, cacheRead: 30, cacheWrite: null, reasoning: 5, total: null },
      }],
    };
    document.body.append(waterfall);
    await waterfall.updateComplete;

    const summary = waterfall.shadowRoot?.querySelector("summary")?.textContent ?? "";
    expect(summary).toContain("repository-review → planner");
    expect(summary).toContain("read_file");
    expect(summary).toContain("Partial · input 50 · cache read 30 · reasoning 5");
    expect(summary).not.toContain("main → repository-review");
    expect(waterfall.shadowRoot?.textContent).toContain("Tool nameread_file");
  });

  it("labels duplicate usage evidence as corroborating without adding it to rollups", async () => {
    const waterfall = document.createElement("am-trace-waterfall") as TraceWaterfall;
    waterfall.trace = { ...traceFixture, activities: [{ ...traceFixture.activities[1], contributesToTotal: false }] };
    document.body.append(waterfall);
    await waterfall.updateComplete;

    expect(waterfall.shadowRoot?.querySelector("summary")?.textContent).toContain("Corroborating · 60 tokens");
    expect(waterfall.shadowRoot?.textContent).toContain("Excluded from rollup as corroborating usage evidence");
  });

  it("highlights a span targeted by conversation navigation", async () => {
    const table = document.createElement("am-activity-table") as ActivityTable;
    table.activities = traceFixture.activities;
    table.highlightedTraceId = "trace-123456789";
    table.highlightedSpanId = "child";
    document.body.append(table);
    await table.updateComplete;

    expect(table.shadowRoot?.querySelector('tr[data-highlighted="true"]')?.textContent).toContain("Agent delegation");
    expect(table.shadowRoot?.querySelectorAll('tr[data-highlighted="true"]')).toHaveLength(1);
    expect(table.shadowRoot?.querySelector('tr[aria-current="location"]')?.textContent).toContain("Agent delegation");
  });

});
