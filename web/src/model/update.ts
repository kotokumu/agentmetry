import type { ConversationTarget } from "./trace-analysis";

export type TimeRange = "1h" | "24h" | "7d";
export type ActivityDirection = "newer" | "older";

export type TelemetrySource = Readonly<{ id: string; label: string }>;

export type TokenUsage = Readonly<{
  input: number | null;
  output: number | null;
  cacheRead: number | null;
  cacheWrite: number | null;
  reasoning: number | null;
  total: number | null;
}>;

export type Activity = Readonly<{
  source: string;
  signal: "trace" | "log" | "metric";
  name: string;
  traceId?: string;
  spanId?: string;
  parentSpanId?: string;
  promptId?: string;
  usageId?: string;
  relatedTraceId?: string;
  relatedSpanId?: string;
  kind: "unknown" | "prompt" | "response" | "tool" | "delegation" | "message" | "reasoning";
  toolName?: string;
  targetAgentId?: string;
  targetAgentType?: string;
  content?: string;
  agentId: string;
  agentDefinition?: string;
  agentType?: string;
  parentAgentId?: string;
  runId: string;
  model: string;
  startedAt?: string;
  endedAt?: string;
  observedAt: string;
  status?: string;
  tokens: TokenUsage;
  costUsd?: number;
  contributesToTotal: boolean;
}>;

export type PlanUsageSnapshot = Readonly<{
  source: string;
  accountId?: string;
  plan?: string;
  windowId: string;
  windowDurationMinutes?: number;
  usedPercent: number;
  resetsAt?: string;
  capturedAt: string;
  authority: string;
}>;

export type AgentSession = Readonly<{
  agentId: string;
  agentDefinition?: string;
  agentType?: string;
  parentAgentId?: string;
  model?: string;
  activityCount: number;
  tokens: TokenUsage;
}>;

export type Session = Readonly<{
  id: string;
  sourceId: string;
  sources: readonly TelemetrySource[];
  traceIds: readonly string[];
  startedAt: string;
  endedAt: string;
  activityCount: number;
  agentCount?: number;
  tokens: TokenUsage;
  costUsd?: number;
  agents: readonly AgentSession[];
  activities: readonly Activity[];
  activityOffset?: number;
  hasEarlier?: boolean;
  hasMore?: boolean;
  nextPageToken?: string;
  previousPageToken?: string;
}>;

export type ConversationRef = Readonly<{ sourceId: string; id: string }>;

export type TraceAgent = Readonly<{
  sourceId: string;
  conversationId: string;
  agentId: string;
  agentDefinition?: string;
  agentType?: string;
  parentAgentId?: string;
  model?: string;
}>;

export type Trace = Readonly<{
  traceId: string;
  startedAt: string;
  endedAt: string;
  status: string;
  rootSpanCount: number;
  missingParentCount: number;
  conversations: readonly ConversationRef[];
  agents: readonly TraceAgent[];
  activities: readonly Activity[];
  activityOffset: number;
  activityCount: number;
  hasMore: boolean;
  nextPageToken?: string;
  previousPageToken?: string;
}>;

export type Overview = Readonly<{
  sources: readonly TelemetrySource[];
  signalCounts: Readonly<{ traces: number; logs: number; metrics: number }>;
  runCount: number;
  agentCount: number;
  tokens: TokenUsage;
  recentActivity: readonly Activity[];
  sessions: readonly Session[];
  planUsage: readonly PlanUsageSnapshot[];
}>;

export type Model = Readonly<{
  status: "idle" | "loading" | "ready" | "failed";
  range: TimeRange;
  sourceId: string;
  search: string;
  requestGeneration: number;
  overview?: Overview;
  selectedSessionId?: string;
  selectedSessionSourceId?: string;
  requestedConversation?: ConversationTarget;
  routedConversation?: Session;
  highlightedSpanId?: string;
  conversationStatus: "idle" | "loading" | "ready" | "failed";
  conversationRequestGeneration: number;
  conversationError?: string;
  activityPage?: Readonly<{
    sessionId: string;
    sourceId: string;
    direction: ActivityDirection;
    exact: boolean;
    offset: number;
    loading: boolean;
    error?: string;
  }>;
  selectedTraceId?: string;
  traceStatus: "idle" | "loading" | "ready" | "failed";
  traceRequestGeneration: number;
  trace?: Trace;
  traceError?: string;
  error?: string;
}>;

export type Message =
  | Readonly<{ type: "connected" }>
  | Readonly<{ type: "range-selected"; range: TimeRange }>
  | Readonly<{ type: "source-selected"; sourceId: string }>
  | Readonly<{ type: "search-submitted"; search: string }>
  | Readonly<{ type: "conversation-route-selected"; target: ConversationTarget }>
  | Readonly<{ type: "conversation-route-cleared" }>
  | Readonly<{ type: "conversation-received"; generation: number; target: ConversationTarget; conversation: Session }>
  | Readonly<{ type: "conversation-failed"; generation: number; target: ConversationTarget; error: string }>
  | Readonly<{ type: "session-selected"; sessionId: string; sourceId: string }>
  | Readonly<{ type: "activities-requested"; sessionId: string; sourceId: string; direction: ActivityDirection }>
  | Readonly<{ type: "activities-received"; generation: number; sessionId: string; sourceId: string; direction: ActivityDirection; exact: boolean; offset: number; activities: readonly Activity[]; total: number; hasEarlier: boolean; hasMore: boolean; nextPageToken?: string; previousPageToken?: string }>
  | Readonly<{ type: "activities-failed"; generation: number; sessionId: string; sourceId: string; direction: ActivityDirection; exact: boolean; offset: number; error: string }>
  | Readonly<{ type: "trace-selected"; traceId: string }>
  | Readonly<{ type: "trace-activities-requested"; traceId: string; offset: number; pageToken: string }>
  | Readonly<{ type: "trace-received"; generation: number; traceId: string; trace: Trace }>
  | Readonly<{ type: "trace-failed"; generation: number; traceId: string; error: string }>
  | Readonly<{ type: "trace-closed" }>
  | Readonly<{ type: "overview-received"; generation: number; overview: Overview }>
  | Readonly<{ type: "overview-failed"; generation: number; error: string }>;

export type Effect =
  | Readonly<{
    type: "fetch-conversation";
    generation: number;
    target: ConversationTarget;
  }>
  | Readonly<{
    type: "fetch-overview";
    generation: number;
    range: TimeRange;
    sourceId: string;
    search: string;
  }>
  | Readonly<{
    type: "fetch-activities";
    generation: number;
    range: TimeRange;
    sourceId: string;
    search: string;
    sessionId: string;
    direction: ActivityDirection;
    exact: boolean;
    offset: number;
    limit: number;
    pageToken: string;
    traceId?: string;
    spanId?: string;
  }>
  | Readonly<{
    type: "fetch-trace";
    generation: number;
    traceId: string;
    offset: number;
    limit: number;
    pageToken: string;
  }>;

export const initialModel = (): Model => ({
  status: "idle",
  range: "24h",
  sourceId: "",
  search: "",
  requestGeneration: 0,
  conversationStatus: "idle",
  conversationRequestGeneration: 0,
  traceStatus: "idle",
  traceRequestGeneration: 0,
});

export const update = (
  model: Model,
  message: Message,
): readonly [Model, readonly Effect[]] => {
  switch (message.type) {
    case "connected":
      return requestOverview(model);
    case "range-selected":
      return requestOverview(model, { range: message.range });
    case "source-selected":
      return requestOverview(model, { sourceId: message.sourceId });
    case "search-submitted":
      return requestOverview(model, { search: message.search.trim() });
    case "conversation-route-selected": {
      const generation = model.conversationRequestGeneration + 1;
      return [{
        ...model,
        requestedConversation: message.target,
        routedConversation: undefined,
        selectedSessionId: message.target.conversationId,
        selectedSessionSourceId: message.target.sourceId,
        highlightedSpanId: message.target.spanId,
        conversationStatus: "loading",
        conversationRequestGeneration: generation,
        conversationError: undefined,
      }, [{ type: "fetch-conversation", generation, target: message.target }]];
    }
    case "conversation-route-cleared":
      return [{
        ...model,
        requestedConversation: undefined,
        routedConversation: undefined,
        highlightedSpanId: undefined,
        selectedSessionId: model.overview?.sessions[0]?.id,
        selectedSessionSourceId: model.overview?.sessions[0]?.sourceId,
        conversationStatus: "idle",
        conversationError: undefined,
      }, []];
    case "conversation-received":
      if (message.generation !== model.conversationRequestGeneration
        || message.target.sourceId !== model.requestedConversation?.sourceId
        || message.target.conversationId !== model.requestedConversation.conversationId) return [model, []];
      return [{ ...model, routedConversation: message.conversation, conversationStatus: "ready", conversationError: undefined }, []];
    case "conversation-failed":
      if (message.generation !== model.conversationRequestGeneration
        || message.target.sourceId !== model.requestedConversation?.sourceId
        || message.target.conversationId !== model.requestedConversation.conversationId) return [model, []];
      return [{ ...model, routedConversation: undefined, conversationStatus: "failed", conversationError: message.error }, []];
    case "session-selected":
      return [{
        ...model,
        selectedSessionId: message.sessionId,
        selectedSessionSourceId: message.sourceId,
        requestedConversation: undefined,
        routedConversation: undefined,
        highlightedSpanId: undefined,
        conversationStatus: "idle",
        conversationError: undefined,
      }, []];
    case "activities-requested": {
      if (model.activityPage?.loading) return [model, []];
      const exact = model.requestedConversation?.sourceId === message.sourceId
        && model.requestedConversation.conversationId === message.sessionId;
      const session = exact
        ? model.routedConversation
        : model.overview?.sessions.find(({ id, sourceId }) => id === message.sessionId && sourceId === message.sourceId);
      if (!session) return [model, []];
      if (message.direction === "newer" && !session.hasEarlier) return [model, []];
      if (message.direction === "older" && (session.hasMore === false || (session.hasMore === undefined && session.activities.length >= session.activityCount))) return [model, []];
      const currentOffset = session.activityOffset ?? 0;
      const offset = message.direction === "newer"
        ? Math.max(0, currentOffset - 100)
        : currentOffset + session.activities.length;
      const limit = message.direction === "newer" ? currentOffset - offset : 100;
      const pageToken = message.direction === "newer"
        ? session.previousPageToken ?? ""
        : session.nextPageToken ?? "";
      const generation = exact ? model.conversationRequestGeneration : model.requestGeneration;
      return [
        { ...model, activityPage: { sessionId: message.sessionId, sourceId: message.sourceId, direction: message.direction, exact, offset, loading: true }, error: undefined },
        [{
          type: "fetch-activities",
          generation,
          range: model.range,
          sourceId: message.sourceId,
          search: model.search,
          sessionId: message.sessionId,
          direction: message.direction,
          exact,
          offset,
          limit,
          pageToken,
          ...(exact ? {
            traceId: model.requestedConversation?.traceId,
            spanId: model.requestedConversation?.spanId,
          } : {}),
        }],
      ];
    }
    case "activities-received": {
      const expectedGeneration = message.exact ? model.conversationRequestGeneration : model.requestGeneration;
      if (message.generation !== expectedGeneration
        || model.activityPage?.sessionId !== message.sessionId
        || model.activityPage.sourceId !== message.sourceId
        || model.activityPage.direction !== message.direction
        || model.activityPage.exact !== message.exact
        || model.activityPage.offset !== message.offset) return [model, []];
      if (message.exact) {
        const current = model.routedConversation;
        if (!current) return [model, []];
        const activities = message.direction === "newer"
          ? [...message.activities, ...current.activities]
          : [...current.activities, ...message.activities];
        const activityOffset = message.direction === "newer" ? message.offset : current.activityOffset ?? 0;
        return [{
          ...model,
          routedConversation: {
            ...current,
            activities,
            activityCount: message.total,
            activityOffset,
            hasEarlier: activityOffset > 0,
            hasMore: activityOffset + activities.length < message.total,
            nextPageToken: message.nextPageToken,
            previousPageToken: message.previousPageToken,
          },
          activityPage: { ...model.activityPage, loading: false },
        }, []];
      }
      return [
        {
          ...model,
          overview: model.overview === undefined ? undefined : {
            ...model.overview,
            sessions: model.overview.sessions.map((session) => session.id === message.sessionId && session.sourceId === message.sourceId
              ? {
                ...session,
                activities: message.direction === "newer" ? [...message.activities, ...session.activities] : [...session.activities, ...message.activities],
                activityCount: message.total,
                activityOffset: message.direction === "newer" ? message.offset : session.activityOffset ?? 0,
                hasEarlier: message.hasEarlier,
                hasMore: message.hasMore,
                nextPageToken: message.nextPageToken,
                previousPageToken: message.previousPageToken,
              }
              : session),
          },
          activityPage: { ...model.activityPage, loading: false },
        },
        [],
      ];
    }
    case "activities-failed": {
      const expectedGeneration = message.exact ? model.conversationRequestGeneration : model.requestGeneration;
      if (message.generation !== expectedGeneration
        || model.activityPage?.sessionId !== message.sessionId
        || model.activityPage.sourceId !== message.sourceId
        || model.activityPage.direction !== message.direction
        || model.activityPage.exact !== message.exact
        || model.activityPage.offset !== message.offset) return [model, []];
      return [{ ...model, activityPage: { ...model.activityPage, loading: false, error: message.error } }, []];
    }
    case "trace-selected": {
      const generation = model.traceRequestGeneration + 1;
      return [{
        ...model,
        selectedTraceId: message.traceId,
        traceStatus: "loading",
        traceRequestGeneration: generation,
        trace: undefined,
        traceError: undefined,
      }, [{ type: "fetch-trace", generation, traceId: message.traceId, offset: 0, limit: 100, pageToken: "" }]];
    }
    case "trace-activities-requested": {
      if (model.selectedTraceId !== message.traceId || !model.trace || !model.trace.hasMore) return [model, []];
      const generation = model.traceRequestGeneration + 1;
      return [{ ...model, traceStatus: "loading", traceRequestGeneration: generation, traceError: undefined }, [
        { type: "fetch-trace", generation, traceId: message.traceId, offset: message.offset, limit: 100, pageToken: message.pageToken },
      ]];
    }
    case "trace-received":
      if (message.generation !== model.traceRequestGeneration || message.traceId !== model.selectedTraceId) return [model, []];
      {
        const current = model.trace;
        const trace = current && current.traceId === message.trace.traceId && message.trace.activityOffset > current.activityOffset
          ? { ...message.trace, activities: [...current.activities, ...message.trace.activities] }
          : message.trace;
        return [{ ...model, traceStatus: "ready", trace, traceError: undefined }, []];
      }
    case "trace-failed":
      if (message.generation !== model.traceRequestGeneration || message.traceId !== model.selectedTraceId) return [model, []];
      return [{ ...model, traceStatus: "failed", trace: undefined, traceError: message.error }, []];
    case "trace-closed":
      return [{ ...model, selectedTraceId: undefined, traceStatus: "idle", trace: undefined, traceError: undefined }, []];
    case "overview-received": {
      if (message.generation !== model.requestGeneration) return [model, []];
      const currentExists = message.overview.sessions.some(({ id, sourceId }) =>
        id === model.selectedSessionId && sourceId === model.selectedSessionSourceId);
      const selectedSessionId = model.requestedConversation
        ? model.requestedConversation?.conversationId
        : currentExists ? model.selectedSessionId : message.overview.sessions[0]?.id;
      const selectedSessionSourceId = model.requestedConversation
        ? model.requestedConversation?.sourceId
        : currentExists ? model.selectedSessionSourceId : message.overview.sessions[0]?.sourceId;
      return [
        {
          ...model,
          status: "ready",
          overview: message.overview,
          selectedSessionId,
          selectedSessionSourceId,
          highlightedSpanId: model.requestedConversation?.spanId,
          activityPage: undefined,
          error: undefined,
        },
        [],
      ];
    }
    case "overview-failed":
      if (message.generation !== model.requestGeneration) return [model, []];
      return [{ ...model, status: "failed", error: message.error }, []];
  }
};

const requestOverview = (
  model: Model,
  changes: Partial<Pick<Model, "range" | "sourceId" | "search">> = {},
): readonly [Model, readonly Effect[]] => {
  const generation = model.requestGeneration + 1;
  const range = changes.range ?? model.range;
  const sourceId = changes.sourceId ?? model.sourceId;
  const search = changes.search ?? model.search;
  return [
    { ...model, range, sourceId, search, status: "loading", requestGeneration: generation, activityPage: undefined, error: undefined },
    [{ type: "fetch-overview", generation, range, sourceId, search }],
  ];
};
