# Conversation Identity and Trace Explorer Design

- Status: Implemented
- Date: 2026-08-11
- Scope: Agentmetry conversation and trace navigation

## 1. Requirement Summary

Agentmetry must keep source conversations separate from OTLP traces. The dashboard uses conversations as the primary work-history navigation and uses trace identity as a cross-cutting causal view. Selecting a trace from an activity opens a Trace Explorer that can show every correlated span and log event, including evidence from other conversations and agents.

## 2. Requirement Specification

### Functional requirements

1. Two source conversations remain two dashboard items even when they share a trace ID.
2. Conversation identity is namespaced by telemetry source and native conversation ID.
3. One conversation can reference zero, one, or many trace IDs without owning those traces.
4. An activity with trace identity exposes an accessible Trace Explorer action; an activity without trace identity does not invent one.
5. `GET /api/v1/traces/{traceId}` returns the complete retained trace: correlated spans and logs across conversations are not truncated by the dashboard time filter.
6. MCP exposes the same trace query through a read-only `get_trace` tool.
7. The Trace Explorer shows trace identity, time range, status/completeness, participating conversations, agents, and a chronological parent/child span view with correlated logs.
8. `/traces/{traceId}` is a reload-safe SPA deep link. Back navigation returns to the previously selected conversation when available.
9. Raw OTLP evidence and existing canonical observations remain unchanged.

### Error and edge cases

- An unknown or syntactically invalid trace ID returns a stable not-found or validation response.
- Logs with trace ID but no span ID remain visible as trace events.
- A child span whose parent is absent is retained and marked as a missing-parent edge.
- Multiple root spans are permitted and reported.
- Empty conversation identity does not produce a conversation item but does not remove the event from trace results.
- The same native conversation ID emitted by two telemetry sources produces two conversation items.
- A trace may contain activities from multiple conversations, sources, and agents.
- Metrics are not included unless a future projection exposes exemplar trace context; aggregate metrics are not converted into trace activities.

### Non-goals

- Reconstructing unreported conversations or trace IDs.
- Distributed trace search by arbitrary attributes.
- Sampling repair, tail-based trace completion, or remote trace federation.
- A database schema migration; the product already stores the required identifiers and timestamps.

## 3. Conceptual Model

| Concept | Identity | Meaning | Invariant |
|---|---|---|---|
| Conversation | `(source_id, native_conversation_id)` | A source-defined session, thread, or work context | Never merged solely by trace identity |
| Trace | `trace_id` | A causal graph of OTLP spans plus trace-correlated logs | Never substituted for conversation identity |
| Trace Activity | stored observation identity | A span or log participating in a trace | Keeps original signal and source evidence |
| Trace Participant | conversation or agent reference | An entity observed on one or more trace activities | Derived from explicit fields only |
| Missing Parent | `(trace_id, child_span_id, parent_span_id)` | A reported parent edge whose parent span is absent | Does not remove or reparent the child |

The Conversation-to-Trace relationship is many-to-many and is derived from activities carrying both identities.

## 4. Responsibility Assignment

| Responsibility | Owner | Reason |
|---|---|---|
| Conversation identity and grouping rules | `internal/query` model plus SQLite query projection | Keeps identity policy outside transports and UI |
| Trace reconstruction and completeness | SQLite trace reader | Owns data retrieval and graph evidence close to persisted observations |
| Trace query contract | `internal/query.TraceReader` | Consumer-oriented interface independent from SQL and OTLP DTOs |
| HTTP validation/status mapping | `internal/transport/httpapi` | Transport-specific responsibility only |
| MCP tool mapping | `internal/transport/mcpserver` | MCP DTO and schema responsibility only |
| Trace selection and loading state | pure functions in `web/src/model` | Keeps state transitions deterministic and DOM-independent |
| Trace resource link | `am-activity-table` | The activity row owns its reload-safe trace affordance |
| Trace summary, participants, and waterfall | separate Lit components | Components remain reusable and independently testable |
| SPA history and network effects | `am-app` effect boundary | Browser APIs do not leak into the functional model |

## 5. SOLID Risk Assessment

- **SRP:** Do not add trace SQL, graph derivation, or navigation state to HTTP handlers or Lit render functions.
- **OCP:** Add the trace reader beside the overview reader; source plugins require no trace-specific changes.
- **LSP:** HTTP and MCP consume the same trace query semantics and represent missing fields consistently.
- **ISP:** `TraceReader` exposes only `GetTrace`; callers that only need overview do not depend on it.
- **DIP:** Application composition depends on query contracts. Query and UI models do not import SQLite, `pdata`, MCP, or DOM types.
- **Primitive obsession:** Conversation identity is a source-qualified value, not an unqualified string or a delimiter-encoded public key.

## 6. Module and Package Boundary Plan

```text
internal/query
  overview.go          existing conversation-oriented read model
  trace.go             trace filter, result, participant, reader contract

internal/storage/sqlite
  overview.go          source-qualified conversation grouping
  trace.go             span/log retrieval and trace reconstruction

internal/transport/httpapi
  handler.go           GET /api/v1/traces/{traceId}

internal/transport/mcpserver
  server.go            get_trace tool and DTO mapping

web/src/model
  update.ts            pure trace selection/loading transitions
  selectors.ts         selected conversation and trace selectors

web/src/components
  activity-table.ts    native trace resource link
  trace-summary.ts     trace identity/status/duration
  trace-participants.ts
  trace-waterfall.ts

web/src/app
  agentmetry-app.ts     composition, fetch, and History API boundary
```

Dependency direction remains transport/storage/UI adapters toward query contracts and pure UI state.

## 7. Interface and Signature Proposal

```go
type ConversationRef struct {
    SourceID string `json:"sourceId"`
    ID       string `json:"id"`
}

type TraceFilter struct {
    TraceID string
}

type Trace struct {
    TraceID            string               `json:"traceId"`
    StartedAt          time.Time            `json:"startedAt"`
    EndedAt            time.Time            `json:"endedAt"`
    Status             TraceStatus          `json:"status"`
    RootSpanCount      int64                `json:"rootSpanCount"`
    MissingParentCount int64                `json:"missingParentCount"`
    Conversations      []ConversationRef    `json:"conversations"`
    Agents             []TraceAgent         `json:"agents"`
    Activities         []Activity           `json:"activities"`
}

type TraceReader interface {
    GetTrace(context.Context, TraceFilter) (Trace, error)
}
```

The existing session read model gains explicit source-qualified identity for the
v1 compatibility contract. Public trace lookup uses the native trace ID path
segment and never accepts a session ID as a fallback.

HTTP:

```text
GET /api/v1/traces/{traceId}
200 { "version": "v1", "trace": ... }
400 invalid trace ID or range
404 trace not found in the selected range
```

MCP:

```text
get_trace({ traceId }) -> { trace }
```

UI messages and effects add `trace-selected`, `trace-closed`, `trace-received`, `trace-failed`, and `fetch-trace`. These are immutable values processed by the existing pure `update` function.

## 8. Test Specification

### Backend behavior

1. Shared trace ID across two source conversations produces two overview conversation items.
2. Identical native conversation IDs from different sources remain separate.
3. Trace lookup returns spans and trace-correlated logs from all participating conversations.
4. Trace lookup orders activities chronologically and reports roots and missing parents correctly.
5. Unknown trace lookup returns the query-layer not-found error.
6. HTTP passes range and trace identity, returns versioned JSON, validates input, and maps not-found to 404.
7. MCP `get_trace` returns the same semantic result and typed token presence as HTTP.

### UI behavior

1. Selecting a trace updates the model and emits one fetch effect without changing the selected conversation.
2. Stale trace responses do not replace a newer selection.
3. Closing a trace clears trace state without clearing conversation state.
4. Activity rows render a native trace resource link only for reported trace IDs.
5. The Trace Explorer renders participating conversations/agents, roots, missing parents, span hierarchy, and correlated logs.
6. A direct `/traces/{traceId}` load fetches and renders the trace.
7. Browser back/close returns to the conversation view.

## 9. Detailed Design

The overview query groups meaningful activities by `(source, run_id)` only. It no longer uses a disjoint-set union between run and trace identifiers. Agent topology remains derived from explicit `agent_id`, `parent_agent_id`, type, and delegation evidence inside each conversation.

The trace query reads every retained span and log with the requested trace ID. It deliberately does not inherit the overview time filter: cutting a trace at the dashboard window could falsely classify an older parent as missing. It extends activity timing with start/end/status where available. Span IDs form an in-memory set. A span is a root when its parent ID is empty; it has a missing parent when the parent ID is non-empty and absent from the complete retained set. Logs remain point-in-time activities and are not fabricated as spans.

Trace IDs are parsed once at the query boundary as non-zero, 32-character hexadecimal OTLP identifiers and normalized to lowercase. Trace status is a closed `unknown | ok | error` projection: any error span wins, otherwise any OK span wins over unknown. Public collection fields are always JSON arrays, including when no conversation or agent metadata was reported.

Trace participants are stable, sorted projections:

- conversations deduplicate by `(source, run_id)`;
- agents deduplicate by `(source, run_id, agent_id)`;
- activities retain source, signal, model, token evidence, and native trace/span fields.

The application renders either the conversation workspace or a full-width Trace Explorer under the global header/KPIs. The activity table renders a native `/traces/{traceId}` link; it does not fetch, mutate application state, or own trace state. Loading that URL lets the application route boundary fetch trace JSON. This keeps navigation operational without relying on a custom event crossing nested shadow roots.

## 10. TDD Construction Plan

1. Replace the shared-trace session test with failing conversation-separation and source-namespace tests.
2. Make overview grouping pass without adding trace-query behavior.
3. Add failing SQLite trace-reader tests for correlation, ordering, roots, and missing parents.
4. Implement the smallest query model and SQLite reader.
5. Add failing HTTP and MCP contract tests, then implement the adapters.
6. Add failing pure-model tests for trace selection, stale responses, and close behavior.
7. Implement model types and transitions.
8. Add failing component tests for the trace action and explorer components.
9. Implement the Lit components and app composition/history effects.
10. Run all Go tests, `go vet`, web tests, and production build; then verify the complete workflow in a real browser using a temporary database.
