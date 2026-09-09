import { afterEach, describe, expect, it, vi } from "vitest";
import type { Activity } from "../model/telemetry";
import { ActivityTable } from "./activity-table";
import type { ContentEvidencePanel } from "./content-evidence";

const activity = (overrides: Partial<Activity> = {}): Activity => ({
  id: "activity-a", source: "codex", signal: "trace", traceId: "trace-a", spanId: "span-a",
  name: "received.tool_result", kind: "tool", toolName: "exec_command", content: "Received output",
  agentId: "worker", runId: "conversation-a", model: "model-a", observedAt: "2026-09-04T01:00:00Z",
  status: "Error", contributesToTotal: true,
  tokens: { input: 10, output: 0, cacheRead: null, cacheWrite: null, reasoning: null, total: 10 },
  ...overrides,
});

const selectedBody = (table: ActivityTable): string | undefined =>
  table.shadowRoot?.querySelector("#activity-detail pre.received-content")?.textContent ?? undefined;

afterEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
});

describe("activity reading", () => {
  it("shows each operation cost in the row and the selected operation detail", async () => {
    const table = new ActivityTable();
    table.activities = [
      activity({
        modelCallCost: {
          callId: "call-a", identityBasis: "usage_id", basis: "rate_card_estimate", amountMicroUsd: 3_345n,
          rateEntryId: "codex:gpt-example:2026-09-01",
        },
      }),
      activity({
        id: "activity-b", spanId: "span-b",
        modelCallCost: {
          callId: "call-b", identityBasis: "journal_evidence_fallback", basis: "unavailable", amountMicroUsd: null,
          primaryReason: "rate_not_found",
        },
      }),
      activity({ id: "activity-c", spanId: "span-c", modelCallCost: undefined }),
    ];
    document.body.append(table);
    await table.updateComplete;

    const headers = Array.from(table.shadowRoot?.querySelectorAll("thead th") ?? [], (cell) => cell.textContent?.trim());
    const costs = Array.from(table.shadowRoot?.querySelectorAll("tbody tr td:nth-child(4)") ?? [], (cell) => cell.textContent?.trim());
    expect(headers).toContain("Estimated cost");
    expect(costs).toEqual(["$0.003345", "—", "—"]);

    table.shadowRoot?.querySelector<HTMLButtonElement>('tr[data-activity-id="activity-a"] button.select-activity')?.click();
    await table.updateComplete;
    const detail = table.shadowRoot?.querySelector("#activity-detail")?.textContent ?? "";
    expect(detail).toContain("Model call ID");
    expect(detail).toContain("call-a");
    expect(detail).toContain("Call identity");
    expect(detail).toContain("usage_id");
    expect(detail).toContain("Cost basis");
    expect(detail).toContain("rate_card_estimate");
    expect(detail).toContain("Estimated cost");
    expect(detail).toContain("$0.003345");
    expect(detail).toContain("Rate entry");
    expect(detail).toContain("codex:gpt-example:2026-09-01");

    table.shadowRoot?.querySelector<HTMLButtonElement>('tr[data-activity-id="activity-b"] button.select-activity')?.click();
    await table.updateComplete;
    const unavailableDetail = table.shadowRoot?.querySelector("#activity-detail")?.textContent ?? "";
    expect(unavailableDetail).toContain("Unavailable reason");
    expect(unavailableDetail).toContain("rate_not_found");
  });

  it("shows the model reported by each activity next to its agent", async () => {
    const table = new ActivityTable();
    table.activities = [activity({ agentId: "agent-01", agentDefinition: "Reviewer", model: "GPT-6 Astra" }), activity({ id: "activity-b", agentId: "agent-02", model: "" })];
    document.body.append(table);
    await table.updateComplete;

    const cells = table.shadowRoot?.querySelectorAll("tbody tr td:nth-child(3)");
    expect(cells?.[0]?.textContent).toContain("agent-01");
    expect(cells?.[0]?.textContent).toContain("Reviewer");
    expect(cells?.[0]?.textContent).toContain("GPT-6 Astra");
    expect(cells?.[0]?.textContent?.match(/agent-01/g)).toHaveLength(1);
    expect(cells?.[1]?.textContent).toContain("agent-02");
    expect(cells?.[1]?.textContent).toContain("Not reported");
  });

  it("keeps rows compact and opens the chosen full body with its metadata", async () => {
    const longBody = `received output\n${"a-very-long-token".repeat(150)}\nfinal line`;
    const table = new ActivityTable();
    table.activities = [activity({ content: longBody }), activity({ id: "activity-b", spanId: "span-b", content: "Other body" })];
    const selected = vi.fn();
    table.addEventListener("activity-selected", selected);
    document.body.append(table);
    await table.updateComplete;

    expect(table.shadowRoot?.querySelector("tbody")?.textContent).not.toContain(longBody);
    const button = table.shadowRoot?.querySelector<HTMLButtonElement>('tr[data-activity-id="activity-a"] button.select-activity');
    expect(button).not.toBeNull();
    button!.click();
    await table.updateComplete;

    expect(table.selectedActivityId).toBe("activity-a");
    expect((selected.mock.calls[0][0] as CustomEvent).detail).toEqual({ activityId: "activity-a" });
    expect(button?.getAttribute("aria-pressed")).toBe("true");
    const detail = table.shadowRoot?.querySelector<HTMLElement>("#activity-detail");
    expect(selectedBody(table)).toBe(longBody);
    expect(detail?.textContent).toContain("codex");
    expect(detail?.textContent).toContain("conversation-a");
    expect(detail?.textContent).toContain("received.tool_result");
    expect(detail?.textContent).toContain("model-a");
    expect(detail?.textContent).toContain("Error");
    expect(detail?.textContent).not.toContain("Other body");
  });

  it("offers an exact activity to the referenced-file workspace", async () => {
    const table = new ActivityTable();
    table.activities = [activity({ id: "activity-file", content: "src/selection.ts" })];
    const requested = vi.fn();
    table.addEventListener("activity-files-requested", requested);
    document.body.append(table);
    await table.updateComplete;

    table.shadowRoot?.querySelector<HTMLButtonElement>('tr[data-activity-id="activity-file"] button.select-activity')?.click();
    await table.updateComplete;
    table.shadowRoot?.querySelector<HTMLButtonElement>("button.view-files")?.click();

    expect(requested).toHaveBeenCalledOnce();
    expect((requested.mock.calls[0][0] as CustomEvent).detail).toEqual({ activityId: "activity-file" });
  });

  it("shows each newly selected activity from the top of the detail pane", async () => {
    const table = new ActivityTable();
    table.activities = [
      activity({ id: "activity-a", content: "First body" }),
      activity({ id: "activity-b", spanId: "span-b", content: "Second body" }),
    ];
    document.body.append(table);
    await table.updateComplete;

    const detail = table.shadowRoot?.querySelector<HTMLElement>("#activity-detail");
    expect(detail).not.toBeNull();
    detail!.scrollTop = 240;

    table.shadowRoot?.querySelector<HTMLButtonElement>('tr[data-activity-id="activity-b"] button.select-activity')?.click();
    await table.updateComplete;

    expect(detail?.scrollTop).toBe(0);
    expect(table.shadowRoot?.activeElement).toBe(detail);
    expect(selectedBody(table)).toBe("Second body");
  });

  it("restores an offscreen selection and keeps its body selected during live arrivals", async () => {
    const table = new ActivityTable();
    table.selectionContext = "codex/conversation-a";
    table.pagingContext = "codex/conversation-a";
    table.activities = Array.from({ length: 300 }, (_, index) => activity({ id: `activity-${index}`, spanId: `span-${index}`, content: `Body ${index}` }));
    table.selectedActivityId = "activity-249";
    document.body.append(table);
    await table.updateComplete;

    expect(table.shadowRoot?.querySelectorAll("tbody tr")).toHaveLength(100);
    expect(table.shadowRoot?.querySelector('tr[data-selected="true"]')?.getAttribute("data-activity-id")).toBe("activity-249");
    expect(selectedBody(table)).toBe("Body 249");
    table.activities = [activity({ id: "new", content: "A new body" }), ...table.activities];
    await table.updateComplete;
    expect(table.selectedActivityId).toBe("activity-249");
    expect(selectedBody(table)).toBe("Body 249");
    expect(table.shadowRoot?.querySelector("tbody tr")?.getAttribute("data-activity-id")).toBe("activity-200");
  });

  it("keeps an unloaded selected identity and retained body explicit without substituting another body", async () => {
    const table = new ActivityTable();
    table.activities = [activity()];
    table.selectedActivityId = "activity-a";
    document.body.append(table);
    await table.updateComplete;
    table.activities = [activity({ id: "activity-b", content: "Replacement body" })];
    await table.updateComplete;

    expect(table.selectedActivityId).toBe("activity-a");
    const detail = table.shadowRoot?.querySelector("#activity-detail");
    expect(detail?.textContent).toContain("Selected activity is not in the loaded activity page");
    expect(detail?.textContent).toContain("activity-a");
    expect(selectedBody(table)).toBe("Received output");
    expect(detail?.textContent).not.toContain("Replacement body");
  });

  it("clears selection on a source or conversation change while accepting an explicit restoration", async () => {
    const table = new ActivityTable();
    table.selectionContext = "codex/conversation-a";
    table.pagingContext = "codex/conversation-a";
    table.activities = [activity()];
    table.selectedActivityId = "activity-a";
    document.body.append(table);
    await table.updateComplete;
    table.selectionContext = "claude/conversation-a";
    table.pagingContext = "claude/conversation-a";
    table.activities = [activity({ source: "claude", content: "Other source" })];
    await table.updateComplete;
    expect(table.selectedActivityId).toBe("");
    expect(selectedBody(table)).toBeUndefined();

    table.selectionContext = "codex/conversation-b";
    table.pagingContext = "codex/conversation-b";
    table.activities = [activity({ id: "restored", runId: "conversation-b", content: "Restored body" })];
    table.selectedActivityId = "restored";
    await table.updateComplete;
    expect(table.selectedActivityId).toBe("restored");
    expect(selectedBody(table)).toBe("Restored body");
  });

  it("keeps legacy activity identities distinct across sources and conversations", async () => {
    const table = new ActivityTable();
    table.activities = [
      activity({ id: undefined, source: "codex", content: "First body" }),
      activity({ id: undefined, source: "claude", content: "Second body" }),
      activity({ id: undefined, source: "claude", runId: "conversation-b", content: "Third body" }),
    ];
    document.body.append(table);
    await table.updateComplete;
    const buttons = table.shadowRoot?.querySelectorAll<HTMLButtonElement>("button.select-activity");
    buttons?.[2]?.click();
    await table.updateComplete;
    expect(selectedBody(table)).toBe("Third body");
    expect(table.shadowRoot?.querySelectorAll('button[aria-pressed="true"]')).toHaveLength(1);
  });

  it("preserves a selected body through agent filtering and restores it when the filter clears", async () => {
    const table = new ActivityTable();
    const selected = activity({ id: "selected", agentId: "reviewer", content: "Selected reviewer body" });
    const other = activity({ id: "other", agentId: "main", content: "Other agent body" });
    table.selectionContext = "codex:conversation-a";
    table.pagingContext = "codex:conversation-a:";
    table.activities = [selected, other];
    table.selectedActivityId = "selected";
    document.body.append(table);
    await table.updateComplete;

    table.pagingContext = "codex:conversation-a:main";
    table.activities = [other];
    table.retainedSelectedActivity = selected;
    table.selectedVisibility = "outside_agent_filter";
    await table.updateComplete;
    expect(table.selectedActivityId).toBe("selected");
    expect(table.shadowRoot?.querySelector("#activity-detail")?.textContent).toContain("Outside current agent filter");
    expect(selectedBody(table)).toBe("Selected reviewer body");
    expect(table.shadowRoot?.querySelector("#activity-detail")?.textContent).not.toContain("Other agent body");

    table.pagingContext = "codex:conversation-a:";
    table.activities = [selected, other];
    table.selectedVisibility = "not_loaded";
    await table.updateComplete;
    expect(table.selectedActivityId).toBe("selected");
    expect(selectedBody(table)).toBe("Selected reviewer body");
  });

  it("retains an agent-page-only selection across another agent page and back", async () => {
    const table = new ActivityTable();
    const agentOnly = activity({ id: "agent-only", agentId: "reviewer", content: "Agent-only retained body" });
    const otherAgent = activity({ id: "other-agent", agentId: "planner", content: "Planner body" });
    table.selectionContext = "codex:conversation-a";
    table.pagingContext = "codex:conversation-a:reviewer";
    table.agentFilterId = "reviewer";
    table.activities = [agentOnly];
    document.body.append(table);
    await table.updateComplete;
    table.shadowRoot?.querySelector<HTMLButtonElement>("button.select-activity")?.click();
    await table.updateComplete;

    table.pagingContext = "codex:conversation-a:planner";
    table.agentFilterId = "planner";
    table.activities = [otherAgent];
    await table.updateComplete;
    expect(table.selectedActivityId).toBe("agent-only");
    expect(table.shadowRoot?.querySelector("#activity-detail")?.textContent).toContain("Outside current agent filter");
    expect(selectedBody(table)).toBe("Agent-only retained body");
    expect(table.shadowRoot?.querySelector("#activity-detail")?.textContent).not.toContain("Planner body");

    table.pagingContext = "codex:conversation-a:reviewer";
    table.agentFilterId = "reviewer";
    table.activities = [agentOnly];
    await table.updateComplete;
    expect(table.shadowRoot?.querySelector('tr[data-selected="true"]')?.getAttribute("data-activity-id")).toBe("agent-only");
    expect(selectedBody(table)).toBe("Agent-only retained body");
  });

  it("explains an empty body and returns keyboard focus to its selected row", async () => {
    const table = new ActivityTable();
    table.activities = [activity({ content: "" })];
    document.body.append(table);
    await table.updateComplete;
    const button = table.shadowRoot?.querySelector<HTMLButtonElement>("button.select-activity");
    button?.focus();
    button?.click();
    await table.updateComplete;
    const detail = table.shadowRoot?.querySelector<HTMLElement>("#activity-detail");
    expect(table.shadowRoot?.querySelector("#activity-detail")?.textContent).toContain("No body was reported for this activity");
    expect(table.shadowRoot?.activeElement).toBe(detail);
    detail?.querySelector<HTMLButtonElement>("button.return-to-activity")?.click();
    await table.updateComplete;
    expect(table.shadowRoot?.activeElement).toBe(button);
    expect(table.selectedActivityId).toBe("activity-a");
  });

  it("opens the native highlighted evidence body without overriding later user selection", async () => {
    const table = new ActivityTable();
    table.activities = [
      activity({ id: "correlated-log", signal: "log", content: "Correlated log body" }),
      activity(),
      activity({ id: "other", spanId: "span-b", content: "Other selected body" }),
    ];
    table.highlightedTraceId = "trace-a";
    table.highlightedSpanId = "span-a";
    document.body.append(table);
    await table.updateComplete;
    expect(table.selectedActivityId).toBe("activity-a");
    expect(selectedBody(table)).toBe("Received output");
    expect(table.focusTraceEvidence("trace-a", "span-a")).toBe(true);
    expect(table.shadowRoot?.activeElement?.closest("tr")?.getAttribute("data-activity-id")).toBe("activity-a");

    table.shadowRoot?.querySelector<HTMLButtonElement>('tr[data-activity-id="other"] button.select-activity')?.click();
    await table.updateComplete;
    table.activities = [activity({ id: "new", spanId: "span-new" }), ...table.activities];
    await table.updateComplete;
    expect(table.selectedActivityId).toBe("other");
    expect(selectedBody(table)).toBe("Other selected body");
  });

  it("shows producer-redacted metadata without exposing the marker as readable content", async () => {
    const table = new ActivityTable();
    table.activities = [activity({
      content: "[REDACTED]",
      contentEvidence: { source: "codex", activityId: "activity-a", signal: "trace", kind: "prompt", evidence: "unknown", availability: "redacted", fields: ["prompt"], truncated: false, redactionReason: "producer_redacted" },
    })];
    table.selectedActivityId = "activity-a";
    document.body.append(table);
    await table.updateComplete;
    expect(table.shadowRoot?.textContent).not.toContain("[REDACTED]");
    expect(selectedBody(table)).toBeUndefined();
    const evidence = table.shadowRoot?.querySelector<ContentEvidencePanel>("am-content-evidence");
    await evidence?.updateComplete;
    expect(evidence?.shadowRoot?.textContent).toContain("Producer-redacted");
    expect(table.shadowRoot?.querySelector("#activity-detail")?.textContent).not.toContain("No body was reported");
  });

  it("summarizes explicit document references by filename and opens the structured reference view", async () => {
    const raw = JSON.stringify({ file_paths: ["AGENTS.md", "docs/runbook.md"] });
    const table = new ActivityTable();
    table.activities = [activity({
      content: raw,
      contentEvidence: { source: "claude", activityId: "activity-a", signal: "log", kind: "tool_input", evidence: "unknown", availability: "available", fields: ["tool_input"], truncated: false },
    })];
    table.selectedActivityId = "activity-a";
    document.body.append(table);
    await table.updateComplete;

    expect(table.shadowRoot?.querySelector(".preview")?.textContent).toBe("AGENTS.md · runbook.md");
    expect(table.shadowRoot?.querySelectorAll("#activity-detail .document-item")).toHaveLength(2);
    expect(selectedBody(table)).toBe(raw);
  });

  it("presents canonical backend error status consistently", async () => {
    const table = new ActivityTable();
    table.activities = [activity({ status: "error" })];
    table.selectedActivityId = "activity-a";
    document.body.append(table);
    await table.updateComplete;

    expect(table.shadowRoot?.querySelector(".status")?.textContent).toBe("Error");
    expect(table.shadowRoot?.querySelector("#activity-detail")?.textContent).toContain("StatusError");
    expect(table.shadowRoot?.querySelector("#activity-detail")?.textContent).not.toContain("Statuserror");
  });
});
