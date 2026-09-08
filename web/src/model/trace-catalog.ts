import type { ContentEvidence } from "./telemetry";

export type TraceCatalogFailure = "observed" | "not_observed" | "not_reported";
export type TraceCatalogConditions = Readonly<{
  failureObservation?: TraceCatalogFailure;
  minDurationMs?: number;
}>;

export type TraceCatalogEntry = Readonly<{
  traceId: string;
  startedAt: string;
  endedAt: string;
  durationMs?: number;
  status: string;
  activityCount: number;
  rootSpanCount: number;
  missingParentCount: number;
  conversations: readonly { sourceId: string; id: string }[];
}>;

export type TraceCatalogPage = Readonly<{
  traces: readonly TraceCatalogEntry[];
  nextPageToken?: string;
  hasMore: boolean;
  appliedConditions: TraceCatalogConditions;
}>;

export type SessionFileRead = Readonly<{
  id: string;
  sourceId: string;
  sessionId: string;
  reference: string;
  activityId: string;
  observedAt: string;
  agentId: string;
  model: string;
  outputContent?: string;
  outputAvailability: "available" | "not_reported" | "redacted" | "not_returned" | "not_confirmed";
  outputMapping: "confirmed" | "not_confirmed";
  coverage: "complete" | "partial" | "unavailable";
  contentEvidence: ContentEvidence;
}>;

export type SessionFileReadPage = Readonly<{
  reads: readonly SessionFileRead[];
  distinctReferenceCount: number;
  nextPageToken?: string;
  hasMore: boolean;
  coverage: "complete" | "partial" | "unavailable";
}>;
