import { afterEach, describe, expect, it, vi } from "vitest";
import "./dashboard-summary";
import { agentmetryClient } from "../api/agentmetry-client";
import type { DashboardSummary } from "../model/telemetry";
import type { DashboardSummary as DashboardSummaryElement } from "./dashboard-summary";
import { notReported, unavailable } from "../presentation/missing-data";

const dashboard = (overrides: Partial<DashboardSummary> = {}): DashboardSummary => ({
  sources: [], signalCounts: { traces: 100, logs: 200, metrics: 300 }, runCount: 7, agentCount: 4,
  tokens: { input: 10, output: 20, cacheRead: 30, cacheWrite: 40, reasoning: 50, total: 150 },
  recentActivity: [], planUsage: [], ...overrides,
});

const cards = (element: DashboardSummaryElement) => [...element.shadowRoot!.querySelectorAll<HTMLElement>("am-kpi-card")].map((card) => card as HTMLElement & { value: string; label: string });

afterEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
});

describe("am-dashboard-summary", () => {
  it("loads standalone usage metrics without relying on the session workspace", async () => {
    const getDashboard = vi.spyOn(agentmetryClient, "getDashboard").mockResolvedValue(dashboard());
    const element = document.createElement("am-dashboard-summary") as DashboardSummaryElement;
    Object.assign(element, { range: "7d", sourceId: "codex", conversationStatus: "loading", conversationCount: 1, activityCount: 2 });
    document.body.append(element);

    await vi.waitFor(() => expect(getDashboard).toHaveBeenCalledWith("7d", "codex", "", expect.any(AbortSignal)));
    await vi.waitFor(() => expect(cards(element)).toHaveLength(3));
    expect(cards(element).map((card) => card.value)).toEqual(["7", "4", "150"]);
  });

  it("does not replace period aggregates with small loaded-list counts", async () => {
    vi.spyOn(agentmetryClient, "getDashboard").mockResolvedValue(dashboard({ runCount: 48, agentCount: 19, tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null } }));
    const element = document.createElement("am-dashboard-summary") as DashboardSummaryElement;
    Object.assign(element, { active: true, conversationStatus: "ready", conversationCount: 2, activityCount: 3 });
    document.body.append(element);

    await vi.waitFor(() => expect(cards(element)).toHaveLength(3));
    expect(cards(element).map((card) => card.value)).toEqual(["48", "19", notReported()]);
  });

  it("does not turn a missing aggregate token total into zero", async () => {
    vi.spyOn(agentmetryClient, "getDashboard").mockResolvedValue(dashboard({ tokens: { input: 1, output: 2, cacheRead: null, cacheWrite: null, reasoning: null, total: null } }));
    const element = document.createElement("am-dashboard-summary") as DashboardSummaryElement;
    document.body.append(element);

    await vi.waitFor(() => expect(cards(element)).toHaveLength(3));
    expect(cards(element)[2].value).toBe(notReported());
    expect(cards(element)[2].value).not.toBe("0");
  });

  it("shows unavailable values when the period dashboard request fails", async () => {
    const getDashboard = vi.spyOn(agentmetryClient, "getDashboard").mockRejectedValue(new Error("server unavailable"));
    const element = document.createElement("am-dashboard-summary") as DashboardSummaryElement;
    document.body.append(element);

    await vi.waitFor(() => expect(getDashboard).toHaveBeenCalled());
    await vi.waitFor(() => expect(cards(element)).toHaveLength(3));
    expect(cards(element).map((card) => card.value)).toEqual([unavailable(), unavailable(), unavailable()]);
    expect(cards(element).map((card) => card.value)).not.toContain("Loading");
  });
});
