import { comparisonWire } from "../test-fixtures/rework-comparison";
import { mapReworkComparison } from "../api/agentmetry-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { type ReworkComparisonViewState } from "../model/rework-comparison";
import type { Session, TokenUsage } from "../model/telemetry";
import "./rework-comparison";
import type { ReworkComparison } from "./rework-comparison";

const tokens = (total: number | null): TokenUsage => ({ input: total, output: 0, cacheRead: null, cacheWrite: null, reasoning: null, total });
const session = (id: string, startedAt: string, endedAt: string): Session => ({
  id, sourceId: "codex", sources: [{ id: "codex", label: "Codex" }], traceIds: [], startedAt, endedAt,
  activityCount: 0, tokens: tokens(1_000), agents: [], activities: [],
});
const readyState = (baseline: Session, current: Session): ReworkComparisonViewState => {
  const wire = comparisonWire();
  wire.baseline!.sessionId = baseline.id;
  wire.current!.sessionId = current.id;
  const report = mapReworkComparison(wire);
  if (report.status !== "ready") throw new Error(report.reason);
  return {
    status: "ready",
    options: [{ sessionId: baseline.id, endedAt: baseline.endedAt }],
    selectedBaselineId: baseline.id,
    rows: report.rows,
    warnings: report.warnings,
    harness: report.harness,
  };
};

afterEach(() => document.body.replaceChildren());

describe("am-rework-comparison", () => {
  it("rounds a tiny raw delta only for display without a misleading sign", async () => {
    const wire = comparisonWire();
    wire.rows[0].delta = 0.04;
    const report = mapReworkComparison(wire);
    if (report.status !== "ready") throw new Error("expected ready");
    const panel = document.createElement("am-rework-comparison") as ReworkComparison;
    panel.state = { ...report, options: [], selectedBaselineId: "before" };
    document.body.append(panel);
    await panel.updateComplete;
    const change = panel.shadowRoot?.querySelector(".change");
    expect(change?.classList.contains("unchanged")).toBe(true);
    expect(change?.textContent).toContain("0.0 pp");
    expect(change?.textContent).not.toContain("+0.0");
    expect(report.rows[0].availability === "comparable" ? report.rows[0].delta : null).toBe(0.04);
  });

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
    expect(content).toContain("Reported harness fingerprint changed");
    expect(content).toContain("AGENTS v1");
    expect(content).toContain("AGENTS v2");
    expect(content).toContain("4 / 4 reported records");
    expect(panel.shadowRoot?.querySelector("table caption")?.textContent).toContain("Normalized diagnostic comparison");
    expect(panel.shadowRoot?.querySelectorAll("th[scope='col']")).toHaveLength(4);
    expect(panel.shadowRoot?.querySelectorAll("details.metric-help")).toHaveLength(6);
    const help = panel.shadowRoot?.querySelectorAll<HTMLDetailsElement>("details.metric-help")[1];
    help?.querySelector("summary")?.click();
    expect(help?.open).toBe(true);
    expect(help?.textContent).toContain("not task- or change-level first-pass success");
    const harnessHelp = panel.shadowRoot?.querySelector<HTMLDetailsElement>(".harness-context details.metric-help");
    harnessHelp?.querySelector("summary")?.click();
    expect(harnessHelp?.open).toBe(true);
    expect(harnessHelp?.textContent).toContain("does not prove effective configuration equality");
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
