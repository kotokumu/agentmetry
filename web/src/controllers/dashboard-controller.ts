import { Task, TaskStatus } from "@lit/task";
import type { ReactiveControllerHost } from "lit";
import type { AgentmetryClient } from "../api/agentmetry-client";
import type { DashboardSummary, TimeRange } from "../model/telemetry";
import { telemetryFilterKey, type TelemetryFilters } from "./query-filters";

type DashboardResult = Readonly<{ key: string; value: DashboardSummary }>;

export class DashboardController {
  private readonly task: Task<readonly [TimeRange, string, string], DashboardResult>;
  private readonly filters: () => TelemetryFilters;
  private lastSuccessfulValue?: DashboardSummary;
  private wasDisconnected = false;

  constructor(host: ReactiveControllerHost, client: AgentmetryClient, filters: () => TelemetryFilters) {
    this.filters = filters;
    host.addController(this);
    this.task = new Task(host, {
      args: () => {
        const value = filters();
        return [value.range, value.sourceId, value.search] as const;
      },
      task: async ([range, sourceId, search], { signal }) => ({
        key: telemetryFilterKey({ range, sourceId, search }),
        value: await client.getDashboard(range, sourceId, search, signal),
      }),
      onComplete: ({ value }) => { this.lastSuccessfulValue = value; },
    });
  }

  get value() { return this.task.value?.key === telemetryFilterKey(this.filters()) ? this.task.value.value : undefined; }
  get lastValue() { return this.lastSuccessfulValue; }
  get loading() { return this.task.status === TaskStatus.PENDING && this.value === undefined; }
  get failed() { return this.task.status === TaskStatus.ERROR && this.value === undefined; }
  get error() { return this.task.error; }
  refresh() { void this.task.run(); }
  hostConnected() {
    if (!this.wasDisconnected) return;
    this.wasDisconnected = false;
    void this.task.run();
  }
  hostDisconnected() {
    this.wasDisconnected = true;
    this.task.abort();
  }
}
