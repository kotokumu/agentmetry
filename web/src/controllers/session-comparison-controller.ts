import { Task, TaskStatus } from "@lit/task";
import type { ReactiveControllerHost } from "lit";
import type { AgentmetryClient } from "../api/agentmetry-client";
import {
  buildReworkComparisonReport,
  eligibleComparisonBaselines,
  type ReworkComparisonViewState,
} from "../model/rework-comparison";
import type { ReworkAnalysis, Session } from "../model/telemetry";
import { affectsSession, type LiveUpdateWindow } from "./live-update-controller";

type BaselineResult = Readonly<{ key: string; session?: Session; analysis?: ReworkAnalysis }>;
export type ComparisonBaseline = Readonly<{ session: Session; analysis: ReworkAnalysis }>;
export type SessionComparisonReader = Pick<AgentmetryClient, "getSessionSummary" | "getSessionRework">;
export type SessionComparisonDependencies = Readonly<{
  reader: SessionComparisonReader;
  current: () => Session | undefined;
  sessions: () => readonly Session[];
  isActive?: () => boolean;
}>;
type ExplicitSelection = Readonly<{ currentKey: string; baselineId: string }>;

export class SessionComparisonController {
  private readonly dependencies: Required<SessionComparisonDependencies>;
  private readonly baselineTask: Task<readonly [boolean, string, string, string, string], BaselineResult>;
  private explicitSelection?: ExplicitSelection;
  private observedCurrentKey = "";
  private wasDisconnected = false;

  constructor(private readonly host: ReactiveControllerHost, dependencies: SessionComparisonDependencies) {
    this.dependencies = { ...dependencies, isActive: dependencies.isActive ?? (() => true) };
    host.addController(this);
    this.baselineTask = new Task(host, {
      args: () => {
        const current = this.dependencies.current();
        const baseline = this.baselineTarget;
        return [
          this.dependencies.isActive(), current?.sourceId ?? "", current?.id ?? "",
          baseline?.sourceId ?? "", baseline?.id ?? "",
        ] as const;
      },
      task: async ([active, currentSourceId, currentSessionId, baselineSourceId, baselineSessionId], { signal }) => {
        const key = comparisonKey(currentSourceId, currentSessionId, baselineSourceId, baselineSessionId);
        if (!active || !currentSourceId || !currentSessionId || !baselineSourceId || !baselineSessionId) return { key };
        const [session, analysis] = await Promise.all([
          this.dependencies.reader.getSessionSummary(baselineSourceId, baselineSessionId, signal),
          this.dependencies.reader.getSessionRework(baselineSourceId, baselineSessionId, signal),
        ]);
        return { key, session, analysis };
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
    this.baselineTask.abort();
  }

  hostConnected() {
    if (!this.wasDisconnected) return;
    this.wasDisconnected = false;
    void this.baselineTask.run();
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

  get selectedBaselineId(): string {
    return this.baselineTarget?.id ?? "";
  }

  get baseline(): ComparisonBaseline | undefined {
    if (!this.dependencies.isActive()) return undefined;
    const current = this.dependencies.current();
    const target = this.baselineTarget;
    if (!current || !target) return undefined;
    const expectedKey = comparisonKey(current.sourceId, current.id, target.sourceId, target.id);
    const result = this.baselineTask.value;
    if (result?.key !== expectedKey || !result.session || !result.analysis) return undefined;
    if (result.session.sourceId !== target.sourceId || result.session.id !== target.id) return undefined;
    if (result.analysis.sourceId !== target.sourceId || result.analysis.sessionId !== target.id) return undefined;
    return { session: result.session, analysis: result.analysis };
  }

  get loading(): boolean {
    return Boolean(this.baselineTarget) && this.baselineTask.status === TaskStatus.PENDING && this.baseline === undefined;
  }

  get failed(): boolean {
    return Boolean(this.baselineTarget) && this.baselineTask.status === TaskStatus.ERROR;
  }

  viewState(currentAnalysis?: ReworkAnalysis, currentFailed = false): ReworkComparisonViewState {
    const options = this.candidates.map(({ id, endedAt }) => ({ sessionId: id, endedAt }));
    if (options.length === 0) return { status: "empty" };
    const context = { options, selectedBaselineId: this.selectedBaselineId } as const;
    if (this.loading) return { ...context, status: "loading" };
    if (this.failed) return { ...context, status: "failed", message: "Baseline diagnostics could not be loaded." };
    const current = this.dependencies.current();
    const baseline = this.baseline;
    if (!currentAnalysis || !current || !baseline) {
      return {
        ...context,
        status: "waiting",
        message: currentFailed ? "Current diagnostics are unavailable above." : "Both current and baseline evidence are required.",
      };
    }
    const report = buildReworkComparisonReport(baseline.session, baseline.analysis, current, currentAnalysis);
    return report.status === "invalid"
      ? { ...context, status: "invalid", code: report.code, reason: report.reason }
      : { ...context, status: "ready", rows: report.rows, warnings: report.warnings };
  }

  selectBaseline(sessionId: string) {
    if (!this.candidates.some(({ id }) => id === sessionId)) return;
    this.explicitSelection = { currentKey: this.currentKey, baselineId: sessionId };
    this.host.requestUpdate();
  }

  refresh(): Promise<void> {
    return this.baselineTask.run().then(() => undefined);
  }

  applyLiveUpdate(window: LiveUpdateWindow): Promise<void> {
    const baseline = this.baselineTarget;
    if (!baseline || (!window.resyncRequired && !affectsSession(window.targets, baseline.sourceId, baseline.id))) return Promise.resolve();
    return this.baselineTask.run().then(() => undefined);
  }
}

const sessionKey = (sourceId: string, sessionId: string) => `${sourceId}\u0000${sessionId}`;
const comparisonKey = (currentSourceId: string, currentSessionId: string, baselineSourceId: string, baselineSessionId: string) =>
  `${sessionKey(currentSourceId, currentSessionId)}\u0000${sessionKey(baselineSourceId, baselineSessionId)}`;
