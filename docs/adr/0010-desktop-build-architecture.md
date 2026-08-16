# ADR 0010: Desktop Build Architecture

- Status: Accepted
- Date: 2026-08-11
- Scope: Agentmetry desktop development and distribution builds
- Supersedes: The build-management portions of [ADR 0009](0009-desktop-packaging.md)

## Decision

Agentmetry uses one runtime binary and one desktop shell:

- `cmd/agentmetry` is the canonical Go runtime for headless, container, and
  desktop profiles.
- The Go runtime embeds and serves the canonical SPA from `web/dist`.
- Tauri is a native shell and process supervisor. It loads the sidecar's
  loopback URL and does not package a second copy of the SPA.
- `build/desktop/` owns desktop build orchestration and target-specific build
  adapters.
- Tauri's bundler owns native application bundles and Windows/Linux installers.
- macOS DMG assembly is an explicit macOS packaging adapter around the Tauri
  `.app` output because a DMG is a distribution container, not a second app
  runtime.

The build graph is therefore:

```text
web/src + web package
        │
        ▼
web/dist  ───────────────┐
                         │
Go source + web/dist     │
        │                │
        ▼                │
agentmetry-<target>     │
        │                │
        └──────────────┐  │
                       ▼  ▼
                 Tauri shell + sidecar
                       │
                       ▼
          .app / NSIS / AppImage / deb
                       │
                       ▼
              macOS DMG adapter
```

There is exactly one authoritative production UI: the SPA served by the Go
sidecar. Tauri's `frontendDist` is intentionally unset so the UI is not copied
into the Tauri application resources.

## Requirements

### Functional requirements

1. `go build ./cmd/agentmetry` remains the canonical server build.
2. A desktop build produces a target-specific Go sidecar before Tauri bundling.
3. The sidecar embeds the same `web/dist` used by headless mode.
4. The desktop shell starts the sidecar and loads its loopback dashboard URL.
5. macOS produces an `.app` and a DMG containing that exact `.app`.
6. Windows and Linux use the same build entry point with native Tauri bundles.
7. Development, local release, and CI builds share the same input preparation
   command and target mapping.
8. Generated sidecars and Tauri build output never become source inputs or git
   changes.

### Non-functional requirements

- Build orchestration must work on macOS, Windows, and Linux without POSIX shell
  assumptions.
- Platform-specific commands must be isolated in named adapters.
- The desktop shell must not import Go domain, storage, or transport packages.
- The build must fail before bundling when the web output or target sidecar is
  missing.
- A developer must be able to build the headless server without Tauri or Node
  after the checked-in/generated web assets are available.

### Out of scope

- Code signing and notarization
- Auto-update publication (subsequently added by
  [ADR 0014](0014-desktop-auto-update.md))
- Universal macOS binaries
- SQLCipher/SEE and Keychain/credential-manager integration

## Conceptual model

```text
BuildInput
  WebSource
  GoSource
  TauriSource
  TargetTriple

PreparedInputs
  WebDist
  SidecarBinary(target)

DesktopBundle
  TauriApplication(target)
  PlatformInstaller(target)
```

Invariants:

- `SidecarBinary.target` equals the Tauri build target.
- The sidecar is built from the same repository revision as the shell.
- A release bundle cannot be created without a prepared sidecar.
- The Tauri shell's production URL and the Go dashboard address remain the same
  loopback contract.
- The DMG contains only the Tauri `.app` produced by the immediately preceding
  bundle step.

## Directory structure and responsibility

```text
.
├── cmd/agentmetry/                 canonical Go runtime entrypoint
├── internal/                       Go application, storage, ingest, APIs
├── web/
│   ├── src/                        SPA source
│   ├── dist/                       generated embedded SPA output
│   └── package.json                web-only dependencies and tests
├── build/
│   └── desktop/
│       ├── targets.mjs             pure target metadata
│       ├── build-sidecar.mjs       Go sidecar compiler
│       └── package-macos-dmg.mjs   macOS DMG adapter
├── src-tauri/
│   ├── src/main.rs                 shell, tray, sidecar lifecycle
│   ├── tauri.conf.json             shell and bundle configuration
│   ├── Cargo.toml                  shell dependencies
│   ├── capabilities/               Tauri permissions
│   ├── icons/                      source/native icons
│   └── binaries/                   generated Tauri sidecar staging area
├── .github/workflows/desktop.yml   native-host CI matrix
└── package.json                    repository desktop build entrypoints
```

`scripts/` is not used for desktop build logic. A top-level `scripts/` directory
is reserved for repository maintenance scripts that are not part of producing
the application. Desktop build code lives under `build/desktop/` so its module
boundary is visible from the directory name.

## Responsibility assignment

| Responsibility | Owner | Output/contract |
|---|---|---|
| SPA compilation | `web/package.json` | `web/dist` |
| Target metadata | `build/desktop/targets.mjs` | Go/Tauri target mapping |
| Go sidecar compilation | `build/desktop/build-sidecar.mjs` | `src-tauri/binaries/agentmetry-<target>` |
| Shell compilation and app bundle | Tauri CLI/Cargo | `src-tauri/target/.../bundle` |
| macOS DMG assembly | `build/desktop/package-macos-dmg.mjs` | `.dmg` containing `.app` |
| Build entrypoint | root `package.json` | stable developer/CI commands |
| CI host selection | GitHub Actions | native macOS/Windows/Linux build |
| Runtime behavior | `src-tauri/src/main.rs` and Go | sidecar lifecycle and HTTP contract |

## Build entrypoints

```text
npm run desktop:inputs
  web build → target sidecar

npm run desktop:dev
  desktop:inputs → tauri dev → shell starts sidecar

npm run desktop:build
  desktop:inputs via tauri hook → tauri build

npm run desktop:build:macos
  desktop:inputs → tauri .app → macOS DMG adapter
```

The Windows and Linux commands select Tauri's native bundle targets but reuse
`desktop:inputs`. There is no second CI-only build path.

## Interface proposal

Target metadata is the only cross-language build interface:

```js
getTargetMetadata(targetTriple) -> {
  goos,
  goarch,
  sidecarFilename,
}
```

The Tauri external binary contract remains:

```text
src-tauri/binaries/agentmetry-<rust-target-triple>[.exe]
```

The Go runtime contract remains its existing CLI flags. No desktop-specific
runtime code is added to the Go domain or storage packages.

## Test specifications

1. Target metadata returns the expected Go OS/architecture and sidecar filename
   for macOS arm64/x64, Windows x64/arm64, and Linux x64/arm64.
2. Unsupported targets fail before invoking `go build`.
3. Input preparation creates `web/dist` and the exact target sidecar path.
4. Tauri configuration has no `frontendDist` path and therefore does not
   duplicate the Go-embedded SPA.
5. macOS packaging fails if the `.app` is missing and succeeds with the exact
   `.app` in the DMG.
6. Native CI jobs invoke the same root build entrypoint as local builds.
7. `go test ./...`, web tests, Rust checks, and macOS bundle smoke checks are
   separate gates with clear failure ownership.

## SOLID and risk assessment

- **SRP:** target selection, sidecar compilation, Tauri bundling, and DMG
  assembly are separate modules.
- **DIP:** the Go runtime depends only on its CLI contract; the shell depends on
  the sidecar HTTP contract, not Go packages.
- **OCP:** adding a supported target extends the metadata table and CI matrix;
  it does not change runtime code.
- **Risk:** `web/dist` is a generated embed input and is ignored by Git. A clean
  checkout must run the Web build before any desktop or server build that
  imports it.
- **Risk:** fixed loopback ports can collide with another Agentmetry process;
  authenticated dynamic-port/IPC startup remains a later security milestone.

## Migration plan

1. Move desktop helpers from `scripts/` to `build/desktop/`.
2. Extract target metadata from the sidecar compiler.
3. Remove Tauri's duplicate `frontendDist` input.
4. Make root package scripts and CI use the same named commands.
5. Add artifact and configuration smoke validation.
6. Rebuild and inspect the macOS `.app` and DMG.

## Continuous delivery

`.github/workflows/release-please.yml` owns release preparation and
`.github/workflows/release.yml` owns distribution. A `v*` tag is the
publication contract:

1. Release Please maintains the changelog and application version in a Release
   PR targeting `main`.
2. Merging that PR creates the matching tag and draft GitHub Release from the
   merged `main` commit.
3. Release preflight independently verifies that the tagged commit belongs to
   `main` history.
4. Native macOS, Windows, and Linux runners install the locked dependencies.
5. Each runner invokes the same root package entrypoint used locally.
6. The tag version is checked against `src-tauri/tauri.conf.json`.
7. macOS imports a Developer ID Application certificate into an ephemeral
   keychain, then signs, notarizes, staples, and verifies the app and DMG.
8. Package files are uploaded as short-lived workflow artifacts and to a draft
   GitHub Release.
9. The publish job verifies the complete asset set and publishes the draft only
   after every native build and release gate succeeds.

Updater signatures and `latest.json` are generated by Tauri Action. Apple
signing and notarization credentials remain only in GitHub repository secrets.
