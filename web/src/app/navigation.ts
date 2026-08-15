import type { ConversationTarget } from "../model/trace-analysis";
import type { TimeRange } from "../model/telemetry";

export type NavigationFilters = Readonly<{
  range: TimeRange;
  sourceId: string;
  search: string;
}>;

export type NavigationOrigin = Readonly<{
  kind: "conversation" | "trace";
  href: string;
  label: string;
}>;

export type NavigationViewState = Readonly<{
  selectedAgentId?: string;
  scrollY?: number;
}>;

export type NavigationState = Readonly<{
  origin?: NavigationOrigin;
  view?: NavigationViewState;
}>;

const DEFAULT_FILTERS: NavigationFilters = {
  range: "24h",
  sourceId: "",
  search: "",
};

export const filtersFromLocation = (location: Pick<URL, "searchParams">): NavigationFilters => {
  const range = location.searchParams.get("range");
  return {
    range: range === "1h" || range === "7d" ? range : DEFAULT_FILTERS.range,
    sourceId: location.searchParams.get("source") ?? DEFAULT_FILTERS.sourceId,
    search: location.searchParams.get("q") ?? DEFAULT_FILTERS.search,
  };
};

export const dashboardLocation = (filters: NavigationFilters) => withFilters("/", filters);

export const conversationLocation = (
  target: ConversationTarget,
  filters: NavigationFilters,
) => {
  const query = filterParameters(filters);
  if (target.traceId && target.spanId) {
    query.set("traceId", target.traceId);
    query.set("spanId", target.spanId);
  }
  return withQuery(
    `/conversations/${encodeURIComponent(target.sourceId)}/${encodeURIComponent(target.conversationId)}`,
    query,
  );
};

export const traceLocation = (traceId: string, filters: NavigationFilters) =>
  withFilters(`/traces/${encodeURIComponent(traceId)}`, filters);

export const navigationOriginFromState = (state: unknown): NavigationOrigin | undefined => {
  if (!state || typeof state !== "object" || !("origin" in state)) return undefined;
  const origin = (state as { origin?: unknown }).origin;
  if (!origin || typeof origin !== "object") return undefined;
  const candidate = origin as Partial<NavigationOrigin>;
  if ((candidate.kind !== "conversation" && candidate.kind !== "trace")
    || typeof candidate.href !== "string"
    || !candidate.href.startsWith("/")
    || candidate.href.startsWith("//")
    || typeof candidate.label !== "string"
    || !candidate.label.trim()) return undefined;
  return { kind: candidate.kind, href: candidate.href, label: candidate.label };
};

export const navigationViewStateFromState = (state: unknown): NavigationViewState | undefined => {
  if (!state || typeof state !== "object" || !("view" in state)) return undefined;
  const view = (state as { view?: unknown }).view;
  if (!view || typeof view !== "object") return undefined;
  const candidate = view as Partial<NavigationViewState>;
  const selectedAgentId = typeof candidate.selectedAgentId === "string" ? candidate.selectedAgentId : undefined;
  const scrollY = typeof candidate.scrollY === "number" && Number.isFinite(candidate.scrollY) && candidate.scrollY >= 0
    ? candidate.scrollY
    : undefined;
  return selectedAgentId === undefined && scrollY === undefined ? undefined : { selectedAgentId, scrollY };
};

const filterParameters = (filters: NavigationFilters) => {
  const query = new URLSearchParams();
  if (filters.range !== DEFAULT_FILTERS.range) query.set("range", filters.range);
  if (filters.sourceId) query.set("source", filters.sourceId);
  if (filters.search) query.set("q", filters.search);
  return query;
};

const withFilters = (path: string, filters: NavigationFilters) => withQuery(path, filterParameters(filters));

const withQuery = (path: string, query: URLSearchParams) => {
  const encoded = query.toString();
  return encoded ? `${path}?${encoded}` : path;
};
