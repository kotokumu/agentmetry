import { describe, expect, it } from "vitest";
import {
  conversationLocation,
  dashboardLocation,
  filtersFromLocation,
  navigationOriginFromState,
  navigationViewStateFromState,
  traceLocation,
  type NavigationFilters,
} from "./navigation";

const filtered: NavigationFilters = {
  range: "1h",
  sourceId: "codex",
  search: "tool error",
};

describe("Agentmetry navigation locations", () => {
  it("keeps the unfiltered dashboard URL compact", () => {
    expect(dashboardLocation({ range: "24h", sourceId: "", search: "" })).toBe("/");
  });

  it("round-trips list filters through a shareable URL", () => {
    const location = dashboardLocation(filtered);

    expect(location).toBe("/?range=1h&source=codex&q=tool+error");
    expect(filtersFromLocation(new URL(location, "http://127.0.0.1:17890"))).toEqual(filtered);
  });

  it("carries list filters into conversation and trace entries", () => {
    expect(conversationLocation({ sourceId: "codex", conversationId: "conversation/1" }, filtered)).toBe(
      "/conversations/codex/conversation%2F1?range=1h&source=codex&q=tool+error",
    );
    expect(traceLocation("trace/1", filtered)).toBe(
      "/traces/trace%2F1?range=1h&source=codex&q=tool+error",
    );
  });

  it("carries a trace span target alongside list filters", () => {
    expect(conversationLocation({
      sourceId: "codex",
      conversationId: "conversation/1",
      traceId: "trace/1",
      spanId: "span 1",
    }, filtered)).toBe(
      "/conversations/codex/conversation%2F1?range=1h&source=codex&q=tool+error&traceId=trace%2F1&spanId=span+1",
    );
  });

  it("carries an exact span into the trace URL without losing filters", () => {
    expect(traceLocation("trace/1", filtered, "span 4")).toBe(
      "/traces/trace%2F1?range=1h&source=codex&q=tool+error&spanId=span+4",
    );
  });

  it("accepts only safe same-origin navigation origins", () => {
    expect(navigationOriginFromState({ origin: {
      kind: "conversation",
      href: "/conversations/codex/conversation-1?range=1h",
      label: "Conversation conversation-1",
    } })).toEqual({
      kind: "conversation",
      href: "/conversations/codex/conversation-1?range=1h",
      label: "Conversation conversation-1",
    });
    expect(navigationOriginFromState({ origin: { kind: "trace", href: "https://example.com", label: "Trace 123" } })).toBeUndefined();
    expect(navigationOriginFromState({ origin: { kind: "trace", href: "//example.com", label: "Trace 123" } })).toBeUndefined();
  });

  it("preserves an identifiable evidence control in return state", () => {
    const evidenceFocus = { kind: "episode", traceId: "trace-1", spanId: "span-4" };
    expect(navigationViewStateFromState({ view: { evidenceFocus } })?.evidenceFocus).toEqual(evidenceFocus);
    expect(navigationViewStateFromState({ view: { evidenceFocus: { kind: "invalid", spanId: "x" } } })).toBeUndefined();
  });

  it("accepts bounded history-entry view state", () => {
    expect(navigationViewStateFromState({ view: { selectedAgentId: "reviewer", scrollY: 420 } })).toEqual({
      selectedAgentId: "reviewer",
      scrollY: 420,
    });
    expect(navigationViewStateFromState({ view: { selectedAgentId: 3, scrollY: -1 } })).toBeUndefined();
  });

  it("retains a validated trace viewport without accepting a half-open or reversed interval", () => {
    const traceInvestigation = { startedAt: "2026-09-04T00:00:00Z", endedAt: "2026-09-04T00:01:00Z", kind: "tool", errorsOnly: true, selectedSpanId: "0000000000000004" } as const;
    expect(navigationViewStateFromState({ view: { traceInvestigation } })?.traceInvestigation).toEqual(traceInvestigation);
    expect(navigationViewStateFromState({ view: { traceInvestigation: { startedAt: traceInvestigation.startedAt } } })).toBeUndefined();
    expect(navigationViewStateFromState({ view: { traceInvestigation: { startedAt: traceInvestigation.endedAt, endedAt: traceInvestigation.startedAt } } })).toBeUndefined();
  });

  it("retains investigation purpose and stable activity selection without accepting unknown purposes", () => {
    expect(navigationViewStateFromState({ view: { purpose: "rework", selectedActivityId: "trace:codex:span-4" } }))
      .toMatchObject({ purpose: "rework", selectedActivityId: "trace:codex:span-4" });
    expect(navigationViewStateFromState({ view: { purpose: "evaluation", selectedActivityId: 42 } })).toBeUndefined();
  });

  it("ignores invalid filter values from deep links", () => {
    expect(filtersFromLocation(new URL("/?range=year&source=codex&q=hello", "http://127.0.0.1:17890"))).toEqual({
      range: "24h",
      sourceId: "codex",
      search: "hello",
    });
  });
});
