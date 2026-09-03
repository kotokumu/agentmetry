## Purpose

Defines how users investigate received telemetry through precise evidence navigation, readable activity details, reusable filters, and trace timelines without adding external data collection.

## ADDED Requirements

### Requirement: Direct navigation to diagnostic evidence

Agentmetry SHALL open the activity identified by a diagnostic's trace ID and span ID, reveal its available content, and visibly identify the target even when it is outside the current activity page. It SHALL preserve the originating conversation, agent selection, filters, view, and position when returning. A missing target SHALL produce an unavailable-target state rather than silently selecting a different activity.

#### Scenario: Target outside the visible page

- **GIVEN** several failed spans in one trace and a diagnostic referring to a span outside the current page
- **WHEN** the user opens that diagnostic's evidence
- **THEN** the identified span is loaded, selected, and revealed
- **AND** returning restores the originating investigation state

#### Scenario: Evidence no longer available

- **GIVEN** a diagnostic link whose target cannot be found in retained projections
- **WHEN** the user opens it
- **THEN** the UI identifies the unavailable target and keeps the return action available

### Requirement: Access to every reported failure episode

Agentmetry SHALL make every episode returned by the diagnostic analysis accessible in the UI, including episodes after the first three. It SHALL show the analysis coverage and SHALL NOT present a partial episode result as a complete history of the original execution.

#### Scenario: More than three episodes

- **GIVEN** an analysis reporting five episodes
- **WHEN** the user requests additional episodes
- **THEN** the fourth and fifth episodes and their evidence actions are accessible without requiring an API client

### Requirement: Purpose-based conversation views and activity detail

Agentmetry SHALL provide views for reading execution activity, examining rework, and comparing diagnostics. Selecting an activity SHALL expose its available body and metadata next to the activity list at sufficient width, or in a switchable detail view at narrow width. Switching views SHALL preserve the selected conversation and activity. Live updates SHALL NOT replace the selection or force the user away from the body being read.

#### Scenario: Read a received prompt and tool result

- **GIVEN** a conversation containing a received prompt and tool result
- **WHEN** the user switches from diagnostics to execution and selects each activity
- **THEN** each available body is readable without scrolling through all diagnostic cards

#### Scenario: Narrow layout and live arrival

- **GIVEN** the user is reading a selected activity in a narrow layout
- **WHEN** new activities arrive and the user returns to the list
- **THEN** the original selection remains identifiable and the list position is preserved

### Requirement: Structured conversation filters

Agentmetry SHALL combine the existing time, source, and text filters with observed failure, elapsed conversation duration, model, and tool conditions. Conditions SHALL apply to the full stored projection result set before pagination, and each conversation SHALL appear once. Model and tool conditions SHALL mean that matching records exist within the same conversation; they SHALL NOT imply that both values occur in one activity. Missing values SHALL NOT satisfy positive numeric or observed-failure conditions.

Invalid filter input SHALL be identified without applying an invalid query or silently dropping the condition. The last valid results SHALL remain identifiable as belonging to the last applied query.

#### Scenario: Filter matches outside the loaded page

- **GIVEN** a matching failed conversation outside the currently loaded page and a conversation with unknown outcomes
- **WHEN** the user filters for observed failures and a minimum elapsed duration
- **THEN** the matching conversation is included and unknown outcomes are not treated as failures

#### Scenario: Model and tool in different activities

- **GIVEN** one conversation with a selected model in one activity and a selected tool in another
- **WHEN** both conversation filters are applied
- **THEN** the conversation matches and the UI identifies the filters as conversation-level conditions

### Requirement: Local saved investigation filters

Agentmetry SHALL allow users to name, apply, replace, and delete filter sets within the local Web profile. Saved conditions SHALL include time range, source, text, and supported structured filters. Applying saved relative time ranges SHALL evaluate them at the current time and SHALL NOT claim to restore a frozen result set. Active filters SHALL be represented in navigation URLs and restored by browser history. Unsupported saved conditions SHALL be reported rather than silently discarded.

#### Scenario: Restore saved conditions

- **GIVEN** a named filter set using a relative 24-hour range
- **WHEN** the user reloads the UI and applies it later
- **THEN** the same conditions are applied using the current 24-hour window
- **AND** the active conditions remain visible and shareable through the URL

### Requirement: Trace overview and focused time ranges

Agentmetry SHALL offer a trace overview, time-range zoom, and activity-type/error filters while retaining access to the selected activity. The overview SHALL distinguish full retained-projection coverage from partially loaded coverage. Changing the visible range SHALL NOT redefine the trace's overall extent. Missing parents SHALL remain visible as missing, and temporal overlap SHALL NOT be presented as proven dependency or causality.

#### Scenario: Trace exceeds one activity page

- **GIVEN** a trace with 1200 projected activities and only one detail page loaded
- **WHEN** the user views the overview and zooms to a long-running interval
- **THEN** the overview states its coverage, loads the relevant detail range, and retains the overall time reference

### Requirement: Accessible investigation controls

Selection, expansion, view switching, filter application, and returning from detail SHALL be operable by keyboard with visible focus. Failure, selection, and missing-content states SHALL have textual or structural identifiers in addition to color. At 200% browser zoom the detail body and return controls SHALL remain reachable.

#### Scenario: Keyboard investigation at enlarged text size

- **GIVEN** browser zoom of 200% and keyboard-only navigation
- **WHEN** the user opens evidence, reads its body, and returns
- **THEN** focus follows the selected target and returns to an identifiable originating control without relying on color
