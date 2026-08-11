# Trace Agent Analysis and Conversation Navigation

- Status: Implemented
- Date: 2026-08-11
- Risk: Medium

## 1. Requirement summary

Trace Explorer must support agent-oriented analysis, not only span timing. It must expose inter-agent communication, per-agent observed token consumption, operation/message details comparable to the conversation view, and reload-safe navigation from a trace span to its source conversation.

## 2. Requirement specification

1. Trace Explorer shows observed token totals for each `(source, conversation, agent)` participant.
2. Token totals use only activities marked `contributesToTotal`; cache and reasoning components retain their reported semantics.
3. The trace timeline identifies the source agent and reported target agent for delegation or message activities.
4. Every trace row is expandable. Its collapsed summary shows span/event name, responsible agent, reported communication target, observed total tokens, duration/time, and status.
5. Expanded details show the equivalent evidence available in the conversation operation/message list: content, source, conversation, model, token components, trace/span/parent IDs, operation kind, and target agent metadata.
6. Every trace span with source and conversation identity links to `/conversations/{source}/{conversation}?traceId={trace}&spanId={span}`.
7. Loading that URL reads the exact source-qualified conversation independently of dashboard time filters and display pagination, then highlights the exact `(trace ID, span ID)` activity.
8. Logs without a conversation ID remain visible but do not invent a conversation link.
9. Existing Trace, MCP, and database schemas remain unchanged; HTTP gains a source-neutral exact conversation read endpoint.

## 3. Conceptual model

| Concept | Identity | Meaning |
|---|---|---|
| Trace agent usage | `(source, conversation, agent)` | Derived observed usage contributed by one trace participant |
| Communication edge | `(source agent, target agent, activity)` | Explicit reported delegation/message evidence |
| Conversation target | `(source, conversation, trace?, span?)` | Reload-safe navigation destination from trace evidence |
| Highlighted activity | `(trace_id, span_id)` within the selected conversation | Visual focus after cross-view navigation |

Trace agent usage is a derived UI projection over the existing canonical activities. It is not a new stored counter.

## 4. Responsibility assignment

| Responsibility | Owner |
|---|---|
| Aggregate trace-agent token evidence | Pure selector in `web/src/model` |
| Build conversation/span URLs | Pure navigation functions in `web/src/model` |
| Render participant usage | `am-trace-participants` |
| Render collapsed trace summaries, expanded evidence, communication, and navigation | `am-trace-waterfall` |
| Parse conversation deep links | `am-app` route boundary |
| Read a source-qualified conversation | `query.ConversationReader` and SQLite adapter |
| Select source-qualified conversation | Pure model update after exact conversation response |
| Highlight a target span | `am-activity-table` property/rendering |

## 5. SOLID risk assessment

- Token aggregation must not move into Lit render methods.
- Source-specific Claude or Codex rules must not enter trace components.
- The timeline must not mutate application state or fetch conversation data.
- Conversation identity remains source-qualified throughout routing and selection.
- The activity table gains presentation inputs only; it does not learn trace-query behavior.

## 6. Module boundary plan

```text
web/src/model/trace-analysis.ts   pure usage and URL derivation
web/src/model/update.ts           requested conversation/span route state
web/src/components/
  trace-participants.ts           per-agent usage
  trace-waterfall.ts              expandable summaries, evidence, communication, and conversation links
  activity-table.ts               optional highlighted span
web/src/app/agentmetry-app.ts     route parsing and component composition
internal/query/conversation.go    exact source-neutral read contract
internal/storage/sqlite/          exact retained-conversation adapter
internal/transport/httpapi/       exact conversation HTTP endpoint
```

## 7. Interface proposal

```ts
type ConversationTarget = Readonly<{
  sourceId: string;
  conversationId: string;
  traceId?: string;
  spanId?: string;
}>;

type TraceAgentUsage = Readonly<{
  sourceId: string;
  conversationId: string;
  agentId: string;
  activityCount: number;
  tokens: TokenUsage;
}>;

aggregateTraceAgentUsage(trace: Trace): readonly TraceAgentUsage[];
conversationHref(activity: Activity): string | undefined;
conversationTargetFromLocation(pathname: string, search: string): ConversationTarget | undefined;
```

`am-activity-table` gains optional `highlightedTraceId` and `highlightedSpanId` properties. `am-trace-waterfall` renders native conversation anchors in expanded evidence for linkable rows.

## 8. Test specification

1. Agent usage aggregation separates equal agent IDs across source/conversation boundaries.
2. Aggregation includes only contributing activities and preserves nullable token components.
3. Conversation URLs percent-encode source, conversation, trace, and span IDs.
4. Route parsing rejects malformed paths and returns decoded source-qualified identity.
5. Exact conversation receipt selects a source-qualified conversation outside the overview range without falling back to the first conversation.
6. Trace participants show activity count and observed token components.
7. Collapsed timeline summaries show the responsible agent, target agent, token total, status, and timing without expansion.
8. Expanded rows show content, model, token breakdown, identifiers, and a native conversation link.
9. The conversation detail table highlights the target span without hiding other activities.

## 9. Detailed design

Agent usage is folded from `trace.activities`. The key is a structured tuple, never a delimiter-visible public identity. An activity increments the agent activity count when it reports an agent ID. Tokens are added only when `contributesToTotal` is true; a component remains `null` until at least one contributing activity explicitly reports it.

The waterfall retains chronological and parent-depth layout. Each row is a native `<details>` disclosure. Its `<summary>` keeps the analysis-critical fields visible: tool name or operation/span, `agent → target`, observed total tokens, status, and timing bar. Expanded evidence shows the content, source, conversation, model, tool and telemetry names, all reported token components, trace/span/parent identifiers, kind, target metadata, and whether reported usage contributes to the rollup. A native conversation anchor is present in expanded evidence when both `source` and `runId` exist; its target URL carries `traceId` and `spanId` together. The collapsed `<summary>` remains the sole disclosure control.

On a conversation deep link, the application requests the exact retained conversation through `ConversationReader` while overview data loads independently for dashboard summaries. The exact read does not accept range, search, offset, or limit parameters. When a trace and span are requested, both must match a trace activity inside the source-qualified conversation; otherwise the endpoint returns not found and the UI shows an explicit error. The activity table receives both IDs, marks the matching row semantically, and scrolls it into view. No unrelated overview conversation is used as a fallback.

Trace rows use the same canonical `Activity` vocabulary as the conversation table, but present it at a different density: analysis-critical identity and usage remain visible while complete evidence is disclosed on demand.

## 10. TDD construction plan

1. Add failing pure tests for token aggregation, URL derivation, and route parsing.
2. Implement the pure trace-analysis module.
3. Add failing model tests for source-qualified route selection after overview load.
4. Implement route state transitions.
5. Add failing component tests for participant usage, collapsed communication/token summaries, expanded evidence, conversation links, and span highlighting.
6. Implement the smallest component changes.
7. Add a failing composition test for the expandable trace analysis view.
8. Implement Trace Explorer composition and route parsing.
9. Run all web tests, production build, Go tests, and browser QA.
10. Review boundaries, event/navigation behavior, and duplicated presentation logic.

## 11. Verification

- Web unit and component suite: 40 tests passing.
- Production SPA build: passing.
- Go unit and integration suite: passing.
- Go static analysis: passing.
- The embedded SPA, trace API, exact conversation/span response, and target-not-found response were verified through a running local server with a cross-conversation fixture.
