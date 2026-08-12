# Agentmetry

Local observability for AI coding agents.

Agentmetry receives OpenTelemetry (OTLP) data from Claude Code and Codex, then
shows sessions, subagents, activities, token usage, and costs in a local Web UI.
It also exposes the same data through HTTP and MCP.

> **Early development / PoC**
>
> APIs, storage schema, and source integrations may change. Feedback and issue
> reports are welcome.

## Highlights

- One local process with OTLP HTTP/gRPC, SQLite, Web UI, HTTP API, and MCP
- Claude Code and Codex source profiles
- Session, subagent, model, tool, token, and cost views
- Lossless local telemetry journal with canonical query projections
- No external database or hosted service required after installation
- Optional Tauri desktop packaging for macOS, Windows, and Linux

## Quick start

Requirements: Go 1.26, Node.js 24, and npm.

```sh
git clone https://github.com/theoden9014/agentmetry.git
cd agentmetry
make build
./bin/agentmetry
```

Open <http://127.0.0.1:17890>.

The server listens on:

| Service | Address |
| --- | --- |
| Web UI, HTTP API, MCP | `127.0.0.1:17890` |
| OTLP gRPC | `127.0.0.1:4317` |
| OTLP HTTP | `http://127.0.0.1:4318` |
| SQLite database | `data/agentmetry.db` |

The Web UI is built into the Go binary. `make build` builds the frontend first,
then builds the backend with the generated assets embedded.

## MCP for agent self-analysis

The MCP endpoint is read-only and stateless. An AI agent should call
`get_agent_context` first to discover available data, privacy rules, and
required identifiers. It must then provide the source-qualified `source` and
`runId` to the run, timeline, token-usage, and analysis tools. The server never
assumes that the latest run belongs to the caller.

Analysis results report observed evidence, projection completeness, and source
coverage separately. Missing token values remain `null`; activity bodies are
omitted unless `includeContent` is explicitly set. OTLP-only data does not
guarantee task outcomes, git diffs, test results, or artifact conflicts, so
those are reported as unavailable rather than inferred.

## Connect your agents

### Claude Code

Add the following environment settings to Claude Code's settings file, for
example `~/.claude/settings.json`:

```json
{
  "env": {
    "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
    "OTEL_LOGS_EXPORTER": "otlp",
    "OTEL_TRACES_EXPORTER": "otlp",
    "OTEL_METRICS_EXPORTER": "otlp",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "grpc",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4317",
    "OTEL_LOG_USER_PROMPTS": "1",
    "OTEL_LOG_ASSISTANT_RESPONSES": "1",
    "OTEL_LOG_TOOL_DETAILS": "1"
  }
}
```

Restart Claude Code after changing the settings.

### Codex

Add the local OTLP destination to Codex's configuration:

```toml
[otel]
environment = "agentmetry-local"
log_user_prompt = true
exporter = { otlp-grpc = { endpoint = "http://127.0.0.1:4317" } }
trace_exporter = { otlp-grpc = { endpoint = "http://127.0.0.1:4317" } }
metrics_exporter = { otlp-grpc = { endpoint = "http://127.0.0.1:4317" } }
```

Set `log_user_prompt = false` if prompts must not be stored.

Agentmetry stores accepted telemetry locally. Prompts, responses, tool details,
and command output may contain sensitive information; enable content logging
only when it is appropriate for your environment.

## Docker

```sh
docker build -t agentmetry .
docker run --rm \
  -p 17890:17890 -p 4317:4317 -p 4318:4318 \
  -v agentmetry-data:/data agentmetry
```

## Development

```sh
# Backend unit and fixture tests
go test ./...

# Web tests
npm --prefix web ci
npm --prefix web test -- --run

# Backend integration tests, including the embedded Web UI
npm --prefix web run build
go test -tags=integration ./...
```

Provider contract tests make paid API requests and are opt-in:

```sh
ANTHROPIC_API_KEY=... OPENAI_API_KEY=... \
  go test -tags=providerlive ./internal/source/claude ./internal/source/codex
```

The real-agent MCP integration test is also opt-in because it invokes local
Claude and Codex SDKs and may consume subscription or API quota:

```sh
npm --prefix evals/agentmetry ci
npm --prefix evals/agentmetry run e2e
```

It starts an isolated Agentmetry instance, runs both SDKs through Promptfoo,
requires each agent to call `get_agent_context` and
`get_source_capabilities`, checks that each run is observed under the expected
source ID, and performs source-qualified MCP analysis from the runner. The
runner verifies MCP initialization and tool discovery before making calls, and
the deterministic fixture integration test repeats the lifecycle through the
official MCP client while asserting semantic results. Local SDK authentication
is reused; no credentials are written to the repository or broadly inherited
by the Codex subprocess.

See [the token accounting test strategy](docs/adr/0013-token-accounting-test-strategy.md)
for the unit, fixture, and provider-live test boundaries.

## Documentation

- [PoC scope and acceptance criteria](docs/poc-spec.md)
- [Architecture overview](docs/grand-architecture.md)
- [Trace explorer design](docs/design/trace-explorer.md)
- [API contract](docs/design/api-proto-contract.md)
- [CI and build validation](docs/adr/0012-ci-build-validation.md)
- [Desktop build architecture](docs/adr/0010-desktop-build-architecture.md)

## Desktop builds

The Tauri desktop shell uses the same Go server and embedded Web UI. Build
platform packages with:

```sh
npm ci
npm --prefix web ci
npm run desktop:build:macos   # or :windows / :linux
```

See the [desktop build architecture](docs/adr/0010-desktop-build-architecture.md)
for packaging details.

Tagged desktop releases also publish signed updater bundles. Installed desktop
apps check the latest GitHub Release at startup, keep the local collector
running while the update downloads and verifies, then restart to apply it.
Maintainers must configure the repository secret `TAURI_SIGNING_PRIVATE_KEY`;
the corresponding public key is embedded in `src-tauri/tauri.conf.json`.

Create a release by updating the Tauri version and pushing the matching tag:

```sh
git tag v0.2.0
git push origin v0.2.0
```

The tag and `src-tauri/tauri.conf.json` version must match exactly.

## Contributing

Please open an issue for bugs, source-format changes, or feature proposals.
Small, focused pull requests are welcome. Before submitting a change, run the
backend and Web tests relevant to the area you touched.

## License

No license has been selected yet. Until a license is added, this repository is
source-available and should not be redistributed as open-source software.
