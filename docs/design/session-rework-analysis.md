# Session Rework Analysis

- Status: Implemented
- Date: 2026-08-16
- Risk: Medium

## 1. Requirement summary

Agentmetry must normalize Claude Code and Codex OTLP activities into a producer-neutral event vocabulary and calculate evidence-backed development-efficiency/rework indicators for one source-qualified session. Missing telemetry must remain unknown instead of being treated as success or zero.

## 2. Requirement specification

### Functional requirements

1. Project every retained session activity into a canonical event containing source/run/agent identity, operation, target, observed outcome, duration, token usage, and evidence identity.
2. Classify operations as `read`, `edit`, `execute`, `test`, `build`, `lint`, `api_call`, or `other` from normalized tool names, event names, and command attributes.
3. Extract file and command targets from common semantic attributes, provider payloads, and patch headers without source-specific logic in the analyzer.
4. Preserve outcome as tri-state: `true`, `false`, or unavailable.
5. Calculate per-session test/build/lint failure count, fail-fix-retry cycles, rework duration/tokens, tool failure rate, API retry waste, repeated command count, and repeated file edit count.
6. Return the report through a read-only `analyze_rework` MCP tool using explicit `source` and `runId` identity.
7. Return evidence for detected cycles and capability states for metrics that cannot be supported from OTLP alone.

### Non-functional requirements

- Analysis is deterministic and producer-neutral after canonicalization.
- Token totals include only activities already selected as authoritative contributions.
- Duration aggregation must not double-count nested/overlapping activities from the same agent.
- Existing stored OTLP remains replayable and no database migration is required.
- Reports disclose projection completeness and heuristic limits.

### Inputs and outputs

- Input: a verified source-qualified session and up to 1,000 retained meaningful activities.
- Output: one versioned rework report, canonical coverage counts, typed token measurements, detected cycles, evidence, and unsupported capability states.

### Normal cases

- A failed test, a later file edit, and the same test command retried form one fail-fix-retry cycle.
- A command or file edited more than once by the same agent increments the respective repeat count after its first occurrence.
- An explicitly failed tool attempt contributes to the tool failure numerator; only attempts with known outcomes contribute to its denominator.

### Error cases

- Invalid or unknown source-qualified run identity returns the existing run-not-found error.
- Malformed embedded provider payloads are ignored for target/outcome extraction rather than failing the report.

### Edge cases

- Missing success is unknown, not success.
- Zero reported tokens remain distinguishable from missing tokens.
- Concurrent/nested event intervals are merged per agent for rework duration.
- Duplicate activity evidence contributes tokens and duration at most once.
- Targetless retries fall back to stable operation/tool identity; unrelated targetless operations do not match solely because they are unknown.

### Acceptance criteria

- Both Claude and Codex-shaped attributes normalize to the same operation/target model.
- All requested metrics supported by retained OTLP have behavior tests.
- `analyze_rework` returns source/run identity, metrics, cycles, evidence, and completeness metadata.
- Revert detection and cross-agent artifact overlap are explicitly marked unavailable rather than guessed.

### Non-goals

- Proving semantic code reverts without before/after file content or VCS diffs.
- Proving cross-agent duplicate work without stable artifact/change identities.
- Inferring task outcome, hidden chain of thought, or developer intent.
- Persisting a second canonical-event table in the first iteration.

### Risks and open questions

- Provider command and file attributes may change; extraction is best-effort and coverage is reported.
- API retry identity is not always emitted, so retry matching uses agent plus observed target/model/event identity and is heuristic.
- Rework is an operational proxy, not a direct measure of code quality or human productivity.

## 3. Conceptual model

| Concept | Meaning | State | Behavior | Constraint / invariant |
|---|---|---|---|---|
| Canonical event | Producer-neutral activity used by the analyzer | identity, operation, target, tri-state success, interval, usage | exposes stable match/evidence keys | missing outcome remains unknown |
| Operation | Development action category | read/edit/execute/test/build/lint/api_call/other | classifies tool/event/command evidence | test/build/lint take precedence over generic execute |
| Target | Artifact or command acted on | optional file and command | supplies repeat/retry identity | empty fields are not invented |
| Failure | Explicit unsuccessful event | canonical event with `success=false` | starts retry/API-waste tracking | unknown is not failure |
| Rework cycle | failure followed by edit and matching retry | failure, corrective events, retry | contributes deduplicated effort/tokens | retry matches stable operation target |
| Rework report | Session-level derived projection | metrics, cycles, coverage, capabilities | aggregates canonical events | every value names its evidence/availability |

### Relationships

| Concept A | Relationship | Concept B | Notes |
|---|---|---|---|
| Session activity | projects to | Canonical event | projection is read-time and lossless storage remains authoritative |
| Failure | opens | Rework cycle | a corrective edit and matching retry close it |
| Canonical event | contributes to | Rework report | contribution is deduplicated by evidence identity |
| Target | identifies | repeat/retry | agent identity scopes command/file repetition |

### Structural risks

- Missing concepts: a future change/artifact identity is needed for reliable revert and overlap analysis.
- Hidden state: unknown success and missing token counters must not collapse into false/zero.
- Change-prone areas: attribute/command extraction and operation classification.
- Boundary candidates: canonical projection hides provider payload shapes; analyzer hides metric rules.

## 4. Responsibility assignment

| Responsibility | Owner | Reason to change | SOLID concern | Not owner | Reason |
|---|---|---|---|---|---|
| Retain raw normalized attributes | SQLite activity adapter | storage/query projection changes | DIP | analyzer | analyzer must not issue SQL or parse DB rows |
| Define canonical operation/event vocabulary | `internal/canonical` | vocabulary/contract changes | SRP | source plugins | shared vocabulary is producer-neutral |
| Project activity attributes into canonical events | `internal/query` canonicalizer | telemetry semantic extraction changes | OCP/SRP | MCP handler | transport must not contain analysis rules |
| Detect cycles and aggregate metrics | `internal/query` rework analyzer | metric rule changes | SRP | SQLite/MCP | policy remains pure and testable |
| Expose read-only report | MCP service | external tool contract changes | ISP | analyzer | analyzer does not know MCP schemas |

### SOLID risk assessment

| Principle | Risk | Mitigation |
|---|---|---|
| SRP | One large analyzer mixes parsing and aggregation | Separate canonicalization from report aggregation |
| OCP | Source-specific branches spread into metrics | Normalize provider aliases before analysis and classify semantic evidence only |
| LSP | Unknown outcomes silently act like success | Use optional success and explicit availability contracts |
| ISP | Existing broad reader grows for derived analysis | Reuse existing summary/activity reader contracts; add no storage interface |
| DIP | Core rules depend on MCP or SQLite DTOs | Keep rules as pure functions over query activities/canonical events |

### Procedural risk

- Rules at risk in handlers: retry matching, token deduplication, and failure-rate denominator.
- Behavior that belongs near state: event matching/evidence identity and report aggregation.
- Premature abstractions: provider-specific strategy classes and a persisted event repository.

## 5. Module/package boundary plan

```text
source profiles + OTLP normalizer
            ↓
lossless span/log attributes (SQLite)
            ↓
query.Activity + internal-only attributes
            ↓
canonical event projection → pure rework analyzer
            ↓
analyze_rework MCP output
```

- `internal/canonical`: stable event/operation/target value types.
- `internal/query`: pure activity-to-event projection and session analysis rules.
- `internal/storage/sqlite`: hydrates retained attributes for internal analysis only.
- `internal/transport/mcpserver`: maps the report to a bounded public schema.

## 6. Interface/signature proposal

| Name | Consumer | Responsibility | Signature | Error contract |
|---|---|---|---|---|
| `CanonicalizeActivity` | rework analyzer/tests | project one activity | `func CanonicalizeActivity(Activity) canonical.Event` | total function; absent/malformed evidence remains unknown |
| `AnalyzeRework` | MCP service/tests | calculate a session report | `func AnalyzeRework(Session, []Activity) ReworkReport` | total function; report carries completeness |
| `analyze_rework` | AI/MCP client | analyze explicit run | `{source, runId} -> ReworkAnalysisOutput` | existing validation/run-not-found errors |

Example call site:

```go
summary, activities, err := service.loadRunAnalysis(ctx, input)
report := query.AnalyzeRework(summary, activities)
```

### Boundary decisions

| Boundary | Hidden detail | Reason |
|---|---|---|
| Canonicalizer | nested/stringified provider arguments | isolate schema churn |
| Analyzer | state machines, interval merging, deduplication | keep policy out of transport |
| MCP mapper | Go durations, optional counters, internal attributes | provide stable JSON contract without leaking bodies |

### Interface risks

- Oversized interfaces: none; existing reader contracts are reused.
- Primitive obsession: operation, outcome, target, availability, and cycle are named types.
- Infrastructure leakage: canonical events never expose SQL rows or OTLP SDK types.
- Boolean flag risks: success is optional state, not a mode-switch argument.

## 7. Test specifications

| Behavior | Given | When | Then | Level | Notes |
|---|---|---|---|---|---|
| Canonicalize provider events | Claude/Codex-shaped command and file attributes | projected | same operation/target vocabulary | unit | includes stringified nested arguments |
| Preserve unknown outcome | no status/outcome evidence | projected | success is unavailable | unit | never defaults true |
| Count validation failures | failed test/build/lint events | analyzed | each explicit failure is counted | unit | unknown excluded |
| Detect fail-fix-retry | failure, edit, matching retry | analyzed | one cycle with evidence | unit | unrelated retry excluded |
| Aggregate rework effort | overlapping same-agent cycle events and authoritative tokens | analyzed | duration merged and tokens deduplicated | unit | different agents may contribute in parallel |
| Compute tool failure rate | failed/succeeded/unknown tool attempts | analyzed | failed / known outcomes | unit | null when denominator is zero |
| Detect API retry waste | failed API attempt followed by matching call | analyzed | count/duration/tokens reflect failed attempts | unit | heuristic confidence |
| Count repeats | repeated same-agent command/file targets | analyzed | only occurrences after first count | unit | normalized whitespace |
| Expose MCP report | verified source/run fixture | tool called | report and metadata returned | service | read-only |

### Invariant tests

| Invariant | Example | Expected result |
|---|---|---|
| Unknown is not false | command with no status | no failure contribution |
| Token authority preserved | corroborating duplicate usage | counted once/not included |
| Evidence deduplicated | same event appears in overlapping cycles | one duration/token contribution |
| Deterministic ordering | input activities reversed | identical metrics/cycle order |

### Error/edge tests

| Case | Given | When | Then |
|---|---|---|---|
| Malformed argument JSON | invalid string payload | canonicalize | target absent, no panic/error |
| Empty retry target | two unrelated unknown operations | analyze | no accidental cycle |
| No known tool outcomes | tools with missing success | analyze | failure rate unavailable |
| Partial activity page | activity count exceeds loaded events | analyze | coverage marked partial |

### Testability feedback

- Interface concerns: canonicalization must remain a total pure function.
- Responsibility concerns: MCP tests should assert mapping, not recreate metric rules.
- Coupling concerns: attributes are internal-only on `query.Activity`.

## 8. Detailed design

Activities are sorted by observed execution time before projection. Operation classification first recognizes test/build/lint commands, then semantic tool/event names, then generic execution/model calls. Target extraction checks normalized top-level semantic keys, recursively decoded `arguments`/`input`/`tool_input`, and patch headers. Outcome extraction uses span status, explicit success/error fields, exit codes, and failure event names in that precedence order.

Fail-fix-retry state is keyed by agent plus operation and target. An explicit failure opens a candidate; at least one later edit marks it corrective; a later matching operation closes a cycle. Rework effort is the union of canonical events covered by detected cycles: durations are merged per agent and typed token usage includes only authoritative contributions. API retry waste independently counts explicit failed API attempts that have a later matching API call.

Repeated commands and file edits are scoped by agent. Each occurrence after the first increments the metric. Tool failure rate uses only tool events with known outcomes. Report coverage includes total/classified/outcome-known event counts plus full/partial activity projection state.

`change_revert_detection` and `cross_agent_work_overlap` are returned as unavailable with a reason. Their future implementation requires stable file-change content hashes or VCS patch identities, not command-name coincidence.

## 9. TDD construction plan

| Behavior | Red test | Green implementation | Refactor target |
|---|---|---|---|
| Canonical event projection | provider-shaped activity table tests | event types and canonicalizer | extraction helpers and vocabulary names |
| Core metrics/cycles | scenario table tests | deterministic analyzer | explicit candidate/cycle concepts |
| Effort/token dedupe | overlapping interval/token tests | per-agent merge and evidence set | shared evidence key behavior |
| MCP contract | service fixture tool test | output mapper/tool registration | reuse metadata and token mapping |
| Storage attribute hydration | SQLite session activity test | select/scan attributes JSON | keep attributes internal-only |

## 10. Construction log

### Canonical event projection

- Red: provider-shaped command, file, patch, build, lint, stdin, malformed-payload, and tri-state outcome tests failed before the event model/canonicalizer existed.
- Green: added producer-neutral operation/event/target types and total activity canonicalization.
- Refactor: isolated extraction, command normalization, operation classification, and outcome parsing helpers; corrected `write_stdin` classification.

### Rework metrics

- Red: session scenario tests failed before failure/cycle/effort/repeat/API rules existed.
- Green: added deterministic cycle detection, repeat counters, failure rate, API retry waste, and coverage/capability output.
- Refactor: merged overlapping intervals per agent, deduplicated effort by evidence identity, and kept unknown outcomes out of denominators.

### Storage and MCP boundary

- Red: SQLite activity reads did not hydrate retained attributes or authoritative token-contribution state; MCP report test had no handler.
- Green: hydrated internal-only attributes, selected authoritative usage over the full activity projection, and added `analyze_rework`.
- Refactor: reused existing source-qualified readers, token mapping, pagination cap, and analysis metadata.

### Review findings

| Location | Finding | Resolution |
|---|---|---|
| canonical activity derivation | `gen_ai.model.error` was materialized as unknown and therefore absent from session analysis | classify model/API errors as response evidence |
| session activity reader | authoritative usage selection was missing outside overview/trace projections | select contributions before activity page slicing |
| command classification | substring matching could classify `write_stdin` as an edit | use explicit edit/read tool vocabulary before generic execution |
| rework effort | overlapping cycles/events could double-count duration and tokens | deduplicate evidence and merge intervals per agent |
| rework cycle window | unrelated concurrent agent activity could inflate one agent's rework | include only the failing/retrying agent's observed activity in that cycle |
| rework interval boundary | an activity extending past the retry could inflate rework time | clip activity intervals to the failure-to-retry window before merging |

No database schema migration was introduced. Revert and cross-agent overlap remain explicit unavailable capabilities until stable change identities exist.

## 11. Verification

- Focused canonical/query/storage/MCP/provider tests: passing.
- Full Go suite: passing.
- `go vet ./...`: passing.
- `git diff --check`: passing.

## 12. Dashboard extension design

### Requirement summary

The selected conversation view must show the same server-calculated rework indicators exposed through MCP. Opening or switching a source-qualified session loads its analysis independently from the conversation summary so analysis latency or failure does not hide the activity timeline.

### Requirement specification

- Add a source-qualified `GetSessionRework` Connect RPC.
- Display validation failures, fail-fix-retry cycles, rework time/tokens, tool failure rate, API retry attempts, repeated commands, and re-edited files.
- Show activity coverage and unsupported revert/cross-agent-overlap capabilities without converting unavailable data to zero.
- Keep analysis loading/error/retry state local to the rework panel.
- Do not reproduce classification or aggregation rules in TypeScript.
- Do not expose retained raw OTLP attributes or operation bodies through the new RPC.
- Preserve the existing session list, trace navigation, agent filtering, and activity pagination behavior.

Normal case: selecting a conversation renders its existing summary immediately and fills the rework panel when the analysis RPC completes. Error case: only the rework panel shows an alert and retry action. Edge cases: null failure rate/token total render as not reported; partial projections render a visible partial-evidence note; stale results from a previous conversation are never shown.

Acceptance criteria: the RPC maps the Go report without recomputation; the client maps optional values faithfully; session switching cancels or invalidates stale analysis; the panel is responsive and accessible; backend, controller, component, and composition tests pass.

Non-goals: dashboard-wide cross-session ranking, trend charts, persisted rework rollups, and drill-down into cycle evidence in this iteration.

### Conceptual model

| Concept | Meaning | State | Behavior | Constraint / invariant |
|---|---|---|---|---|
| Session rework resource | Read projection for one `(source, session)` | metrics, coverage, capabilities | maps one analyzer result | identity is always source-qualified |
| Rework panel state | Independent UI resource state | loading, ready, failed | renders/retries analysis | never blocks conversation content |
| Rework metric | Observed or derived scalar | value or unavailable | formats count/rate/time/token | unavailable is not zero |
| Evidence coverage | Completeness of analyzed activity projection | complete/partial, event counts | explains metric confidence | always displayed when partial |

Relationships: a selected conversation requests exactly one session rework resource; the controller owns its lifecycle; the panel only presents the typed model; the Connect adapter maps but does not calculate metrics.

Structural risks: stale async results during navigation, duplicated analysis rules in the browser, optional proto values collapsing to zero, and an oversized conversation component. Boundaries: storage/query owns report construction, Connect owns wire mapping, the client owns DTO mapping, the controller owns lifecycle, and a dedicated component owns presentation.

### Responsibility assignment

| Responsibility | Owner | Reason to change | SOLID concern | Not owner | Reason |
|---|---|---|---|---|---|
| Build a complete session report | SQLite `SessionReworkReader` | retained projection/query changes | DIP/SRP | Connect handler | transport must not page and analyze |
| Define wire contract | protobuf + Connect adapter | API compatibility changes | ISP | UI component | presentation must not know Go types |
| Map optional wire values | dashboard API client | generated DTO changes | SRP | controller | controller owns lifecycle only |
| Cancel/invalidate session analysis | conversations controller | navigation/lifecycle changes | SRP | component | component must not fetch |
| Render metrics/coverage/capabilities | `am-rework-summary` | visual/product changes | SRP | workspace/controller | keep main workspace composition small |

SOLID risks: adding analysis methods to broad readers is mitigated with a narrow `SessionReworkReader`; generated transport types stop at the API client; the component accepts one typed report plus explicit loading/error state; no provider-specific conditionals enter UI code.

Procedural risks: pagination and canonicalization must not move into the Connect handler; async request checks must remain in the controller task; metric formatting belongs in presentation helpers/component rather than controller branches.

### Module boundary plan

```text
SQLite retained activities → query.AnalyzeRework → SessionReworkReader
                                              ↓
                                  Connect GetSessionRework
                                              ↓
                         API client → ConversationsController
                                              ↓
                                  am-rework-summary
```

### Interface/signature proposal

| Name | Consumer | Responsibility | Signature | Error contract |
|---|---|---|---|---|
| `SessionReworkReader.GetSessionRework` | Connect dashboard adapter | read one complete report | `(context.Context, ConversationIdentity) (SessionRework, error)` | conversation not found or storage error |
| `GetSessionRework` RPC | web client | retrieve typed display projection | `{source_id, session_id} -> metrics/coverage/capabilities` | invalid argument, not found, internal |
| `agentmetryClient.getSessionRework` | controller | map generated response | `(sourceId, sessionId, signal?) => Promise<SessionRework>` | rejects request errors |
| `ConversationsController.rework` | workspace | current ready value | getter | undefined unless identity matches |
| `am-rework-summary` | workspace | render isolated panel | properties: `analysis`, `loading`, `error` | emits retry intent only |

Boundary decisions: cycle evidence remains in MCP for now; raw attributes never cross Connect; durations use milliseconds; tool failure rate remains optional; token counters preserve reported/missing semantics.

Interface risks: no boolean behavior flags are added; RPC is purpose-specific; the component receives a cohesive report instead of many primitive properties.

### Test specifications

| Behavior | Given | When | Then | Level |
|---|---|---|---|---|
| Read complete report | stored failure/edit/retry activities | storage reader called | analyzer report uses all authoritative activities | integration |
| Map optional metrics | report with missing rate/tokens | RPC called | proto optionals remain absent | adapter |
| Load selected report | session target changes | controller task runs | only matching report is exposed | controller |
| Isolate failures | rework request rejects | conversation succeeds | timeline remains and panel offers retry | component/composition |
| Render metrics | ready report | panel renders | eight KPI values and coverage text are visible | component |
| Render unavailable | missing rate/capability unavailable | panel renders | “Not reported”/“Not available” appear, never zero | component |
| Responsive composition | selected conversation | workspace renders | rework panel occupies full detail width | component |

Invariant tests: stale source/session results are hidden; partial coverage is never labeled complete; optional values never become zero; raw attributes are absent from the response schema.

Testability feedback: a narrow reader keeps transport tests stub-friendly; a dedicated panel avoids reaching into controller internals; controller tests use a deferred Promise to prove stale results are hidden.

### Detailed design

SQLite reuses one private full-activity loader for pagination and analysis, ensuring usage-contribution selection happens before page slicing. `GetSessionRework` loads the retained session summary and all meaningful activities once, then calls the existing pure analyzer. Connect maps scalar counts, milliseconds, typed tokens, coverage, and capability states.

The conversations controller owns a third Lit task keyed by active state, source, and conversation ID. Its ready getter verifies the returned identity before exposing the value. Disconnect, route clearing, and reconnection follow the same lifecycle as the conversation task. The workspace composes `am-rework-summary` after the session header and before traffic/topology. The panel uses eight compact KPI cards, a coverage note, and two capability notes; failures affect only this panel.

### TDD construction plan

| Behavior | Red test | Green implementation | Refactor target |
|---|---|---|---|
| Storage report read | SQLite session scenario | narrow reader + shared full loader | remove duplicate contribution logic |
| Connect contract | handler mapping test | proto messages/RPC + mapper | isolate optional mapping helpers |
| Client/controller lifecycle | deferred/stale-result tests | client mapper + keyed Lit task | reuse identity key |
| Panel states/metrics | component loading/error/ready tests | dedicated component | presentation format helpers |
| Workspace composition | selected-session test | compose panel | keep workspace render method declarative |

### Dashboard construction log

- Red: storage, Connect mapping, stale-session controller, panel-state, and app-composition tests failed before their respective boundaries existed.
- Green: added the complete-session reader and RPC, faithful web DTO mapping, a source/session-keyed Lit task, and an isolated eight-card rework panel.
- Refactor: shared full activity loading and authoritative usage selection, kept retry state inside the panel, and kept the workspace limited to component composition.

### Dashboard review findings

| Location | Finding | Resolution |
|---|---|---|
| storage reader | analyzing the visible activity page would make metrics depend on pagination | analyze the complete retained meaningful activity projection |
| wire/client mapping | an absent failure rate could collapse to `0%` | preserve proto optional presence as `null` and render “Not reported” |
| controller | a late response could appear under a newly selected session | key both task result and exposed value by source/session identity |
| workspace | an analysis failure could replace otherwise usable session content | give the panel independent loading, alert, and retry states |
| UI capability notes | unsupported metrics could be mistaken for zero | show “Not available” with the required evidence reason |

### Dashboard verification

- Web unit/component/composition suite: 77 tests passing.
- Web TypeScript and production Vite build: passing.
- Full Go suite: passing.
- `go vet ./...`: passing.
- `git diff --check`: passing.
- Browser visual QA: desktop four-column layout and 390 px single-column layout render without horizontal overflow.

## 13. Metric explanation interaction design

### Requirement summary

Every rework KPI must explain what it measures without forcing the user to leave the session. Pointer users can reveal the explanation by hovering or clicking a question-mark control; keyboard users can focus the same control and dismiss it with Escape. The explanation must distinguish the observed signal from its interpretation so a derived value is not mistaken for a complete productivity score.

### Requirement specification

- Each of the eight rework cards has a named help control.
- Hover, keyboard focus, or click reveals the same concise explanation.
- Mouse leave closes an unpinned explanation; a click keeps it open until a second click, blur, or Escape.
- The control reports expanded state and the explanation is exposed as a tooltip relationship.
- Cards without an explanation remain unchanged.
- Explanations contain the metric definition, a useful interpretation, and the main evidence limitation.
- The interaction must work at desktop and single-column mobile widths without horizontal overflow.

Normal case: hovering or clicking `?` reveals the selected metric definition. Error/edge cases: missing descriptions render no empty control; multiple cards have unique tooltip identities; Escape restores focus without leaving stale expanded state. Non-goals: changing metric formulas, adding persisted preferences, comparing sessions, and building evidence drill-down in this change.

Acceptance criteria: eight help controls render in the ready rework panel; pointer, click, focus, blur, and Escape states are tested; screen-reader names identify the metric; existing KPI consumers and tests remain compatible.

### Conceptual model

| Concept | Meaning | State | Behavior | Constraint / invariant |
|---|---|---|---|---|
| Metric explanation | Definition and interpretation of one KPI | immutable text | supplies contextual help | never changes the metric value |
| Help disclosure | Accessible transient presentation | closed, previewed, pinned | open/close/dismiss | one card owns one disclosure |
| KPI card | Metric value presentation | label, value, hint, optional explanation | renders optional disclosure | no help control without content |

Relationships: `am-rework-summary` owns metric-specific explanation language; `am-kpi-card` owns disclosure interaction and accessibility; the workspace and controller remain unaware of the interaction.

Structural risks: duplicating help state in the summary, clipping overlays inside the card surface, non-unique ARIA references, hover-only behavior, and tooltip overflow on mobile. Boundary candidates: keep explanatory copy at the feature component and generic interaction at the reusable KPI component.

### Responsibility assignment and SOLID assessment

| Responsibility | Owner | Reason to change | SOLID concern | Not owner | Reason |
|---|---|---|---|---|---|
| Define metric meaning | rework summary | formulas/product language change | SRP | generic KPI card | must remain feature-neutral |
| Manage hover/click/focus state | KPI card | disclosure interaction changes | SRP | workspace | composition should not own local state |
| Calculate metrics | query analyzer | evidence/formula changes | DIP | explanation UI | help text must not calculate |

Risks: SRP is protected by separating copy from interaction; OCP is served by one optional property rather than rework-specific branches; ISP/DIP are unchanged because no new service interface is introduced. Procedural risk is limited to local disclosure transitions; no global manager or generic tooltip framework is warranted.

### Module boundary and interface design

```text
Rework metric definition copy → am-kpi-card(description)
                                      ↓
                           accessible help disclosure
```

| Name | Consumer | Responsibility | Signature | Error contract |
|---|---|---|---|---|
| `KpiCard.description` | feature components | optional explanation content | `string` property | empty means no control |
| help disclosure events | pointer/keyboard users | transition local display state | native mouse/focus/keyboard handlers | Escape always closes |

Example: `<am-kpi-card label="Rework time" description="Observed duration …">`. Hidden details are Lit state and unique DOM identity; no framework or transport DTO crosses the boundary. Interface risks: one cohesive optional string avoids primitive flags; the card stays reusable; no infrastructure leakage.

### Test specifications

| Behavior | Given | When | Then | Level |
|---|---|---|---|---|
| Optional control | card has no description | render | no question-mark button exists | component |
| Hover preview | described card is closed | pointer enters/leaves | tooltip opens/closes | component |
| Click pin | described card is hovered | click then leave | tooltip remains until click/blur | component |
| Keyboard access | help button focused | focus then Escape | tooltip opens then closes | component |
| Accessible relationship | described card renders | inspect control | name, expanded state, and tooltip ID agree | component |
| Rework coverage | ready report renders | inspect cards | all eight KPIs have explanations | feature component |

Invariants: metric values never change when help opens; unique cards never share tooltip IDs; a closed tooltip reports hidden; empty descriptions introduce no focus stop. Edge cases: long explanation wraps inside its card, rapid pointer/click transitions do not leave `aria-expanded` stale, and mobile sizing stays within the card.

### Detailed design and TDD plan

The KPI host becomes the positioning context while its existing article keeps visual clipping. A sibling help control and tooltip sit above that surface. Local state distinguishes visible and click-pinned behavior; pointer/focus preview opens transiently, click toggles pinned state, blur and Escape reset both states. Rework-specific copy is supplied declaratively by the summary.

| Behavior | Red test | Green implementation | Refactor target |
|---|---|---|---|
| Accessible disclosure | help-state component test | optional description and local transitions | named close/open methods |
| Eight explanations | rework rendering assertion | pass concise definition copy | central explanation constants if repeated |
| Existing compatibility | full component suite | optional property defaults empty | remove feature-specific logic from card |

### Explanation construction log

- Red: the rework panel exposed no metric descriptions, and the generic KPI card had no accessible disclosure behavior.
- Green: added optional explanations with pointer preview, click pinning, focus access, blur dismissal, Escape handling, and unique ARIA relationships; supplied all eight metrics with definition and limitation copy.
- Refactor: kept metric language in the rework feature and generic interaction state in the KPI card; renamed labels that implied more certainty than the current rules provide.
- Verification: component/full Web suites and production build pass; desktop browser interaction renders above adjacent cards without horizontal overflow; keyboard Escape restores the closed state.

## 14. Independent metric review

Two independent agents reviewed the metrics: one for product/actionability and one for measurement validity. They agreed that the current report is useful as a session-level diagnostic signal, but is not yet a valid productivity score, developer ranking, or cross-provider comparison.

### Findings by priority

| Priority | Finding | Risk | Recommended direction |
|---|---|---|---|
| P0 | OTLP observations and logical operation attempts are one concept | span/log/provider emission density can duplicate counts | model `TelemetryObservation → OperationAttempt → ReworkEpisode` and count each attempt once |
| P0 | true zero and insufficient evidence are conflated | a session with missing outcomes can look better | give every metric availability, numerator/denominator, metric-specific coverage, confidence, and rule version |
| P0 | a cycle requires any edit and retry, but not a successful retry | “fix” and recovered behavior are overstated | model recovered, still-failing, and unresolved episodes; call weakly linked changes “suspected” |
| P0 | unresolved failures and task outcomes are absent | stopping early can look better than persisting to success | show final validation/outcome and open failure episodes as quality guardrails |
| P1 | raw counts have no workload denominator or baseline | long/difficult sessions look worse | add failure rate, retry-window active-time/token share, and comparable cohort median/range |
| P1 | scalar cards have no evidence drill-down | users cannot determine what to improve | list top cycles/commands/files with timeline links and confidence |
| P1 | tool/API/repetition categories mix healthy and unhealthy behavior | normal test reruns and iterative edits can be mislabeled waste | keep repetition neutral, categorize failures, and require stronger retry/change identities |
| P1 | retained projection completeness is not capture completeness | users can overtrust “complete” | separate stored projection, source capture, outcome, target, duration, and token coverage |
| P1 | source/model/rule differences are not versioned in the dashboard | trends and Claude/Codex comparisons can reflect instrumentation drift | expose rule/source-profile versions and compare only compatible cohorts |
| P2 | heuristic quality is not calibrated | rule changes can alter metrics without improving truth | maintain labeled Claude/Codex fixtures and track detection precision/recall by rule version |

### Display recommendation

The next dashboard hierarchy should make final recovery state, evidence confidence, rework active-time share, and rework token share the primary summary. Raw diagnostic counts belong below them. Each episode should expose failure, suspected corrective changes, retry result, time, tokens, confidence, and a timeline link. All cards are overlapping views of the same evidence and must be labeled non-additive.

### Changes applied in this iteration

- Added a visible “diagnostic signals—not a productivity score” qualification.
- Renamed `Fix/retry cycles` to `Failure/edit/retry` and `API retry waste` to `Estimated API retry` in the dashboard.
- Added per-card explanations that state the observed definition and principal false-positive/coverage limitation.
- Renamed complete status to `Complete retained projection`.

Formula/model changes, evidence drill-down, normalized shares, outcomes, confidence, and cohort trends remain follow-up work because they require explicit contract and conceptual-model changes rather than presentation-only edits.

### Post-implementation review

| Location | Issue | Principle | Severity | Resolution |
|---|---|---|---|---|
| KPI help interaction | disclosure state could have leaked into the workspace | SRP | low | state remains local to the card |
| metric copy | generic card could become coupled to rework semantics | OCP/SRP | low | copy is passed by the feature component |
| overlay | card surface clipping could hide the explanation | UI boundary | medium | control/tooltip are siblings of the clipped article surface |
| accessibility | hover-only behavior would exclude keyboard/touch users | contract | high | focus, click, blur, Escape, ARIA name/state/control are covered |

No procedural service, global event manager, transport change, or feature-specific branch was introduced in the reusable card.
