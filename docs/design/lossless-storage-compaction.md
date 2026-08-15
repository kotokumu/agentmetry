# Lossless Local Storage Compaction

## Requirement Summary

Agentmetry currently stores each accepted OTLP export as protobuf, export JSON,
per-record JSON, attributes JSON, and query projections. A three-hour production
sample reached 15.3 GB. The new design must preserve every accepted OTLP record
without relying on a short retention period, while sharply reducing both future
growth and the existing database size.

## Functional Requirements

1. Persist one lossless, replayable representation of every acknowledged OTLP
   export.
2. Compress the replay representation before acknowledging the export.
3. Do not persist a JSON rendering that can be regenerated from the protobuf.
4. Materialize only explicitly semantic agent activities in observation and
   query projections. Incidental runtime spans remain available in the replay
   journal.
5. Compact a legacy database by streaming exports into a sibling database,
   rebuilding current semantic projections, validating the result, and swapping
   files only after validation succeeds.
6. Preserve journal metadata, normalization failures, plan snapshots, and all
   user-visible semantic conversations, traces, usage, and activities.
7. Report compaction progress and leave the source database untouched on error
   or cancellation.
8. Ship schema and data migrations inside the application. An upgrade starts a
   maintenance HTTP surface immediately, runs the data migration before OTLP
   listeners accept writes, and switches to the normal application only after
   the database is ready.

## Non-Functional Requirements

- Journal durability remains `synchronous=FULL` with WAL.
- Stored payloads must round-trip byte-for-byte and match the recorded SHA-256.
- Decoding must enforce the recorded uncompressed size and the receiver's 32 MB
  request limit.
- Compression must be deterministic enough for tests, but the compressed bytes
  are not a public compatibility contract.
- Compaction transient disk usage is proportional to the compact database, not
  to another full-size legacy copy.
- Normal ingestion must not perform blocking legacy compaction.
- A single cross-process database ownership lock is shared by normal storage and
  data migration. A live database can never be replaced.
- Representative JSON-heavy fixtures must use at most 30% of their former
  journal-and-observation payload bytes.

## Inputs and Outputs

- Input: accepted OTLP protobuf plus signal, transport, receive time, hash, and
  current normalization result.
- Stored output: codec-tagged payload, original byte length, hash, metadata, and
  bounded semantic projections.
- Compaction input: a closed legacy SQLite database.
- Compaction output: a validated compact SQLite database or an error with the
  legacy database still authoritative.

## Normal Cases

- Compressible protobuf is stored with the `zstd` codec.
- Tiny or incompressible protobuf is stored with the `identity` codec.
- Runtime-only spans are absent from observations and read models but remain in
  the journal payload.
- Existing exports are replayed into the current projection model during
  compaction.

## Error Cases

- Compression or durable commit failure rejects the OTLP export.
- Unknown codec, size mismatch, hash mismatch, or malformed protobuf rejects
  replay and compaction.
- Insufficient disk, cancellation, schema failure, integrity-check failure, or
  validation mismatch removes only the incomplete destination.
- A failed-normalization legacy export remains journaled as failed and creates
  no derived rows.

## Edge Cases

- Empty, tiny, and already-compressed payloads use identity storage when zstd is
  not smaller.
- Sparse observation ordinals retain their original record positions.
- Relevant child spans may have a parent that exists only in the journal; the
  projection records the parent ID without materializing the incidental parent.
- A legacy database may contain an empty journal, only failed exports, or an
  empty WAL. An existing database with no journal table fails closed because no
  lossless authority exists from which projections can be regenerated.
- A process crash between validation and replacement must leave either the old
  or new complete database recoverable through an explicit migration manifest.
- A desktop update may restart while a migration is incomplete; the next launch
  deterministically recovers the manifest before opening SQLite.

## Acceptance Criteria

- All new journal payloads round-trip byte-for-byte with their SHA-256 intact.
- No new export JSON is persisted.
- A fixture containing one semantic span and hundreds of runtime spans produces
  one projected span and preserves all spans in the replay payload.
- Dashboard, conversation, trace, usage, and MCP behavior tests remain green.
- A legacy fixture compacts to fewer bytes, preserves export count and hashes,
  passes `PRAGMA integrity_check`, and can be opened normally.
- Candidate build/validation failure and every durable replacement-manifest
  phase leave one complete authoritative database recoverable on next launch.
- The observed production-shaped fixture reduces storage by at least 70%.
- A populated older storage generation upgrades directly to current and keeps
  the same journal count, hashes, and receipt metadata.
- Every automatic and forced migration entrypoint rejects a newer storage
  generation without modifying the authoritative database.
- A newer binary recovers a same-format manifest targeting an older storage
  generation before applying its current rebuild; unsupported manifest formats
  fail closed.

## Non-Goals

- Time-based retention, remote archival, cross-device synchronization, or
  encryption at rest.
- Querying compressed protobuf directly from SQL.
- Preserving incidental runtime spans in dashboard and trace read models.
- Treating zstd bytes or the current SQLite schema as a public API.
- Using Atlas SQL as a second implementation of the data transformation.

## Migration Ownership

| Concern | Sole owner | Boundary |
|---|---|---|
| Desired SQLite schema and safe additive DDL | Atlas integration in `storage/sqlite` | Runs whenever a database is opened; never decodes or rewrites telemetry data |
| Legacy telemetry data transformation | Go compactor | Owns protobuf restore, zstd encoding, semantic replay, validation, manifest recovery, and file replacement |
| Upgrade/startup sequencing and presentation | Application bootstrap / desktop shell | Starts the maintenance surface, reports progress, and starts normal listeners after Go compaction succeeds |

Atlas and the Go compactor are deliberately not chained as two data migration
engines. Atlas owns schema only. The Go compactor owns data only. Both are
compiled into the distributed sidecar, so users do not install an Atlas CLI or
run a separate migration command after updating the desktop application.

## Upgrade Cohorts

Direct upgrades are supported from every published Agentmetry release starting
at v1.0.0. Releases through v1.2.0 belong to the pre-compression storage family
and upgrade directly to storage generation 2; users do not need to install an
intermediate application version.

| User cohort | Input | Behavior |
|---|---|---|
| Fresh install | No database | Atlas creates the current empty schema, the journal is marked current, and no historical data migration runs |
| Continuously updated | Current storage generation | Startup is a no-op unless Atlas detects an unsafe projection-schema diff, in which case projections rebuild from the journal |
| v1.0.0-v1.0.2 | Pre-compression journal family | The compatible legacy reader streams directly into generation 2 |
| v1.1.0-v1.1.2 | Pre-compression journal family | The compatible legacy reader streams directly into generation 2 |
| v1.2.0 | Pre-compression journal family | The compatible legacy reader streams directly into generation 2 |
| Future supported older generation | Lossless journal readable by this binary | The reader streams directly into the latest schema; intermediate projection schemas are skipped |
| Interrupted migration | Durable replacement manifest | Recovery completes the validated install or restores the untouched legacy DB before migration resumes |
| Downgraded application | Storage generation newer than the binary | Every migration entrypoint rejects it without writes |

One coordinated storage generation covers the Atlas target schema, lossless
journal compatibility, normalizer policy, and derived projection format. It is
bumped whenever projections must be regenerated, even if journal decoding did
not change. Derived projection tables do not accumulate fine-grained data
migrations: an older supported generation is rebuilt directly into the latest
schema from verified journal payloads. Future journal encodings add a backwards
reader/adapter and must still round-trip the original bytes and SHA-256.

The replacement manifest has its own stable format version and separately
records its target storage generation. Format 1 is the first and only supported
manifest format. A newer binary can recover a format-1 manifest targeting an
older storage generation, then rebuild that recovered generation to current.
Unknown manifest formats and target generations newer than the binary fail
closed.

### Durable Replacement Recovery

| Interrupted point / durable phase | Recovery behavior |
|---|---|
| Candidate build, replay, or validation fails/cancels | Remove the incomplete candidate; the source remains authoritative and no manifest is installed |
| `validated`, before or after source rename | Restore the preserved backup when necessary, discard the candidate, and keep the legacy source authoritative |
| `source-preserved` | Restore the backup if no source is installed; otherwise validate the installed candidate and either finalize it or roll back |
| `installed` | Validate the installed journal and generation; finalize only when valid, otherwise restore the backup |
| `verified` | Revalidate defensively, then finish deleting the backup, candidate family, and manifest; an interrupted cleanup is idempotently retried |

## Risks and Open Questions

- Reprojection can intentionally remove previously misclassified runtime spans;
  semantic totals, not raw read-model row counts, are the compatibility target.
- A very small amount of free disk is still required for the compact sibling.
- Very old journal revisions require a maintained backwards reader; unsupported
  newer revisions fail closed to protect downgrade safety.

## Conceptual Model

| Concept | Meaning | State | Behavior | Constraint / Invariant |
|---|---|---|---|---|
| Journal payload | Replay source for one accepted export | codec, bytes, original size, hash | encode, decode, verify | Decodes to the exact acknowledged protobuf |
| Semantic activity | Agent event useful to product queries | source, identity, kind, usage, content | decide projection eligibility | Eligibility uses explicit semantic evidence, not name substrings |
| Derived projection | Replaceable SQLite query model | spans, logs, metrics, rollups | rebuild from journal | Never owns lossless data |
| Legacy database | Current authoritative store during migration | path, schema, exports | stream exports | Remains untouched until destination validation |
| Compact database | Candidate replacement | path, schema, checkpoint | ingest replayed exports, validate | Cannot become authoritative before validation |
| Migration manifest | Crash-recovery state | source, candidate, phase | advance atomically | At least one complete database is always identifiable |

## Relationships

| Concept A | Relationship | Concept B | Notes |
|---|---|---|---|
| Journal payload | is source of | Semantic activity | Re-normalization is versioned |
| Semantic activity | is materialized as | Derived projection | Incidental spans are omitted |
| Legacy database | is streamed into | Compact database | No full in-memory copy |
| Migration manifest | governs authority of | Legacy/compact database | Supports crash recovery |

## Structural Risks

- Missing concepts: codec metadata and migration authority were previously
  implicit.
- Hidden state: payload encoding and migration phase must never be inferred from
  SQLite value types or filenames alone.
- Change-prone areas: codecs, source semantic rules, and projection versions.
- Boundary candidates: journal codec, semantic projection policy, replay reader,
  and database replacement.

## Responsibility Assignment

| Responsibility | Owner | Reason to change | SOLID concern | Not owner | Reason |
|---|---|---|---|---|---|
| Encode/decode journal bytes | `journal.Payload` | Codec or safety limit changes | SRP/OCP | OTLP receiver | Receiver owns protocol acceptance |
| Recognize semantic activities | canonical semantic policy | Producer semantics change | SRP | SQLite store | Storage must not classify events |
| Persist journal and projections atomically | SQLite `Store` | Schema/transaction changes | DIP | Codec | Codec has no database knowledge |
| Rebuild one export | replay service | Normalizer version changes | SRP | Compactor | Compactor orchestrates files, not OTLP semantics |
| Stream and validate replacement | compactor | Migration/recovery changes | SRP | `Store.Open` | Normal startup must stay bounded |

## SOLID Risk Assessment

| Principle | Risk | Mitigation |
|---|---|---|
| SRP | One compaction function handles files, decode, normalize, and validation | Separate replay and replacement responsibilities |
| OCP | Codec switch spreads through SQL and receiver | Persist explicit codec and centralize payload behavior |
| LSP | Identity and zstd codecs provide different verification guarantees | One decoded payload contract verifies size and hash |
| ISP | A broad repository interface appears for tests | Use concrete store and narrow callback/progress contracts |
| DIP | Core semantic rules depend on SQLite rows | Decide semantic eligibility on canonical activities before storage |

## Procedural Risk

- File replacement and crash recovery must belong to a migration state concept,
  not a sequence of unlabelled filesystem calls.
- Payload verification belongs with the journal payload value.
- Do not introduce a generic manager or repository abstraction.

## Proposed Interfaces / Signatures

| Name | Consumer | Responsibility | Signature | Error Contract |
|---|---|---|---|---|
| `journal.Encode` | SQLite store | Choose identity/zstd and create verified stored payload | `Encode(raw []byte) (Payload, error)` | Never returns an unverifiable payload |
| `Payload.Decode` | replay/validator | Restore and verify acknowledged protobuf | `Decode() ([]byte, error)` | Reject codec, size, limit, or hash mismatch |
| `canonical.IsSemantic` | normalizer | Select product-relevant activities | `IsSemantic(ActivityEvidence) bool` | Pure and total |
| `ReplayExport` | compactor | Decode and normalize one stored export | `ReplayExport(ctx, StoredExport) (AcceptedExport, error)` | Keeps failed historical normalization status |
| `Migrate` | application bootstrap / CLI | Acquire ownership, recover, build, validate, and install a replacement database | `Migrate(ctx, sourcePath string, progress func(Progress)) (Result, error)` | Exactly one authoritative, normally openable database remains after every return or recoverable crash |

## Example Call Sites

```go
stored, err := journal.Encode(accepted.Envelope.Protobuf)
if err != nil { return err }
if err := store.commitAcceptedExport(ctx, accepted, stored); err != nil { return err }

result, err := compactor.Migrate(ctx, databasePath, reportProgress)
```

## Boundary Decisions

| Boundary | Hidden detail | Reason |
|---|---|---|
| Journal payload | zstd library, thresholds, limits | Replay callers need verified bytes only |
| Semantic policy | producer aliases and evidence rules | Persistence must remain source-neutral |
| Replay | OTLP pdata decoding and normalizer version | Compactor should not duplicate ingestion |
| Compactor | temporary paths, fsync, manifest, atomic rename | Callers receive one recoverable operation |

## Interface Risks

- Oversized interfaces: avoid a general storage repository.
- Primitive obsession: codec, migration phase, and progress are named types.
- Infrastructure leakage: SQLite rows remain inside the storage/compaction layer.
- Boolean flag risks: encode decisions are derived, not passed as flags.

## Test Specifications

| Behavior | Given | When | Then | Test Level | Notes |
|---|---|---|---|---|---|
| Lossless compression | compressible protobuf | encode then decode | exact bytes and hash return | unit | includes corruption cases |
| Adaptive identity | tiny payload | encode | identity is selected | unit | avoids negative compression |
| Bounded projection | semantic and runtime spans | normalize/commit | only semantic rows are projected | integration | raw replay contains both |
| Atomic commit | journal/projection batch | storage failure | neither side is visible | integration | existing invariant |
| Legacy compaction | populated legacy DB | compact | smaller valid DB with same exports | integration | compare hashes |
| Failed compaction | injected failure | compact | legacy DB remains readable | integration | test phases |
| Query compatibility | representative Codex/Claude data | compact/open/query | semantic results match | end-to-end | excludes incidental rows |

## Invariant Tests

| Invariant | Example | Expected Result |
|---|---|---|
| Acknowledged export is replayable | stored zstd row | exact protobuf |
| Derived data is replaceable | delete/rebuild projections | same semantic queries |
| Authority changes only after validation | invalid candidate | source remains at canonical path |
| Sparse ordinals preserve provenance | runtime ordinal 0, semantic ordinal 7 | stored ordinal is 7 |

## Error / Edge Case Tests

| Case | Given | When | Then |
|---|---|---|---|
| Corrupt compressed bytes | valid metadata, modified blob | decode | corruption error |
| Decompression bomb metadata | size above receiver limit | decode | reject before allocation |
| Unknown codec | future codec name | decode | explicit unsupported-codec error |
| No free space | destination write fails | compact | source untouched, candidate cleaned |
| Cancellation | context ends mid-stream | compact | source untouched, resumable cleanup |

## Testability Feedback

- Journal payload behavior is independently testable without SQLite.
- Semantic eligibility is a pure behavior test.
- Compaction needs filesystem integration tests but no mocked SQL call ordering.

## Detailed Design

New databases store `payload_codec`, compressed `payload_protobuf`, original
`payload_size`, and the SHA-256 of original protobuf. `payload_json` is omitted
from the compact schema. Observations retain semantic promoted columns and
provenance ordinal only; their full JSON and attribute copies are omitted because
the journal is authoritative. Read models include only spans/logs that carry
explicit agent, conversation, tool, content, usage, or recognized event evidence.

Legacy databases remain readable through a legacy export reader. Compaction
creates a sibling candidate with the compact schema, streams exports in ID order,
decodes legacy protobuf, validates its hash, replays current normalization, and
commits into the candidate. It then copies plan snapshots, rebuilds rollups,
checkpoints WAL, runs integrity and journal validations, and records a validated
manifest. Replacement is a separate, explicit phase so tests can prove recovery
for every file state.

## TDD Plan

| Behavior | Red Test | Green Implementation | Refactor Target |
|---|---|---|---|
| Verified compressed payload | journal round-trip/corruption tests | payload value with zstd codec | codec constants and limits |
| Strict semantic selection | noisy Codex span fixture | pure evidence policy | source aliases vs common rules |
| Compact journal commit | storage test rejects duplicated JSON | compact schema/write path | journal row mapping |
| Legacy streaming compaction | legacy fixture size/hash test | reader, replayer, candidate writer | migration phase ownership |
| Crash-safe replacement | durable-phase recovery table test | manifest state machine | filesystem boundary |
