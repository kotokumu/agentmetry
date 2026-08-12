# ADR 0014: Signed Desktop Auto-Updates

- Status: Accepted
- Date: 2026-08-12
- Scope: Agentmetry desktop release distribution and startup behavior
- Extends: [ADR 0010](0010-desktop-build-architecture.md)

## Decision

Agentmetry desktop checks a signed static update manifest from the latest
GitHub Release when the shell starts. A newer semantic version is downloaded
and verified while the Go sidecar continues collecting telemetry. The shell
stops the sidecar only after verification, installs the bundle, and restarts.

Git tags remain the release contract. A tag `vX.Y.Z` must exactly match the
version in `src-tauri/tauri.conf.json`. The release workflow uses Tauri's native
updater artifacts and publishes `latest.json` alongside the platform bundles.

## Requirements

1. Update checks never block the window or sidecar from starting.
2. No update is installed unless its signature matches the embedded public key.
3. Network, manifest, signature, and download failures leave the current app
   and sidecar running.
4. The sidecar remains available during download and verification.
5. Installation stops the sidecar before replacing the application bundle.
6. If installation fails synchronously, the shell attempts to relaunch the
   existing sidecar.
7. A successful installation restarts the desktop shell; a short interruption
   during application replacement is accepted.
8. The release workflow reads the updater private key only from the repository
   secret `TAURI_SIGNING_PRIVATE_KEY`; the key is never committed.

## Conceptual model

| Concept | State | Behavior | Invariant |
| --- | --- | --- | --- |
| Release manifest | version and platform assets | describes an available update | URLs use HTTPS |
| Update package | downloaded bytes and signature | verifies before installation | unverified bytes are never installed |
| Sidecar lifecycle | running or stopped | collects telemetry and serves the UI | remains running until verification completes |
| Signing identity | public/private key pair | signs and verifies update packages | private key is never committed |

## Responsibilities

| Responsibility | Owner |
| --- | --- |
| Version comparison, download, signature verification, installation | Tauri Updater |
| Startup scheduling, error isolation, sidecar stop/restart | Desktop shell |
| Native builds, signing, `latest.json`, Release assets | GitHub Actions |
| Release version and public update configuration | Tauri configuration |
| Private signing material | GitHub repository secret |

The updater is infrastructure owned by the shell. The Go runtime, storage, and
Web UI do not depend on Tauri updater types or GitHub release formats.

## Failure behavior

- No newer version: continue normally.
- Update endpoint unavailable or malformed: log and continue normally.
- Download or signature verification fails: log and continue with the running
  sidecar.
- Installation fails after the sidecar stops: attempt to relaunch the current
  sidecar and log the failure.
- Windows installer exits the application as part of installation; other
  platforms explicitly restart after installation.

## Release workflow

The `release` workflow runs on `v*` tags and manual dispatch. It validates the
tag/version contract, requires the updater signing secret, builds each native
platform with Tauri Action, uploads signed updater assets, and merges their
metadata into `latest.json`. The existing macOS DMG adapter still publishes a
human-installable DMG in addition to the updater archive.

## Consequences

- Releases cannot be produced without the updater private key.
- Losing the private key prevents existing installations from trusting future
  updates, so maintainers must keep a secured backup outside the repository.
- GitHub Releases is now runtime distribution infrastructure for desktop apps.
- Auto-update begins only after a user installs a build containing this updater.
- Updates are near-seamless but not zero-downtime because replacing the shell
  and bundled sidecar requires a restart.
