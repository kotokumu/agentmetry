import { afterEach, describe, expect, it, vi } from "vitest";
import { LitElement } from "lit";
import { Code, ConnectError } from "@connectrpc/connect";
import type { AgentmetryClient, ActivityPage } from "../api/agentmetry-client";
import type { Activity, Session, TokenUsage, Trace } from "../model/telemetry";
import { ConversationsController } from "./conversations-controller";
import { TraceController } from "./trace-controller";
import type { TelemetryFilters } from "./query-filters";
import { ProjectionTargetKind } from "../gen/agentmetry/v1/agentmetry_pb";

const tokens: TokenUsage = { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null };
const activity = (name: string): Activity => ({
  source: "codex", signal: "trace", name, kind: "tool", agentId: "main", runId: "session-1", model: "model",
  observedAt: "2026-08-11T00:00:00Z", tokens, contributesToTotal: false,
});
const session = (activities: readonly Activity[] = [activity("first")]): Session => ({
  id: "session-1", sourceId: "codex", sources: [{ id: "codex", label: "Codex" }], traceIds: [],
  startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:01:00Z", activityCount: 2,
  tokens, agents: [], activities, activityOffset: 0, hasEarlier: false, hasMore: true, nextPageToken: "next",
});

let conversationsClient: AgentmetryClient;
let traceClient: AgentmetryClient;

class ConversationsHost extends LitElement {
  readonly conversations: ConversationsController;
  filters: TelemetryFilters = { range: "24h", sourceId: "", search: "" };
  active = true;
  constructor() {
    super();
    this.conversations = new ConversationsController(this, conversationsClient, () => this.filters, () => this.active);
  }
}

class TraceHost extends LitElement {
  readonly trace: TraceController;
  constructor() {
    super();
    this.trace = new TraceController(this, traceClient);
  }
}

customElements.define("test-conversations-host", ConversationsHost);
customElements.define("test-trace-host", TraceHost);

afterEach(() => document.body.replaceChildren());

describe("Lit data controllers", () => {

  it("loads rework for the selected source-qualified conversation and never exposes stale identity", async () => {
    const first = session([]);
    const second = { ...first, id: "session-2" };
    const getSessionRework = vi.fn().mockImplementation((_sourceId: string, sessionId: string) => Promise.resolve({
      sourceId: "codex", sessionId,
      metrics: { validationFailures: sessionId === "session-2" ? 2 : 1, failFixRetryCycles: 0, reworkDurationMs: 0, reworkTokens: tokens, toolAttemptsWithOutcome: 0, toolFailures: 0, toolFailureRate: null, apiRetryWaste: { attempts: 0, durationMs: 0, tokens }, repeatedCommands: 0, reeditedFiles: 0 },
      coverage: { activityCoverage: "observed_projection_complete", canonicalEvents: 1, classifiedEvents: 1, knownOutcomes: 0 },
      capabilities: { changeRevert: { state: "unavailable", reason: "needs diffs" }, crossAgentOverlap: { state: "unavailable", reason: "needs identities" } },
    }));
    const client = {
      listSessions: vi.fn().mockResolvedValue([first, second]),
      getSession: vi.fn().mockImplementation((_sourceId: string, sessionId: string) => Promise.resolve(sessionId === "session-2" ? second : first)),
      getSessionRework,
    } as unknown as AgentmetryClient;
    conversationsClient = client;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);

    await vi.waitFor(() => expect(host.conversations.rework?.sessionId).toBe("session-1"));
    host.conversations.select({ sourceId: "codex", conversationId: "session-2" });
    await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-2"));
    await vi.waitFor(() => expect(host.conversations.rework?.sessionId).toBe("session-2"));

    expect(host.conversations.rework?.metrics.validationFailures).toBe(2);
    expect(getSessionRework).toHaveBeenLastCalledWith("codex", "session-2", expect.any(AbortSignal));
  });

  it("does not expose sessions from the previous filter while reloading", async () => {
    let resolveFiltered!: (sessions: readonly Session[]) => void;
    const filtered = new Promise<readonly Session[]>((resolve) => { resolveFiltered = resolve; });
    const client = {
      listSessions: vi.fn().mockImplementation((_range: string, sourceId: string) => sourceId ? filtered : Promise.resolve([session([])])),
      getSession: vi.fn().mockResolvedValue(session()),
    } as unknown as AgentmetryClient;
    conversationsClient = client;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.conversations.sessions).toHaveLength(1));

    host.filters = { ...host.filters, sourceId: "claude" };
    host.conversations.filtersChanged();
    await host.updateComplete;
    await vi.waitFor(() => expect(host.conversations.loadingList).toBe(true));

    expect(host.conversations.sessions).toEqual([]);
    expect(host.conversations.selected).toBeUndefined();
    resolveFiltered([]);
  });

  it("restarts aborted tasks when a host is reconnected", async () => {
    const firstRequest = new Promise<readonly Session[]>(() => undefined);
    const listSessions = vi.fn()
      .mockReturnValueOnce(firstRequest)
      .mockResolvedValueOnce([session([])]);
    const client = {
      listSessions,
      getSession: vi.fn().mockResolvedValue(session()),
    } as unknown as AgentmetryClient;
    conversationsClient = client;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    await vi.waitFor(() => expect(listSessions).toHaveBeenCalledTimes(1));

    host.remove();
    document.body.append(host);

    await vi.waitFor(() => expect(host.conversations.sessions).toHaveLength(1));
    expect(listSessions).toHaveBeenCalledTimes(2);
  });

	it("retries a failed conversation when the same target is selected again", async () => {
    const getSession = vi.fn()
      .mockRejectedValueOnce(new Error("temporary"))
      .mockResolvedValueOnce(session());
    const client = {
      listSessions: vi.fn().mockResolvedValue([session([])]),
      getSession,
    } as unknown as AgentmetryClient;
    conversationsClient = client;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.conversations.conversationFailed).toBe(true));

    host.conversations.select({ sourceId: "codex", conversationId: "session-1" });

    await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-1"));
	  expect(getSession).toHaveBeenCalledTimes(2);
	});

	it("adopts the aggregated root returned for a child conversation target", async () => {
	  const root = { ...session(), id: "parent" };
	  conversationsClient = {
		listSessions: vi.fn().mockResolvedValue([]),
		getSession: vi.fn().mockResolvedValue(root),
	  } as unknown as AgentmetryClient;
	  const host = document.createElement("test-conversations-host") as ConversationsHost;
	  document.body.append(host);
	  host.conversations.select({ sourceId: "codex", conversationId: "child" });

	  await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("parent"));
	  expect(host.conversations.target).toEqual({ sourceId: "codex", conversationId: "parent" });
	});

  it("appends a conversation page while keeping the already rendered activities", async () => {
    const nextPage: ActivityPage = {
      activities: [activity("second")], total: 2, offset: 1, hasEarlier: true, hasMore: false,
    };
    const client = {
      listSessions: vi.fn().mockResolvedValue([session([])]),
      getSession: vi.fn().mockResolvedValue(session()),
      listSessionActivities: vi.fn().mockResolvedValue(nextPage),
    } as unknown as AgentmetryClient;
    conversationsClient = client;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);

    await vi.waitFor(() => expect(host.conversations.selected?.activities).toHaveLength(1));
    await host.conversations.loadActivities("older");

    expect(host.conversations.selected?.activities.map(({ name }) => name)).toEqual(["first", "second"]);
    expect(host.conversations.selected?.hasMore).toBe(false);
  });

  it("clears manual conversation pagination when disconnected", async () => {
    const pendingPage = new Promise<ActivityPage>(() => undefined);
    const client = {
      listSessions: vi.fn().mockResolvedValue([session([])]),
      getSession: vi.fn().mockResolvedValue(session()),
      listSessionActivities: vi.fn().mockReturnValue(pendingPage),
    } as unknown as AgentmetryClient;
    conversationsClient = client;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-1"));

    void host.conversations.loadActivities("older");
    expect(host.conversations.activityPage?.loading).toBe(true);
    host.remove();

    expect(host.conversations.activityPage).toBeUndefined();
  });

  it("uses an authoritative bounded session snapshot when establishing a sync cursor", async () => {
    const older = { ...activity("older"), id: "older", observedAt: "2026-08-10T23:00:00Z" };
    const current = session([{ ...activity("first"), id: "first" }, older]);
    const latest = { ...session([{ ...activity("updated"), id: "first" }, { ...activity("new"), id: "new", observedAt: "2026-08-11T00:02:00Z" }]), activityCount: 3 };
    const client = { listSessions: vi.fn().mockResolvedValue([current]), getSession: vi.fn().mockResolvedValueOnce(current).mockResolvedValueOnce(latest) } as unknown as AgentmetryClient;
    conversationsClient = client;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-1"));

    await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-1", targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "session-1", traceId: "" }] });

	expect(host.conversations.selected?.activities.map(({ name }) => name)).toEqual(["new", "updated", "older"]);
  });

  it("refreshes the selected session rework analysis after a live update", async () => {
    const current = session([{ ...activity("current"), id: "current" }]);
    const rework = (validationFailures: number) => ({
      sourceId: "codex", sessionId: "session-1",
      metrics: { validationFailures, failFixRetryCycles: 0, reworkDurationMs: 0, reworkTokens: tokens, toolAttemptsWithOutcome: 0, toolFailures: 0, toolFailureRate: null, apiRetryWaste: { attempts: 0, durationMs: 0, tokens }, repeatedCommands: 0, reeditedFiles: 0 },
      coverage: { activityCoverage: "observed_projection_complete", canonicalEvents: 1, classifiedEvents: 1, knownOutcomes: 0 },
      capabilities: { changeRevert: { state: "unavailable", reason: "needs diffs" }, crossAgentOverlap: { state: "unavailable", reason: "needs identities" } },
    });
    const getSessionRework = vi.fn().mockResolvedValueOnce(rework(1)).mockResolvedValueOnce(rework(2));
    conversationsClient = {
      listSessions: vi.fn().mockResolvedValue([current]),
      getSession: vi.fn().mockResolvedValue(current),
      getSessionRework,
    } as unknown as AgentmetryClient;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.conversations.rework?.metrics.validationFailures).toBe(1));

    await host.conversations.applyLiveUpdate({
      resyncRequired: false,
      throughCursor: "cursor-1",
      targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "session-1", traceId: "" }],
    });

    await vi.waitFor(() => expect(host.conversations.rework?.metrics.validationFailures).toBe(2));
    expect(getSessionRework).toHaveBeenCalledTimes(2);
  });

  it("keeps an agent-filtered resident page populated while establishing live sync", async () => {
    const old = { ...activity("old"), id: "old", agentId: "main", observedAt: "2026-08-11T00:00:00Z" };
    const added = { ...activity("new"), id: "new", agentId: "main", observedAt: "2026-08-11T00:02:00Z" };
    const current = { ...session([old]), agents: [{ agentId: "main", activityCount: 1, tokens }] };
    const latest = { ...session([added]), activityCount: 2, agents: [{ agentId: "main", activityCount: 2, tokens }] };
    conversationsClient = {
      listSessions: vi.fn().mockResolvedValue([current]),
      getSession: vi.fn().mockResolvedValueOnce(current).mockResolvedValueOnce(latest),
      listSessionActivities: vi.fn().mockResolvedValue({ activities: [old], total: 1, offset: 0, hasEarlier: false, hasMore: false }),
    } as unknown as AgentmetryClient;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-1"));
    host.conversations.selectAgent("main");
    await vi.waitFor(() => expect(host.conversations.agentActivityPage?.loading).toBe(false));

    await host.conversations.applyLiveUpdate({
      resyncRequired: false,
      throughCursor: "cursor-1",
      targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "session-1", traceId: "" }],
    });

    expect(host.conversations.agentActivityPage?.activities.map(({ name }) => name)).toEqual(["new", "old"]);
  });

	it("applies session REMOVE and UPSERT mutations idempotently after bootstrap", async () => {
	const old = { ...activity("old"), id: "old" };
	const current = session([old]);
	const added = { ...activity("new"), id: "new", observedAt: "2026-08-11T00:02:00Z" };
	const client = {
	  listSessions: vi.fn().mockResolvedValue([current]),
	  getSession: vi.fn().mockResolvedValue(current),
	  getSessionSummary: vi.fn().mockResolvedValue({ ...current, activityCount: 1, activities: [] }),
	  syncSessionActivities: vi.fn().mockResolvedValue({ mutations: [{ operation: "remove", activityId: "old" }, { operation: "upsert", activityId: "new", activity: added }], throughCursor: "cursor-2", resyncRequired: false }),
	} as unknown as AgentmetryClient;
	conversationsClient = client;
	const host = document.createElement("test-conversations-host") as ConversationsHost;
	document.body.append(host);
	await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-1"));
	const targets = [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "session-1", traceId: "" }];
	await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-1", targets });
	await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-2", targets });
	expect(host.conversations.selected?.activities).toEqual([added]);
	});

	it("replaces the bounded snapshot when session membership may have changed", async () => {
	  const stale = { ...activity("stale child"), id: "stale-child", runId: "old-child" };
	  const current = session([stale]);
	  const fresh = { ...session([{ ...activity("fresh root"), id: "fresh-root" }]), id: "parent", activityCount: 1 };
	  const syncSessionActivities = vi.fn().mockResolvedValue({ mutations: [], throughCursor: "cursor-2", resyncRequired: false });
	  conversationsClient = {
		listSessions: vi.fn().mockResolvedValue([current]),
		getSession: vi.fn().mockResolvedValueOnce(current).mockResolvedValueOnce(current).mockResolvedValueOnce(fresh),
		syncSessionActivities,
	  } as unknown as AgentmetryClient;
	  const host = document.createElement("test-conversations-host") as ConversationsHost;
	  document.body.append(host);
	  await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-1"));

	  await host.conversations.applyLiveUpdate({
		resyncRequired: false,
		throughCursor: "cursor-1",
		targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "session-1", traceId: "" }],
	  });
	  await host.conversations.applyLiveUpdate({
		resyncRequired: false,
		throughCursor: "cursor-2",
		targets: [{ kind: ProjectionTargetKind.ALL_SESSIONS, sourceId: "", sessionId: "", traceId: "" }],
	  });

	  expect(syncSessionActivities).not.toHaveBeenCalled();
	  expect(host.conversations.selected?.activities.map(({ id }) => id)).toEqual(["fresh-root"]);
	  expect(host.conversations.selected?.id).toBe("parent");
	  expect(host.conversations.target).toEqual({ sourceId: "codex", conversationId: "parent" });
	});

  it("replaces resident session state and adopts the server cursor when sync requires resync", async () => {
    const stale = session([{ ...activity("stale"), id: "stale" }, { ...activity("off-head stale"), id: "off-head" }]);
    const fresh = session([{ ...activity("fresh"), id: "fresh" }]);
    const syncSessionActivities = vi.fn()
      .mockResolvedValueOnce({ mutations: [], throughCursor: "reset-cursor", resyncRequired: true })
      .mockResolvedValueOnce({ mutations: [], throughCursor: "cursor-3", resyncRequired: false });
    const client = {
      listSessions: vi.fn().mockResolvedValue([stale]),
      getSession: vi.fn().mockResolvedValueOnce(stale).mockResolvedValueOnce(stale).mockResolvedValue(fresh),
      syncSessionActivities,
    } as unknown as AgentmetryClient;
    conversationsClient = client;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-1"));
    const targets = [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "session-1", traceId: "" }];

    await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-1", targets });
    await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-2", targets });
    await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-3", targets });

    expect(host.conversations.selected?.activities.map(({ id }) => id)).toEqual(["fresh"]);
    expect(syncSessionActivities).toHaveBeenNthCalledWith(2, "codex", "session-1", "reset-cursor", "cursor-3", "", expect.any(AbortSignal));
  });

  it("falls back to an authoritative session snapshot when mutation catch-up exceeds ten pages", async () => {
    const stale = session([{ ...activity("stale"), id: "stale" }]);
    const fresh = session([{ ...activity("fresh"), id: "fresh" }]);
    const syncSessionActivities = vi.fn().mockImplementation((..._args: unknown[]) => Promise.resolve({
      mutations: [], throughCursor: syncSessionActivities.mock.calls.length <= 10 ? "cursor-2" : "cursor-3",
      resyncRequired: false,
      nextPageToken: syncSessionActivities.mock.calls.length <= 10 ? `page-${syncSessionActivities.mock.calls.length}` : undefined,
    }));
    const client = {
      listSessions: vi.fn().mockResolvedValue([stale]),
      getSession: vi.fn().mockResolvedValueOnce(stale).mockResolvedValueOnce(stale).mockResolvedValue(fresh),
      syncSessionActivities,
    } as unknown as AgentmetryClient;
    conversationsClient = client;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-1"));
    const targets = [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "session-1", traceId: "" }];

    await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-1", targets });
    await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-2", targets });
    await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-3", targets });

    expect(host.conversations.selected?.activities.map(({ id }) => id)).toEqual(["fresh"]);
    expect(syncSessionActivities).toHaveBeenCalledTimes(11);
    expect(syncSessionActivities).toHaveBeenNthCalledWith(11, "codex", "session-1", "cursor-2", "cursor-3", "", expect.any(AbortSignal));
  });

  it("ignores a non-cooperative session page after a live update resets paging", async () => {
    const current = session([{ ...activity("current"), id: "current" }]);
    const fresh = session([{ ...activity("fresh"), id: "fresh" }]);
    let resolvePage!: (page: ActivityPage) => void;
    const page = new Promise<ActivityPage>((resolve) => { resolvePage = resolve; });
    const client = {
      listSessions: vi.fn().mockResolvedValue([current]),
      getSession: vi.fn().mockResolvedValueOnce(current).mockResolvedValueOnce(current).mockResolvedValue(fresh),
      listSessionActivities: vi.fn().mockReturnValue(page),
      syncSessionActivities: vi.fn().mockResolvedValue({ mutations: [], throughCursor: "cursor-2", resyncRequired: false }),
    } as unknown as AgentmetryClient;
    conversationsClient = client;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-1"));
    const targets = [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "session-1", traceId: "" }];
    await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-1", targets });

    const paging = host.conversations.loadActivities("older");
    await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-2", targets });
    resolvePage({ activities: [{ ...activity("stale-page"), id: "stale-page" }], total: 2, offset: 1, hasEarlier: true, hasMore: false });
    await paging;

    expect(host.conversations.selected?.activities.map(({ id }) => id)).toEqual(["current", "fresh"]);
  });

  it("keeps a capped session resident window contiguous when a live head arrives", async () => {
    const resident = Array.from({ length: 2000 }, (_, index) => ({ ...activity(`old-${index}`), id: `old-${index}` }));
    const current = { ...session(resident), activityCount: 2100 };
    const fresh = { ...session([{ ...activity("fresh-head"), id: "fresh-head" }]), activityCount: 2101, nextPageToken: "head-next" };
    const client = {
      listSessions: vi.fn().mockResolvedValue([current]),
      getSession: vi.fn().mockResolvedValueOnce(current).mockResolvedValueOnce(current).mockResolvedValue(fresh),
      syncSessionActivities: vi.fn().mockResolvedValue({ mutations: [], throughCursor: "cursor-2", resyncRequired: false }),
    } as unknown as AgentmetryClient;
    conversationsClient = client;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.conversations.selected?.activities).toHaveLength(2000));
    const targets = [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "session-1", traceId: "" }];

    await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-1", targets });
    await host.conversations.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-2", targets });

    expect(host.conversations.selected?.activities).toHaveLength(2000);
    expect(host.conversations.selected?.activities[0]?.id).toBe("fresh-head");
    expect(host.conversations.selected?.activities.filter(({ id }) => id?.startsWith("old-") ?? false)).toHaveLength(1999);
    expect(host.conversations.selected?.activityOffset).toBe(0);
    expect(host.conversations.selected?.nextPageToken).toBe("next");
  });

  it("removes a selected session immediately when its live refresh returns not found", async () => {
    const removed = session([{ ...activity("removed"), id: "removed" }]);
    const remaining = { ...session([{ ...activity("remaining"), id: "remaining" }]), id: "session-2" };
    const client = {
      listSessions: vi.fn().mockResolvedValue([removed, remaining]),
      getSession: vi.fn()
        .mockResolvedValueOnce(removed)
        .mockRejectedValueOnce(new ConnectError("gone", Code.NotFound))
        .mockResolvedValue(remaining),
    } as unknown as AgentmetryClient;
    conversationsClient = client;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-1"));

    await host.conversations.applyLiveUpdate({
      resyncRequired: false,
      throughCursor: "cursor-1",
      targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "session-1", traceId: "" }],
    });

    expect(host.conversations.sessions.map(({ id }) => id)).toEqual(["session-2"]);
    await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-2"));
  });

  it("detects removal of an affected requested session while its workspace is inactive", async () => {
    const current = session();
    const getSession = vi.fn()
      .mockResolvedValueOnce(current)
      .mockRejectedValueOnce(new ConnectError("gone", Code.NotFound));
    conversationsClient = {
      listSessions: vi.fn().mockResolvedValue([current]),
      getSession,
    } as unknown as AgentmetryClient;
    const host = document.createElement("test-conversations-host") as ConversationsHost;
    document.body.append(host);
    host.conversations.select({ sourceId: "codex", conversationId: "session-1" });
    await vi.waitFor(() => expect(host.conversations.selected?.id).toBe("session-1"));
    host.active = false;
    host.requestUpdate();
    await host.updateComplete;

    await host.conversations.applyLiveUpdate({
      resyncRequired: false,
      throughCursor: "cursor-after-removal",
      targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "session-1", traceId: "" }],
    });

    expect(getSession).toHaveBeenLastCalledWith("codex", "session-1", undefined, undefined, expect.any(AbortSignal));
    expect(host.conversations.takeRemovedSession()).toEqual({ sourceId: "codex", conversationId: "session-1" });
  });

  it("appends trace pages without replacing the first page", async () => {
    const first: Trace = {
      traceId: "trace-1", startedAt: "", endedAt: "", status: "ok", rootSpanCount: 1, missingParentCount: 0,
      conversations: [], agents: [], activities: [activity("first")], activityOffset: 0, activityCount: 2,
      hasMore: true, nextPageToken: "next",
    };
    const second: Trace = { ...first, activities: [activity("second")], activityOffset: 1, hasMore: false, nextPageToken: undefined };
    const client = {
      getTrace: vi.fn().mockImplementation((_traceId: string, offset: number) => Promise.resolve(offset === 0 ? first : second)),
    } as unknown as AgentmetryClient;
    traceClient = client;
    const host = document.createElement("test-trace-host") as TraceHost;
    document.body.append(host);
    host.trace.open("trace-1");

    await vi.waitFor(() => expect(host.trace.value?.activities).toHaveLength(1));
    await host.trace.loadMore();

    expect(host.trace.value?.activities.map(({ name }) => name)).toEqual(["first", "second"]);
    expect(host.trace.value?.hasMore).toBe(false);
  });

  it("hides the previous trace while the next trace is loading", async () => {
    const first: Trace = {
      traceId: "trace-a", startedAt: "", endedAt: "", status: "ok", rootSpanCount: 1, missingParentCount: 0,
      conversations: [], agents: [], activities: [], activityOffset: 0, activityCount: 0, hasMore: false,
    };
    let resolveSecond!: (trace: Trace) => void;
    const second = new Promise<Trace>((resolve) => { resolveSecond = resolve; });
    const client = {
      getTrace: vi.fn().mockImplementation((traceId: string) => traceId === "trace-a" ? Promise.resolve(first) : second),
    } as unknown as AgentmetryClient;
    traceClient = client;
    const host = document.createElement("test-trace-host") as TraceHost;
    document.body.append(host);
    host.trace.open("trace-a");
    await vi.waitFor(() => expect(host.trace.value?.traceId).toBe("trace-a"));

    host.trace.open("trace-b");
    await vi.waitFor(() => expect(host.trace.loading).toBe(true));

    expect(host.trace.value).toBeUndefined();
    resolveSecond({ ...first, traceId: "trace-b" });
  });

  it("clears page loading when navigation invalidates an in-flight page", async () => {
    const first: Trace = {
      traceId: "trace-a", startedAt: "", endedAt: "", status: "ok", rootSpanCount: 1, missingParentCount: 0,
      conversations: [], agents: [], activities: [activity("first")], activityOffset: 0, activityCount: 2,
      hasMore: true, nextPageToken: "next",
    };
    let resolvePage!: (trace: Trace) => void;
    const page = new Promise<Trace>((resolve) => { resolvePage = resolve; });
    const client = {
      getTrace: vi.fn().mockImplementation((_traceId: string, offset: number) => offset === 0 ? Promise.resolve(first) : page),
    } as unknown as AgentmetryClient;
    traceClient = client;
    const host = document.createElement("test-trace-host") as TraceHost;
    document.body.append(host);
    host.trace.open("trace-a");
    await vi.waitFor(() => expect(host.trace.value?.traceId).toBe("trace-a"));

    const loadingPage = host.trace.loadMore();
    expect(host.trace.loadingPage).toBe(true);
    host.trace.open("trace-b");
    expect(host.trace.loadingPage).toBe(false);
    resolvePage({ ...first, activities: [activity("second")], activityOffset: 1, hasMore: false });
    await loadingPage;
    expect(host.trace.loadingPage).toBe(false);
  });

  it("clears manual trace pagination when disconnected", async () => {
    const first: Trace = {
      traceId: "trace-a", startedAt: "", endedAt: "", status: "ok", rootSpanCount: 1, missingParentCount: 0,
      conversations: [], agents: [], activities: [activity("first")], activityOffset: 0, activityCount: 2,
      hasMore: true, nextPageToken: "next",
    };
    const client = {
      getTrace: vi.fn().mockImplementation((_traceId: string, offset: number) => offset === 0 ? Promise.resolve(first) : new Promise<Trace>(() => undefined)),
    } as unknown as AgentmetryClient;
    traceClient = client;
    const host = document.createElement("test-trace-host") as TraceHost;
    document.body.append(host);
    host.trace.open("trace-a");
    await vi.waitFor(() => expect(host.trace.value?.traceId).toBe("trace-a"));

    void host.trace.loadMore();
    expect(host.trace.loadingPage).toBe(true);
    host.remove();

    expect(host.trace.loadingPage).toBe(false);
  });

  it("uses bounded mutation sync after establishing the trace cursor", async () => {
    const first: Trace = { traceId: "trace-a", startedAt: "", endedAt: "", status: "ok", rootSpanCount: 1, missingParentCount: 0, conversations: [], agents: [], activities: [{ ...activity("first"), id: "first" }], activityOffset: 0, activityCount: 1, hasMore: false };
	const getTrace = vi.fn().mockResolvedValue(first);
	const syncTraceActivities = vi.fn().mockResolvedValue({ mutations: [], throughCursor: "cursor-2", resyncRequired: false });
	traceClient = { getTrace, syncTraceActivities } as unknown as AgentmetryClient;
    const host = document.createElement("test-trace-host") as TraceHost;
    document.body.append(host);
    host.trace.open("trace-a");
    await vi.waitFor(() => expect(host.trace.value?.traceId).toBe("trace-a"));

    const targets = [{ kind: ProjectionTargetKind.TRACE, sourceId: "", sessionId: "", traceId: "trace-a" }];
	await host.trace.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-1", targets });
	await host.trace.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-2", targets });

	expect(syncTraceActivities).toHaveBeenCalledWith("trace-a", "cursor-1", "cursor-2", "", expect.any(AbortSignal));
  });

  it("restarts trace paging from the head after a live tail update without losing the middle", async () => {
    const activities = Array.from({ length: 300 }, (_, index) => ({
      ...activity(`activity-${index}`),
      id: `activity-${index}`,
      observedAt: new Date(Date.UTC(2026, 7, 11, 0, 0, 0, index)).toISOString(),
    }));
    const tracePage = (offset: number): Trace => ({
      traceId: "trace-a", startedAt: "", endedAt: "", status: "ok", rootSpanCount: 1, missingParentCount: 0,
      conversations: [], agents: [], activities: activities.slice(offset, offset + 100), activityOffset: offset,
      activityCount: activities.length, hasMore: offset + 100 < activities.length,
      nextPageToken: offset + 100 < activities.length ? `page-${offset + 100}` : undefined,
    });
    const getTrace = vi.fn().mockImplementation((_traceId: string, offset: number, _limit: number, pageToken: string) => {
      const tokenOffset = pageToken ? Number(pageToken.replace("page-", "")) : offset;
      return Promise.resolve(tracePage(tokenOffset));
    });
    const syncTraceActivities = vi.fn().mockResolvedValue({
      mutations: [{ operation: "upsert", activityId: "activity-299", activity: activities[299] }],
      throughCursor: "cursor-2", resyncRequired: false,
    });
    traceClient = { getTrace, syncTraceActivities } as unknown as AgentmetryClient;
    const host = document.createElement("test-trace-host") as TraceHost;
    document.body.append(host);
    host.trace.open("trace-a");
    await vi.waitFor(() => expect(host.trace.value?.traceId).toBe("trace-a"));

    const targets = [{ kind: ProjectionTargetKind.TRACE, sourceId: "", sessionId: "", traceId: "trace-a" }];
    await host.trace.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-1", targets });
    await host.trace.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-2", targets });
    await host.trace.loadMore();
    await host.trace.loadMore();

    expect(host.trace.value?.activities.map(({ id }) => id)).toEqual(activities.map(({ id }) => id));
    expect(host.trace.value?.hasMore).toBe(false);
  });

  it("replaces resident trace state when mutation history requires resync", async () => {
    const stale: Trace = { traceId: "trace-a", startedAt: "", endedAt: "", status: "ok", rootSpanCount: 1, missingParentCount: 0, conversations: [], agents: [], activities: [{ ...activity("stale"), id: "stale" }], activityOffset: 0, activityCount: 1, hasMore: false };
    const fresh: Trace = { ...stale, activities: [{ ...activity("fresh"), id: "fresh" }] };
    const getTrace = vi.fn().mockResolvedValueOnce(stale).mockResolvedValueOnce(stale).mockResolvedValue(fresh);
    const syncTraceActivities = vi.fn().mockResolvedValue({ mutations: [], throughCursor: "reset-trace-cursor", resyncRequired: true });
    traceClient = { getTrace, syncTraceActivities } as unknown as AgentmetryClient;
    const host = document.createElement("test-trace-host") as TraceHost;
    document.body.append(host);
    host.trace.open("trace-a");
    await vi.waitFor(() => expect(host.trace.value?.traceId).toBe("trace-a"));
    const targets = [{ kind: ProjectionTargetKind.TRACE, sourceId: "", sessionId: "", traceId: "trace-a" }];

    await host.trace.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-1", targets });
    await host.trace.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-2", targets });

    expect(host.trace.value?.activities.map(({ id }) => id)).toEqual(["fresh"]);
  });

  it("clears a trace removed during a live update without retrying the acknowledged window", async () => {
    const current: Trace = { traceId: "trace-a", startedAt: "", endedAt: "", status: "ok", rootSpanCount: 1, missingParentCount: 0, conversations: [], agents: [], activities: [activity("current")], activityOffset: 0, activityCount: 1, hasMore: false };
    const getTrace = vi.fn()
      .mockResolvedValueOnce(current)
      .mockRejectedValueOnce(new ConnectError("gone", Code.NotFound));
    traceClient = { getTrace } as unknown as AgentmetryClient;
    const host = document.createElement("test-trace-host") as TraceHost;
    document.body.append(host);
    host.trace.open("trace-a");
    await vi.waitFor(() => expect(host.trace.value?.traceId).toBe("trace-a"));

    await expect(host.trace.applyLiveUpdate({
      resyncRequired: true,
      throughCursor: "cursor-after-removal",
      targets: [{ kind: ProjectionTargetKind.TRACE, sourceId: "", sessionId: "", traceId: "trace-a" }],
    })).resolves.toBeUndefined();

    expect(host.trace.value).toBeUndefined();
    expect(host.trace.takeRemovedTrace()).toBe("trace-a");
    expect(host.trace.takeRemovedTrace()).toBeUndefined();
  });

  it("falls back to an authoritative trace snapshot when mutation catch-up exceeds ten pages", async () => {
    const stale: Trace = { traceId: "trace-a", startedAt: "", endedAt: "", status: "ok", rootSpanCount: 1, missingParentCount: 0, conversations: [], agents: [], activities: [{ ...activity("stale"), id: "stale" }], activityOffset: 0, activityCount: 1, hasMore: false };
    const fresh: Trace = { ...stale, activities: [{ ...activity("fresh"), id: "fresh" }] };
    const getTrace = vi.fn().mockResolvedValueOnce(stale).mockResolvedValueOnce(stale).mockResolvedValue(fresh);
    const syncTraceActivities = vi.fn().mockImplementation((..._args: unknown[]) => Promise.resolve({
      mutations: [], throughCursor: "cursor-2", resyncRequired: false,
      nextPageToken: syncTraceActivities.mock.calls.length <= 10 ? `page-${syncTraceActivities.mock.calls.length}` : undefined,
    }));
    traceClient = { getTrace, syncTraceActivities } as unknown as AgentmetryClient;
    const host = document.createElement("test-trace-host") as TraceHost;
    document.body.append(host);
    host.trace.open("trace-a");
    await vi.waitFor(() => expect(host.trace.value?.traceId).toBe("trace-a"));
    const targets = [{ kind: ProjectionTargetKind.TRACE, sourceId: "", sessionId: "", traceId: "trace-a" }];

    await host.trace.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-1", targets });
    await host.trace.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-2", targets });

    expect(host.trace.value?.activities.map(({ id }) => id)).toEqual(["fresh"]);
    expect(syncTraceActivities).toHaveBeenCalledTimes(10);
  });

  it("ignores a non-cooperative trace page after a live update resets paging", async () => {
    const current: Trace = { traceId: "trace-a", startedAt: "", endedAt: "", status: "ok", rootSpanCount: 1, missingParentCount: 0, conversations: [], agents: [], activities: [{ ...activity("current"), id: "current" }], activityOffset: 0, activityCount: 2, hasMore: true, nextPageToken: "next" };
    const fresh: Trace = { ...current, activities: [{ ...activity("fresh"), id: "fresh" }] };
    let resolvePage!: (trace: Trace) => void;
    const page = new Promise<Trace>((resolve) => { resolvePage = resolve; });
    const getTrace = vi.fn().mockImplementation((_traceId: string, offset: number) => {
      if (offset > 0) return page;
      return Promise.resolve(getTrace.mock.calls.length <= 2 ? current : fresh);
    });
    traceClient = {
      getTrace,
      syncTraceActivities: vi.fn().mockResolvedValue({ mutations: [], throughCursor: "cursor-2", resyncRequired: false }),
    } as unknown as AgentmetryClient;
    const host = document.createElement("test-trace-host") as TraceHost;
    document.body.append(host);
    host.trace.open("trace-a");
    await vi.waitFor(() => expect(host.trace.value?.traceId).toBe("trace-a"));
    const targets = [{ kind: ProjectionTargetKind.TRACE, sourceId: "", sessionId: "", traceId: "trace-a" }];
    await host.trace.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-1", targets });

    const paging = host.trace.loadMore();
    await host.trace.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-2", targets });
    resolvePage({ ...current, activities: [{ ...activity("stale-page"), id: "stale-page" }], activityOffset: 1, hasMore: false });
    await paging;

    expect(host.trace.value?.activities.map(({ id }) => id)).toEqual(["current", "fresh"]);
  });

  it("replaces a capped trace resident window with a contiguous authoritative head", async () => {
    const resident = Array.from({ length: 2000 }, (_, index) => ({ ...activity(`old-${index}`), id: `old-${index}` }));
    const current: Trace = { traceId: "trace-a", startedAt: "", endedAt: "", status: "ok", rootSpanCount: 1, missingParentCount: 0, conversations: [], agents: [], activities: resident, activityOffset: 0, activityCount: 2100, hasMore: true, nextPageToken: "old-next" };
    const head: Trace = { ...current, activities: [{ ...activity("fresh-head"), id: "fresh-head" }], activityCount: 2101, nextPageToken: "head-next" };
    const getTrace = vi.fn().mockResolvedValueOnce(current).mockResolvedValueOnce(current).mockResolvedValue(head);
    traceClient = {
      getTrace,
      syncTraceActivities: vi.fn().mockResolvedValue({ mutations: [], throughCursor: "cursor-2", resyncRequired: false }),
    } as unknown as AgentmetryClient;
    const host = document.createElement("test-trace-host") as TraceHost;
    document.body.append(host);
    host.trace.open("trace-a");
    await vi.waitFor(() => expect(host.trace.value?.activities).toHaveLength(2000));
    const targets = [{ kind: ProjectionTargetKind.TRACE, sourceId: "", sessionId: "", traceId: "trace-a" }];

    await host.trace.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-1", targets });
    await host.trace.applyLiveUpdate({ resyncRequired: false, throughCursor: "cursor-2", targets });

    expect(host.trace.value?.activities.map(({ id }) => id)).toEqual(["fresh-head"]);
    expect(host.trace.value?.activityOffset).toBe(0);
    expect(host.trace.value?.nextPageToken).toBe("head-next");
  });
});
