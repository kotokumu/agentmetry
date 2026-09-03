export type TimeRange = "1h" | "24h" | "7d";
export type ActivityDirection = "newer" | "older";
export type TelemetrySource = Readonly<{ id: string; label: string }>;
export type TokenUsage = Readonly<{ input: number | null; output: number | null; cacheRead: number | null; cacheWrite: number | null; reasoning: number | null; total: number | null }>;
export type ContentEvidence = Readonly<{
  source: string; activityId: string; signal: string;
  kind: "prompt" | "response" | "tool_input" | "tool_output" | "tool_input_output" | "model_input" | "reference" | "unknown";
  evidence: "reference" | "read_output" | "explicit_model_input" | "unknown";
  availability: "available" | "not_reported" | "redacted" | "not_returned";
  fields: readonly string[]; truncated: boolean;
  redactionReason?: "producer_redacted" | "encrypted_input";
}>;
export type Activity = Readonly<{
  id?: string; source: string; signal: "trace" | "log" | "metric"; name: string; traceId?: string; spanId?: string; parentSpanId?: string;
  missingParent?: boolean;
  promptId?: string; usageId?: string; relatedTraceId?: string; relatedSpanId?: string;
  kind: "unknown" | "prompt" | "response" | "tool" | "delegation" | "message" | "reasoning";
  toolName?: string; targetAgentId?: string; targetAgentType?: string; content?: string; contentEvidence?: ContentEvidence; agentId: string;
  agentDefinition?: string; agentType?: string; parentAgentId?: string; runId: string; model: string;
  startedAt?: string; endedAt?: string; observedAt: string; status?: string; tokens: TokenUsage; costUsd?: number; contributesToTotal: boolean;
}>;
export type PlanUsageSnapshot = Readonly<{ source: string; accountId?: string; plan?: string; windowId: string; windowDurationMinutes?: number; usedPercent: number; resetsAt?: string; capturedAt: string; authority: string }>;
export type AgentSession = Readonly<{ agentId: string; agentDefinition?: string; agentType?: string; parentAgentId?: string; model?: string; activityCount: number; tokens: TokenUsage }>;
export type Session = Readonly<{
  id: string; sourceId: string; sources: readonly TelemetrySource[]; traceIds: readonly string[]; startedAt: string; endedAt: string;
  activityCount: number; agentCount?: number; tokens: TokenUsage; costUsd?: number; agents: readonly AgentSession[]; activities: readonly Activity[];
  activityOffset?: number; hasEarlier?: boolean; hasMore?: boolean; nextPageToken?: string; previousPageToken?: string;
}>;
export type AnalysisCapability = Readonly<{ state: string; reason: string }>;
export type RecurringFailureEpisode = Readonly<{
  agentId: string; operation: string; validationFingerprint: string; errorFingerprints: readonly string[];
  failureAttempts: number; resolved: boolean; resolutionDurationMs: number; resolutionTokens: TokenUsage;
  traceId: string; spanId: string;
}>;
export type HarnessEvidenceCounts = Readonly<{
  eligibleRecords: number;
  reportedRecords: number;
  unreportedRecords: number;
  invalidRecords: number;
  distinctIdentities: number;
}>;
export type HarnessIdentity = Readonly<{ scope: string; fingerprint: string; label?: string }>;
export type HarnessEvidenceState = "no_eligible_records" | "unreported" | "uniform" | "mixed" | "incomplete" | "invalid";
export type HarnessContext =
  | Readonly<{ availability: "available"; state: "uniform"; counts: HarnessEvidenceCounts; identity: HarnessIdentity }>
  | Readonly<{ availability: "available"; state: Exclude<HarnessEvidenceState, "uniform">; counts: HarnessEvidenceCounts }>
  | Readonly<{ availability: "unavailable"; reason: "server_unsupported" | "invalid_server_payload" }>;
export type ReworkAnalysis = Readonly<{
  sourceId: string;
  sessionId: string;
  sessionTokens?: TokenUsage;
  harness: HarnessContext;
  metrics: Readonly<{
    validationFailures: number;
    failFixRetryCycles: number;
    reworkDurationMs: number;
    totalAgentEffortMs: number;
    reworkAgentEffortRate: number | null;
    reworkTokens: TokenUsage;
    toolAttemptsWithOutcome: number;
    toolFailures: number;
    toolFailureRate: number | null;
    apiRetryWaste: Readonly<{ attempts: number; durationMs: number; tokens: TokenUsage }>;
    repeatedCommands: number;
    reeditedFiles: number;
    validationAttemptsWithOutcome: number;
    firstPassEligibleValidations: number;
    firstPassSuccesses: number;
    firstPassSuccessRate: number | null;
    recurringFailureLoops: number;
    repeatedFailureAttempts: number;
    resolvedFailureLoops: number;
    unresolvedFailureLoops: number;
    failureResolutionDurationMs: number;
    failureResolutionTokens: TokenUsage;
  }>;
  coverage: Readonly<{
    activityCoverage: string;
    canonicalEvents: number;
    classifiedEvents: number;
    knownOutcomes: number;
    validationAttempts: number;
    fingerprintedFailures: number;
    identifiedValidationAttempts: number;
    idBackedValidationAttempts: number;
    mergedValidationAttempts: number;
    uncorrelatedValidationObservations: number;
    conflictingAttemptObservations: number;
    ambiguousFailureAttempts: number;
  }>;
  capabilities: Readonly<{
    changeRevert: AnalysisCapability;
    crossAgentOverlap: AnalysisCapability;
  }>;
  failureEpisodes: readonly RecurringFailureEpisode[];
}>;
export type ConversationRef = Readonly<{ sourceId: string; id: string }>;
export type TraceAgent = Readonly<{ sourceId: string; conversationId: string; agentId: string; agentDefinition?: string; agentType?: string; parentAgentId?: string; model?: string }>;
export type Trace = Readonly<{
  traceId: string; startedAt: string; endedAt: string; status: string; rootSpanCount: number; missingParentCount: number;
  conversations: readonly ConversationRef[]; agents: readonly TraceAgent[]; activities: readonly Activity[];
  activityOffset: number; activityCount: number; hasMore: boolean; nextPageToken?: string; previousPageToken?: string;
}>;
export type DashboardSummary = Readonly<{
  sources: readonly TelemetrySource[]; signalCounts: Readonly<{ traces: number; logs: number; metrics: number }>;
  runCount: number; agentCount: number; tokens: TokenUsage; recentActivity: readonly Activity[]; planUsage: readonly PlanUsageSnapshot[];
}>;
