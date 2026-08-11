# ADR 0013: Token Accounting Test Strategy

- Status: Accepted
- Date: 2026-08-12
- Scope: Claude and Codex token and cost accounting

## Decision

Token accounting is tested at two deterministic levels and one explicitly
opt-in provider level:

| Test | Command | Guarantees |
| --- | --- | --- |
| Unit | `go test ./...` | Fixed-point cost formula, rounding, invalid input rejection, and the rule that cache/reasoning breakdowns are not double-counted |
| Fixture integration | `go test ./...` and `go test -tags=integration ./...` | Provider aliases, JSON/JSONL usage parsing, Claude cache-inclusive input normalization, Codex cached-input semantics, ingestion, and SQLite rollups |
| Provider live | `go test -tags=providerlive ./internal/source/claude ./internal/source/codex` | Real CLI output is parsed and compared with the provider-reported usage oracle |

The Web UI is not part of token correctness testing. The calculation and
normalization happen in Go, so the test boundary is the provider output,
source adapter, canonical model, and storage projection.

## Canonical semantics

- `Input` is total input tokens, including cache reads and cache writes.
- `CacheRead` and `CacheWrite` are input breakdowns.
- `Output` is the provider-reported output total.
- `Reasoning` is an output breakdown and is never added to `Output`.
- `Total()` is `Input + Output`.

Claude Code's request fields are normalized as:

```text
canonical.Input = input_tokens + cache_read_input_tokens + cache_creation_input_tokens
```

Codex/OpenAI's `input_tokens` already includes cached input, so it is retained
as-is and `cached_input_tokens` is kept only as a breakdown.

## Cost semantics

The calculator uses integer micro-USD per million tokens and half-up rounding
for each component. It charges uncached input, cache read, cache write, and
output independently. A pricing record is passed into the calculator rather
than hidden in normalization, so rate-card versioning and provider-reported
cost can remain separate.

The provider live tests treat the usage returned by Claude Code and Codex as
the oracle for token fields. They do not infer a provider cost from token
counts; deterministic cost tests use an explicit rate card. This keeps a rate
catalog change from silently changing the meaning of observed provider usage.

## Failure policy

Missing or malformed provider usage is a test failure, not zero usage. Live
tests are excluded from ordinary PR CI because they require credentials and
make paid requests; the explicit `providerlive` command and workflow are the
automation gate for real provider contracts.
