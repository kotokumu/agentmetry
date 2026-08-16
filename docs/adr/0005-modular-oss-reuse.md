# ADR 0005: Modular Reuse of OpenObserve and GreptimeDB

- Status: Superseded by the current native product architecture
- Date: 2026-08-11
- Research: luna + parallel subagents, using only official repositories, documentation, and crates

## Question

Can Agentmetry embed selected Rust modules from OpenObserve or GreptimeDB—such as their OTLP receiver, WAL, query, storage, or MCP—rather than using either project only as a complete server?

The following distinction is essential:

```text
crate separation in the source tree
        !=
crates.io publication + SemVer compatibility + embedded API + independent initialization
```

## Decision

| Candidate | Internal crates | Stable external modules | Receiver-only embed | Recommended boundary |
|---|---:|---:|---:|---|
| OpenObserve v0.92 | 40 workspace members / 45 packages | none | no | external full backend or AGPL full fork |
| GreptimeDB v1.1.4 | 71 crates | effectively none | no | optional external DB backend |
| Rotel v0.2.5 | smaller library-oriented codebase | pre-1.0, pin required | yes, with changes | Rust OTLP receiver candidate |
| Go Collector/pdata | official component/version policy | yes | yes | mature baseline |

Remove partial embedding of OpenObserve and GreptimeDB modules from the shortlist. Vendoring an entire workspace and starting it in the same process is technically possible, but that is full-product fork maintenance rather than reuse of stable modules.

## OpenObserve v0.92

### Packaging and API status

- The root `openobserve` package sets `publish=false`.
- The 36 main packages for the API, core, search, MCP, and related functionality also set `publish=false`.
- All workspace libraries use version `0.1.0`.
- Internal dependencies use `path = "src/..."` rather than registry and SemVer dependencies.
- Product versions do not correspond to library API versions.

Private modules include `openobserve-api-grpc/http/ingest/search`, `openobserve-core`, `openobserve-mcp`, `ingestion-common`, `db`, `schema`, and `search`.

### OTLP handler coupling

```text
API handler
  -> openobserve-core
  -> global config/auth/org/stream
  -> schema/db/infra/node
  -> ingester/WAL
```

The OTLP receiver is not an independent abstraction with sink injection. It depends on global configuration, organization and stream headers, metrics, token caches, and internal node routing. The gRPC server also registers search, query, internal ingestion, cluster services, and other fixed services; it has no `otlp-only` feature.

### WAL/query/MCP/UI

- The WAL crate is relatively small, but it is an unpublished version 0.1.0 AGPL component with a proprietary format and a dependency on global fsync configuration; it is not a canonical database.
- The ingester is tightly coupled to a global writer map, Arrow/Parquet, memtables, and WAL replay.
- Query and search are tightly coupled to DataFusion, Tantivy, object storage, and global infrastructure.
- MCP generates tools from the complete OpenObserve OpenAPI specification and loops back to the localhost REST API, so it cannot be reused independently.
- The UI is tightly coupled to the OpenObserve authentication, organization, stream, and search APIs.

Decision:

- Selective same-process embedding: reject.
- Full vendoring or forking of an exact tag: technically possible, but requires AGPL full-product maintenance.
- Unmodified external backend/API/MCP: viable for a PoC.

## GreptimeDB v1.1.4

### Packaging and API status

- The workspace contains 71 crates.
- The absence of `publish=false` does not imply a public API.
- Major crates are not officially published to crates.io as stable packages.
- Internal dependencies are unversioned path dependencies.
- There is no external repository, description, or SemVer contract for those crates.
- The project uses nightly Rust and Git-revision dependencies such as forked DataFusion and sqlparser.
- `Cargo.lock` contains approximately 1,357 packages.

### OTLP handler coupling

The official OTLP handler accepts all three signals, but supports only HTTP protobuf, rejects HTTP JSON, and does not provide a standard OTLP gRPC service.

```text
OTLP HTTP handler
  -> PipelineHandler
  -> QueryContext/auth/session
  -> stored pipeline
  -> catalog/table/schema
  -> frontend Instance
  -> insert/query Output
  -> storage
```

The `servers` crate depends on Arrow, DataFusion, the catalog, query, pipeline, MySQL/PostgreSQL protocols, and other components. It has no `otlp-only` feature.

### Standalone/WAL/query/UI/MCP

- Public types such as `InstanceCreator` make it possible to start the entire workspace in one process, but there is no embedded SDK or example.
- It owns process-global Tokio runtimes, logging, the Prometheus registry, and telemetry.
- It constructs the datanode, Mito/File engines, catalog, query, flow, and network servers together.
- WAL `LogStore` depends on Greptime Region/Entry types and the global runtime.
- Mito and query also depend on many internal services.
- The dashboard is a finished UI that embeds a separate artifact into the binary.
- MCP is a Python server in another repository and runs as a separate process that connects to a running GreptimeDB instance over MySQL/HTTP.

Decision:

- Partial embedding of OTLP, WAL, query, or storage: reject.
- Full workspace vendoring: technically possible, but not recommended.
- Child-process or remote backend: viable.

## Recommended Agentmetry Boundaries

### Self-owned Core

```text
Rotel adapter or Go OTel receiver
  -> Agentmetry normalization
  -> canonical commit
  -> ACK
  -> SQLite
  -> optional DuckDB/Parquet
  -> Agentmetry Web UI/MCP
```

### OpenObserve profile

```text
Agent telemetry -> OpenObserve full backend -> REST/MCP integration
```

### GreptimeDB profile

```text
Agentmetry Core
  -> SQLite canonical
  -> optional OTLP/SQL export -> GreptimeDB
```

## Consequences

- Do not treat a project as modularly reusable merely because it is a Rust monorepo.
- Version 1 will not have Git or path dependencies on internal OpenObserve or GreptimeDB crates.
- Use stable upstream APIs across network and API boundaries.
- If an internal module becomes necessary, reevaluate fork cost, licensing, and API tracking as a full-product decision.

## Primary Sources

OpenObserve:

- [v0.92 Cargo workspace](https://github.com/openobserve/openobserve/blob/v0.92.0/Cargo.toml)
- [gRPC API crate](https://github.com/openobserve/openobserve/blob/v0.92.0/src/api/grpc/Cargo.toml)
- [ingest API crate](https://github.com/openobserve/openobserve/blob/v0.92.0/src/api/ingest/Cargo.toml)
- [WAL](https://github.com/openobserve/openobserve/tree/v0.92.0/src/wal)
- [MCP](https://github.com/openobserve/openobserve/tree/v0.92.0/src/mcp)
- [v0.92 release](https://github.com/openobserve/openobserve/releases/tag/v0.92.0)
- [AGPL license](https://github.com/openobserve/openobserve/blob/v0.92.0/LICENSE)

GreptimeDB:

- [v1.1.4 workspace](https://github.com/GreptimeTeam/greptimedb/blob/v1.1.4/Cargo.toml)
- [OTLP handler](https://github.com/GreptimeTeam/greptimedb/blob/v1.1.4/src/servers/src/http/otlp.rs)
- [standalone command](https://github.com/GreptimeTeam/greptimedb/blob/v1.1.4/src/cmd/src/standalone.rs)
- [v1.1.4 release](https://github.com/GreptimeTeam/greptimedb/releases/tag/v1.1.4)
- [GreptimeDB MCP server](https://github.com/GreptimeTeam/greptimedb-mcp-server)
