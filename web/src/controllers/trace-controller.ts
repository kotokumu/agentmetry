import { Task, TaskStatus } from "@lit/task";
import { Code, ConnectError } from "@connectrpc/connect";
import type { ReactiveControllerHost } from "lit";
import { ExactTraceEvidenceUnavailableError, type ActivityMutation, type AgentmetryClient } from "../api/agentmetry-client";
import { traceWindowForState, type TraceInvestigationState, type TraceOverview } from "../model/trace-investigation";
import type { Trace } from "../model/telemetry";
import { affectsTrace, type LiveUpdateWindow } from "./live-update-controller";

export class TraceController {
  private readonly host: ReactiveControllerHost;
  private readonly client: AgentmetryClient;
  private readonly task: Task<readonly [string, string, string], TraceLoad>;
  private traceOverride?: Trace;
  private overviewOverride?: TraceOverview;
  private selectedActivityOverride?: Trace["activities"][number];
  private matchingActivitiesOverride?: number;
  private overviewStateOverride?: TraceLoad["overviewState"];
  private windowStateOverride?: TraceLoad["windowState"];
  private overviewErrorOverride?: string;
  private windowErrorOverride?: string;
  private pageRequest = 0;
  private pageAbort?: AbortController;
  private wasDisconnected = false;
  private liveRequest = 0;
  private liveAbort?: AbortController;
  private liveLoading = false;
  private syncCursor = "";
  private removedTraceId?: string;
  traceId = "";
  anchorSpanId = "";
  investigation: TraceInvestigationState = {};
  private anchorUnavailable = false;
  loadingPage = false;
  pageError?: string;

  constructor(host: ReactiveControllerHost, client: AgentmetryClient) {
    this.host = host;
    this.client = client;
    host.addController(this);
    this.task = new Task(host, {
      args: () => [this.traceId, this.anchorSpanId, investigationKey(this.investigation)] as const,
      task: ([traceId, anchorSpanId, key], { signal }) => this.loadTrace(traceId, anchorSpanId, key, signal),
    });
  }

  get value() {
    if (this.anchorUnavailable) return undefined;
    if (this.traceOverride?.traceId === this.traceId) return this.traceOverride;
    return this.currentLoad?.trace;
  }
  get overview() { return this.overviewOverride ?? this.currentLoad?.overview; }
  get selectedActivity() { return this.selectedActivityOverride ?? this.currentLoad?.selectedActivity; }
  get matchingActivities() { return this.matchingActivitiesOverride ?? this.currentLoad?.matchingActivities ?? this.value?.activityCount ?? 0; }
  get overviewState() { return this.overviewStateOverride ?? this.currentLoad?.overviewState ?? "loading"; }
  get windowState() { return this.windowStateOverride ?? this.currentLoad?.windowState ?? "loading"; }
  get overviewError() { return this.overviewErrorOverride ?? this.currentLoad?.overviewError; }
  get windowError() { return this.windowErrorOverride ?? this.currentLoad?.windowError; }
  private get currentLoad() {
    const key = loadKey(this.traceId, this.anchorSpanId, this.investigation);
    return this.task.value?.key === key ? this.task.value : undefined;
  }
  get loading() { return this.task.status === TaskStatus.PENDING && this.value === undefined; }
  get failed() { return this.anchorUnavailable || this.task.status === TaskStatus.ERROR; }
  get error() { return this.pageError ?? this.task.error; }

  open(traceId: string, anchorSpanId = "", investigation: TraceInvestigationState = {}) {
    const selectedSpanId = investigation.selectedSpanId ?? anchorSpanId;
    const nextInvestigation = { ...investigation, ...(selectedSpanId ? { selectedSpanId } : {}) };
    if (traceId === this.traceId && selectedSpanId === this.anchorSpanId && investigationKey(nextInvestigation) === investigationKey(this.investigation)) return;
    this.traceId = traceId;
    this.anchorSpanId = selectedSpanId;
    this.investigation = nextInvestigation;
    this.anchorUnavailable = false;
    this.liveLoading = false;
    this.liveRequest += 1;
    this.traceOverride = undefined;
    this.clearInvestigationOverrides();
    this.removedTraceId = undefined;
    this.pageError = undefined;
    this.pageAbort?.abort();
	this.liveAbort?.abort();
	this.syncCursor = "";
    this.loadingPage = false;
    this.pageRequest += 1;
    this.host.requestUpdate();
  }

  close() { this.open(""); }

  setInvestigation(investigation: TraceInvestigationState) {
    this.open(this.traceId, investigation.selectedSpanId ?? "", investigation);
  }

  takeRemovedTrace() {
    const traceId = this.removedTraceId;
    this.removedTraceId = undefined;
    return traceId;
  }
  refresh() {
    this.traceOverride = undefined;
    this.clearInvestigationOverrides();
    void this.task.run();
  }

  async applyLiveUpdate(window: LiveUpdateWindow) {
    const current = this.value;
    if (!current || (!window.resyncRequired && !affectsTrace(window.targets, current.traceId))) return;
    const request = ++this.liveRequest;
    this.pageRequest += 1;
    this.pageAbort?.abort();
    this.loadingPage = false;
    this.liveAbort?.abort();
    const abort = new AbortController();
    this.liveAbort = abort;
    this.liveLoading = true;
    try {
      if (this.windowState === "available") {
        const loaded = await this.loadTrace(current.traceId, this.anchorSpanId, investigationKey(this.investigation), abort.signal);
        if (request !== this.liveRequest || abort.signal.aborted || this.traceId !== current.traceId) return;
        this.applyLoadOverride(loaded);
        this.syncCursor = window.throughCursor;
        this.host.requestUpdate();
        return;
      }
      if (this.anchorSpanId) {
        const anchored = await this.client.getTrace(current.traceId, 0, 100, "", abort.signal, false, this.anchorSpanId);
        if (request !== this.liveRequest || abort.signal.aborted) return;
        this.traceOverride = anchored;
        this.syncCursor = window.throughCursor;
        this.host.requestUpdate();
        return;
      }
      const mutations: ActivityMutation[] = [];
      let incremental = !window.resyncRequired && Boolean(this.syncCursor) && Boolean(window.throughCursor);
      let convergedCursor = window.throughCursor;
      if (incremental) {
        let pageToken = "";
        for (let pageIndex = 0; pageIndex < 10; pageIndex += 1) {
          const page = await this.client.syncTraceActivities(current.traceId, this.syncCursor, window.throughCursor, pageToken, abort.signal);
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
      // Reset the offset token from a bounded head window, then layer the
      // mutation set on top so a live tail remains visible without a full
      // trace reload.
      const head = await this.client.getTrace(current.traceId, 0, 100, "", abort.signal);
      if (request !== this.liveRequest || abort.signal.aborted || this.traceId !== current.traceId) return;
      const mergedActivities = incremental
        ? applyTraceMutations([...current.activities, ...head.activities], mutations)
        : head.activities;
      // Opaque head paging tokens describe a contiguous head window. At the
      // resident cap, retain that authoritative window instead of combining it
      // with an evicted/non-contiguous historical tail.
      const activities = incremental && (current.activities.length >= 2000 || mergedActivities.length >= 2000)
        ? head.activities
        : mergedActivities;
      this.traceOverride = { ...head, activities, hasMore: activities.length < head.activityCount };
      this.syncCursor = convergedCursor;
      this.host.requestUpdate();
    } catch (error) {
      if (request !== this.liveRequest || abort.signal.aborted) return;
      if (isNotFound(error)) {
        if (this.anchorSpanId) {
          this.anchorUnavailable = true;
          this.pageError = `Requested span ${this.anchorSpanId} is unavailable in retained trace data.`;
          this.host.requestUpdate();
          return;
        }
        this.traceId = "";
        this.traceOverride = undefined;
        this.removedTraceId = current.traceId;
        this.syncCursor = window.throughCursor;
        this.host.requestUpdate();
        return;
      }
      throw error;
    } finally {
      if (request === this.liveRequest) this.liveLoading = false;
    }
  }

  hostDisconnected() {
    this.liveLoading = false;
    this.liveRequest += 1;
    this.wasDisconnected = true;
    this.pageRequest += 1;
    this.pageAbort?.abort();
    this.liveAbort?.abort();
	this.syncCursor = "";
    this.traceOverride = undefined;
    this.clearInvestigationOverrides();
    this.loadingPage = false;
    this.pageError = undefined;
    this.task.abort();
  }

  hostConnected() {
    if (!this.wasDisconnected) return;
    this.wasDisconnected = false;
    void this.task.run();
  }

  async loadMore() {
    const trace = this.value;
    if (!trace || !trace.hasMore || this.loadingPage || this.liveLoading || !trace.nextPageToken) return;
    const request = ++this.pageRequest;
    this.pageAbort?.abort();
    const abort = new AbortController();
    this.pageAbort = abort;
    this.loadingPage = true;
    this.pageError = undefined;
    this.host.requestUpdate();
    try {
      const nextResult = this.windowState === "available"
        ? await this.client.getTraceWindow(trace.traceId, traceWindowForState(this.investigation), trace.activityOffset + trace.activities.length, 100, trace.nextPageToken, abort.signal)
        : undefined;
      const next = nextResult?.trace ?? await this.client.getTrace(trace.traceId, trace.activityOffset + trace.activities.length, 100, trace.nextPageToken, abort.signal);
      if (request !== this.pageRequest || this.traceId !== trace.traceId) return;
	  const combined = deduplicateActivities([...trace.activities, ...next.activities]);
	  const evicted = Math.max(0, combined.length - 2000);
	  this.traceOverride = { ...next, activities: combined.slice(evicted), activityOffset: trace.activityOffset + evicted, previousPageToken: trace.previousPageToken };
      if (nextResult) this.matchingActivitiesOverride = nextResult.matchingActivities;
    } catch (error) {
      if (request !== this.pageRequest || abort.signal.aborted) return;
      this.pageError = error instanceof Error ? error.message : "Trace unavailable";
    }
    if (request === this.pageRequest) {
      this.loadingPage = false;
      this.host.requestUpdate();
    }
  }

  private async loadTrace(traceId: string, anchorSpanId: string, investigation: string, signal: AbortSignal): Promise<TraceLoad> {
    const key = `${traceId}:${anchorSpanId}:${investigation}`;
    if (!traceId) return { key, overviewState: "unsupported", windowState: "unsupported", matchingActivities: 0 };
    const window = traceWindowForState(this.investigation);
    const overviewPromise = typeof this.client.getTraceOverview === "function"
      ? this.client.getTraceOverview(traceId, signal)
      : Promise.reject(new ConnectError("Trace overview is unsupported", Code.Unimplemented));
    const exactPromise = anchorSpanId
      ? this.client.getTrace(traceId, 0, 100, "", signal, false, anchorSpanId)
      : Promise.resolve<Trace | undefined>(undefined);
    const windowPromise = typeof this.client.getTraceWindow === "function"
      ? this.client.getTraceWindow(traceId, window, 0, 100, "", signal)
      : Promise.reject(new ConnectError("Trace windows are unsupported", Code.Unimplemented));
    const [overviewResult, exactResult, windowResult] = await Promise.allSettled([overviewPromise, exactPromise, windowPromise]);
    const bothInvestigationRPCsUnsupported = overviewResult.status === "rejected" && isUnimplemented(overviewResult.reason)
      && windowResult.status === "rejected" && isUnimplemented(windowResult.reason);
    if (exactResult.status === "rejected"
      && !(bothInvestigationRPCsUnsupported && exactResult.reason instanceof ExactTraceEvidenceUnavailableError)) throw exactResult.reason;
    const exactTrace = exactResult.status === "fulfilled" ? exactResult.value : undefined;
    let trace: Trace;
    let matchingActivities: number;
    let windowState: TraceLoad["windowState"] = "available";
    let windowError: string | undefined;
    if (windowResult.status === "fulfilled") {
      trace = windowResult.value.trace;
      matchingActivities = windowResult.value.matchingActivities;
    } else if (isUnimplemented(windowResult.reason)) {
      windowState = "unsupported";
      windowError = "This server does not support bounded trace windows.";
      trace = exactTrace ?? await this.client.getTrace(traceId, 0, 100, "", signal);
      matchingActivities = trace.activityCount;
    } else {
      throw windowResult.reason;
    }
    const overviewState: TraceLoad["overviewState"] = overviewResult.status === "fulfilled"
      ? "available"
      : isUnimplemented(overviewResult.reason) ? "unsupported" : "failed";
    const overviewError = overviewResult.status === "rejected"
      ? overviewState === "unsupported" ? "This server does not support trace overviews." : errorMessage(overviewResult.reason)
      : undefined;
    const selectedActivity = anchorSpanId && exactTrace
      ? exactTrace.activities.find((activity) => activity.signal === "trace" && activity.traceId === traceId && activity.spanId === anchorSpanId)
      : undefined;
    return {
      key, trace, matchingActivities, windowState, windowError,
      overview: overviewResult.status === "fulfilled" ? overviewResult.value : undefined,
      overviewState, overviewError, selectedActivity,
    };
  }

  private applyLoadOverride(load: TraceLoad) {
    this.traceOverride = load.trace;
    this.overviewOverride = load.overview;
    this.selectedActivityOverride = load.selectedActivity ?? this.selectedActivity;
    this.matchingActivitiesOverride = load.matchingActivities;
    this.overviewStateOverride = load.overviewState;
    this.windowStateOverride = load.windowState;
    this.overviewErrorOverride = load.overviewError;
    this.windowErrorOverride = load.windowError;
  }

  private clearInvestigationOverrides() {
    this.overviewOverride = undefined;
    this.selectedActivityOverride = undefined;
    this.matchingActivitiesOverride = undefined;
    this.overviewStateOverride = undefined;
    this.windowStateOverride = undefined;
    this.overviewErrorOverride = undefined;
    this.windowErrorOverride = undefined;
  }
}

type TraceLoad = Readonly<{
  key: string;
  trace?: Trace;
  overview?: TraceOverview;
  selectedActivity?: Trace["activities"][number];
  matchingActivities: number;
  overviewState: "loading" | "available" | "unsupported" | "failed";
  windowState: "loading" | "available" | "unsupported";
  overviewError?: string;
  windowError?: string;
}>;

const investigationKey = (investigation: TraceInvestigationState) => JSON.stringify(traceWindowForState(investigation));
const loadKey = (traceId: string, spanId: string, investigation: TraceInvestigationState) => `${traceId}:${spanId}:${investigationKey(investigation)}`;

const activityIdentity = (activity: Trace["activities"][number]) => activity.id ?? `${activity.signal}\u0000${activity.traceId ?? ""}\u0000${activity.spanId ?? ""}\u0000${activity.observedAt}\u0000${activity.name}`;

const applyTraceMutations = (current: Trace["activities"], mutations: readonly ActivityMutation[]) => {
	const values = new Map(current.map((activity) => [activityIdentity(activity), activity]));
	for (const mutation of mutations) {
		if (mutation.operation === "remove") values.delete(mutation.activityId);
		else if (mutation.activity) values.set(mutation.activityId, mutation.activity);
	}
	return [...values.values()].sort((left, right) => left.observedAt.localeCompare(right.observedAt) || activityIdentity(left).localeCompare(activityIdentity(right))).slice(-2000);
};

const deduplicateActivities = (activities: readonly Trace["activities"][number][]) =>
  [...new Map(activities.map((activity) => [activityIdentity(activity), activity])).values()]
    .sort((left, right) => left.observedAt.localeCompare(right.observedAt) || activityIdentity(left).localeCompare(activityIdentity(right)));

const isNotFound = (error: unknown) => error instanceof ConnectError && error.code === Code.NotFound;
const isUnimplemented = (error: unknown) => error instanceof ConnectError && error.code === Code.Unimplemented;
const errorMessage = (error: unknown) => error instanceof Error ? error.message : "Trace overview unavailable";
