# Specification Analysis: project-supported-otel-span-events

## 1. Boundary

| Item | Decision | Evidence |
|---|---|---|
| Product capability | New `otel-span-event-projection` capability | Proposal and delta spec |
| Change classification | Observable behavior change | Supported span events become queryable activities |
| Included behavior | Provider allowlisting, projected identity and provenance, aggregate safety, content rules, and replay coverage | Delta spec |
| Excluded behavior | Generic event projection, inferred content, authoritative usage without provider evidence, and raw-journal mutation | Proposal Out of Scope and design |

---

## 2. Consumers and Observable Events

| Consumer / actor | Trigger / prior state | Interaction or event | Observable result |
|---|---|---|---|
| Investigator | A supported provider span event is retained | Queries conversation activity | Sees one stable projected event with provenance |
| Aggregation or diagnostic consumer | Parent, log, and event evidence may describe one operation | Reads totals or outcomes | Receives no duplicate contribution without an authoritative correlation rule |
| Investigator | An event lacks an allowlisted content field | Requests activity content | Receives no synthesized body |
| Operator | Retained exports predate the projection version | Reads projection coverage or runs a future replay | Can distinguish new-only coverage from completed replay coverage |

---

## 3. Concept Analysis

| Concept or change | Specification decision | Evidence | Owner capability and rationale |
|---|---|---|---|
| Supported Span Event | A provider event whose name and retained fields match a fixture-backed allowlist | Provider evidence and delta requirement | `otel-span-event-projection`; it owns admission into canonical projection |
| Projected Event | A non-contributing canonical activity derived from one supported span event | Delta requirement and design decision 2 | `otel-span-event-projection`; it owns event visibility and provenance |
| Event Identity | Stable identity from retained export, parent trace/span, event ordinal, and derivation version | Delta requirement and design decision 2 | `otel-span-event-projection`; it owns replay idempotency |
| Correlation Evidence | Received identities that may establish that observations describe one operation | Delta requirement and design decision 3 | `otel-span-event-projection`; it owns duplicate prevention for event evidence |
| Projection Coverage | Declared extent and version of event projection over retained exports | Delta requirement and design decision 4 | `otel-span-event-projection`; it owns whether older exports were processed |

### 3-1. Supporting Models

| Event state | Canonical activity | Raw retention | Aggregate contribution |
|---|---|---|---|
| Unsupported | None | Preserved | None |
| Supported, uncorrelated | One Projected Event | Preserved | None |
| Supported, correlated | One Projected Event | Preserved | Only when an explicit provider authority rule designates the unique fact |

---

## 4. Main Spec Conceptual Model Replacements

### `otel-span-event-projection`

```markdown
A **Supported Span Event** is an OTLP span event whose provider, event name, and retained fields match a versioned allowlist backed by pinned provider evidence. Events outside that contract remain available only through the lossless raw export.

A **Projected Event** is the canonical activity derived from one Supported Span Event. It retains provider source, source event name, event timestamp, parent trace and span identities, event ordinal, and projection version. Its **Event Identity** is derived deterministically from the retained export identity, parent identity, event ordinal, and identity version, so replay produces the same canonical identity.

Projected Events relate to their parent spans without becoming native spans. A native trace/span identity continues to select the stored parent span. Event ordinals distinguish otherwise identical events on the same parent.

**Correlation Evidence** consists only of received identities such as provider source, conversation identity, parent trace/span, event identity, and an explicitly received operation, call, or request identity. Time proximity does not establish equality. A Projected Event does not contribute tokens or outcomes unless a provider rule identifies it as the unique authoritative fact and the required Correlation Evidence is present.

**Projection Coverage** identifies the projection version and the retained-export range to which it has been applied. New-ingestion-only coverage and completed retained-export replay are distinct states; raw retention alone does not imply canonical event projection.

### References

- `[provider evidence]` [Claude Code telemetry](https://github.com/kotokumu/agentmetry/blob/main/docs/source-telemetry/claude-code.md)
- `[provider evidence]` [Codex telemetry](https://github.com/kotokumu/agentmetry/blob/main/docs/source-telemetry/codex.md)
- `[raw-retention contract]` [OTLP ingestion specification](https://github.com/kotokumu/agentmetry/blob/main/openspec/specs/otlp-ingestion/spec.md)
```

---

## 5. Requirement Candidates

| Requirement slug | Actor and event | Guarantee | Concepts used | Normative representations | Scenario tags |
|---|---|---|---|---|---|
| `only-evidenced-provider-events-are-projected` | Ingestion receives a span event | Only allowlisted provider events create activities | Supported Span Event, Projected Event | Partition | happy, unsupported, provider-boundary |
| `projected-event-identity-and-provenance-are-stable` | Normalization or replay processes a supported event | Identity and provenance remain stable | Projected Event, Event Identity | Invariant | happy, idempotency, boundary |
| `event-evidence-does-not-duplicate-totals-or-outcomes` | A consumer aggregates related evidence | Events do not duplicate totals or outcomes | Correlation Evidence | Decision, Invariant | duplicate, missing-evidence |
| `content-requires-an-exact-supported-field-contract` | A consumer reads event content | Only an exact allowlisted field supplies content | Supported Span Event | Partition | happy, absent-content, unknown-field |
| `replay-of-retained-exports-is-explicit-and-idempotent` | An operator includes older exports | Replay is explicit, resumable, versioned, and observable | Event Identity, Projection Coverage | State Transition, Invariant | resume, idempotency, new-only |

---

## 6. Unresolved Decisions

None.

---

## 7. Sources

- [Change proposal](https://github.com/kotokumu/agentmetry/blob/main/openspec/changes/project-supported-otel-span-events/proposal.md)
- [Delta specification](https://github.com/kotokumu/agentmetry/blob/main/openspec/changes/project-supported-otel-span-events/specs/otel-span-event-projection/spec.md)
- [Change design](https://github.com/kotokumu/agentmetry/blob/main/openspec/changes/project-supported-otel-span-events/design.md)
