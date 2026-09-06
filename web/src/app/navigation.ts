import type { ConversationTarget } from "../model/trace-analysis";
import type { SessionListView } from "../model/session-catalog";
import { conditionParameters, filtersFromParameters, type InvestigationFilters } from "../model/investigation-conditions";
import { parseTraceInvestigationState, type TraceInvestigationState } from "../model/trace-investigation";

export type NavigationFilters = InvestigationFilters & Readonly<{ sessionView?: SessionListView }>;

export type NavigationOrigin = Readonly<{
  kind: "conversation" | "trace";
  href: string;
  label: string;
}>;

export type NavigationViewState = Readonly<{
  selectedAgentId?: string;
  purpose?: "execution" | "rework" | "comparison";
  selectedActivityId?: string;
  traceInvestigation?: TraceInvestigationState;
  evidenceFocus?: Readonly<{ kind: "episode" | "activity"; traceId: string; spanId: string }>;
  scrollY?: number;
}>;

export type NavigationState = Readonly<{
  origin?: NavigationOrigin;
  view?: NavigationViewState;
}>;

const sessionViewFromParameters = (query: URLSearchParams): SessionListView => {
  const values = query.getAll("view");
  return values.length === 1 && values[0] === "all" ? "all" : "roots";
};

export const filtersFromLocation = (location: Pick<URL, "searchParams">): NavigationFilters => ({
  ...filtersFromParameters(location.searchParams),
  ...(sessionViewFromParameters(location.searchParams) === "all" ? { sessionView: "all" } : {}),
});

export const canonicalSessionListLocation = (location: URL): string => {
  const query = new URLSearchParams(location.searchParams);
  const view = sessionViewFromParameters(query);
  query.delete("view");
  if (view === "all") query.set("view", "all");
  return withQuery(location.pathname, query) + location.hash;
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

export const traceLocation = (traceId: string, filters: NavigationFilters, spanId?: string) => {
  const query = filterParameters(filters);
  if (spanId) query.set("spanId", spanId);
  return withQuery(`/traces/${encodeURIComponent(traceId)}`, query);
};

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
  const purpose = candidate.purpose === "execution" || candidate.purpose === "rework" || candidate.purpose === "comparison" ? candidate.purpose : undefined;
  const selectedActivityId = typeof candidate.selectedActivityId === "string" ? candidate.selectedActivityId : undefined;
  const scrollY = typeof candidate.scrollY === "number" && Number.isFinite(candidate.scrollY) && candidate.scrollY >= 0
    ? candidate.scrollY
    : undefined;
  const focus = candidate.evidenceFocus;
  const traceInvestigation = parseTraceInvestigationState(candidate.traceInvestigation);
  const evidenceFocus = focus && (focus.kind === "episode" || focus.kind === "activity")
    && typeof focus.traceId === "string" && typeof focus.spanId === "string" && focus.traceId && focus.spanId ? focus : undefined;
  return selectedAgentId === undefined && scrollY === undefined && evidenceFocus === undefined && purpose === undefined && selectedActivityId === undefined && traceInvestigation === undefined
    ? undefined : { selectedAgentId, scrollY, ...(purpose ? { purpose } : {}), ...(selectedActivityId !== undefined ? { selectedActivityId } : {}), ...(evidenceFocus ? { evidenceFocus } : {}), ...(traceInvestigation ? { traceInvestigation } : {}) };
};

const filterParameters = (filters: NavigationFilters) => {
  const query = conditionParameters(filters);
  if (filters.sessionView === "all") query.set("view", "all");
  return query;
};

const withFilters = (path: string, filters: NavigationFilters) => withQuery(path, filterParameters(filters));

const withQuery = (path: string, query: URLSearchParams) => {
  const encoded = query.toString();
  return encoded ? `${path}?${encoded}` : path;
};
