# Initial Vertical Slice Specification

> Historical document. This records the original feasibility milestone and is
> not the current product contract. See [the documentation index](../README.md)
> and [current architecture](../architecture.md).

> The current read API is protobuf-first and supersedes the original
> overview-shaped HTTP/MCP contract described below. See
> [api-proto-contract.md](../design/api-proto-contract.md) and
> [agentmetry.proto](../../proto/agentmetry/v1/agentmetry.proto).

- Status: Approved for construction
- Date: 2026-08-11
- Scope: technical feasibility and architecture validation

## Requirement Summary

The PoC must prove one executable vertical slice: OTLP telemetry enters Agentmetry, is durably recorded in an embedded SQLite database, is normalized into agent-oriented observations, and the same query model is exposed through HTTP, MCP, and a Lit dashboard. The implementation must preserve the intended architecture instead of hiding all behavior in transport handlers or stateful UI components.

## Functional Requirements

1. `agentmetry serve` starts the OTLP receiver, HTTP API, static SPA, MCP endpoint, and embedded SQLite store.
2. The OTLP admission surface accepts traces, logs, and metrics using the selected OpenTelemetry implementation.
3. An accepted batch is acknowledged only after its canonical PoC projection is committed to SQLite. Unsupported details may be preserved as JSON, but the batch must not be silently dropped.
4. Spans retain trace/span/parent identity, timestamps, name, status, resource and span attributes.
5. Logs retain timestamp, severity, body, trace/span identity, resource and log attributes.
6. Metric points retain timestamp, instrument name, kind, numeric value where representable, resource and point attributes.
7. The normalizer recognizes common agent/model/session attributes and typed input/output/cache/reasoning token attributes without depending on Claude Code or Codex SDKs.
8. The query model returns an overview containing signal counts, run/agent counts, and typed token totals.
9. HTTP and MCP obtain that overview from the same application query function.
10. The SPA renders the overview and recent agent activity using independently reusable Lit Web Components.
11. A deterministic OTLP fixture or sender demonstrates the entire path without Claude Code or Codex being installed.

## Non-Functional Requirements

- Server Core: Go; UI: TypeScript + Lit + Vite; store: embedded SQLite WAL.
- The Core is one OS process and requires no external database or Node runtime after build.
- The built SPA is embedded in, or served by, the Go Core.
- Core policy does not depend on HTTP, MCP, SQLite DTOs, or OpenTelemetry SDK structs.
- UI domain state is immutable data. State transitions and derivations are ordinary composable functions; browser/network effects are isolated at the imperative boundary.
- Lit elements are thin Custom Element adapters. A dashboard page composes components; it is not one monolithic dashboard element.
- Default listeners bind to loopback only.
- Tests must run offline after dependencies have been fetched.

## Inputs and Outputs

| Input | Output |
|---|---|
| OTLP trace/log/metric request | protocol success after commit, or protocol error without false durable acknowledgement |
| `GET /api/v1/overview` | versioned JSON overview |
| MCP `get_overview` tool | the same semantic overview as HTTP |
| browser navigation | embedded SPA assets and rendered dashboard components |
| shutdown signal | bounded drain, database close, process exit |

## Normal Cases

- A span with model, agent, session, and token attributes is visible in recent activity and contributes to token totals.
- A retry of the same span identity updates the observation without double-counting its current token values.
- A log without trace identity is retained as a log observation.
- A gauge or sum numeric point contributes to the metric observation count.
- HTTP and MCP views match after the same committed batch.

## Error Cases

- Invalid OTLP payload returns a protocol error and creates no observations.
- SQLite commit failure returns an export failure; no success is reported.
- Unsupported metric point shapes are counted/preserved as unsupported rather than crashing the receiver.
- API query failure returns a structured non-200 response without exposing SQL details.
- Unknown SPA route falls back to the SPA entry point; unknown `/api/` routes do not.

## Edge Cases

- Missing agent/model/session/token attributes produce an observation with explicit unknown/zero values.
- Token attributes represented as integer or numeric string normalize when lossless; negative token values are rejected from totals.
- Parent span can arrive after a child.
- Empty OTLP batch commits no observations and succeeds.
- Repeated span revision must not inflate the overview.
- UI receives an empty overview and renders a valid zero state.

## Acceptance Criteria

1. `go test ./...` passes.
2. UI unit/component tests pass.
3. UI production build succeeds.
4. `go build ./cmd/agentmetry` succeeds with the built SPA embedded.
5. A smoke test starts the binary with a temporary data directory, sends a fixture, and verifies matching HTTP/MCP-visible totals.
6. SQLite is in WAL mode and OTLP success occurs after the transaction commit path.
7. No domain rule exists only inside an HTTP, MCP, OTLP, or Lit handler.
8. At least the time filter, KPI card, activity table, and token chart are separate Custom Elements.
9. Functional-core tests demonstrate immutable transition/derivation behavior independently of the DOM.

## Non-Goals

- Production-ready Claude Code/Codex enrichment adapters.
- Complete coverage of every OTLP metric aggregation and every vendor attribute spelling.
- Tauri shell, signing, notarization, updater, tray, or installer in this PoC.
- Authentication for non-loopback deployment.
- DuckDB/Parquet, ClickHouse, OpenObserve, or replaceable storage plugins.
- Production retention, encryption, backup UI, alerting, or collaboration analysis.
- Pixel-final dashboard design.

## Risks and Open Questions

- The official Collector component lifecycle may make direct embedding heavier than the PoC warrants; the implementation spike must retain official `pdata` semantics and record any reduced receiver surface explicitly.
- Producer attribute names can change; source plugins isolate their versioned aliases and fixtures from the common pipeline.
- A pure-Go versus CGO SQLite binding changes packaging behavior and must be recorded as a PoC choice, not silently treated as the final ADR decision.
- MCP transport/API conformance depends on the current official SDK and must be covered by a focused integration test.

## Conceptual Model

| Concept | Meaning | State | Behavior | Constraint / Invariant |
|---|---|---|---|---|
| Telemetry Batch | One admitted OTLP signal batch | signal, observations | validates and commits atomically | success follows commit |
| Observation | Vendor-neutral recorded fact | identity, time, attributes, provenance | exposes normalized agent fields | raw facts are not inferred facts |
| Span Observation | Timed activity | trace/span/parent, duration, status | replaces an older revision by identity | one current row per trace/span |
| Log Observation | Emitted event | time, severity, body, optional trace | appends unless stable identity exists | absence of trace is valid |
| Metric Observation | Numeric measurement | instrument, kind, time, value | records supported numeric points | unsupported shape is explicit |
| Agent Identity | Observed agent label | source value or unknown | groups activity | no fabricated stable identity |
| Token Usage | Typed usage vector | input/output/cache/reasoning | adds non-negative components | missing is distinct from observed zero internally |
| Overview | Read model | counts, totals, recent activity | derives a consumer-safe snapshot | HTTP and MCP semantics match |
| UI Model | Immutable dashboard state | load state, range, overview | transitions by message | effects are data, not executed inside transition |
| UI Component | Dashboard building block | input properties only | renders and emits intent | no network/database ownership |

## Relationships

| Concept A | Relationship | Concept B | Notes |
|---|---|---|---|
| Telemetry Batch | contains | Observation | committed as one transaction per signal request |
| Observation | may identify | Agent Identity | attribute-derived and provenance-aware |
| Observation | may contain | Token Usage | typed fields, not one ambiguous total |
| Overview | summarizes | Observation | current canonical rows only |
| UI Model | contains | Overview | immutable snapshot |
| Dashboard Page | composes | UI Component | layout and event wiring only |

## Structural Risks

- Missing concepts: unsupported metric point and token provenance can otherwise disappear silently.
- Hidden state: OTLP acknowledgement boundary, SQLite transaction, UI request lifecycle.
- Change-prone areas: vendor attributes, Collector APIs, MCP SDK, metric data model.
- Boundary candidates: admission, normalization, commit, overview query, transports, UI effects.

## Responsibility Assignment

| Responsibility | Owner | Reason to change | SOLID concern | Not owner | Reason |
|---|---|---|---|---|---|
| OTLP decode/lifecycle | ingest adapter | protocol/library changes | DIP | domain | must not import pdata/protobuf |
| Attribute normalization | canonical package | semantic mapping changes | SRP | OTLP handler | shared by signal adapters/tests |
| Atomic persistence | SQLite store | schema/durability changes | DIP | normalizer | storage policy differs from semantics |
| Overview derivation | query package/store query | product read-model changes | ISP | HTTP/MCP | both transports share it |
| HTTP representation | HTTP transport | API contract changes | SRP | query model | transport formatting only |
| MCP representation | MCP transport | MCP contract changes | SRP | query model | tool wiring only |
| UI transitions | UI model module | interaction behavior changes | SRP | Lit element | testable without DOM |
| UI effects | effect interpreter | browser/API changes | DIP | transition function | keep transition deterministic |
| Component rendering | Lit elements | visual/interaction contract | SRP | store | receives state, emits intent |

## SOLID Risk Assessment

| Principle | Risk | Mitigation |
|---|---|---|
| SRP | one `serve` handler accumulates ingest/query/static logic | compose small transport owners in bootstrap only |
| OCP | premature storage/plugin framework | one SQLite implementation; retain raw/schema migration path only |
| LSP | fake repository weakens commit guarantee | define commit postcondition and use real SQLite integration tests |
| ISP | broad repository interface | separate batch commit and overview query consumer interfaces |
| DIP | `pdata`, SQL, or HTTP DTOs leak inward | map at adapters; canonical types are dependency-free |

## Procedural Risk

- Attribute alias rules belong in token/identity normalization, not request handlers.
- Span replacement and token non-double-counting belong in the store schema/commit behavior.
- UI loading/retry transitions belong in the UI model, not click handlers.
- A generic plugin/repository framework is premature for one PoC database.

## Proposed Interfaces / Signatures

| Name | Consumer | Responsibility | Signature | Error Contract |
|---|---|---|---|---|
| `NormalizeTraces` | OTLP adapter | map decoded spans to canonical batch | `(context.Context, ptrace.Traces) (canonical.Batch, error)` | invalid canonical identity |
| `NormalizeLogs` | OTLP adapter | map decoded logs to canonical batch | `(context.Context, plog.Logs) (canonical.Batch, error)` | invalid value conversion |
| `NormalizeMetrics` | OTLP adapter | map decoded metrics to canonical batch | `(context.Context, pmetric.Metrics) (canonical.Batch, error)` | marks unsupported point; fatal only for corrupt input |
| `CommitBatch` | admission | atomically persist observations | `(context.Context, canonical.Batch) error` | success means durable transaction committed |
| `GetOverview` | HTTP/MCP | return product read model | `(context.Context, query.OverviewFilter) (query.Overview, error)` | typed unavailable/internal error |
| `update` | UI shell | transition immutable model | `(Model, Message) => readonly [Model, readonly Effect[]]` | total function with explicit failure messages |
| component properties/events | dashboard composition | render value and emit intent | `Readonly<Props> -> TemplateResult`, typed CustomEvent | no I/O side effects |

## Boundary Decisions

| Boundary | Hidden detail | Reason |
|---|---|---|
| Ingest adapter | Collector/pdata lifecycle | replace/upgrade without changing canonical rules |
| Commit port | SQLite schema and transactions | enforce durable ACK contract |
| Query port | SQL and materialization | identical HTTP/MCP semantics |
| HTTP/MCP | protocol DTOs | prevent transport-driven domain design |
| Functional UI core | DOM, fetch, SSE, Tauri | deterministic state and behavior tests |
| Lit components | rendering lifecycle | reusable standard Custom Elements |

## Test Specifications

| Behavior | Given | When | Then | Test Level | Notes |
|---|---|---|---|---|---|
| typed token normalization | integer and string attributes | normalize span | typed non-negative totals result | unit | aliases table-driven |
| span retry | same trace/span twice | commit revision | one span and latest totals remain | SQLite integration | no double count |
| durable admission | valid batch | export | response success follows commit | integration | failing store returns failure |
| overview parity | committed fixture | call HTTP and MCP | semantic JSON values match | integration | transport envelopes may differ |
| initial UI load | idle model | `connected` message | loading model and fetch effect result | unit | no DOM |
| load success | loading model | overview received | ready immutable model, no effect | unit | prior model unchanged |
| filter intent | time filter interaction | component emits event | typed intent has selected range | component | no fetch inside component |
| empty dashboard | zero overview | render components | valid zero/empty state | component | accessible labels |

## Invariant Tests

| Invariant | Example | Expected Result |
|---|---|---|
| commit before success | injected commit failure | OTLP export fails |
| current span uniqueness | retry same identity | one current row |
| non-negative tokens | `-3` input token attribute | ignored/rejected, never negative overview |
| transport parity | same query filter | HTTP/MCP overview fields equal |
| immutable UI transition | frozen prior model | next model returned without mutation |

## Error / Edge Case Tests

| Case | Given | When | Then |
|---|---|---|---|
| malformed request | invalid protobuf | POST OTLP | 400/appropriate protocol error, no row |
| no attributes | minimal span | normalize | unknown identity and zero typed usage |
| unsupported histogram detail | histogram point | normalize | observation/unsupported count, no panic |
| API failure | closed/cancelled query | request | structured error, no SQL text |
| stale UI response | request generation changed | old response arrives | old response does not replace current model |

## Detailed Design

```text
cmd/agentmetry
  bootstrap only
internal/canonical
  dependency-free observations and normalization vocabulary
internal/ingest/otel
  Collector/pdata adapters and durable export acknowledgement
internal/storage/sqlite
  migrations, transaction commit, overview SQL
internal/query
  overview contracts
internal/transport/httpapi
  JSON API and embedded SPA
internal/transport/mcp
  get_overview tool
web/src/model
  immutable Model/Message/Effect and update/derive functions
web/src/components
  Lit Custom Elements; properties in, typed events out
web/src/app
  composition and effect interpreter
```

The Core dependency direction is transport/ingest/storage -> canonical/query contracts. Bootstrap may know concrete implementations; canonical and query contracts know no framework. The UI dependency direction is app/effects -> model, and components -> read-only view types; the model imports neither Lit nor browser APIs.

## TDD Plan

| Behavior | Red Test | Green Implementation | Refactor Target |
|---|---|---|---|
| normalization | table-driven canonical test | typed attribute readers | central alias vocabulary |
| SQLite commit/query | temporary DB integration test | schema + transaction + overview | isolate row mapping |
| admission ACK | fake failing committer test | exporter returns commit error | keep protocol adapter thin |
| HTTP overview | handler test | query handler/DTO | common overview contract |
| UI transition | frozen model Vitest | `update` function | effect/message naming |
| Web Components | component behavior tests | Lit elements | shared formatting only after duplication |
| MCP overview | SDK integration test | one tool binding | reuse query and DTO mapping |
| vertical slice | process/fixture smoke test | bootstrap wiring | lifecycle and shutdown ownership |
