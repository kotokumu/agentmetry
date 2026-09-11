## Why

Agentmetry retains every accepted OTLP export together with canonical observations and query projections, but it has no lifecycle for aging that data out of the active database. The current local profile therefore grows continuously. Users need a safe middle state that removes historical query materialization while preserving lossless telemetry for later recovery, followed by predictable physical deletion when the configured retention period ends.

## Intended Outcomes

- Users can configure when accepted telemetry leaves active query storage and when it is permanently deleted.
- Archived telemetry keeps only a lossless, verified representation sufficient for later restoration; it does not remain searchable or contribute to active observations, projections, or aggregates.
- Users can inspect archive periods and restore selected archived periods into active query storage.
- Automatic archive and deletion, manual restore, and recovery from interrupted lifecycle operations preserve or recover intact authority until authorized deletion completes; an operation that starts with corrupt content preserves remaining bytes and verifiable lifecycle metadata without claiming payload recovery.
- Existing installations do not begin deleting telemetry until a retention policy is explicitly enabled.

## Success Criteria

- SC-1: After an enabled archive cutoff is reached and scheduled maintenance succeeds, eligible exports no longer contribute to Web, HTTP, or MCP query results and remain represented by verified archive data without observations or projections.
- SC-2: Restoring an archived segment or receive-time period verifies every selected payload against its recorded size and SHA-256, applies the current normalizer, and republishes the resulting current observations and query projections. A payload that still fails current normalization becomes an active failed raw export without derived data.
- SC-3: A selected finite retention hold begins only after a restore is completely published, blocks both archival and deletion without changing original receive time, and makes the restored export immediately eligible for the normal age policy when the hold expires.
- SC-4: After an enabled deletion cutoff is reached, a previously archived Archive Segment is removed from archive inventory and a later restore request reports that it was deleted by retention expiry; no restorable raw content remains under Agentmetry's storage ownership.
- SC-5: Before authorized deletion completes, cancellation, storage exhaustion, or process termination preserves or recovers the previously verified authoritative copy for an operation that begins with intact authority. An operation that begins with corrupt content preserves remaining bytes and verifiable lifecycle metadata without claiming payload recovery. If corruption is newly detected, the operation makes no destructive change to remaining copies and reports the operation, target, and reason; recovery is guaranteed only when another verified copy exists. Completed authorized deletion may leave no restorable copy.
- SC-6: A disabled or unconfigured retention policy starts no automatic archive or deletion work and leaves ingestion unchanged; existing archive inventory and manual restoration remain available.
- SC-7: When a policy is first enabled or shortened, telemetry already older than both cutoffs is archived and verified in one successful maintenance operation but cannot be deleted until a later successful scheduled operation.
- SC-8: Enabled retention is evaluated on application startup and at least once in each subsequent 24-hour interval while the application remains running; eligible work and failures are visible in retention status.
- SC-9: Disabling a policy stops automatic archive and deletion without moving data; archive inventory and manual restore remain available. Lengthening a policy recalculates future eligibility for retained Active and Archived exports and never recreates Deleted data.
- SC-10: A restore operation publishes all selected Archived exports and removes them from archive inventory only after every payload passes integrity validation and every current normalization outcome is prepared; current normalization failure is a valid Active raw-only outcome. If any selected payload fails integrity validation or cannot be read, the operation publishes none and leaves the archive unchanged.
- SC-11: An automatic archive operation revalidates the current enabled policy immediately before changing stable query visibility, and an automatic deletion operation does so immediately before physically deleting archive content. Disabling or lengthening the policy cancels work that the current policy no longer authorizes, while a completed transition is not reversed automatically. Manual restore does not require an enabled policy and instead revalidates target integrity, exclusive transition authority, and the selected hold before publication.
- SC-12: Each maintenance or hold-expiry evaluation fixes one UTC evaluation instant. A cutoff or hold of `N` whole days is exactly `N × 24 hours`, and a hold expires at its completed-publication instant plus that duration. Later clock correction affects only a later evaluation and never reverses a completed transition.
- SC-13: Later payload verification that finds the sole Archived copy corrupt marks it corrupt and unrestorable, blocks restoration and the current destructive operation, and reports the loss of integrity without claiming recovery. Corruption does not create a retention exemption: a later scheduled deletion may remove the Archive Segment when its identity and membership metadata remain verifiable and the current policy authorizes expiry.
- SC-14: A restore request is rejected as a conflict when another restore owns transition authority over any export whose state or Archive Segment membership the request could change. An overlapping automatic expiry releases its authority and is cancelled when the restore claims the set before physical deletion's serialized point of no return; when deletion passes that point first, the affected exports complete as Deleted. For a receive-time period, matching Active exports are reported unchanged and receive no new hold, matching Deleted exports are reported as deleted and excluded, and matching Archived exports form the atomic restore set. No matching Archived export produces no data change.
- SC-15: Every Maintenance Cycle advances each Retained Export by at most one stable lifecycle state, including data already older than both cutoffs and data whose restore hold has expired.
- SC-16: When a period restore selects only part of an Archive Segment, the original segment becomes superseded only after one or more replacement segments partition every unselected member exactly once and exclude every selected member. The replacement relation is acyclic and every traversal terminates at current or terminal segment references. Current inventory replaces the original identity with the replacement identities, and a later request for the original identity reports its transitively resolved current replacements. A full-segment restore records the original identity as historically restored without asserting that its former members remain Active forever.
- SC-17: At one reported measurement instant, retention capacity status separately reports operating-system allocated bytes for all Agentmetry database, archive, and staging files; mutually exclusive SQLite page bytes assigned to Active raw, observations, and query projections; allocated bytes for current Archive Segment files; reusable SQLite freelist bytes; and filesystem-available bytes. Component values do not sum to total allocation because total allocation also includes metadata, write-ahead logs, operation files, and filesystem allocation granularity. Before archive or restore, the status reports estimated additional peak filesystem allocation or an unavailable reason and warns when an available estimate exceeds available bytes; capacity never authorizes early archive or deletion.

## Scope

### In Scope

- An opt-in policy with whole-day archive and deletion cutoffs from 1 through 36,500 days, where the deletion cutoff is strictly later than the archive cutoff and age is evaluated in UTC from immutable export receive time; invalid values are rejected without changing the active policy.
- Automatic transition of complete accepted exports from active storage to lossless archive storage.
- Removal and deterministic regeneration of observations, projections, relationships, and aggregates affected by an archive or restore transition.
- A verified, lossless, raw-only archive representation with bounded physical Archive Segments.
- Archive inventory by Archive Segment identity, receive-time bounds, export count, size, verification state, and scheduled deletion time without exposing archived session content.
- Manual restoration by Archive Segment or a UTC receive-time period `[start, end)`. Period restoration selects every Archived complete export whose receive time is in the interval, including exports in Archive Segments that also contain unselected exports; it reports matching Active exports unchanged, matching Deleted exports as deleted, and any matching Restoring export as a conflict for the entire request. Successful restoration removes only the selected Archived exports from archive inventory after publishing them Active, applies a finite hold selected as a whole number from 1 through 36,500 days, and reports no matching Archived export without changing data.
- Automatic physical deletion of expired archive segments.
- Crash-safe, idempotent recovery and truthful progress or failure reporting for archive, overlapping restore, and deletion operations; restoration preempts concurrent expiry before physical deletion's serialized point of no return for every Archive Segment it must retain or replace.
- An Archive Segment is deletion-eligible only when every contained export has reached the deletion cutoff and no restore operation owns transition authority over any contained export.
- Policy disablement and edits that preserve archive inventory and manual restoration; shortening follows the staged transition guarantee, while lengthening changes only future eligibility.
- Existing-database compatibility and migration of storage metadata required to enable the lifecycle.

### Out of Scope

- Manual deletion of any active or archived telemetry, including sessions, exports, Archive Segments, and periods.
- Session-level archive selection, archive browsing, or session-level restoration.
- Querying archived telemetry without restoring it.
- Capacity-triggered early archival or deletion; initial capacity behavior is limited to reporting and warning.
- An indefinite retention exemption for restored data.
- Remote archival, cloud synchronization, encryption at rest, or an external archive service.
- Producer-side retention or deletion of Claude Code, Codex, or other source data.

## Capabilities

### New Capabilities

- `telemetry-retention`: Owns the user-observable post-admission Active-to-Archived, Archived-to-Restoring-to-Active, and Archived-to-Deleted transitions; receive-time policy; archive inventory; period restoration and hold; permanent expiry; and failure/recovery guarantees. Initial Active persistence remains owned by `otlp-ingestion`; retention is separate because its consumer trigger is a configured lifecycle or restore action after admission, not receipt of an OTLP request.

### Modified Capabilities

- `otlp-ingestion`: `[[otlp-ingestion/lossless-raw-export-retention]]` — Qualify indefinite active-journal retention so a successfully admitted export may later move to verified archive storage or be permanently deleted only under the telemetry-retention lifecycle.

## Affected Concepts

| Concept | Candidate owner capability | Change |
|---|---|---|
| Retained Export | otlp-ingestion | Change from an indefinitely active journal row to a lossless export that may reside in Active or Archived storage until authorized expiry. |
| Retention Policy | telemetry-retention | Add archive and deletion cutoffs measured from export receive time and explicit enablement. |
| Archive Segment | telemetry-retention | Add the immutable, verified grouping that contains complete export envelopes without query materialization. |
| Retention Hold | telemetry-retention | Add a finite protection period applied to restored exports without changing original receive time. |
| Lifecycle Operation | telemetry-retention | Add durable archive, restore, delete, failure, and recovery states visible through maintenance status. |

## Decisions Required

None. The approved scope uses complete exports; immutable `received_at` and fixed-instant UTC policy evaluation; current-policy revalidation before automatic archive publication or deletion; half-open period or Archive Segment restoration; current-normalizer replay, including current failure behavior; a finite non-mutable hold that blocks archive and deletion until expiry; conflict rejection for overlapping restores; explicit corrupt-and-unrestorable archive reporting without redundancy; an opt-in policy; and no capacity-triggered or manual deletion in the initial change.

## Impact

The change affects users who operate long-lived local Agentmetry profiles, the retention guarantee of every accepted OTLP export, archive and maintenance status exposed by the local application, and historical results visible through Web, HTTP, and MCP queries after data leaves or re-enters Active storage. It introduces a storage-format and data-lifecycle migration and must preserve current ingestion acknowledgement, active-query semantics, source provenance, pricing authority, and failure recovery. Existing telemetry remains untouched until the user enables a policy.
