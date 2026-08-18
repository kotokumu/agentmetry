# Claude Model Call Attribution

- Status: Approved for implementation
- Date: 2026-08-18
- Risk: High

## 1. Requirement summary

Claude Code emits complementary evidence for one model call. The `api_request`
log owns authoritative token and cost usage, while the `llm_request` trace span
owns the runtime agent instance identity. Agentmetry must join those records by
their canonical usage ID so a subagent's tokens appear on that runtime subagent
without counting the call twice.

## 2. Requirement specification

- `agent.name` describes an agent definition or type and never becomes a
  runtime agent ID.
- Claude `api_request` and `llm_request` records with the same source,
  conversation ID, and usage ID represent evidence for one model call.
- The authoritative `api_request` remains the only token and cost contribution
  for that call.
- The `llm_request` runtime agent ID and parent agent ID are applied to the
  matching request projection.
- Correlation works whether the log or trace export arrives first.
- Correlation is limited to usage IDs in the current batch or a revised span's
  previous projection key.
- A later correlation updates both persisted session-agent rollups and live
  activity projections.
- A call without matching runtime identity remains unattributed to a unique
  subagent; Agentmetry does not invent one from `agent.name`.
- Existing lossless journals are replayed into the corrected projection during
  the storage-generation migration.
- Claude aggregate token metrics and `subagent_completed.total_tokens` are not
  added to call totals.

## 3. Conceptual model

`ModelCallEvidence` is a provider-scoped record identified by source,
conversation ID, and usage ID. It has two independent authorities:

- `UsageEvidence`: token and cost measurements from the authoritative request
  log.
- `RuntimeAgentEvidence`: instance and parent identity from the request trace.

`AttributedModelCall` is the canonical projection produced by combining those
authorities. The raw journal remains the immutable source of truth.

## 4. Responsibility assignment

| Responsibility | Owner |
| --- | --- |
| Map Claude producer fields and usage identity | Claude source plugin |
| Derive canonical agent context without producer-specific name fallback | Canonical normalization |
| Correlate Claude call evidence after projection writes | SQLite projection layer |
| Rebuild affected agent/session rollups | SQLite rollup layer |
| Replay existing raw exports under the new behavior | Storage generation migration |

## 5. SOLID risk assessment

- Provider-specific correlation stays outside generic canonical derivation.
- Usage authority and identity authority remain separate, avoiding a single
  priority rule that selects the wrong field owner.
- The correlation service mutates disposable projections only; it does not
  alter raw attributes or journal records.
- The implementation is idempotent and order-independent for the same stored
  evidence, while duplicate authoritative log records remain distinct inputs.

## 6. Module boundary plan

- Update [canonical token derivation](https://github.com/kotokumu/agentmetry/blob/main/internal/canonical/tokens.go)
  to accept only explicit runtime agent ID attributes.
- Add Claude model-call attribution to the
  [SQLite projection transaction](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/store.go).
- Persist canonical usage ID on spans and logs with a source/conversation/usage
  index so reconciliation is bounded to affected calls.
- Rebuild only the affected historical agent buckets when correlation moves an
  activity written by an older projection sequence.
- Bump the coordinated storage generation so historical journals are replayed.
- Keep metric projection and the source plugin's usage-authority behavior
  unchanged.

## 7. Interface proposal

```go
func reconcileClaudeModelCallAgents(
    ctx context.Context,
    tx *sql.Tx,
    batch canonical.Batch,
    previous map[storedSpanKey]storedSpanScope,
    sequence int64,
) (claudeAttributionResult, error)
```

The function runs inside the existing projection transaction after spans and
logs are stored and before session rollups are updated. It selects matching
Claude request spans for the affected conversations, updates request-log agent
identity only when it differs, and publishes upserts for changed activities.
The result requests a full session rebuild only when a span revision overlaps a
historical attribution repair and lists historical trace projections that must
be rebuilt; ordinary late evidence uses targeted agent buckets.

## 8. Test specification

1. Canonical derivation does not treat `agent.name` as `AgentID`.
2. A Claude log followed by its trace attributes authoritative tokens to the
   trace runtime agent.
3. A Claude trace followed by its log produces the same result.
4. The request contributes tokens once and the corroborating trace contributes
   none.
5. A changed request-log activity is present in the projection sync feed.
6. An unmatched `agent.name` does not create a runtime subagent instance.
7. Existing storage generations require journal replay.

## 9. Detailed design

After each batch is projected, the SQLite layer derives affected Claude
conversation IDs from semantic spans and logs. For each affected conversation,
it finds corroborating spans with a non-empty canonical usage ID and explicit
runtime agent ID. It joins authoritative request logs on source, conversation
ID, and usage ID, then copies runtime-owned identity fields (`agent_id` and
`parent_agent_id`) into the log projection. If revised trace evidence no longer
provides runtime identity, the log projection returns to the explicit identity
in its own canonical attributes. Definition, type, model, usage, cost,
timestamps, and raw attributes stay owned by their original records.

Changed logs receive activity-feed upserts in the current projection sequence.
Canonical usage ID is stored in indexed span and log columns. Reconciliation
queries only usage IDs introduced by the batch or retained from a revised
span's previous projection. When an older log changes attribution, Agentmetry
rebuilds only its old and new agent buckets while excluding current-sequence
rows that the ordinary incremental aggregation will add. Agent-only changes do
not rewrite the log's projection sequence, so trace activity counts are not
incremented again.

The storage generation is incremented. On upgrade, Agentmetry rebuilds its
disposable projection database by replaying the lossless OTLP journal through
the current normalizer and correlation behavior.

## 10. TDD construction plan

1. Add canonical and storage behavior tests and confirm they fail.
2. Remove the `agent.name` runtime-ID fallback.
3. Implement transactional Claude call attribution and feed publication.
4. Repair affected historical agent and trace projections without rewriting the
   activity's projection sequence; use a full session rebuild for overlapping
   span revisions.
5. Increment the storage generation and update migration expectations.
6. Run focused tests, the full Go suite, format/lint checks, and a final design
   review.
