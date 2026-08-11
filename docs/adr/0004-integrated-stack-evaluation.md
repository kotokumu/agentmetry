# ADR 0004: Integrated Technology Stack Evaluation

- Status: Proposed
- Date: 2026-08-10
- Scope: Runtime × Ingestion × Storage × GUI × MCP × Packaging
- Evidence: ADR 0001–0003 and their primary sources

## Decision

The currently recommended stack is:

```text
Go Server Core
  OpenTelemetry Collector/pdata
  SQLite WAL canonical store
  optional future Parquet/DuckDB projection
  Web API / SSE / MCP Go SDK

TypeScript/Lit SPA
  stateless Web Components + functional state transitions
Thin Tauri desktop shell
```

Spike Rust + Rotel + SQLite as a strong challenger, but do not promote it to the production baseline until it resolves durable ACKs, raw-wire preservation, and support for all three operating systems.

Evaluate OpenObserve in the full-OSS PoC and SigNoz as an external/reference backend. Limit Vector and Quickwit to optional external gateway and search components rather than using them in the product core.

## Evaluation Criteria

| Criterion | Weight | Hard requirement |
|---|---:|---|
| OTLP correctness and durable ACK | 25 | All three signals, major transports, ACK after commit, retry/idempotency |
| `.dmg` / binary / single-container | 20 | macOS/Windows/Linux, with no unnecessary child processes in the core |
| Canonical data fit | 15 | Late correction, small transactions, graphs, tokens, FTS, migrations |
| Maintainability | 15 | Stable APIs, upgrade surface, fork size, test ownership |
| Resource/performance potential | 10 | Startup, RSS, ingestion/query; final decision based on measurements |
| Web UI/MCP delivery | 10 | Local Web UI, MCP HTTP/stdio, agent-specific extensions |
| License/distribution | 5 | Redistribution, notarization, SBOM/NOTICE |

Scores range from 1 (poor fit) to 5 (strong fit), normalized to 100 points. Unmeasured values are not precise estimates and are used only to prioritize spikes.

## Stack Comparison

| Stack | OTLP 25 | Package 20 | Data 15 | Maintain 15 | Resource 10 | UI/MCP 10 | License 5 | Total |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| A. Go + Collector/pdata + SQLite + TS/Tauri | 5 | 4 | 5 | 4 | 4 | 4 | 5 | **89** |
| B. Rust + vendored Rotel + SQLite + TS/Tauri | 3 | 4 | 5 | 3 | 5 | 4 | 5 | **78** |
| C. Rust + vendored Rotel + DuckDB + TS/Tauri | 3 | 4 | 3 | 3 | 5 | 4 | 5 | **72** |
| D. OpenObserve full backend + desktop wrapper | 4 | 3 | 3 | 4 | 3 | 5 | 2 | **71** |
| E. SigNoz + ClickHouse Embedded + desktop wrapper | 5 | 2 | 4 | 3 | 2 | 4 | 3 | **69** |

## A. Go + Collector/pdata + SQLite

Strengths:

- Maximizes reuse of the stable three-signal OTLP receiver and pdata.
- Makes it straightforward to design Agentmetry's pipeline around ACK-after-commit and raw capture.
- SQLite fits late correction, idempotent UPSERT, FTS, and migrations.
- Standalone and container deployments use one core process.
- The official MCP Go SDK is Tier 1.

Risks:

- A full Collector distribution may add RSS and binary overhead.
- A lightweight pdata approach expands ownership of the admission layer.
- A pinned SQLite build requires CGO and native OS CI.
- The Tauri shell and core create a Rust/TypeScript/Go build pipeline.

Decision:

**Production baseline.** Spike both an OCB/Collector configuration and a lightweight pdata configuration. If compatibility is equivalent, choose by resource SLOs.

## B. Rust + Rotel + SQLite

Strengths:

- Rotel provides reusable support for all three OTLP signals, gRPC/HTTP protobuf/JSON, gzip, batching, and retries.
- The toolchain can align with Tauri.
- Bundled SQLite and a single-process core.
- May outperform Go in resource use, startup, and binary size.

Current blockers:

- Rotel v0.2.5 ACKs after an in-memory enqueue, before the database commit.
- It does not preserve raw wire bytes.
- It is pre-1.0 and does not provide a stable, published library contract.
- Official Windows x64 and macOS x64 artifacts are missing.
- TLS, authentication, and partial-success behavior must be added.

Decision:

**Strong challenger.** Vendor Rotel at a fixed revision and spike an adapter that provides durable ACKs, raw capture, and platform support. Do not adopt it before it passes every hard gate.

## C. Rust + Rotel + DuckDB

Strengths:

- Uses bundled DuckDB in the same process from Rust.
- Strong token, latency, and model aggregation, Parquet support, and long-range scans.

Risks:

- Carries the same Rotel blockers as option B.
- Many small transactions and same-row corrections are not its primary target.
- FTS indexes do not update automatically when their table changes.
- Requires a C++ toolchain, FFI, and extension pinning.

Decision:

**Benchmark only.** Adopt a DuckDB-only canonical store only if it beats the SQLite stack in correctness and operability as well as performance. Do not choose it based only on group-by speed.

## D. OpenObserve Full Backend

Strengths:

- Single Rust server binary.
- All three OTLP signals, Web UI, SQL/PromQL/FTS, trace/agent/session views, and MCP.
- Shortest route to a full-OSS solution.

Risks:

- AGPL-3.0
- Record-level late correction and preservation of raw OTLP wire data are not guaranteed.
- Rejects old events by default.
- Agentmetry would own `.dmg`/`.msi` integration, the custom canonical UX, and upgrades.

Decision:

**No-fork PoC.** Measure requirements coverage with the same fixtures used for SigNoz. Do not create a large fork for Agentmetry-specific features.

## E. SigNoz + ClickHouse Embedded

Strengths:

- Mature three-signal support, queries, dashboards, and MCP.
- Official Darwin binaries.
- Telemetry-scale storage.

Risks:

- Multi-process architecture including the server, collector, ClickHouse, and Keeper.
- Native Windows and ClickHouse constraints.
- Startup, RSS, disk, and upgrade complexity.
- Collector AGPL obligations.

Decision:

**External/reference backend.** A full `.dmg` profile is implementable, but it will not be the strict-core baseline.

## Role of Other Rust OSS

| Component | Role |
|---|---|
| Vector | Optional external gateway when TLS, disk buffering, or end-to-end ACKs are required |
| Quickwit | Optional logs/traces FTS backend; not used for metrics or canonical corrections |
| OpenObserve | Full external backend; do not selectively embed internal crates |
| GreptimeDB | Optional external database; do not selectively embed internal OTLP/query/storage crates |
| DataFusion | Rust analytical engine challenger |
| Tantivy | Supplemental FTS candidate if SQLite FTS5 misses its SLO |

Follow [ADR 0005](0005-modular-oss-reuse.md) for the OpenObserve and GreptimeDB module-reuse decision. Internal workspace crate boundaries do not imply stable external APIs; version 1 will use both projects only across network/API boundaries.

## Hard Gates

Use the same fixtures and reference results for every custom stack.

1. Traces, logs, all metric types, gRPC/HTTP protobuf/JSON, and gzip
2. Handling of raw requests, provenance, and unknown fields
3. ACK after database commit, SIGKILL immediately after ACK, and retry duplicates
4. Late/out-of-order correction, 10,000+ agent DAGs, and token totals
5. macOS arm64/x64, Windows x64, Linux x64
6. `.dmg`/`.msi` signing, standalone operation, and a non-root single container
7. Web UI, SSE, MCP HTTP/stdio
8. cold start, idle RSS, artifact size, p95/p99 ingestion/query
9. Interrupted migrations, backup/restore, and retention
10. SBOM, NOTICE, and license obligations

## Promotion Rule

- Adopt A if it passes all hard gates and product SLOs.
- Promote B only if it passes every hard gate and has a clear product-artifact or maintainability advantage over A.
- Promote C only if it also satisfies B's gates and outperforms A and B without sacrificing canonical correctness or operability.
- Reconsider D or E as the product baseline only if its total cost, including fork size, distribution, and licensing, is lower than a custom stack.

## Deferred Extensibility

Version 1 will implement exactly one selected stack and one canonical backend. It will not include a runtime-selectable database, generic repository framework, dual writes, or storage plugins.

Preserve future migration paths through raw OTLP, canonical schema versions, stable IDs, a migration ledger, and reprojection fixtures.
