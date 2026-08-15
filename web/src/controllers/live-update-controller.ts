import { ProjectionTargetKind } from "../gen/agentmetry/v1/agentmetry_pb";
import type { AgentmetryClient, ProjectionChangeTarget } from "../api/agentmetry-client";

export type LiveUpdateWindow = Readonly<{
  targets: readonly ProjectionChangeTarget[];
  resyncRequired: boolean;
  throughCursor: string;
}>;

export type LiveUpdateDelivery = LiveUpdateWindow & Readonly<{
  waitUntil(promise: Promise<unknown>): void;
}>;

export const LIVE_UPDATE_EVENT = "agentmetry-live-update";

export class LiveUpdateController {
  private readonly client: AgentmetryClient;
  private readonly apply: (window: LiveUpdateWindow) => void | Promise<void>;
  private abort?: AbortController;
  private cursor = "";
  private pending = new Map<string, ProjectionChangeTarget>();
  private resyncRequired = false;
  private throughCursor = "";
  private renderTimer?: ReturnType<typeof setTimeout>;
  private reconnectTimer?: ReturnType<typeof setTimeout>;
  private applying?: AbortSignal;
  private applyRetryDelay = 250;

  constructor(client: AgentmetryClient, apply: (window: LiveUpdateWindow) => void | Promise<void>) {
    this.client = client;
    this.apply = apply;
  }

  start() {
    if (this.abort) return;
    this.abort = new AbortController();
    void this.consume(this.abort.signal, 250);
  }

  stop() {
    this.abort?.abort();
    this.abort = undefined;
    if (this.renderTimer) clearTimeout(this.renderTimer);
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.renderTimer = undefined;
    this.reconnectTimer = undefined;
    this.pending.clear();
    this.resyncRequired = false;
    this.throughCursor = this.cursor;
  }

  private async consume(signal: AbortSignal, retryDelay: number): Promise<void> {
    try {
      for await (const window of this.client.watchProjectionChanges(this.cursor, signal)) {
        if (signal.aborted) return;
        this.throughCursor = window.throughCursor || this.throughCursor || this.cursor;
        if (window.targets.length === 0 && !window.resyncRequired && this.pending.size === 0 && this.applying !== signal) {
          this.cursor = this.throughCursor;
          continue;
        }
        this.resyncRequired ||= window.resyncRequired;
        for (const target of window.targets) this.addTarget(target);
        if (window.resyncRequired) {
          this.pending.set("all-sessions", { kind: ProjectionTargetKind.ALL_SESSIONS, sourceId: "", sessionId: "", traceId: "" });
          this.pending.set("all-traces", { kind: ProjectionTargetKind.ALL_TRACES, sourceId: "", sessionId: "", traceId: "" });
          this.pending.set("overview", { kind: ProjectionTargetKind.OVERVIEW, sourceId: "", sessionId: "", traceId: "" });
        }
        this.scheduleRender();
        retryDelay = 250;
      }
    } catch {
      // Existing data stays visible while a bounded reconnect resumes from cursor.
    }
    if (signal.aborted) return;
    this.reconnectTimer = setTimeout(() => void this.consume(signal, Math.min(retryDelay * 2, 5000)), retryDelay);
  }

  private scheduleRender(delay = 300) {
    if (this.renderTimer) return;
    this.renderTimer = setTimeout(() => {
      this.renderTimer = undefined;
      void this.flush();
    }, delay);
  }

  private async flush() {
    const signal = this.abort?.signal;
    if (!signal || signal.aborted || this.applying === signal) return;
    const targets = [...this.pending.values()];
    const resyncRequired = this.resyncRequired;
    const throughCursor = this.throughCursor;
    if (targets.length === 0 && !resyncRequired) {
      this.cursor = throughCursor || this.cursor;
      return;
    }
    this.pending.clear();
    this.resyncRequired = false;
    this.applying = signal;
    let retry = false;
    try {
      await this.apply({ targets, resyncRequired, throughCursor });
      if (!signal.aborted && this.abort?.signal === signal) {
        this.cursor = throughCursor || this.cursor;
        this.applyRetryDelay = 250;
      }
    } catch {
      if (!signal.aborted && this.abort?.signal === signal) {
        for (const target of targets) this.addTarget(target);
        this.resyncRequired ||= resyncRequired;
        retry = true;
      }
    } finally {
      if (this.applying === signal) this.applying = undefined;
      if (!signal.aborted && this.abort?.signal === signal && (this.pending.size > 0 || this.resyncRequired || this.throughCursor !== this.cursor)) {
        const delay = retry ? this.applyRetryDelay : 300;
        if (retry) this.applyRetryDelay = Math.min(this.applyRetryDelay * 2, 5000);
        this.scheduleRender(delay);
      }
    }
  }

  private addTarget(target: ProjectionChangeTarget) {
    const coarse = coarseTarget(target.kind);
    if (coarse && this.pending.has(targetKey(coarse))) return;
    this.pending.set(targetKey(target), target);
    if (this.pending.size <= 1024) return;
    const compacted = new Map<string, ProjectionChangeTarget>();
    for (const value of this.pending.values()) {
      const replacement = coarseTarget(value.kind) ?? value;
      compacted.set(targetKey(replacement), replacement);
    }
    this.pending = compacted;
  }
}

const targetKey = ({ kind, sourceId, sessionId, traceId }: ProjectionChangeTarget) => `${kind}\u0000${sourceId}\u0000${sessionId}\u0000${traceId}`;

const emptyTarget = (kind: ProjectionTargetKind): ProjectionChangeTarget => ({ kind, sourceId: "", sessionId: "", traceId: "" });
const coarseTarget = (kind: ProjectionTargetKind): ProjectionChangeTarget | undefined => {
  switch (kind) {
    case ProjectionTargetKind.SOURCE: return emptyTarget(ProjectionTargetKind.ALL_SOURCES);
    case ProjectionTargetKind.SESSION: return emptyTarget(ProjectionTargetKind.ALL_SESSIONS);
    case ProjectionTargetKind.TRACE: return emptyTarget(ProjectionTargetKind.ALL_TRACES);
    case ProjectionTargetKind.PLAN_USAGE: return emptyTarget(ProjectionTargetKind.OVERVIEW);
    default: return undefined;
  }
};

export const affectsOverview = (targets: readonly ProjectionChangeTarget[]) => targets.some(({ kind }) =>
  kind === ProjectionTargetKind.OVERVIEW || kind === ProjectionTargetKind.SOURCE || kind === ProjectionTargetKind.ALL_SOURCES || kind === ProjectionTargetKind.PLAN_USAGE,
);

export const affectsSessionList = (targets: readonly ProjectionChangeTarget[], sourceFilter: string) => targets.some(({ kind, sourceId }) =>
  kind === ProjectionTargetKind.ALL_SESSIONS || (kind === ProjectionTargetKind.SESSION && (!sourceFilter || sourceFilter === sourceId))
    || kind === ProjectionTargetKind.ALL_SOURCES || (kind === ProjectionTargetKind.SOURCE && (!sourceFilter || sourceFilter === sourceId)),
);

export const affectsSession = (targets: readonly ProjectionChangeTarget[], sourceId: string, sessionId: string) => targets.some((target) =>
  target.kind === ProjectionTargetKind.ALL_SESSIONS
    || (target.kind === ProjectionTargetKind.SESSION && target.sourceId === sourceId && target.sessionId === sessionId),
);

export const affectsTrace = (targets: readonly ProjectionChangeTarget[], traceId: string) => targets.some((target) =>
  target.kind === ProjectionTargetKind.ALL_TRACES || (target.kind === ProjectionTargetKind.TRACE && target.traceId === traceId),
);
