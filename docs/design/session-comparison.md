# Session comparison

## 1. Requirement summary

Agentmetry must let a user compare the selected conversation with an earlier
conversation from the same telemetry source. The comparison turns existing
session diagnostics into an explicit Before/After view without presenting the
result as a productivity score or causal proof.

## 2. Requirement specification

### User-visible behavior

- The conversation workspace shows a comparison panel after Development
  rework.
- Eligible baselines are the non-overlapping, same-source conversations in the
  currently loaded conversation list. The default is the one that ended most
  recently before the current conversation started. A user can choose another
  eligible baseline.
- The panel compares normalized signals: initial validation success, rework
  token share, detected retry-cycle agent-effort share, tool failure rate, and
  recurring failure loops per 100 outcome-known validation attempts.
- Every row shows Baseline, Current, the change in percentage points or
  normalized rate units, and the row-specific evidence denominator.
- The direction label is `Improved`, `Regressed`, `No change`, or
  `Not comparable`. Direction reflects the desired direction of that metric.
- Missing numerator/denominator evidence remains unavailable; it is never
  converted to zero.
- The panel states that two sessions are diagnostic observations, not causal
  evidence, and shows the validation sample sizes used by each session.

### Constraints and non-goals

- Baselines must be from the same producer/source and end no later than the
  current conversation starts. Cross-provider and overlapping-session
  comparisons are excluded because source span profiles and concurrent work
  are not yet calibrated.
- Candidate discovery is deliberately scoped to the currently loaded,
  filter-bounded conversation list (at most 100 items). The UI names this
  scope; it does not claim to find the nearest session outside that view.
- This slice does not infer a task type, AGENTS.md version, model/config change,
  or causal relationship.
- It does not aggregate cohorts or persist a named experiment.
- It reuses `GetSessionRework`; no new public RPC is required for an
  interaction-local comparison.

### Acceptance criteria

1. A current conversation with eligible sessions in the loaded list selects
   the most recently ended eligible baseline deterministically. Ties use the
   lexicographically smaller session ID. Invalid timestamps are ineligible.
2. Changing the baseline loads only that source-qualified conversation and
   stale results are never rendered.
3. The five formulas and desired directions are:

   | Metric | Formula | Unit | Better |
   |---|---|---|---|
   | Initial validation success proxy | `first_pass_successes / first_pass_eligible_validations` | percent | higher |
   | Rework token share | `rework_tokens.total / session_tokens.total` | percent | lower |
   | Detected retry-cycle effort share | `rework_duration / total_agent_effort` | percent | lower |
   | Tool failure rate | `tool_failures / tool_attempts_with_outcome` | percent | lower |
   | Recurring loops | `recurring_failure_loops / validation_attempts_with_outcome * 100` | per 100 validations | lower |

4. A missing value, non-positive denominator, non-finite value, negative
   numerator, or numerator greater than its denominator renders `—`, an
   availability reason, and `Not comparable`. An observed zero numerator with
   a positive denominator remains a valid zero.
5. A baseline request failure is isolated to the comparison panel and can be
   retried.
6. Current rework loading/failure behavior remains unchanged.
7. Partial retained projection remains comparable but shows a per-side warning;
   it is not silently treated as complete evidence.
8. Switching current conversation resets any explicit baseline selection.
   During baseline switching, old values are hidden until the new identity is
   loaded; aborted requests never render errors.
9. Values are displayed to one decimal place. A delta whose formatted absolute
   value is below `0.1` percentage points or per-100 units is `No change` and
   renders `0.0`, avoiding contradictory labels and negative zero.
10. The initial-success label always includes `proxy`; its explanation states
    that identities are agent + operation + normalized command + working
    directory, not task or change boundaries.

## 3. Conceptual model

| Concept | Meaning | Invariants |
|---|---|---|
| Comparison subject | selected source-qualified conversation | differs from baseline |
| Baseline candidate | visible same-source conversation completed before current starts | ordered by end time descending, then ID ascending |
| Comparison observation | one named normalized diagnostic | both values use the same formula |
| Direction | interpretation of the current-minus-baseline delta | unavailable if either value is missing |
| Diagnostic snapshot | identity-checked minimal values for one session | session and analysis identities must match |
| Comparison evidence | numerator, denominator, availability reason, and projection coverage for each side | remains visible and non-causal |

`Session + ReworkAnalysis -> DiagnosticSnapshot`

`Baseline snapshot + Current snapshot -> ComparisonObservation[]`

## 4. Responsibility assignment

| Responsibility | Owner | Reason |
|---|---|---|
| Load and stale-check baseline summary/rework | `SessionComparisonController` | independent failure and cancellation lifecycle |
| Choose eligible/default baseline | pure comparison model | deterministic domain rule over visible candidates |
| Derive normalized comparison values | comparison model | pure, testable domain presentation logic |
| Render selector, table, limitations, retry | `am-rework-comparison` | feature-local interaction and display |
| Compose the panel | `am-conversation-workspace` | existing conversation layout boundary |
| Fetch one report | `AgentmetryClient` | existing transport adapter, unchanged |

## 5. SOLID risk assessment

- SRP: comparison math must not accumulate in the workspace or either existing
  conversation controller. Baseline query lifecycle gets its own small
  controller.
- OCP: metric definitions are declarative descriptors so another normalized
  signal can be added without branching through the renderer.
- ISP: the component receives comparison-specific state instead of the entire
  controller.
- DIP: the pure comparison model depends on web model values, not generated
  protobuf messages.
- Avoid a premature server comparison abstraction until experiments/cohorts
  require a durable public contract.

## 6. Module and package boundary plan

- `web/src/model/rework-comparison.ts`: pure snapshot and comparison behavior.
- `web/src/components/rework-comparison.ts`: selector and accessible table.
- `web/src/controllers/session-comparison-controller.ts`: baseline query
  lifecycle, cancellation, stale-result protection, and retry.
- `web/src/components/conversation-workspace.ts`: composition only.
- Existing API, query analyzer, storage, and protobuf boundaries remain
  unchanged.

Dependencies point from component/controller to the pure model. The model has
no DOM, transport, generated-code, or controller dependencies.

## 7. Interface proposal

```ts
type ComparisonValue =
  | Readonly<{ availability: "available"; displayValue: number; numerator: number; denominator: number }>
  | Readonly<{ availability: "unavailable"; reason: string; numerator: number | null; denominator: number | null }>;

type ReworkComparisonRow =
  | Readonly<{
      availability: "comparable";
      id: ComparisonMetricID;
      baseline: AvailableComparisonValue;
      current: AvailableComparisonValue;
      delta: number;
      direction: "improved" | "regressed" | "unchanged";
      unit: "percent" | "per100";
    }>
  | Readonly<{
      availability: "unavailable";
      id: ComparisonMetricID;
      baseline: ComparisonValue;
      current: ComparisonValue;
      unit: "percent" | "per100";
    }>;

buildReworkComparisonReport(
  baselineSession: Session,
  baselineAnalysis: ReworkAnalysis,
  currentSession: Session,
  currentAnalysis: ReworkAnalysis,
): ReworkComparisonReport;
```

Percentages use display units throughout the contract (`25.0` means `25.0%`),
so values and percentage-point deltas cannot be mixed. Available/unavailable
values and comparable/unavailable rows are discriminated unions; contradictory
states such as an unavailable row with a direction are not representable.

The dedicated controller depends on the narrow `SessionComparisonReader`
interface (`getSessionSummary` and `getSessionRework` only). It exposes one
`ReworkComparisonViewState` to the component, plus `selectBaseline` and
`refresh` commands. Transport errors are mapped to stable user-facing state at
this boundary.

## 8. Test specifications

### Pure behavior

- Given complete reports, a table-driven test covers improved, regressed, and
  unchanged behavior for all five formulas and desired directions.
- Given a missing rate, absent token total, or zero validation denominator, the
  affected row is unavailable while other rows remain comparable.
- Values at, below, and above the one-decimal display boundary produce labels
  consistent with their formatted delta and never render negative zero.
- Session/analysis identity mismatch, invalid ratios, and invalid timestamps
  are rejected instead of producing a comparison.

### Controller behavior

- Selects the most recently ended eligible visible same-source session and
  excludes current, overlapping, future, invalid-time, and other-source
  sessions.
- Explicit baseline selection triggers a source-qualified request.
- Rapid baseline/current/filter changes and disconnect never expose a stale
  prior response. Old rows disappear while the new baseline loads.
- Failure and retry remain isolated from current conversation/rework state.

### Component behavior

- Renders a labelled selector, semantic table caption/headers,
  Baseline/Current/Change columns, per-row evidence denominators, projection
  warnings, and the non-causal qualification.
- Dispatches bubbling/composed `comparison-baseline-selected` with a session ID
  and `comparison-retry-requested` once per user action.
- Renders empty, loading, error, and partially comparable states accessibly.

### Integration regression

- Workspace passes the selected session and current/baseline analyses to the
  comparison component without changing existing rework behavior.

## 9. Detailed design

The pure model derives candidates from the filter-bounded session list already
loaded by the workspace. Eligibility requires matching `sourceId`, a different
session ID, valid timestamps, and `baseline.endedAt <= current.startedAt`.
Candidates sort by descending end time and then ascending session ID. An
explicit baseline is scoped to the current source/session identity; changing
current resets it. If it becomes ineligible, the first candidate is used.

`createReworkDiagnosticSnapshot` rejects source/session identity mismatches and
copies only the values needed for comparison. Each metric validates its own
numerator and denominator. Projection coverage is retained as a warning, not a
claim that capture is complete. Delta is current minus baseline. Direction is
derived after rounding the displayed percentage-point or per-100 delta to one
decimal place. Labels and formatting stay component-local.

The component uses a native labelled `select`, semantic table with caption and
scoped headers, `role=status` for loading, and `role=alert` for retryable
failure. It never uses green/red alone: direction is always rendered as text.
Baseline IDs are shortened visually but preserved in option titles. The empty
state explains that candidates are limited to non-overlapping conversations in
the current list.

## 10. TDD construction plan

1. Red: pure comparison tests for complete and missing evidence. Green: add the
   model. Refactor: metric descriptors and shared direction logic.
2. Red: controller tests for default, explicit, stale, and retry behavior.
   Green: add the baseline task and source-qualified getters. Refactor: reuse
   conversation identity helpers.
3. Red: component tests for rows, events, and states. Green: add the component.
   Refactor: isolate formatting and keep selection state outside the component.
4. Red: workspace composition assertion. Green: wire properties/events.
5. Run the complete Web suite and production build, then browser-check the
   comparison with representative local telemetry.
