import { Task, TaskStatus } from "@lit/task";
import type { ReactiveControllerHost } from "lit";
import type { ActivityPage, AgentmetryClient } from "../api/agentmetry-client";
import type { ConversationTarget } from "../model/trace-analysis";
import type { ActivityDirection, ReworkAnalysis, Session, TimeRange } from "../model/telemetry";
import { telemetryFilterKey, type TelemetryFilters } from "./query-filters";

type ConversationRef = Readonly<{ sourceId: string; conversationId: string }>;
type SessionsResult = Readonly<{ key: string; sessions: readonly Session[] }>;
type ConversationResult = Readonly<{ key: string; session?: Session }>;
type ReworkResult = Readonly<{ key: string; analysis?: ReworkAnalysis }>;

export type ActivityPageState = Readonly<{
  direction: ActivityDirection;
  loading: boolean;
  error?: string;
}>;

type AgentActivityPage = ActivityPage & Readonly<{
  sessionId: string;
  sourceId: string;
  agentId: string;
  loading: boolean;
  error?: string;
}>;

export class ConversationsController {
  private readonly host: ReactiveControllerHost;
  private readonly client: AgentmetryClient;
  private readonly filters: () => TelemetryFilters;
  private readonly isActive: () => boolean;
  private readonly sessionsTask: Task<readonly [TimeRange, string, string], SessionsResult>;
  private readonly conversationTask: Task<readonly [boolean, string, string, string, string], ConversationResult>;
  private readonly reworkTask: Task<readonly [boolean, string, string], ReworkResult>;
  private requested?: ConversationTarget;
  private selectedRef?: ConversationRef;
  private sessionOverride?: Session;
  private activityRequest = 0;
  private agentActivityRequest = 0;
  private activityAbort?: AbortController;
  private agentActivityAbort?: AbortController;
  private wasDisconnected = false;

  activityPage?: ActivityPageState;
  selectedAgentId = "";
  agentActivityPage?: AgentActivityPage;

  constructor(host: ReactiveControllerHost, client: AgentmetryClient, filters: () => TelemetryFilters, isActive: () => boolean = () => true) {
    this.host = host;
    this.client = client;
    this.filters = filters;
    this.isActive = isActive;
    host.addController(this);
    this.sessionsTask = new Task(host, {
      args: () => {
        const value = filters();
        return [value.range, value.sourceId, value.search] as const;
      },
      task: async ([range, sourceId, search], { signal }) => ({
        key: telemetryFilterKey({ range, sourceId, search }),
        sessions: await client.listSessions(range, sourceId, search, signal),
      }),
    });
    this.conversationTask = new Task(host, {
      args: () => {
        const target = this.target;
        return [isActive(), target?.sourceId ?? "", target?.conversationId ?? "", this.requested?.traceId ?? "", this.requested?.spanId ?? ""] as const;
      },
      task: async ([active, sourceId, conversationId, traceId, spanId], { signal }) => ({
        key: conversationKey(sourceId, conversationId, traceId, spanId),
        session: active && sourceId && conversationId
          ? await client.getSession(sourceId, conversationId, traceId || undefined, spanId || undefined, signal)
          : undefined,
      }),
    });
    this.reworkTask = new Task(host, {
      args: () => {
        const target = this.target;
        return [isActive(), target?.sourceId ?? "", target?.conversationId ?? ""] as const;
      },
      task: async ([active, sourceId, conversationId], { signal }) => ({
        key: conversationKey(sourceId, conversationId, "", ""),
        analysis: active && sourceId && conversationId
          ? await client.getSessionRework(sourceId, conversationId, signal)
          : undefined,
      }),
    });
  }

  hostDisconnected() {
    this.wasDisconnected = true;
    this.activityRequest += 1;
    this.agentActivityRequest += 1;
    this.activityAbort?.abort();
    this.agentActivityAbort?.abort();
    this.sessionOverride = undefined;
    this.activityPage = undefined;
    this.selectedAgentId = "";
    this.agentActivityPage = undefined;
    this.sessionsTask.abort();
    this.conversationTask.abort();
    this.reworkTask.abort();
  }

  hostConnected() {
    if (!this.wasDisconnected) return;
    this.wasDisconnected = false;
    void this.sessionsTask.run();
    void this.conversationTask.run();
    void this.reworkTask.run();
  }

  private get listedSessions() {
    return this.sessionsTask.value?.key === telemetryFilterKey(this.filters()) ? this.sessionsTask.value.sessions : [];
  }

  get sessions(): readonly Session[] {
    const sessions = this.listedSessions;
    const selected = this.selected;
    return selected && !sessions.some(({ id, sourceId }) => id === selected.id && sourceId === selected.sourceId)
      ? [selected, ...sessions]
      : sessions;
  }

  get sources() {
    const values = new Map<string, { id: string; label: string }>();
    for (const session of this.listedSessions) {
      for (const source of session.sources) values.set(source.id, source);
    }
    return [...values.values()];
  }

  get target(): ConversationRef | undefined {
    if (this.requested) return { sourceId: this.requested.sourceId, conversationId: this.requested.conversationId };
    const sessions = this.listedSessions;
    if (this.selectedRef && sessions.some(({ id, sourceId }) => id === this.selectedRef?.conversationId && sourceId === this.selectedRef.sourceId)) return this.selectedRef;
    const first = sessions[0];
    return first ? { sourceId: first.sourceId, conversationId: first.id } : undefined;
  }

  get selected(): Session | undefined {
    if (!this.isActive()) return undefined;
    const target = this.target;
    if (!target) return undefined;
    if (this.sessionOverride?.id === target.conversationId && this.sessionOverride.sourceId === target.sourceId) return this.sessionOverride;
    const result = this.conversationTask.value;
    const expectedKey = conversationKey(target.sourceId, target.conversationId, this.requested?.traceId ?? "", this.requested?.spanId ?? "");
    const value = result?.key === expectedKey ? result.session : undefined;
    return value?.id === target.conversationId && value.sourceId === target.sourceId ? value : undefined;
  }

  get loadingList() { return this.sessionsTask.status === TaskStatus.PENDING && this.listedSessions.length === 0; }
  get listFailed() { return this.sessionsTask.status === TaskStatus.ERROR && this.listedSessions.length === 0; }
  get loadingConversation() { return this.conversationTask.status === TaskStatus.PENDING && this.selected === undefined; }
  get conversationFailed() { return this.conversationTask.status === TaskStatus.ERROR; }
  get conversationError() { return this.conversationTask.error; }
  get highlightedTraceId() { return this.requested?.traceId ?? ""; }
  get highlightedSpanId() { return this.requested?.spanId ?? ""; }
  get rework(): ReworkAnalysis | undefined {
    if (!this.isActive()) return undefined;
    const target = this.target;
    const result = this.reworkTask.value;
    if (!target || result?.key !== conversationKey(target.sourceId, target.conversationId, "", "")) return undefined;
    const value = result.analysis;
    return value?.sourceId === target.sourceId && value.sessionId === target.conversationId ? value : undefined;
  }
  get loadingRework() { return this.reworkTask.status === TaskStatus.PENDING && this.rework === undefined; }
  get reworkFailed() { return this.reworkTask.status === TaskStatus.ERROR; }
  get reworkError() { return this.reworkTask.error; }

  refreshList() { void this.sessionsTask.run(); }
  refreshSelected() {
    this.sessionOverride = undefined;
    void this.conversationTask.run();
  }
  refreshRework() { void this.reworkTask.run(); }

  select(target: ConversationTarget) {
    const previous = this.target;
    const retry = this.conversationTask.status === TaskStatus.ERROR
      && previous?.sourceId === target.sourceId
      && previous.conversationId === target.conversationId;
    this.requested = target;
    this.selectedRef = { sourceId: target.sourceId, conversationId: target.conversationId };
    this.resetDetailState();
    if (retry) void this.conversationTask.run();
  }

  clearRoute() {
    this.requested = undefined;
    this.selectedRef = undefined;
    this.resetDetailState();
  }

  filtersChanged() {
    this.selectedRef = undefined;
    this.requested = undefined;
    this.resetDetailState();
  }

  selectAgent(agentId: string) {
    this.selectedAgentId = agentId;
    this.agentActivityPage = undefined;
    this.agentActivityRequest += 1;
    this.host.requestUpdate();
    if (agentId) void this.loadAgentActivities("older", agentId);
  }

  async loadActivities(direction: ActivityDirection) {
    const session = this.selected;
    if (!session || this.activityPage?.loading) return;
    if (direction === "newer" && !session.hasEarlier) return;
    if (direction === "older" && (session.hasMore === false || (session.hasMore === undefined && session.activities.length >= session.activityCount))) return;
    const currentOffset = session.activityOffset ?? 0;
    const offset = direction === "newer" ? Math.max(0, currentOffset - 100) : currentOffset + session.activities.length;
    const limit = direction === "newer" ? currentOffset - offset : 100;
    const pageToken = direction === "newer" ? session.previousPageToken ?? "" : session.nextPageToken ?? "";
    const request = ++this.activityRequest;
    this.activityAbort?.abort();
    const abort = new AbortController();
    this.activityAbort = abort;
    this.activityPage = { direction, loading: true };
    this.host.requestUpdate();
    try {
      const page = await this.client.listSessionActivities(session.sourceId, session.id, direction, offset, limit, pageToken, this.requested?.traceId, this.requested?.spanId, undefined, abort.signal);
      if (request !== this.activityRequest || this.target?.conversationId !== session.id || this.target.sourceId !== session.sourceId) return;
      this.sessionOverride = mergeSessionPage(session, page, direction);
      this.activityPage = { direction, loading: false };
    } catch (error) {
      if (request !== this.activityRequest || abort.signal.aborted) return;
      this.activityPage = { direction, loading: false, error: error instanceof Error ? error.message : "Activities unavailable" };
    }
    this.host.requestUpdate();
  }

  async loadAgentActivities(direction: ActivityDirection, agentId = this.selectedAgentId) {
    const session = this.selected;
    if (!session || !agentId) return;
    const current = this.agentActivityPage?.sessionId === session.id && this.agentActivityPage.agentId === agentId ? this.agentActivityPage : undefined;
    const offset = direction === "older" ? (current ? current.offset + current.activities.length : 0) : Math.max(0, (current?.offset ?? 0) - 100);
    const request = ++this.agentActivityRequest;
    this.agentActivityAbort?.abort();
    const abort = new AbortController();
    this.agentActivityAbort = abort;
    this.agentActivityPage = {
      sessionId: session.id, sourceId: session.sourceId, agentId,
      activities: current?.activities ?? [], total: current?.total ?? 0, offset: current?.offset ?? 0,
      hasEarlier: current?.hasEarlier ?? false, hasMore: current?.hasMore ?? true, loading: true,
    };
    this.host.requestUpdate();
    try {
      const page = await this.client.listSessionActivities(session.sourceId, session.id, direction, offset, 100, direction === "older" ? current?.nextPageToken : current?.previousPageToken, undefined, undefined, agentId, abort.signal);
      if (request !== this.agentActivityRequest || this.selectedAgentId !== agentId || this.target?.conversationId !== session.id) return;
      this.agentActivityPage = mergeAgentActivityPage(session.id, session.sourceId, agentId, current, page, direction);
    } catch (error) {
      if (request !== this.agentActivityRequest || this.selectedAgentId !== agentId || abort.signal.aborted) return;
      this.agentActivityPage = { sessionId: session.id, sourceId: session.sourceId, agentId, activities: current?.activities ?? [], total: current?.total ?? 0, offset: current?.offset ?? 0, hasEarlier: current?.hasEarlier ?? false, hasMore: current?.hasMore ?? false, loading: false, error: error instanceof Error ? error.message : "Agent activities unavailable" };
    }
    this.host.requestUpdate();
  }

  private resetDetailState() {
    this.activityAbort?.abort();
    this.agentActivityAbort?.abort();
    this.activityRequest += 1;
    this.agentActivityRequest += 1;
    this.sessionOverride = undefined;
    this.activityPage = undefined;
    this.selectedAgentId = "";
    this.agentActivityPage = undefined;
    this.host.requestUpdate();
  }
}

const mergeSessionPage = (session: Session, page: ActivityPage, direction: ActivityDirection): Session => {
  const activities = direction === "newer" ? [...page.activities, ...session.activities] : [...session.activities, ...page.activities];
  const activityOffset = direction === "newer" ? page.offset : session.activityOffset ?? page.offset;
  return {
    ...session,
    activities,
    activityCount: page.total,
    activityOffset,
    hasEarlier: activityOffset > 0,
    hasMore: activityOffset + activities.length < page.total,
    nextPageToken: page.nextPageToken,
    previousPageToken: page.previousPageToken,
  };
};

const conversationKey = (sourceId: string, conversationId: string, traceId: string, spanId: string) => `${sourceId}\u0000${conversationId}\u0000${traceId}\u0000${spanId}`;

const mergeAgentActivityPage = (
  sessionId: string,
  sourceId: string,
  agentId: string,
  current: AgentActivityPage | undefined,
  page: ActivityPage,
  direction: ActivityDirection,
): AgentActivityPage => ({
  sessionId,
  sourceId,
  agentId,
  activities: direction === "newer" ? [...page.activities, ...(current?.activities ?? [])] : [...(current?.activities ?? []), ...page.activities],
  total: page.total,
  offset: direction === "newer" ? page.offset : current?.offset ?? page.offset,
  hasEarlier: page.hasEarlier,
  hasMore: page.hasMore,
  nextPageToken: page.nextPageToken,
  previousPageToken: page.previousPageToken,
  loading: false,
});
