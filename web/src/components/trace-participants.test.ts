import { afterEach, describe, expect, it } from "vitest";
import type { Trace } from "../model/telemetry";
import "./trace-participants";
import type { TraceParticipants } from "./trace-participants";

const trace = (): Trace => ({
  traceId: "trace/1", startedAt: "2026-09-08T00:00:00Z", endedAt: "2026-09-08T00:01:00Z",
  status: "ok", rootSpanCount: 1, missingParentCount: 0,
  conversations: [{ sourceId: "codex", id: "session/1" }, { sourceId: "claude", id: "session/2" }],
  agents: [], activities: [], activityOffset: 0, activityCount: 0, hasMore: false,
});

afterEach(() => document.body.replaceChildren());

describe("am-trace-participants", () => {
  it("renders source-qualified participant links and emits the existing trace navigation event", async () => {
    const element = document.createElement("am-trace-participants") as TraceParticipants;
    element.trace = trace();
    element.locationForConversation = (target) => `/conversations/${encodeURIComponent(target.sourceId)}/${encodeURIComponent(target.conversationId)}`;
    const events: CustomEvent[] = [];
    element.addEventListener("conversation-selected-from-trace", (event) => events.push(event as CustomEvent));
    document.body.append(element);
    await element.updateComplete;

    const link = element.shadowRoot?.querySelector<HTMLAnchorElement>("a[data-participant]");
    expect(link?.href).toContain("/conversations/codex/session%2F1");
    expect(link?.querySelector("small")?.textContent).toBe("codex");
    expect(link?.querySelector("code")?.textContent).toBe("session/1");
    link?.click();

    expect(events).toHaveLength(1);
    expect(events[0].detail).toEqual({ sourceId: "codex", conversationId: "session/1" });
    expect(events[0].bubbles).toBe(true);
    expect(events[0].composed).toBe(true);
  });

  it("keeps participant navigation independent of the selected trace span", async () => {
    const element = document.createElement("am-trace-participants") as TraceParticipants;
    element.trace = trace();
    document.body.append(element);
    await element.updateComplete;
    const events: CustomEvent[] = [];
    element.addEventListener("conversation-selected-from-trace", (event) => events.push(event as CustomEvent));
    element.shadowRoot?.querySelector<HTMLAnchorElement>("a[data-participant]")?.click();
    expect(events[0].detail).toEqual({ sourceId: "codex", conversationId: "session/1" });
  });
});
