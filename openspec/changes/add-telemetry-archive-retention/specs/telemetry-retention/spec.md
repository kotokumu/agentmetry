## Purpose

Defines how Agentmetry automatically moves aged retained exports out of active
query storage, restores selected archived exports, and permanently expires
archive content without losing the authoritative copy before deletion.

## ADDED Requirements

### Requirement: retention-policy

Agentmetry MUST accept only a valid opt-in Retention Policy and MUST derive
automatic archive and deletion eligibility from immutable receive time.

- **Input and Acceptance**:

  | Partition | Condition or range | Acceptance or result |
  |---|---|---|
  | Disabled update | Retention is explicitly disabled and both cutoffs are omitted | Accept; start no automatic archive or deletion transition |
  | Invalid disabled update | Retention is explicitly disabled and either cutoff is supplied | Reject and keep the current policy unchanged |
  | Valid enabled update | Retention is enabled, both cutoffs are whole numbers from 1 through 36,500 days inclusive, and deletion is strictly greater than archive | Accept as a new policy revision |
  | Invalid enabled update | Retention is enabled and either cutoff is missing, outside 1 through 36,500 days, not a whole number, or deletion is less than or equal to archive | Reject and keep the current policy unchanged |

- **Behavior Rules**:
  - A Maintenance Cycle MUST fix one UTC evaluation instant at its start.
  - Absence of a configured policy MUST start no automatic archive or deletion work.
  - The age of an export MUST equal the fixed evaluation instant minus its immutable `received_at`.
  - Agentmetry MUST classify Retention Eligibility using this complete decision table:

    | Rule | Condition: Lifecycle disposition | Condition: Effective hold | Condition: Age at evaluation instant | Output or response | Side Effects |
    |---|---|---|---|---|---|
    | Protected Active | Active | Yes | Any | Not archive-eligible | None |
    | Young Active | Active | No | Less than archive cutoff | Not archive-eligible | None |
    | Aged Active | Active | No | Greater than or equal to archive cutoff | Archive-eligible | None |
    | Young Archived | Archived without restore Transition Authority | Not applicable | Less than deletion cutoff | Not deletion-eligible | None |
    | Aged Archived | Archived without restore Transition Authority | Not applicable | Greater than or equal to deletion cutoff | Individually deletion-eligible | None |
    | Restoring | Archived with restore Transition Authority | Not applicable | Any | Report the derived transient Restoring state and do not allow an automatic transition | None |
    | Deleted | Deleted | Not applicable | Any | Not eligible for an automatic transition | None |
  - A Retention Hold of `N` days MUST expire at its completed-publication instant plus exactly `N × 24 hours`; at or after that instant the original receive-time rules apply again.
  - Event, span, log, metric, and provider timestamps MUST NOT affect retention age.
- **Side Effects**: Disabling or lengthening a policy recalculates future eligibility but does not restore Archived data or recreate Deleted data. Shortening a policy may make existing data eligible but does not bypass the required Active-to-Archived-to-Deleted sequence.
- **Concurrency and Idempotency**: An automatic transition MUST revalidate the current policy revision immediately before stable archive publication or physical deletion. If disabling or lengthening the policy removes authority, the operation MUST stop without publishing that transition. A completed transition MUST NOT be reversed automatically by a later policy change or clock correction.

#### Scenario: Enable retention for old active exports [happy]

- **GIVEN** a user has Active exports older than both proposed cutoffs and no enabled policy
- **WHEN** the user enables valid archive and deletion cutoffs
- **THEN** the policy is accepted and those exports become archive-eligible without becoming directly deletion-eligible in Active state

#### Scenario: Reject equal cutoffs [error]

- **GIVEN** a user has an enabled valid Retention Policy
- **WHEN** the user submits equal archive and deletion cutoffs
- **THEN** Agentmetry rejects the update and preserves the existing policy

#### Scenario: Evaluate the exact archive boundary [boundary]

- **GIVEN** an Active export without a hold has age exactly equal to the archive cutoff at the cycle's fixed UTC instant
- **WHEN** Agentmetry evaluates retention eligibility
- **THEN** the export is archive-eligible

#### Scenario: Disable retention without disabling restore [compatibility]

- **GIVEN** archive inventory exists under an enabled policy
- **WHEN** the user disables retention
- **THEN** Agentmetry starts no new automatic archive or deletion transitions and keeps inventory and manual restoration available

### Requirement: retention-scheduling-and-status

Agentmetry MUST schedule enabled retention predictably and expose Maintenance
Cycle results separately from lifecycle work-unit results.

- **Preconditions**: Automatic scheduling applies only while a valid Retention Policy is enabled.
- **Behavior Rules**:
  - Agentmetry MUST start a Maintenance Cycle during application startup.
  - While the application remains running, Agentmetry MUST start at least one later Maintenance Cycle in every 24-hour interval measured from the preceding cycle start.
  - Each cycle MUST select a fixed cohort of complete exports present at cycle start; exports accepted afterward MUST wait for a later cycle.
  - A Maintenance Cycle MUST be a parent record and MUST NOT itself hold Transition Authority. Its child archive or deletion Lifecycle Operations own fixed affected-export sets and authority independently.
  - A setup or cohort-evaluation failure MUST produce a failed cycle. After successful evaluation, a cycle with no child work MUST complete. A cycle with children MUST remain running until every child is terminal; it MUST then fail if any child failed, MUST be cancelled if every child was cancelled, and MUST otherwise complete.
  - Retention status MUST expose every running Maintenance Cycle, the newest 100 terminal Maintenance Cycles in reverse completion order, every pending or running Lifecycle Operation, and the newest 100 terminal Lifecycle Operations in reverse completion order. A cycle record MUST include identity, status, start instant, fixed UTC evaluation instant, cohort size after successful evaluation or an unavailable reason after evaluation failure, child counts by status, completion instant when terminal, and an error summary when failed. Each operation record MUST include operation identity and kind, status, request instant, affected export and segment counts, and an error reason when applicable. Automatic child operations MUST also include their UTC evaluation instant; manual restores MUST NOT fabricate one.
- **Concurrency and Idempotency**: Concurrent scheduling MUST NOT create two automatic operations with transition authority over the same export. Re-running a cycle after interruption MUST preserve the one-transition-per-export-per-cycle rule.
- **Failure Handling**: A failed cycle MUST remain visible until displaced by 100 newer terminal Maintenance Cycles and MUST NOT prevent a later scheduled cycle. A failed Lifecycle Operation MUST remain visible until displaced by 100 newer terminal Lifecycle Operations.

#### Scenario: Run maintenance after startup [happy]

- **GIVEN** a user starts Agentmetry with a valid enabled Retention Policy
- **WHEN** startup completes
- **THEN** Agentmetry starts a Maintenance Cycle and exposes its status

#### Scenario: Exclude concurrent ingestion from a fixed cohort [concurrency]

- **GIVEN** an automatic archive cycle has selected its export cohort
- **WHEN** Agentmetry admits another OTLP export during that cycle
- **THEN** the new export remains Active and is first eligible for selection by a later cycle

#### Scenario: Continue after a failed cycle [error]

- **GIVEN** the latest scheduled cycle failed and its reason is visible
- **WHEN** the next scheduling interval is reached while retention remains enabled
- **THEN** Agentmetry starts a later cycle without hiding the prior failure result

### Requirement: automatic-archive

Agentmetry MUST move each eligible Active Retained Export to a verified raw-only
Archive Segment without exposing partial query state.

- **Preconditions**: A valid Retention Policy is enabled and the cycle's fixed cohort contains one or more complete archive-eligible Active exports.
- **Behavior Rules**:

  | Current state | Trigger or event | Guard | Next state | Output or Side Effects |
  |---|---|---|---|---|
  | Active | Maintenance Cycle processes the export | Current policy still authorizes archive and no effective hold exists | Archived | Publish an intact Archive Segment, then remove the export's observations, projections, relationships, and aggregate contributions from Active storage |
  | Active | Maintenance Cycle processes the export | Current policy no longer authorizes archive or a hold is effective | Active | Leave raw and derived state unchanged and report the export as skipped |

  Unlisted transitions MUST NOT be performed by automatic archive.
- **Invariants**:
  - Agentmetry MUST keep at least one verified authoritative raw copy for every selected export until stable Archived publication completes.
  - An Archive Segment MUST contain complete exports and MUST NOT contain observations, query projections, relationships, aggregates, or archived session content.
  - Queries MUST observe either the complete Active outcome or its complete absence after Archived publication, never a partial removal.
- **Side Effects**: A successful operation updates archive inventory and every affected active aggregate. It MAY group multiple selected exports into one or more finite immutable Archive Segments.
- **Concurrency and Idempotency**: Each selected export MUST advance at most once and by only Active-to-Archived during its Maintenance Cycle. A retry after successful publication MUST recognize the Archived state without duplicating logical exports.
- **Failure Handling**: Cancellation, storage exhaustion, verification failure, or process termination before complete publication MUST leave or recover the prior complete Active state and MUST report the operation, target, and reason.

#### Scenario: Archive an eligible export [happy]

- **GIVEN** a cycle contains an eligible Active export with a successful Current Normalization Outcome
- **WHEN** automatic archive completes successfully
- **THEN** its raw export appears in an intact Archive Segment and none of its observations or projections contributes to Web, HTTP, or MCP queries

#### Scenario: Do not archive and delete in one cycle [boundary]

- **GIVEN** an Active export is older than both configured cutoffs
- **WHEN** its first successful lifecycle cycle archives it
- **THEN** it remains Archived until at least one later successful scheduled cycle

#### Scenario: Cancel after writing an archive candidate [error]

- **GIVEN** an archive candidate has been written but stable Archived publication has not completed
- **WHEN** the operation is cancelled
- **THEN** Agentmetry preserves or recovers the complete Active export and does not expose the candidate as authoritative inventory

### Requirement: archive-inventory-and-capacity

Agentmetry MUST expose bounded archive inventory and capacity information without
making Archived telemetry queryable or using capacity as deletion authority.

- **Behavior Rules**:
  - Each archive inventory entry MUST expose only its Archive Segment identity, minimum-inclusive and maximum-inclusive immutable receive times, export count, stored byte size, latest Payload Integrity, latest Lifecycle Metadata Integrity, and scheduled deletion time or its unavailable reason under the current policy.
  - Inventory MUST NOT expose archived observations, projections, signal/source breakdowns, session identities, or raw payload content.
  - Every capacity response MUST report one measurement instant. Total allocation MUST use operating-system allocated bytes for all Agentmetry database, archive, and staging files. Active raw, observation, and query-projection bytes MUST use mutually exclusive allocated SQLite pages assigned to those classes. Archive Segment bytes MUST use operating-system allocated bytes for current segment files. Reusable database bytes MUST use SQLite freelist pages, and available bytes MUST use the containing filesystem's available-byte measure.
  - The component measures MUST NOT overlap one another and MUST NOT be presented as a partition that sums to total allocation; metadata, write-ahead logs, operation files, and allocation granularity account for the remainder.
  - Before archive or restore begins, capacity status MUST report estimated additional peak filesystem allocation or an explicit unavailable reason. It MUST warn when an available estimate exceeds available filesystem bytes.
  - A scheduled deletion time MUST be recalculated after an accepted policy change. It MUST be unavailable with a reason while retention is disabled or Lifecycle Metadata Integrity is unverifiable.
- **Invariants**:
  - Capacity state MUST NOT make an export archive-eligible or deletion-eligible.
- **Failure Handling**: Insufficient capacity MAY reject or fail archive or restore work before publication, but MUST NOT cause early deletion. A failed integrity check MUST change the segment's inventory integrity state and expose the failure reason without exposing payload content.

#### Scenario: Inspect archive inventory [happy]

- **GIVEN** a user has intact and corrupt Archive Segments
- **WHEN** the user opens archive inventory
- **THEN** Agentmetry shows their periods, counts, sizes, integrity states, and current scheduled deletion times without archived session or telemetry content

#### Scenario: Distinguish reusable and allocated bytes [boundary]

- **GIVEN** the active database contains reusable pages while its filesystem allocation remains unchanged
- **WHEN** the user inspects capacity status
- **THEN** Agentmetry reports reusable database bytes separately from filesystem-allocated bytes

#### Scenario: Warn without early expiry [error]

- **GIVEN** a restore estimate exceeds currently available storage
- **WHEN** Agentmetry reports the capacity condition
- **THEN** it does not treat any Archive Segment as deletion-eligible before its receive-time cutoff

### Requirement: archive-restoration

Agentmetry MUST restore exactly the selected Archived exports whose validation
succeeds and MUST publish their current Active outcomes atomically.

- **Preconditions**: Archive inventory remains available regardless of whether a Retention Policy is enabled.
- **Input and Acceptance**:

  Scope classification:

  | Partition | Condition or range | Acceptance or result |
  |---|---|---|
  | Current segment scope | One Archive Segment identity in `current` reference state | Classify as a restore scope and select every Archived export in that segment |
  | Superseded segment lookup | One Archive Segment identity in `superseded` reference state | Classify as lookup-only and report its transitively resolved current replacement identities |
  | Restored segment lookup | One Archive Segment identity in `restored` reference state | Classify as lookup-only and report historical restore completion and that the identity is no longer current |
  | Deleted segment lookup | One Archive Segment identity in `deleted` reference state | Classify as lookup-only and report deletion by retention expiry |
  | Unknown segment lookup | An unrecognized Archive Segment identity | Classify as lookup-only and report not found |
  | Valid period scope | UTC `start < end` | Classify as a restore scope and select retained exports with `received_at >= start` and `received_at < end` |
  | Invalid period | Missing bound or `start >= end` | Reject without changing data |

  Hold applicability:

  | Rule | Condition: Scope classification | Condition: Hold | Output or response | Side Effects |
  |---|---|---|---|---|
  | Valid restore | Current segment or valid period | Whole number from 1 through 36,500 days inclusive | Proceed with restore validation | None before publication |
  | Invalid restore hold | Current segment or valid period | Omitted, below 1 day, above 36,500 days, or not a whole number | Reject | No data or hold change |
  | Valid lookup | Lookup-only segment identity | Omitted | Return the classified lookup result | No data or hold change |
  | Invalid lookup hold | Lookup-only segment identity | Supplied | Reject | No data or hold change |

- **Behavior Rules**:
  - For period scope, Agentmetry MUST classify matching exports in this order:

    | Rule | Condition: Matching Restoring export | Condition: Matching Archived export | Output or response | Side Effects |
    |---|---|---|---|---|
    | Conflict | Present | Any | Reject the entire request as conflicting | No data or hold change |
    | Restore | Absent | One or more | Report matching Active exports unchanged and Deleted exports as deleted; use all matching Archived exports as one atomic restore set | Publish the Archived set only after all common rules succeed |
    | No archived match | Absent | None | Report matching Active exports unchanged and Deleted exports as deleted; report that no Archived export matched | No data or hold change |

  - The selected restore set MUST contain the matching Archived exports. The affected-export set MUST additionally contain every member of each current Archive Segment that contains a selected export, because the operation can replace that membership.
  - For a segment identity in `deleted` reference state, Agentmetry MUST report deletion by retention expiry. For `superseded`, it MUST follow replacement edges transitively and report every current replacement identity. For `restored`, it MUST report historical completion and that the old identity is no longer current, without making a claim about the former members' current states or changing any hold. An unknown segment identity MUST be reported as not found.
  - Agentmetry MUST verify every selected export against its recorded original size and SHA-256 and MUST support every selected segment representation and payload codec before publishing any result.
  - Agentmetry MUST run the current normalizer for every selected export and prepare its complete Current Normalization Outcome. A current normalization failure MUST prepare an Active raw export with current failed status and error and no derived data.
  - Agentmetry MUST publish every selected export's verified raw data, Current Normalization Outcome, observations, projections, relationships, and aggregate effects as one complete restore result.
  - The selected hold MUST become effective for every restored export only at complete publication and MUST NOT change its original receive time.
- **Invariants**:
  - The operation MUST retain the pre-operation Archived bytes and lifecycle state until complete Active publication; it MUST retain verified payload authority only while Payload Integrity remains intact.
  - Unselected exports in an intersected Archive Segment MUST retain verified Archived authority.
  - Replacement segments MUST contain every unselected member exactly once and no selected member. The replacement relation MUST be acyclic and every traversal MUST terminate at a current, restored, or deleted Segment Reference State.
  - Queries MUST NOT observe prepared raw or derived state before complete publication.
- **Side Effects**: Complete publication removes only restored exports from archive inventory. When it restores every member of an original segment, Agentmetry MUST remove that segment from current inventory and record its identity in `restored` reference state. When a period cuts a segment, Agentmetry MUST publish one or more verified replacement segments containing every unselected member before marking the original segment superseded. Current inventory MUST replace the original identity with the replacement identities, and a later request for the original identity MUST report its transitively resolved current replacements.
- **Concurrency and Idempotency**: Before preparation, a restore MUST acquire Transition Authority over its complete affected-export set. Another restore whose affected-export set intersects that authority MUST conflict as a whole. An overlapping automatic expiry MUST release authority and cancel when the restore claims the set before physical deletion's serialized point of no return. If deletion passes that point first, it MUST complete the affected exports as Deleted and the restore MUST report them deleted. A repeated request after exports are Active MUST report them unchanged and MUST NOT extend, shorten, or replace their hold.
- **Failure Handling**: If any selected payload is unreadable, corrupt, unsupported, or cannot be completely prepared or published because of capacity or storage failure, Agentmetry MUST publish none of the restore set, MUST preserve its bytes, metadata, inventory membership, and stable Archived state, and MUST report the failing target and reason. It MUST update integrity state when validation discovers corruption and MUST NOT claim that a corrupt payload is verified authority. Current normalization failure alone is not an operation failure.
- **References**: [related] [[otlp-ingestion/lossless-raw-export-retention]]; [related] [[otlp-ingestion/atomic-persistence-and-normalization-failure]]

#### Scenario: Restore one intact segment [happy]

- **GIVEN** a user selects one intact Archive Segment and a seven-day hold
- **WHEN** restore completes with the current normalizer
- **THEN** every export in the segment becomes Active together, disappears from archive inventory, and is protected until exactly seven 24-hour periods after publication

#### Scenario: Restore a period cutting two segments [boundary]

- **GIVEN** a UTC receive-time interval contains Archived exports from parts of two segments
- **WHEN** the user restores that interval
- **THEN** Agentmetry restores exactly the Archived exports inside `[start, end)` and keeps every outside export in verified archive inventory

#### Scenario: Preserve a current normalization failure [error]

- **GIVEN** every selected raw payload passes integrity checks but one export fails the current normalizer
- **WHEN** the restore result is published
- **THEN** that export becomes Active with current failed status and no derived data while the other selected exports publish their complete current outcomes

#### Scenario: Reject a corrupt atomic set [error]

- **GIVEN** a selected restore set contains one payload that fails SHA-256 verification
- **WHEN** the user requests restoration
- **THEN** no selected export becomes Active and the entire archive set remains unchanged

#### Scenario: Reject overlapping restoration [concurrency]

- **GIVEN** a running restore owns transition authority over an export in a second period request
- **WHEN** the user submits the second request
- **THEN** Agentmetry rejects the entire second request as a conflict

#### Scenario: Repeat after Active publication [idempotency]

- **GIVEN** the requested period contains only exports already restored to Active with an existing hold
- **WHEN** the user repeats the restore request with another hold duration
- **THEN** Agentmetry reports the exports already Active and does not modify their hold

### Requirement: automatic-archive-expiry

Agentmetry MUST physically delete an Archive Segment only in a later scheduled
cycle that independently authorizes every member for expiry.

- **Preconditions**: A valid Retention Policy is enabled and the Archive Segment existed before the current Maintenance Cycle.
- **Behavior Rules**:

  | Rule | Condition: Restore owns any member | Condition: Identity and membership verifiable | Condition: Every member reached deletion cutoff | Output or response | Side Effects |
  |---|---|---|---|---|---|
  | Restore priority | Yes | Any | Any | Skip as owned by restoration | No content change |
  | Unsafe authority | No | No | Unknown | Fail the deletion and report unverifiable metadata | No content change |
  | Delete | No | Yes | Yes | Transition the segment's exports from Archived to Deleted | Physically remove archive payload content, remove the segment from inventory, and retain non-content deletion evidence |
  | Not yet eligible | No | Yes | No | Keep Archived and report the next scheduled deletion time | No content change |

  The Delete rule MUST revalidate the current enabled policy immediately before physical removal. Unlisted transitions MUST NOT be performed by automatic expiry.
- **Invariants**:
  - Agentmetry MUST NOT delete an Active export directly.
  - Agentmetry MUST NOT delete a segment containing any export younger than the deletion cutoff.
  - Deletion Evidence MUST contain no raw payload, observations, projections, archived session content, or other restorable representation.
- **Side Effects**: Deletion Evidence MUST preserve enough segment and export identity, immutable receive-time coverage, deletion time, and policy-revision information to report later segment or period restore matches as deleted by retention expiry.
- **Concurrency and Idempotency**: Restore transition authority MUST preempt deletion before physical deletion's serialized point of no return. When preempted deletion has reversibly staged content, Agentmetry MUST restore verified Archived authority and current inventory membership durably before the restore reads or publishes it. Deletion MUST hold that same exclusion boundary from its final authority check through the point of no return. If deletion passes first, it MUST complete as Deleted. A retry after completed deletion MUST preserve Deleted state and MUST NOT recreate inventory or report another content deletion.
- **Failure Handling**: Cancellation or failure before deleting an intact segment completes MUST preserve or recover its verified authoritative content. Failure while deleting an already corrupt segment MUST preserve any remaining bytes and verifiable lifecycle metadata without claiming recoverable payload authority. A corrupt payload discovered by another operation MUST remain reported as corrupt and MUST be processed by a later Delete rule when its metadata and current policy authorize expiry, because corruption grants no retention exemption.

#### Scenario: Delete an eligible intact segment [happy]

- **GIVEN** every export in an Archive Segment is past the deletion cutoff and no restore owns a member
- **WHEN** a later Maintenance Cycle completes expiry
- **THEN** Agentmetry removes the segment's payload content and inventory entry and reports its exports Deleted by retention expiry

#### Scenario: Keep a mixed-age segment [boundary]

- **GIVEN** all but one export in an Archive Segment have reached the deletion cutoff
- **WHEN** automatic expiry evaluates the segment
- **THEN** Agentmetry keeps the entire segment Archived and exposes its later scheduled deletion time

#### Scenario: Let restoration win [concurrency]

- **GIVEN** an eligible Archive Segment has a restore operation with transition authority over one member
- **WHEN** automatic expiry evaluates the segment
- **THEN** Agentmetry skips deletion and leaves the segment content unchanged

#### Scenario: Let committed deletion win [concurrency]

- **GIVEN** automatic expiry crosses physical deletion's serialized point of no return before a restore claims authority
- **WHEN** the restore races with that deletion
- **THEN** Agentmetry completes the affected exports as Deleted, reports them deleted to the restore requester, and applies no hold

#### Scenario: Expire previously reported corrupt payload [error]

- **GIVEN** an Archive Segment payload is corrupt but its identity and complete membership remain verifiable and every member is expired
- **WHEN** a later scheduled deletion revalidates the enabled policy
- **THEN** Agentmetry deletes the corrupt payload and records the exports as Deleted by retention expiry

### Requirement: lifecycle-integrity-and-recovery

Agentmetry MUST preserve truthful archive authority and recover lifecycle
operations without losing the last verified copy before authorized deletion.

- **Behavior Rules**:
  - Every Archive Segment MUST identify a representation version and the codecs required to reconstruct its Retained Exports.
  - Before publishing an archive candidate, Agentmetry MUST reconstruct every member, verify its recorded original size and SHA-256, and verify the segment's complete membership evidence.
  - Payload Integrity MUST be recorded independently from Lifecycle Metadata Integrity so later expiry can distinguish unrestorable content from unverifiable deletion authority.
  - A reader MUST either support and verify a segment's recorded representation and codecs or reject the operation without changing authority.
  - Detecting payload corruption MUST mark the segment corrupt and unrestorable, stop the detecting operation before destructive change, and expose the operation, target, verification time, and reason.
- **Invariants**:
  - An operation that begins with intact payload authority MUST retain or recover that verified authoritative copy after cancellation, storage exhaustion, or process termination until authorized deletion completes.
  - An operation that begins with corrupt payload content MUST preserve any remaining bytes and verifiable lifecycle metadata until authorized deletion completes and MUST NOT claim payload recovery.
  - Duplicate candidate and source copies created by interruption MUST represent one logical Retained Export and MUST NOT both become authoritative.
  - Recovery MUST NOT expose partial observations, projections, relationships, aggregates, inventory, or deletion evidence as a completed transition.
- **Concurrency and Idempotency**: Each durable operation MUST retain its fixed affected-export set and preserve Transition Authority over that complete set while it is running. For restore, the fixed selected restore set MUST remain a subset of that affected-export set. Recovery and retries MUST converge on the same stable state without duplicating logical exports or shortening a hold.
- **Failure Handling**: Recovery from corruption is guaranteed only when another verified complete copy exists. When the sole copy is corrupt, Agentmetry MUST report that no recovery is available. Completed authorized deletion MAY leave no restorable copy.
- **References**: [related] [[otlp-ingestion/lossless-raw-export-retention]]

#### Scenario: Recover an interrupted archive publication [happy]

- **GIVEN** an intact archive candidate and the prior Active source both exist after process termination
- **WHEN** Agentmetry recovers the operation
- **THEN** it verifies and selects one authoritative result without duplicating the logical export or losing its raw data

#### Scenario: Reject an unsupported archive version [compatibility]

- **GIVEN** a restore selects a segment whose recorded representation version is not supported by the current reader
- **WHEN** Agentmetry validates the operation
- **THEN** it rejects the restore and leaves the segment authoritative and unchanged

#### Scenario: Report latent sole-copy corruption [error]

- **GIVEN** an Archived segment has no alternate verified copy and later payload verification fails
- **WHEN** Agentmetry records the verification result
- **THEN** it reports the segment corrupt and unrestorable without claiming recovery or removing the remaining bytes

#### Scenario: Recover a restore interrupted before publication [idempotency]

- **GIVEN** verified raw data and some derived restore results were prepared but complete Active publication did not occur
- **WHEN** Agentmetry recovers or retries the restore
- **THEN** queries still exclude the prepared result until one complete Active outcome is published
