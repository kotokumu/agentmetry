<p align="center">
  <img src="./assets/brand/agentmetry-logo-readme.png" alt="Agentmetry" width="640">
</p>

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
[GitHub Releases](https://github.com/kotokumu/agentmetry/releases/latest).
The desktop app starts the local collector, dashboard, API, and MCP server as a
single application.

To build the standalone binary from source, install Go 1.26, Node.js 24, and
npm, then run:

```sh
git clone https://github.com/kotokumu/agentmetry.git
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

### Compare harness revisions

The Before/After diagnostics can show whether two sessions reported the same
harness fingerprint. Generate a fingerprint from the files that define the
harness for your project:

```sh
agentmetry harness fingerprint \
  --scope project-7f2a \
  --label "AGENTS v2" \
  --file AGENTS.md \
  --file .codex/config.toml
```

The command prints JSON with `scope`, `fingerprint`, and `label`. File contents
stay local. Use an opaque, stable scope for sessions that should be comparable;
do not put secrets or configuration content in the scope or label.

For Codex, copy the generated values as literal static headers into the
user-level `~/.codex/config.toml` for each configured Agentmetry exporter:

```toml
[otel]
exporter = { otlp-grpc = { endpoint = "http://127.0.0.1:4317", headers = { "x-agentmetry-harness-scope" = "project-7f2a", "x-agentmetry-harness-fingerprint" = "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", "x-agentmetry-harness-label" = "AGENTS v2" } } }
```

Apply the same `headers` map inside `trace_exporter` and `metrics_exporter` when
those exporters are configured separately. Codex does not expand `${ENV}` in
these static header values, and project-local `.codex/config.toml` files do not
apply `otel` settings. Regenerate and replace the literal fingerprint whenever
the selected files change. For Claude Code, copy the generated values into
`OTEL_EXPORTER_OTLP_HEADERS` in its environment:

```text
x-agentmetry-harness-scope=project-7f2a,x-agentmetry-harness-fingerprint=sha256:…,x-agentmetry-harness-label=AGENTS-v2
```

The dashboard treats missing, partial, invalid, or mixed reporting as not comparable
instead of assuming the harness was unchanged. A reported fingerprint match is
an association only; it does not prove complete effective-configuration equality
or causality.

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

Licensed under the [Apache License 2.0](LICENSE).
