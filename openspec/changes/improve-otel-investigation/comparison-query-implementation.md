# Shared comparison query implementation

## Scope and Contract

| Item | Implemented behavior |
|---|---|
| Task | 3.1, pure Go comparison and its read contract |
| Files | `internal/query/rework_comparison.go`, `internal/query/rework_comparison_test.go` |
| Requested pair | `ReworkComparisonPair` carries baseline/current `ConversationIdentity` values |
| Input facts | `ReworkDiagnosticSnapshot` contains canonical identity, start/end times, and existing `SessionRework` analysis. The storage reader supplies both snapshots from one transaction |
| Read interface | `ReworkComparisonReader.CompareRework(context.Context, ReworkComparisonPair) (ReworkComparison, error)` |
| Pure rule | `CompareReworkSnapshots(baseline, current ReworkDiagnosticSnapshot) ReworkComparison` performs no I/O or identity resolution |
| Result | Ready/invalid status, reason code/text, body-free baseline/current summaries, and five rows for an eligible pair. Summary retains source/conversation identity, times, full analysis coverage, complete/partial/unknown projection classification, and existing harness context |
| Identity guard | Each analysis must match its supplied canonical identity. Same canonical conversation, cross-source pair, invalid interval, and overlapping baseline are rejected. Boundary-touching and valid zero-duration intervals are eligible |
| Metric value | Availability/reason plus nullable numerator, denominator, and raw value. A comparable row has a raw nullable delta; unavailable rows have no delta. Query returns no display direction or rounded value |
| Token authority | The denominator comes from `Analysis.SessionTokens`, never an independently loaded summary. Existing canonical token presence distinguishes missing totals from explicitly reported zero |
| Duration operands | Existing duration measurements are expressed in milliseconds, matching the baseline Web input semantics |
| Disclosure | Results contain no analysis cycles, failure episodes, activities, or content. Harness uses the existing internal type and `json:"-"`; transport adapters map it through the existing validated harness view |

The five metric identities, units, operands, ratio guards, and availability reasons follow the accepted Web comparison semantics. Raw delta precision and retained operands for unavailable token denominators implement R7/R8 explicitly: `5000/10000 → 5004/10000` yields approximately `0.04` percentage points, and an observed zero denominator remains visible as zero.

---

## Red-Green-Refactor Evidence

| Cycle | Red | Green | Refactor |
|---|---|---|---|
| Fixed five-metric oracle | `gotests -w -all -use_go_cmp internal/query/rework_comparison.go` creates the scaffold. The populated case fails against the empty implementation: missing ready status, summaries and all five expected rows | All five explicit numerator/denominator/value/unit/delta assertions and coverage/harness pass | Query-owned values and small private functions keep arithmetic independent of adapters and storage |
| Missingness and eligibility | Added cases fail because zero denominators produce NaN and invalid pairs remain ready | Nullable token operands, per-metric guards, and identity/time eligibility make every case pass | The result preserves available operands and independent coverage; fixed metric calls introduce no registry/strategy abstraction |
| Precision and body-free output | The numerical oracle includes raw `0.04` and repeating ratios; initial analysis includes evidence/episode sentinels excluded from the expected result | Full result comparison passes with a small floating-point tolerance; only body-free summaries and rows remain | Test data and expected outputs stay independent per generated table case; no helper computes expected diagnostics from production code |

The gotests `args` and `want ReworkComparison` scaffold is retained. Assertions use `cmp.Diff`, `cmp.AllowUnexported` for the existing private harness representation, and `cmpopts.EquateApprox(1e-12, 1e-12)` for numerical tolerance. No mocks are needed for the pure rule.

---

## Verification

| Command | Actual Result |
|---|---|
| `GOCACHE=/tmp/agentmetry-comparison-go-cache go test ./internal/query -run '^TestCompareReworkSnapshots$' -count=1 -v` | PASS, all 15 cases |
| `GOCACHE=/tmp/agentmetry-comparison-go-cache go test ./internal/query -count=1` | PASS, full query package |
| `git diff --check -- internal/query/rework_comparison.go internal/query/rework_comparison_test.go` | PASS |

The dedicated cache avoids the sandbox restriction on the user's default Go build cache. No repository dependency or Go configuration changes are needed.

Coverage includes the all-five numeric oracle, partial and unknown coverage, harness retention, raw sub-decimal and repeating ratios, missing/reported-zero token numerator, zero/missing denominator with retained operands, all five inconsistent-evidence reasons, touching zero-duration intervals, same canonical root, cross-source ID collision, overlap, reversed/missing/out-of-range time, stale analysis identity, and empty identity.

Storage transaction consistency, generated/wire mappings, public Web/MCP parity, aggregate compatibility, and presentation rounding remain with tasks 3.2–3.4. This implementation does not modify their files or mark task checkboxes.
