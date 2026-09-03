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

export type ComparisonReference = Readonly<{ sourceId: string; sessionId: string }>;
export type ReworkComparisonPair = Readonly<{ baseline: ComparisonReference; current: ComparisonReference }>;
export type ComparisonSubject = ComparisonReference & Readonly<{
  startedAt: string; endedAt: string; projectionCoverage: ProjectionCoverage;
  coverage: ReworkAnalysis["coverage"]; harness: HarnessContext;
}>;
export type SharedReworkComparison = ReworkComparisonReport & Readonly<{ baseline: ComparisonSubject; current: ComparisonSubject }>;

export type ComparisonBaselineOption = Readonly<{ sessionId: string; endedAt: string }>;
type ComparisonViewContext = Readonly<{ options: readonly ComparisonBaselineOption[]; selectedBaselineId: string }>;
export type ReworkComparisonViewState =
  | Readonly<{ status: "empty" }>
  | (ComparisonViewContext & Readonly<{ status: "loading" }>)
  | (ComparisonViewContext & Readonly<{ status: "failed"; message: string }>)
  | (ComparisonViewContext & Readonly<{ status: "waiting"; message: string }>)
  | (ComparisonViewContext & Readonly<{ status: "invalid"; code: ComparisonInvalidCode; reason: string }>)
  | (ComparisonViewContext & Readonly<{ status: "ready"; rows: readonly ReworkComparisonRow[]; warnings: readonly string[]; harness: HarnessRelationship }>);

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

const parseTime = (value: string): number | undefined => {
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : undefined;
};

const roundForDisplay = (value: number): number => {
  const multiplier = 10 ** COMPARISON_DISPLAY_DECIMALS;
  const rounded = Math.sign(value) * Math.round(Math.abs(value) * multiplier + Number.EPSILON) / multiplier;
  return rounded === 0 ? 0 : rounded;
};

export const displayComparisonDirection = (id: ComparisonMetricID, delta: number): ComparisonDirection => {
  const displayed = roundForDisplay(delta);
  return displayed === 0 ? "unchanged" : (displayed > 0) === (id === "initial_validation_success_proxy") ? "improved" : "regressed";
};
export const roundComparisonDisplay = roundForDisplay;
