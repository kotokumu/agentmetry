import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { agentmetryClient } from "../api/agentmetry-client";
import type { TraceOverview } from "../model/trace-investigation";
import type { Activity, Trace } from "../model/telemetry";
import "./trace-overview";
import "./trace-waterfall";
import "./trace-explorer";
import type { TraceOverviewPanel } from "./trace-overview";
import type { TraceWaterfall } from "./trace-waterfall";
import type { TraceExplorer } from "./trace-explorer";

const activity = (id: string, spanId: string): Activity => ({
  id, source: "codex", signal: "trace", name: id, kind: "tool", traceId: "trace-a", spanId,
  agentId: "main", runId: "run", model: "model", observedAt: "2026-08-11T00:10:00Z",
  startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:20:00Z", content: `${id} body`,
  tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null }, contributesToTotal: true,
});
const overview: TraceOverview = {
  traceId: "trace-a", startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:20:00Z",
  totalActivities: 1200, returnedActivities: 1000, coverage: "partial",
  activities: [{ id: "long", source: "codex", signal: "trace", spanId: "long", parentSpanId: "off-page-parent", name: "long span", kind: "tool", status: "error", startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:20:00Z", missingParent: false }],
};
const trace = (activities: readonly Activity[]): Trace => ({
  traceId: "trace-a", startedAt: overview.startedAt, endedAt: overview.endedAt, status: "error", rootSpanCount: 1,
  missingParentCount: 0, conversations: [], agents: [], activities, activityOffset: 0, activityCount: activities.length, hasMore: false,
});

describe("trace investigation components", () => {
  beforeEach(() => document.body.replaceChildren());
  afterEach(() => vi.restoreAllMocks());

  it("shows partial overview scale and emits bounded keyboard-operable zoom/filter requests", async () => {
    const panel = document.createElement("am-trace-overview") as TraceOverviewPanel;
    panel.overview = overview;
    panel.investigation = { selectedSpanId: "selected" };
    panel.matchingActivities = 200;
    const listener = vi.fn();
    panel.addEventListener("trace-investigation-requested", listener);
    document.body.append(panel);
    await panel.updateComplete;

    expect(panel.shadowRoot?.textContent).toContain("1,000 of 1,200 activities");
    expect(panel.shadowRoot?.textContent).toContain("partial retained coverage");
    expect(panel.shadowRoot?.querySelectorAll('input[type="range"]')).toHaveLength(2);
    expect(panel.shadowRoot?.querySelector(".missing-parent")).toBeNull();
    panel.shadowRoot?.querySelectorAll<HTMLButtonElement>("button")[0]?.click();

    expect(listener).toHaveBeenCalledOnce();
    expect((listener.mock.calls[0][0] as CustomEvent).detail.investigation).toMatchObject({
      startedAt: "2026-08-11T00:05:00.000Z", endedAt: "2026-08-11T00:15:00.000Z", selectedSpanId: "selected",
    });
  });

  it("uses authoritative overview parent metadata instead of page absence", async () => {
    const waterfall = document.createElement("am-trace-waterfall") as TraceWaterfall;
    waterfall.trace = trace([{ ...activity("long", "long"), parentSpanId: "off-page-parent" }]);
    waterfall.overview = overview;
    document.body.append(waterfall);
    await waterfall.updateComplete;

    expect(waterfall.shadowRoot?.textContent).not.toContain("Missing parent");
    expect(waterfall.shadowRoot?.textContent).not.toContain("Parent availability unknown");
  });

  it("prefers detailed parent assessment and labels cap-excluded parent evidence unknown", async () => {
    const waterfall = document.createElement("am-trace-waterfall") as TraceWaterfall;
    const detailedPresent = { ...activity("present", "present"), parentSpanId: "parent-a", missingParent: false };
    const detailedMissing = { ...activity("missing", "missing"), parentSpanId: "parent-b", missingParent: true, status: "error" };
    const outsideOverviewCap = { ...activity("outside", "outside"), parentSpanId: "parent-c", missingParent: undefined };
    waterfall.trace = trace([detailedPresent, detailedMissing, outsideOverviewCap]);
    waterfall.overview = {
      ...overview,
      activities: [
        { ...overview.activities[0], id: "present", spanId: "present", parentSpanId: "parent-a", missingParent: true },
        { ...overview.activities[0], id: "missing", spanId: "missing", parentSpanId: "parent-b", missingParent: false },
      ],
    };
    document.body.append(waterfall);
    await waterfall.updateComplete;

    const rows = [...waterfall.shadowRoot?.querySelectorAll<HTMLElement>(".row") ?? []];
    expect(rows[0]?.textContent).not.toContain("Missing parent");
    expect(rows[1]?.textContent).toContain("Missing parent parent-b");
    expect(rows[1]?.textContent).toContain("Error");
    expect(rows[1]?.textContent).not.toContain("Statuserror");
    expect(rows[2]?.textContent).toContain("Parent availability unknown parent-c");
  });

  it("keeps selected evidence body outside the filtered rows and exposes clear/show controls", async () => {
    const waterfall = document.createElement("am-trace-waterfall") as TraceWaterfall;
    waterfall.trace = trace([activity("visible", "visible")]);
    waterfall.selectedSpanId = "selected";
    waterfall.selectedActivity = activity("selected", "selected");
    waterfall.selectedAvailability = "outside_filters";
    const clear = vi.fn();
    const show = vi.fn();
    waterfall.addEventListener("trace-selection-cleared", clear);
    waterfall.addEventListener("trace-selection-show-requested", show);
    document.body.append(waterfall);
    await waterfall.updateComplete;

    expect(waterfall.shadowRoot?.textContent).toContain("Selected evidence is outside the current filters");
    expect(waterfall.shadowRoot?.textContent).toContain("selected body");
    expect(waterfall.shadowRoot?.textContent).not.toContain("Selected evidencevisible body");
    const buttons = [...waterfall.shadowRoot?.querySelectorAll<HTMLButtonElement>(".selection-state button") ?? []];
    buttons[0]?.click();
    buttons[1]?.click();
    expect(clear).toHaveBeenCalledOnce();
    expect(show).toHaveBeenCalledOnce();
  });

  it("restores viewport, filter, and selected body as one public navigation state", async () => {
    const selected = { ...activity("selected", "selected"), content: "restored exact body" };
    const visible = { ...activity("visible", "visible"), kind: "prompt" as const };
    const state = {
      startedAt: "2026-08-11T00:05:00Z", endedAt: "2026-08-11T00:06:00Z", kind: "prompt" as const,
      errorsOnly: false, selectedSpanId: "selected",
    };
    vi.spyOn(agentmetryClient, "getTrace").mockResolvedValue(trace([selected]));
    const getTraceWindow = vi.spyOn(agentmetryClient, "getTraceWindow").mockResolvedValue({ trace: trace([visible]), matchingActivities: 1 });
    vi.spyOn(agentmetryClient, "getTraceOverview").mockResolvedValue({
      ...overview, coverage: "complete", returnedActivities: 2, totalActivities: 2,
      activities: [
        ...overview.activities,
        { id: "selected", source: "codex", signal: "trace", spanId: "selected", name: "selected", kind: "tool", status: "ok", startedAt: selected.startedAt!, endedAt: selected.endedAt!, missingParent: false },
      ],
    });
    const explorer = document.createElement("am-trace-explorer") as TraceExplorer;
    explorer.traceId = "trace-a";
    explorer.anchorSpanId = "route-anchor-that-history-overrides";
    explorer.requestedInvestigation = state;
    const changed = vi.fn();
    explorer.addEventListener("trace-view-state-changed", changed);
    document.body.append(explorer);

    await vi.waitFor(() => expect(explorer.shadowRoot?.querySelector("am-trace-waterfall")?.shadowRoot?.textContent).toContain("restored exact body"));
    expect(explorer.navigationViewState).toEqual({ traceInvestigation: state });
    expect(explorer.shadowRoot?.querySelector("am-trace-waterfall")?.shadowRoot?.textContent).toContain("outside the current filters");
    explorer.shadowRoot?.querySelector("am-trace-waterfall")?.shadowRoot?.querySelectorAll<HTMLButtonElement>(".selection-state button")[1]?.click();
    await vi.waitFor(() => expect(explorer.shadowRoot?.querySelector("am-trace-overview")?.shadowRoot?.querySelector<HTMLSelectElement>('select[aria-label="Trace activity kind"]')?.value).toBe("tool"));
    expect(getTraceWindow).toHaveBeenLastCalledWith("trace-a", expect.objectContaining({ kind: "tool" }), 0, 100, "", expect.any(AbortSignal));
    explorer.shadowRoot?.querySelector("am-trace-overview")?.dispatchEvent(new CustomEvent("trace-investigation-requested", {
      detail: { investigation: { ...explorer.navigationViewState.traceInvestigation, errorsOnly: true } }, bubbles: true, composed: true,
    }));

    expect(changed).toHaveBeenCalledTimes(2);
    expect((changed.mock.calls[1][0] as CustomEvent).detail).toEqual({ traceInvestigation: { ...explorer.navigationViewState.traceInvestigation, errorsOnly: true } });
  });
});
