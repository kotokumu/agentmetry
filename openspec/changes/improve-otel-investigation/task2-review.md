# Task 2 implementation review

## Verdict

**PASS — task 2 review.** Review covers exact trace evidence, episode access, navigation restoration, and their existing paging/live boundaries. The three identified regressions are corrected in the implementation and covered by focused regression tests. No unresolved blocker remains in the reviewed scope. Later purpose views, structured filters, and overview/range features are outside this review.

---

## Resolved findings

| Module / Boundary | Problem | Dependency or Responsibility Issue | Suggested Direction | Severity |
|---|---|---|---|---|
| ReworkSummary / return focus | Span-only lookup can choose an episode from another trace and focus can override reading position. | Return focus must preserve the complete evidence identity and restored viewport. | Implemented: `focusEvidence(traceId, spanId)` matches both fields in the episode and link; workspace forwards both; focus uses `preventScroll: true`. The cross-trace same-span test verifies the fifth episode is selected. | Medium — resolved |
| TraceController / live request lifecycle | Cancelled live loading can keep paging disabled on the next target. | Request invalidation must clear its loading state while rejecting stale completion. | Implemented: open and disconnect clear `liveLoading` and advance request generation. The deferred live refresh → target switch → load-more test verifies new-target paging and rejection of the old result. | Medium — resolved |
| ActivityTable / trace link contract | Native log correlation can turn a valid trace link into a failed native-span lookup or mix unrelated IDs. | Trace membership and exact-span selection must remain distinct; href and click must agree. | Implemented: shared `traceAnchorSpan` keeps native logs unanchored, uses native span IDs for span rows, and uses related span IDs only with the related trace path. The native-log test includes a different related pair and verifies both href and event. | Medium — resolved |

---

## Confirmed boundaries

| Area | Review result |
|---|---|
| SQLite exact target | `traceSpanOffset` ranks only metadata in the same read transaction as the trace summary/page. It uses the existing global order and permits only `signal=trace` to satisfy the target. |
| Paging | Anchor offset uses `Page.OffsetAround`; existing page-size validation remains in all three adapters. Anchor takes precedence over valid page/tail positions without expanding the page limit. |
| Missing target | Query has a distinct target-not-found error. Connect/HTTP preserve NotFound with the target error text; MCP preserves the query error. Web checks that an anchored response actually includes the native target and retains the return action on failure. |
| Stale different target | Initial reads are keyed by trace/span; opening another target clears override and loading state and invalidates page/live requests. |
| Episode access | Incremental display exposes every returned episode and retains the existing analysis coverage. |
| Disclosure | MCP anchored reads preserve body opt-in. No collector, raw read, or provider mapping is added. |

Review evidence is the corrected implementation and added SQLite, adapter, controller, component, and app assertions. The implementation owner reports red-to-green verification of the three fixes, 122 passing tests across four Web files, and a successful build. The reviewer inspects the corrected paths and regression assertions without rerunning optional tests; actual commands/results remain in the construction record.

---

## Source locations

- [Rework summary](https://github.com/kotokumu/agentmetry/blob/main/web/src/components/rework-summary.ts), [conversation workspace](https://github.com/kotokumu/agentmetry/blob/main/web/src/components/conversation-workspace.ts), and [navigation](https://github.com/kotokumu/agentmetry/blob/main/web/src/app/navigation.ts).
- [Trace controller](https://github.com/kotokumu/agentmetry/blob/main/web/src/controllers/trace-controller.ts), [activity table](https://github.com/kotokumu/agentmetry/blob/main/web/src/components/activity-table.ts), and [trace waterfall](https://github.com/kotokumu/agentmetry/blob/main/web/src/components/trace-waterfall.ts).
- [SQLite trace read](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/trace.go), [query contract](https://github.com/kotokumu/agentmetry/blob/main/internal/query/trace.go), and existing Connect/HTTP/MCP adapters.
