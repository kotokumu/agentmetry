# ADR 0006: Lossless OTLP Journal and Canonical Observations

- Status: Accepted
- Date: 2026-08-11

## Requirement Summary

Agentmetry must retain every accepted OTLP export while its UI, MCP tools, and vendor interpretations evolve. Codex, Claude Code, and future producers are converted into a source-neutral Agentmetry observation model. "Normalization" in this ADR means semantic normalization across producers, not relational database normalization.

## Requirement Specification

1. A valid OTLP traces, logs, or metrics export is acknowledged only after durable SQLite commit.
2. The commit atomically stores an immutable canonical-protobuf envelope and all canonical observations derived from it.
3. Canonical observations retain every field understood by the pinned OTLP schema, including fields not used by the current UI.
4. Producer-specific names remain available as provenance while common concepts such as session, agent, model, token usage, tool operation, and delegation use Agentmetry names.
5. Nested data remains nested JSON where relational decomposition adds no immediate query value. OTLP value types must remain distinguishable.
6. Every observation references its source export and normalizer version.
7. Historical exports can be normalized again after the canonical model or a source profile changes.
8. A normalization failure never loses a valid export. The raw envelope is committed with a failed status and is retryable.
9. Agentmetry does not currently apply an automatic retention policy.

### Non-goals

- Third-normal-form decomposition of the complete OTLP data model.
- Byte-for-byte preservation of HTTP JSON whitespace, headers, or compressed frames.
- Preservation of protobuf fields unknown to the pinned decoder version.
- A stable public SQL schema; consumers use the documented API surfaces.

## Conceptual Model

```text
OTLP Export
  ├── Journal Envelope
  │     signal, transport, received time, canonical protobuf, hash
  └── Canonical Observations[]
        common identity and time fields
        source-neutral kind and operation
        agent/session/model/token fields
        complete canonical payload JSON
        original attributes JSON
        source profile and normalizer version

Canonical Observations
  -> versioned Agentmetry read models
  -> Dashboard
  -> MCP
```

### Invariants

- Journal envelopes are append-only.
- An acknowledged export has a durable journal row.
- Every canonical observation references its journal export.
- Canonical payloads preserve arrays, maps, bytes, integers, booleans, and nested values without string coercion.
- The journal is the replay source; dashboard and MCP views are derived.
- Identical payload hashes are not automatically deduplicated because identical arrivals can be legitimate.

## Responsibility Assignment

| Component | Responsibility |
|---|---|
| OTLP receiver | Decode the request and produce canonical protobuf |
| Source detector | Identify Codex, Claude Code, or an unknown producer from resource data |
| Canonical normalizer | Convert every signal record into source-neutral observations |
| Source profile | Add vendor-specific semantic interpretation without hiding original fields |
| Export store | Atomically append the envelope and canonical observations |
| Read-model builder | Build turns, agents, quota views, timelines, and dashboard rows |
| Replay service | Renormalize pending, failed, or obsolete exports |

## SOLID Risk Assessment

- **SRP:** wire decoding, semantic normalization, vendor interpretation, storage, and UI aggregation stay separate.
- **OCP:** a new producer adds a source profile; the OTLP receiver and journal remain unchanged.
- **LSP:** traces, logs, and metrics share the same durable-acceptance contract.
- **ISP:** ingestion depends on one export commit port; queries do not depend on receiver internals.
- **DIP:** SQLite implements storage contracts owned by the ingestion/domain boundary.

The main risk is a procedural normalizer with producer-specific conditionals. Common OTLP conversion and source profiles must remain separate collaborators.

## Module Boundary Plan

```text
internal/ingest
  envelope.go          export envelope and accepted-export contract

internal/ingest/otel
  receiver.go          gRPC/HTTP protocol adapters
  normalize.go         complete OTLP -> canonical observations

internal/observation
  model.go             source-neutral canonical observation model

internal/source/codex
internal/source/claude
  profile.go           producer-specific semantic enrichment

internal/storage/sqlite
  journal.go           immutable envelopes and normalization state
  observations.go      append-only canonical observation records
  readmodels.go        replaceable UI/MCP projections
```

## Interface Proposal

```go
type Envelope struct {
    Signal     Signal
    Transport  Transport
    ReceivedAt time.Time
    Payload    []byte // canonical OTLP protobuf ExportRequest
    SHA256     [32]byte
}

type Observation struct {
    Ordinal           int
    Signal            Signal
    Kind              Kind
    Source            Source
    SourceEventName   string
    OccurredAt        time.Time
    Identity          Identity
    Agent             AgentContext
    Usage             TokenUsage
    Payload           json.RawMessage
    SourceAttributes  json.RawMessage
    NormalizerVersion int
}

type AcceptedExport struct {
    Envelope     Envelope
    Observations []Observation
}

type ExportCommitter interface {
    CommitExport(context.Context, AcceptedExport) error
}
```

## Physical Storage Direction

The first migration adds only two canonical tables:

### `otlp_exports`

- identity, signal, transport, received time
- canonical protobuf payload, SHA-256, byte size
- source hint, source application version
- normalization status, version, and error

### `observations`

- export identity and stable ordinal
- signal, canonical kind, source, source event name
- occurred/observed time
- trace/span/session/agent/model columns used by current queries
- token usage columns used by usage analysis
- complete canonical `payload_json`
- complete original `attributes_json`
- normalizer version

Only frequently queried fields are promoted to columns. Span events, links, metric temporality, histogram buckets, exemplars, log bodies, resource/scope data, and other details remain losslessly represented in `payload_json` until a proven query requires a specialized index or derived table.

Existing `spans`, `logs`, and `metrics` become replaceable read models. They are not the canonical store.

### Column Promotion

New query requirements do not require producers to resend telemetry:

1. Add a nullable physical column for a frequently grouped or ranged value, or add an expression index for a simple JSON scalar.
2. Backfill it from versioned `payload_json` in bounded batches.
3. Make the current normalizer populate it for new observations.
4. Record the projection migration and its completion checkpoint.
5. Build a replaceable derived table instead when the value requires joins, graph traversal, windowing, or aggregation.

Canonical observations remain immutable during this process. A failed or interrupted backfill resumes from its checkpoint, and the UI continues to use the previous read model until the new projection is complete.

## Test Specifications

1. Storage failure prevents OTLP acknowledgement.
2. Envelope and observations commit atomically.
3. A valid export with normalization failure is journaled, marked failed, acknowledged, and replayable.
4. Trace fixtures preserve resources, scopes, schema URLs, status, events, links, dropped counts, and typed attributes in canonical JSON.
5. Log fixtures preserve event/observed time, severity, body AnyValue, trace context, flags, and attributes.
6. Metric fixtures preserve gauge, sum, histogram, exponential histogram, summary, temporality, monotonicity, buckets, quantiles, exemplars, and flags.
7. AnyValue fixtures preserve int64 boundaries, bytes, arrays, and nested key-value lists.
8. Codex and Claude fixtures with equivalent concepts produce the same canonical fields and retain their source-specific fields.
9. Replay with a new normalizer version replaces canonical observations for one export without changing the journal.
10. Existing dashboard, MCP, subagent, UTC, and end-to-end tests remain green.

## Detailed Design

The receiver decodes each request once, marshals it to canonical OTLP protobuf, and normalizes the same in-memory `pdata` object. SQLite appends the envelope and all observations in one transaction before returning success.

Canonical payload JSON uses an explicit versioned schema owned by Agentmetry, not a stringified `pdata` dump. Common columns support current product queries; the payload retains everything else. Authorization headers and transport secrets are never stored.

Request-level usage events are canonical observations. Aggregate OTLP metrics are also retained but keep temporality and aggregation metadata, so usage analysis never blindly sums cumulative histogram exports.

## TDD Construction Plan

1. Add failing journal durability and atomicity tests.
2. Add the envelope contract and two-table SQLite migration.
3. Change each receiver to commit canonical protobuf and current observations.
4. Add complete signal fixtures and failing preservation tests.
5. Extend the canonical observation payload signal by signal.
6. Add Codex and Claude source profiles.
7. Add versioned replay behavior.
8. Rebuild the existing dashboard read models from observations.
9. Run Go tests, vet, web tests, and the in-process OTLP end-to-end suite.
10. Review interface size, replay safety, and normalizer responsibilities.

## Consequences

- Storage grows intentionally while the product model evolves.
- UI and MCP changes can reuse accumulated telemetry without asking producers to resend it.
- SQLite remains the supported local store because the canonical model is append-oriented and only useful fields are indexed.
- The pinned Collector/pdata version defines which OTLP fields can be preserved semantically; upgrades require compatibility fixtures.
