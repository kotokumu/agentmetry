# ADR 0002: Server Runtime and Language

- Status: Superseded by the current Go product architecture
- Date: 2026-08-10
- Research: luna, using only official primary sources

## Context

Agentmetry will deliver the same Server Core in these forms:

- GUI sidecar launched from a `.dmg` or `.msi`
- Standalone `agentmetry serve` executable
- Single Docker container

The core provides OTLP/gRPC, OTLP/HTTP, traces/logs/metrics, normalization, SQLite-family storage, Web UI/API, SSE, MCP Streamable HTTP, retention, and migrations. Domain behavior must not diverge between the GUI, binary, and container.

Go is not a requirement. Compare Rust, Go, and TypeScript runtimes based on the maturity of official implementations and the resulting product artifacts.

## Current Hypothesis, Not Yet a Decision

The provisional frontrunner separates responsibilities as follows:

```text
Go Server Core
  OTLP / normalization / storage / query / HTTP / MCP

TypeScript/Lit Web UI
  stateless Web Components + functional state transitions

Thin Tauri Rust/TS Shell
  window / tray / sidecar / installer / update only
```

This does not select Go. Based on [Rust Telemetry Ecosystem ADR 0003](0003-rust-telemetry-ecosystem.md), the Rust prototype will use pinned/vendored Rotel rather than build the entire receiver. Compare a Go Collector distribution, a lightweight Go pdata core, and Rust + Rotel adapter under the same conformance, resource, and packaging gates.

Use TypeScript for the Web UI. Build the Lit SPA with Vite and expose its reusable boundary as standard Custom Elements. Do not use Node, Bun, or Deno as the default Server Core, though they remain useful for MCP and UI tooling.

## UI Programming Model

Use Lit only as a rendering adapter. Do not place domain behavior or state transitions in `LitElement`. The default UI element is a stateless presentational Web Component.

```text
HTTP / SSE adapters
        ↓ messages
Pure update functions: (Model, Message) -> (Model, Effects)
        ↓ immutable view model
Lit Web Components: properties -> template
        ↓
DOM + typed CustomEvent intents
```

Rules:

- Represent domain and view models as readonly TypeScript data and discriminated unions.
- Implement filtering, selection, aggregation, timeline layout, and state transitions as pure functions from inputs to outputs.
- Components accept immutable properties and report requested changes upward through typed `CustomEvent` values. They do not own API calls, storage, or global singletons.
- Limit stateful coordination to one store at the SPA root, separating reducer/update functions from the effect interpreter.
- Isolate HTTP, SSE, clocks, clipboard, downloads, and the Tauri bridge at effect boundaries.
- Do not make the entire dashboard one monolithic Custom Element. Make time-range filters, KPI cards, trace waterfalls, agent graphs, token charts, event tables, and detail panels independent components with public Custom Element contracts.
- Pages and dashboards compose and connect those elements. Do not embed queries or state transitions in individual presentation components.
- Decide whether buttons, icons, and spacing primitives need custom elements based on design-system requirements. Do not expand the public API by default.
- Choose Shadow DOM or light DOM per component based on style isolation, accessibility, and table/graph library integration. Do not apply either mechanically to every element.

Representative public elements are `am-time-range-filter`, `am-kpi-card`, `am-run-list`, `am-trace-waterfall`, `am-agent-graph`, `am-token-chart`, `am-event-table`, and `am-event-detail`. Do not require a React/Next.js server; serve the same static build artifact from Tauri, standalone, and container deployments.

Behavior tests verify pure update functions, visible output from properties, emitted typed intents, and keyboard/accessibility contracts rather than internal DOM structure. End-to-end tests confirm that Tauri and Web hosts produce the same results for the same fixture.

## Candidate Comparison

| Candidate | OTLP receive | MCP | Native storage | Packaging | Position |
|---|---|---|---|---|---|
| Go | Collector `otlpreceiver` and `pdata` are Stable for all three signals; OCB can build a custom single binary | official Tier 1 | SQLite uses CGO; DuckDB binding is WIP/CGO | `embed.FS`, cross-platform sidecar/container | provisional frontrunner |
| Rust | Rotel supports reusable three-signal OTLP, but durable ACKs, raw wire capture, and platform support need work | official Tier 2 | bundled rusqlite, duckdb-rs | same toolchain as Tauri, native binary | strong spike challenger |
| TypeScript/Node | OTel JS is a producer SDK; receiver/pdata must be built | official Tier 1 | `node:sqlite` is a release candidate; native addon matrix | SEA is in active development | UI yes, core no |
| TypeScript/Bun | receiver must be built; gRPC compatibility requires validation | TS SDK Tier 1 | built-in SQLite | can compile | experiment only |
| TypeScript/Deno | receiver must be built | TS SDK Tier 1 | addon/FFI | can compile | core no |

## Why Go Is the Provisional Frontrunner

- The OpenTelemetry Collector OTLP receiver is Stable for gRPC/HTTP and traces/metrics/logs.
- `pdata` provides stable models for all three signals.
- OpenTelemetry Collector Builder can produce a distribution containing the OTLP receiver and Agentmetry components.
- The official MCP Go SDK is Tier 1 and supports stdio and Streamable HTTP.
- Web assets can be embedded at compile time and shared by standalone and container artifacts.
- Tauri can bundle sidecars written in any language per target triple, so the core does not need to use Rust.

Risks:

- SQLite/DuckDB introduce C/C++ toolchains and native CI.
- Collector modules must be pinned as a library/distribution and tracked through Go and module upgrades.
- Embedding the full Collector service may add binary, RSS, and lifecycle overhead.

## Why Rust Remains a Challenger

Advantages:

- Shared toolchain with Tauri
- Bundled SQLite and a relatively mature DuckDB binding
- Native binary, resource control, and memory safety
- Rotel reuse of OTLP gRPC/HTTP protobuf/JSON, all three signals, gzip, batching, and retry

Costs:

- Rotel is a pre-1.0 source dependency without a stable library contract, requiring adapter and vendor maintenance.
- Current Rotel ACKs on in-memory enqueue and must be changed to ACK after a durable journal commit.
- Raw wire bytes, receiver TLS/authentication, and Windows/macOS Intel builds must be added.
- The official MCP SDK is Tier 2.
- DuckDB still requires C++ FFI and a cross compiler.
- Rust's native ABI cannot serve as a plugin contract.

Do not choose Rust based on abstract performance expectations. Adopt it only if the Rotel adapter passes durability, platform, and conformance gates and produces a better product artifact than the Go prototype.

## Why TypeScript Is Not the Default Core

TypeScript is strong for the UI and MCP ecosystem. The following issues remain for the Server Core:

- No Collector receiver/pdata; three-signal OTLP reception must be built
- Stability of Node SEA and `node:sqlite`
- Native addon artifacts, extraction, and signing per platform
- Bundle and RSS cost of the V8/runtime
- gRPC/Node compatibility and database addon validation for Bun and Deno

Return TypeScript to the core shortlist only if it demonstrates a product advantage that outweighs these costs.

## Go Prototype Alternatives

### A. Custom Collector Distribution

Use OCB to include the following in one binary:

```text
OTLP receiver
memory/batch/limits
Agentmetry normalizer/exporter
Web/API/MCP extension
storage adapter
```

This maximizes protocol correctness and lifecycle reuse while measuring Collector framework overhead.

### B. Lightweight pdata Core

Use `pdata` and official OTLP requests/protobuf with a thin gRPC/HTTP admission layer. This may reduce resource footprint, but contract tests must define ownership of TLS, compression, JSON/protobuf, limits, partial success, and backpressure.

Do not use only a generated protobuf receiver in production.

## Process and MCP Contract

- `agentmetry serve` provides Streamable HTTP MCP and targets one PID 1 process in standalone/container deployments.
- A `.dmg` may use two processes: the GUI shell and Server Core sidecar.
- Clients requiring stdio start the same artifact as a separate `agentmetry mcp --stdio` process, which forwards to the existing local HTTP core.
- The strict single-process profile enables only Streamable HTTP MCP.
- Reserve stdio stdout for JSON-RPC and send diagnostics to stderr.

## Extension Boundary

Do not use Go `plugin`, Rust native ABI, or Node native modules as Agentmetry's public plugin ABI.

Extensions use versioned MCP/HTTP/stdio process boundaries. If necessary, evaluate a WASM sandbox in a separate future ADR. Keep the canonical schema language-neutral and preserve raw OTLP/provenance and the normalization version.

## Spike Gate

Compare these prototypes:

1. Go Custom Collector distribution
2. Go lightweight pdata Core
3. Rust + pinned Rotel adapter

### OTLP Conformance

- gRPC, HTTP protobuf, and HTTP JSON
- Traces, logs, and metrics
- gzip, TLS, headers, and cancellation
- Resource/Scope, AnyValue, events, and links
- Exponential histogram, exemplars, and temporality
- Partial success, invalid/oversized payloads, and backpressure
- Decode differential against the official Collector

### MCP

- Official conformance suite
- stdio and Streamable HTTP
- Cancellation, pagination, and large-result limits
- Record applicable SDK tier

### Resource and Packaging

- Idle RSS, cold start, and binary/app/installer size
- Sustained and burst OTLP, including p95/p99
- Tail latency under concurrent queries, UI SSE, and MCP
- Native CI for macOS arm64/x64, Windows x64, and Linux x64
- `.dmg` code signing/notarization, Windows signing, and non-root container PID 1
- Offline first launch, sidecar hash/permissions, and shutdown/drain

### Native DB Boundary

- Pinned SQLite amalgamation/CGO or bundled link
- WAL, FTS5, JSON, and online backup
- Crash loops and interrupted migrations
- Treat breaking changes and API maturity in a future DuckDB binding as release gates

### Decision Rule

1. OTLP/MCP correctness, crash safety, and three-OS packaging are hard gates.
2. If multiple candidates pass, choose the option with fewer maintenance responsibilities when all remain within resource SLOs.
3. If Go meets its SLOs, select it for Collector/pdata reuse.
4. Select Rust only if Go misses its SLOs, Rust is materially better, and receiver maintenance cost is acceptable.

## Consequences

Positive:

- The decision is based on OTLP compatibility and product artifacts rather than language preference.
- UI, core, and shell responsibilities remain separate, allowing a future core-runtime replacement.
- GUI, binary, and container contracts remain consistent.

Negative:

- Building three prototypes has an initial cost.
- The UI, core, and shell span multiple languages, increasing build-pipeline complexity.
- Even Go requires native toolchain CI because of SQLite.

## Primary Sources

Checked on: 2026-08-10.

OpenTelemetry / Go:

- [OTLP receiver](https://github.com/open-telemetry/opentelemetry-collector/blob/main/receiver/otlpreceiver/README.md)
- [pdata](https://github.com/open-telemetry/opentelemetry-collector/tree/main/pdata)
- [Collector Builder](https://opentelemetry.io/docs/collector/extend/ocb/)
- [Go embed](https://pkg.go.dev/embed)
- [Go SQLite](https://github.com/mattn/go-sqlite3)
- [DuckDB Go bindings](https://github.com/duckdb/duckdb-go-bindings)

Rust:

- [OTel protobuf](https://docs.rs/opentelemetry-proto/latest/opentelemetry_proto/)
- [OTel Rust status](https://opentelemetry.io/docs/languages/rust/)
- [MCP Rust SDK](https://github.com/modelcontextprotocol/rust-sdk)
- [rusqlite](https://github.com/rusqlite/rusqlite)
- [duckdb-rs](https://github.com/duckdb/duckdb-rs)
- [Rust platform support](https://doc.rust-lang.org/rustc/platform-support.html)

MCP / TypeScript:

- [MCP SDK tiers](https://modelcontextprotocol.io/community/sdk-tiers)
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [MCP TypeScript SDK](https://github.com/modelcontextprotocol/typescript-sdk)
- [OTel JavaScript status](https://opentelemetry.io/docs/languages/js/)
- [Node SQLite](https://nodejs.org/api/sqlite.html)
- [Node SEA](https://nodejs.org/api/single-executable-applications.html)
- [Bun executables](https://bun.com/docs/bundler/executables)
- [Deno compile](https://docs.deno.com/runtime/reference/cli/compile/)

Desktop:

- [Tauri sidecars](https://v2.tauri.app/develop/sidecar/)
- [Tauri distribution](https://v2.tauri.app/distribute/)
- [Windows installers](https://v2.tauri.app/distribute/windows-installer/)
