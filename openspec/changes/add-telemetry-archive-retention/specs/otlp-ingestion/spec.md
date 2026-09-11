## MODIFIED Requirements

### Requirement: Lossless raw export retention

Agentmetry SHALL serialize a decoded request to protobuf before provider
normalization and SHALL use those bytes as the replayable raw export. A
successfully committed raw export SHALL retain its signal, transport, receive
time, stored payload, codec, SHA-256, original protobuf size, detected source,
normalizer version, normalization status, and normalization error. Agentmetry
SHALL NOT store the compressed payload length as separate metadata.

Provider aliases and canonical fields SHALL NOT be written back into the raw
protobuf. While a Retained Export is Active or has an intact Archived copy under
the Retention State model [related]
[[concept:telemetry-retention/retention-state]], data omitted from canonical
projections SHALL remain recoverable from its raw export. Moving the export
between Active and Archived SHALL preserve its logical identity, immutable
receive time, exact pre-normalization protobuf, and replay metadata. Only an
authorized transition to Deleted under
[[telemetry-retention/automatic-archive-expiry]] may intentionally make the raw
export unrecoverable.

#### Scenario: SC-RAW-01 — Preserve pre-normalization data

- **GIVEN** a decoded export containing provider attributes and OTLP detail not represented canonically
- **WHEN** Agentmetry normalizes and commits the export
- **THEN** the raw payload contains the original decoded OTLP data and no provider-added aliases

#### Scenario: SC-RAW-02 — Replay a retained export

- **GIVEN** a committed raw export with a valid codec, hash, and size metadata
- **WHEN** Agentmetry replays it with a normalizer
- **THEN** Agentmetry can reconstruct the decoded request from the retained protobuf

#### Scenario: SC-RAW-03 — Preserve raw identity through archive and restore [compatibility]

- **GIVEN** an intact Archived export that was accepted before retention was enabled
- **WHEN** a user restores the export under [[telemetry-retention/archive-restoration]]
- **THEN** its original receive time, pre-normalization protobuf, and replay metadata equal the values recorded at admission

#### Scenario: SC-RAW-04 — Retain admitted raw data [happy]

- **GIVEN** an OTLP client submits a valid decoded export
- **WHEN** Agentmetry commits the export successfully
- **THEN** the complete pre-normalization protobuf and replay metadata are retained with immutable receive time
