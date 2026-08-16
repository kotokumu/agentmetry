import { afterEach, describe, expect, it, vi } from "vitest";
import { LitElement } from "lit";
import type { AgentmetryClient, ActivityPage } from "../api/agentmetry-client";
import type { Activity, Session, TokenUsage, Trace } from "../model/telemetry";
import { ConversationsController } from "./conversations-controller";
import { TraceController } from "./trace-controller";
import type { TelemetryFilters } from "./query-filters";

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
  constructor() {
    super();
    this.conversations = new ConversationsController(this, conversationsClient, () => this.filters);
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
});
