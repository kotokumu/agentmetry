# Contributing to Agentmetry

Agentmetry welcomes focused bug fixes, source-profile updates, tests, and
documentation improvements. Open an issue before a broad architectural change
so its product behavior and compatibility impact can be agreed first.

## Development environment

Install Go 1.26, Node.js 24, npm, and a Rust toolchain when working on the
desktop shell.

```sh
npm ci
npm --prefix web ci
make build
```

The main packages follow the ownership boundaries described in
[the product architecture](docs/architecture.md). Keep protocol, database,
browser, and desktop-framework details out of canonical and query contracts.

## Tests

Run the checks relevant to your change:

```sh
go test ./...
npm --prefix web test -- --run
npm --prefix web run build
go test -tags=integration ./...
npm run desktop:test
cargo check --manifest-path src-tauri/Cargo.toml
```

Provider-live tests make paid API requests and are opt-in:

```sh
ANTHROPIC_API_KEY=... OPENAI_API_KEY=... \
  go test -tags=providerlive ./internal/source/claude ./internal/source/codex
```

The real-agent evaluation invokes locally authenticated Claude and Codex SDKs
and may consume subscription or API quota:

```sh
npm --prefix evals/agentmetry ci
npm --prefix evals/agentmetry run e2e
```

Never commit provider credentials, local telemetry databases, prompts, command
output, or unsanitized fixtures.

## Pull requests

Keep changes focused and use Conventional Commit titles. Describe observable
behavior, compatibility impact, tests run, and any migration or privacy risk.
Update the current documentation when product behavior changes; preserve old
decision context in ADRs or the archive.

## Releases

Release Please creates the release PR from `main`, calculates the next version,
updates product metadata and the changelog, and creates the tag and GitHub
Release after the release PR is merged. Merge that PR only after its checks
pass. Do not create or push release tags directly.

The distribution workflow owns signed platform artifacts and updater metadata.
Its preflight rejects tags whose version differs from the checked-in product
version or whose commit is not in `main` history.
