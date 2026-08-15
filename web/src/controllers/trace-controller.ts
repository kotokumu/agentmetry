import { Task, TaskStatus } from "@lit/task";
import { Code, ConnectError } from "@connectrpc/connect";
import type { ReactiveControllerHost } from "lit";
import type { ActivityMutation, AgentmetryClient } from "../api/agentmetry-client";
import type { Trace } from "../model/telemetry";
import { affectsTrace, type LiveUpdateWindow } from "./live-update-controller";

export class TraceController {
  private readonly host: ReactiveControllerHost;
  private readonly client: AgentmetryClient;
  private readonly task: Task<readonly [string], Trace | undefined>;
  private traceOverride?: Trace;
  private pageRequest = 0;
  private pageAbort?: AbortController;
  private wasDisconnected = false;
  private liveRequest = 0;
  private liveAbort?: AbortController;
  private liveLoading = false;
  private syncCursor = "";
  private removedTraceId?: string;
  traceId = "";
  loadingPage = false;
  pageError?: string;

  constructor(host: ReactiveControllerHost, client: AgentmetryClient) {
    this.host = host;
    this.client = client;
    host.addController(this);
    this.task = new Task(host, {
      args: () => [this.traceId] as const,
      task: ([traceId], { signal }) => traceId ? client.getTrace(traceId, 0, 100, "", signal) : Promise.resolve(undefined),
    });
  }

  get value() {
    if (this.traceOverride?.traceId === this.traceId) return this.traceOverride;
    return this.task.value?.traceId === this.traceId ? this.task.value : undefined;
  }
  get loading() { return this.task.status === TaskStatus.PENDING && this.value === undefined; }
  get failed() { return this.task.status === TaskStatus.ERROR; }
  get error() { return this.pageError ?? this.task.error; }

  open(traceId: string) {
    if (traceId === this.traceId) return;
    this.traceId = traceId;
    this.traceOverride = undefined;
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

  takeRemovedTrace() {
    const traceId = this.removedTraceId;
    this.removedTraceId = undefined;
    return traceId;
  }
  refresh() {
    this.traceOverride = undefined;
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
    this.wasDisconnected = true;
    this.pageRequest += 1;
    this.pageAbort?.abort();
    this.liveAbort?.abort();
	this.syncCursor = "";
    this.traceOverride = undefined;
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
      const next = await this.client.getTrace(trace.traceId, trace.activityOffset + trace.activities.length, 100, trace.nextPageToken, abort.signal);
      if (request !== this.pageRequest || this.traceId !== trace.traceId) return;
	  const combined = deduplicateActivities([...trace.activities, ...next.activities]);
	  const evicted = Math.max(0, combined.length - 2000);
	  this.traceOverride = { ...next, activities: combined.slice(evicted), activityOffset: trace.activityOffset + evicted, previousPageToken: trace.previousPageToken };
    } catch (error) {
      if (request !== this.pageRequest || abort.signal.aborted) return;
      this.pageError = error instanceof Error ? error.message : "Trace unavailable";
    }
    if (request === this.pageRequest) {
      this.loadingPage = false;
      this.host.requestUpdate();
    }
  }
}

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
