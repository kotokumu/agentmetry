import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "./session-file-reads";
import { agentmetryClient } from "../api/agentmetry-client";
import { localization } from "../localization/localization";
import type { SessionFileRead } from "../model/trace-catalog";
import type { Activity, ContentEvidence } from "../model/telemetry";

const evidence = (availability: ContentEvidence["availability"] = "available"): ContentEvidence => ({
  source: "codex", activityId: "activity-1", signal: "log", kind: "tool_output",
  evidence: "read_output", availability, fields: ["output"], truncated: false,
});

const read = (id: string, reference: string, observedAt: string, activityId = "activity-1", mapping: SessionFileRead["outputMapping"] = "not_confirmed"): SessionFileRead => ({
  id, sourceId: "codex", sessionId: "session-files", reference, activityId, observedAt,
  agentId: "agent-01", model: "GPT-6 Luna", outputContent: `const selected = ${id};`,
  outputAvailability: "available", outputMapping: mapping, coverage: "complete", contentEvidence: evidence(),
});

beforeEach(async () => {
  await localization.select("en");
});

afterEach(async () => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
  await localization.select("en");
});

describe("am-session-file-reads", () => {
  it("groups candidates by reference and switches reading time inside the selected file", async () => {
    const reads = [
      read("read-selection-2", "src/selection.ts", "2026-09-08T00:02:00Z", "activity-2", "confirmed"),
      read("read-selection-1", "src/selection.ts", "2026-09-08T00:01:00Z", "activity-1", "confirmed"),
      read("read-other-1", "README.md", "2026-09-08T00:00:00Z", "activity-other", "confirmed"),
    ];
    const list = vi.spyOn(agentmetryClient, "listSessionFileReads").mockImplementation(async (_source, _session, _pageToken, reference) => ({
      reads: reference ? reads.filter((item) => item.reference === reference) : reads,
      distinctReferenceCount: 2, hasMore: false, coverage: "complete",
    }));
    const selected: { readId?: string; activityId?: string } = {};
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    element.sourceId = "codex";
    element.sessionId = "session-files";
    element.active = true;
    element.addEventListener("file-read-selected", (event) => Object.assign(selected, (event as CustomEvent).detail));
    document.body.append(element);

    await vi.waitFor(() => expect(list).toHaveBeenCalledWith("codex", "session-files", "", "", expect.any(AbortSignal)));
    await vi.waitFor(() => expect(element.shadowRoot?.querySelectorAll(".file-list button")).toHaveLength(2));
    const fileButton = [...(element.shadowRoot?.querySelectorAll<HTMLButtonElement>(".file-list button") ?? [])]
      .find((button) => button.textContent?.includes("src/selection.ts"));
    fileButton?.click();
    await vi.waitFor(() => expect(list).toHaveBeenLastCalledWith("codex", "session-files", "", "src/selection.ts", expect.any(AbortSignal)));
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector<HTMLSelectElement>(".read-history")).toBeTruthy());

    const history = element.shadowRoot?.querySelector<HTMLSelectElement>(".read-history select");
    expect(history?.options).toHaveLength(2);
    history!.value = "read-selection-1";
    history?.dispatchEvent(new Event("change", { bubbles: true }));
    await element.updateComplete;
    expect(selected).toEqual({ readId: "read-selection-1", activityId: "activity-1" });
    expect(element.shadowRoot?.querySelector(".read-body")?.textContent).toContain("read-selection-1");
    expect(element.shadowRoot?.querySelector(".record-evidence")).not.toBeNull();
  });

  it("remembers each reference's selected reading time without searching another reference for the requested read", async () => {
    const aLatest = read("a-latest", "src/a.ts", "2026-09-08T00:03:00Z", "activity-a-latest", "confirmed");
    const aOlder = read("a-older", "src/a.ts", "2026-09-08T00:02:00Z", "activity-a-older", "confirmed");
    const bLatest = read("b-latest", "src/b.ts", "2026-09-08T00:01:00Z", "activity-b-latest", "confirmed");
    const list = vi.spyOn(agentmetryClient, "listSessionFileReads").mockImplementation(async (_source, _session, pageToken, reference) => {
      if (!reference) return { reads: [aLatest, bLatest, aOlder], distinctReferenceCount: 2, hasMore: false, coverage: "complete" };
      if (reference === "src/a.ts") return { reads: [aLatest, aOlder], distinctReferenceCount: 2, hasMore: false, coverage: "complete" };
      return { reads: [bLatest], distinctReferenceCount: 2, nextPageToken: "b-next", hasMore: true, coverage: "complete" };
    });
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, { sourceId: "codex", sessionId: "session-files", active: true });
    document.body.append(element);

    await vi.waitFor(() => expect(element.shadowRoot?.querySelector<HTMLSelectElement>(".read-history select")?.value).toBe(aLatest.id));
    const history = element.shadowRoot?.querySelector<HTMLSelectElement>(".read-history select");
    history!.value = aOlder.id;
    history!.dispatchEvent(new Event("change", { bubbles: true }));
    await vi.waitFor(() => expect(history?.value).toBe(aOlder.id));
    expect((element as unknown as { selectedReadByReference: Map<string, string> }).selectedReadByReference.get("src/a.ts")).toBe(aOlder.id);
    const buttons = () => [...(element.shadowRoot?.querySelectorAll<HTMLButtonElement>(".file-list button") ?? [])];
    buttons().find((button) => button.title === "src/b.ts")?.click();
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector(".file-detail h3")?.textContent).toBe("src/b.ts"));
    const bHistoryCalls = list.mock.calls.filter(([, , , reference]) => reference === "src/b.ts");
    expect(bHistoryCalls).toHaveLength(1);
    buttons().find((button) => button.title === "src/a.ts")?.click();
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector<HTMLSelectElement>(".read-history select")?.value).toBe(aOlder.id));
  });

  it("prioritizes an exact requested read over a remembered read in the same reference", async () => {
    const remembered = read("read-old", "src/selection.ts", "2026-09-08T00:01:00Z", "activity-old", "confirmed");
    const requested = read("read-new", "src/selection.ts", "2026-09-08T00:02:00Z", "activity-new", "confirmed");
    vi.spyOn(agentmetryClient, "listSessionFileReads").mockImplementation(async (_source, _session, _token, reference) => ({
      reads: reference ? [requested, remembered] : [requested, remembered],
      distinctReferenceCount: 1, hasMore: false, coverage: "complete",
    }));
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, { sourceId: "codex", sessionId: "session-files", active: true });
    document.body.append(element);

    await vi.waitFor(() => expect(element.shadowRoot?.querySelector<HTMLSelectElement>(".read-history select")?.value).toBe(requested.id));
    const history = element.shadowRoot!.querySelector<HTMLSelectElement>(".read-history select")!;
    history.value = remembered.id;
    history.dispatchEvent(new Event("change", { bubbles: true }));
    await element.updateComplete;

    element.requestedReadId = requested.id;
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector<HTMLSelectElement>(".read-history select")?.value).toBe(requested.id));
  });

  it("restores remembered history from a later page instead of selecting the latest read", async () => {
    const latest = read("read-latest", "src/selection.ts", "2026-09-08T00:03:00Z", "activity-latest", "confirmed");
    const middle = read("read-middle", "src/selection.ts", "2026-09-08T00:02:00Z", "activity-middle", "confirmed");
    const remembered = read("read-remembered", "src/selection.ts", "2026-09-08T00:01:00Z", "activity-remembered", "confirmed");
    const other = read("read-other", "README.md", "2026-09-08T00:02:00Z", "activity-other", "confirmed");
    const list = vi.spyOn(agentmetryClient, "listSessionFileReads").mockImplementation(async (_source, _session, pageToken, reference) => {
      if (!reference) return { reads: [latest, other], distinctReferenceCount: 2, hasMore: false, coverage: "complete" };
      if (reference === "README.md") return { reads: [other], distinctReferenceCount: 2, hasMore: false, coverage: "complete" };
      return pageToken === "history-next"
        ? { reads: [remembered], distinctReferenceCount: 2, hasMore: false, coverage: "complete" }
        : { reads: [latest, middle], distinctReferenceCount: 2, nextPageToken: "history-next", hasMore: true, coverage: "complete" };
    });
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, { sourceId: "codex", sessionId: "session-files", active: true });
    document.body.append(element);

    await vi.waitFor(() => expect(element.shadowRoot?.querySelector<HTMLSelectElement>(".read-history select")?.value).toBe(latest.id));
    const history = element.shadowRoot!.querySelector<HTMLSelectElement>(".read-history select")!;
    history.value = middle.id;
    history.dispatchEvent(new Event("change", { bubbles: true }));
    await element.updateComplete;
    element.shadowRoot!.querySelector<HTMLButtonElement>(".file-detail .more")!.click();
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector<HTMLSelectElement>(".read-history select")?.options).toHaveLength(3));
    const loadedHistory = element.shadowRoot!.querySelector<HTMLSelectElement>(".read-history select")!;
    loadedHistory.value = remembered.id;
    loadedHistory.dispatchEvent(new Event("change", { bubbles: true }));
    await element.updateComplete;
    const buttons = [...element.shadowRoot!.querySelectorAll<HTMLButtonElement>(".file-list button")];
    buttons.find((button) => button.title === "README.md")!.click();
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector(".file-detail h3")?.textContent).toBe("README.md"));
    buttons.find((button) => button.title === "src/selection.ts")!.click();
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector<HTMLSelectElement>(".read-history select")?.value).toBe(remembered.id));
    expect(list).toHaveBeenCalledWith("codex", "session-files", "history-next", "src/selection.ts", expect.any(AbortSignal));
  });

  it("selects file reads by an activity chosen from the log and keeps missing links explicit", async () => {
    const linked = read("read-linked", "src/selection.ts", "2026-09-08T00:08:00Z", "activity-from-log", "confirmed");
    const otherFile = read("read-other-file", "README.md", "2026-09-08T00:08:01Z", "activity-from-log", "confirmed");
    const list = vi.spyOn(agentmetryClient, "listSessionFileReads").mockImplementation(async (_source, _session, _token, reference) => ({
      reads: reference === "README.md" ? [otherFile] : reference === "src/selection.ts" ? [linked] : [linked, otherFile],
      distinctReferenceCount: 2, hasMore: false, coverage: "complete",
    }));
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, { sourceId: "codex", sessionId: "session-files", active: true, requestedActivityId: "activity-from-log" });
    document.body.append(element);

    await vi.waitFor(() => expect(element.shadowRoot?.querySelector(".file-detail h3")?.textContent).toBe("src/selection.ts"));
    expect(element.shadowRoot?.querySelector(".read-meta")?.textContent).toContain("GPT-6 Luna");
    expect(list).toHaveBeenCalledWith("codex", "session-files", "", "src/selection.ts", expect.any(AbortSignal));

    element.requestedActivityId = "activity-not-linked";
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector(".file-detail")?.textContent).toContain("No retained file read is linked to activity activity-not-linked."));
    expect(element.shadowRoot?.querySelector(".file-detail")?.textContent).not.toContain("src/selection.ts");
  });

  it("shows retained activity output when file mapping is unknown and preserves exact read selection", async () => {
    const item = { ...read("read-unknown", "src/selection.ts", "2026-09-08T00:02:00Z", "activity-exact"), coverage: "partial" as const };
    const list = vi.spyOn(agentmetryClient, "listSessionFileReads").mockResolvedValue({
      reads: [item], distinctReferenceCount: 1, hasMore: false, coverage: "partial",
    });
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, { sourceId: "codex", sessionId: "session-files", active: true, requestedReadId: "read-unknown", activityContent: {
      id: "activity-exact", source: "codex", signal: "log", name: "read_file", kind: "tool", runId: "session-files", agentId: "agent-01", model: "GPT-6 Luna", observedAt: "2026-09-08T00:02:00Z", content: "const selected = rows[0];", contentEvidence: evidence(), tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null }, contributesToTotal: false,
    } satisfies Activity });
    let opened: CustomEvent | undefined;
    element.addEventListener("file-read-open-activity", (event) => { opened = event as CustomEvent; });
    document.body.append(element);

    await vi.waitFor(() => expect(element.shadowRoot?.querySelector(".read-body")).toBeTruthy());
    expect(element.shadowRoot?.textContent).toContain("Activity input/output (individual file mapping not confirmed)");
    expect(element.shadowRoot?.querySelector(".read-body")?.textContent).toContain("const selected = rows[0];");
    expect(element.shadowRoot?.querySelector(".read-body")?.textContent).not.toContain("read-unknown");
    expect(element.shadowRoot?.textContent).toContain("Partial projected coverage");
    element.shadowRoot?.querySelector<HTMLButtonElement>(".detail-actions button")?.click();
    expect(opened?.detail).toEqual({ readId: "read-unknown", activityId: "activity-exact" });
    expect(list).toHaveBeenCalledWith("codex", "session-files", "", "", expect.any(AbortSignal));
    expect(list).toHaveBeenCalledWith("codex", "session-files", "", "src/selection.ts", expect.any(AbortSignal));
  });

  it("does not present a reference-only path as file or activity body", async () => {
    const item = read("read-reference", "src/selection.ts", "2026-09-08T00:02:30Z", "activity-reference");
    vi.spyOn(agentmetryClient, "listSessionFileReads").mockResolvedValue({ reads: [item], distinctReferenceCount: 1, hasMore: false, coverage: "complete" });
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, { sourceId: "codex", sessionId: "session-files", active: true, requestedReadId: item.id, activityContent: {
      id: "activity-reference", source: "codex", signal: "log", name: "read_file", kind: "tool", runId: "session-files", agentId: "agent-01", model: "GPT-6 Luna", observedAt: "2026-09-08T00:02:30Z", content: "src/selection.ts", contentEvidence: { ...evidence(), kind: "reference", evidence: "reference", fields: ["file_path"] }, tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null }, contributesToTotal: false,
    } satisfies Activity });
    document.body.append(element);

    await vi.waitFor(() => expect(element.shadowRoot?.querySelector(".read-meta")).toBeTruthy());
    expect(element.shadowRoot?.textContent).toContain("Reference only; the file body was not reported.");
    expect(element.shadowRoot?.querySelector(".read-body")).toBeNull();
  });

  it("loads a requested read from the second reference-history page without replacing it", async () => {
    const target = read("read-page-2", "src/selection.ts", "2026-09-08T00:03:00Z", "activity-page-2", "confirmed");
    const first = read("read-page-1", "src/selection.ts", "2026-09-08T00:01:00Z", "activity-page-1", "confirmed");
    const list = vi.spyOn(agentmetryClient, "listSessionFileReads").mockImplementation(async (_source, _session, pageToken, reference) => {
      if (!reference) return { reads: [target], distinctReferenceCount: 1, hasMore: false, coverage: "complete" };
      return pageToken === "history-next"
        ? { reads: [target], distinctReferenceCount: 1, hasMore: false, coverage: "complete" }
        : { reads: [first], distinctReferenceCount: 1, nextPageToken: "history-next", hasMore: true, coverage: "complete" };
    });
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, { sourceId: "codex", sessionId: "session-files", active: true, requestedReadId: target.id });
    document.body.append(element);

    await vi.waitFor(() => expect(element.shadowRoot?.querySelector(".read-body")).toBeTruthy());
    expect(element.shadowRoot?.querySelector(".read-body")?.textContent).toContain(target.id);
    expect(list).toHaveBeenCalledWith("codex", "session-files", "history-next", "src/selection.ts", expect.any(AbortSignal));
  });

  it("does not reuse activity content from a different identity or session", async () => {
    const item = read("read-identity", "src/selection.ts", "2026-09-08T00:04:00Z", "activity-new");
    vi.spyOn(agentmetryClient, "listSessionFileReads").mockResolvedValue({
      reads: [item], distinctReferenceCount: 1, hasMore: false, coverage: "complete",
    });
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, {
      sourceId: "codex", sessionId: "session-files", active: true, requestedReadId: item.id,
      activityContent: {
        id: "activity-old", source: "codex", signal: "log", name: "read_file", kind: "tool", runId: "other-session",
        agentId: "agent-01", model: "GPT-6 Luna", observedAt: "2026-09-08T00:03:00Z", content: "stale activity body", contentEvidence: evidence(),
        tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null }, contributesToTotal: false,
      } satisfies Activity,
    });
    document.body.append(element);

    await vi.waitFor(() => expect(element.shadowRoot?.querySelector(".read-meta")).toBeTruthy());
    expect(element.shadowRoot?.textContent).not.toContain("stale activity body");
    expect(element.shadowRoot?.textContent).not.toContain("const selected = read-identity;");
    expect(element.shadowRoot?.textContent).toContain("The exact activity output is not retained for this reading.");
  });

  it.each([
    ["redacted" as const, "Producer-redacted", "secret activity body"],
    ["not_returned" as const, "Body not requested", "returned activity body"],
  ])("keeps activity output state visible for %s evidence", async (availability, label, body) => {
    const item = read(`read-${availability}`, "src/selection.ts", "2026-09-08T00:05:00Z", "activity-state");
    vi.spyOn(agentmetryClient, "listSessionFileReads").mockResolvedValue({ reads: [item], distinctReferenceCount: 1, hasMore: false, coverage: "complete" });
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, { sourceId: "codex", sessionId: "session-files", active: true, requestedReadId: item.id, activityContent: {
      id: "activity-state", source: "codex", signal: "log", name: "read_file", kind: "tool", runId: "session-files", agentId: "agent-01", model: "GPT-6 Luna", observedAt: "2026-09-08T00:05:00Z", content: body, contentEvidence: evidence(availability), tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null }, contributesToTotal: false,
    } satisfies Activity });
    document.body.append(element);

    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain(label));
    expect(element.shadowRoot?.querySelector(".read-body")).toBeNull();
  });

  it.each(["redacted", "not_returned"] as const)("keeps %s availability ahead of reference-only evidence", async (availability) => {
    const item = read(`read-reference-${availability}`, "src/selection.ts", "2026-09-08T00:05:30Z", "activity-reference-state");
    vi.spyOn(agentmetryClient, "listSessionFileReads").mockResolvedValue({ reads: [item], distinctReferenceCount: 1, hasMore: false, coverage: "complete" });
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, { sourceId: "codex", sessionId: "session-files", active: true, requestedReadId: item.id, activityContent: {
      id: "activity-reference-state", source: "codex", signal: "log", name: "read_file", kind: "tool", runId: "session-files", agentId: "agent-01", model: "GPT-6 Luna", observedAt: "2026-09-08T00:05:30Z", content: "reference", contentEvidence: { ...evidence(availability), kind: "reference", evidence: "reference" }, tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null }, contributesToTotal: false,
    } satisfies Activity });
    document.body.append(element);

    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain(availability === "redacted" ? "Producer-redacted" : "Body not requested"));
    expect(element.shadowRoot?.textContent).not.toContain("Reference only; the file body was not reported.");
  });

  it("shows an exact activity body with a nearby truncated state and keeps technical evidence folded", async () => {
    const item = read("read-truncated", "src/selection.ts", "2026-09-08T00:06:00Z", "activity-truncated");
    vi.spyOn(agentmetryClient, "listSessionFileReads").mockResolvedValue({ reads: [item], distinctReferenceCount: 1, hasMore: false, coverage: "complete" });
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, { sourceId: "codex", sessionId: "session-files", active: true, requestedReadId: item.id, activityContent: {
      id: "activity-truncated", source: "codex", signal: "log", name: "read_file", kind: "tool", runId: "session-files", agentId: "agent-01", model: "GPT-6 Luna", observedAt: "2026-09-08T00:06:00Z", content: "truncated activity body", contentEvidence: { ...evidence(), truncated: true }, tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null }, contributesToTotal: false,
    } satisfies Activity });
    document.body.append(element);

    await vi.waitFor(() => expect(element.shadowRoot?.querySelector(".read-body")).toBeTruthy());
    const detail = element.shadowRoot?.querySelector<HTMLDetailsElement>(".record-evidence");
    expect(element.shadowRoot?.textContent).toContain("Received content is truncated.");
    expect(detail?.querySelector("am-content-evidence")).toBeTruthy();
    expect(detail?.open).toBe(false);
  });

  it("shows truncation beside confirmed available file output", async () => {
    const item = { ...read("read-confirmed-truncated", "src/selection.ts", "2026-09-08T00:06:30Z", "activity-confirmed-truncated", "confirmed"), contentEvidence: { ...evidence(), truncated: true } };
    vi.spyOn(agentmetryClient, "listSessionFileReads").mockResolvedValue({ reads: [item], distinctReferenceCount: 1, hasMore: false, coverage: "complete" });
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, { sourceId: "codex", sessionId: "session-files", active: true, requestedReadId: item.id });
    document.body.append(element);

    await vi.waitFor(() => expect(element.shadowRoot?.querySelector(".read-body")).toBeTruthy());
    expect(element.shadowRoot?.textContent).toContain("Received content is truncated.");
  });

  it("discards a delayed file response after the session identity changes", async () => {
    let resolveFirst!: (page: Awaited<ReturnType<typeof agentmetryClient.listSessionFileReads>>) => void;
    const first = new Promise<Awaited<ReturnType<typeof agentmetryClient.listSessionFileReads>>>((resolve) => { resolveFirst = resolve; });
    const newer = read("read-new-session", "src/new.ts", "2026-09-08T00:07:00Z", "activity-new-session");
    const stale = read("read-old-session", "src/old.ts", "2026-09-08T00:06:00Z", "activity-old-session");
    const list = vi.spyOn(agentmetryClient, "listSessionFileReads").mockImplementation(async (_source, session) => session === "session-old" ? first : { reads: [newer], distinctReferenceCount: 1, hasMore: false, coverage: "complete" });
    const element = document.createElement("am-session-file-reads") as import("./session-file-reads").SessionFileReads;
    Object.assign(element, { sourceId: "codex", sessionId: "session-old", active: true });
    document.body.append(element);
    await vi.waitFor(() => expect(list).toHaveBeenCalledWith("codex", "session-old", "", "", expect.any(AbortSignal)));

    element.sessionId = "session-new";
    await vi.waitFor(() => expect(list).toHaveBeenCalledWith("codex", "session-new", "", "", expect.any(AbortSignal)));
    resolveFirst({ reads: [stale], distinctReferenceCount: 1, hasMore: false, coverage: "complete" });
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("src/new.ts"));
    expect(element.shadowRoot?.textContent).not.toContain("src/old.ts");
  });
});
