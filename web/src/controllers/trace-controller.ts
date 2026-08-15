import { Task, TaskStatus } from "@lit/task";
import type { ReactiveControllerHost } from "lit";
import type { AgentmetryClient } from "../api/agentmetry-client";
import type { Trace } from "../model/telemetry";

export class TraceController {
  private readonly host: ReactiveControllerHost;
  private readonly client: AgentmetryClient;
  private readonly task: Task<readonly [string], Trace | undefined>;
  private traceOverride?: Trace;
  private pageRequest = 0;
  private pageAbort?: AbortController;
  private wasDisconnected = false;
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
    this.pageError = undefined;
    this.pageAbort?.abort();
    this.loadingPage = false;
    this.pageRequest += 1;
    this.host.requestUpdate();
  }

  close() { this.open(""); }
  refresh() {
    this.traceOverride = undefined;
    void this.task.run();
  }

  hostDisconnected() {
    this.wasDisconnected = true;
    this.pageRequest += 1;
    this.pageAbort?.abort();
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
    if (!trace || !trace.hasMore || this.loadingPage || !trace.nextPageToken) return;
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
      this.traceOverride = { ...next, activities: [...trace.activities, ...next.activities] };
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
