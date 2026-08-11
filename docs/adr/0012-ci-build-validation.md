# ADR 0012: CI Build Validation

- Status: Accepted
- Date: 2026-08-11
- Scope: Pull request/main validation and release separation

## Decision

Continuous integration and continuous delivery are separate workflows:

- `.github/workflows/ci.yml` validates pull requests and pushes to `main`.
- `.github/workflows/release.yml` builds and publishes packages only for `v*`
  tags, with manual execution available for build-only diagnostics.

CI does not publish installers or create Releases. CD does not act as the PR
test gate.

## CI gates

1. Web dependencies install, Web tests pass, and `web/dist` builds.
2. Go tests run only after `web/dist` has been generated.
3. Desktop target metadata tests pass and the Linux sidecar input builds.
4. Tauri Rust dependencies compile with `cargo check`.

`web/dist` is intentionally ignored by Git. The generated directory is a build
input for Go's `embed`, so every path that compiles or tests the Go runtime must
run the Web build first. The Dockerfile already uses a dedicated Web build stage
and copies its output into the Go build stage.

## Release gates

The tag workflow remains responsible for native macOS, Windows, and Linux
package generation and GitHub Release publication. It verifies that the tag
version matches the Tauri application version before building. Signing and
notarization remain future release gates.
