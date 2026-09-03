# Investigation requirements and frozen evidence

## Problem, Goal, and Context

Users investigate AI coding-agent executions from OTel data already received by agentmetry. The change makes the evidence of an observed failure and its received input/content easier to locate, and makes diagnostic comparisons consistent across Web and MCP.

## Current Behavior

Conversation lists, selected-agent filtering, URL filters, navigation return state, activity bodies, rework analysis, and a paginated waterfall exist. Episode links currently specify only trace identity and the UI shows three episodes. Normalized comparison lives in Web; MCP aggregate comparison accepts one to ten source-qualified runs.

## Desired Behavior

Users can reach the exact reported evidence, read available input/context/tool content, return without losing position, repeat filtered investigations, and inspect long traces without confusing partial data with complete executions.

## Constraints and Approved Assumptions

- The user approved the proposal, design, specs, and task list by requesting implementation in order after their presentation (2026-09-04: 「では順番に」). Routine implementation decisions remain within that accepted scope.
- Only received OTLP information is used. AGENTS.md, Skills, repository files, Git state, transcripts, and external services are not read to fill missing telemetry.
- Prompt/context content is in scope only where its received fields support the interpretation. Reading a file does not establish subsequent inclusion in a model request.
- Existing ingestion/raw retention and existing aggregate comparison behavior remain compatible.
- The accepted plan treats unverified extra canonical mappings as a documented follow-up decision, not an assumption that every provider emits every context field.

## Requirement Summary

Functional identifiers below point to the accepted requirement headings in the two delta specs. They do not prescribe additional architecture.

## Functional Requirements

| ID | Observable requirement | Accepted spec requirement |
| --- | --- | --- |
| R1 | Open exact evidence and restore origin; missing targets are explicit | Direct navigation to diagnostic evidence |
| R2 | Access all reported episodes, retaining analysis coverage | Access to every reported failure episode |
| R3 | Switch investigation purpose and read one activity while preserving selection during updates | Purpose-based conversation views and activity detail |
| R4 | Filter the full conversation set before pagination using failure/duration/model/tool and existing conditions | Structured conversation filters |
| R5 | Save/apply/replace/delete local named filter conditions and restore URL/history | Local saved investigation filters |
| R6 | Inspect full/partial trace overview, zoom, type/error filters, and retain selection | Trace overview and focused time ranges |
| R7 | Compare the same pair with five equal diagnostics and denominators across Web/MCP | Shared diagnostic comparison meaning |
| R8 | Validate normalized comparison eligibility without restricting old aggregate comparison | Explicit comparison eligibility and missing values |
| R9 | Distinguish reference, received read/tool result, and explicitly reported model input | Content provenance and evidence strength |
| R10 | Preserve provider-specific interpretation, explicit redaction, and unknown causes | Provider-specific content interpretation |
| R11 | Distinguish retained raw, projected content, analyzed coverage, and filtered visibility | Retained, projected, and visible coverage |
| R12 | Preserve read-only MCP and explicit content opt-in | Preserve content access defaults |

## Non-Functional Requirements

- R13: keyboard operation, visible focus, non-color identifiers, and usable detail/return controls at 200% browser zoom (Accessible investigation controls).
- Local application operation, Go/SQLite storage, and Web/HTTP/MCP consumers remain supported.
- Long traces must not require loading every body to show their extent; no unsupported runtime latency SLA is introduced.
- Behavior tests and existing relevant tests are required; paid provider-live execution is not required.

## Inputs and Outputs

Inputs: source-qualified conversation references, trace/span references, received activities and their attributes/bodies, filter conditions, local saved conditions, selected time ranges, and incoming projection updates.

Outputs: scoped lists and selected activity detail, normalized pairwise diagnostics, unavailable reasons, explicit evidence provenance and coverage, stable navigation/selection, and trace overview/range results.

## Normal Cases

Open a reported failed span beyond the current page; inspect its input/tool content; return; apply saved filters; compare an eligible same-source non-overlapping pair; zoom a long trace.

## Error Cases

Missing evidence target; invalid filter; invalid baseline identity/time; unsupported saved condition; local storage failure; unsupported server; absent/redacted content. Errors do not silently select a different target, drop a filter, fabricate content, or mark a failed save successful.

## Edge Cases

Multiple failures in one trace, more than three episodes, 1200 activities, zero denominator, missing versus zero token usage, partial projection reads, live arrivals during comparison/detail reading, missing parent, same source-independent run ID, relative saved time advancing, and model/tool conditions matching different activities in one conversation.

## Acceptance Criteria

Each R1–R13 must pass the Given/When/Then scenarios in its named delta requirement. Fixed fixture outputs must agree for R7/R8; current aggregate behavior must continue to pass. R9–R11 must not require non-OTLP collection. R13 is verified by browser operation as well as relevant component tests. Task checkboxes are updated only after their stated completion conditions are verified.

## Non-Goals

Outcome scoring, annotation/comment workflows, configuration or prompt version management, extra collectors, external evaluation, ingestion schema expansion without a separate supported mapping proposal, and inferred task success/causality.

## Risks and Open Questions

Provider context coverage differs and must be measured against existing docs and fixtures. Missing coverage is an accepted result. UI/state regressions, query cost, old-client compatibility, and concurrent-read consistency require tests. There are no unresolved product-scope decisions; concrete provider extensions remain outside this construction scope.

## Current-System Evidence

Evidence packet ID: OTEL-INVESTIGATION-1. Baseline commit: ed6e5038d82d67298c8a569ad67f955caadd5cc9. Accepted requirements: the two delta specs in this change. This packet excludes the proposed model, interfaces, package plan, and implementation choices.

| Fact | Source | Relevance |
| --- | --- | --- |
| Accepted user scope is OTel-only with received prompt/context content | User messages in this task on 2026-09-04 | Prevents extra data collectors and evaluation scope |
| Original protobuf is retained before normalization; span events are raw-only | [receiver](https://github.com/kotokumu/agentmetry/blob/main/internal/ingest/otel/receiver.go), [normalizer](https://github.com/kotokumu/agentmetry/blob/main/internal/ingest/otel/normalize.go), existing otlp-ingestion spec | Content availability is not equal to raw retention |
| Codex logs and trace-safe events have different content coverage | [Codex snapshot](https://github.com/kotokumu/agentmetry/blob/main/docs/source-telemetry/codex.md) | Provider-specific evidence interpretation |
| Claude prompt/response and tool content have distinct controls and locations | [Claude snapshot](https://github.com/kotokumu/agentmetry/blob/main/docs/source-telemetry/claude-code.md) | Provider-specific absence and redaction |
| Conversation references include source and ID | [query API](https://github.com/kotokumu/agentmetry/blob/main/internal/query/api.go) | Identity must survive navigation and comparison |
| Trace reads include full counts/extent and paginated activities | [trace query](https://github.com/kotokumu/agentmetry/blob/main/internal/query/trace.go) | Extent and loaded detail differ |
| Existing navigation persists selected agent and scroll; conversation links can anchor spans | [navigation](https://github.com/kotokumu/agentmetry/blob/main/web/src/app/navigation.ts) | Preserve existing return behavior |
| Episodes contain trace/span references but UI links only trace and limits to three | [rework UI](https://github.com/kotokumu/agentmetry/blob/main/web/src/components/rework-summary.ts), [types](https://github.com/kotokumu/agentmetry/blob/main/web/src/model/telemetry.ts) | Exact evidence selection and complete result access |
| Five normalized diagnostics and eligibility exist in Web | [comparison](https://github.com/kotokumu/agentmetry/blob/main/web/src/model/rework-comparison.ts) | Preserve metric meaning and denominator rules |
| MCP aggregate comparison supports one to ten explicit runs; bodies are opt-in in read tools | [MCP](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/mcpserver/server.go) | Backward compatibility and content defaults |
| Projected data changes are streamed to Web | [protobuf API](https://github.com/kotokumu/agentmetry/blob/main/proto/agentmetry/v1/agentmetry.proto) | Live navigation and consistent reads |

## Risk Assessment

High risk: additive public read API and multi-module state/query changes. The accepted design is authorized by the user's request to proceed; detailed implementation must remain within its boundaries. Model/scenario, architecture/interface, test, and construction gates are recorded separately before runtime edits. No raw-data or ingestion schema migration is planned.
