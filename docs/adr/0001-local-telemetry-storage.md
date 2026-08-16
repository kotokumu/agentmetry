# ADR 0001: Local Telemetry Storage

- Status: Superseded by ADR 0006 and the current product architecture
- Date: 2026-08-10
- Decision owner: Agentmetry architecture
- Research: luna, using only official primary sources

## Context

Agentmetry will deliver the same Server Core in three forms:

1. A GUI app installed from a macOS `.dmg` or Windows installer
2. A standalone `agentmetry serve` binary
3. A single Docker container

Every form provides OTLP traces/logs/metrics, the Web UI, MCP, retention, and analysis. The standard profile must run on macOS arm64/x64, Windows x64, and Linux x64 without an external database or another service.

Primary workloads:

- Append OTLP batches, retry duplicates, and upsert late or out-of-order data
- High-cardinality attributes and raw JSON/provenance
- Point lookup of run/turn/agent/activity/message graphs
- OLAP aggregation by time range, token, latency, error, model, and agent
- Optional full-text search of captured content
- Retention, backup, migrations, and crash recovery
- Concurrent UI/MCP reads and ingestion

## Current Hypothesis, Not Yet a Decision

### MVP provisional frontrunner

**The current shortlist provisionally favors linking SQLite WAL directly into the Server Core. This is not an adoption decision.** Do not freeze the production schema until the full storage-category longlist has been researched, requirements-based exclusions are complete, and candidates pass hard gates using the same fixture benchmark. If adopted, production builds will compile the official amalgamation with pinned versions and compile options rather than depend on the operating system's SQLite.

Recommended order:

1. **SQLite WAL:** MVP default
2. **SQLite canonical journal + immutable Parquet/DuckDB:** scale-up path
3. **DuckDB standalone:** benchmark challenger
4. **ClickHouse server:** optional profile for SigNoz compatibility or very large local datasets
5. **clickhouse-local:** excluded as an application datastore

SQLite is the provisional recommendation because of its overall fit for mutable late data, transactional idempotent upserts, live FTS, crash recovery, in-process distribution across three operating systems, and single-binary/container/process contracts—not merely its analytical performance.

Distinguish these distribution requirements:

- `1 user-visible command/container`: may permit internal child processes.
- `strict single-process Server Core`: OTLP, Web UI, MCP, and storage share one process with no database child.

The default profile satisfies the latter. Desktop may use two processes for the GUI shell and Server Core sidecar, but the Server Core has no database child.

## Candidate Comparison

### Longlist Funnel

| Classification | Candidates | Requirement-based result |
|---|---|---|
| Direct shortlist | SQLite | Meets strict single-process, mutable canonical data, three-OS, FTS/graph/transaction requirements |
| Derived analytics | DuckDB + Parquet | Rebuildable long-range OLAP/export layer derived from SQLite |
| Optional external backend | ClickHouse, GreptimeDB | Large-scale/remote profiles; not bundled in the default desktop canonical profile |
| Research: query/lake | DataFusion + Arrow/Parquet, Lance/LanceDB | Future analysis candidates rather than transactional canonical stores |
| Research: journal/KV | RocksDB, Pebble, redb, LMDB | Can implement a write journal but require custom SQL/FTS/graph/OLAP/schema layers |
| Initial exclusion | libSQL, SurrealDB, PGlite, chDB, PostgreSQL/TimescaleDB, VictoriaMetrics/Logs, InfluxDB 3, QuestDB | Do not fit the default local canonical requirements for the reasons below |

### Longlist Findings

| Candidate | Useful capability | Why not the default canonical store |
|---|---|---|
| GreptimeDB | Three-OS standalone, all three OTLP signals over HTTP, SQL, JSON attributes, FTS, TTL | Traces are experimental, ExponentialHistogram is unsupported, and some metric attributes are lost, preventing use as a lossless primary store |
| DataFusion | Embeddable Arrow query engine | Not a database providing durable transactions, WAL, upserts, and migrations |
| Lance/LanceDB | Columnar datasets, search, bulk merge/upsert | Fragment rewrites, garbage collection, and compaction make it excessive for a journal |
| RocksDB | Write-heavy LSM, compaction, TTL | Adds C++/FFI while requiring custom SQL, graph, FTS, OLAP, and schema layers |
| Pebble | Production Go LSM | Intentionally omits transactions, column families, and related features, requiring a complete custom query layer |
| redb | Pure Rust, ACID, crash-safe, stable format | Provides only KV primitives and couples the runtime to Rust |
| LMDB | Durable mmap B+tree | Requires custom SQL, FTS, OLAP, schema, and binding operations without an advantage over SQLite |
| PostgreSQL/TimescaleDB | JSONB, UPSERT, FTS, recursive SQL, migrations, retention | Server child process and operations conflict with the strict desktop default |
| VictoriaMetrics/Logs | Mature metrics and log engines | Separate engines per signal and immature traces prevent one canonical/query surface |
| InfluxDB 3 Core | Arrow/DataFusion/Parquet, WAL | Server architecture still requires separate agent graph, log FTS, and span correction implementations |
| QuestDB | Out-of-order data, WAL, time-series SQL | JVM/server, mmap/file-descriptor footprint, and poor fit for variable OTLP graphs |
| libSQL | SQLite compatibility and replication | Replication/vector features add little locally, and embedded replicas are moving toward legacy status |
| SurrealDB | Graph/document/FTS/embedded | Core BSL 1.1, local-engine maturity/FFI, and weak fit for OLAP/OTLP metrics |
| PGlite | Local Postgres WASM | Single-user/connection constraints and an expanded WASM runtime/durability surface |
| chDB | In-process ClickHouse | No Windows support, primarily macOS/Linux, and not a substitute for the SigNoz server schema |

This funnel does not ignore viable products. It evaluates canonical stores, derived query engines, external scale backends, and journal primitives as distinct responsibilities.

| Candidate | Strength | Critical constraint | Position |
|---|---|---|---|
| SQLite WAL | Directly linked official amalgamation, three OSes, transactional UPSERT, WAL, FTS5, recursive CTEs, backup API | Single writer, large scans/group-bys, no native TTL | MVP provisional |
| DuckDB | In-process static/shared link, columnar OLAP, Parquet, group-by/scan, MERGE INTO | Live FTS does not update automatically; small upserts; extension/version/toolchain burden | benchmark challenger |
| SQLite + Parquet/DuckDB | Separates a hot mutable journal from cold OLAP | Custom compaction, manifest, late-cold-update, and backup consistency work | scale-up only |
| ClickHouse server | Batch ingestion, OLAP, TTL, JSON, FTS, backup | Separate process, no native Windows, limited macOS support, merge-based deduplication | optional full profile |
| clickhouse-local | File/Parquet ad hoc processing | Not officially intended for application serving | rejected |

## ClickHouse Assessment

ClickHouse was not removed from consideration. The following facts were confirmed:

- Plain `MergeTree`, a non-replicated single node, and single-block INSERT do not require Keeper.
- `ReplacingMergeTree` deduplication occurs during background merges; immediate correctness before a merge requires `FINAL`, `argMax`, or similar logic.
- Mutations and updates cost more than in a row-oriented transactional store.
- ClickHouse distributes official macOS x86_64/arm64 server binaries, but macOS is an Additional Platform with limited support.
- Native Windows is unsupported, and requiring WSL2 conflicts with the native `.msi` requirement.
- `clickhouse-local` uses the same binary but is officially intended for file processing, development, and testing; the server is recommended for a persistent application datastore.
- The SigNoz schema has a non-replicated mode but is tightly coupled to Collector and migrator versions. Do not reuse it as the canonical schema outside the SigNoz profile.

Therefore, ClickHouse is assigned to an optional multi-process profile because it does not fit the strict single-process desktop/server packaging contract, not because its performance is insufficient. Do not claim that `clickhouse-local` or chDB replaces a SigNoz-compatible server store.

## Provisional SQLite Design

```text
one database file
  ingest_batches       raw protobuf / hash / signal / transform version
  resources / scopes
  spans / span_events / span_links / logs
  metric_descriptors
  gauge_points / sum_points
  histogram_points / exponential_histogram_points / summary_points
  exemplars
  attributes           typed AnyValue EAV
  runs / turns / agents / tasks / relations
  messages / artifacts / findings
  promoted_columns     service/model/tool/agent/run/status/token
  content_fts          opt-in only
  schema_migrations
```

Rules:

- Set `journal_mode=WAL`.
- Use `synchronous=FULL` in the durable profile.
- Aggregate ingestion through a dedicated single-writer queue and transact per OTLP batch.
- UI/MCP readers use short read transactions and cursor pagination.
- Raw arrivals are append-only; canonical projections use version-aware UPSERT.
- Represent missing tokens as NULL/observation state, never zero.
- Canonical query fields use typed columns and B-tree indexes.
- Typed attributes are authoritative and preserve OTLP `AnyValue` bytes, int64, arrays, and kvlists.
- Also retain vendor attributes as JSON for display/debugging; promote only frequent keys to generated/expression indexes or typed columns.
- Synchronize only opted-in content into FTS5 after redaction, in the same transaction.
- Implement retention with time-indexed batch DELETE plus checkpoint/incremental vacuum.
- Use the Online Backup API or `VACUUM INTO`; never copy only a live `.db` file.
- Use numbered transactional migrations; perform complex changes as new table → copy → rename.
- Separate event time from ingestion order and record `first_seen_at`, `last_seen_at`, and `revision`.
- Identify spans by `(source, trace_id, span_id)` and metric points by instrument, attribute set, start, time, and related fields.
- Append logs without a universal ID by default; deduplicate only when the producer supplies an ID.
- Materialize critical paths in the application after run closure or a quiet period, and invalidate only the affected trace when a late span arrives.

## Scale-up Path

Only if SQLite misses volume/query SLOs, compact closed time partitions into immutable Parquet and query across them with DuckDB.

```text
SQLite
  hot mutable data
  raw/canonical journal
  segment manifest
  late delta
       |
       +--> immutable Parquet segments --> DuckDB analytical reads
```

Because a SQLite transaction and filesystem rename cannot form one atomic transaction, implement the manifest as a `writing → published` state machine. Use fault injection to test temporary writes, fsync, atomic rename, manifest commit, and orphan cleanup.

Do not start with two storage layers. Accept compaction, manifest, and backup complexity only after measurements show SQLite alone misses its SLOs.

Make DuckDB/Parquet derived state deletable and rebuildable, and prohibit canonical dual writes. Update it only from SQLite's durable outbox/checkpoint. Disable runtime extension download/autoload and pin every required extension version.

## Benchmark Gate

### Dataset

- Replay sanitized Claude Code and Codex fixtures
- 100k / 1M / 10M activities
- Mixed traces/logs/metrics
- Duplicate rates of 0/1/10/30%
- Late-arrival rates of 0/1/10/30%
- Run graphs with 100 / 1k / 10k nodes
- Content off/on and high-cardinality attributes

### Ingest

- OTLP batches of 1 / 64 / 512 / 4096
- Steady and burst traffic
- Concurrent UI/MCP reads
- Throughput, commit p50/p95/p99, CPU, RSS, and SSD writes

### Queries

1. Trace/run point lookup
2. Run timeline and parent/child graph
3. Token sum by type/model/agent/run
4. Latency/error/tool p95 over 15m/24h/30d
5. High-cardinality exact/range filters
6. Artifact overlap and coordination risk
7. Critical-path DAG input extraction
8. Optional full-text search

Measure warm/cold p50/p95, correctness, and temporary memory.

### Maintenance and Faults

- Tail latency during checkpoints, retention, and vacuum/compaction
- Forced termination during every phase of ingestion commit, migration, backup, and retention
- Reopen/recovery time
- Zero ACKed-row loss in the FULL durability profile
- Zero duplicates/corruption
- Backup restore and N-1 → N migration

### Packaging

- macOS arm64/x64, Windows x64, and Linux x64
- `.dmg`, installer, standalone binary, and single container
- Bundle delta, cold start, and idle RSS/CPU
- Code signing/notarization and native runtime dependencies

### Hard Gates

1. Native packaging works on all three operating systems.
2. The strict-profile Server Core process tree has no database child.
3. A crash after durable ACK causes no data loss or corruption.
4. Tokens, deduplication, and critical-path input match the reference implementation.
5. The candidate meets product-owner-defined bundle, RSS, query, and ingestion SLOs.

If SQLite meets its SLOs, adopt it for simplicity. Otherwise, proceed to SQLite + Parquet/DuckDB. Reconsider DuckDB alone as the default only if it meets mutable/live-query constraints and materially outperforms SQLite.

## Consequences

### Deliberately deferred extensibility

Version 1 will implement one selected canonical backend and optimize for that engine. It will not implement multiple database adapters, runtime-selectable storage plugins, a common SQL DSL, or canonical dual writes.

Preserve future migration through raw OTLP/provenance, canonical schema versions, stable identities, a migration ledger, and reprojection tests. The storage boundary is internal and prevents SQL/schema leakage to consumers; it is not a generic plugin framework.

Positive:

- GUI, binary, and container deployments share the same in-process storage contract.
- Late data, idempotency, content search, and metadata updates remain transactional.
- ClickHouse/SigNoz remain optional backends, preserving a future scale path.

Negative:

- SQLite may hit limits early for large-scale OLAP.
- The application manages TTL, attribute promotion, vacuum, and rollups.
- Scale-up adds segment manifests and query federation.

## Primary Sources

Checked on: 2026-08-10.

SQLite:

- [WAL](https://sqlite.org/wal.html)
- [Isolation](https://sqlite.org/isolation.html)
- [UPSERT](https://sqlite.org/lang_upsert.html)
- [JSON](https://sqlite.org/json1.html)
- [Expression indexes](https://sqlite.org/expridx.html)
- [Generated columns](https://sqlite.org/gencol.html)
- [FTS5](https://sqlite.org/fts5.html)
- [Backup API](https://sqlite.org/backup.html)
- [ALTER TABLE](https://sqlite.org/lang_altertable.html)
- [Downloads](https://sqlite.org/download.html)
- [Amalgamation](https://www.sqlite.org/amalgamation.html)

DuckDB / Parquet:

- [Concurrency](https://duckdb.org/docs/stable/connect/concurrency)
- [MERGE INTO](https://duckdb.org/docs/stable/sql/statements/merge_into)
- [Indexes](https://duckdb.org/docs/stable/sql/indexes)
- [Full-text search](https://duckdb.org/docs/lts/core_extensions/full_text_search)
- [JSON](https://duckdb.org/docs/stable/data/json/overview)
- [Parquet](https://duckdb.org/docs/stable/data/parquet/overview)
- [Partitioned writes](https://duckdb.org/docs/stable/data/partitioning/partitioned_writes)
- [Storage](https://duckdb.org/docs/stable/internals/storage)
- [Install](https://duckdb.org/install/)

ClickHouse / SigNoz:

- [clickhouse-local](https://clickhouse.com/docs/concepts/features/tools-and-utilities/clickhouse-local)
- [Transactions](https://clickhouse.com/docs/concepts/features/operations/insert/transactions)
- [Supported platforms](https://clickhouse.com/support/platforms)
- [JSON](https://clickhouse.com/docs/reference/data-types/newjson)
- [TTL](https://clickhouse.com/docs/concepts/features/operations/delete/ttl)
- [Backup/restore](https://clickhouse.com/docs/concepts/features/backup-restore/overview)
- [SigNoz architecture](https://signoz.io/docs/architecture/)
- [SigNoz non-replicated configuration](https://signoz.io/docs/manage/administrator-guide/clickhouse/distributed-clickhouse/kubernetes/)
- [SigNoz Collector license](https://github.com/SigNoz/signoz-otel-collector/blob/main/LICENSE)

Additional landscape:

- [DataFusion](https://datafusion.apache.org/user-guide/introduction.html)
- [Lance read/write](https://lancedb.github.io/lance/introduction/read_and_write.html)
- [RocksDB compaction](https://github.com/facebook/rocksdb/wiki/Compaction)
- [Pebble](https://github.com/cockroachdb/pebble)
- [redb](https://github.com/cberner/redb)
- [GreptimeDB standalone](https://docs.greptime.com/getting-started/installation/greptimedb-standalone/)
- [GreptimeDB OpenTelemetry](https://docs.greptime.com/user-guide/ingest-data/for-observability/opentelemetry/)
- [PostgreSQL license](https://www.postgresql.org/about/licence/)
- [Timescale editions](https://docs.timescale.com/about/latest/timescaledb-editions/)
- [VictoriaMetrics deployment](https://docs.victoriametrics.com/victoriametrics-cloud/deployments/single-or-cluster/)
- [InfluxDB 3 durability](https://docs.influxdata.com/influxdb3/core/reference/internals/durability/)
- [QuestDB out-of-order data](https://questdb.com/docs/concepts/out-of-order-data/)
- [libSQL](https://docs.turso.tech/libsql)
- [SurrealDB storage engines](https://surrealdb.com/docs/build/embedding/storage-engines)
- [PGlite](https://github.com/electric-sql/pglite)
- [chDB installation](https://chdb.readthedocs.io/en/latest/installation.html)
