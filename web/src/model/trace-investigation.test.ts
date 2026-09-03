import { describe, expect, it } from "vitest";
import { parseTraceInvestigationState, traceWindowForState } from "./trace-investigation";

describe("trace investigation state", () => {
  it("restores a bounded viewport, filters, and exact selected span", () => {
    const state = parseTraceInvestigationState({
      startedAt: "2026-08-11T00:01:00.000Z",
      endedAt: "2026-08-11T00:02:00.000Z",
      kind: "tool",
      errorsOnly: true,
      selectedSpanId: "00000000000000aa",
    });

    expect(state).toEqual({
      startedAt: "2026-08-11T00:01:00.000Z",
      endedAt: "2026-08-11T00:02:00.000Z",
      kind: "tool",
      errorsOnly: true,
      selectedSpanId: "00000000000000aa",
    });
    expect(traceWindowForState(state!)).toEqual({
      startedAt: "2026-08-11T00:01:00.000Z",
      endedAt: "2026-08-11T00:02:00.000Z",
      kind: "tool",
      errorsOnly: true,
    });
  });

  it.each([
    { startedAt: "2026-08-11T00:01:00Z" },
    { endedAt: "2026-08-11T00:02:00Z" },
    { startedAt: "invalid", endedAt: "2026-08-11T00:02:00Z" },
    { startedAt: "2026-08-11T00:02:00Z", endedAt: "2026-08-11T00:01:00Z" },
    { kind: "artifact" },
    { errorsOnly: "yes" },
    { selectedSpanId: "" },
  ])("rejects invalid history state %#", (value) => {
    expect(parseTraceInvestigationState(value)).toBeUndefined();
  });

  it("accepts an instant range and strips absent defaults from the server window", () => {
    const state = parseTraceInvestigationState({
      startedAt: "2026-08-11T00:01:00Z",
      endedAt: "2026-08-11T00:01:00Z",
      errorsOnly: false,
    });

    expect(state).toEqual({
      startedAt: "2026-08-11T00:01:00Z",
      endedAt: "2026-08-11T00:01:00Z",
      errorsOnly: false,
    });
    expect(traceWindowForState(state!)).toEqual({
      startedAt: "2026-08-11T00:01:00Z",
      endedAt: "2026-08-11T00:01:00Z",
      kind: "",
      errorsOnly: false,
    });
  });
});
