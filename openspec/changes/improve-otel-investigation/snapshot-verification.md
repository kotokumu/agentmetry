# Comparison snapshot verification

## Verified behavior

`TestCompareReworkKeepsOneSnapshotAcrossConcurrentCommit` in `internal/storage/sqlite/rework_comparison_snapshot_test.go` verifies R7, scenario S9, and test-design case B11 through the public `Store.CompareRework` method and a real temporary SQLite database.

| Stage | Stored facts for each conversation | Expected public result |
| --- | --- | --- |
| Initial committed data | Failed validation, file edit, successful retry; token total 60; three activities; final timestamp at +5 seconds | Both subjects use the initial time, token denominator, and analyzed coverage |
| Comparison in progress | The first subject is read. Before reading the other subject, `Store.CommitBatch` adds a later validation to both conversations on the writer connection | The in-flight comparison equals the entire initial public result, including all metric rows, times, coverage, and harness view |
| New comparison after the commit | Token total 100; four activities; final timestamp at +8 seconds | Both subjects expose the updated facts; retained-projection coverage remains complete |

Conversation start timestamps follow the existing summary contract: the first retained span's observed timestamp, +1 second in this fixture. The two subjects are one hour apart and remain eligible. Output-token presence explicitly records zero so the fixture has a reported total, rather than treating missing output usage as zero.

---

## Synchronization and ownership

- The test uses `Open`, `CommitBatch`, and `CompareRework` on the real Store. It does not replace query results or construct a fake comparison response.
- A test-only connector delegates to the Store's actual SQLite driver. It wraps only the read pool inside the test and forwards real transaction and query operations.
- Bound fixture conversation identities identify the transition from one subject to the other. The connector commits the writer update at that transition. It does not match SQL text, count queries, require a fixed helper-call order, or use sleeps.
- The reader transaction and writer commit overlap deterministically. Completion of the writer commit precedes the second subject's database query.
- The whole initial public result is the temporal oracle. Independent fixed assertions on before/after timestamps, analyzed counts, projection completeness, and token denominators prove that a real, observable update occurred. Arithmetic semantics remain the responsibility of the separate comparison tests.
- No production hook, interface, driver registration, Store copy, or database schema change is introduced. Driver-wrapper methods exist only in the new test file.

---

## Red and green evidence

The implementation exists when this bounded test task starts. The negative control is a regression-detection check, not a claim that this test preceded the implementation.

| Check | Command / Change | Result |
| --- | --- | --- |
| Green: real implementation | `GOCACHE=/tmp/agentmetry-ui-analysis-gocache go test ./internal/storage/sqlite -run '^TestCompareReworkKeepsOneSnapshotAcrossConcurrentCommit$' -count=1 -v` | PASS; initial and updated fixed-state subtests also pass |
| Red: remove shared-read guarantee temporarily | A Go overlay changes only the two `loadReworkDiagnosticSnapshot` reader arguments from `transaction` to `store.readDB`. Run the same target with `-overlay=/tmp/agentmetry-snapshot-negative-control/overlay.json` | Expected FAIL at `comparison mixed committed snapshots`: current end changes from +5 to +8 seconds, coverage count from 3 to 4, token denominator from 60 to 100, while baseline stays unchanged. Derived comparison deltas also differ |
| Green: race detector | `GOCACHE=/tmp/agentmetry-ui-analysis-gocache go test -race ./internal/storage/sqlite -run '^TestCompareReworkKeepsOneSnapshotAcrossConcurrentCommit$' -count=1` | PASS |

The overlay and replacement file are temporary files under `/tmp/agentmetry-snapshot-negative-control`; production source in the worktree is unchanged. The initial test run exposed two fixture errors—summary start-time expectation and missing explicit output-token presence—which were corrected before the negative-control verification.

`gotests -only '^CompareRework$' -use_go_cmp internal/storage/sqlite/rework_comparison.go` generates a Store scaffold that copies `sync.Mutex` fields. The task's explicit exception and the existing `implementation-log.md` scaffold decision apply: the test retains the Store pointer and calls the public method directly. Assertions compare concrete expected values using `go-cmp`; no production test abstraction is added to accommodate the generated scaffold.

This verification is limited to the named snapshot test. The repository-wide validation remains owned by the integration task.
