import { Code, ConnectError } from "@connectrpc/connect";
import { LitElement } from "lit";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ExactTraceEvidenceUnavailableError, type AgentmetryClient } from "../api/agentmetry-client";
import { ProjectionTargetKind } from "../gen/agentmetry/v1/agentmetry_pb";
import type { TraceOverview, TraceInvestigationWindow } from "../model/trace-investigation";
import type { Activity, Trace } from "../model/telemetry";
import { TraceController } from "./trace-controller";

const activity = (id: string, spanId: string, startedAt = "2026-08-11T00:00:00Z", endedAt = startedAt): Activity => ({
  id, source: "codex", signal: "trace", name: id, kind: "tool", traceId: "trace-a", spanId,
  agentId: "main", runId: "run", model: "model", observedAt: startedAt, startedAt, endedAt,
  tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null }, contributesToTotal: true,
});
const trace = (activities: readonly Activity[], count = activities.length): Trace => ({
  traceId: "trace-a", startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:20:00Z", status: "error",
  rootSpanCount: 1, missingParentCount: 0, conversations: [], agents: [], activities, activityOffset: 0,
  activityCount: count, hasMore: false,
});
const overview = (endedAt = "2026-08-11T00:20:00Z"): TraceOverview => ({
  traceId: "trace-a", startedAt: "2026-08-11T00:00:00Z", endedAt, totalActivities: 1200,
  returnedActivities: 1200, coverage: "complete", activities: [],
});

let traceClient: AgentmetryClient;
class TraceInvestigationHost extends LitElement {
  readonly trace = new TraceController(this, traceClient);
}
if (!customElements.get("test-trace-investigation-host")) customElements.define("test-trace-investigation-host", TraceInvestigationHost);

describe("TraceController investigation windows", () => {
  beforeEach(() => { document.body.replaceChildren(); });

  it("loads body-free overview and a bounded window while retaining exact selected evidence", async () => {
    const selected = { ...activity("selected", "selected-span"), content: "exact selected body" };
    const windowActivity = activity("visible-long-span", "visible", "2026-08-11T00:00:00Z", "2026-08-11T00:20:00Z");
    const getTrace = vi.fn().mockResolvedValue(trace([selected], 1200));
    const getTraceOverview = vi.fn().mockResolvedValue(overview());
    const getTraceWindow = vi.fn().mockResolvedValue({ trace: trace([windowActivity], 1), matchingActivities: 1 });
    traceClient = { getTrace, getTraceOverview, getTraceWindow } as unknown as AgentmetryClient;
    const host = document.createElement("test-trace-investigation-host") as TraceInvestigationHost;
    document.body.append(host);

    host.trace.open("trace-a", "selected-span", {
      startedAt: "2026-08-11T00:05:00Z", endedAt: "2026-08-11T00:06:00Z", kind: "tool", errorsOnly: true,
      selectedSpanId: "selected-span",
    });
    await vi.waitFor(() => expect(host.trace.value?.activities[0]?.id).toBe("visible-long-span"));

    expect(getTrace).toHaveBeenCalledWith("trace-a", 0, 100, "", expect.any(AbortSignal), false, "selected-span");
    expect(getTraceWindow).toHaveBeenCalledWith("trace-a", {
      startedAt: "2026-08-11T00:05:00Z", endedAt: "2026-08-11T00:06:00Z", kind: "tool", errorsOnly: true,
    } satisfies TraceInvestigationWindow, 0, 100, "", expect.any(AbortSignal));
    expect(host.trace.overview?.totalActivities).toBe(1200);
    expect(host.trace.matchingActivities).toBe(1);
    expect(host.trace.selectedActivity?.content).toBe("exact selected body");
    expect(host.trace.overviewState).toBe("available");
    expect(host.trace.windowState).toBe("available");
  });

  it("refreshes the same chosen viewport during live arrival while overview extent grows", async () => {
    const windowValue: TraceInvestigationWindow = {
      startedAt: "2026-08-11T00:05:00Z", endedAt: "2026-08-11T00:06:00Z", kind: "", errorsOnly: false,
    };
    const getTraceOverview = vi.fn().mockResolvedValueOnce(overview()).mockResolvedValueOnce(overview("2026-08-11T00:30:00Z"));
    const getTraceWindow = vi.fn()
      .mockResolvedValueOnce({ trace: trace([activity("before", "before")]), matchingActivities: 1 })
      .mockResolvedValueOnce({ trace: trace([activity("after", "after")]), matchingActivities: 1 });
    traceClient = { getTrace: vi.fn(), getTraceOverview, getTraceWindow } as unknown as AgentmetryClient;
    const host = document.createElement("test-trace-investigation-host") as TraceInvestigationHost;
    document.body.append(host);
    host.trace.open("trace-a", "", { startedAt: windowValue.startedAt, endedAt: windowValue.endedAt });
    await vi.waitFor(() => expect(host.trace.value?.activities[0]?.id).toBe("before"));

    await host.trace.applyLiveUpdate({
      resyncRequired: false, throughCursor: "cursor-2",
      targets: [{ kind: ProjectionTargetKind.TRACE, sourceId: "", sessionId: "", traceId: "trace-a" }],
    });

    expect(getTraceWindow).toHaveBeenLastCalledWith("trace-a", windowValue, 0, 100, "", expect.any(AbortSignal));
    expect(host.trace.investigation).toEqual({ startedAt: windowValue.startedAt, endedAt: windowValue.endedAt });
    expect(host.trace.value?.activities[0]?.id).toBe("after");
    expect(host.trace.overview?.endedAt).toBe("2026-08-11T00:30:00Z");
  });

  it("keeps legacy trace evidence readable and exposes unsupported overview/window honestly", async () => {
    const legacy = trace([activity("legacy", "legacy")]);
    traceClient = {
      getTrace: vi.fn().mockResolvedValue(legacy),
      getTraceOverview: vi.fn().mockRejectedValue(new ConnectError("unknown rpc", Code.Unimplemented)),
      getTraceWindow: vi.fn().mockRejectedValue(new ConnectError("unknown rpc", Code.Unimplemented)),
    } as unknown as AgentmetryClient;
    const host = document.createElement("test-trace-investigation-host") as TraceInvestigationHost;
    document.body.append(host);
    host.trace.open("trace-a");

    await vi.waitFor(() => expect(host.trace.value?.activities[0]?.id).toBe("legacy"));
    expect(host.trace.overviewState).toBe("unsupported");
    expect(host.trace.windowState).toBe("unsupported");
  });

  it("keeps an ignored legacy anchor as not-loaded when both investigation RPCs are unsupported", async () => {
    const legacy = trace([activity("legacy-first-page", "other-span")], 200);
    const getTrace = vi.fn().mockImplementation(async (_traceId: string, _offset: number, _limit: number, _token: string, _signal: AbortSignal, _tail: boolean, anchor: string) => {
      if (anchor) throw new ExactTraceEvidenceUnavailableError(anchor);
      return legacy;
    });
    traceClient = {
      getTrace,
      getTraceOverview: vi.fn().mockRejectedValue(new ConnectError("unknown rpc", Code.Unimplemented)),
      getTraceWindow: vi.fn().mockRejectedValue(new ConnectError("unknown rpc", Code.Unimplemented)),
    } as unknown as AgentmetryClient;
    const host = document.createElement("test-trace-investigation-host") as TraceInvestigationHost;
    document.body.append(host);
    host.trace.open("trace-a", "selected-span", { selectedSpanId: "selected-span" });

    await vi.waitFor(() => expect(host.trace.value?.activities[0]?.id).toBe("legacy-first-page"));
    expect(getTrace).toHaveBeenLastCalledWith("trace-a", 0, 100, "", expect.any(AbortSignal));
    expect(host.trace.investigation.selectedSpanId).toBe("selected-span");
    expect(host.trace.selectedActivity).toBeUndefined();
    expect(host.trace.overviewState).toBe("unsupported");
    expect(host.trace.windowState).toBe("unsupported");
  });
});
