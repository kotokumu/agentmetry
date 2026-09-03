# Shared comparison and purpose views

2026-09-04. Tasks 3.4 and 4.1 are implemented.

The Web client consumes CompareRework rows with their raw numerators, denominators, values, deltas and missing-value reasons. Local comparison formulas were removed after the query implementation passed. Candidate selection remains a UI affordance; the server validates eligibility on a coherent snapshot. Source-qualified keys prevent displaying a result for a different pair. Live changes to either subject refresh the pair. Unsupported RPCs show an unavailable state and retry.

The shared explicit fixture covers all five rows, null versus reported zero, partial coverage and harness context. One-decimal rounding is presentation only. A delta of 0.04 remains in the mapped result but displays 0.0 without a misleading plus sign. The original renderer failed that assertion before the formatter correction. Client/controller/model/component/app checks: 48 tests PASS; TypeScript and Vite build PASS. SQLite→Connect→MCP parity and snapshot concurrency verification are recorded in implementation-log.md and snapshot-verification.md.

Execution, Rework and Comparison controls select purpose views in the existing workspace. Selection remains in source-qualified conversation state and uses a stable activity ID. A purpose change creates a history entry. Browser back restores purpose, agent and activity; the existing trace origin state also retains them. Panels stay mounted to preserve local paging and episode expansion, while inactive panels are hidden.

Before implementation, navigation decoding discarded purpose/activity and the app had no purpose controls (two failing behavior tests). After implementation, navigation and app tests: 35 PASS. The expanded app test switches purpose with an agent/activity selected, goes back, and verifies selection plus the existing conversation/scroll restoration. Actual selected-body and visual accessibility checks are recorded separately when complete.
