import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { GetTraceOverviewResponseSchema, GetTraceWindowResponseSchema } from "../gen/agentmetry/v1/agentmetry_pb";
import { mapTraceOverview, mapTraceWindow } from "./agentmetry-client";

describe("trace investigation API mapping", () => {
  it("keeps body-free overview topology, coverage, and authoritative missing-parent metadata", () => {
    const response = create(GetTraceOverviewResponseSchema, {
      traceId: "trace-1",
      startedAt: timestampFromDate(new Date("2026-08-11T00:00:00Z")),
      endedAt: timestampFromDate(new Date("2026-08-11T00:20:00Z")),
      totalActivities: 1200n,
      returnedActivities: 1000n,
      coverage: "partial",
      activities: [{
        id: "long-span", source: "codex", signal: "trace", spanId: "span-2", parentSpanId: "not-loaded",
        name: "long operation", kind: "tool", status: "error",
        startedAt: timestampFromDate(new Date("2026-08-11T00:00:00Z")),
        endedAt: timestampFromDate(new Date("2026-08-11T00:20:00Z")), missingParent: true,
      }],
    });

    expect(mapTraceOverview(response)).toEqual({
      traceId: "trace-1", startedAt: "2026-08-11T00:00:00.000Z", endedAt: "2026-08-11T00:20:00.000Z",
      totalActivities: 1200, returnedActivities: 1000, coverage: "partial",
      activities: [{
        id: "long-span", source: "codex", signal: "trace", spanId: "span-2", parentSpanId: "not-loaded",
        name: "long operation", kind: "tool", status: "error",
        startedAt: "2026-08-11T00:00:00.000Z", endedAt: "2026-08-11T00:20:00.000Z", missingParent: true,
      }],
    });
  });

  it("maps a bounded page and rejects an empty trace payload", () => {
    const response = create(GetTraceWindowResponseSchema, {
      matchingActivities: 200n,
      trace: { traceId: "trace-1", totalActivities: 200n, page: { startOffset: 100n, hasMore: true, nextPageToken: "next" } },
    });

    expect(mapTraceWindow(response)).toMatchObject({
      matchingActivities: 200,
      trace: { traceId: "trace-1", activityOffset: 100, activityCount: 200, hasMore: true, nextPageToken: "next" },
    });
    expect(() => mapTraceWindow(create(GetTraceWindowResponseSchema))).toThrow("Trace window response was empty");
  });

  it("preserves detailed missing-parent presence for native spans only", () => {
    const response = create(GetTraceWindowResponseSchema, {
      matchingActivities: 3n,
      trace: {
        traceId: "trace-1", totalActivities: 3n,
        activities: [
          { id: "present-parent", signal: "trace", missingParent: false },
          { id: "missing-parent", signal: "trace", missingParent: true, status: "error" },
          { id: "unassessed-log", signal: "log" },
        ],
      },
    });

    expect(mapTraceWindow(response).trace.activities.map(({ missingParent }) => missingParent)).toEqual([false, true, undefined]);
    expect(mapTraceWindow(response).trace.activities[1]?.status).toBe("error");
  });
});
