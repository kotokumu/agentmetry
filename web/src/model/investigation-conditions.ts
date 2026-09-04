import type { TimeRange } from "./telemetry";

export type SessionConditions = Readonly<{ observedFailure?: boolean; minDurationMs?: number; maxDurationMs?: number; model?: string; tool?: string }>;
export type InvestigationFilters = Readonly<{ range: TimeRange; sourceId: string; search: string }> & SessionConditions;
const fields = ["range", "sourceId", "search", "observedFailure", "minDurationMs", "maxDurationMs", "model", "tool"];

export function parseInvestigationFilters(input: unknown): InvestigationFilters {
  if (!input || typeof input !== "object" || Array.isArray(input)) throw new Error("Invalid investigation conditions.");
  const value = input as Record<string, unknown>;
  for (const field of Object.keys(value)) if (!fields.includes(field)) throw new Error(`Unsupported condition: ${field}`);
  if (!["1h", "24h", "7d"].includes(String(value.range))) throw new Error("Unsupported relative time range.");
  for (const field of ["sourceId", "search", "model", "tool"]) {
    if ((field === "sourceId" || field === "search" || value[field] !== undefined)
      && (typeof value[field] !== "string" || (value[field] as string).length > 1024)) throw new Error(`Invalid ${field} condition.`);
  }
  for (const field of ["model", "tool"]) if (typeof value[field] === "string" && new TextEncoder().encode(value[field]).length > 200) throw new Error(`${field} must not exceed 200 bytes.`);
  if (value.observedFailure !== undefined && typeof value.observedFailure !== "boolean") throw new Error("Invalid failure condition.");
  for (const field of ["minDurationMs", "maxDurationMs"]) {
    if (value[field] !== undefined && (typeof value[field] !== "number" || !Number.isFinite(value[field]) || (value[field] as number) < 0)) throw new Error("Duration must be a finite nonnegative number of milliseconds.");
  }
  if (typeof value.minDurationMs === "number" && typeof value.maxDurationMs === "number" && value.minDurationMs > value.maxDurationMs) throw new Error("Minimum duration must not exceed maximum duration.");
  return { range: value.range as TimeRange, sourceId: value.sourceId as string, search: value.search as string, ...sessionConditions(value as SessionConditions) };
}

export function sessionConditions(value: SessionConditions): SessionConditions {
  return {
    ...(value.observedFailure ? { observedFailure: true } : {}),
    ...(value.minDurationMs !== undefined ? { minDurationMs: value.minDurationMs } : {}),
    ...(value.maxDurationMs !== undefined ? { maxDurationMs: value.maxDurationMs } : {}),
    ...(value.model ? { model: value.model } : {}),
    ...(value.tool ? { tool: value.tool } : {}),
  };
}
export const conditionsKey = (value: SessionConditions) => JSON.stringify(sessionConditions(value));
export const investigationFiltersKey = (value: InvestigationFilters) => `${value.range}\u0000${value.sourceId}\u0000${value.search}\u0000${conditionsKey(value)}`;
export const hasSessionConditions = (value: SessionConditions) => conditionsKey(value) !== "{}";

export function conditionParameters(value: InvestigationFilters): URLSearchParams {
  const query = new URLSearchParams();
  if (value.range !== "24h") query.set("range", value.range);
  if (value.sourceId) query.set("source", value.sourceId);
  if (value.search) query.set("q", value.search);
  if (value.observedFailure) query.set("failure", "true");
  if (value.minDurationMs !== undefined) query.set("minMs", String(value.minDurationMs));
  if (value.maxDurationMs !== undefined) query.set("maxMs", String(value.maxDurationMs));
  if (value.model) query.set("model", value.model);
  if (value.tool) query.set("tool", value.tool);
  return query;
}

export function filtersFromParameters(query: URLSearchParams): InvestigationFilters {
  const number = (name: string) => {
    const text = query.get(name);
    if (text === null) return undefined;
    if (!text.trim()) throw new Error(`Invalid ${name} duration.`);
    return Number(text);
  };
  const failure = query.get("failure");
  if (failure !== null && failure !== "true" && failure !== "false") throw new Error("Invalid failure condition.");
  // Historical unknown range values still use the existing 24h route default.
  const range = query.get("range");
  return parseInvestigationFilters({ range: range === "1h" || range === "7d" ? range : "24h", sourceId: query.get("source") ?? "", search: query.get("q") ?? "", observedFailure: failure === "true", minDurationMs: number("minMs"), maxDurationMs: number("maxMs"), model: query.get("model") ?? "", tool: query.get("tool") ?? "" });
}
