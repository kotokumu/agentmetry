## Why

Agentmetry retains OTLP span events in its raw journal but does not project them for investigation. Supported trace-safe provider events can add useful timing, outcome, and relationship evidence, provided the projection has stable identities and cannot duplicate parent-span or log facts.

## What Changes

- Project only allowlisted provider span events with pinned fixture evidence into non-contributing canonical activity metadata.
- Give each projected event a stable identity derived from its raw export record, parent span, and event ordinal.
- Correlate equivalent span, log, and event observations before counting outcomes.
- Define versioned, idempotent replay behavior for previously retained raw exports.
- Keep unsupported events raw without creating a query record.

## Capabilities

### New Capabilities

- `otel-span-event-projection`: Supported provider span events, identity, provenance, deduplication, replay, and query behavior.

### Modified Capabilities

None.

## Impact

The OTLP normalizer/observation path, canonical projection, SQLite schema and queries, activity APIs, source telemetry documentation, and migration/replay tooling are affected. No external dependency is added.

## Non-goals

- Inferring event body fields from an event name, filename, or arbitrary attributes.
- Projecting Codex trace-safe arguments/output bodies that the pinned source evidence says are absent.
- Treating event usage as authoritative before provider authority and correlation are proven.
- Fetching referenced files or modifying immutable raw journal payloads.
