# Agentmetry PoC

This proof of concept receives AI agent OTLP telemetry locally and exposes sessions, subagents, operations, and token usage through a Web UI and MCP.

## Architecture

- Single Go process: OTLP HTTP/gRPC, SQLite, Web API, MCP, and a static SPA
- SQLite WAL: immutable OTLP journal, canonical observations, and replaceable read models
- Embedded Atlas HCL: in-process declarative schema convergence with no migration CLI or child process
- Lit + TypeScript + Vite: dashboard built with Web Components
- Functional core / imperative shell: state transitions, selectors, and normalization are pure functions; Lit and I/O remain at the edges
- Source profiles: producer-specific event attributes are interpreted only at the ingestion boundary

## API Contract

The read API contract is protobuf-first. The schema is in
`proto/agentmetry/v1/agentmetry.proto`; Buf owns linting, generation, and
breaking-change checks. Generated Go and TypeScript clients are checked in so
normal builds do not need Buf or network access.

```sh
BUF_CACHE_DIR=/tmp/agentmetry-buf-cache buf lint
BUF_CACHE_DIR=/tmp/agentmetry-buf-cache buf build
BUF_CACHE_DIR=/tmp/agentmetry-buf-cache buf generate
```

Connect serves the generated query service to the Web client. MCP tools remain
explicit semantic adapters over the same application query service; RPCs are
not automatically exposed as tools.

The Web initial load is bounded: dashboard aggregates, one paged session-list
page, and one activity page for the selected session. The legacy JSON routes
under `/api/v1` remain for compatibility, but are not used by the Web client or
the new MCP tools.

## Run Locally

Requires Go 1.26 and Node.js.

```sh
cd web
npm ci
npm run build
cd ..
go run ./cmd/agentmetry
```

After startup:

- Dashboard / API / MCP: `http://127.0.0.1:17890`
- OTLP gRPC: `127.0.0.1:4317`
- OTLP HTTP: `http://127.0.0.1:4318`
- MCP Streamable HTTP: `http://127.0.0.1:17890/mcp`
- SQLite: `data/agentmetry.db`

## Desktop build and packaging

The desktop application uses Tauri 2 as a cross-platform native shell and bundles
the Go server as a sidecar. The shell chooses the platform application-data
directory and starts the same server binary used by the headless profile. The Go
sidecar is the only process that serves the embedded SPA; Tauri does not package
a second copy of `web/dist`.

Desktop build logic lives under `build/desktop/`. The root `package.json` exposes
the stable build entrypoints, while `src-tauri/` contains only the native shell,
Tauri configuration, and generated sidecar staging files.

Install the JavaScript dependencies and build the macOS package:

```sh
npm ci
npm --prefix web ci
npm run desktop:build:macos
```

The input-preparation step can be run independently:

```sh
npm run desktop:inputs
```

It builds `web/dist` and the target-specific Go sidecar under
`src-tauri/binaries/`. Tauri then bundles that sidecar into the native
application. The unsigned `.app` and `.dmg` artifacts are written under
`src-tauri/target/`. The first local milestone is intentionally unsigned;
Developer ID signing, notarization, and the protected SQLite unlock flow are
separate release steps.

For local development:

```sh
npm run desktop:dev
```

For native release packages, use the platform-specific entrypoint:

```sh
npm run desktop:build:macos
npm run desktop:build:windows
npm run desktop:build:linux
```

Each entrypoint uses the same `desktop:inputs` hook before Tauri bundles the
native installer. The CI matrix invokes these same named commands on native
macOS, Windows, and Linux runners.

Pushing a version tag such as `v0.1.0` starts the release workflow. It verifies
that the tag matches `src-tauri/tauri.conf.json`, builds the macOS DMG, Windows
NSIS installer, and Linux AppImage/deb packages, then publishes them to a GitHub
Release. The current release workflow publishes unsigned artifacts; signing and
notarization will be added when the platform credentials and protected storage
release gates are ready.

The desktop app is resident by default and starts the OTLP receivers with the
sidecar. Closing the window hides it to the system tray while OTLP reception
continues. The tray menu provides `Open Agentmetry`, `Hide Window`, and
`Quit Agentmetry`; quitting is the explicit action that stops the receivers and
the sidecar.

## Agent Configuration

### Claude Code

Persist the OTLP settings in Claude Code instead of exporting environment
variables for every shell. Use one of these files:

- User: `~/.claude/settings.json` on macOS/Linux or
  `%USERPROFILE%\.claude\settings.json` on Windows
- Project: `.claude/settings.json`
- Local project override: `.claude/settings.local.json`

```json
{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "env": {
    "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
    "OTEL_METRICS_EXPORTER": "otlp",
    "OTEL_LOGS_EXPORTER": "otlp",
    "OTEL_TRACES_EXPORTER": "otlp",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "grpc",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4317",
    "OTEL_LOG_USER_PROMPTS": "1",
    "OTEL_LOG_ASSISTANT_RESPONSES": "1",
    "OTEL_LOG_TOOL_DETAILS": "1",
    "OTEL_LOG_TOOL_CONTENT": "1"
  }
}
```

Restart Claude Code after changing the file. The content settings may capture
prompts, responses, file contents, command output, and other sensitive local
data. Agentmetry stores the accepted OTLP payload losslessly in the local
SQLite database.

Use `"OTEL_LOG_RAW_API_BODIES": "1"` only when full raw API bodies are also
required. They may contain the complete conversation history and add
substantial duplicate data.

To add authoritative plan-limit percentages, configure the same Agentmetry
executable as the status-line command. The command forwards the plan windows
to the local dashboard and prints a compact status line back to Claude Code:

```json
{
  "statusLine": {
    "type": "command",
    "command": "/absolute/path/to/agentmetry import-plan-usage --source=claude"
  }
}
```

This is optional enrichment. OTLP model-token observations continue to work
without it. Agentmetry never converts observed tokens into a subscription
percentage because the account limit and its weighting are not present in
OTLP.

### Codex

Add only the OTLP destination to the Codex configuration. No Agentmetry-specific hook or Codex modification is required.

```toml
[otel]
environment = "agentmetry-local"
log_user_prompt = true
exporter = { otlp-grpc = { endpoint = "http://127.0.0.1:4317" } }
trace_exporter = { otlp-grpc = { endpoint = "http://127.0.0.1:4317" } }
metrics_exporter = { otlp-grpc = { endpoint = "http://127.0.0.1:4317" } }
```

Set `log_user_prompt = false` if prompts must not be stored.

The standard build also contains a parser for the account rate-limit response
exposed by the Codex App Server. Account usage remains a separate input from
OTLP and is stored as authoritative plan-window snapshots rather than inferred
from token counts.

## Dashboard Data Loading

Session and token totals are calculated from every observation in the selected
time range. The activity table initially renders 100 rows and automatically
requests the next 100 when the user scrolls near its end. Long message and tool
content stays collapsed until explicitly expanded, so raw evidence can remain
stored without making the dashboard document unbounded.

## Docker

The same single process provides OTLP, the Web UI, API, MCP, and SQLite inside one container.

```sh
docker build -t agentmetry-poc .
docker run --rm -p 17890:17890 -p 4317:4317 -p 4318:4318 -v agentmetry-data:/data agentmetry-poc
```

## Verification

```sh
go test ./...
cd web && npm test -- --run && npm run build
```

`go test ./...` includes an in-process end-to-end test built with `httptest`. It sends OTLP protobuf to the production HTTP router, persists observations in a temporary SQLite database, and verifies the HTTP API, MCP tool, and embedded SPA without fixed ports or external processes.

## Storage and Schema Evolution

Every accepted OTLP export is committed to SQLite as canonical protobuf and JSON before it is acknowledged. Agentmetry also stores one source-neutral observation per span, log record, or metric, including its complete normalized payload and original attribute layers. The current `spans`, `logs`, and `metrics` tables are read models; the OTLP journal can rebuild improved projections later.

The desired SQLite schema lives in an HCL file embedded in the Go executable. At startup, the Atlas Go library inspects the local database, computes the diff, and applies safe additive changes in-process. Agentmetry automatically permits new tables, indexes, and columns that are nullable or have defaults. Destructive, rename, constraint, type, and table-rebuild changes stop startup until an explicit migration is supplied.

## Source Plugins

Producer semantics implement the public `sourceplugin.Plugin` interface. The standard distribution statically registers its first-party source plugins, preserving one cross-platform executable. Each plugin owns source detection and semantic aliases; OTLP admission, canonical derivation, storage, UI, and MCP contain no producer-specific field names.

To add a source in a custom build:

1. Implement `ID`, `Match`, and `Normalize` against `sourceplugin.Event`.
2. Keep all producer event names, attribute aliases, and fixtures inside that plugin package.
3. Add the plugin to the composition registry.

Runtime shared-library loading is intentionally unsupported. A future runtime extension format can use WASM or a versioned process protocol without exposing Go ABI assumptions.

## What OTLP Alone Reveals

Live ingestion has been verified with Codex 0.146.0. Agentmetry links parent and child `conversation.id` values through their shared trace ID and displays the model and token fields for each `response.completed` event. Codex exports collaboration-tool delegation messages in encrypted form, so Agentmetry shows the task name, subagent type, and outcome while explicitly marking the message body as encrypted. If Codex does not export response content or cost over OTLP, Agentmetry reports it as unavailable instead of inferring it.

See [docs/poc-spec.md](docs/poc-spec.md) for detailed boundaries and PoC acceptance criteria, and [docs/grand-architecture.md](docs/grand-architecture.md) for the long-term architecture.
