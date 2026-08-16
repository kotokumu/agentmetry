# ADR 0003: Rust Telemetry Ecosystem Reuse

- Status: Superseded by the current Go product architecture
- Date: 2026-08-10
- Research: luna + parallel subagents, using only official sources

## Context

Reevaluate whether choosing a Rust Server Core requires building the entire OTLP receiver and pipeline from scratch. At minimum, evaluate Rotel, Vector, Quickwit, and OpenObserve without conflating collectors, pipelines, search backends, and full backends.

Agentmetry requirements:

- OTLP traces, logs, and metrics from Claude Code and Codex
- gRPC, HTTP protobuf/JSON, and gzip
- ACK after a durable database commit
- Lossless preservation of raw OTLP and provenance
- Normalizer, agent graph, token analysis, Web UI, and MCP
- `.dmg`/`.msi`, standalone, and single-container distributions
- No database or collector child process in the strict Server Core

## Current Decision

Update the comparison to these two options:

1. **Low-risk baseline:** Go + official OTel Collector/pdata + SQLite canonical + optional DuckDB/Parquet
2. **Rust challenger:** Rust + pinned/vendored Rotel + SQLite canonical + optional DuckDB/Parquet + Tauri

Rotel improves the Rust option from "build the entire receiver" to "modify, isolate, and reuse an existing receiver." Do not use the current Rotel implementation unchanged at the production ingestion boundary.

Add OpenObserve to the full-OSS PoC. Treat Vector only as an optional external gateway and Quickwit only as an optional search backend.

## Ecosystem Funnel

| Candidate | Proper role | Three signals | Same-process reuse | Decision |
|---|---|---:|---:|---|
| Rotel v0.2.5 | embedded OTLP receiver/pipeline | yes | possible through a source/Git dependency | Rust direct shortlist; adapter/changes required |
| Vector v0.57 | external gateway/pipeline | yes; trace/metrics transforms are experimental | no stable embedded contract | optional external gateway |
| Quickwit v0.9 | logs/traces search/FTS | no metrics | separate server | optional search only |
| OpenObserve v0.92 | full observability backend | yes | not a library embedded in Agentmetry Core | OSS full-backend PoC |
| Tantivy | embedded FTS | n/a | yes | only if SQLite FTS5 is insufficient |
| DataFusion | embedded Arrow/Parquet analytics | n/a | yes | DuckDB challenger |

## Rotel Assessment

### Confirmed capabilities

- OTLP traces, logs, and metrics
- gRPC protobuf
- HTTP protobuf/JSON
- gzip
- Bounded in-memory channel and backpressure
- HTTP/gRPC graceful shutdown
- Batching and export retries
- Apache-2.0
- Public Rust modules and builders permit same-process embedding

### Adoption blockers in v0.2.5

- Returns a success response after enqueueing to the bounded channel rather than after the SQLite commit
- Implements all-success/all-error behavior rather than partial success
- No ingress TLS, mTLS, or authentication
- Does not preserve pre-decode raw wire bytes, compression, or unknown future fields
- Pre-1.0, without a stable crates.io library/API contract
- Official binaries exist only for macOS arm64 and Linux arm64/x64
- No Windows x64, macOS x64, or Linux musl artifacts

For the Rust spike, vendor Rotel at a fixed revision and isolate it behind an Agentmetry adapter. A change or wrapper must provide:

1. OTLP ACK only after the durable journal commit completes
2. Pre-decode capture of raw request bytes and provenance
3. Duplicate and retry semantics
4. Windows and macOS Intel builds and CI
5. TLS/authentication when allowing addresses other than localhost
6. Malformed, oversized, and partial-failure tests

## Vector Assessment

Strengths:

- All three OTLP signals over gRPC/HTTP
- TLS/mTLS
- Bounded memory/disk buffers
- Source-to-sink end-to-end acknowledgements
- Retries, backpressure, and VRL transforms
- Windows x64, macOS arm64, and Linux artifacts

Constraints:

- The OTLP source is beta, and trace/metrics transformation is experimental
- HTTP supports protobuf only, not OTLP JSON
- Converts data to Vector Value instead of preserving raw wire bytes
- The core and subcrates set `publish=false` and provide no stable embedded API contract
- Same-process vendoring adds significant build, API, and size burden
- MPL-2.0

Vector can therefore support an optional `Claude/Codex → Vector → Agentmetry` gateway, but will not be embedded in the strict Agentmetry Core.

## Quickwit Assessment

- OTLP logs and traces
- gRPC/HTTP protobuf
- Append-oriented immutable splits, FTS, and search
- No metrics
- Officially discourages mutable workloads
- Retention operates at split rather than document granularity
- No Windows support
- Recommends at least 8 GB for the indexer
- No MCP

Do not use Quickwit as the Agentmetry canonical store. Consider it only if an external logs/traces FTS backend becomes necessary.

## OpenObserve Assessment

OpenObserve v0.92 is a strong candidate for the full external OSS option.

- All three OTLP signals over HTTP/gRPC
- Single server binary
- Web UI, SQL, PromQL, and FTS
- Trace waterfall and service graph
- SQLite metadata plus local/object storage
- WAL → Parquet
- Windows x64, macOS arm64, and Linux
- OSS MCP, trace/session analysis, agent/service graph, agent filters, and AI evaluation

Risks:

- AGPL-3.0
- Unclear record-level late upsert/correction contract
- Rejects old events by default
- "Original data" storage does not guarantee raw OTLP wire bytes
- macOS Intel artifacts and `.dmg`/`.msi` integration remain Agentmetry's responsibility
- Changing the canonical model or UX for Agentmetry could require fork maintenance

Compare SigNoz and OpenObserve with the same fixtures in the PoC. Measure requirements coverage using the upstream binary, API, and MCP without assuming a fork.

## Rust and DuckDB

The `bundled` feature in `duckdb-rs` compiles and links the DuckDB C++ source, allowing DuckDB to run in the same process as a Rust Server Core. It supports JSON, Parquet, and Arrow appenders and is strong at long-range token grouping and comparison analytics.

Reasons not to provisionally recommend a DuckDB-only canonical store:

- Its main focus is bulk processing, not many small transactions
- Same-row UPDATE/DELETE operations can conflict
- Late correction requires MERGE and transaction-retry design
- FTS indexes do not automatically track changes to their source table
- Extension pinning and a C++ toolchain are required

Stacks to compare:

```text
A: Rust + Rotel adapter + DuckDB canonical
B: Rust + Rotel adapter + SQLite canonical
                         └─ DuckDB/Parquet derived analytics
```

Prototype A as well, but adopt it only if it beats B under the same gates, including durable ACKs, late correction, live FTS, and migrations.

## Spike Gates

Hard gates required to make the Rust option equivalent to the Go baseline:

1. Native builds for macOS arm64/x64, Windows x64, and Linux x64
2. ACK after the SQLite/DuckDB commit
3. No loss after SIGKILL immediately following ACK; idempotent handling of retry duplicates
4. Preservation of raw request bytes and pre-decode provenance
5. Traces, logs, all metric types × gRPC/HTTP protobuf/JSON × gzip
6. Partial failures, malformed and oversized input, shutdown, and backpressure
7. Signed `.dmg`/`.msi`, single-container, and standalone strict core
8. Idle RSS, binary size, cold start, and p95/p99 ingestion/query latency
9. Pinned dependencies, API adapters, and upgrade compatibility tests
10. SBOM and Apache/MPL/AGPL obligations

## Consequences

Positive:

- The Rust option no longer requires a receiver built entirely from scratch, making toolchain integration with Tauri and storage realistic to compare.
- Vector, Quickwit, and OpenObserve can be reused according to their proper roles.
- Go is not fixed as the only possible OTLP ecosystem.

Negative:

- Agentmetry must supplement Rotel's production durability and platform support.
- Vendoring and forks add upstream tracking costs.
- Rust + DuckDB still requires a C++ toolchain and FFI.

## Primary Sources

Checked on: 2026-08-10.

Rotel:

- [README v0.2.5](https://github.com/rotel-dev/rotel/blob/v0.2.5/README.md)
- [Library modules](https://github.com/rotel-dev/rotel/blob/v0.2.5/src/lib.rs)
- [gRPC receiver](https://github.com/rotel-dev/rotel/blob/v0.2.5/src/receivers/otlp/otlp_grpc.rs)
- [HTTP receiver](https://github.com/rotel-dev/rotel/blob/v0.2.5/src/receivers/otlp/otlp_http.rs)
- [Release v0.2.5](https://github.com/rotel-dev/rotel/releases/tag/v0.2.5)
- [License](https://github.com/rotel-dev/rotel/blob/v0.2.5/LICENSE)

Vector:

- [OpenTelemetry source](https://vector.dev/docs/reference/configuration/sources/opentelemetry/)
- [Versioning](https://github.com/vectordotdev/vector/blob/v0.57.0/VERSIONING.md)
- [License](https://github.com/vectordotdev/vector/blob/v0.57.0/LICENSE)

Quickwit:

- [OpenTelemetry service](https://quickwit.io/docs/main-branch/log-management/otel-service)
- [Index configuration](https://quickwit.io/docs/main-branch/configuration/index-config)
- [Cluster sizing](https://quickwit.io/docs/main-branch/deployment/cluster-sizing)

OpenObserve:

- [Repository](https://github.com/openobserve/openobserve)
- [Release v0.92.0](https://github.com/openobserve/openobserve/releases/tag/v0.92.0)
- [License](https://github.com/openobserve/openobserve/blob/main/LICENSE)

DuckDB:

- [duckdb-rs](https://github.com/duckdb/duckdb-rs)
- [MERGE INTO](https://duckdb.org/docs/current/sql/statements/merge_into)
- [FTS](https://duckdb.org/docs/current/core_extensions/full_text_search)
