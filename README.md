# Agentmetry

Local observability for AI coding agents.

Agentmetry receives OpenTelemetry (OTLP) data from Claude Code and Codex, then
shows sessions, subagents, activities, token usage, and costs in a local Web UI.
It also exposes the same data through HTTP and MCP.

## Highlights

- One local process with OTLP HTTP/gRPC, SQLite, Web UI, HTTP API, and MCP
- Claude Code and Codex source profiles
- Session, subagent, model, tool, token, and cost views
- Lossless local telemetry journal with canonical query projections
- No external database or hosted service required after installation
- Signed desktop releases and automatic updates for macOS, Windows, and Linux

## Install

Download the latest desktop package for your platform from
[GitHub Releases](https://github.com/theoden9014/agentmetry/releases/latest).
The desktop app starts the local collector, dashboard, API, and MCP server as a
single application.

To build the standalone binary from source, install Go 1.26, Node.js 24, and
npm, then run:

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

## Documentation

- [Documentation index](docs/README.md)
- [Product architecture](docs/architecture.md)
- [Storage maintenance](docs/operations/storage-maintenance.md)
- [API contract](docs/design/api-proto-contract.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

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
The updater public key is embedded in `src-tauri/tauri.conf.json`. CD imports
the Apple certificate into an ephemeral keychain, signs and notarizes the app
and DMG, validates them with Gatekeeper, and publishes the GitHub Release only
after every platform succeeds.

## Contributing

Bug reports, source-format updates, and focused pull requests are welcome. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow and release
policy. Report security issues through the private channel documented in
[SECURITY.md](SECURITY.md).

## License

No license has been selected yet. Until a license is added, this repository is
source-available and should not be redistributed as open-source software.
