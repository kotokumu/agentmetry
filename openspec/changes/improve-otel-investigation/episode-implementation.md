# Episode access implementation

## Scope and Result

| Item | Result |
|---|---|
| Accepted task | 2.3: access every reported episode; episode-side evidence navigation and return-focus integration |
| Owned runtime files | `web/src/components/rework-summary.ts`; `web/src/components/rework-summary.test.ts` |
| Behavior | First three episodes remain visible initially. Show more reveals further groups of three until all reported episodes are reachable. The reported count and existing partial-analysis label remain visible |
| Identity state | Expansion survives new analysis objects/loading for the same source and conversation. Changing source or conversation resets the initial limit |
| Link contract | Optional `locationForTrace(traceId, spanId?)` defaults to an encoded trace URL with `spanId`. Ordinary clicks emit bubbling/composed `trace-selected` with sourceId, conversationId, traceId, spanId and `evidenceOrigin: "episode"`. Modifier and non-primary clicks keep native behavior |
| Focus contract | `focusEvidence(traceId, spanId): Promise<boolean>` reveals an episode beyond the initial three, awaits rendering, checks conversation identity, and focuses the matching evidence link. An unavailable target returns false without replacing current focus or expansion |
| Integration ownership | Root connects the callback/event/focus method through workspace and navigation. This component change does not update task checkboxes or other UI files |

---

## Red-Green-Refactor Evidence

| Cycle | Red | Green | Refactor |
|---|---|---|---|
| All reported episodes | First test fails because Show more is absent | One test passes after replacing the API-only remainder card with an incremental button and reported count | Rendering moves into the component because expansion is component-owned state; existing ordering and coverage remain |
| Identity-scoped expansion | Two new cases fail with five links after switching conversation/source; same-conversation live case passes | All four tests pass after resetting only on source-qualified identity change | State uses explicit source and conversation fields; temporary loading does not invent a new identity |
| Evidence navigation and focus | Four cases fail: trace-only default href, unused location callback, and absent focus API | All thirteen tests pass after exact span links, source-qualified event and focus method | One sorted-episode getter serves rendering and focus lookup; query-selector interpolation is avoided |

Focused command: `npm --prefix web test -- --run src/components/rework-summary.test.ts`.

Recorded final focused result: **13 tests passed**. Cases include partial coverage, episodes four/five, live identity retention, both identity reset dimensions, exact/default/custom links, all four modifier keys, non-primary click, hidden-origin focus and missing-origin behavior.

---

## Related Verification

| Check | Observed Result | Remaining Owner |
|---|---|---|
| `npm --prefix web test -- --run src/components/rework-summary.test.ts src/components/components.test.ts` | 62 passed, 2 failed. Both failures are activity-table assertions for old trace-only links in `components.test.ts` at lines 569 and 609; episode tests pass | Root owns concurrent trace URL/UI expectation updates |
| `web/node_modules/.bin/tsc -b web` | Stops at `navigation.test.ts:72`: pending `NavigationViewState.evidenceFocus` property. No episode-file diagnostic is reported | Root owns navigation state integration |
| `git diff --check -- web/src/components/rework-summary.ts web/src/components/rework-summary.test.ts` | Pass | Complete for these files |

The related checks are not recorded as complete integration success. Root receives the exact failures for reconciliation with parallel changes. Browser focus/return verification remains part of the integrated investigation journey.
