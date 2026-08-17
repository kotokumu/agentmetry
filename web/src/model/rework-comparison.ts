import type { HarnessContext, ReworkAnalysis, Session } from "./telemetry";

export const COMPARISON_DISPLAY_DECIMALS = 1;

export type ComparisonDirection = "improved" | "regressed" | "unchanged";
export type ComparisonMetricID =
  | "initial_validation_success_proxy"
  | "rework_token_share"
  | "retry_cycle_effort_share"
  | "tool_failure_rate"
  | "recurring_loops_per_100_validations";
export type ComparisonUnit = "percent" | "per100";
export type ProjectionCoverage = "complete" | "partial" | "unknown";
export type ComparisonInvalidCode = "identity_mismatch" | "invalid_time" | "baseline_ineligible";
export type HarnessComparisonIssue = "server_unsupported" | "invalid_server_payload" | "no_eligible_records" | "unreported" | "mixed" | "incomplete" | "invalid";
type UniformHarnessContext = Extract<HarnessContext, Readonly<{ availability: "available"; state: "uniform" }>>;
export type HarnessRelationship =
  | Readonly<{ status: "reported_same" | "reported_changed"; baseline: UniformHarnessContext; current: UniformHarnessContext }>
  | Readonly<{ status: "not_comparable"; baseline: HarnessContext; current: HarnessContext; baselineIssue?: HarnessComparisonIssue; currentIssue?: HarnessComparisonIssue; relationshipIssue?: "scope_mismatch" }>;

export type AvailableComparisonValue = Readonly<{
  availability: "available";
  displayValue: number;
  numerator: number;
  denominator: number;
}>;

export type UnavailableComparisonValue = Readonly<{
  availability: "unavailable";
  reason: string;
  numerator: number | null;
  denominator: number | null;
}>;

export type ComparisonValue = AvailableComparisonValue | UnavailableComparisonValue;

export type ReworkDiagnosticSnapshot = Readonly<{
  sourceId: string;
  sessionId: string;
  startedAtMs: number;
  endedAtMs: number;
  projectionCoverage: ProjectionCoverage;
  sessionTokens: number | null;
  firstPassSuccesses: number;
  firstPassEligibleValidations: number;
  reworkTokens: number | null;
  reworkDurationMs: number;
  totalAgentEffortMs: number;
  toolFailures: number;
  toolAttemptsWithOutcome: number;
  recurringFailureLoops: number;
  validationAttemptsWithOutcome: number;
}>;

type ComparableRow = Readonly<{
  availability: "comparable";
  id: ComparisonMetricID;
  baseline: AvailableComparisonValue;
  current: AvailableComparisonValue;
  delta: number;
  direction: ComparisonDirection;
  unit: ComparisonUnit;
}>;

type UnavailableRow = Readonly<{
  availability: "unavailable";
  id: ComparisonMetricID;
  baseline: ComparisonValue;
  current: ComparisonValue;
  unit: ComparisonUnit;
}>;

export type ReworkComparisonRow = ComparableRow | UnavailableRow;

export type ReworkComparisonReport =
  | Readonly<{ status: "ready"; rows: readonly ReworkComparisonRow[]; warnings: readonly string[]; harness: HarnessRelationship }>
  | Readonly<{ status: "invalid"; code: ComparisonInvalidCode; reason: string }>;

export type ComparisonBaselineOption = Readonly<{ sessionId: string; endedAt: string }>;
type ComparisonViewContext = Readonly<{ options: readonly ComparisonBaselineOption[]; selectedBaselineId: string }>;
export type ReworkComparisonViewState =
  | Readonly<{ status: "empty" }>
  | (ComparisonViewContext & Readonly<{ status: "loading" }>)
  | (ComparisonViewContext & Readonly<{ status: "failed"; message: string }>)
  | (ComparisonViewContext & Readonly<{ status: "waiting"; message: string }>)
  | (ComparisonViewContext & Readonly<{ status: "invalid"; code: ComparisonInvalidCode; reason: string }>)
  | (ComparisonViewContext & Readonly<{ status: "ready"; rows: readonly ReworkComparisonRow[]; warnings: readonly string[]; harness: HarnessRelationship }>);

type MetricDefinition = Readonly<{
  id: ComparisonMetricID;
  unit: ComparisonUnit;
  preferredDirection: "higher" | "lower";
  value: (snapshot: ReworkDiagnosticSnapshot) => ComparisonValue;
}>;

export const eligibleComparisonBaselines = (current: Session, sessions: readonly Session[]): readonly Session[] => {
  const currentStartedAt = parseTime(current.startedAt);
  if (currentStartedAt === undefined) return [];
  return sessions
    .filter((candidate) => {
      if (candidate.sourceId !== current.sourceId || candidate.id === current.id) return false;
      const startedAt = parseTime(candidate.startedAt);
      const endedAt = parseTime(candidate.endedAt);
      return startedAt !== undefined && endedAt !== undefined && endedAt >= startedAt && endedAt <= currentStartedAt;
    })
    .sort((left, right) => {
      const ended = Date.parse(right.endedAt) - Date.parse(left.endedAt);
      if (ended !== 0) return ended;
      return left.id < right.id ? -1 : left.id > right.id ? 1 : 0;
    });
};

export const createReworkDiagnosticSnapshot = (
  session: Session,
  analysis: ReworkAnalysis,
): ReworkDiagnosticSnapshot | undefined => {
  if (session.sourceId !== analysis.sourceId || session.id !== analysis.sessionId) return undefined;
  const startedAtMs = parseTime(session.startedAt);
  const endedAtMs = parseTime(session.endedAt);
  if (startedAtMs === undefined || endedAtMs === undefined || endedAtMs < startedAtMs) return undefined;
  const { metrics } = analysis;
  const sessionTokenTotal = analysis.sessionTokens === undefined
    ? analysis.harness.availability === "unavailable" && analysis.harness.reason === "server_unsupported"
      ? session.tokens.total
      : null
    : analysis.sessionTokens.total;
  return {
    sourceId: session.sourceId,
    sessionId: session.id,
    startedAtMs,
    endedAtMs,
    projectionCoverage: projectionCoverage(analysis.coverage.activityCoverage),
    sessionTokens: finiteOrNull(sessionTokenTotal),
    firstPassSuccesses: metrics.firstPassSuccesses,
    firstPassEligibleValidations: metrics.firstPassEligibleValidations,
    reworkTokens: finiteOrNull(metrics.reworkTokens.total),
    reworkDurationMs: metrics.reworkDurationMs,
    totalAgentEffortMs: metrics.totalAgentEffortMs,
    toolFailures: metrics.toolFailures,
    toolAttemptsWithOutcome: metrics.toolAttemptsWithOutcome,
    recurringFailureLoops: metrics.recurringFailureLoops,
    validationAttemptsWithOutcome: metrics.validationAttemptsWithOutcome,
  };
};

export const buildReworkComparisonReport = (
  baselineSession: Session,
  baselineAnalysis: ReworkAnalysis,
  currentSession: Session,
  currentAnalysis: ReworkAnalysis,
): ReworkComparisonReport => {
  if (baselineSession.sourceId !== baselineAnalysis.sourceId || baselineSession.id !== baselineAnalysis.sessionId
    || currentSession.sourceId !== currentAnalysis.sourceId || currentSession.id !== currentAnalysis.sessionId) {
    return { status: "invalid", code: "identity_mismatch", reason: "The loaded analysis does not belong to its displayed conversation." };
  }
  if (!validSessionTime(baselineSession) || !validSessionTime(currentSession)) {
    return { status: "invalid", code: "invalid_time", reason: "A conversation has an invalid start or end time." };
  }
  const baseline = createReworkDiagnosticSnapshot(baselineSession, baselineAnalysis);
  const current = createReworkDiagnosticSnapshot(currentSession, currentAnalysis);
  if (!baseline || !current) return { status: "invalid", code: "invalid_time", reason: "A conversation has an invalid start or end time." };
  const rows = compareReworkSnapshots(baseline, current);
  if (!rows) return { status: "invalid", code: "baseline_ineligible", reason: "The baseline is no longer eligible for the selected conversation." };
  const warnings = [
    coverageWarning("Baseline", baseline.projectionCoverage),
    coverageWarning("Current", current.projectionCoverage),
  ].filter((value): value is string => Boolean(value));
  return { status: "ready", rows, warnings, harness: compareHarnessContexts(baselineAnalysis.harness, currentAnalysis.harness) };
};

export const compareHarnessContexts = (baseline: HarnessContext, current: HarnessContext): HarnessRelationship => {
  const baselineIssue = harnessIssue(baseline);
  const currentIssue = harnessIssue(current);
  if (baselineIssue || currentIssue) {
    return {
      status: "not_comparable",
      baseline,
      current,
      ...(baselineIssue ? { baselineIssue } : {}),
      ...(currentIssue ? { currentIssue } : {}),
    };
  }
  if (baseline.availability !== "available" || baseline.state !== "uniform"
    || current.availability !== "available" || current.state !== "uniform") {
    return { status: "not_comparable", baseline, current, baselineIssue: "invalid_server_payload", currentIssue: "invalid_server_payload" };
  }
  if (baseline.identity.scope !== current.identity.scope) {
    return { status: "not_comparable", baseline, current, relationshipIssue: "scope_mismatch" };
  }
  return {
    status: baseline.identity.fingerprint === current.identity.fingerprint ? "reported_same" : "reported_changed",
    baseline,
    current,
  };
};

const harnessIssue = (context: HarnessContext): HarnessComparisonIssue | undefined => context.availability === "unavailable"
  ? context.reason
  : context.state === "uniform" ? undefined : context.state;

export const compareReworkSnapshots = (
  baseline: ReworkDiagnosticSnapshot,
  current: ReworkDiagnosticSnapshot,
): readonly ReworkComparisonRow[] | undefined => {
  if (baseline.sourceId !== current.sourceId || baseline.sessionId === current.sessionId || baseline.endedAtMs > current.startedAtMs) return undefined;
  return metricDefinitions.map((definition): ReworkComparisonRow => {
    const baselineValue = definition.value(baseline);
    const currentValue = definition.value(current);
    if (baselineValue.availability === "unavailable" || currentValue.availability === "unavailable") {
      return { availability: "unavailable", id: definition.id, baseline: baselineValue, current: currentValue, unit: definition.unit };
    }
    const delta = roundForDisplay(currentValue.displayValue - baselineValue.displayValue);
    const direction: ComparisonDirection = delta === 0
      ? "unchanged"
      : (delta > 0) === (definition.preferredDirection === "higher") ? "improved" : "regressed";
    return { availability: "comparable", id: definition.id, baseline: baselineValue, current: currentValue, delta, direction, unit: definition.unit };
  });
};

const metricDefinitions: readonly MetricDefinition[] = [
  {
    id: "initial_validation_success_proxy",
    unit: "percent",
    preferredDirection: "higher",
    value: (snapshot) => ratioValue(snapshot.firstPassSuccesses, snapshot.firstPassEligibleValidations, "No eligible validation identities", "Inconsistent initial validation evidence"),
  },
  {
    id: "rework_token_share",
    unit: "percent",
    preferredDirection: "lower",
    value: (snapshot) => {
      if (snapshot.sessionTokens === null || snapshot.sessionTokens <= 0) return unavailable("Session token total unavailable");
      if (snapshot.reworkTokens === null) return unavailable("Rework token usage unavailable", null, snapshot.sessionTokens);
      return ratioValue(snapshot.reworkTokens, snapshot.sessionTokens, "Session token total unavailable", "Inconsistent rework token evidence");
    },
  },
  {
    id: "retry_cycle_effort_share",
    unit: "percent",
    preferredDirection: "lower",
    value: (snapshot) => ratioValue(snapshot.reworkDurationMs, snapshot.totalAgentEffortMs, "Observed agent-active duration unavailable", "Inconsistent retry-cycle effort evidence"),
  },
  {
    id: "tool_failure_rate",
    unit: "percent",
    preferredDirection: "lower",
    value: (snapshot) => ratioValue(snapshot.toolFailures, snapshot.toolAttemptsWithOutcome, "No tool outcomes observed", "Inconsistent tool outcome evidence"),
  },
  {
    id: "recurring_loops_per_100_validations",
    unit: "per100",
    preferredDirection: "lower",
    value: (snapshot) => ratioValue(snapshot.recurringFailureLoops, snapshot.validationAttemptsWithOutcome, "No validation outcomes observed", "Inconsistent recurring-loop evidence", 100),
  },
];

const ratioValue = (
  numerator: number,
  denominator: number,
  missingReason: string,
  inconsistentReason: string,
  scale = 100,
): ComparisonValue => {
  if (!Number.isFinite(denominator) || denominator <= 0) return unavailable(missingReason, finiteOrNull(numerator), finiteOrNull(denominator));
  if (!Number.isFinite(numerator) || numerator < 0 || numerator > denominator) return unavailable(inconsistentReason, finiteOrNull(numerator), denominator);
  return { availability: "available", displayValue: (numerator / denominator) * scale, numerator, denominator };
};

const unavailable = (reason: string, numerator: number | null = null, denominator: number | null = null): UnavailableComparisonValue => ({
  availability: "unavailable",
  numerator,
  denominator,
  reason,
});

const projectionCoverage = (value: string): ProjectionCoverage => value === "observed_projection_complete"
  ? "complete"
  : value ? "partial" : "unknown";

const coverageWarning = (side: "Baseline" | "Current", coverage: ProjectionCoverage) => coverage === "complete"
  ? ""
  : coverage === "partial"
    ? `${side} evidence is a partial retained projection.`
    : `${side} projection coverage is unknown.`;

const parseTime = (value: string): number | undefined => {
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : undefined;
};

const validSessionTime = (session: Session): boolean => {
  const startedAt = parseTime(session.startedAt);
  const endedAt = parseTime(session.endedAt);
  return startedAt !== undefined && endedAt !== undefined && endedAt >= startedAt;
};

const finiteOrNull = (value: number | null): number | null => value !== null && Number.isFinite(value) ? value : null;

const roundForDisplay = (value: number): number => {
  const multiplier = 10 ** COMPARISON_DISPLAY_DECIMALS;
  const rounded = Math.sign(value) * Math.round(Math.abs(value) * multiplier + Number.EPSILON) / multiplier;
  return rounded === 0 ? 0 : rounded;
};
