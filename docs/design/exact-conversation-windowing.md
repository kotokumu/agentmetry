# Exact Conversation Windowing

- Status: Implemented and verified
- Date: 2026-08-11
- Risk: Medium

## 1. Requirement summary

A conversation deep link must become interactive quickly even when the retained conversation contains thousands of activities. The requested trace/span must remain visible, and users must be able to load newer and older activities without reapplying dashboard time filters.

## 2. Requirement specification

1. An exact conversation response returns at most 100 activities.
2. A trace/span deep link returns a contiguous window containing the requested trace activity.
3. The response reports the window offset, total activity count, and whether newer or older retained activities exist.
4. Loading either direction remains source-qualified and independent of dashboard range and search filters.
5. Scrolling near the top loads newer activities; scrolling near the bottom loads older activities.
6. Pages merge without gaps, overlap, reordering, or duplicate rows while the retained dataset is unchanged. Live-insert stability is deferred to the cursor follow-up below.
7. The highlighted trace/span remains semantically marked after adjacent pages load.

## 3. Conceptual model

| Concept | Identity | Meaning |
|---|---|---|
| Conversation window | `(source, conversation, offset, limit)` | One contiguous slice of retained activities ordered newest first |
| Targeted window | Conversation window plus `(trace, span)` | Initial slice positioned so the target is included |
| Newer page | Slice immediately before the current offset | Activities prepended to the current window |
| Older page | Slice immediately after the current window | Activities appended to the current window |

Invariant: `activityOffset + activities.length <= activityCount`.

## 4. Responsibility assignment

| Responsibility | Owner |
|---|---|
| Choose the target-containing initial offset | SQLite exact-conversation adapter |
| Expose bounded window metadata | Source-neutral query `Session` projection |
| Validate paging parameters | HTTP boundary |
| Calculate the next contiguous page | Pure UI update function |
| Detect top/bottom paging intent | `am-activity-table` |
| Select exact versus dashboard paging transport | Application effect runner |

## 5. SOLID risk assessment

- The component must emit direction intent, not calculate API offsets.
- Dashboard range pagination and exact retained pagination must share page merge behavior without sharing their data-source assumptions.
- The storage adapter owns ordering and target positioning; the HTTP handler must not inspect activity arrays.
- Source-specific producer knowledge must not enter paging contracts.

## 6. Module boundary plan

```text
internal/query/overview.go                 conversation window metadata
internal/storage/sqlite/conversation.go   bounded exact window projection
internal/transport/httpapi/handler.go      exact paging parameters
web/src/model/update.ts                    directional page state transitions
web/src/components/activity-table.ts       directional scroll intent
web/src/app/agentmetry-app.ts              transport selection and composition
```

## 7. Interface proposal

```go
type ConversationFilter struct {
    SourceID, ConversationID string
    TraceID, SpanID          string
    ActivityOffset           int
    ActivityLimit            int
    UseActivityOffset        bool
}
```

```ts
type ActivityDirection = "newer" | "older";

type Session = {
  activityOffset: number;
  hasEarlier: boolean;
  hasMore: boolean;
};
```

`activities-needed` carries `{ direction }`. The existing activity page effect gains exact-route identity and an explicit offset/limit. Exact continuation requests retain the original trace/span identity and send `mode=page`; this preserves the same projected activity set while `UseActivityOffset` disables initial target positioning.

## 8. Test specification

1. A 7,000-activity exact conversation returns no more than 100 activities.
2. A target beyond the first page is contained in the returned window.
3. Targeted responses report correct earlier/later flags and offset.
4. Newer page receipt prepends and lowers the offset.
5. Older page receipt appends and preserves the offset.
6. Top and bottom scroll thresholds emit the correct direction.
7. Exact paging calls the range-independent conversation endpoint.
8. Normal dashboard paging continues to use the existing range-aware endpoint.
9. Continuation pages retain an unknown-kind target in the projection without re-centering the requested offset.

## 9. Detailed design

The SQLite adapter first builds the complete source-qualified conversation projection so token contribution and agent metadata remain identical to the overview. It locates the target in the newest-first activity order, chooses a window beginning up to 25 rows before the target, and then applies a maximum 100-row slice. Requests without a target use the supplied offset. Continuations with a target retain that target in the projection but set `UseActivityOffset`, so the explicit offset wins instead of re-centering. The public response retains the complete counts and aggregates while bounding only `activities`. Optional SQL predicates are assembled from trusted fragments with parameterized values; this lets exact conversation lookups use the `run_id` index instead of scanning the time index across the complete telemetry database.

The UI stores the absolute offset of its current contiguous window. A newer request uses `max(0, offset - 100)` and prepends the returned page. An older request uses `offset + activities.length` and appends it. Exact-route pages use the exact conversation endpoint; dashboard pages retain their range-aware endpoint. Only one directional request may be active at a time. Each scroll-boundary entry is latched until the viewport leaves that boundary, preventing layout-driven scroll events from loading multiple pages for one gesture.

## 10. TDD construction plan

1. Add failing storage tests for target-containing bounded windows and explicit offsets.
2. Add failing pure update tests for prepend/append state transitions.
3. Add failing component tests for top/bottom directional intent.
4. Implement query metadata and bounded SQLite projection.
5. Implement HTTP paging validation and exact page response.
6. Implement UI state/effects and component composition.
7. Run Web and Go regression suites, production build, and live 7,000-activity URL verification.
8. Review boundary ownership and merge invariants.

## Verification record

- The reported 7,000-plus-activity conversation rendered in the browser with the requested span highlighted.
- The initial table contained 100 activities plus its header; both newer and older continuations were available.
- Loading older activities added exactly one 100-row page and remained at 200 activities after subsequent layout and scroll events.
- Adding an adjacent page no longer recenters the already revealed trace target.
- An adjacent page around an unknown-kind target preserves the initial activity count and returns the exact next retained row without a gap.
- The exact API response decreased from approximately 4.3 MB to 121 KB.
- After removing optional `OR` predicates from the exact query path, response time on the 4-million-span local database decreased from approximately 9 seconds to 0.4 seconds in the final live check.
- All Go tests, `go vet`, all 45 Web tests, and the production Web build passed.

## Follow-up architecture work

The current v1 query keeps exact conversation totals and agent rollups authoritative by projecting the complete conversation before slicing its displayed activities. The indexed query meets the observed local interaction need, but a larger-scale implementation should separate summary aggregation from the bounded activity-window query so adjacent-page requests do not rebuild the summary.

Numeric offsets are a documented v1 trade-off. A live producer can insert newer records while a user is paging, shifting later offsets. A future continuation contract should expose opaque earlier/later cursors backed by a deterministic storage ordering key, while cursor encoding remains private to the storage adapter.

Cursor acceptance tests must cover equal timestamps across signal types and an insertion between two adjacent-page requests. The cursor should include a snapshot watermark and a total ordering key such as `(observed_at, signal, persisted row identity)`.
