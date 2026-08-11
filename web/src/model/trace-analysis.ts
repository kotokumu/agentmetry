import type { Activity, TokenUsage, Trace, TraceAgent } from "./update";

export type ConversationTarget = Readonly<{
  sourceId: string;
  conversationId: string;
  traceId?: string;
  spanId?: string;
}>;

export type TraceAgentUsage = TraceAgent & Readonly<{
  activityCount: number;
  tokens: TokenUsage;
}>;

export type TokenEvidence = Readonly<{
  kind: "total" | "partial" | "none";
  total?: number;
  components: readonly (readonly [string, number])[];
}>;

const emptyTokens = (): TokenUsage => ({
  input: null,
  output: null,
  cacheRead: null,
  cacheWrite: null,
  reasoning: null,
  total: null,
});

const agentKey = (sourceId: string, conversationId: string, agentId: string) =>
  JSON.stringify([sourceId, conversationId, agentId]);

const addComponent = (current: number | null, observed: number | null) =>
  observed === null ? current : (current ?? 0) + observed;

const addTokens = (current: TokenUsage, observed: TokenUsage): TokenUsage => ({
  input: addComponent(current.input, observed.input),
  output: addComponent(current.output, observed.output),
  cacheRead: addComponent(current.cacheRead, observed.cacheRead),
  cacheWrite: addComponent(current.cacheWrite, observed.cacheWrite),
  reasoning: addComponent(current.reasoning, observed.reasoning),
  total: addComponent(current.total, observed.total),
});

export const tokenEvidence = (tokens: TokenUsage): TokenEvidence => {
  const components = [
    ["input", tokens.input],
    ["output", tokens.output],
    ["cache read", tokens.cacheRead],
    ["cache write", tokens.cacheWrite],
    ["reasoning", tokens.reasoning],
  ].filter((entry): entry is [string, number] => entry[1] !== null);
  if (tokens.total !== null) return { kind: "total", total: tokens.total, components };
  return { kind: components.length === 0 ? "none" : "partial", components };
};

export const aggregateTraceAgentUsage = (trace: Trace): readonly TraceAgentUsage[] => {
  const usage = new Map<string, TraceAgentUsage>();
  for (const agent of trace.agents) {
    usage.set(agentKey(agent.sourceId, agent.conversationId, agent.agentId), {
      ...agent,
      activityCount: 0,
      tokens: emptyTokens(),
    });
  }
  for (const activity of trace.activities) {
    if (!activity.agentId) continue;
    const key = agentKey(activity.source, activity.runId, activity.agentId);
    const current = usage.get(key) ?? {
      sourceId: activity.source,
      conversationId: activity.runId,
      agentId: activity.agentId,
      agentDefinition: activity.agentDefinition,
      agentType: activity.agentType,
      parentAgentId: activity.parentAgentId,
      model: activity.model,
      activityCount: 0,
      tokens: emptyTokens(),
    };
    usage.set(key, {
      ...current,
      agentDefinition: current.agentDefinition || activity.agentDefinition,
      agentType: current.agentType || activity.agentType,
      parentAgentId: current.parentAgentId || activity.parentAgentId,
      model: current.model || activity.model,
      activityCount: current.activityCount + 1,
      tokens: activity.contributesToTotal ? addTokens(current.tokens, activity.tokens) : current.tokens,
    });
  }
  return [...usage.values()].sort((left, right) =>
    left.sourceId.localeCompare(right.sourceId)
      || left.conversationId.localeCompare(right.conversationId)
      || left.agentId.localeCompare(right.agentId));
};

export const conversationHref = (activity: Activity): string | undefined => {
  if (!activity.source || !activity.runId) return undefined;
  const traceId = activity.traceId || activity.relatedTraceId;
  const spanId = activity.spanId || activity.relatedSpanId;
  if (activity.signal !== "trace" && !traceId) return undefined;
  const path = `/conversations/${encodeURIComponent(activity.source)}/${encodeURIComponent(activity.runId)}`;
  if (!traceId || !spanId) return path;
  const parameters = new URLSearchParams({ traceId, spanId });
  return `${path}?${parameters}`;
};

export const conversationTargetFromLocation = (pathname: string, search: string): ConversationTarget | undefined => {
  const match = pathname.match(/^\/conversations\/([^/]+)\/([^/]+)$/);
  if (!match) return undefined;
  try {
    const sourceId = decodeURIComponent(match[1]);
    const conversationId = decodeURIComponent(match[2]);
    if (!sourceId || !conversationId) return undefined;
    const parameters = new URLSearchParams(search);
    const traceId = parameters.get("traceId") || undefined;
    const spanId = parameters.get("spanId") || undefined;
    if (Boolean(traceId) !== Boolean(spanId)) return undefined;
    return { sourceId, conversationId, traceId, spanId };
  } catch {
    return undefined;
  }
};
