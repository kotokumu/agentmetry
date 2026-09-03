# Raw OTLP span event and projection audit

## 1. Verified fixture

`TestValidOTLPJournalRetainsRawSpanEventBeyondProjection` constructs a valid OTLP protobuf export with one Codex-shaped semantic span and one nested span event. It uses the production normalizer, observation builder, journal compression/hash verification, SQLite commit, raw restore and trace query. The fixture is synthetic and fixed on 2026-09-04. It is not an upstream capture and does not assert the current Codex or Claude wire format.

| Fact | Raw journal after restore | Current projection |
|---|---|---|
| Parent identity | Exact 16-byte trace ID and 8-byte span ID | Same trace/span ID |
| Time | Span start/end and event timestamp | Span start/end only |
| Content | Parent `output`; event `output` sentinel | Parent output only; no event activity |
| Usage | Parent input/output and a deliberately distinct event input value | Parent input/output only |
| Event identity | Parent trace/span plus ordinal, name and timestamp; no native event ID | No corresponding activity |
| Attributes | Full received span and event maps | Only profiled parent span attributes |

The test also verifies that the event output and event usage are not independently projected or double counted. This describes the current implementation. It does not establish that a producer sends those synthetic event fields.

---

## 2. Value and constraints

| Candidate | Evidence and value | Required identity and deduplication | Decision |
|---|---|---|---|
| Event timing/name/status metadata | Valid OTLP provides parent trace/span, event ordinal/name/timestamp; known provider snapshots describe trace-safe events | Stable derived identity must include export record, parent identity and event ordinal. A provider event corresponding to an already projected log/span must not add totals or a second outcome without a documented correlation key | Plan as a follow-up, initially metadata-only and non-contributing |
| Event content | Journal can retain arbitrary event attributes, but the fixture value is synthetic. The pinned Codex snapshot says trace-safe tool-result events omit arguments/output body. The pinned Claude snapshot does not name the tool-output body attribute | Provider and exact received field must be proven by a pinned fixture before projection. No filename/event-name inference or external fetch | Do not map content in the follow-up without new pinned evidence |
| Event usage | Valid OTLP can carry event usage, but the synthetic parent/event values prove only storage fidelity | Must define provider authority and correlation with parent/log/metric usage before contributing to totals | Retain raw; exclude from totals in the initial follow-up |
| Existing raw exports | Journal rows retain canonical OTLP protobuf, codec, size and hash | Backfill requires versioned, idempotent replay with the same identity/deduplication rules; it must not silently rewrite journal payload | Include explicit old-raw scope in the follow-up; implementation requires an accepted replay decision |

Projection coverage and body availability stay independent. The raw export can contain an unprojected event while analysis is complete relative to the current projection. A display filter can hide an activity without changing either state.

---

## 3. Verification

`GOCACHE=/tmp/agentmetry-comparison-go-cache go test ./internal/storage/sqlite -run '^TestValidOTLPJournalRetainsRawSpanEventBeyondProjection$' -count=1` passes. The fixture uses no network, external provider process, private capture or ingestion behavior change.
