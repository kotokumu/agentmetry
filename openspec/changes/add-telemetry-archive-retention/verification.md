# Verification record

Verified on 2026-09-10 against the implementation in this change.

## Automated gates

| Command | Result |
|---|---|
| `go test ./internal/retention ./internal/archivefs ./internal/storage/sqlite ./internal/compaction ./internal/transport/connectapi` | Passed |
| `go test ./...` | Passed |
| `go test -tags=integration ./...` | Passed |
| `go vet ./...` | Passed |
| `npm --prefix web test -- --run` | Passed: 39 files, 420 tests |
| `npm --prefix web run build` | Passed |
| `buf lint` | Passed |
| `buf breaking --against '.git#branch=main'` | Passed |
| `openspec validate add-telemetry-archive-retention --strict` | Passed |
| `git diff --check` | Passed |

## Acceptance evidence

| Tasks | Executable evidence and observed result |
|---|---|
| 1.1 | `TestPolicyPartitions`, `TestEvaluateUsesFixedReceiveAgeAndHoldBoundary`, `TestRestoreScopeAndHoldBoundaries`, and `TestAggregateCycle` cover accepted/rejected policy, hold, scope, cutoff, and cycle partitions. |
| 1.2–1.4 | SQLite migration and replay tests pass, including `TestMigrateRebuildsTrueLegacySchemaAndPreservesJournalMetadata`, journal identity assertions, `TestArchiveFallsBackToRemainingSpanProjectionCandidate`, and `TestRestoreKeepsNewestStableExportAsProjectionWinner`. Foreign-key checks pass after archive, restore, replacement, and deletion. |
| 2.1–2.3 | `TestSegmentRoundTripPreservesRawExportsAndUsesWholeStreamCompression`, `TestSegmentFramingIsDeterministic`, corruption/format/metadata rejection tests, `TestPlanSegmentsGroupsByUTCDateAndOriginalByteBound`, `TestArchivePublicationRevalidatesCurrentPolicyAndHold`, and archive/restore SQLite integration tests pass. Publication rejects a disabled/revised policy or newly held export after claiming and preserves Active authority. The archive contains replay metadata and raw protobuf only. |
| 2.4 | `TestSchedulerStartsNextCycleFromTickerCadenceWhilePriorWorkIsBlocked`, `TestMaintenanceRecordsSetupFailureAfterDurableCycleStart`, and `TestSchedulerRunsNextCadenceAfterPriorCycleFails` directly cover initial scheduling, durable setup failure, and successful terminal completion of the next cadence on the same scheduler. Fixed-cohort and cycle aggregation/history tests also pass. Planned child counts make interrupted planning distinguishable from zero-work completion. |
| 3.1 | `TestArchiveRestoreRoundTripRemovesAndRebuildsActiveData` and `TestRetentionCapacitySeparatesReusablePagesFromAllocatedFile` directly compare reported database and current-archive allocation to independent OS allocation results, assert a nonzero freelist without allocation shrink, and cover mutually exclusive raw/observation/projection page classes plus exact peak estimates. `TestRetentionCapacityMapsEveryWireField` asserts all 16 response fields, and the two-page Connect inventory test asserts every inventory field on both pages. |
| 3.2–3.4 | `TestArchiveRestoreRoundTripRemovesAndRebuildsActiveData`, `TestPeriodRestorePublishesReplacementForUnselectedMembers`, `TestRestoreHoldStartsAtPublisherCommitBoundaryAfterGateWait`, `TestRestoreScopeClassifiesMixedStatesAndRestoringConflict`, `TestSegmentReferenceLookupResolvesTransitiveReplacementsAndRestoredTerminal`, `TestRestorePublishesCurrentNormalizationFailureWithoutDerivedData`, `TestRestoreConflictAndCorruptAtomicSetLeaveEveryExportArchived`, `TestRestoreAndDeletionArbitrationAtSerializedCutover`, `TestRestoreRejectsInsufficientCapacityBeforeReadingOrPublishingArchive`, and `TestRestoreVerificationFailuresRemainAtomicAndTerminal` pass. The period fixture cuts two current segments, asserts publisher-owned exact holds and two replacements, then repeats with a different hold and observes no change. Mixed states, Restoring conflict, multi-edge replacement resolution, restored lookup, and corrupt/unsupported/storage/capacity atomic failure are direct assertions. |
| 4.1–4.2 | `TestExpiredArchiveBecomesTerminalDeletionEvidence`, `TestDeletionRequiresWholeSegmentAgeAndVerifiableMetadataButNotIntactPayload`, `TestDeletionRevalidatesCurrentPolicyBeforePointOfNoReturn`, `TestRestoreAndDeletionArbitrationAtSerializedCutover`, `TestRestoreArchiveReclassifiesCommittedDeletionAsDeleted`, `TestRestoreArchiveUsesClaimClassificationForMixedPostPONRPeriod`, `TestPostPONRRestoreDoesNotAggregateLiveMaintenanceParent`, `TestDeletionPublishesTruthWhenDirectorySyncFailsAfterUnlink`, and `TestDeletionErrorConvergesInProcessAtEitherSideOfCutover` pass. They directly assert current-policy revalidation, mixed-age retention, both integrity axes, policy evidence, both serialized cutover outcomes at the API boundary, and deterministic parent failure ownership. |
| 4.3–4.4 | `TestStartupRecoversInterruptedArchiveClaimAndInstalledFile` covers `planned`/`file_ready`; `TestStartupRecoversRestoreInterruptedBeforePublication` and `TestStartupRecoversRestoreFileReadyAndContentRemovedPhases` cover `planned`/`file_ready`/`content_removed`; `TestStartupRecoversSerializedDeletionFromFilePresence` covers `staging`/`staged`/`delete_committing`/`content_removed` with installed/staged/missing file authority. Every case asserts terminal operation, released authority, file presence, lifecycle/inventory state, and raw/derived visibility. Later-cycle deletion resume, compaction recovery, and terminal pruning also pass. |
| 5.1–5.2 | Generated Go/TypeScript clients compile; Buf lint and breaking checks pass. Connect tests cover policy presence, invalid updates/holds, capacity, two-page inventory wire fields, terminal lookup, accepted segment and period restores, durable operation acknowledgement/status, and integrity/format/capacity/conflict error-code mapping. Status queries return every live record plus the newest 100 terminal cycles/operations. |
| 5.3 | Six Web component tests cover policy persistence, raw-only inventory/capacity, complete paging, segment/period restore actions with finite holds, durable result counts/IDs, integrity/cycle errors, capacity warning and validation alert roles, absence of manual deletion, and polling while either an operation or a cycle is running. Japanese localization generation and the production build pass. |
| 6.1–6.5 | Full normal/integration suites, migration/compaction recovery, generated-code checks, documentation review, strict OpenSpec validation, and independent requirements/model/architecture P0/P1 reviews pass. |

## Scenario coverage summary

- Policy and raw-retention scenarios are covered by the retention model tests, journal/migration suites, and the archive/restore round trip.
- Scheduling, fixed-cohort, failure-continuation, archive, and no-same-cycle-expiry scenarios are covered by scheduler, cycle snapshot, archive recovery, and terminal deletion evidence tests.
- Inventory/capacity scenarios are covered at SQLite, Connect, and Web boundaries, including integrity and unavailable-reason presentation.
- Restoration scenarios are covered by full- and partial-segment restore, stable projection precedence, mixed state/authority classification, transitive reference resolution, publisher-boundary hold calculation, corrupt/unsupported archive rejection, durable claims, and cleanup retry tests.
- Expiry/recovery scenarios are covered by policy revalidation, pre-/post-cutover fault injection, every durable archive/restore/delete phase, same-process later-cycle recovery, deletion evidence, compaction recovery, and archive format/integrity tests.

## Scenario traceability

| Scenario | Given fixture and principal observable assertion |
|---|---|
| Enable retention for old active exports | `TestArchiveRestoreRoundTripRemovesAndRebuildsActiveData`: an aged Active export becomes Archived only after the valid policy is enabled. |
| Reject equal cutoffs | `TestPolicyPartitions`: equal archive/delete cutoffs return `ErrInvalidPolicy`. |
| Evaluate the exact archive boundary | `TestEvaluateUsesFixedReceiveAgeAndHoldBoundary`: equality with the fixed cutoff is eligible. |
| Disable retention without disabling restore | `TestArchiveRestoreRoundTripRemovesAndRebuildsActiveData`: policy is disabled after archive; segment restore still republishes Active raw and derived data. |
| Run maintenance after startup | `TestSchedulerStartsNextCycleFromTickerCadenceWhilePriorWorkIsBlocked`: scheduler launch durably starts the initial cycle. |
| Exclude concurrent ingestion from a fixed cohort | `TestRetentionCycleHighWaterExcludesExportsCommittedAfterStart` and `TestDeletionCohortExcludesSegmentsPublishedAfterCycleStart`: post-start exports/segments are excluded. |
| Continue after a failed cycle | `TestSchedulerRunsNextCadenceAfterPriorCycleFails`: one scheduler records the first cycle's injected evaluation failure and completes its next cadence successfully under a distinct cycle ID. |
| Archive an eligible export | `TestArchiveRestoreRoundTripRemovesAndRebuildsActiveData`: archive retains exact raw bytes and removes observations/projections. |
| Do not archive and delete in one cycle | `TestExpiredArchiveBecomesTerminalDeletionEvidence`: the first cycle leaves the export Archived and a later cycle deletes it. |
| Cancel after writing an archive candidate | `TestStartupRecoversInterruptedArchiveClaimAndInstalledFile`: candidate/current orphan is removed, authority released, Active preserved, operation failed. |
| Inspect archive inventory | `TestRetentionConnectPolicyValidationAndCapacity` and `retention-settings` component tests: paging, period/count/size/integrity/schedule fields cross both boundaries without payload content. |
| Distinguish reusable and allocated bytes | `TestRetentionCapacitySeparatesReusablePagesFromAllocatedFile`: independently measured OS allocation exactly equals reported database/current-archive allocation while reusable freelist bytes remain separately reported. |
| Warn without early expiry | `retention-settings` warning test and `TestDeletionRequiresWholeSegmentAgeAndVerifiableMetadataButNotIntactPayload`: warning is surfaced while eligibility remains receive-time/metadata based. |
| Restore one intact segment | `TestArchiveRestoreRoundTripRemovesAndRebuildsActiveData`, `TestRestoreHoldStartsAtPublisherCommitBoundaryAfterGateWait`, and Connect async restore assertions: exact raw bytes, derived rows, publisher-owned hold instant, durable operation ID, terminal status, and later `restored` lookup are observed. |
| Restore a period cutting segments | `TestPeriodRestorePublishesReplacementForUnselectedMembers`: one half-open period cuts two current segments, restores one member from each with an exact five-day publication-time hold, and publishes one-member replacements for both unselected sides. |
| Preserve a current normalization failure | `TestRestorePublishesCurrentNormalizationFailureWithoutDerivedData`: Active raw is published with the current failed status/error and zero observations. |
| Reject a corrupt atomic set | `TestRestoreConflictAndCorruptAtomicSetLeaveEveryExportArchived`: corrupted two-export segment restores none, keeps both Archived, and releases authority. |
| Reject overlapping restoration | `TestRestoreConflictAndCorruptAtomicSetLeaveEveryExportArchived`: a second claim returns `ErrRestoreConflict` while the first owns the affected set. |
| Repeat after Active publication | `TestPeriodRestorePublishesReplacementForUnselectedMembers`: repeating the same period with a different hold leaves the already Active members and original hold timestamps unchanged. |
| Delete an eligible intact segment | `TestExpiredArchiveBecomesTerminalDeletionEvidence`: segment file/membership disappear and self-contained Deleted evidence remains. |
| Keep a mixed-age segment | `TestDeletionRequiresWholeSegmentAgeAndVerifiableMetadataButNotIntactPayload`: same-day members straddling the exact cutoff keep the whole segment Archived. |
| Let restoration win | `TestDeletionErrorConvergesInProcessAtEitherSideOfCutover`: a pre-cutover staged file is restored and the delete operation is cancelled before authority release. |
| Let committed deletion win | `TestRestoreAndDeletionArbitrationAtSerializedCutover` and `TestRestoreArchiveReclassifiesCommittedDeletionAsDeleted`: physical deletion converges to hold-free Deleted evidence and the restore API returns Deleted counts rather than a conflict. |
| Expire previously reported corrupt payload | `TestDeletionRequiresWholeSegmentAgeAndVerifiableMetadataButNotIntactPayload`: payload=`corrupt` with verifiable metadata remains eligible and is deleted. |
| Recover an interrupted archive publication | `TestStartupRecoversInterruptedArchiveClaimAndInstalledFile`: both absent and installed-file crash shapes recover to one Active authority. |
| Reject an unsupported archive version | `TestOpenVerifiedClassifiesUnsupportedFormatWithoutDamagingIntegrityAxes` and `TestRestoreVerificationFailuresRemainAtomicAndTerminal`: the reader returns `ErrArchiveFormat`; restore publishes nothing, terminates its operation, and does not invent payload/metadata corruption. |
| Report latent sole-copy corruption | `TestOpenVerifiedRejectsCorruption`, metadata-only mismatch test, and corrupt atomic restore test: independent integrity axes and error are recorded while Archived state remains. |
| Recover a restore interrupted before publication | `TestStartupRecoversRestoreInterruptedBeforePublication` and `TestStartupRecoversRestoreFileReadyAndContentRemovedPhases`: `planned`/`file_ready` retain the sole Archived copy and fail cleanly; already-published `content_removed` retains complete Active state and completes cleanup. |
| SC-RAW-01 Preserve pre-normalization data | Archive round trip compares restored protobuf byte-for-byte with the admitted raw request. |
| SC-RAW-02 Replay a retained export | Archive round trip asserts current observations/log projections are rebuilt from the retained export. |
| SC-RAW-03 Preserve raw identity | Archive round trip and journal identity tests assert explicit historic export ID and occurrence survive restore/restart. |
| SC-RAW-04 Retain admitted raw data | Journal/migration suites assert every committed export has one lossless raw row until its authorized lifecycle transition. |

Recovery coverage additionally includes `TestDeletionPublishesTruthWhenDirectorySyncFailsAfterUnlink`, `TestDeletionResumeAggregatesEachParentBeforeALaterFailure`, `TestDeletionResumeRediscoversTerminalChildWithUnfinishedParent`, `TestCompactionRecoversStagedDeletionBeforeSnapshot`, `TestPublishedRestoreCleanupRetriesUntilOperationCanBecomeTerminal`, and `TestRestoreVerificationFailuresRemainAtomicAndTerminal`. Together with the phase-table tests, these assert directory-sync failure, partial batch recovery, parent-aggregate retry, pre-snapshot recovery, cleanup retry, unsupported/corrupt/storage-read failures, and terminal convergence at every durable phase.

## Migration and authority evidence

| Required evidence | Executable observation |
|---|---|
| Representative legacy database | `TestMigrateRebuildsTrueLegacySchemaAndPreservesJournalMetadata`, published release-cohort upgrades, and older-generation direct-upgrade tests pass into generation 7. |
| Lifecycle-aware database | `TestForcedCompactionPreservesArchivedLifecycleCatalog` copies policy, retained export identity, current/historical segment membership, operations, cycles, and replacement relations with foreign keys intact. |
| Journal digest and raw identity | Journal tests recompute payload SHA-256/size after decompression and assert explicit export IDs and payload occurrences; archive round trip compares the restored protobuf bytes exactly. |
| Active query equivalence | Compaction replay tests compare reconstructed current session/trace/log/metric/cost projections; archive fallback and stable-winner tests assert projection precedence after removing/restoring one owner. |
| Archive-file verification | Compaction validation and archive tests verify file SHA-256, membership digest, member ID/ordinal/receive time, original sizes, codec, and format version before accepting the candidate. |
| Pre-service recovery | Store open invokes lifecycle recovery before query services are returned; interrupted archive, restore, staged deletion, and compaction-before-snapshot tests assert truthful authority before reopening succeeds. |
