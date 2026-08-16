# Trace Navigation Correction

- Status: Implemented
- Date: 2026-08-11
- Risk: Medium

## 1. Requirement summary

Clicking a reported trace ID in an activity row must open the Trace Explorer. The behavior must work in the production build and must not depend on a custom event crossing nested shadow roots.

## 2. Requirement specification

- An activity with a valid trace ID renders one accessible same-origin link.
- The link target is `/traces/{percent-encoded trace ID}`.
- An activity without a trace ID renders no invented navigation target.
- Loading the target URL reconstructs the Trace Explorer through the existing route parser and query effect.
- Browser-native navigation remains available when JavaScript event delegation fails.
- The Trace Explorer return control is always a native `/` link. It uses browser history only when it was opened from the same-origin dashboard root or a source-qualified conversation route and a prior history entry exists.

## 3. Conceptual model

`TraceReference` is navigation evidence owned by an activity. It points to a `TraceResource` identified by an OTLP trace ID. Navigation is represented by a URL, not by an imperative component-to-application command.

## 4. Responsibility assignment

| Responsibility | Owner |
|---|---|
| Build the trace resource URL | `am-activity-table` |
| Navigate to the resource | Browser anchor semantics |
| Interpret the route and load trace data | `am-app` |
| Return to the originating dashboard state | Browser history, guarded by the same-origin referrer |
| Render trace evidence | Trace Explorer components |

## 5. SOLID risk assessment

- The table must not fetch traces or mutate application state.
- The application must not inspect activity-table shadow DOM.
- The trace URL remains a stable interface between the two responsibilities.
- Removing the custom intent event reduces hidden temporal coupling.

## 6. Module boundary plan

- Change only the activity trace affordance and its behavior tests.
- Remove obsolete trace-event registration from the application shell.
- Reuse the existing deep-link route and Trace Explorer query path unchanged.

## 7. Interface proposal

```html
<a class="trace" href="/traces/{encodedTraceId}" aria-label="Open trace {traceId}">
  {shortTraceId}
</a>
```

## 8. Test specification

1. A reported trace ID renders a link with the correct accessible name and encoded path.
2. Missing trace identity renders no link.
3. Loading the link path fetches and renders the Trace Explorer.
4. The HTTP server returns the embedded SPA for the trace path.
5. A same-origin dashboard referrer plus a usable history entry selects browser history return; an absent, foreign, or new-tab history selects the root fallback.
6. The production build passes browser QA by resolving the real link and its destination.

## 9. Detailed design

`am-activity-table` derives a pure URL from the reported trace ID and renders a native anchor. No click handler, `CustomEvent`, host listener, or `history.pushState` call is required for opening a trace. Full-page same-origin navigation is supported because all trace state is reconstructible from the URL and local API. The close action is a native `href="/"` anchor. Its handler uses `history.back()` only when `document.referrer` identifies the same-origin dashboard root or a valid source-qualified conversation route and `history.length > 1`, allowing the browser to restore its prior selected conversation and span. Direct, foreign, and new-tab deep links use the root fallback; the anchor also works when application JavaScript is unavailable.

## 10. TDD construction plan

1. Change the component test to require an anchor and encoded `href`; confirm it fails.
2. Replace the button/event implementation with the smallest native link.
3. Remove obsolete application event wiring and update the composition test.
4. Run web tests and production build.
5. Activate the link in a real browser and verify the Trace Explorer URL and content.
6. Review for dead event code and navigation duplication.
