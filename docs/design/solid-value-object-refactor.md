# SOLID and Value Object Refactoring

- Status: Implemented
- Date: 2026-08-14
- Scope: Go query input boundary (`internal/query`) and its HTTP, Connect, MCP, and SQLite collaborators

## Requirement Summary

Query inputs currently represent source-qualified conversations, OTLP anchors, and pagination as unrelated primitive fields. Validation is duplicated by transports and SQLite, so invalid combinations can cross the application boundary. Refactor these inputs into immutable value objects while preserving every external HTTP, Connect, MCP, JSON, and protobuf contract.

## Functional Requirements

1. A conversation identity always contains a trimmed, non-empty source ID and conversation ID.
2. An OTLP trace or span ID is normalized to lowercase and rejects malformed or all-zero hexadecimal values.
3. An activity anchor contains either both a trace ID and a span ID or neither.
4. Pagination always contains a non-negative offset and a size from 1 through 100; its zero value represents the default page of 100 items.
5. Timeline direction is expressed by a query concept rather than an unchecked string.
6. Query filters expose these concepts to readers; transports translate external DTOs into them and SQLite consumes already-valid inputs.

## Non-Functional Requirements

- Preserve external API payloads, status codes, defaults, and pagination behavior.
- Keep the core query package independent from HTTP, Connect, MCP, protobuf, and SQLite.
- Avoid provider-style abstractions with only one implementation.
- Keep value objects immutable and comparable.
- Preserve the current test suite and add behavior tests for every invariant.

## Inputs and Outputs

| Input | Query-boundary output |
|---|---|
| source ID and conversation/session ID strings | `ConversationIdentity` |
| trace ID and optional span ID strings | `TraceID` and `ActivityAnchor` |
| offset and page-size integers | `Page` |
| timeline direction string/enum | `TimelineDirection` |

Reader outputs remain the existing `Session`, `Trace`, `SessionPage`, and `ActivityPage` transport-neutral projections.

## Normal Cases

- Uppercase OTLP IDs are normalized once at the transport boundary.
- A missing page request uses offset 0 and size 100.
- An explicit valid page is passed unchanged to SQLite.
- A source-qualified conversation identity reaches every session reader as one value.

## Error Cases

- Empty or overlong source/conversation IDs are rejected before invoking a reader.
- Negative offsets and page sizes outside 1..100 are rejected.
- A lone trace or span ID is rejected as an incomplete anchor.
- Malformed and all-zero OTLP IDs are rejected.
- Unsupported timeline directions are rejected at construction.

## Edge Cases

- Whitespace around conversation identifiers is removed before length and emptiness checks.
- The zero `Page` is valid and means the documented default, so zero-value filters are safe.
- The zero `ActivityAnchor` means no anchor and is valid.
- Page end/continuation calculations remain overflow-safe for normal API-bounded page sizes.

## Acceptance Criteria

1. New value-object tests demonstrate normalization and all invariants.
2. Query reader signatures no longer accept primitive source/session pairs.
3. Trace, conversation, session-list, and activity-page filters use value objects.
4. SQLite no longer reparses OTLP IDs or silently clamps pagination received through these filters.
5. Existing HTTP, Connect, MCP, storage, and end-to-end tests pass unchanged in observable behavior.
6. `go test ./...` and formatting/static checks pass.

## Non-Goals

- Changing response DTOs or persisted SQLite schemas.
- Converting telemetry observations and canonical projection DTOs to domain entities in this change.
- Hiding every primitive in the repository.
- Introducing repositories, factories, or dependency-injection frameworks without a second implementation need.
- Changing token accounting, ingestion, or analysis behavior.

## Risks and Open Questions

- Compile-time migration touches all query adapters; full Go tests protect the public behavior.
- Existing package-internal callers may have relied on invalid filter values being silently repaired. The intended contract now rejects them at construction.
- Source-specific identifier constraints may eventually differ; current limits preserve the existing HTTP contract and are provider-neutral.

## Conceptual Model

| Concept | Meaning | State | Behavior | Constraint / Invariant |
|---|---|---|---|---|
| `ConversationIdentity` | Source-qualified run/conversation identity | source ID, conversation ID | construct, expose components | both non-empty; source <= 100, conversation <= 500 |
| `TraceID` | OTLP trace identity | normalized hex text | parse, stringify | exactly 32 hex characters; not all zero |
| `SpanID` | OTLP span identity | normalized hex text | parse, stringify | exactly 16 hex characters; not all zero |
| `ActivityAnchor` | Optional exact activity location | trace ID, span ID, presence | construct, inspect | both IDs present or both absent |
| `Page` | Bounded result window | offset, size | expose bounds, compute continuation | offset >= 0; size 1..100; zero value defaults to size 100 |
| `TimelineDirection` | Ordering/navigation policy | enum value | parse, stringify | only the currently supported `older` direction |
| Query filters | Requests understood by query readers | value objects plus optional criteria | carry valid policy input | cannot split coupled concepts into unrelated arguments |

## Relationships

| Concept A | Relationship | Concept B | Notes |
|---|---|---|---|
| `ConversationIdentity` | identifies | `Session` | source qualification prevents cross-source ambiguity |
| `ActivityAnchor` | locates within | `ConversationIdentity` | anchor is optional and validated independently |
| `Page` | windows | `SessionPage`, `ActivityPage`, `Trace` | one rule shared by all query adapters |
| Transport adapter | constructs | value objects | owns syntax-to-domain translation |
| SQLite reader | consumes | query filters | owns persistence only, not transport validation |

## Structural Risks

- Missing concepts: source-qualified identity, complete activity anchor, bounded page, and direction were implicit.
- Hidden state: “anchor supplied” and “page mode” were encoded through empty strings and a boolean.
- Change-prone areas: each transport previously repeated validation and default rules.
- Boundary candidates: constructors in `internal/query` hide validation rules from transports and SQLite.

## Responsibility Assignment

| Responsibility | Owner | Reason to change | SOLID concern | Not owner | Reason |
|---|---|---|---|---|---|
| Conversation identity invariants | `ConversationIdentity` | identity contract changes | SRP, primitive obsession | HTTP/Connect/MCP handlers | duplication across protocols |
| OTLP ID normalization | `TraceID` / `SpanID` | OTLP identity contract changes | SRP | SQLite reader | persistence must not validate protocol syntax |
| Anchor completeness | `ActivityAnchor` | navigation contract changes | SRP | handlers and SQL queries | empty-string coordination is hidden state |
| Pagination bounds/defaults | `Page` | query-window policy changes | SRP, OCP | every transport/store method | repeated clamps diverge |
| Timeline direction vocabulary | `TimelineDirection` | navigation policy changes | OCP | raw strings in filters | unchecked variants leak into storage |
| External request decoding | transport adapters | protocol schema changes | SRP, DIP | query package | core must not import protocol DTOs |
| Query execution | SQLite store | schema/query changes | SRP, DIP | value objects | value objects must have no I/O |

## SOLID Risk Assessment

| Principle | Risk | Mitigation |
|---|---|---|
| SRP | Handlers and SQLite both validate the same inputs | move invariants to values; keep decoding in adapters and SQL in store |
| OCP | A new protocol or page rule requires edits everywhere | all adapters construct the same query concepts |
| LSP | Reader implementations could silently interpret invalid filters differently | only valid value objects cross reader contracts |
| ISP | Consumers could depend on an oversized backend | retain existing small reader interfaces and compose them only at application edges |
| DIP | Query policy could depend on HTTP/protobuf/SQLite DTOs | value objects live in provider-neutral `internal/query` |

## Procedural Risk

- Rules at risk of being placed in handlers/use cases: paired anchor validation, identifier trimming, page bounds, direction validation.
- Behavior that should move closer to state: normalization and invariants move into value constructors; page calculations move into `Page`.
- Abstractions that may be premature: no new repository, strategy, factory, or generic service is introduced.

## Module / Package Boundary Plan

```text
HTTP / Connect / MCP DTOs
          |
          v
internal/query value objects + consumer-oriented reader contracts
          |
          v
internal/storage/sqlite reader implementation
```

`internal/query` remains the stable application-facing contract. Transport and persistence packages depend inward on it; it does not depend outward on either.

## Proposed Interfaces / Signatures

| Name | Consumer | Responsibility | Signature | Error Contract |
|---|---|---|---|---|
| `NewConversationIdentity` | transports/tests | build source-qualified identity | `(sourceID, conversationID string) (ConversationIdentity, error)` | `ErrInvalidConversationIdentity` |
| `ParseTraceID` | transports/tests | build normalized trace ID | `(string) (TraceID, error)` | `ErrInvalidTraceID` |
| `NewActivityAnchor` | transports/tests | build optional complete anchor | `(traceID, spanID string) (ActivityAnchor, error)` | wrapped trace/span/anchor error |
| `NewPage` | transports/tests | build bounded window | `(offset, size int) (Page, error)` | `ErrInvalidPage` |
| `ParseTimelineDirection` | transports | build navigation direction | `(string) (TimelineDirection, error)` | `ErrInvalidTimelineDirection` |
| `SessionSummaryReader.GetSessionSummary` | app services | load one run | `(context.Context, ConversationIdentity) (Session, error)` | existing not-found/storage errors |

## Example Call Sites

```go
identity, err := query.NewConversationIdentity(request.SourceID, request.SessionID)
page, err := query.NewPage(offset, pageSize)
traceID, err := query.ParseTraceID(request.TraceID)

session, err := reader.GetSessionSummary(ctx, identity)
trace, err := reader.GetTrace(ctx, query.TraceFilter{TraceID: traceID, Page: page})
```

## Boundary Decisions

| Boundary | Hidden detail | Reason |
|---|---|---|
| `ConversationIdentity` | trimming and length limits | one identity contract for all protocols |
| `ActivityAnchor` | optional-pair state and OTLP parsing | impossible partial anchors |
| `Page` | defaults, bounds, next/end math | consistent pagination policy |
| transport adapters | protocol field names and error mapping | query/store remain transport-neutral |
| SQLite store | SQL text and scan representation | no database details leak into interfaces |

## Interface Risks

- Oversized interfaces: transport servers still compose only the reader capabilities they use.
- Primitive obsession: optional search text and agent IDs remain strings because they currently have no shared invariant worth a type.
- Infrastructure leakage: no value object contains HTTP, protobuf, MCP, or SQL types.
- Boolean flag risks: conversation page mode becomes a named enum rather than `UseActivityOffset`.

## Test Specifications

| Behavior | Given | When | Then | Test Level | Notes |
|---|---|---|---|---|---|
| Normalize conversation identity | padded valid components | construct | trimmed values returned | unit | observable contract |
| Reject invalid conversation identity | missing/overlong component | construct | typed error | unit | table-driven |
| Normalize OTLP IDs | uppercase IDs | parse | lowercase value object | unit | preserve existing behavior |
| Enforce complete anchor | only one ID | construct | error | unit | pair invariant |
| Default pagination | zero `Page` | read size/offset | 100/0 | unit | safe zero value |
| Enforce pagination | negative offset or invalid size | construct | typed error | unit | table-driven |
| Page continuation | known offset, size, and count | compute | correct end/next state | unit | behavior on value |
| Reject unsupported direction | `newer` | parse | typed error | unit | preserve current contract |
| Adapter mapping | valid external request | invoke handler | reader receives equivalent values | transport tests | existing stubs capture filters |
| Store behavior | valid filters | query SQLite | same sessions/traces/pages | integration-style unit | existing store fixtures |

## Invariant Tests

| Invariant | Example | Expected Result |
|---|---|---|
| Conversation is source-qualified | `codex`, `run-1` | comparable valid identity |
| OTLP IDs are non-zero normalized hex | uppercase non-zero trace | lowercase value |
| Anchor is complete | trace without span | construction fails |
| Page is bounded | offset 0, size 101 | construction fails |
| Zero page is useful | `Page{}` | offset 0, size 100 |

## Error / Edge Case Tests

| Case | Given | When | Then |
|---|---|---|---|
| whitespace-only identity | `"   "` | construct | invalid identity error |
| all-zero OTLP ID | correct length | parse | typed OTLP error |
| negative page offset | -1 | construct | invalid page error |
| zero explicit size | 0 | construct | invalid page error; callers use `DefaultPage`/zero value intentionally |
| absent anchor | empty pair | construct | valid zero anchor |

## Testability Feedback

- Interface concerns: primitive pairs made stubs and callers able to express invalid requests.
- Responsibility concerns: storage silently fixed invalid pages, hiding caller defects.
- Coupling concerns: duplicated validation made adapter tests protocol-specific rather than contract-driven.

## Detailed Design

1. Add immutable, comparable value types in `internal/query/value_objects.go`.
2. Keep their fields private; expose intention-revealing accessors and behavior.
3. Replace coupled primitive fields in query filters and the session-summary method signature.
4. Construct values in HTTP, Connect, and MCP adapters and translate errors to their existing error protocols.
5. Consume accessors in SQLite; remove defensive reparsing/clamping that is now unreachable.
6. Keep response models as strings to preserve serialization and generated contract compatibility.

## TDD Plan

| Behavior | Red Test | Green Implementation | Refactor Target |
|---|---|---|---|
| Conversation identity | constructor normalization/error tables | private fields, constructor, accessors | replace primitive reader signature |
| OTLP identities and anchor | typed value and pair tests | ID values and optional anchor | replace filter string fields |
| Page invariants and behavior | default/bounds/end tests | immutable page methods | remove store clamps and adapter arithmetic duplication where practical |
| Direction vocabulary | parse tests | enum parser | replace raw direction strings |
| Adapter/store compatibility | existing package tests fail to compile/behavior tests fail | migrate call sites in small slices | simplify repeated validation |

## Construction Log

### Value-object invariants

- Red: added `internal/query/value_objects_test.go`; `go test ./internal/query` failed because the constructors and value types did not exist.
- Green: implemented the smallest private representations, constructors, accessors, and pagination behavior; the query package passed.
- Refactor: moved shared OTLP parsing out of `trace.go`, made the zero `Page` the safe default, and named page/anchor calculations by intent.
- Tests run: `GOCACHE=/tmp/agentmetry-go-cache go test ./internal/query`.

### Reader-contract migration

- Red: changed filters and the session-summary reader contract; compilation failed at every primitive call site in HTTP, Connect, MCP, SQLite, and their tests.
- Green: migrated each adapter to construct values and each SQLite reader to consume their accessors.
- Refactor: removed SQLite ID reparsing and silent page clamping; replaced the conversation boolean flag with `ConversationPageMode`.
- Tests run: `GOCACHE=/tmp/agentmetry-go-cache go test ./internal/...` (all non-network packages passed; Connect required the final unrestricted local-listener run).

### Anchored small-page behavior

- Red: review showed that an anchor-derived offset was combined with the original page end, which could exclude the anchor or create an invalid slice for small pages.
- Green: added `Page.OffsetAround` and `Page.WindowEnd`, then used the derived offset for the page window.
- Refactor: centralized the established 25-item anchor context while centering anchors on pages smaller than 50 items.
- Tests run: the new `TestListSessionActivitiesKeepsAnchorInsideSmallPage` and the existing exact-conversation pagination test pass.

### Final verification

- `GOCACHE=/tmp/agentmetry-go-cache go test ./...` passed.
- `GOCACHE=/tmp/agentmetry-go-cache go vet ./...` passed.
- `git diff --check` passed.

## Review Findings

| Location | Issue | Principle | Severity | Resolution |
|---|---|---|---|---|
| transport handlers | repeated identity, OTLP pair, page, and direction validation | SRP, DIP | medium | construct query value objects at each transport boundary |
| SQLite readers | reparsed IDs and silently repaired pages | SRP, LSP | medium | consume valid values through query contracts |
| conversation query | `UseActivityOffset` hid a policy switch in a boolean | interface clarity | medium | replace with `ConversationPageMode` |
| anchored activity pagination | derived start offset used the original end calculation | invariant ownership | high | move window and anchor calculations to `Page`; add regression test |
| query interfaces | primitive source/session pairs could be swapped or partially supplied | primitive obsession, ISP | medium | pass one `ConversationIdentity` value |

## Procedural Review

| Location | Smell | Better Owner | Refactoring Direction |
|---|---|---|---|
| HTTP/Connect/MCP request methods | repeated validation steps | value constructors | adapters only translate and map errors |
| SQLite pagination methods | conditionals normalizing caller input | `Page` | store focuses on query execution |
| anchor navigation | empty-string coordination and magic offset | `ActivityAnchor` and `Page` | expose complete anchor and named window behavior |

No new manager, processor, repository, factory, or one-method strategy abstraction was introduced. Response projections remain data-oriented intentionally because they are transport-neutral query outputs, while behavior governing input state now sits with the corresponding values.

## Refactoring Result

1. Query input invariants have one owner in `internal/query`.
2. Consumer-oriented reader interfaces carry domain vocabulary instead of coupled primitive arguments.
3. Transport details remain outside the query package, and SQL details remain inside SQLite.
4. Existing observable API and storage behavior is protected by unit, adapter, and full repository tests.
