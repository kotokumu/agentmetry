# Live Telemetry Updates

- Status: Implemented; release validation pending
- Scope: Connect query API, SQLite projections, and Lit dashboard

## Requirement Summary

Agentmetry must converge an already-open dashboard to newly committed OTLP
telemetry without a page or desktop application restart. Session summaries are
small and bounded, so the first session page may be refreshed as a snapshot.
Session and trace activities can be large and must be synchronized as bounded
mutations without discarding already loaded pages or moving the user's reading
position.

The live protocol is a projection invalidation protocol, not a second telemetry
transport. Connect server streaming announces which read-model scopes changed.
Unary Connect RPCs remain authoritative for snapshots and activity
synchronization. The feed is intentionally not tied to OTLP exports so future
non-OTLP projections can participate in the same ordered stream.

## Functional Requirements

1. A transaction that changes a visible read projection appends one ordered
   projection commit covering every affected typed target. OTLP telemetry,
   plan-usage imports, and future projection writers use the same commit model.
2. The dashboard overview refreshes after any visible telemetry invalidation.
3. The current filtered first page of at most 100 sessions refreshes when a
   session scope changes.
4. Refreshing the session page preserves the explicitly or implicitly selected
   session when that session still exists.
5. An open session synchronizes activity mutations and its summary only when its
   source-qualified session reference is affected.
6. An open trace synchronizes activity mutations and its summary/topology only
   when its trace ID is affected.
7. Activity synchronization supports both `UPSERT` and `REMOVE`. A span update
   replaces the existing activity with the same stable identity. A span that
   leaves a session produces a removal for the old session and an upsert for the
   new session.
8. Log records keep their current append semantics. Equal log payloads received
   twice remain two activities with different identities.
9. Initial snapshot and watch startup must not have a gap. Reconnection resumes
   from the last applied cursor. Duplicate invalidations and mutations are
   harmless.
10. When a cursor belongs to another projection generation, has fallen outside
    retained history, or cannot be resumed,
    the server returns an explicit resynchronization event. The client reloads
    visible snapshots, discards affected sync cursors, and subscribes from the
    new position.
11. Each affected open activity view refreshes a bounded 100-item head window,
    merges keyed mutations with the resident window, and restarts historical
    paging from the new head token. This preserves loaded evidence without
    re-fetching an unbounded session or trace.
12. Session activity order is newest first. Trace activity order is oldest first.
    Both use the same stable total-order key so equal timestamps do not reorder.

## Non-Functional Requirements

- Normal commits become visible within one second under normal local load.
- The server forms bounded change windows over contiguous projection commits
  instead of emitting one network message per database commit. A live window
  closes after 100 milliseconds, 256 commits, or 1,024 distinct targets,
  whichever comes first. Catch-up windows close immediately at a bound.
- The client forms a separate 300-millisecond render window. Since the timer is
  started by the first pending event and is not reset by later events, a
  continuous stream cannot postpone UI convergence.
- Activity synchronization and page responses remain bounded to 100 items.
- Delivery is at-least-once and ordered within a stream. Exactly-once delivery is
  not required; final convergence is required.
- Slow clients must not cause unbounded in-memory queues. Durable projection-feed
  reads are authoritative; in-memory notification is only a constant-memory wake-up
  optimization.
- The durable projection feed retains at most 50,000 commits and approximately
  128 MiB of target/mutation payload estimates. A subscriber older than the
  earliest retained cursor receives `ResyncRequired` instead of replaying an
  unbounded backlog.
- Stored target payload is capped at 2 KiB per commit. Retention accounts for
  target JSON and activity-mutation identity fields; SQLite row/index/WAL
  overhead is monitored separately. Higher rates remain safe but may require
  resynchronization sooner.
- Each projection commit stores one versioned target payload row, never one row
  per target. Exact targets are normalized, deduplicated, and converted to typed
  coarse targets before persistence when they exceed 1,024 entries or 2 KiB.
- Target coarse-graining is lossless for invalidation: `all_sources` dominates
  source targets, `all_sessions` dominates session targets, and `all_traces`
  dominates trace targets. It never lets one resource kind suppress another.
- The in-memory notification queue is O(1); per-subscriber stream/cursor state is
  O(N). The local server supports at most eight concurrent live subscribers and
  rejects additional subscribers with `RESOURCE_EXHAUSTED`.
- SQLite uses one serialized writer and a bounded read pool of four connections.
  Feed readers never hold a read transaction while waiting for a notification.
- Each loaded activity state retains at most 2,000 activities and renders at most
  200 rows at once.
- One activity catch-up cycle processes at most 10 pages or 1,000 mutations.
  Exceeding a bound performs an explicit scoped snapshot replacement and adopts
  the snapshot interval cursor; it never advances a cursor over retained state.
- Stream cancellation must follow the request and process contexts so application
  shutdown is not delayed by live clients.
- A frontend cursor is acknowledged only after every mounted affected projection
  applies the window successfully. Temporary query failures retain the targets
  and retry with bounded backoff from the last acknowledged cursor.
- Session summaries, session activity pages, and trace responses are each built
  from one read-only SQLite snapshot so summary and page metadata cannot cross
  commit boundaries.
- Public cursors are opaque strings. Clients must not parse storage positions.

## Inputs and Outputs

| Input | Output |
|---|---|
| current projection position | opaque projection cursor |
| watch request after a cursor | ordered invalidation windows or resync requirement |
| session ref and activity sync interval | bounded activity mutations, current session summary, next cursor |
| trace ID and activity sync interval | bounded activity mutations, current trace summary, next cursor |
| activity page request | bounded page and an opaque continuation token |

## Normal Cases

- A new run causes the session page to refresh, the new session to appear first,
  and the previously selected session to remain selected.
- A new activity for the open session updates its summary and is merged by stable
  activity identity.
- A completed or corrected span updates the rendered activity rather than adding
  a duplicate.
- A new span or log for the open trace updates trace counts, status, topology, and
  its keyed resident activity window while resetting continuation from a fresh
  bounded head page.
- Closing the desktop window only hides it. The stream remains connected, so
  reopening displays converged data without restarting the application.

## Error and Edge Cases

- Failed normalization is journaled but does not append a projection commit.
- A transaction rollback emits no invalidation.
- Metrics invalidate the overview but do not force a session or trace refresh.
- Plan-usage commits target overview and plan-usage scopes without pretending to
  be OTLP telemetry.
- Late telemetry is ordered by its activity timestamp and stable identity, not by
  arrival order, while its sync mutation is selected by commit position.
- A stale generation, malformed cursor, or cursor ahead of the current feed
  produces `ResyncRequired`; it never silently starts from “now”.
- A stream failure keeps the last good data visible and retries from the last
  cursor with bounded exponential backoff.
- Activity pagination and live synchronization are serialized per view. A live
  update cancels an in-flight page and obtains a new bounded head/token before
  paging resumes, so stale offsets are never continued after a mutation window.
- When a resident activity set has reached 2,000 items, the next live update
  replaces it with the contiguous authoritative head. This keeps opaque paging
  tokens aligned with the represented window.

## Acceptance Criteria

1. With the dashboard open, ingesting a new session shows it without reload.
2. Hiding and reopening the desktop window shows sessions received while hidden.
3. Ingesting a new activity for the selected session changes only the affected
   summary/list/detail regions and preserves route and scroll context.
4. Updating an existing span does not create a duplicate activity.
5. Moving an updated span between sessions removes it from the old loaded view
   and adds it to the new view.
6. Two logs with identical fields remain distinguishable and render twice.
7. Late and equal-timestamp activities are neither skipped nor duplicated across
   snapshot paging and live synchronization.
8. A change committed between bootstrap-position acquisition and snapshot loading
   is eventually reflected.
9. Disconnecting and reconnecting the stream replays missed invalidations, and a
   generation mismatch causes an explicit resync.
10. More than 100 mutations are returned over bounded pages and converge to the
    same state as a fresh snapshot.
11. A continuous burst is coalesced into at most one frontend update window per
    300 ms.
12. Existing unary HTTP, MCP, navigation, pagination, and desktop shutdown tests
    continue to pass.
13. A burst of 10,000 projection commits is delivered as bounded change windows,
    does not allocate a per-subscriber queue, and converges to the final cursor.
14. A subscriber more than 50,000 commits behind receives one resync requirement
    rather than an unbounded replay.
15. A commit affecting more than 1,024 distinct targets produces a bounded coarse
    invalidation that safely refreshes every currently visible affected scope.
16. Projection-change storage plateaus after retention cleanup; cleanup does not
    double p99 commit latency relative to the same ingest without cleanup.
17. Eight concurrent subscribers converge independently. A ninth is rejected
    without increasing writer latency or delaying existing subscribers.
18. A ten-minute activity stream does not exceed 2,000 resident activities or
    200 rendered rows per open view, including while away from the live edge.
19. Under the supported envelope, activity synchronization sustains at least twice
    the mutation arrival rate. Beyond its catch-up bounds it performs one explicit
    scope resync and never leaves a silent cursor gap.
20. Baseline long-session ingestion is measured before live-feed acceptance. A
    100,000-activity session must not exhibit unbounded per-commit growth in
    rollup latency; incremental rollups are a prerequisite if this gate fails.

## Non-Goals

- Producer-side deduplication of repeated OTLP logs.
- Exactly-once network delivery.
- Cross-process notification for another process writing the same SQLite file;
  the ownership lock continues to require one writer.
- Replacing Connect with SSE or WebSocket.
- Keeping every historical page mounted in the DOM.

## Conceptual Model

| Concept | Meaning | State | Behavior | Constraint / Invariant |
|---|---|---|---|---|
| Projection generation | Identity of one built SQLite projection | opaque generation ID | rejects cursors from another projection | stable across normal restart; changes after rebuild |
| Projection commit | Atomic record that one transaction changed visible query results | sequence, time, versioned typed-target JSON | advances the feed | stored in the same transaction as projection changes |
| Projection position | Latest committed projection change considered by the read model | generation and sequence | orders invalidations | monotonically advances within one generation |
| Projection cursor | Public representation of a projection position | opaque token | encode/decode and validate | never parsed by clients |
| Change target | Typed invalidation scope | overview/source/session/trace/plan-usage or a coarse typed scope | identifies readers to refresh | never uses stringly typed resource kinds |
| Projection change window | Targets affected in one contiguous cursor interval | from-exclusive/through-inclusive cursor and bounded target set | closes on time, commit count, or target count and deduplicates targets | contains no activity payload |
| Feed retention window | Resumable suffix of projection commits | earliest and latest retained sequence | compacts old change metadata | cursors before the window require resync |
| Change window policy | Capacity and time limits for one streamed interval | 100 ms, 256 commits, 1,024 targets, 2 KiB | closes and coarse-grains windows | all dimensions are bounded |
| Activity capacity window | Bounded resident and rendered subset | 2,000 resident, 200 rendered | evicts or restarts paging beyond bounds | summary remains authoritative |
| Session reference | Source-qualified session identity | source ID and session ID | equality and filtering | both fields are required |
| Activity identity | Stable identity of one projected activity | opaque ID | keyed replacement/removal | stable within a projection generation |
| Activity mutation | Change required to converge a loaded activity set | upsert activity or remove ID | applies idempotently | exactly one operation kind |
| Activity sync interval | Commit-ordered mutation range | after/through cursor | bounded paging | independent from display-time ordering |
| Activity page window | Bounded historical browsing boundary | scope, offset token, ordered activities | loads older/newer pages | token is reset from the head after live mutation |
| Resync requirement | Cursor cannot be resumed safely | reason and latest cursor | triggers scoped snapshot reload | no silent gap |

## Relationships

| Concept A | Relationship | Concept B | Notes |
|---|---|---|---|
| projection generation | namespaces | projection cursor | prevents cursor reuse after rebuild |
| projection commit | advances | projection position | any visible writer can participate |
| projection commit | contains | change targets | target rows and projection writes are atomic |
| projection change window | unions | change targets | tells consumers what to synchronize |
| feed retention window | bounds | replay | old subscribers resynchronize |
| change window policy | bounds | projection change window | limits latency, payload, and work |
| activity capacity window | bounds | activity state and DOM | prevents time windows from hiding memory growth |
| activity sync interval | returns | activity mutations | commit order selects changes |
| activity mutation | targets | activity identity | reducer is idempotent |
| activity page window | coexists with | activity sync interval | live mutation invalidates and restarts the paging boundary |

## Responsibility Assignment

| Responsibility | Owner | Reason to change | Not owner |
|---|---|---|---|
| derive affected old and new scopes | projection writer | projection storage/upsert semantics change | Connect handler |
| append commit and typed targets atomically | SQLite projection feed | feed schema/retention changes | UI |
| persist projection position and row revision | SQLite store | projection schema changes | UI |
| read durable invalidations and wait for commits | projection change reader | delivery/replay policy changes | transport DTO mapper |
| normalize/coarse-grain target sets | projection change target set | target algebra or size policy changes | transport handler |
| enforce feed retention in chunks | projection feed store | resume/capacity policy changes | projection writer |
| validate opaque cursors | cursor value object | cursor version or generation changes | controllers |
| map query values to protobuf stream | Connect server | public transport contract changes | SQLite store |
| own the single frontend stream and reconnect | live update controller | browser lifecycle/retry changes | session/trace controllers |
| refresh bounded session snapshots | conversations controller | session-list behavior changes | live stream controller |
| merge session activity mutations | session activity state | session ordering/view behavior changes | API client |
| merge trace activity mutations | trace activity state | trace ordering/view behavior changes | API client |
| preserve bounded reading UX | activity table | presentation/scroll behavior changes | storage |
| bound resident activity state | keyed activity state | UI capacity policy changes | stream controller |

## SOLID Risk Assessment

| Principle | Risk | Mitigation |
|---|---|---|
| SRP | Connect handler or app element becomes an update orchestrator | keep cursor/feed policy and frontend stream coordination in dedicated modules |
| OCP | stringly generic event framework makes every consumer defensive | use a closed protobuf enum plus validated scope fields, with backward-compatible enum additions |
| LSP | SQLite implementation silently drops gaps while a fake reader does not | define ordered, replayable, explicit-resync contract in the reader interface |
| ISP | existing broad backend interface grows for every sync operation | add consumer-oriented change and activity-sync reader interfaces |
| DIP | UI or transport learns export IDs and SQLite row IDs | expose opaque cursors and activity identities only |

Capacity policy is centralized in the feed, transport, and activity-state
modules, and its bounds are covered by contract/load tests.

Procedural risks are concentrated in projection commit scope derivation and
frontend mutation merging. Scope derivation belongs beside span upsert state;
mutation behavior belongs in keyed activity state rather than event handlers.

## Module and Package Boundaries

| Module | Change |
|---|---|
| `internal/query` | projection cursor, typed targets, invalidation, mutation, sync filter/result values and small reader interfaces |
| `internal/storage/sqlite` | additive one-row-per-commit projection feed, metadata/revision columns, old/new scope capture, stable activity IDs, chunked retention, indexed change/sync queries, writer/read-pool split, wake-up primitive |
| `internal/transport/connectapi` | cursor validation mapping and server-streaming handler |
| `proto/agentmetry/v1` | watch/bootstrap/sync messages and RPCs; `Activity.activity_id` |
| `web/src/api` | async watch client and sync RPC mappings |
| `web/src/controllers` | one live-update coordinator plus keyed session/trace activity state |
| `web/src/components` | render a bounded activity row window and preserve selection/navigation state |

Dependency direction remains `web -> Connect contract -> query ports <- SQLite`.
Tauri continues to host the same HTTP UI and does not own live-update semantics.

## Implemented Interfaces and Signatures

```go
type ProjectionChangeReader interface {
	CurrentProjectionPosition(context.Context) (ProjectionPosition, error)
	ReadProjectionChanges(context.Context, ProjectionPosition, int, int) (ProjectionChangeWindow, error)
	WaitForProjectionChange(context.Context, ProjectionPosition) error
}

type ActivitySyncReader interface {
	SyncSessionActivities(context.Context, SessionActivitySyncFilter) (ActivitySyncPage, error)
	SyncTraceActivities(context.Context, TraceActivitySyncFilter) (ActivitySyncPage, error)
}
```

```proto
rpc WatchProjectionChanges(WatchProjectionChangesRequest)
    returns (stream WatchProjectionChangesResponse);
rpc SyncSessionActivities(SyncSessionActivitiesRequest)
    returns (SyncSessionActivitiesResponse);
rpc SyncTraceActivities(SyncTraceActivitiesRequest)
    returns (SyncTraceActivitiesResponse);
```

The watch response carries a fixed `through_cursor`, repeated enum-typed target
messages, and explicit resync fields. Sync requests carry `after_cursor`, an
optional fixed `through_cursor`, scope, and a bounded page token. Sync responses
carry mutations, the fixed through cursor, resync fields, and page information.

`Activity` gains an opaque `activity_id`. SQLite derives span identity from the
span logical key and log identity from the durable log row. Both are namespaced
by the projection generation in the public encoding.

## Boundary Decisions

| Boundary | Hidden detail | Reason |
|---|---|---|
| projection cursor | projection generation and feed sequence | permits storage evolution and rebuild detection |
| activity ID | span composite key or log row identity | prevents infrastructure keys from becoming API contracts |
| change reader | SQLite projection feed, retention, windowing, and wake-up channel | transport only consumes ordered change semantics |
| sync reader | revision columns and mutation derivation SQL | controllers only apply mutations |
| frontend live controller | Connect async iterable, retry schedule, and render windows | feature controllers receive windowed typed changes |

## Test Specifications

| Behavior | Given | When | Then | Level |
|---|---|---|---|---|
| atomic invalidation | projection commit succeeds/fails | transaction completes/rolls back | only success becomes watch-visible | storage integration |
| old/new scope | an existing span changes session | sync old and new refs | old receives remove; new receives upsert | storage integration |
| late activity | old timestamp arrives after cursor | sync changes | mutation is returned and display order is stable | storage integration |
| stable identity | a span is resent and two equal logs arrive | sync changes | one span ID is replaced; two log IDs remain | storage integration |
| no startup gap | cursor is read, then commit occurs before snapshot | snapshot and watch run | commit is represented by snapshot or replay, possibly both | API integration |
| reconnect | stream stops after a known cursor | client reconnects | missed scopes replay in order | API/UI controller |
| invalid generation | cursor belongs to rebuilt projection | watch starts | explicit resync event is returned | API integration |
| bounded sync | 101 mutations affect one session | pages are loaded | each page is at most 100 and final state contains all 101 | API integration |
| bounded event burst | 10,000 commits arrive before a subscriber reads | stream catches up | contiguous bounded windows reach the final cursor without a subscriber queue | storage/API integration |
| target overflow | a range contains over 1,024 distinct targets | window is closed | explicit targets are replaced by safe typed coarse targets | query/storage integration |
| retention expiry | subscriber is over 50,000 commits behind | watch resumes | one resync event is returned, with no backlog replay | API integration |
| continuous UI burst | changes continue across render windows | coordinator runs | refresh cadence is bounded by the non-resetting 300 ms timer | UI controller |
| compact commit storage | 50,000 commits contain exact and coarse targets | retention is full | feed has one bounded payload row per commit, not per target | storage integration |
| chunked retention | feed crosses high-water marks repeatedly | cleanup runs | DB/WAL reaches a plateau and no hot-path vacuum occurs | storage load |
| subscriber limit | eight streams are active | ninth connects | ninth receives resource exhausted; active streams and writer remain healthy | API integration |
| bounded activity state | continuous mutations arrive | resident state grows | resident and DOM counts remain within policy | UI load |
| catch-up overload | mutation arrival exceeds drain capacity | catch-up bound is reached | scoped snapshot replaces resident state and advances to its explicit cursor | UI/API integration |
| long session baseline | one session reaches 100,000 activities | more commits arrive | commit/rollup p95 and p99 do not grow without bound | storage load |
| stable paging | a live insert occurs between activity pages | live sync completes | in-flight paging is canceled and continuation restarts from a fresh bounded head token | controller integration |
| list refresh | a burst affects several sessions | render window closes | list refreshes once and selection remains | UI controller |
| bounded head merge | a user has loaded historical pages | new mutations arrive | resident evidence is keyed/deduplicated and paging restarts at the current head | component/controller |
| trace convergence | a span changes status/topology | trace sync completes | summary and keyed activities match fresh trace state | end-to-end |

Invariant tests cover monotonic positions, opaque cursor round trips, stable
total ordering for equal timestamps, idempotent mutation application, and silent
gap prohibition. Error tests cover malformed/ahead/generation-mismatched cursors,
stream cancellation, sync paging cancellation, and unavailable reconnects.

## Detailed Design

1. Add projection metadata containing a persistent generation ID. Normal startup
   retains it; a regenerated projection creates a new one.
2. Add `projection_changes` with exactly one row per visible projection commit.
   The row contains a versioned JSON target payload capped at 2 KiB. A
   projection writer allocates one sequence; normalizes, deduplicates, and
   coarse-grains its typed targets; and records the payload in the same SQLite
   transaction as the changed projection rows. OTLP, plan usage, and future
   writers share this mechanism. The public cursor encodes generation plus sequence.
3. Projection rows record their creating/updating projection sequence through
   additive columns. A small tombstone table records only scope removals that
   cannot be derived from current rows; activity bodies are never copied into the
   change feed.
4. Derive invalidation scopes from new input and the previous stored scope of
   upserted spans. Failed normalization has no visible projection commit.
5. Retain approximately the latest 50,000 commits and 128 MiB of accounted
   change payload. Cleanup runs only at a 512-commit boundary and deletes up to
   1,000 expired commits per chunk until both bounds converge. Mutation bytes
   are accounted before the final cleanup check. It never runs
   `VACUUM` on the write path. Expired cursors produce `ResyncRequired`, and
   activity mutations follow the same boundary through a foreign key cascade.
6. After a successful commit, rotate a store-owned notification channel. Watch
   readers always query the durable feed before waiting and recheck position
   after acquiring the channel, preventing publish-before-wait races. The channel
   carries no event payload and remains constant-memory regardless of subscribers.
7. The stream reader forms a cursor window `(from, through]` over contiguous
   commits. A live window closes after 100 ms, 256 commits, or 1,024 distinct
   targets; catch-up closes immediately on a bound. Targets are deduplicated and
   the serialized window is capped at 2 KiB.
   Target overflow is expressed with typed coarse targets rather than a larger
   message. “Window” is the protocol concept; no domain write is called a batch.
8. An empty watch emits an authoritative bootstrap checkpoint with coarse
   overview/session/trace targets. Feature controllers load bounded snapshots
   for that checkpoint, record its cursor, then apply later mutation intervals.
   This permits duplicates but no missing interval.
9. Activity pages use a stable total order. After a live mutation, feature
   controllers cancel any in-flight offset page and restart continuation from a
   fresh bounded head token, preventing stale-offset continuation.
10. Sync APIs select changed activity identities by commit interval, collapse
   repeated mutations to the latest operation for each target scope, and return
   bounded `UPSERT`/`REMOVE` pages plus current summaries. Session and trace
   projection tables have composite indexes beginning with scope identity and
   projection sequence. Tombstones have the equivalent scope/sequence indexes.
   Every page revalidates that its fixed interval is still retained.
11. A single frontend controller reconnects and forms 300 ms render windows over
   incoming change windows with a capped target set. Feature controllers
   serialize pagination and sync work, apply mutations through keyed state, and
   refresh one bounded head window to renew paging continuation. The coordinator
   advances its replay cursor only after all mounted consumers resolve; failed
   applications are retried from the prior acknowledged cursor.
12. Each feature catch-up is limited to 10 pages and 1,000 mutations. Crossing a
   limit replaces the scoped resident state with an authoritative bounded head
   snapshot and adopts the server-provided through cursor.
13. Keyed state keeps at most 2,000 activities and ActivityTable renders at most
   200 rows. Loaded sub-windows are navigated without mounting the entire
   resident collection. At capacity, a live mutation replaces resident state
   with a contiguous head window so its opaque continuation remains valid.
14. The store uses a single writer connection and a four-connection read pool.
   Live streams are capped at eight. Notification queue memory is O(1), while the
   explicitly bounded subscriber state is O(N).
15. Session and trace summaries use materialized rollups and membership tables.
   Append-only commits and monotonic existing-span revisions apply deltas;
   scope moves or non-monotonic corrections use a conservative scoped rebuild.
   Revised session time extrema are repaired with indexed scope/time lookups.
16. Session summaries infer omitted parent-agent links only for non-root agents
   still missing an explicit parent, using at most 64 evidence spans and 64
   indexed ancestor steps. Summary/member/activity reads share one read snapshot.

## TDD Construction Plan

| Behavior | Red test | Minimal implementation | Refactor target |
|---|---|---|---|
| cursor and stable identity | query value-object tests | opaque codecs and identity values | remove primitive cursor strings from core |
| atomic revision/scopes | SQLite commit tests | projection feed, generation, scope capture | projection commit concept |
| activity mutations | SQLite sync tests | bounded delta queries | shared keyed activity ordering |
| stable snapshot pages | storage pagination tests | opaque bounded continuation tokens | page value object |
| Connect contracts | in-memory Connect server tests | proto mapping and stream loop | separate streaming mechanics from mapping |
| frontend reducer | pure reducer tests | keyed upsert/remove/order | shared mutation state utility |
| live coordinator | controller tests with fake async stream | bootstrap/retry/render windows | isolate retry clock |
| burst and retention | storage/API load-oriented tests | bounded windows, target caps, retention reset | isolate window/retention policies |
| capacity windows | frontend load tests | resident/DOM caps and scoped resync | activity capacity policy |
| subscriber/read scaling | API/storage load tests | writer/read pool and eight-stream cap | connection policy |
| long-session write path | storage benchmark | incremental rollup if baseline gate fails | session aggregate ownership |
| session/trace wiring | component/controller tests | targeted refresh/sync and bounded resident state | keep components I/O-free |
| vertical slice | app E2E ingest-after-load test | final service wiring | remove duplicated setup |

## Resolved Risks and Open Questions

- A numeric revision alone is not public; generation prevents reuse after rebuild.
- Stream notification and activity data remain separate contracts.
- Old and new span scopes are both invalidated.
- Activity page and sync cursors have different roles.
- Hidden desktop windows keep the connection; OS/network suspension uses replay.
- OTLP, plan usage, and future read projections share one ordered feed without
  sharing their payload schemas.

## Capacity Review and Load Gates

The event-volume architecture review found the cursor-window model sound after
adding capacity boundaries. The supported initial envelope is 100 sustained
projection commits/second, 1,000 commits/second burst, eight live subscribers,
and 1,000 activity mutations/second for one hot visible scope. OTLP record rate
and export commit rate are measured separately because exporter batch shape can
change commit pressure by orders of magnitude.

Required load suites run at 10, 100, and 1,000 commits/second with 4, 256, and
1,024 targets; a 10,000-commit burst; 1/8/32 attempted subscribers; 100/500/1,000
mutations/second; retention rollover beyond 100,000 commits; one commit with
10,000 distinct targets; and one 100,000-activity session. They record commit
p50/p95/p99, visibility p99, cursor lag, CPU, RSS, goroutines, DB/WAL bytes,
window count, sync query rows, and rendered row count.

Pass conditions are: no cursor or target loss; normal visibility p99 at most one
second; refresh rate at most 3.34/second; feed DB/WAL size plateaus after the
second retention window; retention cleanup p99 is less than twice baseline;
fast subscribers are unaffected by slow ones; activity sync has 2× headroom;
and state/DOM counts never exceed their declared capacity windows. Thirty-two
subscribers are a rejection/load-isolation test, not a supported concurrency
level.

The implementation was reviewed against these gates before release. Targeted
benchmarks on an Apple M5 Pro measured an existing-span revision in a
100,000-activity hot session at about 0.71 ms, a 100,000-span session summary at
about 0.16 ms, a 100,000-activity trace head read at about 1.01 ms, and a single
10,000-span commit at about 0.85 s. These numbers
are regression baselines, not cross-machine latency guarantees.
