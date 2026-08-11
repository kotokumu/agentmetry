import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt";
import {
  AgentmetryQueryService,
  PageDirection,
  TimeRange,
  type Activity as ActivityMessage,
  type AgentSummary,
  type Dashboard,
  type PlanUsageSnapshot,
  type SessionSummary,
  type TokenUsage as TokenUsageMessage,
} from "../gen/agentmetry/v1/agentmetry_pb";
import type {
  Activity,
  ActivityDirection,
  AgentSession,
  Overview,
  PlanUsageSnapshot as PlanUsage,
  Session,
  TimeRange as UiTimeRange,
  TokenUsage,
  Trace,
} from "../model/update";

const transport = createConnectTransport({ baseUrl: "" });
const client = createClient(AgentmetryQueryService, transport);

export type ActivityPage = Readonly<{
  activities: readonly Activity[];
  total: number;
  offset: number;
  hasEarlier: boolean;
  hasMore: boolean;
  nextPageToken?: string;
  previousPageToken?: string;
}>;

export const agentmetryClient = {
  async loadOverview(range: UiTimeRange, sourceId: string, search: string): Promise<Overview> {
    const filter = { range: toTimeRange(range), sourceId, search };
    const [dashboardResponse, sessionsResponse] = await Promise.all([
      client.getDashboard({ filter }),
      client.listSessions({ filter, page: { pageSize: 100 } }),
    ]);
    const dashboard = dashboardResponse.dashboard;
    if (!dashboard) throw new Error("Dashboard response was empty");
    const sessions = sessionsResponse.sessions.map((session) => mapSession(session));
    const first = sessions[0];
    if (first) {
      sessions[0] = await this.getSession(first.sourceId, first.id);
    }
    return mapOverview(dashboard, sessions);
  },

  async getSession(sourceId: string, sessionId: string, traceId?: string, spanId?: string): Promise<Session> {
    const response = await client.getSession({ sourceId, sessionId });
    if (!response.session) throw new Error("Session response was empty");
    const session = mapSession(response.session, response.traceIds);
    const page = await this.listSessionActivities(sourceId, sessionId, "older", 0, 100, "", traceId, spanId);
    return { ...session, activities: page.activities, activityOffset: page.offset, hasEarlier: page.hasEarlier, hasMore: page.hasMore, nextPageToken: page.nextPageToken, previousPageToken: page.previousPageToken };
  },

  async listSessionActivities(sourceId: string, sessionId: string, direction: ActivityDirection, offset: number, limit: number, pageToken = "", traceId?: string, spanId?: string, agentId?: string): Promise<ActivityPage> {
    const response = await client.listSessionActivities({
      sourceId,
      sessionId,
      page: { pageSize: limit, pageToken },
      direction: direction === "newer" ? PageDirection.NEWER : PageDirection.OLDER,
      anchor: traceId && spanId ? { traceId, spanId } : undefined,
      agentId: agentId || "",
    });
    const page = response.page;
    const actualOffset = Number(page?.startOffset ?? offset);
    return {
      activities: response.activities.map(mapActivity),
      total: Number(response.total),
      offset: actualOffset,
      hasEarlier: actualOffset > 0,
      hasMore: page?.hasMore ?? false,
      nextPageToken: page?.nextPageToken || undefined,
      previousPageToken: page?.previousPageToken || undefined,
    };
  },

  async getTrace(traceId: string, offset = 0, limit = 100, pageToken = ""): Promise<Trace> {
    const response = await client.getTrace({ traceId, page: { pageSize: limit, pageToken } });
    const page = response.page;
    const actualOffset = Number(page?.startOffset ?? offset);
    return {
      traceId: response.traceId,
      startedAt: timeValue(response.startedAt),
      endedAt: timeValue(response.endedAt),
      status: response.status,
      rootSpanCount: Number(response.rootSpanCount),
      missingParentCount: Number(response.missingParentCount),
      conversations: response.conversations.map((value) => ({ sourceId: value.sourceId, id: value.id })),
      agents: response.agents.map((value) => ({
        sourceId: value.sourceId,
        conversationId: value.conversationId,
        agentId: value.agentId,
        agentDefinition: value.agentDefinition || undefined,
        agentType: value.agentType || undefined,
        parentAgentId: value.parentAgentId || undefined,
        model: value.model || undefined,
      })),
      activities: response.activities.map(mapActivity),
      activityOffset: actualOffset,
      activityCount: Number(response.totalActivities),
      hasMore: page?.hasMore ?? false,
      nextPageToken: page?.nextPageToken || undefined,
      previousPageToken: page?.previousPageToken || undefined,
    };
  },
};

function mapOverview(value: Dashboard, sessions: readonly Session[]): Overview {
  return {
    sources: value.sources.map((source) => ({ id: source.id, label: source.label })),
    signalCounts: {
      traces: Number(value.signalCounts?.traces ?? 0),
      logs: Number(value.signalCounts?.logs ?? 0),
      metrics: Number(value.signalCounts?.metrics ?? 0),
    },
    runCount: Number(value.runCount),
    agentCount: Number(value.agentCount),
    tokens: mapTokens(value.tokens),
    recentActivity: value.recentActivity.map(mapActivity),
    sessions,
    planUsage: value.planUsage.map(mapPlanUsage),
  };
}

function mapSession(value: SessionSummary, traceIds: readonly string[] = []): Session {
  return {
    id: value.id,
    sourceId: value.sourceId,
    sources: value.sources.map((source) => ({ id: source.id, label: source.label })),
    traceIds,
    startedAt: timeValue(value.startedAt),
    endedAt: timeValue(value.endedAt),
    activityCount: Number(value.activityCount),
    agentCount: Number(value.agentCount),
    tokens: mapTokens(value.tokens),
    costUsd: value.costUsd,
    agents: value.agents.map(mapAgent),
    activities: [],
  };
}

function mapAgent(value: AgentSummary): AgentSession {
  return {
    agentId: value.agentId,
    agentDefinition: value.agentDefinition || undefined,
    agentType: value.agentType || undefined,
    parentAgentId: value.parentAgentId || undefined,
    model: value.model || undefined,
    activityCount: Number(value.activityCount),
    tokens: mapTokens(value.tokens),
  };
}

function mapActivity(value: ActivityMessage): Activity {
  return {
    source: value.source,
    signal: value.signal as Activity["signal"],
    traceId: value.traceId || undefined,
    spanId: value.spanId || undefined,
    parentSpanId: value.parentSpanId || undefined,
    promptId: value.promptId || undefined,
    usageId: value.usageId || undefined,
    relatedTraceId: value.relatedTraceId || undefined,
    relatedSpanId: value.relatedSpanId || undefined,
    name: value.name,
    kind: value.kind as Activity["kind"],
    toolName: value.toolName || undefined,
    targetAgentId: value.targetAgentId || undefined,
    targetAgentType: value.targetAgentType || undefined,
    content: value.content || undefined,
    agentId: value.agentId,
    agentDefinition: value.agentDefinition || undefined,
    agentType: value.agentType || undefined,
    parentAgentId: value.parentAgentId || undefined,
    runId: value.runId,
    model: value.model,
    startedAt: timeValue(value.startedAt),
    endedAt: timeValue(value.endedAt),
    observedAt: timeValue(value.observedAt),
    status: value.status || undefined,
    tokens: mapTokens(value.tokens),
    costUsd: value.costUsd,
    contributesToTotal: value.contributesToTotal,
  };
}

function mapTokens(value?: TokenUsageMessage): TokenUsage {
  return {
    input: value?.input === undefined ? null : Number(value.input),
    output: value?.output === undefined ? null : Number(value.output),
    cacheRead: value?.cacheRead === undefined ? null : Number(value.cacheRead),
    cacheWrite: value?.cacheWrite === undefined ? null : Number(value.cacheWrite),
    reasoning: value?.reasoning === undefined ? null : Number(value.reasoning),
    total: value?.total === undefined ? null : Number(value.total),
  };
}

function mapPlanUsage(value: PlanUsageSnapshot): PlanUsage {
  return {
    source: value.source,
    accountId: value.accountId || undefined,
    plan: value.plan || undefined,
    windowId: value.windowId,
    windowDurationMinutes: value.windowDurationMinutes || undefined,
    usedPercent: value.usedPercent,
    resetsAt: timeValue(value.resetsAt),
    capturedAt: timeValue(value.capturedAt),
    authority: value.authority,
  };
}

function toTimeRange(value: UiTimeRange): TimeRange {
  switch (value) {
    case "1h": return TimeRange.ONE_HOUR;
    case "7d": return TimeRange.SEVEN_DAYS;
    default: return TimeRange.ONE_DAY;
  }
}

function timeValue(value: { seconds: bigint; nanos: number } | undefined): string {
  return value ? timestampDate(value as Timestamp).toISOString() : "";
}
