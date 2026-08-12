# ADR 0009: Cross-Platform Desktop Packaging

- Status: Accepted (runtime packaging scope; build architecture superseded by ADR 0010)
- Date: 2026-08-11
- Scope: macOS first, Windows/Linux build-compatible desktop distribution

## Requirement summary

Agentmetry must be installable as a native desktop application on macOS first. The
same desktop boundary must support Windows and Linux later without duplicating the
Web UI or server behavior. The existing Go server and embedded SPA remain the
product runtime; the desktop shell owns process lifecycle and native packaging.

The first milestone produces an unsigned local macOS `.app` and `.dmg`. Code
signing, notarization, and the protected SQLite unlock flow are separate release
gates, but the desktop boundary must leave room for them.

## Requirement specification

### Functional requirements

1. Launching the desktop application starts exactly one Agentmetry Go sidecar.
2. The sidecar serves the existing embedded SPA and API on loopback.
3. The desktop window opens only after the sidecar health endpoint is ready.
4. Desktop data uses the platform app-data directory rather than the repository's
   relative `data/` directory.
5. Closing the desktop application terminates the sidecar.
6. macOS packaging emits an installable application bundle and DMG.
7. The build uses a cross-platform desktop toolchain and target-specific sidecar
   artifacts, so Windows and Linux can be added through the same configuration.
8. The application remains resident after the main window is closed.
9. The tray menu can show/hide the window and quit the desktop application.
10. OTLP reception starts with the application and remains active while the
    dashboard window is hidden.

### Non-functional requirements

- Keep the Web UI and server core independent of Tauri.
- Do not put database keys or other secrets in command-line arguments, environment
  variables, logs, or the frontend.
- Bind the desktop sidecar to loopback by default.
- Make target-specific native behavior live in the desktop shell/build layer.
- Preserve a headless `go run ./cmd/agentmetry` / `agentmetry serve` workflow.

### Explicitly out of scope for this milestone

- SQLCipher/SEE integration.
- macOS Keychain or Windows Hello unlock implementation.
- Signed/notarized release artifacts.
- Windows/Linux installers and native validation on those hosts.

Those capabilities will attach at the sidecar startup/storage boundary without
changing the Web UI contract.

## Conceptual model

```text
DesktopApplication
  owns DesktopWindow
  supervises SidecarProcess
  resolves PlatformDataDirectory

SidecarProcess
  runs Agentmetry Go server
  owns SQLite connection
  serves embedded SPA/API/MCP

DesktopBuild
  builds web assets
  builds target-specific Go sidecar
  bundles shell + sidecar + metadata
```

The sidecar is the sole owner of the local server and database. The shell never
imports domain, query, or SQLite code.

## Responsibility assignment

| Responsibility | Owner | Boundary |
|---|---|---|
| Web rendering and query UX | Existing Lit SPA | HTTP/Connect API |
| Ingestion, query, SQLite, MCP | Go server | Go process |
| Sidecar launch/readiness/termination | Tauri shell | child-process API |
| Resident lifecycle and user controls | Tauri shell | tray menu + window events |
| Native data directory | Tauri shell | path passed to Go CLI |
| Target binary naming | build script | Go target metadata |
| `.app`/`.dmg` and future installers | Tauri bundler | platform packaging |
| Signing/notarization | release pipeline | CI secrets and platform tools |

## SOLID risk assessment

- **DIP:** the Go server remains unaware of Tauri; its existing CLI flags are the
  sidecar contract.
- **SRP:** Tauri only supervises the process and window. It does not implement
  telemetry, query, or storage behavior.
- **OCP:** adding a target adds a Go target mapping and CI matrix entry without
  changing the server or SPA.
- **Main risk:** a fixed localhost port can collide with a standalone server. The
  first milestone keeps the existing port contract for compatibility; a later
  authenticated dynamic-port/IPC design should remove that collision risk.

## Module and package boundaries

The build-specific structure in this section was replaced by
[ADR 0010](0010-desktop-build-architecture.md). The runtime ownership and Tauri
shell boundaries below remain valid.

```text
src-tauri/
  src/main.rs             native lifecycle and window
  capabilities/           Tauri permissions
  tauri.conf.json         bundle/build configuration
  binaries/               generated target-specific sidecars (ignored)
build/desktop/
  build-sidecar.mjs       Go target build matrix
.github/workflows/
  desktop.yml             host-native desktop build matrix
```

The Go `cmd/agentmetry` package is the sidecar executable. The current `--database`
flag allows the shell to select the platform app-data path without adding a new
storage abstraction.

## Interface proposal

The existing CLI contract is extended only by desktop invocation:

```text
agentmetry --http-address 127.0.0.1:17890 \
  --otlp-http-address 127.0.0.1:4318 \
  --otlp-grpc-address 127.0.0.1:4317 \
  --database <platform-app-data>/agentmetry.db
```

The shell observes `GET /healthz` before creating the visible window. The shell
must terminate the child on application exit.

The main window's close request is converted to `hide`, leaving the tray,
sidecar, dashboard, and OTLP receivers running. The tray menu exposes only
`open`, `hide`, and `quit`. There is no user-facing OTLP start/stop action in
this milestone because the resident application's primary purpose is to keep
receiving telemetry continuously. `quit` terminates the sidecar and exits the
desktop application.

## Test specifications

1. The Go binary still starts with the existing relative database path in headless
   mode.
2. The sidecar command receives an absolute platform data path from the shell.
3. The shell waits for `/healthz` before showing the dashboard window.
4. A child process is terminated when the desktop application exits.
5. The target script maps macOS arm64 and amd64, Windows amd64/arm64, and Linux
   amd64/arm64 to both Go and Tauri target triples.
6. `npm run desktop:build:macos` builds the SPA, sidecar, and macOS bundle.
7. The CI matrix invokes the same build entry point on macOS, Windows, and Linux.
8. Existing `go test ./...` and `npm --prefix web test` remain green.

## Detailed design

Tauri 2 is used because it provides a native WebView shell, sidecar embedding,
platform bundling, and a common configuration for macOS/Windows/Linux. The Go
server already embeds `web/dist`, so the production window loads the sidecar's
loopback URL and avoids a second desktop-only frontend implementation.

The initial package was intentionally unsigned. The release pipeline now uses
the configured entitlements, Developer ID signing, notarization, and stapling,
and refuses to publish when those release gates or credentials are unavailable.
