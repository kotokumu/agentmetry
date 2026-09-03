# Investigation implementation log

## 1. Task 2.1: exact native span anchor

`GetTrace` accepts an optional typed `SpanID`. The SQLite reader ranks identity/timing metadata from native spans and correlated logs in the same global order as its activity page. Only a native `trace` row can satisfy the target. The rank selects a bounded page through the existing `Page.OffsetAround`; an anchor takes precedence over page position and live tail. The trace summary and rank/page reads share the existing read transaction.

| Contract | Implemented behavior |
| --- | --- |
| Query | `TraceFilter.SpanID`; zero value preserves existing reads |
| Connect | `GetTraceRequest.anchor_span_id = 4` |
| HTTP | `GET /api/v1/traces/{traceId}?spanId={spanId}` |
| MCP | `get_trace.anchorSpanId`; captured bodies remain opt-in |
| Missing target | `ErrTraceTargetNotFound` for an existing trace without the native target; distinct from `ErrTraceNotFound` |
| Wire validation | Malformed and all-zero span IDs are rejected. Uppercase hexadecimal IDs normalize through the existing parser. Existing page-size bounds remain 1–100 |

Implementation: [query contract](https://github.com/kotokumu/agentmetry/blob/main/internal/query/trace.go), [SQLite trace read](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/trace.go), [Connect](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/connectapi/server.go), [HTTP](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/httpapi/handler.go), [MCP](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/mcpserver/server.go).

---

## 2. Red and green evidence

The following cycles run on 2026-09-04. Red means an assertion failure from the incomplete implementation, not a tool or sandbox failure.

| Unit / existing test | Observed red | Implemented correction / green |
| --- | --- | --- |
| `Test_traceSpanOffset` | Rank is 0 instead of 2; logs-only and another-trace targets incorrectly succeed | Metadata union plus `ROW_NUMBER` in trace page order; native-span-only match; all three cases pass |
| `TestGetTraceCorrelatesConversationsAndReportsIncompleteParents` anchor subtest | Tail page contains other span IDs and reports offset 120 instead of the requested target page | Resolve rank before detail paging; use `OffsetAround`; exact ordered target page passes |
| `TestGetTraceReturnsNotFoundForUnknownIdentity` logs-only extension | A correlated log returns success without a native target | Propagate `ErrTraceTargetNotFound`; trace-not-found remains distinct; passes |
| `TestConnectServerBoundsTracePages` | Typed trace filter has an empty SpanID despite the request anchor | Add field parsing and transport error mapping; valid anchor, malformed/zero ID, excessive page size, and target-not-found cases pass |
| `TestTraceAPIReturnsCompleteVersionedTrace` and `TestTraceAPIMapsUnknownTraceToNotFound` | HTTP ignores spanId; invalid IDs return 200; missing native target returns 500 | Add parsing and 400/404 mapping; both extended tests pass |
| `TestGetTraceReturnsCompleteTraceAndPreservesTypedUsage` and `TestGetTraceRejectsInvalidOTLPIdentity` | MCP ignores anchorSpanId; invalid anchors reach the reader | Parse and forward typed SpanID; preserve missing-target error and content opt-in; both extended tests pass |

The final public SQLite fixture contains 125 activities. Three added native spans report failures, and the requested span `000000000000006e` occurs beyond the first 100 activities. Two logs share its span ID, including one at the same timestamp. A request with page offset 10, size 5, and tail enabled returns offset 111 with the ordered span IDs `006c`, `006d`, `006e`, correlated log `006e`, and `006f`. Total count 125 and full trace time extent remain unchanged. The fixture tests existing production storage and `GetTrace`, not a mock query implementation.

The rank helper test uses the `gotests -only '^traceSpanOffset$' -use_go_cmp` scaffold and keeps its args/want types. The generated `Store.GetTrace` scaffold is not retained because it copies `sync.Mutex` fields and conflicts with Go vet. Public behavior is verified by extending existing GetTrace integration tests. Adapter tests extend their existing transport test fixtures; no test-only production interfaces or helper services are added.

Test sources: [rank helper](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/trace_test.go), [GetTrace integration](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/store_test.go), [Connect](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/connectapi/server_test.go), [HTTP](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/httpapi/handler_test.go), [MCP](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/mcpserver/server_test.go).

---

## 3. Verification result and scope

| Check | Result |
| --- | --- |
| `go test ./internal/query ./internal/storage/sqlite ./internal/transport/connectapi ./internal/transport/httpapi ./internal/transport/mcpserver -count=1` | PASS for all five packages; temporary Go build cache; local-listener permission for existing httptest servers |
| `buf generate` | PASS using the configured remote generators; Go and TypeScript generated files updated without manual edits |
| `buf lint` | PASS |
| `git diff --check` | PASS |

The default sandbox blocks `httptest` listening and access to the remote Buf generator. Approved escalated runs complete both operations. Those environment failures are not counted as red tests.

This entry covers task 2.1 backend behavior. It does not claim completion of Web focus/return behavior, all-episode presentation, comparison, filters, content classification, or trace overview. Task checkboxes remain under the coordinating agent's verification.

---

## 4. Tasks 3.2 and 3.3: shared comparison snapshot and transports

`Store.CompareRework` opens one read transaction. A shared loader resolves each requested session to its canonical root and reads its summary timing, token totals, full retained diagnostic activities, projection coverage, and harness context through that transaction. The pure query comparison executes once against those two snapshots. `GetSessionRework` reuses the loader in its existing transaction so single-session and paired diagnostics use the same evidence construction.

| Surface | Added contract |
| --- | --- |
| Connect | `CompareRework` with explicit baseline/current `{source_id, session_id}` references |
| MCP | `compare_rework` with explicit baseline/current `{source, runId}` references; read-only annotation and discovery guidance |
| Result | Eligibility status/reason, resolved summaries, coverage, harness context, five metric rows with nullable numerator/denominator/value/delta |
| Content | No prompts, tool bodies, raw attributes, episodes, or activity lists in comparison results |
| Compatibility | `compare_runs` retains its independent 1–10-run aggregate semantics and dimension defaults; timeline/trace content remains opt-in |

The additive protobuf schema was regenerated with the configured Buf generators. Neither generated Go nor TypeScript was edited manually. Adapters map the shared calculation without recomputing or rounding metrics. Invalid same-root, cross-source, and overlapping pairs remain structured eligibility results; unknown sessions remain errors.

Implementation: [snapshot loader](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/rework_comparison.go), [Connect adapter](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/connectapi/rework_comparison.go), [MCP adapter](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/mcpserver/rework_comparison.go), [protobuf](https://github.com/kotokumu/agentmetry/blob/main/proto/agentmetry/v1/agentmetry.proto).

---

## 5. Comparison red and green evidence

| Existing test or generated scaffold | Observed red | Correction and green |
| --- | --- | --- |
| `TestGetSessionReworkAnalyzesTheCompleteStoredSession`, comparison extension | Stub comparison incorrectly returned success for missing baseline/current sessions | Load both subjects through the transaction and propagate `ErrConversationNotFound`; both cases pass |
| `TestConnectServerMapsSessionReworkWithoutInventingOptionalValues`, real SQLite/transport subtest | Connect reports unimplemented for the seeded comparison | Implement additive RPC parsing, shared reader call, and mapping; fixed five-row oracle passes |
| Same real SQLite/transport subtest | MCP reports unknown tool `compare_rework` after Connect is green | Register read-only tool and map the same query result; fixed oracle and Connect/MCP parity pass |
| `Test_mapReworkComparisonValue` | Generated scaffold initially has no cases; this mapper test does not claim a separate observed red | `gotests -only '^mapReworkComparisonValue$' -use_go_cmp` scaffold preserves args/want types; fractional `50.04` and absent numerator/value pointers pass with `protocmp` |

The seeded transport test uses a real temporary SQLite Store and actual Connect and MCP HTTP clients. Its baseline contains a model response followed by failed validation, edit, repeated failed validation, edit, and successful validation. The current session contains a longer model response, a successful independent validation, failed validation, edit, and successful validation. Both have 600 reported tokens and 6,000 milliseconds of activity effort. The inputs are explicit synthetic canonical fixtures, not captured upstream telemetry; synthetic journal envelopes are not evidence of provider parsing coverage.

Expected metrics are calculated from that fixture independently of the production comparison helper:

| Metric | Baseline numerator / denominator → value | Current numerator / denominator → value | Current minus baseline |
| --- | --- | --- | --- |
| Initial validation success proxy | 0 / 1 → 0% | 1 / 2 → 50% | +50 percentage points |
| Rework token share | 500 / 600 → 250/3% | 300 / 600 → 50% | −100/3 percentage points |
| Retry cycle effort share | 5,000 / 6,000 → 250/3% | 3,000 / 6,000 → 50% | −100/3 percentage points |
| Tool failure rate | 2 / 5 → 40% | 1 / 4 → 25% | −15 percentage points |
| Recurring loops per 100 validations | 1 / 3 → 100/3 | 0 / 3 → 0 | −100/3 |

The test asserts all five row IDs, units, availability states, raw operands, unrounded values, and deltas on Connect, then verifies the same values through MCP. It checks six/five canonical events, reported harness identity/counts, and absence of prompt, tool, and context sentinels from both wire results. A sparse session preserves absent numerator/value/delta and its reported denominator of 10 on both transports. Parent/child references resolve to the same canonical root and are rejected by the shared comparison. Missing references and missing sessions are also covered.

The same stored fixture verifies legacy MCP behavior with 1 and 10 runs, cross-source overlapping periods, default and explicit dimensions, rejection of 0 and 11 runs, body opt-in, and page-size 101 rejection. The legacy wall-duration oracle follows observed summary timestamps (5,000 milliseconds and zero for the single-activity session), not summed activity effort; an initial test expectation incorrectly conflated those quantities and was corrected without changing production aggregate behavior.

Tests: [stored transport parity and compatibility](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/connectapi/server_test.go), [nullable mapper scaffold](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/connectapi/rework_comparison_test.go), [session/root resolution](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/store_test.go). The independently implemented [snapshot verification](https://github.com/kotokumu/agentmetry/blob/main/openspec/changes/improve-otel-investigation/snapshot-verification.md) records a deterministic concurrent writer test, a failing negative-control overlay, and a passing race run. The snapshot test was added after implementation and is explicitly regression-detection evidence rather than an original TDD red.

---

## 6. Comparison verification result

| Check | Result |
| --- | --- |
| `go test ./internal/query ./internal/storage/sqlite ./internal/transport/connectapi ./internal/transport/httpapi ./internal/transport/mcpserver -count=1` | PASS for all five packages on 2026-09-04; includes full stored transport parity, legacy compatibility, and snapshot coherence |
| `buf generate` | PASS through configured remote generators with network permission |
| `buf lint` | PASS |
| `git diff --check` | PASS |

New public Store acceptance cases extend the existing integration fixtures; the previously recorded mutex-copy scaffold exception still applies. No test-only production abstraction was added. These entries establish tasks 3.2/3.3 backend behavior only; coordinating verification owns task checkboxes and Web completion.

---

## 7. Task 5.1: structured session conditions

`SessionConditions` adds observed failure, inclusive optional minimum/maximum elapsed milliseconds, exact model, and exact tool predicates. Validation rejects negative, nonfinite, inverted, or overlong input. Each canonical root accumulates independent model/tool/failure matches from all meaningful member activities before pagination. Source, time, and existing text search remain AND conditions. Outcome detection reuses the query layer's existing observed-success interpretation; absent outcome is not failure.

The SQLite reader streams metadata and, when failure matching requires it, stored attributes. It does not select content/body columns, but attributes may themselves contain captured content; this is an internal read rather than a body-free storage guarantee. Model/tool predicates can be satisfied by different parent/child activities. Duration uses the parsed earliest activity start and latest activity end across the group. Missing/invalid endpoints, including the epoch sentinel used for unreported times, cannot satisfy duration predicates. Elapsed duration is not summed span effort.

The fixture exposed an existing limitation: lexicographic MIN/MAX of variable-width RFC3339 fractions can reverse rollup timestamps, such as `.1Z` and `.11025Z`. Structured duration matching parses timestamps and therefore handles the 10.25ms boundary correctly. This change does not rewrite stored rollups or alter legacy summary timestamp calculation; that existing limitation remains separate follow-up work.

`ListSessionsRequest.conditions` and `ListSessionsResponse.applied_conditions` are additive messages. MCP `list_runs`/`get_agent_sessions` accepts `conditions` and returns `appliedConditions`. Storage emits the exact validated conditions only after applying them; transports relay that acknowledgement. A transport cannot manufacture support by echoing request fields. Empty/default conditions retain the prior behavior and omit acknowledgement. MCP dashboard aggregates keep their existing range/source/search scope; structured predicates apply to the session list.

| Test / inspection | Red or baseline | Green / result |
| --- | --- | --- |
| Generated `TestValidateSessionConditions` scaffold | Eight invalid-input cases returned nil from the initial stub | All eleven cases pass, including zero/equal bounds, fractions, NaN, infinities, negatives, inverted bounds, and 200-byte model/tool caps |
| Existing `TestSessionAggregationGroupsBeforePagination`, structured subtest | Unfiltered results included missing-time/unknown-outcome roots; no acknowledgement; inverted range succeeded | Two canonical matches traverse distinct size-one pages behind newer nonmatches; parent model and child tool combine; source/text/duration/failure AND and exact 10.25ms boundaries pass without duplicate roots |
| Existing `TestConnectServerUsesOpaqueSessionPageToken` | Conditions were ignored; no acknowledgement mapped; invalid ranges accepted | Exact values and pre-existing predicates forwarded; storage acknowledgement mapped; absent acknowledgement stays absent; invalid/nonfinite/overlong values return InvalidArgument |
| Existing `TestGetOverviewUsesSharedRangeAndQueryContract` | Conditions were ignored, acknowledgement absent, inverted range accepted | Same query conditions and acknowledgement through MCP; prior range/source/search behavior remains |
| Stored Connect/MCP fixture in `TestConnectServerMapsSessionReworkWithoutInventingOptionalValues` | Added after focused predicate/mapping red cycles | Both real endpoints select the explicit `baseline` identity with failure, model/tool on separate spans, exact 6,000ms first-start-to-last-end duration, source, text, and page size one. Both acknowledge every condition; actual MCP rejects inverted bounds |
| EXPLAIN QUERY PLAN through a temporary Go test overlay over real Store schema | Inspection only, not a test assertion | Metadata query streams spans/logs and uses the existing `session_memberships` primary-key index for root association. No schema/index change. The overlay is under `/tmp/agentmetry-session-condition-plan` |

The final five-package command `go test ./internal/query ./internal/storage/sqlite ./internal/transport/connectapi ./internal/transport/httpapi ./internal/transport/mcpserver -count=1` passes on 2026-09-04. `buf generate` passes with configured remote generators. The query validator uses the `gotests -only '^ValidateSessionConditions$' -use_go_cmp` scaffold without changing its args/want types. Store and transport acceptance tests extend existing fixtures; the documented Store mutex-copy exception remains applicable.

Sources: [query conditions](https://github.com/kotokumu/agentmetry/blob/main/internal/query/session_conditions.go), [full-set metadata predicate](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/session_conditions.go), [SQLite acceptance](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/store_test.go), [Connect and actual MCP fixture](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/connectapi/server_test.go), [MCP mapping acceptance](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/mcpserver/server_test.go).

---

## 8. Task 7.1: body-free trace overview and focused detail window

`GetTraceOverview` returns timing, identity, kind/status, and missing-parent metadata for at most 5,000 activities. It retains the trace's full total and extent and reports complete/partial coverage independently of the returned count. Missing-parent evaluation queries all retained native spans; a parent outside the overview cap still satisfies its child. The wire type has no content, token, cost, or raw-attribute field. Stored outcome attributes are inspected only to derive status and are not returned.

`GetTraceWindow` has a distinct RPC so older servers cannot silently ignore new predicates on legacy `GetTrace`. It accepts a pair of optional inclusive timestamps, one validated existing activity-kind string, `errors_only`, and the existing bounded page. Interval matching uses overlap: an activity is included unless it ends before the requested start or starts after the requested end. Filtering scans metadata for the full trace; only selected page identities are passed to the existing detail loader. The response carries the ordinary full trace summary and selected activities plus `matching_activities`; full trace total/extent remain unchanged. Native trace details also carry optional `missing_parent` presence from the same full-set metadata scan, including activities beyond the 5,000-row overview cap. Exact span navigation continues through `GetTrace.anchor_span_id`.

| Evidence | Result |
| --- | --- |
| Generated query tests | A temporary policy stub failed five range/kind validation cases and four intersection/outcome cases; restored validation and inclusive intersection pass all cases |
| Real Store fixture | Independent traces of 12, 200, 1,200, and 5,001 spans return exact totals/counts; the final trace returns 5,000 rows with partial coverage |
| Parent completeness | The first returned child references native span 5,001 and is not marked missing; the next references an absent ID and is marked missing |
| Range/detail | `[+4s,+5s]` includes four tool spans under inclusive overlap, including a long span beginning at zero; page size two reports four matches and keeps full total 12. Selected body content is loaded only for the page |
| Failure | Both a status-backed error and an attribute-only `success=false` activity match. Hydrated detail status is the canonical `error`; unknown outcomes do not satisfy the query predicate |
| Actual Connect | Overview and window outputs match Store counts/coverage; malformed/missing trace, incomplete range, unsupported kind, and page size 101 map to their specified errors |

`buf generate` and `buf lint` pass using the configured generators. The generated Go and TypeScript files were not edited manually. Tests use synthetic canonical spans and a real temporary SQLite Store, not provider-live telemetry. Test sources: [query range rules](https://github.com/kotokumu/agentmetry/blob/main/internal/query/trace_test.go), [Store and actual Connect](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/trace_overview_test.go), [metadata/detail reads](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/trace_overview.go), [Connect adapter](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/connectapi/trace_investigation.go).

---

## 9. Trace-count and elapsed-duration regression follow-up

An OTLP fixture with trace ID `00000000000000000000000000000999` projects a prompt, an unknown-kind received context activity, and a response. Before the correction, `GetTrace` returned all three bodies while its trace rollup reported two; overview/window inherited the smaller total. Trace rollups now count every stored trace span and log, including unknown-kind evidence. Session rollups continue their separate meaningful-session policy. Revising a session activity to unknown removes it from the session summary while retaining its source-qualified trace evidence.

Opening an existing database compares each derived trace count with stored spans/logs. A mismatch rebuilds affected derived trace data, including totals, roots, missing parents, status, participants, and agents. The regression test writes the former two-count state and proves reopening repairs it to three rather than leaving the UI-visible inconsistency.

The same follow-up corrects structured session elapsed time. Span evidence now contributes its actual start and end; log evidence contributes its observation time to both endpoints. A single ten-second span therefore satisfies an exact 10,000ms inclusive condition instead of appearing as 0ms. Invalid, missing, reversed, and epoch-sentinel endpoints remain excluded.

| Focused regression | Result |
| --- | --- |
| `TestOTLPTraceCountsUnknownKindAcrossDetailOverviewAndWindow` | Red: detail 3, summary 2. Green: detail, overview, and full total are 3; the unknown-kind window has one result; reopening repairs a deliberately stale two-count rollup |
| `TestSpanRevisionToUnknownRemovesEmptySessionButRetainsTraceEvidence` | PASS; session becomes absent while trace keeps one unknown-kind activity with count one |
| `TestSessionAggregationGroupsBeforePagination/structured conditions…` | Red: a single 10s span did not match exact 10,000ms. Green: it matches once with inclusive bounds |

---

## 10. Trace-window detail metadata regression follow-up

The window filter and returned detail now share the same canonical outcome and parent evaluation. Before the correction, an activity selected through `attributes.success=false` kept its empty hydrated status, so the Web client could render it as healthy despite `errors_only`. The selected detail now receives canonical `error` status from the metadata predicate.

`Activity.missing_parent` is an additive optional wire field. `GetTraceWindow` sets it for every returned native trace span: `true` means the referenced parent is absent, while `false` means the parent is retained or the span is a root. Logs and older endpoints leave it absent to distinguish unassessed relationships from an assessed false result. The value comes from the uncapped metadata scan, so a detail page beyond the 5,000-row overview cap is still accurate.

| Focused regression | Result |
| --- | --- |
| Store error-only window | Red: raw `Error` and empty statuses survived hydration. Green: status-backed and attribute-only failures both return canonical `error` |
| Store off-cap detail | Red: native span 5,001 had no parent-assessment presence. Green: its absent parent is returned as present `true`; an assessed root is present `false` |
| Actual Connect | Canonical statuses and both optional-bool presence/value states match the Store result through generated protobuf types |
