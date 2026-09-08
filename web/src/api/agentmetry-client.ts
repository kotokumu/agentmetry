import { conditionsKey, hasSessionConditions, sessionConditions, type SessionConditions } from "../model/investigation-conditions";
import type { SessionCatalog, SessionName, SessionListPage, SessionListQuery, SessionListView as UiSessionListView } from "../model/session-catalog";
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { timestampDate, timestampFromDate, type Timestamp } from "@bufbuild/protobuf/wkt";
import {
  AgentmetryQueryService,
  ActivityMutationOperation,
  PageDirection,
  ProjectionTargetKind,
  TraceFailureObservation,
  TimeRange,
  SessionListView,
  SessionRole,
  CallIdentityBasis,
  CallCostBasis,
  CostSummaryBasis,
  CostCoverage,
  ModelCallEvidenceRole,
  CostUnavailableReason,
  CostAggregationError,
  type ListSessionsResponse,
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
  type CostSummary as CostSummaryMessage,
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
import type { CostBasis, CostSummary, ModelCallCost, ModelCallRef } from "../model/cost";
import type { SessionFileReadPage, TraceCatalogPage, TraceCatalogEntry, SessionFileRead, TraceCatalogConditions } from "../model/trace-catalog";
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
    return (await this.listSessionsPage({ range, sourceId, search, conditions, view: "roots" }, signal)).sessions;
  },

  async listSessionsPage(query: SessionListQuery & Readonly<{ pageToken?: string }>, signal?: AbortSignal): Promise<SessionListPage> {
    const response = await client.listSessions(
      { filter: { range: toTimeRange(query.range), sourceId: query.sourceId, search: query.search },
        page: { pageSize: query.pageSize ?? 100, pageToken: query.pageToken },
        view: query.view === "all" ? SessionListView.ALL : SessionListView.ROOTS,
        conditions: hasSessionConditions(query.conditions) ? sessionConditions(query.conditions) : undefined },
      signal ? { signal } : undefined,
    );
    assertSessionConditionsApplied(query.conditions, response.appliedConditions);
    return mapSessionListResponse(response, query.view);
  },

  async listTraces(range: UiTimeRange, sourceId: string, pageToken = "", signal?: AbortSignal, conditions: TraceCatalogConditions = {}): Promise<TraceCatalogPage> {
    const response = await client.listTraces({
      filter: { range: toTimeRange(range), sourceId },
      page: { pageSize: 50, pageToken },
      conditions: hasTraceCatalogConditions(conditions) ? mapTraceCatalogConditions(conditions) : undefined,
    }, signal ? { signal } : undefined);
    if (response.page?.hasMore && !response.page.nextPageToken) throw new Error("Trace list unavailable");
    const appliedConditions = mapTraceCatalogConditionsResponse(response.appliedConditions);
    if (!sameTraceCatalogConditions(conditions, appliedConditions)) throw new Error("Trace conditions were not applied");
    return {
      traces: response.traces.map(mapTraceCatalogEntry),
      nextPageToken: response.page?.hasMore ? response.page.nextPageToken : undefined,
      hasMore: response.page?.hasMore ?? false,
      appliedConditions,
    };
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

  async listSessionFileReads(sourceId: string, sessionId: string, pageToken = "", reference = "", signal?: AbortSignal): Promise<SessionFileReadPage> {
    const response = await client.listSessionFileReads({
      sourceId, sessionId, reference, page: { pageSize: 50, pageToken },
    }, signal ? { signal } : undefined);
    const coverage = response.coverage === "complete" || response.coverage === "partial" || response.coverage === "unavailable"
      ? response.coverage : "unavailable";
    if (response.page?.hasMore && !response.page.nextPageToken) throw new Error("Session file reads unavailable");
    return {
      reads: response.reads.map((value) => mapSessionFileRead(value, coverage)),
      distinctReferenceCount: Number(response.distinctReferenceCount),
      nextPageToken: response.page?.hasMore ? response.page.nextPageToken : undefined,
      hasMore: response.page?.hasMore ?? false,
      coverage,
    };
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
    costSummary: mapCostSummary(value.costSummary),
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

export function mapSessionListResponse(response: ListSessionsResponse, view: UiSessionListView): SessionListPage {
  const expected = view === "all" ? SessionListView.ALL : SessionListView.ROOTS;
  if (response.appliedView !== expected && !(view === "roots" && response.appliedView === SessionListView.UNSPECIFIED)) {
    throw new Error("Session list unavailable");
  }
  if (response.page?.hasMore && !response.page.nextPageToken) throw new Error("Session list unavailable");
  return {
    sessions: response.sessions.map((value) => {
      const catalog = mapSessionCatalog(value, view);
      if (view === "all" && !catalog) throw new Error("Session list unavailable");
      return { ...mapSession(value), ...(catalog ? { catalog } : {}) };
    }),
    nextPageToken: response.page?.hasMore ? response.page.nextPageToken : "",
  };
}

function mapSessionCatalog(value: SessionSummary, view: UiSessionListView): SessionCatalog | undefined {
  const catalog = value.catalog;
  if (!value.id || !value.sourceId || !catalog?.rootSessionId) return undefined;
  const name = mapSessionName(value);
  if (catalog.role === SessionRole.ROOT && catalog.rootSessionId === value.id && !catalog.parentSessionId) {
    return { role: "root", rootSessionId: catalog.rootSessionId, parentSessionId: "", ...(name ? { name } : {}) };
  }
  if (view === "all" && catalog.role === SessionRole.CHILD && catalog.rootSessionId !== value.id
    && catalog.parentSessionId && catalog.parentSessionId !== value.id) {
    return { role: "child", rootSessionId: catalog.rootSessionId, parentSessionId: catalog.parentSessionId, ...(name ? { name } : {}) };
  }
  return undefined;
}

function mapSessionName(value: SessionSummary): SessionName | undefined {
  const name = value.catalog?.name;
  if (!name || !name.text.trim()) return undefined;
  if (!((value.sourceId === "claude" && name.origin === "claude_code.generate_session_title")
    || (value.sourceId === "codex" && name.origin === "codex_app.list_threads"))) return undefined;
  const time = name.observedAt;
  if (time && (time.seconds < -62135596800n || time.seconds > 253402300799n
    || !Number.isInteger(time.nanos) || time.nanos < 0 || time.nanos > 999999999)) return undefined;
  return { text: name.text, origin: name.origin, ...(time ? { observedAt: timestampDate(time).toISOString() } : {}) };
}

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
    costSummary: mapCostSummary(value.costSummary),
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
    ...mapModelCallRelation(value),
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
    costSummary: mapCostSummary(response.costSummary),
  };
};

function mapTraceCatalogEntry(value: { traceId: string; startedAt?: Timestamp; endedAt?: Timestamp; durationMs?: number; status: string; activityCount: bigint; rootSpanCount: bigint; missingParentCount: bigint; conversations: readonly { sourceId: string; id: string }[]; costSummary?: CostSummaryMessage }): TraceCatalogEntry {
  return {
    traceId: value.traceId, startedAt: timeValue(value.startedAt), endedAt: timeValue(value.endedAt),
    durationMs: value.durationMs, status: value.status, activityCount: Number(value.activityCount),
    rootSpanCount: Number(value.rootSpanCount), missingParentCount: Number(value.missingParentCount),
    conversations: value.conversations.map((conversation) => ({ sourceId: conversation.sourceId, id: conversation.id })),
    costSummary: mapCostSummary(value.costSummary),
  };
}

function mapSessionFileRead(value: { id: string; sourceId: string; sessionId: string; reference: string; activityId: string; observedAt?: Timestamp; agentId: string; model: string; outputContent: string; outputAvailability: string; outputMapping: string; contentEvidence?: { source: string; activityId: string; signal: string; kind: string; evidence: string; availability: string; fields: readonly string[]; truncated: boolean; redactionReason: string } }, coverage: SessionFileRead["coverage"]): SessionFileRead {
  const outputAvailability = ["available", "not_reported", "redacted", "not_returned", "not_confirmed"].includes(value.outputAvailability)
    ? value.outputAvailability as SessionFileRead["outputAvailability"] : "not_confirmed";
  const outputMapping = value.outputMapping === "confirmed" ? "confirmed" : "not_confirmed";
  return {
    id: value.id, sourceId: value.sourceId, sessionId: value.sessionId, reference: value.reference,
    activityId: value.activityId, observedAt: timeValue(value.observedAt), agentId: value.agentId,
    model: value.model, outputContent: value.outputContent || undefined, outputAvailability, outputMapping, coverage,
    contentEvidence: mapFileReadEvidence(value.contentEvidence, value),
  };
}

function mapFileReadEvidence(value: { source: string; activityId: string; signal: string; kind: string; evidence: string; availability: string; fields: readonly string[]; truncated: boolean; redactionReason: string } | undefined, read: { sourceId: string; activityId: string }): ContentEvidence {
  const fallback: ContentEvidence = { source: read.sourceId, activityId: read.activityId, signal: "log", kind: "reference", evidence: "reference", availability: "not_reported", fields: [], truncated: false };
  if (!value || value.source !== read.sourceId || value.activityId !== read.activityId) return fallback;
  const kinds = ["prompt", "response", "tool_input", "tool_output", "tool_input_output", "model_input", "reference", "unknown"];
  const evidence = ["reference", "read_output", "explicit_model_input", "unknown"];
  const availability = ["available", "not_reported", "redacted", "not_returned"];
  if (!kinds.includes(value.kind) || !evidence.includes(value.evidence) || !availability.includes(value.availability)) return fallback;
  const fields = value.fields.filter((field) => ["prompt", "response", "tool_input", "tool_parameters", "full_command", "file_path", "file_paths", "error", "body", "body_ref", "arguments.message", "output"].includes(field));
  const redactionReason = ["producer_redacted", "encrypted_input"].includes(value.redactionReason) ? value.redactionReason as ContentEvidence["redactionReason"] : undefined;
  return { source: value.source, activityId: value.activityId, signal: value.signal, kind: value.kind as ContentEvidence["kind"], evidence: value.evidence as ContentEvidence["evidence"], availability: value.availability as ContentEvidence["availability"], fields, truncated: value.truncated, ...(redactionReason ? { redactionReason } : {}) };
}

const hasTraceCatalogConditions = (conditions: TraceCatalogConditions) => conditions.failureObservation !== undefined || conditions.minDurationMs !== undefined;
const mapTraceCatalogConditions = (conditions: TraceCatalogConditions) => ({
  failureObservation: conditions.failureObservation === "observed" ? TraceFailureObservation.OBSERVED : conditions.failureObservation === "not_observed" ? TraceFailureObservation.NOT_OBSERVED : conditions.failureObservation === "not_reported" ? TraceFailureObservation.NOT_REPORTED : TraceFailureObservation.UNSPECIFIED,
  minDurationMs: conditions.minDurationMs,
});
const mapTraceCatalogConditionsResponse = (value: { failureObservation?: TraceFailureObservation; minDurationMs?: number } | undefined): TraceCatalogConditions => ({
  ...(value?.failureObservation === TraceFailureObservation.OBSERVED ? { failureObservation: "observed" as const } : value?.failureObservation === TraceFailureObservation.NOT_OBSERVED ? { failureObservation: "not_observed" as const } : value?.failureObservation === TraceFailureObservation.NOT_REPORTED ? { failureObservation: "not_reported" as const } : {}),
  ...(value?.minDurationMs !== undefined ? { minDurationMs: value.minDurationMs } : {}),
});
const sameTraceCatalogConditions = (requested: TraceCatalogConditions, applied: TraceCatalogConditions) => requested.failureObservation === applied.failureObservation && requested.minDurationMs === applied.minDurationMs;

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
  costSummary: mapCostSummary(response.costSummary),
});

function mapCostSummary(value?: CostSummaryMessage): CostSummary {
  const basis: CostBasis = value?.basis === CostSummaryBasis.PROVIDER_REPORTED ? "provider_reported"
    : value?.basis === CostSummaryBasis.RATE_CARD_ESTIMATE ? "rate_card_estimate"
      : value?.basis === CostSummaryBasis.MIXED ? "mixed"
        : value?.basis === CostSummaryBasis.UNAVAILABLE ? "unavailable" : "unknown";
  const coverage = value?.coverage === CostCoverage.COMPLETE ? "complete"
    : value?.coverage === CostCoverage.PARTIAL ? "partial"
      : value?.coverage === CostCoverage.UNAVAILABLE ? "unavailable" : "unknown";
  const aggregateError = value?.aggregateError === undefined ? undefined
    : value.aggregateError === CostAggregationError.CALCULATION_ERROR ? "calculation_error" : "unspecified";
  const amountVisible = value?.amountMicroUsd !== undefined && (coverage === "complete" || coverage === "partial")
    && basis !== "unknown" && basis !== "unavailable" && aggregateError === undefined;
  const amountMicroUsd = value?.amountMicroUsd;
  return {
    amountMicroUsd: amountVisible && amountMicroUsd !== undefined ? amountMicroUsd : null,
    basis, coverage,
    eligibleCalls: value?.eligibleCalls ?? 0n,
    pricedCalls: value?.pricedCalls ?? 0n,
    unpricedReasons: value?.unpricedReasons.map((reason) => ({ reason: unavailableReason(reason.reason), count: reason.count })) ?? [],
    ...(aggregateError ? { aggregateError } : {}),
  };
}

function mapModelCallRelation(value: ActivityMessage): { modelCallCost?: ModelCallCost; modelCallRef?: ModelCallRef } {
  if (value.modelCallRelation.case === "modelCallCost") {
    const call = value.modelCallRelation.value;
    const basis: CostBasis = call.basis === CallCostBasis.PROVIDER_REPORTED ? "provider_reported"
      : call.basis === CallCostBasis.RATE_CARD_ESTIMATE ? "rate_card_estimate"
        : call.basis === CallCostBasis.UNAVAILABLE ? "unavailable" : "unknown";
    return { modelCallCost: {
      callId: call.callId, identityBasis: identityBasis(call.identityBasis), basis,
      amountMicroUsd: basis === "unknown" || basis === "unavailable" ? null : call.amountMicroUsd ?? null,
      ...(call.primaryReason !== undefined ? { primaryReason: unavailableReason(call.primaryReason) } : {}),
      ...(call.rateEntryId ? { rateEntryId: call.rateEntryId } : {}),
    } };
  }
  if (value.modelCallRelation.case === "modelCallRef") {
    const ref = value.modelCallRelation.value;
    return { modelCallRef: { callId: ref.callId, identityBasis: identityBasis(ref.identityBasis), evidenceRole: ref.evidenceRole === ModelCallEvidenceRole.DUPLICATE_AUTHORITATIVE ? "duplicate_authoritative" : ref.evidenceRole === ModelCallEvidenceRole.CORROBORATING ? "corroborating" : "unknown" } };
  }
  return {};
}

const identityBasis = (value: CallIdentityBasis) => value === CallIdentityBasis.CLAUDE_CLIENT_REQUEST_ID ? "claude_client_request_id"
  : value === CallIdentityBasis.CLAUDE_REQUEST_ID ? "claude_request_id"
    : value === CallIdentityBasis.CLAUDE_EVENT_SEQUENCE ? "claude_event_sequence"
      : value === CallIdentityBasis.JOURNAL_EVIDENCE_FALLBACK ? "journal_evidence_fallback" : "unknown";

const unavailableReason = (value: CostUnavailableReason) => ({
  [CostUnavailableReason.CONFLICTING_AUTHORITATIVE_EVIDENCE]: "conflicting_authoritative_evidence",
  [CostUnavailableReason.INVALID_PROVIDER_AMOUNT]: "invalid_provider_amount",
  [CostUnavailableReason.MISSING_MODEL]: "missing_model",
  [CostUnavailableReason.MISSING_OCCURRED_AT]: "missing_occurred_at",
  [CostUnavailableReason.MISSING_TOKEN_USAGE]: "missing_token_usage",
  [CostUnavailableReason.UNSUPPORTED_BILLING_MODE]: "unsupported_billing_mode",
  [CostUnavailableReason.UNSUPPORTED_USAGE_CONDITION]: "unsupported_usage_condition",
  [CostUnavailableReason.RATE_NOT_FOUND]: "rate_not_found",
  [CostUnavailableReason.RATE_CONFLICT]: "rate_conflict",
  [CostUnavailableReason.CALCULATION_ERROR]: "calculation_error",
} as Record<number, string>)[value] ?? "unspecified";

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
