import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReworkAnalysis, TokenUsage } from "../model/telemetry";
import "./rework-summary";
import type { ReworkSummary } from "./rework-summary";

const tokens: TokenUsage = { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null };
const analysis = (sessionId = "conversation-1", sourceId = "codex"): ReworkAnalysis => ({
  sourceId, sessionId, sessionTokens: tokens,
  harness: { availability: "unavailable", reason: "server_unsupported" },
  metrics: {
    validationFailures: 20, failFixRetryCycles: 0, reworkDurationMs: 0,
    totalAgentEffortMs: 0, reworkAgentEffortRate: null, reworkTokens: tokens,
    toolAttemptsWithOutcome: 20, toolFailures: 20, toolFailureRate: 1,
    apiRetryWaste: { attempts: 0, durationMs: 0, tokens }, repeatedCommands: 0, reeditedFiles: 0,
    validationAttemptsWithOutcome: 20, firstPassEligibleValidations: 5, firstPassSuccesses: 0,
    firstPassSuccessRate: 0, recurringFailureLoops: 5, repeatedFailureAttempts: 20,
    resolvedFailureLoops: 0, unresolvedFailureLoops: 5, failureResolutionDurationMs: 0,
    failureResolutionTokens: tokens,
  },
  coverage: {
    activityCoverage: "partial_page", canonicalEvents: 20, classifiedEvents: 20, knownOutcomes: 20,
    validationAttempts: 20, fingerprintedFailures: 20, identifiedValidationAttempts: 20,
    idBackedValidationAttempts: 20, mergedValidationAttempts: 0,
    uncorrelatedValidationObservations: 0, conflictingAttemptObservations: 0, ambiguousFailureAttempts: 0,
  },
  capabilities: {
    changeRevert: { state: "unavailable", reason: "Not reported" },
    crossAgentOverlap: { state: "unavailable", reason: "Not reported" },
  },
  failureEpisodes: Array.from({ length: 5 }, (_, index) => ({
    agentId: `agent-${index + 1}`, operation: "test",
    validationFingerprint: `validation-${index + 1}`, errorFingerprints: [`error-${index + 1}`],
    failureAttempts: 6 - index, resolved: false, resolutionDurationMs: 0, resolutionTokens: tokens,
    traceId: "shared-trace", spanId: `span-${index + 1}`,
  })),
});

const mount = async () => {
  const panel = document.createElement("am-rework-summary") as ReworkSummary;
  panel.analysis = analysis();
  document.body.append(panel);
  await panel.updateComplete;
  return panel;
};
const evidenceLinks = (panel: ReworkSummary) => Array.from(panel.shadowRoot!.querySelectorAll<HTMLAnchorElement>(".episode a"));
const showMore = (panel: ReworkSummary) => Array.from(panel.shadowRoot!.querySelectorAll<HTMLButtonElement>("button"))
  .find((button) => button.textContent?.includes("Show more"));

afterEach(() => document.body.replaceChildren());

describe("rework episode investigation", () => {
  it("reveals every reported episode after the first three and retains partial coverage", async () => {
    const panel = await mount();
    expect(evidenceLinks(panel)).toHaveLength(3);
    expect(panel.shadowRoot!.textContent).toContain("Partial evidence");

    const expand = showMore(panel);
    expect(expand).toBeDefined();
    expand!.click();
    await panel.updateComplete;

    expect(evidenceLinks(panel)).toHaveLength(5);
    expect(panel.shadowRoot!.textContent).toContain("validation-4");
    expect(panel.shadowRoot!.textContent).toContain("validation-5");
    expect(panel.shadowRoot!.textContent).toContain("Partial evidence");
    expect(panel.shadowRoot!.textContent).not.toContain("Shown in the analysis API");
    expect(showMore(panel)).toBeUndefined();
  });

  it("keeps expanded episodes across live updates to the same conversation", async () => {
    const panel = await mount();
    showMore(panel)!.click();
    await panel.updateComplete;

    panel.loading = true;
    await panel.updateComplete;
    panel.analysis = { ...analysis(), failureEpisodes: [...analysis().failureEpisodes].reverse() };
    panel.loading = false;
    await panel.updateComplete;

    expect(evidenceLinks(panel)).toHaveLength(5);
    expect(panel.shadowRoot!.textContent).toContain("validation-5");
  });

  it.each([
    ["another conversation", "conversation-2", "codex"],
    ["another source with the same conversation ID", "conversation-1", "claude-code"],
  ])("resets expansion for %s", async (_, sessionId, sourceId) => {
    const panel = await mount();
    showMore(panel)!.click();
    await panel.updateComplete;
    panel.analysis = analysis(sessionId, sourceId);
    await panel.updateComplete;

    expect(evidenceLinks(panel)).toHaveLength(3);
    expect(showMore(panel)).toBeDefined();
  });

  it("links to the exact episode span and dispatches its source-qualified origin", async () => {
    const panel = await mount();
    const navigate = vi.fn();
    const container = document.createElement("div");
    container.addEventListener("trace-selected", navigate);
    container.append(panel);
    document.body.append(container);
    const link = evidenceLinks(panel)[0]!;
    expect(link.getAttribute("href")).toBe("/traces/shared-trace?spanId=span-1");

    const click = new MouseEvent("click", { bubbles: true, composed: true, cancelable: true });
    link.dispatchEvent(click);

    expect(click.defaultPrevented).toBe(true);
    expect(navigate).toHaveBeenCalledOnce();
    expect((navigate.mock.calls[0]![0] as CustomEvent).detail).toEqual({
      sourceId: "codex", conversationId: "conversation-1", traceId: "shared-trace", spanId: "span-1",
      evidenceOrigin: "episode",
    });
  });

  it("uses the supplied trace location builder without dropping the span", async () => {
    const panel = await mount();
    panel.locationForTrace = (traceId, spanId) => `/traces/${traceId}?source=codex&spanId=${spanId}`;
    await panel.updateComplete;

    expect(evidenceLinks(panel)[0]!.getAttribute("href")).toBe("/traces/shared-trace?source=codex&spanId=span-1");
  });

  it.each<MouseEventInit>([{ metaKey: true }, { ctrlKey: true }, { shiftKey: true }, { altKey: true }, { button: 1 }])(
    "preserves native modified link behavior for %j", async (modifiers) => {
      const panel = await mount();
      const navigate = vi.fn();
      panel.addEventListener("trace-selected", navigate);
      const click = new MouseEvent("click", { ...modifiers, bubbles: true, composed: true, cancelable: true });
      evidenceLinks(panel)[0]!.dispatchEvent(click);

      expect(click.defaultPrevented).toBe(false);
      expect(navigate).not.toHaveBeenCalled();
    },
  );

  it("uses both trace and span identity when restoring episode focus", async () => {
    const panel = await mount();
    panel.analysis = { ...panel.analysis!, failureEpisodes: panel.analysis!.failureEpisodes.map((episode, index) => ({ ...episode, traceId: `trace-${index}`, spanId: "same-span" })) };
    await panel.updateComplete;
    expect(await panel.focusEvidence("trace-4", "same-span")).toBe(true);
    expect(panel.shadowRoot!.activeElement?.getAttribute("href")).toBe("/traces/trace-4?spanId=same-span");
  });

  it("expands and focuses an evidence origin after the first three episodes", async () => {
    const panel = await mount();

    expect(await panel.focusEvidence("shared-trace", "span-5")).toBe(true);

    const link = evidenceLinks(panel).find((candidate) => new URL(candidate.href).searchParams.get("spanId") === "span-5");
    expect(link).toBeDefined();
    expect(panel.shadowRoot!.activeElement).toBe(link);
    expect(panel.shadowRoot!.textContent).toContain("Partial evidence");
  });

  it("keeps focus and expansion unchanged when the requested episode is unavailable", async () => {
    const panel = await mount();
    const link = evidenceLinks(panel)[0]!;
    link.focus();

    expect(await panel.focusEvidence("shared-trace", "missing-span")).toBe(false);
    expect(panel.shadowRoot!.activeElement).toBe(link);
    expect(evidenceLinks(panel)).toHaveLength(3);
  });
});
