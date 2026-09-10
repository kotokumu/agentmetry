import { afterEach, describe, expect, it, vi } from "vitest";
import "./trace-catalog";
import { agentmetryClient } from "../api/agentmetry-client";
import { ProjectionTargetKind } from "../gen/agentmetry/v1/agentmetry_pb";
import { LIVE_UPDATE_EVENT } from "../controllers/live-update-controller";
import type { TraceCatalogPage } from "../model/trace-catalog";
import type { TraceCatalog } from "./trace-catalog";

const trace = (traceId: string) => ({
  traceId, startedAt: "2026-09-08T00:00:00Z", endedAt: "2026-09-08T00:00:01Z", durationMs: 1000,
  status: "ok", activityCount: 2, rootSpanCount: 1, missingParentCount: 0, conversations: [],
});

const page = (traces: readonly ReturnType<typeof trace>[], nextPageToken?: string): TraceCatalogPage => ({
  traces, nextPageToken, hasMore: Boolean(nextPageToken), appliedConditions: {},
});

const deferred = <T>() => {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((promiseResolve, promiseReject) => { resolve = promiseResolve; reject = promiseReject; });
  return { promise, resolve, reject };
};

const appendCatalog = (properties: Partial<TraceCatalog> = {}) => {
  const element = document.createElement("am-trace-catalog") as TraceCatalog;
  Object.assign(element, { active: true, ...properties });
  document.body.append(element);
  return element;
};

afterEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
});

describe("am-trace-catalog", () => {
  it("loads more pages without losing the exact trace entries", async () => {
    const list = vi.spyOn(agentmetryClient, "listTraces").mockImplementation(async (_range, _source, token, _signal, conditions) =>
      token ? { ...page([trace("trace-second")]), appliedConditions: conditions ?? {} } : { ...page([trace("trace-first")], "next"), appliedConditions: conditions ?? {} });
    const element = appendCatalog();

    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(1));
    await vi.waitFor(() => expect(element.shadowRoot?.querySelectorAll(".trace-link")).toHaveLength(1));
    element.shadowRoot?.querySelector<HTMLButtonElement>("button.more")?.click();
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(2));
    await vi.waitFor(() => expect(element.shadowRoot?.querySelectorAll(".trace-link")).toHaveLength(2));

    expect(list.mock.calls[1][2]).toBe("next");
    expect([...element.shadowRoot!.querySelectorAll<HTMLAnchorElement>(".trace-link")].map((link) => link.textContent)).toEqual(["trace-first", "trace-second"]);
  });

  it("discards a stale response after the conditions change", async () => {
    const first = deferred<TraceCatalogPage>();
    const second = deferred<TraceCatalogPage>();
    const calls: { signal?: AbortSignal; conditions: unknown }[] = [];
    const list = vi.spyOn(agentmetryClient, "listTraces").mockImplementation(async (_range, _source, _token, signal, conditions) => {
      calls.push({ signal, conditions });
      return calls.length === 1 ? first.promise : second.promise;
    });
    const element = appendCatalog();
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(1));

    element.conditions = { failureObservation: "observed" };
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(2));
    expect(calls[0].signal?.aborted).toBe(true);
    expect(calls[1].conditions).toEqual({ failureObservation: "observed" });

    second.resolve(page([trace("new-condition-trace")]));
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("new-condition-trace"));
    first.resolve(page([trace("stale-trace")]));
    await Promise.resolve();
    expect(element.shadowRoot?.textContent).not.toContain("stale-trace");
  });

  it("reloads the resident page depth on relevant live changes while ignoring unrelated changes", async () => {
    const list = vi.spyOn(agentmetryClient, "listTraces")
      .mockResolvedValueOnce(page([trace("before-live-1")], "before-next"))
      .mockResolvedValueOnce(page([trace("before-live-2")]))
      .mockResolvedValueOnce(page([trace("after-live-1")], "after-next"))
      .mockResolvedValueOnce(page([trace("after-live-2")]));
    const element = appendCatalog();
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("before-live-1"));
    element.shadowRoot?.querySelector<HTMLButtonElement>("button.more")?.click();
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("before-live-2"));

    const ignoredWaits: Promise<unknown>[] = [];
    window.dispatchEvent(new CustomEvent(LIVE_UPDATE_EVENT, { detail: {
      targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "session-1", traceId: "" }],
      resyncRequired: false, throughCursor: "cursor-1", waitUntil: (promise: Promise<unknown>) => ignoredWaits.push(promise),
    } }));
    await Promise.resolve();
    expect(list).toHaveBeenCalledTimes(2);

    const waits: Promise<unknown>[] = [];
    window.dispatchEvent(new CustomEvent(LIVE_UPDATE_EVENT, { detail: {
      targets: [], resyncRequired: true, throughCursor: "cursor-2", waitUntil: (promise: Promise<unknown>) => waits.push(promise),
    } }));
    await vi.waitFor(() => expect(waits).toHaveLength(1));
    await Promise.all(waits);
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("after-live-2"));
    expect(element.shadowRoot?.textContent).not.toContain("before-live");
    expect(list).toHaveBeenCalledTimes(4);
    expect(list.mock.calls[2][2]).toBe("");
    expect(list.mock.calls[3][2]).toBe("after-next");
  });

  it("lets compatible in-flight work finish while inactive and reuses it on activation", async () => {
    const first = deferred<TraceCatalogPage>();
    const list = vi.spyOn(agentmetryClient, "listTraces").mockImplementationOnce(() => first.promise);
    const element = appendCatalog();
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(1));
    element.active = false;
    await element.updateComplete;
    expect((list.mock.calls[0][3] as AbortSignal).aborted).toBe(false);
    first.resolve(page([trace("resident-after-inactive")]));
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("resident-after-inactive"));

    element.active = true;
    await element.updateComplete;
    expect(list).toHaveBeenCalledTimes(1);
  });

  it("keeps completed rows and pagination while inactive without reloading on reactivation", async () => {
    const list = vi.spyOn(agentmetryClient, "listTraces").mockResolvedValue(page([trace("resident-trace")], "next"));
    const element = appendCatalog();
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("resident-trace"));

    element.active = false;
    await element.updateComplete;
    element.active = true;
    await element.updateComplete;

    expect(list).toHaveBeenCalledTimes(1);
    expect(element.shadowRoot?.textContent).toContain("resident-trace");
    expect(element.shadowRoot?.querySelector("button.more")).not.toBeNull();
  });

  it("retries a failed initial load when reactivated", async () => {
    const list = vi.spyOn(agentmetryClient, "listTraces")
      .mockRejectedValueOnce(new Error("temporary"))
      .mockResolvedValueOnce(page([trace("after-retry")]));
    const element = appendCatalog();
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("Traces unavailable"));

    element.active = false;
    await element.updateComplete;
    element.active = true;

    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("after-retry"));
    expect(list).toHaveBeenCalledTimes(2);
  });

  it("invalidates resident rows when the query changes while inactive", async () => {
    const list = vi.spyOn(agentmetryClient, "listTraces")
      .mockResolvedValueOnce(page([trace("old-query")]))
      .mockResolvedValueOnce(page([trace("new-query")]));
    const element = appendCatalog();
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("old-query"));

    element.active = false;
    await element.updateComplete;
    element.conditions = { failureObservation: "observed" };
    await element.updateComplete;
    expect(list).toHaveBeenCalledTimes(1);
    element.active = true;

    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("new-query"));
    expect(element.shadowRoot?.textContent).not.toContain("old-query");
    expect(list).toHaveBeenCalledTimes(2);
    expect(list.mock.calls[1][4]).toEqual({ failureObservation: "observed" });
  });

  it("coalesces relevant live updates while inactive and refreshes atomically on activation", async () => {
    const pending = deferred<TraceCatalogPage>();
    const list = vi.spyOn(agentmetryClient, "listTraces")
      .mockResolvedValueOnce(page([trace("resident")]))
      .mockImplementationOnce(() => pending.promise);
    const element = appendCatalog();
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("resident"));
    element.active = false;
    await element.updateComplete;

    for (const cursor of ["hidden-1", "hidden-2"]) {
      window.dispatchEvent(new CustomEvent(LIVE_UPDATE_EVENT, { detail: {
        targets: [{ kind: ProjectionTargetKind.TRACE, sourceId: "", sessionId: "", traceId: "changed" }],
        resyncRequired: false, throughCursor: cursor, waitUntil: () => undefined,
      } }));
    }
    expect(list).toHaveBeenCalledTimes(1);
    element.active = true;
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(2));
    expect(element.shadowRoot?.textContent).toContain("resident");

    pending.resolve(page([trace("fresh")]));
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("fresh"));
    expect(element.shadowRoot?.textContent).not.toContain("resident");
  });

  it("aborts disconnected work and can fetch fresh data after reconnecting", async () => {
    const first = deferred<TraceCatalogPage>();
    const list = vi.spyOn(agentmetryClient, "listTraces")
      .mockImplementationOnce(() => first.promise)
      .mockResolvedValueOnce(page([trace("after-reconnect")]));
    const element = appendCatalog();
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(1));
    document.body.removeChild(element);
    expect((list.mock.calls[0][3] as AbortSignal).aborted).toBe(true);
    first.resolve(page([trace("disconnected-stale")]));
    await Promise.resolve();

    document.body.append(element);
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(2));
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("after-reconnect"));
    expect(element.shadowRoot?.textContent).not.toContain("disconnected-stale");
  });

  it("emits the exact selected trace and rejects unsafe duration input", async () => {
    vi.spyOn(agentmetryClient, "listTraces").mockResolvedValue(page([trace("trace/exact-id")]));
    const element = appendCatalog({ locationForTrace: (traceId: string) => `/traces/${encodeURIComponent(traceId)}` });
    const selected: string[] = [];
    const conditionRequests: unknown[] = [];
    element.addEventListener("trace-catalog-selected", (event) => selected.push((event as CustomEvent<{ traceId: string }>).detail.traceId));
    element.addEventListener("trace-conditions-requested", (event) => conditionRequests.push((event as CustomEvent).detail.conditions));
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector<HTMLAnchorElement>(".trace-link")).not.toBeNull());

    const link = element.shadowRoot!.querySelector<HTMLAnchorElement>(".trace-link")!;
    expect(link.href).toContain("/traces/trace%2Fexact-id");
    link.click();
    expect(selected).toEqual(["trace/exact-id"]);

    const input = element.shadowRoot!.querySelector<HTMLInputElement>('input[data-filter="duration"]')!;
    input.value = "-1";
    input.dispatchEvent(new Event("change", { bubbles: true }));
    input.value = "1e309";
    input.dispatchEvent(new Event("change", { bubbles: true }));
    expect(conditionRequests).toEqual([]);
    expect(input.checkValidity()).toBe(false);

    input.value = "12.5";
    input.dispatchEvent(new Event("change", { bubbles: true }));
    expect(conditionRequests).toEqual([{ minDurationMs: 12.5 }]);
  });
});
