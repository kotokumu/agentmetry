import { conditionsKey, type SessionConditions } from "../model/investigation-conditions";
import { Task, TaskStatus } from "@lit/task";
import { Code, ConnectError } from "@connectrpc/connect";
import type { ReactiveControllerHost } from "lit";
import type { ActivityMutation, ActivityPage, AgentmetryClient } from "../api/agentmetry-client";
import type { ConversationTarget } from "../model/trace-analysis";
import type { ActivityDirection, ReworkAnalysis, Session, TimeRange } from "../model/telemetry";
import { ProjectionTargetKind } from "../gen/agentmetry/v1/agentmetry_pb";
import { telemetryFilterKey, type TelemetryFilters } from "./query-filters";
import { affectsSession, affectsSessionList, type LiveUpdateWindow } from "./live-update-controller";

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
  private readonly sessionsTask: Task<readonly [TimeRange, string, string, string], SessionsResult>;
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
  private liveRequest = 0;
  private liveAbort?: AbortController;
  private liveLoading = false;
  private syncCursor = "";
  private removedSessionKey = "";
  private removedSession?: ConversationRef;

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
        return [value.range, value.sourceId, value.search, conditionsKey(value)] as const;
      },
      task: async ([range, sourceId, search, conditions], { signal }) => ({
        key: telemetryFilterKey({ range, sourceId, search, ...JSON.parse(conditions) as SessionConditions }),
        sessions: await client.listSessions(range, sourceId, search, signal, JSON.parse(conditions) as SessionConditions),
      }),
    });
    this.conversationTask = new Task(host, {
	  args: () => {
		const target = this.taskTarget;
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
    this.liveAbort?.abort();
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
    const sessions = this.sessionsTask.value?.key === telemetryFilterKey(this.filters()) ? this.sessionsTask.value.sessions : [];
    return this.removedSessionKey
      ? sessions.filter(({ sourceId, id }) => sessionKey(sourceId, id) !== this.removedSessionKey)
      : sessions;
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

	private get taskTarget(): ConversationRef | undefined {
	  if (this.requested) return { sourceId: this.requested.sourceId, conversationId: this.requested.conversationId };
    const sessions = this.listedSessions;
    if (this.selectedRef && sessions.some(({ id, sourceId }) => id === this.selectedRef?.conversationId && sourceId === this.selectedRef.sourceId)) return this.selectedRef;
    const first = sessions[0];
	  return first ? { sourceId: first.sourceId, conversationId: first.id } : undefined;
	}

	get target(): ConversationRef | undefined {
	  const requested = this.taskTarget;
	  if (!requested) return undefined;
	  if (this.sessionOverride?.sourceId === requested.sourceId) {
		return { sourceId: this.sessionOverride.sourceId, conversationId: this.sessionOverride.id };
	  }
	  const expectedKey = conversationKey(requested.sourceId, requested.conversationId, this.requested?.traceId ?? "", this.requested?.spanId ?? "");
	  const resolved = this.conversationTask.value?.key === expectedKey ? this.conversationTask.value.session : undefined;
	  return resolved?.sourceId === requested.sourceId
		? { sourceId: resolved.sourceId, conversationId: resolved.id }
		: requested;
	}

	get selected(): Session | undefined {
	  if (!this.isActive()) return undefined;
	  const requested = this.taskTarget;
	  if (!requested) return undefined;
	  if (this.sessionOverride?.sourceId === requested.sourceId) return this.sessionOverride;
	  const result = this.conversationTask.value;
	  const expectedKey = conversationKey(requested.sourceId, requested.conversationId, this.requested?.traceId ?? "", this.requested?.spanId ?? "");
	  const value = result?.key === expectedKey ? result.session : undefined;
	  return value?.sourceId === requested.sourceId ? value : undefined;
  }

  get loadingList() { return this.sessionsTask.status === TaskStatus.PENDING && this.listedSessions.length === 0; }
  get listError() { return this.sessionsTask.error; }
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

  async applyLiveUpdate(window: LiveUpdateWindow) {
    const filter = this.filters();
    const refreshList = window.resyncRequired || affectsSessionList(window.targets, filter.sourceId)
      ? this.refreshListForLive()
      : Promise.resolve();
    await Promise.all([refreshList, this.applySelectedLiveUpdate(window)]);
  }

  private async refreshListForLive() {
    await this.sessionsTask.run();
    if (this.sessionsTask.status === TaskStatus.ERROR) throw this.sessionsTask.error;
  }

  private async applySelectedLiveUpdate(window: LiveUpdateWindow) {
    if (!this.isActive()) {
      await this.verifyInactiveRequestedSession(window);
      return;
    }
    const session = this.selected;
    if (!session || (!window.resyncRequired && !affectsSession(window.targets, session.sourceId, session.id))) return;
    const request = ++this.liveRequest;
    const agentPage = this.agentActivityPage;
    this.activityRequest += 1;
    this.activityAbort?.abort();
    this.activityPage = undefined;
    this.agentActivityRequest += 1;
    this.agentActivityAbort?.abort();
    this.liveAbort?.abort();
    const abort = new AbortController();
    this.liveAbort = abort;
    this.liveLoading = true;
    try {
      const mutations: ActivityMutation[] = [];
		  const membershipMayHaveChanged = window.targets.some(({ kind }) => kind === ProjectionTargetKind.ALL_SESSIONS);
		  let incremental = !window.resyncRequired && !membershipMayHaveChanged && Boolean(this.syncCursor) && Boolean(window.throughCursor);
      let convergedCursor = window.throughCursor;
      if (incremental) {
        let pageToken = "";
        for (let pageIndex = 0; pageIndex < 10; pageIndex += 1) {
          const page = await this.client.syncSessionActivities(session.sourceId, session.id, this.syncCursor, window.throughCursor, pageToken, abort.signal);
          if (page.resyncRequired) {
            mutations.length = 0;
            incremental = false;
            convergedCursor = page.throughCursor;
            break;
          }
          mutations.push(...page.mutations);
          if (!page.nextPageToken) break;
          if (pageIndex === 9) {
            mutations.length = 0;
            incremental = false;
            break;
          }
          pageToken = page.nextPageToken;
        }
      }
      // The bounded head is authoritative for overlapping activity IDs. Loaded
      // history stays resident during a normal incremental refresh so reading
      // context is not replaced by the head-only snapshot.
      let latest: Session;
      try {
        latest = await this.client.getSession(session.sourceId, session.id, undefined, undefined, abort.signal);
      } catch (error) {
        if (!isNotFound(error)) throw error;
        if (request !== this.liveRequest || abort.signal.aborted) return;
        this.markSessionRemoved({ sourceId: session.sourceId, conversationId: session.id }, convergedCursor);
        return;
      }
      if (request !== this.liveRequest || abort.signal.aborted || this.target?.sourceId !== session.sourceId || this.target.conversationId !== session.id) return;
      const preserveResidentWindow = !window.resyncRequired && !membershipMayHaveChanged && (incremental || !this.syncCursor);
      const activities = preserveResidentWindow
        ? applyActivityMutations([...session.activities, ...latest.activities], mutations)
        : latest.activities;
      this.sessionOverride = {
        ...latest,
        activities,
        activityOffset: 0,
        hasEarlier: false,
        hasMore: activities.length < latest.activityCount,
        nextPageToken: preserveResidentWindow ? session.nextPageToken ?? latest.nextPageToken : latest.nextPageToken,
      };
      if (this.removedSessionKey === sessionKey(session.sourceId, session.id)) this.removedSessionKey = "";
      if (agentPage?.sessionId === session.id && agentPage.sourceId === session.sourceId && agentPage.agentId === this.selectedAgentId) {
        const agent = latest.agents.find(({ agentId }) => agentId === agentPage.agentId);
        const agentActivities = preserveResidentWindow
          ? applyActivityMutations(
            [...agentPage.activities, ...latest.activities.filter(({ agentId }) => agentId === agentPage.agentId)],
            mutations,
            ({ agentId }) => agentId === agentPage.agentId,
          )
          : latest.activities.filter(({ agentId }) => agentId === agentPage.agentId);
        this.agentActivityPage = {
          ...agentPage,
          activities: agentActivities,
          total: agent?.activityCount ?? agentActivities.length,
          offset: 0,
          hasEarlier: false,
          hasMore: agentActivities.length < (agent?.activityCount ?? agentActivities.length),
          loading: false,
          error: undefined,
        };
      }
      this.syncCursor = convergedCursor;
      void this.reworkTask.run();
      this.host.requestUpdate();
    } catch (error) {
      if (request !== this.liveRequest || abort.signal.aborted) return;
      throw error;
    } finally {
      if (request === this.liveRequest) this.liveLoading = false;
    }
  }

  private async verifyInactiveRequestedSession(window: LiveUpdateWindow) {
    const requested = this.requested;
    if (!requested || (!window.resyncRequired && !affectsSession(window.targets, requested.sourceId, requested.conversationId))) return;
    const request = ++this.liveRequest;
    this.liveAbort?.abort();
    const abort = new AbortController();
    this.liveAbort = abort;
    this.liveLoading = true;
    try {
      // The hidden workspace owns only a navigation origin. Check existence
      // without the previous trace/span activity filter and without syncing
      // its inactive resident activity window.
      await this.client.getSession(requested.sourceId, requested.conversationId, undefined, undefined, abort.signal);
    } catch (error) {
      if (request !== this.liveRequest || abort.signal.aborted) return;
      if (!isNotFound(error)) throw error;
      if (this.requested?.sourceId !== requested.sourceId || this.requested.conversationId !== requested.conversationId) return;
      this.markSessionRemoved(requested, window.throughCursor);
    } finally {
      if (request === this.liveRequest) this.liveLoading = false;
    }
  }

  private markSessionRemoved(session: ConversationRef, cursor: string) {
    this.requested = undefined;
    this.selectedRef = undefined;
    this.sessionOverride = undefined;
    this.agentActivityPage = undefined;
    this.removedSessionKey = sessionKey(session.sourceId, session.conversationId);
    this.removedSession = session;
    this.syncCursor = cursor;
    this.host.requestUpdate();
  }

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

  takeRemovedSession() {
    const removed = this.removedSession;
    this.removedSession = undefined;
    return removed;
  }

  clearRoute() {
    this.requested = undefined;
    this.selectedRef = undefined;
    this.resetDetailState();
  }

  filtersChanged() {
    this.selectedRef = undefined;
    this.requested = undefined;
    this.removedSessionKey = "";
    this.removedSession = undefined;
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
    if (!session || this.activityPage?.loading || this.liveLoading) return;
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
    if (!session || !agentId || this.liveLoading) return;
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
    this.liveAbort?.abort();
	this.syncCursor = "";
    this.activityRequest += 1;
    this.agentActivityRequest += 1;
    this.sessionOverride = undefined;
    this.activityPage = undefined;
    this.selectedAgentId = "";
    this.agentActivityPage = undefined;
    this.host.requestUpdate();
  }
}

const activityIdentity = (activity: Session["activities"][number]) => activity.id ?? `${activity.signal}\u0000${activity.traceId ?? ""}\u0000${activity.spanId ?? ""}\u0000${activity.observedAt}\u0000${activity.name}`;
const sessionKey = (sourceId: string, sessionId: string) => `${sourceId}\u0000${sessionId}`;

const applyActivityMutations = (
  current: Session["activities"],
  mutations: readonly ActivityMutation[],
  accepts: (activity: Session["activities"][number]) => boolean = () => true,
) => {
  const values = new Map(current.map((activity) => [activityIdentity(activity), activity]));
  for (const mutation of mutations) {
    if (mutation.operation === "remove" || !mutation.activity || !accepts(mutation.activity)) values.delete(mutation.activityId);
    else values.set(mutation.activityId, mutation.activity);
  }
  const result = [...values.values()].filter(accepts);
  result.sort((left, right) => right.observedAt.localeCompare(left.observedAt) || activityIdentity(left).localeCompare(activityIdentity(right)));
  return result.slice(0, 2000);
};

const isNotFound = (error: unknown) => error instanceof ConnectError && error.code === Code.NotFound;

const mergeSessionPage = (session: Session, page: ActivityPage, direction: ActivityDirection): Session => {
	let activities = [...new Map((direction === "newer" ? [...page.activities, ...session.activities] : [...session.activities, ...page.activities]).map((activity) => [activityIdentity(activity), activity])).values()];
	let activityOffset = direction === "newer" ? page.offset : session.activityOffset ?? page.offset;
	if (activities.length > 2000) {
	  if (direction === "older") {
		const evicted = activities.length - 2000;
		activities = activities.slice(evicted);
		activityOffset += evicted;
	  } else activities = activities.slice(0, 2000);
	}
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
): AgentActivityPage => {
	let activities = direction === "newer" ? [...page.activities, ...(current?.activities ?? [])] : [...(current?.activities ?? []), ...page.activities];
	let offset = direction === "newer" ? page.offset : current?.offset ?? page.offset;
	if (activities.length > 2000) {
	  if (direction === "older") { const evicted = activities.length - 2000; activities = activities.slice(evicted); offset += evicted; }
	  else activities = activities.slice(0, 2000);
	}
	return {
  sessionId, sourceId, agentId, activities,
  total: page.total,
	offset,
  hasEarlier: page.hasEarlier,
  hasMore: page.hasMore,
  nextPageToken: page.nextPageToken,
  previousPageToken: page.previousPageToken,
  loading: false,
};
};
