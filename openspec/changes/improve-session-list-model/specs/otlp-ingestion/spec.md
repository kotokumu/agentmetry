## MODIFIED Requirements

### Requirement: codex-event-normalization

Agentmetry MUST normalize Codex event names and, among aliases synthesized from `codex.agent_communication`, create Session Link Evidence only from a sent spawn compatibility event.

- **Input and Acceptance**: A non-empty string `event.name` takes precedence over the native event name. A missing, empty, or non-string `event.name` leaves the native name as the normalization input.
- **Behavior Rules**:

  | Normalization input | Canonical event |
  |---|---|
  | `codex.sse_event` with `event.kind=response.completed` | `gen_ai.response.completed` |
  | `codex.agent_communication` with `kind=spawn` | `gen_ai.agent.delegation` |
  | `codex.agent_communication` with `kind=followup` | `gen_ai.agent.delegation` |
  | Other `codex.agent_communication` | `gen_ai.agent.message` |
  | Other name beginning `codex.` | `gen_ai.<suffix>` |
  | Name without `codex.` | Original name |

  For `codex.agent_communication` with `kind=spawn` and `state=send`, `sender_thread_id` becomes the parent session alias and `receiver_thread_id` becomes the child session alias. That pair creates Session Link Evidence. Other kinds or states, including follow-up, receive, and fork semantics, create no Session Link Evidence from this compatibility event. Direct canonical parent/child aliases and non-Codex sources retain their existing link behavior. When `model=codex-auto-review`, Agentmetry overwrites the canonical agent type with `system`.
- **Invariants**:
  - `codex.agent_communication` MUST remain an Agentmetry compatibility input implemented by the local Codex profile, not an upstream Codex telemetry contract.
  - Duplicate identical Session Link Evidence MUST remain idempotent.
- **References**: `[related] [[concept:session-catalog/session-link-evidence]]`

#### Scenario: SC-CX-EVT-01 — Normalize a completed Codex response

- **GIVEN** an OTLP producer and a Codex `codex.sse_event` whose `event.kind` is `response.completed`
- **WHEN** Agentmetry normalizes the event
- **THEN** the canonical event name is `gen_ai.response.completed`

#### Scenario: SC-AM-CX-EXT-01 — Create Codex spawn session aliases

- **GIVEN** an OTLP producer and a sent Codex spawn communication with sender and receiver thread IDs
- **WHEN** Agentmetry normalizes the event
- **THEN** it creates parent and child aliases, records Session Link Evidence, and names the event `gen_ai.agent.delegation`

#### Scenario: Non-spawn communication creates no membership [boundary]

- **GIVEN** an OTLP producer and a Codex follow-up, fork, receive state, or other non-sent-spawn communication
- **WHEN** Agentmetry normalizes the event
- **THEN** it creates no Session Link Evidence from that `codex.agent_communication` event

#### Scenario: SC-CX-EVT-02 — Normalize a generic Codex event

- **GIVEN** an OTLP producer and a Codex event named `codex.tool_result`
- **WHEN** Agentmetry normalizes the event
- **THEN** the canonical event name is `gen_ai.tool_result`

#### Scenario: Duplicate spawn evidence is idempotent [idempotency]

- **GIVEN** an OTLP producer and a sent spawn whose identical relationship was already accepted
- **WHEN** Agentmetry accepts the duplicate event
- **THEN** the membership result remains one parent-child relationship

#### Scenario: Non-prefixed Codex event remains unchanged [happy]

- **GIVEN** an OTLP producer and a Codex event name without the `codex.` prefix
- **WHEN** Agentmetry normalizes the event
- **THEN** the canonical event name equals the original name

## RENAMED Requirements

- FROM: `### Requirement: Codex event normalization`
- TO: `### Requirement: codex-event-normalization`
