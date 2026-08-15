import { afterEach, describe, expect, it, vi } from "vitest";
import "./agentmetry-app";
import { shouldReturnThroughHistory, type AgentmetryApp } from "./agentmetry-app";
import type { TimeRangeFilter } from "../components/time-range-filter";
import type { SessionList } from "../components/session-list";
import type { ActivityTable } from "../components/activity-table";
import type { KpiCard } from "../components/kpi-card";
import type { ConversationWorkspace } from "../components/conversation-workspace";
import type { TraceExplorer } from "../components/trace-explorer";
import type { DashboardSummary } from "../components/dashboard-summary";
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

const connectPath = (url: string) => new URL(url, "http://localhost").pathname;
const connectBody = (call: readonly unknown[]) => JSON.parse(new TextDecoder().decode((call[1] as { body: Uint8Array }).body));
const workspaceOf = (app: AgentmetryApp) => app.shadowRoot?.querySelector<ConversationWorkspace>("am-conversation-workspace");
const workspaceRootOf = (app: AgentmetryApp) => workspaceOf(app)?.shadowRoot;
const traceExplorerOf = (app: AgentmetryApp) => app.shadowRoot?.querySelector<TraceExplorer>("am-trace-explorer");
const traceRootOf = (app: AgentmetryApp) => traceExplorerOf(app)?.shadowRoot;
const dashboardOf = (app: AgentmetryApp) => app.shadowRoot?.querySelector<DashboardSummary>("am-dashboard-summary");

const overviewFetch = (overview: TestOverview) => vi.fn().mockImplementation(async (url: string, init?: { body?: BodyInit | null }) => {
  const body = init?.body ? JSON.parse(new TextDecoder().decode(init.body as Uint8Array)) as { agentId?: string } : {};
  switch (connectPath(url).split("/").at(-1)) {
    case "GetDashboard": return connectResponse(dashboardResponse(overview));
    case "ListSessions": return connectResponse(sessionsResponse(overview));
    case "GetSession": return connectResponse(overview.sessions[0] ? { session: sessionSummary(overview.sessions[0]), traceIds: overview.sessions[0].traceIds ?? [] } : {});
    case "ListSessionActivities": return connectResponse(overview.sessions[0] ? activitiesResponse(overview.sessions[0], body.agentId) : { activities: [], page: { hasMore: false }, total: 0 });
    default: return connectResponse({});
  }
});

afterEach(() => {
  document.body.replaceChildren();
  vi.unstubAllGlobals();
  history.replaceState({}, "", "/");
});

describe("Agentmetry app composition", () => {
  it("uses browser history only for a same-origin dashboard referrer", () => {
    expect(shouldReturnThroughHistory("http://127.0.0.1:18080/", "http://127.0.0.1:18080", 2)).toBe(true);
    expect(shouldReturnThroughHistory("http://127.0.0.1:18080/conversations/example/conversation-1?traceId=11111111111111111111111111111111&spanId=aaaaaaaaaaaaaaaa", "http://127.0.0.1:18080", 2)).toBe(true);
    expect(shouldReturnThroughHistory("http://127.0.0.1:18080/", "http://127.0.0.1:18080", 1)).toBe(false);
    expect(shouldReturnThroughHistory("", "http://127.0.0.1:18080", 2)).toBe(false);
    expect(shouldReturnThroughHistory("https://example.com/", "http://127.0.0.1:18080", 2)).toBe(false);
    expect(shouldReturnThroughHistory("http://127.0.0.1:18080/traces/abc", "http://127.0.0.1:18080", 2)).toBe(false);
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
    expect(app.shadowRoot?.querySelector("am-trace-explorer")).not.toBeNull();
    expect(app.shadowRoot?.querySelector("am-app-update-control")).not.toBeNull();
    expect(app.shadowRoot?.querySelector(".kpis")).toBeNull();
    expect(app.shadowRoot?.querySelector(".workspace")).toBeNull();
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
    const tree = workspaceRootOf(app)?.querySelector("am-agent-tree");
    await tree?.updateComplete;
    tree?.shadowRoot?.querySelector<HTMLElement>("[data-agent-id='reviewer']")?.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    await workspaceOf(app)?.updateComplete;

    await vi.waitFor(() => expect(workspaceRootOf(app)?.querySelector<ActivityTable>("am-activity-table")?.activities).toHaveLength(1));
    const table = workspaceRootOf(app)?.querySelector<ActivityTable>("am-activity-table");
    expect(table?.activities).toHaveLength(1);
    expect(table?.activities[0]?.agentId).toBe("reviewer");
    expect(workspaceRootOf(app)?.textContent).toContain("Filtered by");

    workspaceRootOf(app)?.querySelector<HTMLButtonElement>(".agent-filter button")?.click();
    await workspaceOf(app)?.updateComplete;
    expect(workspaceRootOf(app)?.querySelector<ActivityTable>("am-activity-table")?.activities).toHaveLength(2);
    expect(workspaceRootOf(app)?.textContent).not.toContain("Filtered by");
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
    const exactConversation = { id: "conversation-1", sourceId: "codex", sources: [], traceIds: [traceId], startedAt: targetActivity.observedAt, endedAt: targetActivity.observedAt, activityCount: 101, tokens: emptyOverview.tokens, agents: [], activities: [targetActivity] };
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
    expect(activityTable?.shadowRoot?.querySelector<HTMLAnchorElement>("a.trace")?.getAttribute("href")).toBe("/traces/trace-123456789");
    history.pushState({}, "", "/traces/trace-123456789");
    window.dispatchEvent(new PopStateEvent("popstate"));

    await vi.waitFor(() => expect(fetchStub.mock.calls.some(([url]) => connectPath(url as string).endsWith("/GetTrace"))).toBe(true));
    await vi.waitFor(() => expect(traceRootOf(app)?.textContent).toContain("Trace explorer"));
    await vi.waitFor(() => expect(traceRootOf(app)?.querySelector("am-trace-summary")).not.toBeNull());
    expect(traceRootOf(app)?.querySelector("am-trace-waterfall")).not.toBeNull();
    expect(location.pathname).toBe("/traces/trace-123456789");

    const closeLink = traceRootOf(app)?.querySelector<HTMLAnchorElement>("a.trace-close");
    expect(closeLink?.getAttribute("href")).toBe("/");
    closeLink?.click();
    await vi.waitFor(() => expect(workspaceRootOf(app)?.textContent).toContain("Selected conversation"));
    expect(location.pathname).toBe("/");
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
    await vi.waitFor(() => expect(dashboardOf(app)?.conversationCount).toBe(1));
    expect(dashboardOf(app)?.activityCount).toBe(3);
    expect(fetchStub.mock.calls.some(([url]) => connectPath(url as string).endsWith("/GetSession"))).toBe(false);
  });
});
