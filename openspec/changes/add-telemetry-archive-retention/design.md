> **Scope:** This file applies only to this Change. After archive, it is out of scope.

# Telemetry archive and retention design

## 0. Document Boundary

| Information | Normative source |
|---|---|
| User-observable lifecycle rules | Delta specifications in this Change |
| Concept meanings and ownership | `model.md` in this Change |
| Public RPC fields and compatibility | `proto/agentmetry/v1/agentmetry.proto` after implementation |
| SQLite columns, constraints, and indexes | `internal/storage/sqlite/schema.hcl` after implementation |
| Implementation approach, responsibility allocation, migration, and tests | This document |

---

## 1. Context

| Constraint | Current fact | Design consequence |
|---|---|---|
| Raw storage | `otlp_exports` stores one independently encoded payload plus replay metadata | Archiving decodes each stored payload and recompresses many raw protobufs in one segment stream. |
| Projection ownership | Observations reference `export_id`; canonical rows and several derived relationships do not consistently retain export ownership | The storage generation must add export ownership and rebuild existing projections before selective archival is safe. |
| Write consistency | `sqlite.Store` serializes writes through `writeGate`; SQLite uses WAL and `FULL` synchronous mode | Lifecycle publication uses the same gate and one SQLite transaction, so readers see the complete state before or after a transition. |
| Replay | `otel.ReplayExport` and replay-candidate finalization already apply the current normalizer and rebuild derived state | Restore preparation reuses this normalization boundary and adds a publication path that preserves the original export identity. |
| File replacement | Compaction already uses durable manifests, file and directory sync, verification, and crash recovery | Archive file installation and expiry use the same durability rules, adapted to segment files. |
| Process ownership | One lock owns the database for the process lifetime | The sibling archive directory is covered by the same ownership lock; no second writer protocol is introduced. |
| Public API | The Web client uses generated Connect RPCs; MCP is an explicit read-only semantic adapter | Retention management uses a separate Connect service and Web settings surface. No MCP mutation tool is added. |

---

## 2. Goals / Non-Goals

### Goals

- Store Archived raw exports in cross-export compressed segments outside the active SQLite payload table.
- Keep lifecycle metadata sufficient for exact receive-time selection, integrity decisions, recovery, and terminal segment lookup.
- Remove and restore export-owned projections in one SQLite publication transaction.
- Recover every interruption point without treating a candidate copy as a second logical export.
- Add a local policy, archive inventory, capacity status, operation history, and asynchronous restore control to the Web application.

### Non-Goals

- Expose Archived telemetry through existing query or MCP tools.
- Add manual deletion, cancellation controls, remote storage, encryption, or capacity-triggered transitions.
- Make archive segment boundaries or compression level a public compatibility contract.
- Preserve historical normalization output inside an Archive Segment.

---

## 3. Behavior and Traceability

### 3-1. Functional Requirements

| ID | Behavior realized by the design | Specification |
|---|---|---|
| FR-1 | Validate, persist, disable, and revise one local receive-time policy | `telemetry-retention/retention-policy` |
| FR-2 | Run fixed-cohort parent cycles at startup and at least every 24 hours; expose all running and bounded terminal cycle history separately from child work-unit history | `telemetry-retention/retention-scheduling-and-status` |
| FR-3 | Write verified raw-only segments and atomically remove export-owned active materialization | `telemetry-retention/automatic-archive` |
| FR-4 | Return content-free inventory and reproducible storage measurements | `telemetry-retention/archive-inventory-and-capacity` |
| FR-5 | Restore a segment or exact receive-time set with current normalization, partial-segment replacement, and finite hold | `telemetry-retention/archive-restoration` |
| FR-6 | Delete a whole eligible segment with renewed authority and terminal evidence | `telemetry-retention/automatic-archive-expiry` |
| FR-7 | Verify versioned representations and recover archive, restore, and deletion operations idempotently | `telemetry-retention/lifecycle-integrity-and-recovery` |
| FR-8 | Preserve admission identity and pre-normalization bytes through archive and restore | `otlp-ingestion/lossless-raw-export-retention` |

### 3-2. Implementation Bounds

| Bound | Value | Verification |
|---|---|---|
| Policy cutoff or restore hold | 1 through 36,500 exact 24-hour days | Value-object boundary tests and RPC validation tests |
| Archive grouping | One UTC receive date and at most 256 MiB of original protobuf bytes per segment; an export is never split | Segment-planner boundary tests |
| Archive inventory page | At most 100 current segments | Connect and storage pagination tests |
| Status response | Every running Maintenance Cycle, newest 100 terminal cycles, every pending/running child operation, and newest 100 terminal child operations | Repository and Connect tests |
| Format decoder allocation | Streaming decode with per-export payload capped by the existing 32 MiB journal limit | Corrupt-length and large-segment tests |

The grouping bound is an internal operational choice. Changing it may create
different future segment identities but does not change existing segment
identity or any selection rule.

---

## 4. Conceptual-Model-to-Implementation Mapping

| Specification concept / Requirement | Owning component | Physical representation | Notes |
|---|---|---|---|
| Retained Export | `sqlite.Store` ingestion path | `retained_exports.id` plus an Active `otlp_exports.id` with the same value, or one current archive membership | The stable integer ID is allocated once and reused when raw data returns to Active. For post-migration admissions it is also the payload occurrence, so deletion cannot make a uniqueness value reusable. |
| Current Normalization Outcome | OTLP normalizer and SQLite projection publisher | Existing observation/projection tables plus export-ownership columns | A failed outcome has only the Active journal row and failed status. |
| Retention Policy | `retention.Service` and SQLite policy repository | Singleton `retention_policy` row with monotonic revision | Constructors enforce range and ordering before persistence. |
| Retention State and Hold | SQLite lifecycle repository | Stable `retained_exports.state`, `hold_until`, and exclusive operation authority | Stored state is only Active, Archived, or Deleted. Restoring is derived from an Archived export's current restore authority, so inventory remains unchanged until publication. |
| Archive Segment and integrity | `archivefs.Store` and SQLite lifecycle repository | One `.tar.zst` file, immutable `archive_segment_members`, and `current_archive_memberships` | Payload hash belongs to the file; current membership is distinct from immutable history. |
| Segment Reference State | SQLite lifecycle repository | Persistent segment row plus `archive_segment_replacements` edges | Terminal rows contain no payload bytes and remain resolvable. |
| Maintenance Cycle | `retention.Scheduler` and `retention.Service` | Parent `retention_cycles` row with fixed UTC instant and child work-unit operation IDs | Exports accepted after cohort creation are outside the cycle; only child work units own transition authority. |
| Restore Scope | `retention.RestoreRequest` | Validated segment ID or UTC `[start,end)` value | Selection queries only the lifecycle catalog, never archived content. |
| Lifecycle Operation and Transition Authority | `retention.Service` and SQLite lifecycle repository | `retention_operations`, immutable `retention_operation_exports`, and current `retention_export_authorities` | A primary key on current authority, rather than a cross-table partial-index predicate, enforces one owner per export. |
| Deletion Evidence | SQLite lifecycle repository | Self-contained terminal fields in `retained_exports` and the prior segment reference | Evidence does not depend on retained operation-history rows. |
| Capacity Report | `capacity.Probe` | SQLite page accounting plus operating-system file and filesystem accounting | All measurements carry one observation time and availability reason. |

---

## 5. Package and Responsibility Design

| Package / module | Responsibility | Owns | Does not own |
|---|---|---|---|
| `internal/retention` | Lifecycle policy, eligibility, operation orchestration, scheduling, and restore selection | Domain value objects, use cases, operation priority | SQL, archive framing, OTLP normalization, Connect messages |
| `internal/archivefs` | Versioned segment representation and durable local file operations | Tar framing, whole-stream zstd, candidate verification, install/rename/remove, directory sync | Policy decisions, query projections, lifecycle selection |
| `internal/storage/sqlite` | Durable catalog, deletion cutover, and atomic Active publication | State/authority constraints, target snapshots, projection ownership and reconciliation, terminal evidence, one `writeGate` publication owner | Scheduling, file compression, or OTLP normalization |
| `internal/ingest/otel` | Current raw replay interpretation | `ExportReplayer` adapter that decodes and prepares a current normalization outcome outside publication | Retention policy, archive state, or SQLite publication |
| `internal/capacity` | Reproducible storage accounting | OS allocation, filesystem availability, SQLite class accounting, staging estimates | Eligibility or deletion decisions |
| `internal/transport/connectapi` | Local RPC adaptation | Validation-error mapping and bounded response conversion | Lifecycle rules and file access |
| `web/src` | Retention settings and operation presentation | Form state, polling, inventory pagination, restore dialogs | Eligibility calculations and authoritative status |
| `internal/app` | Composition and lifecycle | Dependency wiring, scheduler start/stop, shutdown cancellation | Storage or policy rules |

| Boundary | Hidden detail | Dependency direction |
|---|---|---|
| `internal/retention` ports | SQLite transactions and segment file names | `app` injects SQLite, archive, normalizer, and capacity adapters into retention use cases. |
| `internal/archivefs` | Tar entry layout, zstd options, temp names, fsync sequence | Retention depends on the `SegmentStore` interface, not the package. |
| `internal/storage/sqlite` | Table graph, authority, deletion cutover, and projection reconciliation | Retention depends on narrow planning/publication ports; SQLite may depend on retention value types and passive file handles, never on transport or Web. |
| Connect transport | Protobuf presence rules and status codes | Transport calls narrow retention reader/command interfaces. |

The scheduler, archive, restore, and expiry use cases are separate objects. They
share immutable domain values and repository ports but do not share one mutable
operation builder. This keeps policy scheduling independent from manual restore
and prevents the storage adapter from deciding lifecycle eligibility.

---

## 6. Interface Design

The following signatures are design contracts and do not yet exist in the
repository.

### 6-1. Application Use Cases

```go
// PolicyReader and PolicyUpdater separate read-only status consumers from mutation.
type PolicyReader interface {
	Policy(context.Context) (PolicyView, error)
}

type PolicyUpdater interface {
	// UpdatePolicy returns ErrInvalidPolicy without modifying the current revision.
	UpdatePolicy(context.Context, PolicyUpdate) (PolicyView, error)
}

type RetentionReader interface {
	Status(context.Context) (Status, error)
	ListSegments(context.Context, SegmentPageRequest) (SegmentPage, error)
	Capacity(context.Context) (CapacityReport, error)
}

type RestoreRequester interface {
	// RequestRestore durably claims the complete target set before returning.
	// It returns ErrRestoreConflict if any target already has restore authority.
	RequestRestore(context.Context, RestoreRequest) (Operation, error)
}
```

### 6-2. Lifecycle Orchestration Ports

```go
type LifecyclePlanner interface {
	// BeginCycle snapshots the fixed cohort and current policy revision.
	BeginCycle(context.Context, time.Time) (CyclePlan, error)
	FailOperation(context.Context, OperationID, OperationFailure) error
	RecoverableOperations(context.Context) ([]RecoveryRecord, error)
}

// LifecyclePublisher is implemented once by sqlite.Store. Every catalog,
// projection, aggregate, and change-feed mutation is one SQLite transaction.
type LifecyclePublisher interface {
	// ClaimRestore classifies matches and claims selected exports plus every member
	// whose segment membership can change. If a pre-cutover deletion is staged,
	// it restores and syncs that file before cancelling deletion and returning.
	ClaimRestore(context.Context, RestoreRequest, DeletionStaging) (RestorePlan, error)
	PublishArchive(context.Context, ArchivePublication) error
	PublishRestore(context.Context, RestorePublication) error
	// StageDeletion owns reversible staging: under writeGate it persists staging
	// intent and token, renames and syncs through the passive handle, then records
	// staged before releasing the gate.
	StageDeletion(context.Context, DeletionPlan, DeletionFile) error
	// CommitDeletion owns the serialized point of no return. It acquires the
	// same writeGate as restore claims and policy updates, revalidates authority,
	// records delete_committing, removes and syncs the staged file through the
	// passive handle, and publishes Deleted before releasing the gate.
	CommitDeletion(context.Context, DeletionPlan, DeletionFile) error
}
```

```go
type SegmentStore interface {
	// Build writes to a private candidate, fsyncs it, and verifies its segment and member digests.
	Build(context.Context, SegmentPlan, ExportStream) (CandidateSegment, error)
	Install(context.Context, CandidateSegment) (InstalledSegment, error)
	OpenVerified(context.Context, InstalledSegment) (VerifiedExportStream, error)
	// DeletionFile resolves paths and a durable token but does not rename or remove.
	DeletionFile(context.Context, InstalledSegment) (DeletionFile, error)
	RemoveUnreferenced(context.Context, SegmentID) error
}

// DeletionFile is a passive durable-file adapter. It never decides policy or
// transition priority; LifecyclePublisher invokes it while owning writeGate.
type DeletionFile interface {
	StageAndSync(context.Context) error
	RemoveAndSync(context.Context) error
	RestoreAndSync(context.Context) error
	Inspect(context.Context) (DeletionFilePresence, error)
}

type DeletionStaging interface {
	// Resolve returns the passive handle named by a durable staging token.
	Resolve(context.Context, StagingToken) (DeletionFile, error)
}
```

```go
type ActiveExportReader interface {
	// ActiveExports streams raw data without exposing archived rows.
	ActiveExports(context.Context, []ExportID) (ActiveExportStream, error)
}

type ExportReplayer interface {
	// PrepareRestore uses the current normalizer and returns private, immutable
	// publication input; it performs no SQLite or lifecycle-state mutation.
	PrepareRestore(context.Context, VerifiedExportStream) (PreparedRestore, error)
}
```

### 6-3. Capacity Port

```go
type CapacityProbe interface {
	// Measure acquires one storage observation boundary and returns explicit unavailable reasons.
	Measure(context.Context) (CapacityReport, error)
	EstimateArchive(context.Context, CyclePlan) CapacityEstimate
	EstimateRestore(context.Context, RestorePlan) CapacityEstimate
}
```

### 6-4. Public Protobuf Contract

The implementation adds this design fragment to the existing protobuf SSOT and
generates Go and TypeScript clients. Field numbers are assigned during
implementation and verified by Buf; this fragment intentionally omits them.

```proto
service AgentmetryRetentionService {
  rpc GetRetentionStatus(GetRetentionStatusRequest) returns (GetRetentionStatusResponse);
  rpc UpdateRetentionPolicy(UpdateRetentionPolicyRequest) returns (UpdateRetentionPolicyResponse);
  rpc ListArchiveSegments(ListArchiveSegmentsRequest) returns (ListArchiveSegmentsResponse);
  rpc RestoreArchive(RestoreArchiveRequest) returns (RestoreArchiveResponse);
}

message RestoreArchiveRequest {
  oneof scope {
    string segment_id = /* assigned in SSOT */;
    ReceiveTimeRange receive_time = /* assigned in SSOT */;
  }
  optional int32 hold_days = /* required only for current segment or period restore */;
}
```

`RestoreArchive` acknowledges a durable asynchronous operation. The Web client
polls `GetRetentionStatus`; it never treats the initial acknowledgement as
completed restoration. The service remains local under the existing loopback
listener and is not added to MCP.

---

## 7. Database and File Design

### 7-1. Lifecycle Catalog

| Relation | One row represents | Constraints and indexes |
|---|---|---|
| `retention_policy` | The current configured or disabled policy | Singleton key; monotonic revision; check cutoff range and ordering |
| `retained_exports` | Stable identity, lifecycle state, and terminal deletion authorization of one admitted export | Primary key is the logical export ID; state is Active, Archived, or Deleted; receive-time index; state-specific field checks; Deleted rows carry a self-contained policy revision, cutoff, evaluation, and completion summary |
| `archive_segments` | One current or terminal segment identity | Unique file identity for current segments; state and integrity checks; receive-time and deletion-schedule indexes |
| `archive_segment_members` | Historical immutable membership and ordinal of one export in one segment version | Primary key `(segment_id, export_id)`; unique member ordinal; never rewritten when current membership changes |
| `current_archive_memberships` | The one current segment containing an Archived export | Primary key `export_id`; unique `(segment_id, ordinal)`; only rows for current segments and Archived exports |
| `archive_segment_replacements` | One original-to-replacement supersession edge | Primary key `(original_segment_id, replacement_segment_id)`; replacement cannot equal original; repository validation rejects cycles and requires paths to terminate at current or terminal references |
| `retention_cycles` | One scheduled fixed-instant/fixed-cohort parent result | Unique cycle ID; status and evaluation-instant checks; start/completion indexes |
| `retention_operations` | One archive, restore, or delete work unit, optionally belonging to a cycle | Unique operation ID; nullable cycle ID; kind/status/phase checks; deletion staging token and phase; completion-order index |
| `retention_operation_exports` | Immutable affected membership and selected subset for one work unit | Primary key `(operation_id, export_id)`; selected-role check; remains historical after authority release |
| `retention_export_authorities` | The current exclusive transition owner for one export | Primary key `export_id`; unique operation/role relationship; inserted and removed transactionally with operation phase changes |
| `span_projection_candidates` | One Active export's complete candidate for a trace/span identity | Primary key `(export_id, trace_id, span_id)`; winner-order index; foreign key to Active retained export |

`retained_exports` is also Deletion Evidence after expiry. Deleted rows retain
only identity, receive time, prior segment or equivalent scope, deletion time,
and the copied authorization basis. An operation ID may be retained only as
denormalized diagnostic text and is not a foreign key, so pruning detailed
operation rows cannot invalidate evidence. Terminal segment rows retain lookup
state and replacement edges without archive file paths that can be opened.

For new admissions after migration, `retained_exports.id`, `otlp_exports.id`,
and `payload_occurrence` use the same globally increasing value. Restores insert
all three values explicitly. Legacy payload occurrences remain unchanged, and
the migrated SQLite sequences start above both the largest historical export ID
and the largest historical occurrence. Consequently deleting Active or Archived
content cannot make `(signal, payload_sha256, payload_occurrence)` reusable.

### 7-2. Projection Ownership

| Existing projection area | Change | Reconciliation rule |
|---|---|---|
| `observations` | Existing `export_id` remains authoritative | Delete with the Active journal row. |
| `spans` | Add `span_projection_candidates` and keep `spans` as the materialized winner | Removing an export deletes its candidates, then selects the newest remaining candidate for each affected identity or removes the winner. No incomplete observation row is replayed. |
| `logs` and `metrics` | Add owning `export_id` | Remove only rows owned by exports leaving Active. |
| Session links | Add append-only `session_link_evidence` with `export_id` | Materialize each link while at least one Active evidence row remains. |
| Model-call evidence | Add `export_id` to raw-derived evidence | Rebuild affected source/session call identities and attribution after base-row mutation. |
| Session and trace aggregates | Reuse affected-scope rebuild functions | Determine affected scopes before mutation, then rebuild from the remaining Active base rows in the same transaction. |
| Projection change feed | Append removal/upsert targets in the publication transaction | Existing Web synchronization observes one committed visibility change. |

The storage-generation replay populates every new owner and every complete span
candidate from the `AcceptedExport` being committed. Candidates are retained
only while their source export is Active, so retry fallback is correct without
keeping Archived projection data. No heuristic backfill from timestamps or
projection sequence is permitted.

### 7-3. Archive Representation

Each installed file is `<database>.archive/segments/<segment-id>.tar.zst` with
directory mode `0700` and file mode `0600`.

| Layer | Content | Integrity evidence |
|---|---|---|
| SQLite lifecycle metadata | Segment identity, immutable member IDs/receive times/ordinals, representation version, byte counts, file digest, membership digest, and integrity states | SQLite integrity and constraints plus recomputed canonical membership SHA-256 |
| Tar manifest entry | Format version and the complete ordered replay metadata for every member | Deterministic manifest bytes and membership SHA-256 |
| Tar payload entries | Exact pre-normalization protobuf, one entry per export ID | Existing original size and SHA-256 for every export |
| Zstd stream | One stream over the complete tar archive | Stored-file size and SHA-256 in lifecycle metadata |

The writer uses `zstd.SpeedBestCompression`, one encoder worker, and a 32 MiB
window. Compression spans export boundaries. The reader streams tar entries and
enforces the existing 32 MiB per-export limit before allocation. A format
registry dispatches by explicit version; v1 never guesses a format from file
contents.

### 7-4. Durable Publication Boundaries

| Operation | Durable authority before publication | Publication transaction | Post-publication cleanup |
|---|---|---|---|
| Archive | Active SQLite raw and derived state | Revalidate policy; insert installed segment metadata; reconcile projections; remove Active payload rows; mark exports Archived | Remove candidates and mark operation completed |
| Restore | Original installed segment files and catalog membership | Revalidate restore ownership and hold; insert original IDs into Active journal; publish current outcomes; install replacement metadata; mark selected exports Active and original segments restored/superseded | Remove retired original files and candidates |
| Delete | Under one `writeGate`, persist `staging` intent/token, rename and directory-sync the file through a passive handle, then persist `staged`; immutable membership and a durable child operation identify the proposed work | Under a later `writeGate`, revalidate current policy and restore authority, persist `delete_committing`, remove and directory-sync the staged file, then publish self-contained Deleted evidence and terminal segment state | Release authority and prune detailed terminal operation rows beyond the newest 100 |

Archive and restore install new files by candidate write, file sync, atomic rename,
and directory sync before the SQLite publication transaction. An interruption
before SQLite commit leaves the prior stable state and an unreferenced candidate
or installed file. Recovery removes it or reuses it only after verification.

Deletion staging is reversible preparation and is not the point of no return.
`sqlite.Store.StageDeletion` owns it under the same `writeGate`: it persists a
`staging` intent and durable token before invoking passive atomic rename plus
directory sync, then persists `staged`. A restore that preempts `staging` or
`staged` work holds the gate until the installed path is restored and synced and
deletion is durably cancelled; only then can archive reading and restore
authority proceed. `sqlite.Store.CommitDeletion` is the single irreversible
cutover owner: it reacquires the gate, revalidates current policy and restore
authority, commits `delete_committing`, calls
`DeletionFile.RemoveAndSync`, and commits `content_removed` plus terminal Deleted
publication before releasing the gate. Crossing into `delete_committing` is the
serialized point of no return; later restore claims observe deletion as winning.

Startup recovery runs before public services. For `staging`, installed-only
presence is restaged or cancelled after revalidation; staged-only presence is
verified and recorded `staged`; both paths preserve the installed copy and
cancel after removing only a verified duplicate; neither path fails without
claiming recoverable payload. A `staged` operation reacquires `writeGate`,
revalidates current policy and restore authority, and either restores/cancels or
resumes cutover. Once the durable phase is `delete_committing`, recovery does
not reopen arbitration: it resumes removal when the staged file exists and
completes self-contained Deleted publication whether removal happened before or
during recovery. A `content_removed` operation completes that same publication,
and a terminal operation only removes verified leftover staging. Every phase is
idempotent and all path presence is interpreted against the durable token and
phase rather than guessed from a filename.

---

## 8. Decisions

### Decision: Use whole-stream tar plus zstd for archive segments

- **Choice**: Reconstruct original protobufs, place them and one deterministic manifest in tar, and compress the complete segment as one zstd stream.
- **Rationale**: Repeated OTLP schemas and attribute names can match across exports. Tar provides streaming framing and keeps v1 inspectable without inventing a complex seekable container.
- **Alternatives**: Per-export zstd preserves random access but cannot exploit cross-export redundancy. A custom indexed binary container reduces framing overhead but adds format and recovery risk without a current random-access requirement.
- **Consequences**: A partial restore reads the complete bounded segment. Segment size therefore stays an internal 256 MiB original-data bound.

### Decision: Keep lifecycle metadata in SQLite and payload bytes in sibling files

- **Choice**: SQLite owns identities, selection, authority, and history; the archive directory owns only versioned raw segment files and transient candidates.
- **Rationale**: Period selection and corrupt-content expiry remain possible without decoding payload files. Raw bytes leave the active database and can be physically removed by deleting one segment file.
- **Alternatives**: Keeping archives as SQLite blobs retains large database allocation and weakens physical reclamation. File-only manifests make exact concurrent selection and terminal lookup harder to transact with query visibility.
- **Consequences**: Cross-resource publication requires explicit operation records, fsync, and startup recovery.

### Decision: Add export ownership and targeted projection reconciliation

- **Choice**: Attribute every base projection and evidence row to its source export, retain complete span candidates only for Active exports, and rebuild only affected shared identities and aggregates.
- **Rationale**: Selective archive cannot safely delete canonical spans that were overwritten by retries or shared session/cost projections without ownership. Targeted reconciliation avoids rebuilding the entire multi-gigabyte database on every segment transition.
- **Alternatives**: Whole-database replacement reuses compaction but requires large peak capacity and blocks or reroutes the live backend daily. Reconstructing a full span from observations is impossible because observations are intentionally partial. Retaining candidates after archive would violate raw-only archive semantics.
- **Consequences**: The first storage-generation migration must replay the complete active journal. Active retry candidates consume additional projection space but disappear with their exports.

### Decision: Publish visibility changes in one SQLite transaction

- **Choice**: Prepare compression or normalization outside SQLite, then use `writeGate` and one transaction for all catalog, raw, projection, aggregate, and feed changes in an operation.
- **Rationale**: WAL readers observe a coherent pre- or post-transition snapshot, and ingestion remains serialized with lifecycle changes.
- **Alternatives**: Chunked commits reduce WAL growth but expose partial sessions, costs, and restore sets. Temporarily rejecting all queries broadens observable behavior without necessity.
- **Consequences**: Segment bounds limit transaction size. Capacity preflight includes WAL and staging estimates.

### Decision: Make SQLite the deletion-cutover and publication owner

- **Choice**: One SQLite adapter owns `writeGate` from the final restore/policy arbitration through `delete_committing`, durable file removal, directory sync, and terminal Deleted publication. `archivefs` supplies only a passive staged-file handle.
- **Rationale**: Separate prepare/remove/complete calls allow a restore or policy change to interleave after the final check and make the deletion result contradict the declared priority. The shared gate creates one observable winner and a recoverable durable phase boundary.
- **Alternatives**: Letting the retention service coordinate independent repository and file calls leaves a crash window with no single state-machine owner. Treating rename-to-staging as irreversible makes cancellation semantics depend on an internal filename rather than actual destruction.
- **Consequences**: The gate can be held across one filesystem removal and directory sync. Segment size is bounded, removal is metadata-only on supported filesystems, and fault-injection tests cover every durable phase.

### Decision: Separate cycle history, immutable targets, and current authority

- **Choice**: A Maintenance Cycle is a parent aggregate; archive/delete work units and manual restores are Lifecycle Operations. Immutable target rows record history, while a dedicated one-row-per-export authority table records only current exclusion.
- **Rationale**: A SQL partial index cannot condition target uniqueness on status stored in another table. Separate concepts also make cycle failure propagation and operation-history bounds unambiguous.
- **Alternatives**: One polymorphic operation table makes the cycle both a container and a transition owner. Deleting target rows on completion loses the fixed-set recovery and audit evidence.
- **Consequences**: Setup or cohort-evaluation failure fails the cycle. After successful evaluation, zero child work completes it; otherwise it stays running until all children are terminal, then fails if any child failed, is cancelled only if every child cancelled, and otherwise completes. Multiple scheduled cycles may be running, while per-export authority prevents overlapping child ownership. Child failures remain independently diagnosable.

### Decision: Separate scheduled orchestration from manual restore

- **Choice**: A scheduler invokes cycles only for enabled policies; the restore use case reads inventory and claims targets independently of policy enablement.
- **Rationale**: This enforces the specified disabled-policy boundary and keeps restore priority explicit.
- **Alternatives**: One generic maintenance command obscures policy revalidation and permits accidental restore rejection while disabled.
- **Consequences**: Both use cases share transition-authority persistence but have separate validation paths.

### Decision: Expose retention through a dedicated local Connect service

- **Choice**: Add four retention RPCs and one Web settings/inventory surface; keep MCP unchanged.
- **Rationale**: The existing protobuf/Connect path is the authoritative local Web contract. Restore is mutating and does not fit the read-only MCP boundary.
- **Alternatives**: Ad-hoc HTTP routes duplicate contract tooling. MCP mutation expands safety and exposure policy beyond this scope.
- **Consequences**: Buf lint, generation, breaking checks, Connect adapters, TypeScript clients, localization, and component tests change together.

---

## 9. Test Specification

| Test group | Precondition / action | Required evidence | Requirements |
|---|---|---|---|
| Policy partitions | Construct every boundary and invalid combination; disable with and without cutoffs; change revision during a claimed operation | Accepted values are exact; rejected updates do not mutate policy; stale automatic publication stops | FR-1 |
| Cycle eligibility | Evaluate Active/Archived/Restoring/Deleted, exact cutoff, hold equality, clock movement, and concurrent admission | Fixed instant/cohort and one stable transition per export | FR-1, FR-2 |
| Archive format golden | Encode multiple repeated OTLP exports, stream-decode, and compare bytes/metadata | Every member matches original size/SHA; aggregate stream is used; v1 golden remains readable | FR-3, FR-7, FR-8 |
| Archive publication | Inject failure before candidate sync, after install, before/after SQLite commit, and during projection reconciliation | Exactly one verified authority survives; queries and feed show complete before/after state | FR-3, FR-7 |
| Projection ownership | Archive the latest and non-latest copies of duplicate spans, logs, metrics, links, Claude/Codex cost evidence, sessions, and traces | Complete Active span candidates select the correct winner; removal and recalculated rollups/costs match replay of remaining Active exports | FR-3 |
| Restore selection | Select full segment, half-open boundaries cutting segments, mixed states, empty set, terminal IDs, and overlap through an unselected member of a replaced segment | Exact selected/affected classification, conflict atomicity, acyclic exact replacement partition, terminal references, and unchanged holds | FR-5 |
| Restore replay | Restore successful, current-normalization-failed, corrupt, unsupported-format, and insufficient-capacity members | All-or-none publication; failed normalization is Active raw-only; other failures preserve Archived state | FR-5, FR-7, FR-8 |
| Expiry | Inject a restore claim, policy change, and crash at staged, `delete_committing`, file-removed, and terminal boundaries; also evaluate corrupt and unverifiable metadata | One serialized winner, only the complete authorized segment removed, self-contained terminal evidence, and idempotent recovery | FR-6, FR-7 |
| Identity non-reuse | Admit duplicate payloads before and after archive, restore, deletion, migration, and process restart | Stable IDs and payload occurrences remain unique; restored rows preserve exact historic values | FR-7, FR-8 |
| Cycle aggregation | Run setup-failed, zero-work, overlapping, and multiple-child cycles ending in mixed terminal states | Every running cycle is visible; terminal propagation and bounded cycle/operation histories follow their independent rules | FR-2 |
| Capacity accounting | Seed known SQLite page classes, freelist pages, archive/staging files, and an unavailable estimate | Disjoint component values, total allocation semantics, warning, unavailable reason, and no eligibility change | FR-4 |
| Migration equivalence | Rebuild a legacy fixture containing retries, failed normalization, all signals, links, costs, and rollups | Active query results and journal digests match; every new projection owner and retention row is populated | FR-3, FR-8 |
| Connect and Web | Exercise protobuf presence, invalid inputs, paging, asynchronous polling, localization, disabled restore, and no manual-delete control | Generated-contract results match use cases and Archived content never appears | FR-1, FR-2, FR-4, FR-5 |

Construction follows Red-Green-Refactor by vertical behavior slice: value
objects, format, catalog constraints, archive publication, restore publication,
expiry/recovery, API, then Web. Fault-injection tests are written before each
multi-resource publication path.

---

## 10. Risks / Trade-offs

| Risk | Mitigation |
|---|---|
| Projection ownership is incomplete for a shared derived table | Migration validation compares all active query projections and requires an owner for every export-derived base/evidence row. |
| Long publication transactions grow WAL and delay ingestion | Bound original segment input to 256 MiB, prepare outside the write gate, estimate peak allocation, and rebuild only affected scopes. |
| Crash between filesystem and SQLite commits creates an orphan or missing current path | Persist explicit staged/delete-committing/content-removed phases, retain one cutover owner, fsync every rename/removal boundary, and recover before services become ready. |
| Best-compression zstd consumes CPU | Run only in scheduled background work with one encoder worker; keep ingestion normalization outside the compression worker. |
| Latent segment corruption removes restore capability | Verify on inventory refresh and every restore; keep payload and metadata integrity separate; never claim recovery without another verified copy. |
| Terminal export and segment metadata grows indefinitely | Keep only non-content identity/time/authorization fields; operation history is bounded to the newest 100 terminal operations and is not referenced by deletion evidence. Deleted/superseded evidence remains because lookup semantics require it. |
| Archive files are not encrypted | Restrict directory and files to the owning user. Encryption remains explicitly outside this change. |

---

## 11. Migration / Rollback

| Phase | Design |
|---|---|
| Storage generation | Increment the generation because projection ownership and full span candidates cannot be inferred safely by additive DDL. Recovery first converges every existing lifecycle operation. Candidate construction then copies policy and retained identities/states, replays Active rows with explicit IDs and legacy occurrences, and copies current/terminal segment metadata, immutable and current memberships, replacement edges, and deletion evidence. |
| Candidate validation | Verify every current archive file in place; compare journal digests and Active query results; validate projection ownership and span candidates, current membership uniqueness, acyclic replacement paths, self-contained deletion evidence, operation phases, foreign keys, and one-authority-per-export. Set SQLite sequences above every retained ID and payload occurrence before install. |
| Install | Use the existing candidate/backup/manifest replacement protocol before serving. The archive directory is created only after the migrated database opens successfully. |
| Default behavior | A legacy database with no lifecycle catalog receives an unconfigured policy and every migrated export is Active. A lifecycle-aware database preserves its accepted policy and all stable states. No new automatic work starts until installation and recovery finish. |
| Future data migration | Use the lifecycle-aware candidate procedure above for every later generation. Archive files remain at their existing sibling paths and are never copied or discarded merely because SQLite is replaced. |
| Application rollback | A binary that predates the new storage generation cannot open the migrated database. Rollback requires restoring a pre-upgrade database backup. After successful retention deletion, no rollback can reconstruct deleted raw content. |
| Operation rollback | Before terminal deletion, disable retention to stop new automatic work and allow recovery to converge. Completed archive is reversible through restore; completed deletion is intentionally irreversible. |

---

## 12. Open Questions

None.
