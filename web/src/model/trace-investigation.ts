import type { Activity, Trace } from "./telemetry";

export type TraceInvestigationState = Readonly<{
  startedAt?: string;
  endedAt?: string;
  kind?: Activity["kind"];
  errorsOnly?: boolean;
  selectedSpanId?: string;
}>;

export type TraceInvestigationWindow = Readonly<{
  startedAt?: string;
  endedAt?: string;
  kind: "" | Activity["kind"];
  errorsOnly: boolean;
}>;

export type TraceOverviewActivity = Readonly<{
  id: string;
  source: string;
  signal: Activity["signal"];
  spanId?: string;
  parentSpanId?: string;
  name: string;
  kind: Activity["kind"];
  status?: string;
  startedAt: string;
  endedAt: string;
  missingParent: boolean;
}>;

export type TraceOverview = Readonly<{
  traceId: string;
  startedAt: string;
  endedAt: string;
  totalActivities: number;
  returnedActivities: number;
  coverage: string;
  activities: readonly TraceOverviewActivity[];
}>;

export type TraceWindowResult = Readonly<{
  trace: Trace;
  matchingActivities: number;
}>;

const kinds = new Set<Activity["kind"]>([
  "unknown", "prompt", "response", "tool", "delegation", "message", "reasoning",
]);
const fields = new Set(["startedAt", "endedAt", "kind", "errorsOnly", "selectedSpanId"]);

export const parseTraceInvestigationState = (value: unknown): TraceInvestigationState | undefined => {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const record = value as Record<string, unknown>;
  if (Object.keys(record).some((field) => !fields.has(field))) return undefined;
  const startedAt = optionalNonemptyString(record.startedAt);
  const endedAt = optionalNonemptyString(record.endedAt);
  if (startedAt === null || endedAt === null || Boolean(startedAt) !== Boolean(endedAt)) return undefined;
  if (startedAt && endedAt) {
    const start = Date.parse(startedAt);
    const end = Date.parse(endedAt);
    if (!Number.isFinite(start) || !Number.isFinite(end) || start > end) return undefined;
  }
  if (record.kind !== undefined && (typeof record.kind !== "string" || !kinds.has(record.kind as Activity["kind"]))) return undefined;
  if (record.errorsOnly !== undefined && typeof record.errorsOnly !== "boolean") return undefined;
  const selectedSpanId = optionalNonemptyString(record.selectedSpanId);
  if (selectedSpanId === null) return undefined;
  return {
    ...(startedAt && endedAt ? { startedAt, endedAt } : {}),
    ...(record.kind !== undefined ? { kind: record.kind as Activity["kind"] } : {}),
    ...(record.errorsOnly !== undefined ? { errorsOnly: record.errorsOnly } : {}),
    ...(selectedSpanId ? { selectedSpanId } : {}),
  };
};

export const traceWindowForState = (state: TraceInvestigationState): TraceInvestigationWindow => ({
  ...(state.startedAt && state.endedAt ? { startedAt: state.startedAt, endedAt: state.endedAt } : {}),
  kind: state.kind ?? "",
  errorsOnly: state.errorsOnly ?? false,
});

const optionalNonemptyString = (value: unknown): string | undefined | null => {
  if (value === undefined) return undefined;
  return typeof value === "string" && value.length > 0 ? value : null;
};
