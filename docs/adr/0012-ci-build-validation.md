# ADR 0012: CI Build Validation

- Status: Accepted
- Date: 2026-08-11
- Scope: Pull request/main validation and release separation

## Decision

Continuous integration and continuous delivery are separate workflows:

- `.github/workflows/ci.yml` validates pull requests and pushes to `main`.
- `.github/workflows/release-please.yml` maintains a version and changelog PR
  from Conventional Commits on `main`, then creates the release tag when that
  PR is merged.
- `.github/workflows/release.yml` builds and publishes packages only for `v*`
  tags, with manual execution available for build-only diagnostics.

CI does not publish installers or create Releases. CD does not act as the PR
test gate.

## CI gates

1. Web dependencies install, Web tests pass, and `web/dist/generated` builds.
2. `go test ./...` runs independently of the Web build.
3. Integration tests explicitly build the Web UI and run with `-tags=integration`.
4. Desktop target metadata tests pass and the Linux sidecar input builds.
5. Tauri Rust dependencies resolve from crates.io and compile with `cargo check`.

`web/dist/generated` is intentionally ignored by Git. The empty `web/dist`
directory is retained with `.gitkeep` so the Go package can compile without a
Web build. The Dockerfile still uses a dedicated Web build stage and copies the
generated assets into the Go build stage.

## Release gates

The tag workflow remains responsible for native macOS, Windows, and Linux
package generation and GitHub Release publication. Release Please creates tags
from merged `main` release PRs. The tag workflow independently verifies that
the tagged commit belongs to `main` and that the tag version matches the Tauri
application version before building. Its preflight requires updater and Apple
signing credentials. macOS Developer ID signing, notarization, stapling, and
Gatekeeper assessment are mandatory release gates; the GitHub Release remains
a draft until every platform succeeds.
