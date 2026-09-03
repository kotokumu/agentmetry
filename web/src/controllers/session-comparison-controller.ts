import { Task, TaskStatus } from "@lit/task";
import { Code, ConnectError } from "@connectrpc/connect";
import type { ReactiveControllerHost } from "lit";
import type { AgentmetryClient } from "../api/agentmetry-client";
import { eligibleComparisonBaselines, type ReworkComparisonViewState, type SharedReworkComparison } from "../model/rework-comparison";
import type { Session } from "../model/telemetry";
import { affectsSession, type LiveUpdateWindow } from "./live-update-controller";

type ComparisonResult = Readonly<{ key: string; report?: SharedReworkComparison }>;
export type SessionComparisonReader = Pick<AgentmetryClient, "compareRework">;
export type SessionComparisonDependencies = Readonly<{
  reader: SessionComparisonReader;
  current: () => Session | undefined;
  sessions: () => readonly Session[];
  isActive?: () => boolean;
}>;
type ExplicitSelection = Readonly<{ currentKey: string; baselineId: string }>;

export class SessionComparisonController {
  private readonly dependencies: Required<SessionComparisonDependencies>;
  private readonly comparisonTask: Task<readonly [boolean, string, string, string, string], ComparisonResult>;
  private explicitSelection?: ExplicitSelection;
  private observedCurrentKey = "";
  private wasDisconnected = false;

  constructor(private readonly host: ReactiveControllerHost, dependencies: SessionComparisonDependencies) {
    this.dependencies = { ...dependencies, isActive: dependencies.isActive ?? (() => true) };
    host.addController(this);
    this.comparisonTask = new Task(host, {
      args: () => {
        const current = this.dependencies.current();
        const baseline = this.baselineTarget;
        return [this.dependencies.isActive(), current?.sourceId ?? "", current?.id ?? "", baseline?.sourceId ?? "", baseline?.id ?? ""] as const;
      },
      task: async ([active, currentSourceId, currentSessionId, baselineSourceId, baselineSessionId], { signal }) => {
        const key = comparisonKey(currentSourceId, currentSessionId, baselineSourceId, baselineSessionId);
        if (!active || !currentSourceId || !currentSessionId || !baselineSourceId || !baselineSessionId) return { key };
        const report = await this.dependencies.reader.compareRework({
          baseline: { sourceId: baselineSourceId, sessionId: baselineSessionId },
          current: { sourceId: currentSourceId, sessionId: currentSessionId },
        }, signal);
        if (report.baseline.sourceId !== baselineSourceId || report.baseline.sessionId !== baselineSessionId
          || report.current.sourceId !== currentSourceId || report.current.sessionId !== currentSessionId) {
          throw new Error("Comparison identities do not match the requested conversations.");
        }
        return { key, report };
      },
    });
  }

  hostUpdate() {
    const key = this.currentKey;
    if (key === this.observedCurrentKey) return;
    this.observedCurrentKey = key;
    this.explicitSelection = undefined;
  }

  hostDisconnected() {
    this.wasDisconnected = true;
    this.comparisonTask.abort();
  }

  hostConnected() {
    if (!this.wasDisconnected) return;
    this.wasDisconnected = false;
    void this.comparisonTask.run();
  }

  get candidates(): readonly Session[] {
    const current = this.dependencies.current();
    return current ? eligibleComparisonBaselines(current, this.dependencies.sessions()) : [];
  }

  private get currentKey(): string {
    const current = this.dependencies.current();
    return current ? sessionKey(current.sourceId, current.id) : "";
  }

  private get baselineTarget(): Session | undefined {
    const candidates = this.candidates;
    if (this.explicitSelection?.currentKey === this.currentKey) {
      const selected = candidates.find(({ id }) => id === this.explicitSelection?.baselineId);
      if (selected) return selected;
    }
    return candidates[0];
  }

  get selectedBaselineId(): string { return this.baselineTarget?.id ?? ""; }
  get loading(): boolean { return Boolean(this.baselineTarget) && this.comparisonTask.status === TaskStatus.PENDING; }
  get failed(): boolean { return Boolean(this.baselineTarget) && this.comparisonTask.status === TaskStatus.ERROR; }

  viewState(): ReworkComparisonViewState {
    const options = this.candidates.map(({ id, endedAt }) => ({ sessionId: id, endedAt }));
    if (options.length === 0) return { status: "empty" };
    const context = { options, selectedBaselineId: this.selectedBaselineId } as const;
    if (this.loading) return { ...context, status: "loading" };
    if (this.failed) {
      const error = this.comparisonTask.error;
      const message = error instanceof ConnectError && error.code === Code.Unimplemented
        ? "This server does not support shared diagnostic comparison."
        : error instanceof Error ? error.message : "Diagnostic comparison could not be loaded.";
      return { ...context, status: "failed", message };
    }
    const current = this.dependencies.current();
    const target = this.baselineTarget;
    const result = this.comparisonTask.value;
    if (!this.dependencies.isActive() || !current || !target || !result?.report
      || result.key !== comparisonKey(current.sourceId, current.id, target.sourceId, target.id)) {
      return { ...context, status: "waiting", message: "Waiting for the selected comparison." };
    }
    const report = result.report;
    return report.status === "invalid"
      ? { ...context, status: "invalid", code: report.code, reason: report.reason }
      : { ...context, status: "ready", rows: report.rows, warnings: report.warnings, harness: report.harness };
  }

  selectBaseline(sessionId: string) {
    if (!this.candidates.some(({ id }) => id === sessionId)) return;
    this.explicitSelection = { currentKey: this.currentKey, baselineId: sessionId };
    this.host.requestUpdate();
  }

  refresh(): Promise<void> { return this.comparisonTask.run().then(() => undefined); }

  applyLiveUpdate(window: LiveUpdateWindow): Promise<void> {
    const baseline = this.baselineTarget;
    const current = this.dependencies.current();
    if (!baseline || !current || (!window.resyncRequired
      && !affectsSession(window.targets, baseline.sourceId, baseline.id)
      && !affectsSession(window.targets, current.sourceId, current.id))) return Promise.resolve();
    return this.refresh();
  }
}

const sessionKey = (sourceId: string, sessionId: string) => `${sourceId}\u0000${sessionId}`;
const comparisonKey = (currentSourceId: string, currentSessionId: string, baselineSourceId: string, baselineSessionId: string) =>
  `${sessionKey(currentSourceId, currentSessionId)}\u0000${sessionKey(baselineSourceId, baselineSessionId)}`;
