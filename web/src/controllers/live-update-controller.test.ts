import { afterEach, describe, expect, it, vi } from "vitest";
import { ProjectionTargetKind } from "../gen/agentmetry/v1/agentmetry_pb";
import type { AgentmetryClient, ProjectionChangeWindow } from "../api/agentmetry-client";
import { affectsSession, affectsSessionList, LiveUpdateController } from "./live-update-controller";

afterEach(() => vi.useRealTimers());

describe("LiveUpdateController", () => {
  it("forms one render window from multiple server windows and deduplicates targets", async () => {
    vi.useFakeTimers();
    const sessionTarget = { kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "s-1", traceId: "" };
    const values: ProjectionChangeWindow[] = [
      { throughCursor: "one", targets: [sessionTarget], resyncRequired: false },
      { throughCursor: "two", targets: [sessionTarget], resyncRequired: false },
    ];
    const client = {
      async *watchProjectionChanges() {
        for (const value of values) yield value;
        await new Promise(() => undefined);
      },
    } as unknown as AgentmetryClient;
    const apply = vi.fn();
    const controller = new LiveUpdateController(client, apply);

    controller.start();
    await vi.advanceTimersByTimeAsync(300);

    expect(apply).toHaveBeenCalledTimes(1);
    expect(apply.mock.calls[0]?.[0].targets).toEqual([sessionTarget]);
    controller.stop();
  });

  it("keeps session identity source-qualified", () => {
    const targets = [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "same", traceId: "" }];
    expect(affectsSession(targets, "codex", "same")).toBe(true);
    expect(affectsSession(targets, "claude", "same")).toBe(false);
  });

  it("does not refresh a source-filtered list for another source", () => {
    const targets = [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "same", traceId: "" }];
    expect(affectsSessionList(targets, "claude")).toBe(false);
    expect(affectsSessionList(targets, "codex")).toBe(true);
  });

  it("coarsens a render window instead of retaining unbounded exact targets", async () => {
	vi.useFakeTimers();
	const targets = Array.from({ length: 1100 }, (_, index) => ({ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: `s-${index}`, traceId: "" }));
	const client = { async *watchProjectionChanges() { yield { throughCursor: "one", targets, resyncRequired: false }; await new Promise(() => undefined); } } as unknown as AgentmetryClient;
	const apply = vi.fn();
	const controller = new LiveUpdateController(client, apply);
	controller.start();
	await vi.advanceTimersByTimeAsync(300);
	expect(apply.mock.calls[0]?.[0].targets).toContainEqual({ kind: ProjectionTargetKind.ALL_SESSIONS, sourceId: "", sessionId: "", traceId: "" });
	expect(apply.mock.calls[0]?.[0].targets.length).toBeLessThanOrEqual(1024);
	controller.stop();
  });

  it("retries a failed projection application before acknowledging its cursor", async () => {
    vi.useFakeTimers();
    const target = { kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "s-1", traceId: "" };
    const afterCursors: string[] = [];
    const client = {
      async *watchProjectionChanges(afterCursor: string, signal: AbortSignal) {
        afterCursors.push(afterCursor);
        yield { throughCursor: "one", targets: [target], resyncRequired: false };
        await new Promise<void>((resolve) => signal.addEventListener("abort", () => resolve(), { once: true }));
      },
    } as unknown as AgentmetryClient;
    const apply = vi.fn()
      .mockRejectedValueOnce(new Error("temporary query failure"))
      .mockResolvedValue(undefined);
    const controller = new LiveUpdateController(client, apply);

    controller.start();
    await vi.advanceTimersByTimeAsync(300);
    expect(apply).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(250);
    expect(apply).toHaveBeenCalledTimes(2);
    controller.stop();
    controller.start();
    await vi.advanceTimersByTimeAsync(0);

    expect(afterCursors).toEqual(["", "one"]);
    controller.stop();
  });
});
