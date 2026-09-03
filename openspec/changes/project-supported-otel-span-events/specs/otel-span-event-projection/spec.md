## Purpose

Lets investigators use supported OTLP span-event metadata while preserving raw evidence, provenance, and aggregate correctness across provider signals.

## ADDED Requirements

### Requirement: Only evidenced provider events are projected

The system SHALL project a span event only when its provider, event name, and retained fields match an allowlisted mapping backed by a pinned valid-OTLP fixture. An unsupported event MUST remain in the raw export and MUST NOT create an activity.

#### Scenario: Unsupported event remains raw

- **GIVEN** a valid OTLP span contains an event outside the provider allowlist
- **WHEN** the export is normalized and stored
- **THEN** the event remains byte-equivalent after journal restore and no event activity is created

#### Scenario: Provider mappings do not cross

- **GIVEN** Claude Code and Codex use different supported event names or fields
- **WHEN** an event matches only one provider mapping
- **THEN** the system applies only that provider's mapping

### Requirement: Projected event identity and provenance are stable

Each projected event SHALL carry its provider source, parent trace ID, parent span ID, event name, event timestamp, and a stable activity identity derived from the retained export record, parent identity, and event ordinal. Reprocessing the same export MUST yield the same identity.

#### Scenario: Identical export is reprocessed

- **GIVEN** a supported event in one retained export
- **WHEN** normalization or replay processes the export more than once
- **THEN** exactly one event activity with the same identity is visible

#### Scenario: Similar events have different ordinals

- **GIVEN** two events on one span have the same name and timestamp
- **WHEN** both events are supported
- **THEN** their event ordinals keep their identities distinct

### Requirement: Event evidence does not duplicate totals or outcomes

A projected event SHALL be non-contributing unless its provider authority and correlation rules explicitly designate it as the unique authoritative fact. Correlation MUST consider provider source, conversation identity, parent trace/span, event identity, call or request identity when received, and time. Missing correlation facts MUST NOT be replaced with guessed equality.

#### Scenario: Parent and event repeat usage

- **GIVEN** a parent span and its event both report usage
- **WHEN** the event is projected without a proven authoritative usage rule
- **THEN** aggregate token totals include neither an additional event contribution nor a second usage outcome

#### Scenario: Log and trace event represent one operation

- **GIVEN** a log and a supported span event carry a documented shared operation identity
- **WHEN** rework outcomes are classified
- **THEN** the operation contributes no more than one outcome

### Requirement: Content requires an exact supported field contract

Event content SHALL be projected only from an exact provider field documented by pinned source evidence and fixture. Event name, file reference, length, or operation type alone MUST NOT imply a body or model inclusion. References, read output, and explicit model input MUST remain distinct.

#### Scenario: Trace-safe Codex event has lengths but no body

- **GIVEN** a supported Codex trace-safe event reports output length without output text
- **WHEN** it is projected
- **THEN** its content availability is not reported and no body is synthesized

#### Scenario: Unknown Claude output attribute is received

- **GIVEN** a Claude tool event has an attribute that is not named by the pinned provider contract
- **WHEN** it is retained
- **THEN** the attribute remains raw and is not exposed as canonical content

### Requirement: Replay of retained exports is explicit and idempotent

The system SHALL leave existing raw exports unchanged. If retained exports are included, replay MUST be versioned, resumable, idempotent, and use the same provider mapping and identity/deduplication rules as new ingestion. The product MUST report whether projection coverage includes retained exports.

#### Scenario: Replay is interrupted and resumed

- **GIVEN** a replay range containing supported and unsupported events
- **WHEN** replay stops after a committed batch and resumes
- **THEN** committed event activities are not duplicated, unsupported events remain raw-only, and remaining exports continue from the saved position

#### Scenario: Existing data is not replayed

- **GIVEN** the deployment does not enable retained-export replay
- **WHEN** a user investigates older data
- **THEN** coverage reports that event projection applies only to newly processed exports
