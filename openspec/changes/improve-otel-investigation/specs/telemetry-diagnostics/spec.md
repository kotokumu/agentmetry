## Purpose

Defines consistent diagnostic comparisons and evidence interpretation across Web and MCP so that received measurements, content availability, and limits of the analysis retain the same meaning.

## ADDED Requirements

### Requirement: Shared diagnostic comparison meaning

For the same source-qualified conversation pair and retained data snapshot, Web and MCP SHALL return the same five normalized diagnostics: initial validation success proxy, rework token share, retry-cycle effort share, tool failure rate, and recurring failure loops per 100 validations. They SHALL agree on metric identity, unit, numerator, denominator, value, delta, and availability reason. Presentation rounding SHALL NOT change the underlying comparison result.

Existing MCP aggregate comparison fields SHALL retain their meanings and remain available. This extension SHALL NOT require new telemetry sources or configuration-version management.

#### Scenario: Compare the same pair through both clients

- **GIVEN** two eligible same-source conversations in a fixed data snapshot
- **WHEN** the pair is compared through Web and MCP
- **THEN** the five diagnostics, their denominators, units, and differences agree
- **AND** existing aggregate MCP comparison fields retain their meanings

### Requirement: Explicit comparison eligibility and missing values

The new normalized diagnostic comparison SHALL identify both source-qualified conversations explicitly. A same-conversation, cross-source, temporally overlapping, or invalid-time baseline SHALL be rejected with a reason. This eligibility rule SHALL NOT restrict existing aggregate comparison requests for one to ten explicitly selected runs. Missing numerators and zero or unavailable denominators SHALL produce an unavailable metric rather than a fabricated zero. Partial analysis SHALL remain distinguishable from complete retained-projection analysis. Neither an observed difference nor a passed tool invocation SHALL be labeled as proof of task achievement, productivity, or causal improvement.

#### Scenario: Insufficient denominator

- **GIVEN** an otherwise eligible pair with no outcome-known validation attempts in one conversation
- **WHEN** diagnostics are compared
- **THEN** the affected metric is unavailable with its denominator and reason shown consistently in Web and MCP

#### Scenario: Invalid baseline

- **GIVEN** a baseline from another source or one overlapping the current conversation
- **WHEN** comparison is requested
- **THEN** the request returns an ineligible-baseline result and does not silently substitute another run

### Requirement: Content provenance and evidence strength

Agentmetry SHALL expose available received content with its source, associated activity identity, and content kind when known. It SHALL distinguish a reference to a file, content returned by a file/tool read, and content explicitly reported as model input. A reference or read result alone SHALL NOT establish that the complete content was included in a later model request.

Content interpretation SHALL use verified provider mappings. Unknown semantic kinds SHALL remain unknown rather than being inferred from a filename alone. AGENTS.md, Skill, and other context bodies SHALL be displayed only to the extent present in the received data and supported by existing projections.

#### Scenario: AGENTS.md reference without content

- **GIVEN** received telemetry identifies a read of AGENTS.md but does not contain its body
- **WHEN** the associated context is shown
- **THEN** the reference is visible, its body is unavailable, and model-input inclusion is unconfirmed
- **AND** Agentmetry does not read the local file to fill the gap

#### Scenario: Tool output without model-input linkage

- **GIVEN** telemetry contains a tool output body without evidence linking it to a model request
- **WHEN** that body is displayed
- **THEN** it is identified as received tool output and not as confirmed model input

### Requirement: Provider-specific content interpretation

Agentmetry SHALL preserve differences between the supported Claude Code and Codex content mappings. Explicit producer redaction or encryption indicators SHALL not be rendered as readable prompt content or bypassed through external retrieval. A missing field without an explicit indicator SHALL be described as unreported rather than assigned a speculative cause.

#### Scenario: Codex redacted prompt

- **GIVEN** a supported Codex prompt record containing the producer's redaction marker
- **WHEN** the user reads the prompt activity
- **THEN** its body is identified as producer-redacted, while the received record remains retained

#### Scenario: Claude Code content absence

- **GIVEN** a supported Claude Code activity without response content and without an explicit redaction indicator
- **WHEN** its body is requested
- **THEN** the content is unreported and the UI does not claim that a specific producer setting caused its absence

### Requirement: Retained, projected, and visible coverage

Agentmetry SHALL distinguish content availability, retained-projection analysis coverage, and display filtering. Reading all retained projected records SHALL NOT imply that all original execution records were collected. An activity hidden by current filters SHALL not be described as missing. Information retained only in raw OTLP SHALL not be presented as canonically projected content.

Unsupported attributes and span events SHALL remain raw-retained under the existing ingestion contract and SHALL NOT cause rejection solely because the new investigation UI cannot interpret them. This change SHALL NOT introduce canonical mappings for unsupported events without a separately specified ingestion change.

#### Scenario: Complete projection with unavailable content

- **GIVEN** every retained projected activity has been analyzed but some bodies were not reported
- **WHEN** coverage is displayed
- **THEN** analysis completeness and body availability are shown separately

#### Scenario: Context exists only in an unsupported span event

- **GIVEN** a successfully committed OTLP export contains a context event unsupported by canonical projection
- **WHEN** the conversation is investigated
- **THEN** the original event remains raw-retained and is not fabricated as an available canonical body

### Requirement: Preserve content access defaults

MCP SHALL remain read-only and SHALL continue to omit activity bodies unless content is explicitly requested. New comparison responses SHALL not implicitly include prompt or context bodies. Web content retrieval SHALL follow the existing activity-read path and its limits.

#### Scenario: Agent requests comparison without content

- **GIVEN** a conversation containing prompt and context bodies
- **WHEN** an MCP client requests diagnostic comparison without requesting content
- **THEN** it receives diagnostics and coverage without those bodies
