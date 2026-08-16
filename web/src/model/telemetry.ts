export type TimeRange = "1h" | "24h" | "7d";
export type ActivityDirection = "newer" | "older";
export type TelemetrySource = Readonly<{ id: string; label: string }>;
export type TokenUsage = Readonly<{ input: number | null; output: number | null; cacheRead: number | null; cacheWrite: number | null; reasoning: number | null; total: number | null }>;
export type Activity = Readonly<{
  id?: string; source: string; signal: "trace" | "log" | "metric"; name: string; traceId?: string; spanId?: string; parentSpanId?: string;
  promptId?: string; usageId?: string; relatedTraceId?: string; relatedSpanId?: string;
  kind: "unknown" | "prompt" | "response" | "tool" | "delegation" | "message" | "reasoning";
  toolName?: string; targetAgentId?: string; targetAgentType?: string; content?: string; agentId: string;
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
export type ReworkAnalysis = Readonly<{
  sourceId: string;
  sessionId: string;
  metrics: Readonly<{
    validationFailures: number;
    failFixRetryCycles: number;
    reworkDurationMs: number;
    reworkTokens: TokenUsage;
    toolAttemptsWithOutcome: number;
    toolFailures: number;
    toolFailureRate: number | null;
    apiRetryWaste: Readonly<{ attempts: number; durationMs: number; tokens: TokenUsage }>;
    repeatedCommands: number;
    reeditedFiles: number;
  }>;
  coverage: Readonly<{
    activityCoverage: string;
    canonicalEvents: number;
    classifiedEvents: number;
    knownOutcomes: number;
  }>;
  capabilities: Readonly<{
    changeRevert: AnalysisCapability;
    crossAgentOverlap: AnalysisCapability;
  }>;
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
