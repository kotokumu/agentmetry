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
  DashboardSummary,
  PlanUsageSnapshot as PlanUsage,
  ReworkAnalysis,
  Session,
  TimeRange as UiTimeRange,
  TokenUsage,
  Trace,
} from "../model/telemetry";

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
  async getDashboard(range: UiTimeRange, sourceId: string, search: string, signal?: AbortSignal): Promise<DashboardSummary> {
    const response = await client.getDashboard(
      { filter: { range: toTimeRange(range), sourceId, search } },
      signal ? { signal } : undefined,
    );
    if (!response.dashboard) throw new Error("Dashboard response was empty");
    return mapDashboard(response.dashboard);
  },

  async listSessions(range: UiTimeRange, sourceId: string, search: string, signal?: AbortSignal): Promise<readonly Session[]> {
    const response = await client.listSessions(
      { filter: { range: toTimeRange(range), sourceId, search }, page: { pageSize: 100 } },
      signal ? { signal } : undefined,
    );
    return response.sessions.map((session) => mapSession(session));
  },

  async getSession(sourceId: string, sessionId: string, traceId?: string, spanId?: string, signal?: AbortSignal): Promise<Session> {
    const response = await client.getSession({ sourceId, sessionId }, signal ? { signal } : undefined);
    if (!response.session) throw new Error("Session response was empty");
    const session = mapSession(response.session, response.traceIds);
    const page = await this.listSessionActivities(sourceId, sessionId, "older", 0, 100, "", traceId, spanId, undefined, signal);
    return { ...session, activities: page.activities, activityOffset: page.offset, hasEarlier: page.hasEarlier, hasMore: page.hasMore, nextPageToken: page.nextPageToken, previousPageToken: page.previousPageToken };
  },

  async getSessionRework(sourceId: string, sessionId: string, signal?: AbortSignal): Promise<ReworkAnalysis> {
    const response = await client.getSessionRework({ sourceId, sessionId }, signal ? { signal } : undefined);
    if (!response.metrics || !response.coverage || !response.capabilities?.changeRevert || !response.capabilities.crossAgentOverlap) {
      throw new Error("Session rework response was incomplete");
    }
    const metrics = response.metrics;
    return {
      sourceId: response.sourceId,
      sessionId: response.sessionId,
      metrics: {
        validationFailures: Number(metrics.validationFailures),
        failFixRetryCycles: Number(metrics.failFixRetryCycles),
        reworkDurationMs: Number(metrics.reworkDurationMs),
        reworkTokens: mapTokens(metrics.reworkTokens),
        toolAttemptsWithOutcome: Number(metrics.toolAttemptsWithOutcome),
        toolFailures: Number(metrics.toolFailures),
        toolFailureRate: metrics.toolFailureRate === undefined ? null : metrics.toolFailureRate,
        apiRetryWaste: {
          attempts: Number(metrics.apiRetryWaste?.attempts ?? 0),
          durationMs: Number(metrics.apiRetryWaste?.durationMs ?? 0),
          tokens: mapTokens(metrics.apiRetryWaste?.tokens),
        },
        repeatedCommands: Number(metrics.repeatedCommands),
        reeditedFiles: Number(metrics.reeditedFiles),
      },
      coverage: {
        activityCoverage: response.coverage.activityCoverage,
        canonicalEvents: Number(response.coverage.canonicalEvents),
        classifiedEvents: Number(response.coverage.classifiedEvents),
        knownOutcomes: Number(response.coverage.knownOutcomes),
      },
      capabilities: {
        changeRevert: { state: response.capabilities.changeRevert.state, reason: response.capabilities.changeRevert.reason },
        crossAgentOverlap: { state: response.capabilities.crossAgentOverlap.state, reason: response.capabilities.crossAgentOverlap.reason },
      },
    };
  },

  async listSessionActivities(sourceId: string, sessionId: string, direction: ActivityDirection, offset: number, limit: number, pageToken = "", traceId?: string, spanId?: string, agentId?: string, signal?: AbortSignal): Promise<ActivityPage> {
    const response = await client.listSessionActivities({
      sourceId,
      sessionId,
      page: { pageSize: limit, pageToken },
      direction: direction === "newer" ? PageDirection.NEWER : PageDirection.OLDER,
      anchor: traceId && spanId ? { traceId, spanId } : undefined,
      agentId: agentId || "",
    }, signal ? { signal } : undefined);
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

  async getTrace(traceId: string, offset = 0, limit = 100, pageToken = "", signal?: AbortSignal): Promise<Trace> {
    const response = await client.getTrace({ traceId, page: { pageSize: limit, pageToken } }, signal ? { signal } : undefined);
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

export type AgentmetryClient = typeof agentmetryClient;

function mapDashboard(value: Dashboard): DashboardSummary {
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
