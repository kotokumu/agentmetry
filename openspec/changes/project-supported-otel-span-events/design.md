## Context

[The raw/projection audit](https://github.com/kotokumu/agentmetry/blob/main/openspec/changes/improve-otel-investigation/raw-projection-audit.md) proves that a valid OTLP journal round trip retains span events while the current projection creates only parent-span activities. The pinned Codex evidence describes trace-safe event metadata without body text. The pinned Claude evidence does not name the tool-output body attribute. Native-span anchors currently select the stored span row for an exact trace/span pair.

## Goals / Non-Goals

**Goals:**

- Project only source-owned allowlisted event metadata with stable provenance.
- Keep native span selection unambiguous and prevent repeated totals/outcomes.
- State honestly whether older retained exports received the event projection.

**Non-Goals:**

- Body or usage projection without new pinned evidence and authority rules.
- Generic projection of every `Span.events` record.
- Modifying journal payloads or fetching referenced content.

## Decisions

### 1. Provider plugins own event allowlists

An optional provider capability maps a span-event input to a bounded projected event value. It receives provider, event name, parent identity, ordinal, timestamp, and raw attributes but returns only allowlisted canonical fields. The OTLP normalizer coordinates iteration and has no provider-specific field names.

Keeping the allowlist in the generic normalizer would mix Claude and Codex contracts. Projecting every event as unknown would create unbounded noise and expose unsupported attributes.

### 2. Event activities use related parent identity

A projected event has its own activity ID derived from export identity, parent trace/span, and event ordinal. It carries the parent as `relatedTraceId`/`relatedSpanId`; its native `spanId` remains empty. This preserves the existing rule that an exact trace/span anchor names the native stored span, even when correlated logs or projected events exist.

Timestamp, source event name, outcome/status, and explicitly allowed operation IDs can be copied. Arbitrary attributes are not stored in API metadata. The raw journal remains the source for unsupported fields.

### 3. Initial event records never contribute to aggregates

Projected event activities set `contributesToTotal=false` and omit canonical token usage. Rework classification accepts an event outcome only after a provider rule supplies a documented operation identity and the classifier correlates it with parent-span/log observations. Unknown correlation keeps evidence visible without adding an outcome.

This favors incomplete-but-honest analysis over double counting. Timestamp proximity alone is not a correlation identity.

### 4. New ingestion is the initial data scope

The first projection version processes new valid exports only. SQLite stores the event projection version and exposes coverage that distinguishes new-only processing from a complete retained-export replay. Existing raw exports remain unchanged and raw-only.

A later opt-in replay can be added only with a durable cursor keyed by export order, transactionally committed batches, deterministic identity, and the same projection version. Restarting it upserts the same identities. Automatic unbounded replay during ordinary database open is excluded.

### 5. Existing activity APIs carry event metadata

The existing query activity model and Connect, HTTP, MCP, and Web mappings expose the event kind, source event name, derived identity, related parent, and projection coverage. Content evidence remains absent/not-reported unless a later accepted provider contract adds an exact body field. No dedicated event fetch service is added.

## Risks / Trade-offs

- **[Provider telemetry changes]** → Pin fixture provenance and projection version; unmatched revisions remain raw-only.
- **[Parent/log/event duplication]** → Keep events non-contributing and require explicit correlation identities before outcome use.
- **[Large event volume]** → Allowlist semantic events and index the derived activity identity and parent relation.
- **[Older data appears incomplete]** → Return projection-version coverage instead of implying journal replay occurred.
- **[Derived identity changes]** → Version the derivation and lock it with replay/idempotence fixtures.
