import { afterEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import "./agentmetry-app";
import { agentmetryClient } from "../api/agentmetry-client";
import { LIVE_UPDATE_EVENT, type LiveUpdateDelivery } from "../controllers/live-update-controller";
import { ProjectionTargetKind } from "../gen/agentmetry/v1/agentmetry_pb";
import type { AgentmetryApp } from "./agentmetry-app";
import type { TimeRangeFilter } from "../components/time-range-filter";
import type { SessionList } from "../components/session-list";
import type { ActivityTable } from "../components/activity-table";
import type { KpiCard } from "../components/kpi-card";
import type { ReworkComparison } from "../components/rework-comparison";
import type { ConversationWorkspace } from "../components/conversation-workspace";
import type { TraceExplorer } from "../components/trace-explorer";
import type { DashboardSummary } from "../components/dashboard-summary";
import type { MCPConnection } from "../components/mcp-connection";
import type { TokenUsage } from "../model/telemetry";

const emptyOverview = {
  sources: [],
  signalCounts: { traces: 0, logs: 0, metrics: 0 },
  runCount: 0,
  agentCount: 0,
  tokens: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, reasoning: 0, total: 0 },
  recentActivity: [],
  sessions: [],
  planUsage: [],
};

type TestSession = {
  id: string;
  sourceId: string;
  sources: readonly object[];
  startedAt: string;
  endedAt: string;
  activityCount: number;
  tokens: TokenUsage;
  costUsd?: number;
  agents: readonly object[];
  activities: readonly object[];
  traceIds?: readonly string[];
};
type TestOverview = Omit<typeof emptyOverview, "sessions"> & { sessions: readonly TestSession[] };

const connectResponse = (payload: unknown) => ({
  ok: true,
  status: 200,
  statusText: "OK",
  headers: new Headers({ "content-type": "application/json" }),
  json: async () => payload,
});

const dashboardResponse = (overview: TestOverview) => ({
  dashboard: {
    sources: overview.sources,
    signalCounts: overview.signalCounts,
    runCount: overview.runCount,
    agentCount: overview.agentCount,
    tokens: overview.tokens,
    recentActivity: overview.recentActivity,
    planUsage: overview.planUsage,
  },
});

const sessionSummary = (session: TestSession) => {
  const summary = {
    id: session.id,
    sourceId: session.sourceId,
    sources: session.sources,
    startedAt: session.startedAt,
    endedAt: session.endedAt,
    activityCount: session.activityCount,
    tokens: session.tokens,
    agents: session.agents,
  } as { id: string; sourceId: string; sources: readonly object[]; startedAt: string; endedAt: string; activityCount: number; tokens: TokenUsage; agents: readonly object[]; costUsd?: number };
  if (session.costUsd !== undefined) summary.costUsd = session.costUsd;
  return summary;
};

const sessionsResponse = (overview: TestOverview) => ({
  sessions: overview.sessions.map(sessionSummary),
  page: { hasMore: false },
});

const activitiesResponse = (session: TestSession, agentId = "") => ({
  activities: agentId ? session.activities.filter((activity) => (activity as { agentId?: string }).agentId === agentId) : session.activities,
  page: { hasMore: false },
  total: session.activityCount,
});

const reworkResponse = (session: TestSession) => ({
  sourceId: session.sourceId,
  sessionId: session.id,
  sessionTokens: {
    input: session.tokens.input === null ? undefined : String(session.tokens.input),
    output: session.tokens.output === null ? undefined : String(session.tokens.output),
    total: session.tokens.total === null ? undefined : String(session.tokens.total),
  },
  harnessContext: {
    counts: { eligibleRecords: "8", reportedRecords: "0", unreportedRecords: "8", invalidRecords: "0", distinctIdentities: "0" },
    unreported: {},
  },
  metrics: {
    validationFailures: 2,
    failFixRetryCycles: 1,
    reworkDurationMs: "3500",
    totalAgentEffortMs: "10000",
    reworkAgentEffortRate: 0.35,
    reworkTokens: { input: "100", output: "20", total: "120" },
    toolAttemptsWithOutcome: 4,
    toolFailures: 1,
    toolFailureRate: 0.25,
    apiRetryWaste: { attempts: 1, durationMs: "500", tokens: {} },
    repeatedCommands: 3,
    reeditedFiles: 2,
    validationAttemptsWithOutcome: 4,
    firstPassEligibleValidations: 2,
    firstPassSuccesses: 1,
    firstPassSuccessRate: 0.5,
    recurringFailureLoops: 1,
    repeatedFailureAttempts: 3,
    resolvedFailureLoops: 1,
    unresolvedFailureLoops: 0,
    failureResolutionDurationMs: 6500,
    failureResolutionTokens: { input: 30, output: 8, cacheRead: null, cacheWrite: null, reasoning: null, total: 38 },
  },
  coverage: { activityCoverage: "partial_page", canonicalEvents: 8, classifiedEvents: 7, knownOutcomes: 4, validationAttempts: 4, fingerprintedFailures: 2, identifiedValidationAttempts: 4, idBackedValidationAttempts: 3, mergedValidationAttempts: 1, uncorrelatedValidationObservations: 1, conflictingAttemptObservations: 0, ambiguousFailureAttempts: 0 },
  capabilities: {
    changeRevert: { state: "unavailable", reason: "needs diffs" },
    crossAgentOverlap: { state: "unavailable", reason: "needs identities" },
  },
  failureEpisodes: [{
    agentId: "agent-1", operation: "test", validationFingerprint: "sha256:abcdef1234567890", errorFingerprints: ["sha256:1234567890abcdef"],
    failureAttempts: 3, resolved: true, resolutionDurationMs: 6500, resolutionTokens: {}, traceId: "trace-1", spanId: "span-1",
  }],
});

const connectPath = (url: string) => new URL(url, "http://localhost").pathname;
const connectBody = (call: readonly unknown[]) => JSON.parse(new TextDecoder().decode((call[1] as { body: Uint8Array }).body));
const workspaceOf = (app: AgentmetryApp) => app.shadowRoot?.querySelector<ConversationWorkspace>("am-conversation-workspace");
const workspaceRootOf = (app: AgentmetryApp) => workspaceOf(app)?.shadowRoot;
const traceExplorerOf = (app: AgentmetryApp) => app.shadowRoot?.querySelector<TraceExplorer>("am-trace-explorer");
const traceRootOf = (app: AgentmetryApp) => traceExplorerOf(app)?.shadowRoot;
const dashboardOf = (app: AgentmetryApp) => app.shadowRoot?.querySelector<DashboardSummary>("am-dashboard-summary");

const overviewFetch = (overview: TestOverview) => vi.fn().mockImplementation(async (url: string, init?: { body?: BodyInit | null }) => {
  const body = init?.body ? JSON.parse(new TextDecoder().decode(init.body as Uint8Array)) as { agentId?: string; sessionId?: string } : {};
  const requestedSession = overview.sessions.find(({ id }) => id === body.sessionId) ?? overview.sessions[0];
  switch (connectPath(url).split("/").at(-1)) {
    case "GetDashboard": return connectResponse(dashboardResponse(overview));
    case "ListSessions": return connectResponse(sessionsResponse(overview));
    case "GetSession": return connectResponse(requestedSession ? { session: sessionSummary(requestedSession), traceIds: requestedSession.traceIds ?? [] } : {});
    case "ListSessionActivities": return connectResponse(requestedSession ? activitiesResponse(requestedSession, body.agentId) : { activities: [], page: { hasMore: false }, total: 0 });
    case "GetSessionRework": return connectResponse(requestedSession ? reworkResponse(requestedSession) : {});
    default: return connectResponse({});
  }
});

afterEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  history.replaceState({}, "", "/");
});

describe("Agentmetry app composition", () => {
  it("shows normalized rework indicators in the selected conversation", async () => {
    const overview = {
      ...emptyOverview,
      sessions: [{
        id: "session-rework",
        sourceId: "codex",
        sources: [{ id: "codex", label: "Codex" }],
        startedAt: "2026-08-11T00:00:00Z",
        endedAt: "2026-08-11T00:01:00Z",
        activityCount: 8,
        tokens: { ...emptyOverview.tokens, input: 800, output: 200, total: 1_000 },
        agents: [],
        activities: [],
      }],
    } as TestOverview;
    vi.stubGlobal("fetch", overviewFetch(overview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    const panel = await vi.waitFor(() => {
      const result = workspaceRootOf(app)?.querySelector("am-rework-summary");
      expect(result).not.toBeNull();
      expect((result?.shadowRoot?.textContent ?? "")).toContain("Development rework");
      return result;
    });
    const cards = Array.from(panel?.shadowRoot?.querySelectorAll<KpiCard>("am-kpi-card") ?? []);
    await Promise.all(cards.map((card) => card.updateComplete));

		expect(cards).toHaveLength(12);
    expect(cards.map((card) => card.shadowRoot?.textContent ?? "").join(" ")).toContain("25.0%");
    expect(cards.map((card) => card.shadowRoot?.textContent ?? "").join(" ")).toContain("35.0%");
    expect(cards.map((card) => card.shadowRoot?.textContent ?? "").join(" ")).toContain("12.0%");
    expect(panel?.shadowRoot?.textContent).toContain("Partial evidence");
  });

  it("shows an automatic same-source Before / After comparison without replacing current diagnostics", async () => {
    const current: TestSession = {
      id: "session-current", sourceId: "codex", sources: [{ id: "codex", label: "Codex" }],
      startedAt: "2026-08-11T10:00:00Z", endedAt: "2026-08-11T10:30:00Z", activityCount: 4,
      tokens: { ...emptyOverview.tokens, total: 1_000 }, agents: [], activities: [],
    };
    const baseline: TestSession = {
      ...current,
      id: "session-baseline",
      startedAt: "2026-08-11T08:00:00Z",
      endedAt: "2026-08-11T09:00:00Z",
    };
    vi.stubGlobal("fetch", overviewFetch({ ...emptyOverview, sessions: [current, baseline] } as TestOverview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    const panel = await vi.waitFor(() => {
      const result = workspaceRootOf(app)?.querySelector<ReworkComparison>("am-rework-comparison");
      expect(result?.shadowRoot?.textContent).toContain("Before / After diagnostics");
      expect(result?.shadowRoot?.textContent).toContain("Initial validation success proxy");
      return result;
    });

    expect(panel?.state.status === "ready" ? panel.state.selectedBaselineId : "").toBe("session-baseline");
    expect(panel?.shadowRoot?.querySelector("table")).not.toBeNull();
    expect(workspaceRootOf(app)?.querySelector("am-rework-summary")?.shadowRoot?.textContent).toContain("Development rework");
  });

  it("returns a removed trace route to the filtered dashboard", async () => {
    history.replaceState({}, "", "/traces/trace-gone?range=7d");
    vi.stubGlobal("fetch", overviewFetch(emptyOverview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);
    await vi.waitFor(() => expect(traceExplorerOf(app)).toBeTruthy());

    traceExplorerOf(app)?.dispatchEvent(new CustomEvent("trace-removed", {
      detail: { traceId: "trace-gone" }, bubbles: true, composed: true,
    }));

    await vi.waitFor(() => expect(location.pathname).toBe("/"));
    expect(location.search).toContain("range=7d");
  });

	it("returns a removed conversation route to the filtered dashboard", async () => {
    history.replaceState({}, "", "/conversations/codex/removed?range=7d");
    vi.stubGlobal("fetch", overviewFetch(emptyOverview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);
    await vi.waitFor(() => expect(workspaceOf(app)).toBeTruthy());

    workspaceOf(app)?.dispatchEvent(new CustomEvent("conversation-removed", {
      detail: { sourceId: "codex", conversationId: "removed" }, bubbles: true, composed: true,
    }));

    await vi.waitFor(() => expect(location.pathname).toBe("/"));
	  expect(location.search).toContain("range=7d");
	});

	it("replaces a child conversation route with its canonical aggregated root", async () => {
	  const traceId = "11111111111111111111111111111111";
	  const spanId = "aaaaaaaaaaaaaaaa";
	  history.replaceState({}, "", `/conversations/codex/child?range=7d&traceId=${traceId}&spanId=${spanId}`);
	  const parent: TestSession = {
		id: "parent", sourceId: "codex", sources: [{ id: "codex", label: "Codex" }],
		startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:01:00Z", activityCount: 0,
		tokens: emptyOverview.tokens, agents: [], activities: [],
	  };
	  vi.stubGlobal("fetch", overviewFetch({ ...emptyOverview, sessions: [parent] } as TestOverview));
	  const app = document.createElement("am-app") as AgentmetryApp;
	  document.body.append(app);

	  await vi.waitFor(() => expect(location.pathname).toBe("/conversations/codex/parent"));
	  expect(location.search).toContain("range=7d");
	  expect(location.search).toContain(`traceId=${traceId}`);
	  expect(location.search).toContain(`spanId=${spanId}`);
	  await vi.waitFor(() => expect(workspaceRootOf(app)?.querySelector(".session-id")?.textContent).toContain("parent"));
	});

  it("keeps a valid trace open when its conversation return origin is removed", async () => {
    history.replaceState({}, "", "/conversations/codex/conversation-1?range=7d");
    const conversation: TestSession = {
      id: "conversation-1", sourceId: "codex", sources: [{ id: "codex", label: "Codex" }],
      startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:01:00Z", activityCount: 0,
      tokens: emptyOverview.tokens, agents: [], activities: [],
    };
    const overview = { ...emptyOverview, sessions: [conversation] } as TestOverview;
    const trace = {
      traceId: "trace-a", startedAt: conversation.startedAt, endedAt: conversation.endedAt,
      status: "ok", rootSpanCount: 1, missingParentCount: 0, conversations: [], agents: [], activities: [],
    };
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async (url: string) => {
      switch (connectPath(url).split("/").at(-1)) {
        case "GetDashboard": return connectResponse(dashboardResponse(overview));
        case "ListSessions": return connectResponse(sessionsResponse(overview));
        case "GetSession": return connectResponse({ session: sessionSummary(conversation), traceIds: [] });
        case "ListSessionActivities": return connectResponse(activitiesResponse(conversation));
        case "GetTrace": return connectResponse(trace);
        default: return connectResponse({});
      }
    }));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);
    await vi.waitFor(() => expect(workspaceOf(app)).toBeTruthy());

    workspaceOf(app)?.dispatchEvent(new CustomEvent("trace-selected", {
      detail: { traceId: "trace-a", sourceId: "codex", conversationId: "conversation-1" },
      bubbles: true, composed: true,
    }));
    await vi.waitFor(() => expect(location.pathname).toBe("/traces/trace-a"));
    await vi.waitFor(() => expect(traceExplorerOf(app)).toBeTruthy());

    vi.spyOn(agentmetryClient, "getSession").mockRejectedValueOnce(new ConnectError("gone", Code.NotFound));
    const pending: Promise<unknown>[] = [];
    const detail: LiveUpdateDelivery = {
      resyncRequired: false,
      throughCursor: "cursor-after-removal",
      targets: [{ kind: ProjectionTargetKind.SESSION, sourceId: "codex", sessionId: "conversation-1", traceId: "" }],
      waitUntil: (promise) => pending.push(promise),
    };
    window.dispatchEvent(new CustomEvent(LIVE_UPDATE_EVENT, { detail }));
    await Promise.all(pending);
    await app.updateComplete;

    expect(location.pathname).toBe("/traces/trace-a");
    expect(location.search).toContain("range=7d");
    expect(traceRootOf(app)?.querySelector<HTMLAnchorElement>("a.trace-close")?.getAttribute("href")).toBe("/?range=7d");
    expect(history.state?.origin).toBeUndefined();
  });

  it("uses source-neutral product language", async () => {
    vi.stubGlobal("fetch", overviewFetch(emptyOverview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    await app.updateComplete;

    const content = `${app.shadowRoot?.textContent ?? ""} ${workspaceRootOf(app)?.textContent ?? ""}`;
    expect(content).toContain("AGENTMETRY");
    expect(content).toContain("Agent conversations");
    expect(content).toContain("Receiving OTLP locally");
    expect(content).toContain("Loading conversations");
    expect(app.shadowRoot?.querySelector("main")?.getAttribute("data-density")).toBe("operator");
    expect(app.shadowRoot?.querySelector<HTMLAnchorElement>("a.brand")?.getAttribute("href")).toBe("/");
  });

  it("composes feature components instead of rendering feature internals in the app shell", async () => {
    vi.stubGlobal("fetch", overviewFetch(emptyOverview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);
    await app.updateComplete;

    expect(app.shadowRoot?.querySelector("am-dashboard-summary")).not.toBeNull();
    expect(app.shadowRoot?.querySelector("am-conversation-workspace")).not.toBeNull();
    expect(app.shadowRoot?.querySelector("am-trace-explorer")).toBeNull();
    expect(app.shadowRoot?.querySelector("am-app-update-control")).not.toBeNull();
    expect(app.shadowRoot?.querySelector("am-mcp-connection")).not.toBeNull();
    expect(app.shadowRoot?.querySelector(".kpis")).toBeNull();
    expect(app.shadowRoot?.querySelector(".workspace")).toBeNull();
  });

  it("shows the current-origin MCP connection details", async () => {
    vi.stubGlobal("fetch", overviewFetch(emptyOverview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);
    await app.updateComplete;

    const mcp = app.shadowRoot?.querySelector<MCPConnection>("am-mcp-connection");
    await mcp?.updateComplete;
    mcp?.shadowRoot?.querySelector<HTMLButtonElement>("[aria-controls='mcp-connection-panel']")?.click();
    await mcp?.updateComplete;
    expect(mcp?.shadowRoot?.querySelector<HTMLElement>("[aria-labelledby='mcp-connection-title']")?.hidden).toBe(false);
    expect(mcp?.shadowRoot?.querySelector<HTMLInputElement>("[aria-label='MCP server URL']")?.value)
      .toBe(`${location.origin}/mcp`);
  });

  it("renders conversations without waiting for the dashboard task", async () => {
    const overview = {
      ...emptyOverview,
      sessions: [{
        id: "session-fast",
        sourceId: "codex",
        sources: [{ id: "codex", label: "Codex" }],
        startedAt: "2026-08-11T00:00:00Z",
        endedAt: "2026-08-11T00:01:00Z",
        activityCount: 0,
        tokens: emptyOverview.tokens,
        agents: [],
        activities: [],
      }],
    } as TestOverview;
    const dashboardNeverCompletes = new Promise<ReturnType<typeof connectResponse>>(() => undefined);
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async (url: string) => {
      switch (connectPath(url).split("/").at(-1)) {
        case "GetDashboard": return dashboardNeverCompletes;
        case "ListSessions": return connectResponse(sessionsResponse(overview));
        case "GetSession": return connectResponse({ session: sessionSummary(overview.sessions[0]), traceIds: [] });
        case "ListSessionActivities": return connectResponse(activitiesResponse(overview.sessions[0]));
        default: return connectResponse({});
      }
    }));

    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    await vi.waitFor(() => expect(workspaceRootOf(app)?.textContent).toContain("session-fast"));
    expect(workspaceRootOf(app)?.textContent).toContain("Selected conversation");
    expect(app.shadowRoot?.querySelector(".status")?.textContent).toContain("Refreshing dashboard");
  });

  it("shows unavailable conversation KPIs when the conversation list fails", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async (url: string) => {
      switch (connectPath(url).split("/").at(-1)) {
        case "GetDashboard": return connectResponse(dashboardResponse(emptyOverview));
        case "ListSessions": throw new Error("list unavailable");
        default: return connectResponse({});
      }
    }));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    await vi.waitFor(() => expect(dashboardOf(app)?.conversationStatus).toBe("failed"));
    const cards = Array.from(dashboardOf(app)?.shadowRoot?.querySelectorAll<KpiCard>("am-kpi-card") ?? []);
    expect(cards[0]?.value).toBe("Unavailable");
    expect(cards[2]?.value).toBe("Unavailable");
  });

  it("returns from a conversation route to the dashboard through the brand", async () => {
    history.replaceState({}, "", "/conversations/codex/conversation-1");
    vi.stubGlobal("fetch", overviewFetch(emptyOverview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);
    await app.updateComplete;

    app.shadowRoot?.querySelector<HTMLAnchorElement>("a.brand")?.click();
    await app.updateComplete;

    expect(location.pathname).toBe("/");
    expect(app.shadowRoot?.querySelector<HTMLAnchorElement>("a.brand")?.getAttribute("aria-label")).toBe("Back to Agentmetry dashboard");
  });

  it("interprets a time-range intent by requesting that range", async () => {
    const fetchStub = overviewFetch(emptyOverview);
    vi.stubGlobal("fetch", fetchStub);
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);
    await app.updateComplete;
    await vi.waitFor(() => expect(fetchStub.mock.calls.some(([url]) => connectPath(url as string).endsWith("/GetDashboard"))).toBe(true));

    const filter = app.shadowRoot?.querySelector<TimeRangeFilter>("am-time-range-filter");
    await filter?.updateComplete;
    filter?.shadowRoot?.querySelector<HTMLButtonElement>("button[data-range='1h']")?.click();

    await vi.waitFor(() => expect(fetchStub.mock.calls.some((call) => connectPath(call[0] as string).endsWith("/GetDashboard") && connectBody(call).filter.range === "TIME_RANGE_ONE_HOUR")).toBe(true));
    await app.updateComplete;
    await filter?.updateComplete;
    expect(filter?.selected).toBe("1h");
    expect(filter?.shadowRoot?.querySelector("button[data-range='1h']")?.getAttribute("aria-pressed")).toBe("true");
  });

  it("restores shareable list filters from the current URL", async () => {
    history.replaceState({}, "", "/?range=1h&source=codex&q=tool+error");
    vi.stubGlobal("fetch", overviewFetch(emptyOverview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    await app.updateComplete;

    const range = app.shadowRoot?.querySelector<TimeRangeFilter>("am-time-range-filter");
    const workspace = workspaceOf(app);
    expect(range?.selected).toBe("1h");
    expect(workspace?.sourceId).toBe("codex");
    expect(workspace?.search).toBe("tool error");
  });

  it("keeps list filters when a conversation is opened", async () => {
    const overview = {
      ...emptyOverview,
      sessions: [{
        id: "conversation-1", sourceId: "codex", sources: [{ id: "codex", label: "Codex" }],
        startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:01:00Z",
        activityCount: 1, tokens: emptyOverview.tokens, agents: [], activities: [],
      }],
    } as TestOverview;
    vi.stubGlobal("fetch", overviewFetch(overview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    await vi.waitFor(() => expect(workspaceRootOf(app)?.textContent).toContain("conversation-1"));
    const range = app.shadowRoot?.querySelector<TimeRangeFilter>("am-time-range-filter");
    await range?.updateComplete;
    range?.shadowRoot?.querySelector<HTMLButtonElement>("button[data-range='1h']")?.click();
    await vi.waitFor(() => expect(`${location.pathname}${location.search}`).toBe("/?range=1h"));
    expect(app.shadowRoot?.querySelector<HTMLAnchorElement>("a.brand")?.getAttribute("href")).toBe("/?range=1h");
    await vi.waitFor(() => expect(workspaceRootOf(app)?.textContent).toContain("conversation-1"));
    await workspaceOf(app)?.updateComplete;
    const list = workspaceRootOf(app)?.querySelector<SessionList>("am-session-list");
    await vi.waitFor(() => expect(list?.shadowRoot?.querySelector("a")).toBeTruthy());
    const link = list?.shadowRoot?.querySelector<HTMLAnchorElement>("a");
    expect(link?.getAttribute("href")).toBe("/conversations/codex/conversation-1?range=1h");
    link?.click();
    await app.updateComplete;

    expect(`${location.pathname}${location.search}`).toBe("/conversations/codex/conversation-1?range=1h");
  });

  it("keeps the selected conversation when search is cleared", async () => {
    history.replaceState({}, "", "/?q=session-2");
    const sessions = ["session-1", "session-2"].map((id) => ({
      id, sourceId: "codex", sources: [{ id: "codex", label: "Codex" }],
      startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:01:00Z",
      activityCount: 0, tokens: emptyOverview.tokens, agents: [], activities: [],
    }));
    const overview = { ...emptyOverview, sessions } as TestOverview;
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async (url: string, init?: { body?: BodyInit | null }) => {
      const body = init?.body ? JSON.parse(new TextDecoder().decode(init.body as Uint8Array)) as { sessionId?: string } : {};
      const selected = sessions.find(({ id }) => id === body.sessionId) ?? sessions[0];
      switch (connectPath(url).split("/").at(-1)) {
        case "GetDashboard": return connectResponse(dashboardResponse(overview));
        case "ListSessions": return connectResponse(sessionsResponse(overview));
        case "GetSession": return connectResponse({ session: sessionSummary(selected), traceIds: [] });
        case "ListSessionActivities": return connectResponse(activitiesResponse(selected));
        default: return connectResponse({});
      }
    }));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    await vi.waitFor(() => expect(workspaceRootOf(app)?.querySelector("am-session-list")?.shadowRoot?.querySelectorAll("a")).toHaveLength(2));
    const selectedLink = workspaceRootOf(app)?.querySelector("am-session-list")?.shadowRoot
      ?.querySelector<HTMLAnchorElement>('a[href="/conversations/codex/session-2?q=session-2"]');
    selectedLink?.click();
    await vi.waitFor(() => expect(`${location.pathname}${location.search}`).toBe("/conversations/codex/session-2?q=session-2"));

    workspaceOf(app)?.dispatchEvent(new CustomEvent("search-submitted", {
      detail: { search: "" }, bubbles: true, composed: true,
    }));

    await vi.waitFor(() => expect(`${location.pathname}${location.search}`).toBe("/conversations/codex/session-2"));
    await vi.waitFor(() => expect(workspaceRootOf(app)?.querySelector(".session-id")?.textContent).toContain("session-2"));
  });

  it("uses a focused trace view with an honest direct-link return target", async () => {
    history.replaceState({}, "", "/traces/direct-trace?source=codex&q=tool+error");
    vi.stubGlobal("fetch", overviewFetch(emptyOverview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    await app.updateComplete;
    const trace = traceExplorerOf(app);
    await trace?.updateComplete;

    expect(app.shadowRoot?.querySelector("header")).toBeNull();
    expect(app.shadowRoot?.querySelector("am-dashboard-summary")).toBeNull();
    expect(trace?.shadowRoot?.querySelector("h1")?.textContent).toContain("Trace explorer");
    const returnLink = trace?.shadowRoot?.querySelector<HTMLAnchorElement>("a.trace-close");
    expect(returnLink?.textContent).toContain("Conversations");
    expect(returnLink?.getAttribute("href")).toBe("/?source=codex&q=tool+error");
  });

  it("keeps trace navigation at activity level instead of the session header", async () => {
    const overview = {
      ...emptyOverview,
      runCount: 1,
      sessions: [{
        id: "session-1",
        sourceId: "claude",
        sources: [{ id: "claude", label: "Claude Code" }],
        traceIds: ["raw-trace-id-one", "raw-trace-id-two"],
        startedAt: "2026-08-11T00:00:00Z",
        endedAt: "2026-08-11T00:01:00Z",
        activityCount: 0,
        tokens: emptyOverview.tokens,
        agents: [],
        activities: [],
      }],
    };
    vi.stubGlobal("fetch", overviewFetch(overview as TestOverview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    await vi.waitFor(() => expect(workspaceRootOf(app)?.textContent).toContain("Selected conversation"));

    const content = workspaceRootOf(app)?.textContent ?? "";
    expect(content).not.toContain("2 traces");
    expect(content).not.toContain("raw-trace-id-one");
    expect(content).not.toContain("raw-trace-id-two");
    expect(workspaceRootOf(app)?.querySelector(".workspace .detail > .operations-panel am-activity-table")).not.toBeNull();
    const detail = workspaceRootOf(app)?.querySelector(".workspace .detail");
    const children = Array.from(detail?.children ?? []);
    expect(children.findIndex((child) => child.classList.contains("split"))).toBeLessThan(children.findIndex((child) => child.classList.contains("operations-panel")));
  });

  it("shows token totals and observed cost in the selected conversation details", async () => {
    const overview = {
      ...emptyOverview,
      sessions: [{
        id: "session-usage",
        sourceId: "claude",
        sources: [{ id: "claude", label: "Claude Code" }],
        startedAt: "2026-08-11T00:00:00Z",
        endedAt: "2026-08-11T00:01:00Z",
        activityCount: 1,
        tokens: { input: 120, output: 30, cacheRead: null, cacheWrite: null, reasoning: null, total: 150 },
        costUsd: 0.0125,
        agents: [],
        activities: [],
      }],
    } as TestOverview;
    vi.stubGlobal("fetch", overviewFetch(overview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    await vi.waitFor(() => expect(workspaceRootOf(app)?.querySelector(".session-metrics")).toBeTruthy());

    const metricCards = Array.from(workspaceRootOf(app)?.querySelectorAll<KpiCard>(".session-metrics am-kpi-card") ?? []);
    await Promise.all(metricCards.map((card) => card.updateComplete));
    const metrics = metricCards.map((card) => card.shadowRoot?.textContent ?? "").join(" ");
    expect(metrics).toContain("Total tokens");
    expect(metrics).toContain("150");
    expect(metrics).toContain("Input tokens");
    expect(metrics).toContain("120");
    expect(metrics).toContain("Output tokens");
    expect(metrics).toContain("30");
    expect(metrics).toContain("Estimated cost");
    expect(metrics).toContain("$0.0125");
  });

  it("filters operations when an agent graph node is selected", async () => {
    const tokenUsage = { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null };
    const activities = [
      { source: "codex", signal: "trace" as const, traceId: "trace-main", spanId: "span-main", name: "main operation", kind: "tool" as const, agentId: "main", runId: "session-1", model: "model-a", observedAt: "2026-08-11T00:00:01Z", tokens: tokenUsage, contributesToTotal: false },
      { source: "codex", signal: "trace" as const, traceId: "trace-reviewer", spanId: "span-reviewer", name: "review operation", kind: "tool" as const, agentId: "reviewer", runId: "session-1", model: "model-b", observedAt: "2026-08-11T00:00:02Z", tokens: tokenUsage, contributesToTotal: false },
    ];
    const overview = {
      ...emptyOverview,
      agentCount: 2,
      sessions: [{
        id: "session-1", sourceId: "codex", sources: [{ id: "codex", label: "Codex" }], traceIds: [],
        startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:01:00Z", activityCount: 2, tokens: emptyOverview.tokens,
        agents: [
          { agentId: "main", agentDefinition: "orchestrator", agentType: "root", activityCount: 1, tokens: tokenUsage },
          { agentId: "reviewer", agentDefinition: "repository-review", parentAgentId: "main", agentType: "custom", activityCount: 1, tokens: tokenUsage },
        ],
        activities,
      }],
    } as TestOverview;
    vi.stubGlobal("fetch", overviewFetch(overview));
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    await vi.waitFor(() => expect(workspaceRootOf(app)?.querySelector("am-agent-tree")).toBeTruthy());
    const sessionLink = workspaceRootOf(app)?.querySelector("am-session-list")?.shadowRoot?.querySelector<HTMLAnchorElement>("a");
    sessionLink?.click();
    await vi.waitFor(() => expect(location.pathname).toBe("/conversations/codex/session-1"));
    const tree = workspaceRootOf(app)?.querySelector("am-agent-tree");
    await tree?.updateComplete;
    tree?.shadowRoot?.querySelector<HTMLElement>("[data-agent-id='reviewer']")?.click();
    await workspaceOf(app)?.updateComplete;

    await vi.waitFor(() => expect(workspaceRootOf(app)?.querySelector<ActivityTable>("am-activity-table")?.activities).toHaveLength(1));
    const table = workspaceRootOf(app)?.querySelector<ActivityTable>("am-activity-table");
    expect(table?.activities).toHaveLength(1);
    expect(table?.activities[0]?.agentId).toBe("reviewer");
    expect(workspaceRootOf(app)?.textContent).toContain("Filtered by");

    const scrollTo = vi.spyOn(window, "scrollTo").mockImplementation(() => undefined);
    const scrollY = vi.spyOn(window, "scrollY", "get").mockReturnValue(420);
    workspaceOf(app)?.dispatchEvent(new CustomEvent("session-selected", {
      detail: { sourceId: "codex", sessionId: "session-2" }, bubbles: true, composed: true,
    }));
    await vi.waitFor(() => expect(location.pathname).toBe("/conversations/codex/session-2"));
    expect(scrollTo).toHaveBeenCalledWith({ top: 0, behavior: "auto" });

    history.back();
    await vi.waitFor(() => expect(location.pathname).toBe("/conversations/codex/session-1"));
    await vi.waitFor(() => expect(workspaceRootOf(app)?.textContent).toContain("Filtered by"));
    await vi.waitFor(() => expect(scrollTo.mock.calls.some(([options]) => (options as ScrollToOptions).top === 420)).toBe(true));

    workspaceRootOf(app)?.querySelector<HTMLButtonElement>(".agent-filter button")?.click();
    await workspaceOf(app)?.updateComplete;
    expect(workspaceRootOf(app)?.querySelector<ActivityTable>("am-activity-table")?.activities).toHaveLength(2);
    expect(workspaceRootOf(app)?.textContent).not.toContain("Filtered by");
    scrollTo.mockRestore();
    scrollY.mockRestore();
  });

  it("restores a source-qualified conversation and highlighted span from its URL", async () => {
    const traceId = "11111111111111111111111111111111";
    const spanId = "aaaaaaaaaaaaaaaa";
    history.replaceState({}, "", `/conversations/codex/conversation-1?traceId=${traceId}&spanId=${spanId}`);
    const targetActivity = {
      source: "codex", signal: "trace" as const, traceId, spanId, name: "target operation", kind: "tool" as const,
      agentId: "reviewer", runId: "conversation-1", model: "model-b", observedAt: "2026-08-11T00:00:01Z", contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    };
    const overviewSessions = [
      { id: "conversation-1", sourceId: "claude", sources: [], traceIds: [], startedAt: "", endedAt: "", activityCount: 0, tokens: emptyOverview.tokens, agents: [], activities: [] },
    ];
    const exactConversation = {
      id: "conversation-1", sourceId: "codex", sources: [], traceIds: [traceId],
      startedAt: targetActivity.observedAt, endedAt: targetActivity.observedAt, activityCount: 101,
      tokens: emptyOverview.tokens,
      agents: [{ agentId: "reviewer", agentDefinition: "repository-review", agentType: "custom", activityCount: 101, tokens: emptyOverview.tokens }],
      activities: [targetActivity],
    };
    const exactOverview = { ...emptyOverview, sessions: overviewSessions } as TestOverview;
    const fetchStub = vi.fn().mockImplementation(async (url: string) => {
      switch (connectPath(url).split("/").at(-1)) {
        case "GetDashboard": return connectResponse(dashboardResponse(exactOverview));
        case "ListSessions": return connectResponse(sessionsResponse(exactOverview));
        case "GetSession": return connectResponse({ session: exactConversation, traceIds: [traceId] });
        case "ListSessionActivities": return connectResponse({ activities: [targetActivity], page: { hasMore: false }, total: 101 });
        default: return connectResponse({});
      }
    });
    vi.stubGlobal("fetch", fetchStub);
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    await vi.waitFor(() => expect(workspaceRootOf(app)?.querySelector("am-activity-table")).toBeTruthy());

    const list = workspaceRootOf(app)?.querySelector<SessionList>("am-session-list");
    const table = workspaceRootOf(app)?.querySelector<ActivityTable>("am-activity-table");
    expect(list?.selected).toBe("conversation-1");
    expect(list?.selectedSource).toBe("codex");
    expect(table?.highlightedTraceId).toBe(traceId);
    expect(table?.highlightedSpanId).toBe(spanId);
    expect(table?.activities[0]?.spanId).toBe(spanId);
    expect(fetchStub.mock.calls.some(([url]) => connectPath(url as string).endsWith("/GetSession"))).toBe(true);

    history.replaceState({ view: { selectedAgentId: "reviewer" } }, "", `${location.pathname}${location.search}`);
    window.dispatchEvent(new PopStateEvent("popstate", { state: history.state }));
    await vi.waitFor(() => expect(workspaceRootOf(app)?.textContent).toContain("Filtered by"));
    await vi.waitFor(() => expect(workspaceOf(app)?.shadowRoot?.activeElement?.classList.contains("session-id")).toBe(true));
    expect(document.title).toContain("Conversation conversation-1");
  });

  it("exposes an activity trace route, loads it, and returns to the conversation", async () => {
    const activity = {
      source: "claude",
      signal: "trace" as const,
      traceId: "trace-123456789",
      spanId: "span-1",
      name: "root operation",
      kind: "tool" as const,
      agentId: "main",
      runId: "conversation-1",
      model: "model-a",
      startedAt: "2026-08-11T00:00:00Z",
      endedAt: "2026-08-11T00:00:01Z",
      observedAt: "2026-08-11T00:00:01Z",
      status: "Ok",
      contributesToTotal: false,
      tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
    };
    const overview = {
      ...emptyOverview,
      runCount: 1,
      sessions: [{
        id: "conversation-1",
        sourceId: "claude",
        sources: [{ id: "claude", label: "Claude Code" }],
        traceIds: [activity.traceId],
        startedAt: activity.startedAt,
        endedAt: activity.endedAt,
        activityCount: 1,
        tokens: emptyOverview.tokens,
        agents: [],
        activities: [activity],
      }],
    };
    const trace = {
      traceId: activity.traceId,
      startedAt: activity.startedAt,
      endedAt: activity.endedAt,
      status: "ok",
      rootSpanCount: 1,
      missingParentCount: 0,
      conversations: [{ sourceId: "claude", id: "conversation-1" }],
      agents: [],
      activities: [activity],
    };
    const fetchStub = vi.fn().mockImplementation(async (url: string) => {
      switch (connectPath(url).split("/").at(-1)) {
        case "GetDashboard": return connectResponse(dashboardResponse(overview as TestOverview));
        case "ListSessions": return connectResponse(sessionsResponse(overview as TestOverview));
        case "GetSession": return connectResponse({ session: sessionSummary(overview.sessions[0] as TestSession), traceIds: [activity.traceId] });
        case "ListSessionActivities": return connectResponse(activitiesResponse(overview.sessions[0] as TestSession));
        case "GetTrace": return connectResponse(trace);
        default: return connectResponse({});
      }
    });
    vi.stubGlobal("fetch", fetchStub);
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);
    await vi.waitFor(() => expect(workspaceRootOf(app)?.querySelector("am-activity-table")?.shadowRoot?.querySelector("a.trace")).toBeTruthy());

    const activityTable = workspaceRootOf(app)?.querySelector("am-activity-table");
    const traceLink = activityTable?.shadowRoot?.querySelector<HTMLAnchorElement>("a.trace");
    expect(traceLink?.getAttribute("href")).toBe("/traces/trace-123456789");
    traceLink?.click();

    await vi.waitFor(() => expect(fetchStub.mock.calls.some(([url]) => connectPath(url as string).endsWith("/GetTrace"))).toBe(true));
    await vi.waitFor(() => expect(traceRootOf(app)?.textContent).toContain("Trace explorer"));
    await vi.waitFor(() => expect(traceRootOf(app)?.querySelector("am-trace-summary")).not.toBeNull());
    expect(traceRootOf(app)?.querySelector("am-trace-waterfall")).not.toBeNull();
    expect(location.pathname).toBe("/traces/trace-123456789");

    const closeLink = traceRootOf(app)?.querySelector<HTMLAnchorElement>("a.trace-close");
    expect(closeLink?.getAttribute("href")).toBe("/conversations/claude/conversation-1?traceId=trace-123456789&spanId=span-1");
    expect(closeLink?.textContent).toContain("Conversation conversation-1");

    const scrollTo = vi.spyOn(window, "scrollTo").mockImplementation(() => undefined);
    const scrollY = vi.spyOn(window, "scrollY", "get").mockReturnValue(640);
    const conversationLink = traceRootOf(app)?.querySelector<HTMLAnchorElement>("am-trace-waterfall")?.shadowRoot?.querySelector<HTMLAnchorElement>("a.conversation");
    conversationLink?.click();
    await vi.waitFor(() => expect(location.pathname).toBe("/conversations/claude/conversation-1"));
    expect(location.search).toBe("?traceId=trace-123456789&spanId=span-1");
    await vi.waitFor(() => expect(workspaceRootOf(app)?.textContent).toContain("Selected conversation"));
    await vi.waitFor(() => expect(workspaceRootOf(app)?.querySelector("a.context-return")).not.toBeNull());
    const traceReturn = workspaceRootOf(app)?.querySelector<HTMLAnchorElement>("a.context-return");
    expect(traceReturn?.getAttribute("href")).toBe("/traces/trace-123456789");
    expect(traceReturn?.textContent).toContain("Trace trace-123456789");
    traceReturn?.click();
    await vi.waitFor(() => expect(location.pathname).toBe("/traces/trace-123456789"));
    await vi.waitFor(() => expect(traceRootOf(app)?.querySelector("a.trace-close")).not.toBeNull());
    await vi.waitFor(() => expect(scrollTo.mock.calls.some(([options]) => (options as ScrollToOptions).top === 640)).toBe(true));

    const restoredCloseLink = traceRootOf(app)?.querySelector<HTMLAnchorElement>("a.trace-close");
    expect(restoredCloseLink?.getAttribute("href")).toBe("/conversations/claude/conversation-1?traceId=trace-123456789&spanId=span-1");
    restoredCloseLink?.click();
    await vi.waitFor(() => expect(workspaceRootOf(app)?.textContent).toContain("Selected conversation"));
    expect(location.pathname).toBe("/conversations/claude/conversation-1");
    expect(location.search).toBe("?traceId=trace-123456789&spanId=span-1");
    expect(workspaceRootOf(app)?.querySelector(".workspace")?.getAttribute("data-view")).toBe("detail");
    scrollTo.mockRestore();
    scrollY.mockRestore();
  });

  it("loads a trace from a reload-safe deep link", async () => {
    history.replaceState({}, "", "/traces/direct-trace");
    const overview = {
      ...emptyOverview,
      sessions: [{
        id: "background-conversation", sourceId: "codex", sources: [{ id: "codex", label: "Codex" }],
        startedAt: "2026-08-11T00:00:00Z", endedAt: "2026-08-11T00:01:00Z", activityCount: 3,
        tokens: emptyOverview.tokens, agents: [], activities: [],
      }],
    } as TestOverview;
    const trace = {
      traceId: "direct-trace",
      startedAt: "2026-08-11T00:00:00Z",
      endedAt: "2026-08-11T00:00:01Z",
      status: "unknown",
      rootSpanCount: 0,
      missingParentCount: 0,
      conversations: [],
      agents: [],
      activities: [],
    };
    const fetchStub = vi.fn().mockImplementation(async (url: string) => {
      switch (connectPath(url).split("/").at(-1)) {
        case "GetDashboard": return connectResponse(dashboardResponse(overview));
        case "ListSessions": return connectResponse(sessionsResponse(overview));
        case "GetTrace": return connectResponse(trace);
        default: return connectResponse({});
      }
    });
    vi.stubGlobal("fetch", fetchStub);
    const app = document.createElement("am-app") as AgentmetryApp;
    document.body.append(app);

    await vi.waitFor(() => expect(fetchStub.mock.calls.some(([url]) => connectPath(url as string).endsWith("/GetTrace"))).toBe(true));
    await vi.waitFor(() => expect(traceRootOf(app)?.querySelector("am-trace-summary")?.shadowRoot?.textContent).toContain("direct-trace"));
    expect(dashboardOf(app)).toBeNull();
    expect(fetchStub.mock.calls.some(([url]) => connectPath(url as string).endsWith("/GetDashboard"))).toBe(false);
    expect(fetchStub.mock.calls.some(([url]) => connectPath(url as string).endsWith("/ListSessions"))).toBe(false);
    expect(fetchStub.mock.calls.some(([url]) => connectPath(url as string).endsWith("/GetSession"))).toBe(false);
  });
});
