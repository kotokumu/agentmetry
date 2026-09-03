import { conditionsKey, hasSessionConditions, sessionConditions, type SessionConditions } from "../model/investigation-conditions";
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { timestampDate, timestampFromDate, type Timestamp } from "@bufbuild/protobuf/wkt";
import {
  AgentmetryQueryService,
  ActivityMutationOperation,
  PageDirection,
  ProjectionTargetKind,
  TimeRange,
  type Activity as ActivityMessage,
  type AgentSummary,
  type Dashboard,
  type CompareReworkResponse,
  type ReworkComparisonSummary as ComparisonSummaryMessage,
  type ReworkComparisonValue as ComparisonValueMessage,
  type ReworkCoverage as ReworkCoverageMessage,
  type HarnessContext as HarnessContextMessage,
  type GetTraceOverviewResponse,
  type GetTraceResponse,
  type GetTraceWindowResponse,
  type PlanUsageSnapshot,
  type SessionSummary,
  type TokenUsage as TokenUsageMessage,
} from "../gen/agentmetry/v1/agentmetry_pb";
import type {
  Activity,
  ActivityDirection,
  ContentEvidence,
  AgentSession,
  DashboardSummary,
  HarnessContext,
  PlanUsageSnapshot as PlanUsage,
  ReworkAnalysis,
  Session,
  TimeRange as UiTimeRange,
  TokenUsage,
  Trace,
} from "../model/telemetry";
import type { TraceInvestigationWindow, TraceOverview, TraceWindowResult } from "../model/trace-investigation";

import { compareHarnessContexts, displayComparisonDirection, type ComparisonMetricID, type ComparisonSubject, type ComparisonValue, type ReworkComparisonPair, type ReworkComparisonRow, type SharedReworkComparison } from "../model/rework-comparison";

const transport = createConnectTransport({ baseUrl: "" });
const client = createClient(AgentmetryQueryService, transport);

export type ActivityPage = Readonly<{
  activities: readonly Activity[];
  total: number;
  offset: number;
  hasEarlier: boolean;
  hasMore: boolean;
  nextPageToken?: string;
  previousPageToken?: string;
}>;

export type ProjectionChangeTarget = Readonly<{
  kind: ProjectionTargetKind;
  sourceId: string;
  sessionId: string;
  traceId: string;
}>;

export type ProjectionChangeWindow = Readonly<{
  throughCursor: string;
  targets: readonly ProjectionChangeTarget[];
  resyncRequired: boolean;
}>;

export type ActivityMutation = Readonly<{ operation: "upsert" | "remove"; activityId: string; activity?: Activity }>;
export type ActivitySyncPage = Readonly<{ mutations: readonly ActivityMutation[]; throughCursor: string; resyncRequired: boolean; nextPageToken?: string }>;

export class ExactTraceEvidenceUnavailableError extends Error {
  constructor(readonly spanId: string) {
    super(`Requested span ${spanId} was not returned. Exact evidence is unavailable on this server.`);
    this.name = "ExactTraceEvidenceUnavailableError";
  }
}

export const agentmetryClient = {
  async *watchProjectionChanges(afterCursor: string, signal?: AbortSignal): AsyncGenerator<ProjectionChangeWindow> {
    for await (const response of client.watchProjectionChanges({ afterCursor }, signal ? { signal } : undefined)) {
      yield {
        throughCursor: response.throughCursor,
        targets: response.targets.map(({ kind, sourceId, sessionId, traceId }) => ({ kind, sourceId, sessionId, traceId })),
        resyncRequired: response.resyncRequired,
      };
    }
  },

  async getDashboard(range: UiTimeRange, sourceId: string, search: string, signal?: AbortSignal): Promise<DashboardSummary> {
    const response = await client.getDashboard(
      { filter: { range: toTimeRange(range), sourceId, search } },
      signal ? { signal } : undefined,
    );
    if (!response.dashboard) throw new Error("Dashboard response was empty");
    return mapDashboard(response.dashboard);
  },

  async listSessions(range: UiTimeRange, sourceId: string, search: string, signal?: AbortSignal, conditions: SessionConditions = {}): Promise<readonly Session[]> {
    const response = await client.listSessions(
      { filter: { range: toTimeRange(range), sourceId, search }, page: { pageSize: 100 }, conditions: hasSessionConditions(conditions) ? sessionConditions(conditions) : undefined },
      signal ? { signal } : undefined,
    );
    assertSessionConditionsApplied(conditions, response.appliedConditions);
    return response.sessions.map((session) => mapSession(session));
  },

  async getSession(sourceId: string, sessionId: string, traceId?: string, spanId?: string, signal?: AbortSignal): Promise<Session> {
	const session = await this.getSessionSummary(sourceId, sessionId, signal);
    const page = await this.listSessionActivities(sourceId, sessionId, "older", 0, 100, "", traceId, spanId, undefined, signal);
    return { ...session, activities: page.activities, activityOffset: page.offset, hasEarlier: page.hasEarlier, hasMore: page.hasMore, nextPageToken: page.nextPageToken, previousPageToken: page.previousPageToken };
  },

  async getSessionRework(sourceId: string, sessionId: string, signal?: AbortSignal): Promise<ReworkAnalysis> {
    const response = await client.getSessionRework({ sourceId, sessionId }, signal ? { signal } : undefined);
    if (!response.metrics || !response.coverage || !response.capabilities?.changeRevert || !response.capabilities.crossAgentOverlap) {
      throw new Error("Session rework response was incomplete");
    }
    const metrics = response.metrics;
    const harnessContext = mapHarnessContext(response.harnessContext);
    return {
      sourceId: response.sourceId,
      sessionId: response.sessionId,
      sessionTokens: mapOptionalSessionTokens(response.sessionTokens, harnessContext),
      harness: harnessContext,
      metrics: {
        validationFailures: Number(metrics.validationFailures),
        failFixRetryCycles: Number(metrics.failFixRetryCycles),
        reworkDurationMs: Number(metrics.reworkDurationMs),
        totalAgentEffortMs: Number(metrics.totalAgentEffortMs),
        reworkAgentEffortRate: metrics.reworkAgentEffortRate === undefined ? null : metrics.reworkAgentEffortRate,
        reworkTokens: mapTokens(metrics.reworkTokens),
        toolAttemptsWithOutcome: Number(metrics.toolAttemptsWithOutcome),
        toolFailures: Number(metrics.toolFailures),
        toolFailureRate: metrics.toolFailureRate === undefined ? null : metrics.toolFailureRate,
        apiRetryWaste: {
          attempts: Number(metrics.apiRetryWaste?.attempts ?? 0),
          durationMs: Number(metrics.apiRetryWaste?.durationMs ?? 0),
          tokens: mapTokens(metrics.apiRetryWaste?.tokens),
        },
        repeatedCommands: Number(metrics.repeatedCommands),
        reeditedFiles: Number(metrics.reeditedFiles),
        validationAttemptsWithOutcome: Number(metrics.validationAttemptsWithOutcome),
        firstPassEligibleValidations: Number(metrics.firstPassEligibleValidations),
        firstPassSuccesses: Number(metrics.firstPassSuccesses),
        firstPassSuccessRate: metrics.firstPassSuccessRate === undefined ? null : metrics.firstPassSuccessRate,
        recurringFailureLoops: Number(metrics.recurringFailureLoops),
        repeatedFailureAttempts: Number(metrics.repeatedFailureAttempts),
        resolvedFailureLoops: Number(metrics.resolvedFailureLoops),
        unresolvedFailureLoops: Number(metrics.unresolvedFailureLoops),
        failureResolutionDurationMs: Number(metrics.failureResolutionDurationMs),
        failureResolutionTokens: mapTokens(metrics.failureResolutionTokens),
      },
      coverage: mapReworkCoverage(response.coverage),
      capabilities: {
        changeRevert: { state: response.capabilities.changeRevert.state, reason: response.capabilities.changeRevert.reason },
        crossAgentOverlap: { state: response.capabilities.crossAgentOverlap.state, reason: response.capabilities.crossAgentOverlap.reason },
      },
      failureEpisodes: response.failureEpisodes.map((episode) => ({
        agentId: episode.agentId, operation: episode.operation, validationFingerprint: episode.validationFingerprint,
        errorFingerprints: [...episode.errorFingerprints], failureAttempts: Number(episode.failureAttempts),
        resolved: episode.resolved, resolutionDurationMs: Number(episode.resolutionDurationMs),
        resolutionTokens: mapTokens(episode.resolutionTokens),
        traceId: episode.traceId, spanId: episode.spanId,
      })),
    };
  },

  async compareRework(pair: ReworkComparisonPair, signal?: AbortSignal): Promise<SharedReworkComparison> {
    const response = await client.compareRework(pair, signal ? { signal } : undefined);
    return mapReworkComparison(response);
  },

  async getSessionSummary(sourceId: string, sessionId: string, signal?: AbortSignal): Promise<Session> {
	const response = await client.getSession({ sourceId, sessionId }, signal ? { signal } : undefined);
	if (!response.session) throw new Error("Session response was empty");
	return mapSession(response.session, response.traceIds);
  },

  async syncSessionActivities(sourceId: string, sessionId: string, afterCursor: string, throughCursor: string, pageToken = "", signal?: AbortSignal): Promise<ActivitySyncPage> {
	const response = await client.syncSessionActivities({ sourceId, sessionId, afterCursor, throughCursor, page: { pageSize: 100, pageToken } }, signal ? { signal } : undefined);
	return mapActivitySync(response);
  },

  async syncTraceActivities(traceId: string, afterCursor: string, throughCursor: string, pageToken = "", signal?: AbortSignal): Promise<ActivitySyncPage> {
	const response = await client.syncTraceActivities({ traceId, afterCursor, throughCursor, page: { pageSize: 100, pageToken } }, signal ? { signal } : undefined);
	return mapActivitySync(response);
  },

  async listSessionActivities(sourceId: string, sessionId: string, direction: ActivityDirection, offset: number, limit: number, pageToken = "", traceId?: string, spanId?: string, agentId?: string, signal?: AbortSignal): Promise<ActivityPage> {
    const response = await client.listSessionActivities({
      sourceId,
      sessionId,
      page: { pageSize: limit, pageToken },
      direction: direction === "newer" ? PageDirection.NEWER : PageDirection.OLDER,
      anchor: traceId && spanId ? { traceId, spanId } : undefined,
      agentId: agentId || "",
    }, signal ? { signal } : undefined);
    const page = response.page;
    const actualOffset = Number(page?.startOffset ?? offset);
    return {
      activities: response.activities.map(mapActivity),
      total: Number(response.total),
      offset: actualOffset,
      hasEarlier: actualOffset > 0,
      hasMore: page?.hasMore ?? false,
      nextPageToken: page?.nextPageToken || undefined,
      previousPageToken: page?.previousPageToken || undefined,
    };
  },

  async getTrace(traceId: string, offset = 0, limit = 100, pageToken = "", signal?: AbortSignal, liveTail = false, anchorSpanId = ""): Promise<Trace> {
    const response = await client.getTrace({ traceId, page: { pageSize: limit, pageToken }, liveTail, anchorSpanId }, signal ? { signal } : undefined);
    if (anchorSpanId && !response.activities.some((activity) => activity.signal === "trace" && activity.traceId === traceId.toLowerCase() && activity.spanId === anchorSpanId.toLowerCase())) {
      throw new ExactTraceEvidenceUnavailableError(anchorSpanId);
    }
    return mapTrace(response, offset);
  },

  async getTraceOverview(traceId: string, signal?: AbortSignal): Promise<TraceOverview> {
    const response = await client.getTraceOverview({ traceId }, signal ? { signal } : undefined);
    return mapTraceOverview(response);
  },

  async getTraceWindow(traceId: string, window: TraceInvestigationWindow, offset = 0, limit = 100, pageToken = "", signal?: AbortSignal): Promise<TraceWindowResult> {
    const response = await client.getTraceWindow({
      traceId,
      window: {
        startedAt: window.startedAt ? timestampFromDate(new Date(window.startedAt)) : undefined,
        endedAt: window.endedAt ? timestampFromDate(new Date(window.endedAt)) : undefined,
        kind: window.kind,
        errorsOnly: window.errorsOnly,
      },
      page: { pageSize: limit, pageToken },
    }, signal ? { signal } : undefined);
    return mapTraceWindow(response, offset);
  },
};

export type AgentmetryClient = typeof agentmetryClient;

function mapDashboard(value: Dashboard): DashboardSummary {
  return {
    sources: value.sources.map((source) => ({ id: source.id, label: source.label })),
    signalCounts: {
      traces: Number(value.signalCounts?.traces ?? 0),
      logs: Number(value.signalCounts?.logs ?? 0),
      metrics: Number(value.signalCounts?.metrics ?? 0),
    },
    runCount: Number(value.runCount),
    agentCount: Number(value.agentCount),
    tokens: mapTokens(value.tokens),
    recentActivity: value.recentActivity.map(mapActivity),
    planUsage: value.planUsage.map(mapPlanUsage),
  };
}

export function mapHarnessContext(value?: HarnessContextMessage): HarnessContext {
  if (!value) return { availability: "unavailable", reason: "server_unsupported" };
  const raw = value.counts;
  if (!raw) return invalidHarnessPayload();
  const counts = {
    eligibleRecords: Number(raw.eligibleRecords),
    reportedRecords: Number(raw.reportedRecords),
    unreportedRecords: Number(raw.unreportedRecords),
    invalidRecords: Number(raw.invalidRecords),
    distinctIdentities: Number(raw.distinctIdentities),
  };
  const values = Object.values(counts);
  if (values.some((count) => !Number.isSafeInteger(count) || count < 0)
    || counts.eligibleRecords !== counts.reportedRecords + counts.unreportedRecords + counts.invalidRecords
    || counts.distinctIdentities > counts.reportedRecords) return invalidHarnessPayload();
  switch (value.classification.case) {
    case "noEligibleRecords":
      return values.every((count) => count === 0)
        ? { availability: "available", state: "no_eligible_records", counts }
        : invalidHarnessPayload();
    case "unreported":
      return counts.eligibleRecords > 0 && counts.unreportedRecords === counts.eligibleRecords
        && counts.reportedRecords === 0 && counts.invalidRecords === 0 && counts.distinctIdentities === 0
        ? { availability: "available", state: "unreported", counts }
        : invalidHarnessPayload();
    case "uniform": {
      const identity = value.classification.value.identity;
      if (counts.eligibleRecords <= 0 || counts.reportedRecords !== counts.eligibleRecords
        || counts.unreportedRecords !== 0 || counts.invalidRecords !== 0 || counts.distinctIdentities !== 1
        || !identity || !validHarnessScope(identity.scope) || !validHarnessFingerprint(identity.fingerprint)
        || !validHarnessLabel(identity.label)) return invalidHarnessPayload();
      return {
        availability: "available", state: "uniform", counts,
        identity: { scope: identity.scope, fingerprint: identity.fingerprint, label: identity.label || undefined },
      };
    }
    case "mixed":
      return counts.eligibleRecords > 0 && counts.reportedRecords === counts.eligibleRecords
        && counts.unreportedRecords === 0 && counts.invalidRecords === 0 && counts.distinctIdentities > 1
        ? { availability: "available", state: "mixed", counts }
        : invalidHarnessPayload();
    case "incomplete":
      return counts.eligibleRecords > 0 && counts.reportedRecords > 0 && counts.unreportedRecords > 0
        && counts.invalidRecords === 0 && counts.distinctIdentities > 0
        ? { availability: "available", state: "incomplete", counts }
        : invalidHarnessPayload();
    case "invalid":
      return counts.eligibleRecords > 0 && counts.invalidRecords > 0
        ? { availability: "available", state: "invalid", counts }
        : invalidHarnessPayload();
    default:
      return invalidHarnessPayload();
  }
}

const invalidHarnessPayload = (): HarnessContext => ({ availability: "unavailable", reason: "invalid_server_payload" });
export const mapOptionalSessionTokens = (value: TokenUsageMessage | undefined, harnessContext: HarnessContext): TokenUsage | undefined => {
  if (value) return mapTokens(value);
  return harnessContext.availability === "unavailable" && harnessContext.reason === "server_unsupported"
    ? undefined
    : mapTokens(undefined);
};
const validHarnessScope = (value: string) => /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(value);
const validHarnessFingerprint = (value: string) => /^sha256:[0-9a-f]{64}$/.test(value);
const validHarnessLabel = (value: string) => value.trim() === value && Array.from(value).length <= 80 && !/[\p{Cc},]/u.test(value);

function mapSession(value: SessionSummary, traceIds: readonly string[] = []): Session {
  return {
    id: value.id,
    sourceId: value.sourceId,
    sources: value.sources.map((source) => ({ id: source.id, label: source.label })),
    traceIds,
    startedAt: timeValue(value.startedAt),
    endedAt: timeValue(value.endedAt),
    activityCount: Number(value.activityCount),
    agentCount: Number(value.agentCount),
    tokens: mapTokens(value.tokens),
    costUsd: value.costUsd,
    agents: value.agents.map(mapAgent),
    activities: [],
  };
}

function mapAgent(value: AgentSummary): AgentSession {
  return {
    agentId: value.agentId,
    agentDefinition: value.agentDefinition || undefined,
    agentType: value.agentType || undefined,
    parentAgentId: value.parentAgentId || undefined,
    model: value.model || undefined,
    activityCount: Number(value.activityCount),
    tokens: mapTokens(value.tokens),
  };
}

export function mapActivityContentEvidence(value: ActivityMessage): ContentEvidence {
  const fallback: ContentEvidence = { source: value.source, activityId: value.id, signal: value.signal, kind: "unknown", evidence: "unknown", availability: value.content ? "available" : "not_reported", fields: [], truncated: false };
  const evidence = value.contentEvidence;
  if (!evidence || evidence.source !== value.source || evidence.activityId !== value.id || evidence.signal !== value.signal) return fallback;
  const availability = ["available", "not_reported", "redacted", "not_returned"].includes(evidence.availability)
    ? evidence.availability as ContentEvidence["availability"] : fallback.availability;
  const knownKind = ["prompt", "response", "tool_input", "tool_output", "tool_input_output", "model_input", "reference", "unknown"].includes(evidence.kind);
  const knownEvidence = ["reference", "read_output", "explicit_model_input", "unknown"].includes(evidence.evidence);
  if (!knownKind || !knownEvidence) return { ...fallback, availability };
  const fields = evidence.fields.filter((field) => ["prompt", "response", "tool_input", "tool_parameters", "full_command", "file_path", "error", "body", "body_ref", "arguments.message", "output"].includes(field));
  const redactionReason = ["producer_redacted", "encrypted_input"].includes(evidence.redactionReason)
    ? evidence.redactionReason as ContentEvidence["redactionReason"] : undefined;
  return { ...fallback, kind: evidence.kind as ContentEvidence["kind"], evidence: evidence.evidence as ContentEvidence["evidence"], availability, fields, truncated: evidence.truncated, ...(redactionReason ? { redactionReason } : {}) };
}

function mapActivity(value: ActivityMessage): Activity {
  return {
    id: value.id,
    source: value.source,
    signal: value.signal as Activity["signal"],
    traceId: value.traceId || undefined,
    spanId: value.spanId || undefined,
    parentSpanId: value.parentSpanId || undefined,
    missingParent: value.missingParent,
    promptId: value.promptId || undefined,
    usageId: value.usageId || undefined,
    relatedTraceId: value.relatedTraceId || undefined,
    relatedSpanId: value.relatedSpanId || undefined,
    name: value.name,
    kind: value.kind as Activity["kind"],
    toolName: value.toolName || undefined,
    targetAgentId: value.targetAgentId || undefined,
    targetAgentType: value.targetAgentType || undefined,
    content: value.content || undefined,
    contentEvidence: mapActivityContentEvidence(value),
    agentId: value.agentId,
    agentDefinition: value.agentDefinition || undefined,
    agentType: value.agentType || undefined,
    parentAgentId: value.parentAgentId || undefined,
    runId: value.runId,
    model: value.model,
    startedAt: timeValue(value.startedAt),
    endedAt: timeValue(value.endedAt),
    observedAt: timeValue(value.observedAt),
    status: value.status || undefined,
    tokens: mapTokens(value.tokens),
    costUsd: value.costUsd,
    contributesToTotal: value.contributesToTotal,
  };
}

const mapTrace = (response: GetTraceResponse, offset = 0): Trace => {
  const page = response.page;
  return {
    traceId: response.traceId,
    startedAt: timeValue(response.startedAt),
    endedAt: timeValue(response.endedAt),
    status: response.status,
    rootSpanCount: Number(response.rootSpanCount),
    missingParentCount: Number(response.missingParentCount),
    conversations: response.conversations.map((value) => ({ sourceId: value.sourceId, id: value.id })),
    agents: response.agents.map((value) => ({
      sourceId: value.sourceId,
      conversationId: value.conversationId,
      agentId: value.agentId,
      agentDefinition: value.agentDefinition || undefined,
      agentType: value.agentType || undefined,
      parentAgentId: value.parentAgentId || undefined,
      model: value.model || undefined,
    })),
    activities: response.activities.map(mapActivity),
    activityOffset: Number(page?.startOffset ?? offset),
    activityCount: Number(response.totalActivities),
    hasMore: page?.hasMore ?? false,
    nextPageToken: page?.nextPageToken || undefined,
    previousPageToken: page?.previousPageToken || undefined,
  };
};

export const mapTraceOverview = (response: GetTraceOverviewResponse): TraceOverview => ({
  traceId: response.traceId,
  startedAt: timeValue(response.startedAt),
  endedAt: timeValue(response.endedAt),
  totalActivities: Number(response.totalActivities),
  returnedActivities: Number(response.returnedActivities),
  coverage: response.coverage,
  activities: response.activities.map((activity) => ({
    id: activity.id,
    source: activity.source,
    signal: activity.signal as Activity["signal"],
    spanId: activity.spanId || undefined,
    parentSpanId: activity.parentSpanId || undefined,
    name: activity.name,
    kind: activity.kind as Activity["kind"],
    status: activity.status || undefined,
    startedAt: timeValue(activity.startedAt),
    endedAt: timeValue(activity.endedAt),
    missingParent: activity.missingParent,
  })),
});

export const mapTraceWindow = (response: GetTraceWindowResponse, offset = 0): TraceWindowResult => {
  if (!response.trace) throw new Error("Trace window response was empty");
  return { trace: mapTrace(response.trace, offset), matchingActivities: Number(response.matchingActivities) };
};

function mapActivitySync(value: { mutations: readonly { operation: ActivityMutationOperation; activityId: string; activity?: ActivityMessage }[]; throughCursor: string; resyncRequired: boolean; page?: { nextPageToken: string } }): ActivitySyncPage {
	return {
		mutations: value.mutations.map((mutation) => ({ operation: mutation.operation === ActivityMutationOperation.REMOVE ? "remove" : "upsert", activityId: mutation.activityId, activity: mutation.activity ? mapActivity(mutation.activity) : undefined })),
		throughCursor: value.throughCursor,
		resyncRequired: value.resyncRequired,
		nextPageToken: value.page?.nextPageToken || undefined,
	};
}

function mapTokens(value?: TokenUsageMessage): TokenUsage {
  return {
    input: value?.input === undefined ? null : Number(value.input),
    output: value?.output === undefined ? null : Number(value.output),
    cacheRead: value?.cacheRead === undefined ? null : Number(value.cacheRead),
    cacheWrite: value?.cacheWrite === undefined ? null : Number(value.cacheWrite),
    reasoning: value?.reasoning === undefined ? null : Number(value.reasoning),
    total: value?.total === undefined ? null : Number(value.total),
  };
}

function mapPlanUsage(value: PlanUsageSnapshot): PlanUsage {
  return {
    source: value.source,
    accountId: value.accountId || undefined,
    plan: value.plan || undefined,
    windowId: value.windowId,
    windowDurationMinutes: value.windowDurationMinutes || undefined,
    usedPercent: value.usedPercent,
    resetsAt: timeValue(value.resetsAt),
    capturedAt: timeValue(value.capturedAt),
    authority: value.authority,
  };
}

function toTimeRange(value: UiTimeRange): TimeRange {
  switch (value) {
    case "1h": return TimeRange.ONE_HOUR;
    case "7d": return TimeRange.SEVEN_DAYS;
    default: return TimeRange.ONE_DAY;
  }
}

function timeValue(value: { seconds: bigint; nanos: number } | undefined): string {
  return value ? timestampDate(value as Timestamp).toISOString() : "";
}

const comparisonMetricIDs: readonly ComparisonMetricID[] = ["initial_validation_success_proxy", "rework_token_share", "retry_cycle_effort_share", "tool_failure_rate", "recurring_loops_per_100_validations"];
const invalidComparisonResponse = () => new Error("Invalid or unsupported diagnostic comparison response.");

export function mapReworkComparison(response: CompareReworkResponse): SharedReworkComparison {
  const baseline = mapComparisonSummary(response.baseline);
  const current = mapComparisonSummary(response.current);
  if (response.status === "invalid") {
    const code = response.code;
    if (code !== "identity_mismatch" && code !== "invalid_time" && code !== "baseline_ineligible") throw invalidComparisonResponse();
    if (!response.reason) throw invalidComparisonResponse();
    return { status: "invalid", code, reason: response.reason, baseline, current };
  }
  if (response.status !== "ready" || response.rows.length !== comparisonMetricIDs.length
    || new Set(response.rows.map(({ id }) => id)).size !== comparisonMetricIDs.length) throw invalidComparisonResponse();
  const rows = response.rows.map((row): ReworkComparisonRow => {
    if (!comparisonMetricIDs.includes(row.id as ComparisonMetricID)) throw invalidComparisonResponse();
    const id = row.id as ComparisonMetricID;
    if (row.unit !== (id === "recurring_loops_per_100_validations" ? "per100" : "percent")) throw invalidComparisonResponse();
    const before = mapComparisonValue(row.baseline);
    const after = mapComparisonValue(row.current);
    const unit = row.unit as "percent" | "per100";
    if (row.availability === "unavailable" && (before.availability === "unavailable" || after.availability === "unavailable") && row.delta === undefined) {
      return { availability: "unavailable", id, unit, baseline: before, current: after };
    }
    if (row.availability !== "comparable" || before.availability !== "available" || after.availability !== "available" || row.delta === undefined || !Number.isFinite(row.delta)) throw invalidComparisonResponse();
    return { availability: "comparable", id, unit, baseline: before, current: after, delta: row.delta, direction: displayComparisonDirection(id, row.delta) };
  });
  const warnings = [["Baseline", baseline], ["Current", current]] as const;
  return { status: "ready", baseline, current, rows,
    harness: compareHarnessContexts(baseline.harness, current.harness),
    warnings: warnings.flatMap(([label, subject]) => subject.projectionCoverage === "complete" ? [] : [subject.projectionCoverage === "partial" ? `${label} evidence is a partial retained projection.` : `${label} projection coverage is unknown.`]),
  };
}

function mapComparisonSummary(value?: ComparisonSummaryMessage): ComparisonSubject {
  if (!value?.sourceId || !value.sessionId || !value.coverage
    || !["complete", "partial", "unknown"].includes(value.projectionCoverage)) throw invalidComparisonResponse();
  return { sourceId: value.sourceId, sessionId: value.sessionId, startedAt: timeValue(value.startedAt), endedAt: timeValue(value.endedAt),
    projectionCoverage: value.projectionCoverage as ComparisonSubject["projectionCoverage"], coverage: mapReworkCoverage(value.coverage), harness: mapHarnessContext(value.harnessContext),
  };
}

function mapComparisonValue(value?: ComparisonValueMessage): ComparisonValue {
  if (!value) throw invalidComparisonResponse();
  const numerator = value.numerator ?? null;
  const denominator = value.denominator ?? null;
  if ((numerator !== null && !Number.isFinite(numerator)) || (denominator !== null && !Number.isFinite(denominator))) throw invalidComparisonResponse();
  if (value.availability === "unavailable" && value.reason && value.value === undefined) return { availability: "unavailable", reason: value.reason, numerator, denominator };
  if (value.availability !== "available" || numerator === null || denominator === null || value.value === undefined || !Number.isFinite(value.value)) throw invalidComparisonResponse();
  return { availability: "available", numerator, denominator, displayValue: value.value };
}

function mapReworkCoverage(value: ReworkCoverageMessage): ReworkAnalysis["coverage"] {
  return {
    activityCoverage: value.activityCoverage, canonicalEvents: Number(value.canonicalEvents), classifiedEvents: Number(value.classifiedEvents), knownOutcomes: Number(value.knownOutcomes),
    validationAttempts: Number(value.validationAttempts), fingerprintedFailures: Number(value.fingerprintedFailures), identifiedValidationAttempts: Number(value.identifiedValidationAttempts),
    idBackedValidationAttempts: Number(value.idBackedValidationAttempts), mergedValidationAttempts: Number(value.mergedValidationAttempts),
    uncorrelatedValidationObservations: Number(value.uncorrelatedValidationObservations), conflictingAttemptObservations: Number(value.conflictingAttemptObservations), ambiguousFailureAttempts: Number(value.ambiguousFailureAttempts),
  };
}

export function assertSessionConditionsApplied(requested: SessionConditions, applied?: SessionConditions) {
  if (hasSessionConditions(requested) && (!applied || conditionsKey(requested) !== conditionsKey(applied))) {
    throw new Error("This server does not support all requested investigation conditions.");
  }
}
