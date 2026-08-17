# Provider Traceability

## Purpose

Maps every numbered Claude Code and Codex provider-snapshot heading to the Agentmetry ingestion behavior that recognizes, observes, projects, or retains it.

---

## Provider specification coverage

The coverage matrices connect every numbered heading of the
[Claude Code data specification](https://github.com/kotokumu/agentmetry/blob/main/docs/source-telemetry/claude-code.md)
and [Codex data specification](https://github.com/kotokumu/agentmetry/blob/main/docs/source-telemetry/codex.md)
to Agentmetry behavior. Each heading appears in exactly one row. Snapshot
metadata, source references, grouping headings, and completeness statements
use `Context`; they do not create runtime behavior. Runtime rows use only
`Admission`, `Mapped`, `Projected`, `Attrs`, `Observed`, and `RawOnly`,
joined with `+` when more than one outcome applies.

Agentmetry compatibility rules whose source section is explicitly marked as
outside the upstream snapshot are excluded from provider coverage while
remaining present in the ingestion mapping.

#### Claude Code coverage matrix

| Source specification section | Agentmetry rule or requirement | Disposition | Verification ID |
| --- | --- | --- | --- |
| 1. Snapshot metadata | — | Context | `SC-COVERAGE-01` |
| 2. Source references | — | Context | `SC-COVERAGE-01` |
| 3. OTLP export contract | OTLP transport admission; Lossless raw export retention | Admission+RawOnly | `SC-TRANSPORT-01`, `SC-TRANSPORT-02`, `SC-TRANSPORT-03`, `SC-TRANSPORT-04`, `SC-RAW-01`, `SC-RAW-02` |
| 4. Resource and scope schema | — | Context | `SC-COVERAGE-01` |
| 4-1. Resource attributes | `CL-RES-01`, `CL-RES-02` | Mapped+Attrs+RawOnly | `SC-SOURCE-01`, `SC-SOURCE-03` |
| 4-2. Instrumentation scope | `CL-SCOPE-01` | RawOnly | `SC-RAW-01` |
| 4-3. Shared record and data-point attributes | `CL-ATTR-01`, `CL-ATTR-02` | Mapped+Attrs+RawOnly | `SC-CL-ATTR-01`, `SC-ALIAS-01` |
| 5. Log record schema | — | Context | `SC-COVERAGE-01` |
| 5-1. Common log envelope | `CL-LOG-01` | Projected+Attrs+Observed+RawOnly | `SC-LOG-01` |
| 5-2. Interaction and model events | `CL-EVT-01`, `CL-EVT-02`, `CL-CONT-01`, `CL-USAGE-01`, `CL-COST-01` | Mapped+Projected+Attrs+Observed+RawOnly | `SC-CL-EVT-01`, `SC-CL-EVT-03`, `SC-CL-EVT-04`, `SC-CL-EVT-05`, `SC-CL-EVT-02`, `SC-CL-CONT-01`, `SC-CL-USAGE-01`, `SC-CL-COST-01` |
| 5-3. Tool, permission, and MCP events | `CL-EVT-02`, `CL-CONT-01` | Mapped+Projected+Attrs+Observed+RawOnly | `SC-CL-EVT-02`, `SC-CL-CONT-01` |
| 5-4. Authentication, extensions, hooks, and lifecycle events | `CL-EVT-02` | Projected+Attrs+Observed+RawOnly | `SC-CL-EVT-02` |
| 6. Metric schema | `CL-MET-01`, `CL-MET-02` | Projected+Attrs+Observed+RawOnly | `SC-METRIC-01`, `SC-CL-MET-01` |
| 7. Trace schema | `CL-TRACE-01`, `CL-TRACE-02`, `CL-TRACE-03`, `CL-USAGE-02` | Mapped+Projected+Attrs+Observed+RawOnly | `SC-TRACE-01`, `SC-TRACE-02`, `SC-TRACE-EVENT-01`, `SC-CL-USAGE-02` |
| 7-2. Span attribute inventory | `CL-TRACE-01`, `CL-TRACE-02`, `CL-TRACE-03`, `CL-USAGE-02` | Mapped+Projected+Attrs+Observed+RawOnly | `SC-TRACE-01`, `SC-TRACE-02`, `SC-TRACE-EVENT-01`, `SC-CL-USAGE-02` |
| 8. Content controls | `CL-CTRL-01`, `CL-CONT-01` | Mapped+Attrs+RawOnly | `SC-CL-CTRL-01`, `SC-CL-CONT-01` |
| 9. Completeness and stability | — | Context | `SC-COVERAGE-01` |

#### Codex coverage matrix

| Source specification section | Agentmetry rule or requirement | Disposition | Verification ID |
| --- | --- | --- | --- |
| 1. Snapshot metadata | — | Context | `SC-COVERAGE-01` |
| 2. Source references | — | Context | `SC-COVERAGE-01` |
| 3. OTLP export contract | OTLP transport admission; Lossless raw export retention | Admission+RawOnly | `SC-TRANSPORT-01`, `SC-TRANSPORT-02`, `SC-TRANSPORT-03`, `SC-TRANSPORT-04`, `SC-RAW-01`, `SC-RAW-02` |
| 4. Resource and scope schema | — | Context | `SC-COVERAGE-01` |
| 4-1. Resource attributes | `CX-RES-01`, `CX-RES-02` | Mapped+Attrs+RawOnly | `SC-SOURCE-02`, `SC-SOURCE-03` |
| 4-2. Instrumentation scope | `CX-SCOPE-01` | RawOnly | `SC-RAW-01` |
| 4-3. Shared event attributes | `CX-ATTR-01`, `CX-ATTR-02` | Mapped+Attrs+RawOnly | `SC-CX-ATTR-01`, `SC-CX-USAGE-02` |
| 5. Log record schema | — | Context | `SC-COVERAGE-01` |
| 5-1. Common log envelope | `CX-LOG-01` | Projected+Attrs+Observed+RawOnly | `SC-LOG-01` |
| 5-2. Implemented central event records | `CX-EVT-01`, `CX-EVT-03`, `CX-TOOL-01`, `CX-CONT-01`, `CX-USAGE-01` | Mapped+Projected+Attrs+Observed+RawOnly | `SC-CX-EVT-01`, `SC-CX-EVT-02`, `SC-CX-TOOL-02`, `SC-CX-CONT-01`, `SC-CX-USAGE-01` |
| 6. Metric schema | `CX-MET-01`, `CX-MET-02`, `CX-MET-03` | Projected+Attrs+Observed+RawOnly | `SC-METRIC-01`, `SC-CX-MET-01`, `SC-OBS-02` |
| 6-1. Instrument encoding | `CX-MET-01`, `CX-MET-02`, `CX-MET-03` | Projected+Attrs+Observed+RawOnly | `SC-METRIC-01`, `SC-CX-MET-01`, `SC-OBS-02` |
| 6-2. Runtime and transport metrics | `CX-MET-01`, `CX-MET-02` | Projected+Attrs+Observed+RawOnly | `SC-METRIC-01`, `SC-CX-MET-01` |
| 6-3. Turn, tool, and workflow metrics | `CX-MET-01`, `CX-MET-02` | Projected+Attrs+Observed+RawOnly | `SC-METRIC-01`, `SC-CX-MET-01` |
| 6-4. Published analytics catalog and OTLP implementation boundary | `CX-MET-01`, `CX-MET-02` | Projected+Attrs+Observed+RawOnly | `SC-METRIC-01`, `SC-CX-MET-01` |
| 7. Trace schema | `CX-TRACE-01`, `CX-TRACE-02`, `CX-TRACE-03`, `CX-USAGE-01` | Mapped+Projected+Attrs+Observed+RawOnly | `SC-TRACE-01`, `SC-CX-TRACE-01`, `SC-TRACE-EVENT-01`, `SC-CX-USAGE-01` |
| 7-1. Span export | `CX-TRACE-01`, `CX-TRACE-02` | Mapped+Projected+Attrs+Observed+RawOnly | `SC-TRACE-01`, `SC-CX-TRACE-01` |
| 7-2. Trace-safe event fields | `CX-TRACE-03`, `CX-USAGE-01` | Mapped+Attrs+RawOnly | `SC-TRACE-EVENT-01`, `SC-CX-USAGE-01` |
| 8. Content controls | `CX-CTRL-01`, `CX-CTRL-02`, `CX-CONT-01` | Mapped+Attrs+RawOnly | `SC-CX-CTRL-01`, `SC-CX-TOOL-01`, `SC-CX-CONT-01` |
| 9. Completeness and stability | — | Context | `SC-COVERAGE-01` |

#### Verification: SC-COVERAGE-01 — Cover every provider specification section

- **GIVEN** the current Claude Code and Codex provider data specifications
- **WHEN** the ingestion capability is reviewed
- **THEN** every numbered provider section has exactly one coverage-matrix row and at least one disposition

---

## Provider ingestion mapping

Claude Code and Codex use the same mapping columns. `Yes`
in Raw means the original value remains in the committed OTLP export. `Attrs`
means the value is available through canonical attributes JSON without a
dedicated canonical field. `None` means the derived representation does not
carry the value; it does not mean the raw export discards it. Verification IDs
resolve either to a Scenario in `spec.md` or to a Verification block in this
document. The runtime requirements are authoritative; these tables summarize
their provider-specific outcomes.

#### Claude Code ingestion mapping

| Rule ID | Source section | Signal / OTLP location | Provider selector | Recognition / gate | Canonical result | Observation | Projection | Raw | Verification ID |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `CL-RES-01` | 4-1 | Resource / all signals | Signal name, `event.name`, `service.name` | Lowercase concatenation contains `claude` | `source=claude`; attributes remain merged | Source | Attrs | Yes | `SC-SOURCE-01` |
| `CL-RES-02` | 4-1 | Resource / all signals | Other resource attributes | Local attribute overrides the same resource key | Canonical aliases when recognized | Identity/context only | Attrs | Yes | `SC-SOURCE-03` |
| `CL-SCOPE-01` | 4-2 | Scope / all signals | Scope name and version | Not inspected for source detection or normalization | None | None | None | Yes | `SC-RAW-01` |
| `CL-ATTR-01` | 4-3 | Record/span/data-point attributes | Session, prompt, model, agent, request, target, tool aliases | Provider match and destination absent | Canonical identity, model, agent, request, target, tool | Model-defined identity/context | Dedicated fields+Attrs | Yes | `SC-CL-ATTR-01` |
| `CL-ATTR-02` | 4-3 | Record/span/data-point attributes | Other shared attributes | No dedicated alias | None | None | Attrs | Yes | `SC-ALIAS-01` |
| `CL-LOG-01` | 5-1 | LogRecord envelope | Native fields and attributes | Every decoded log record | Canonical log envelope | One log observation | One log row | Yes | `SC-LOG-01` |
| `CL-EVT-01` | 5-2 | Log/span | Four special Claude event names | Exact normalized source event | Special `gen_ai.*` event name | Signal observation | Signal row | Yes | `SC-CL-EVT-01`, `SC-CL-EVT-03`, `SC-CL-EVT-04`, `SC-CL-EVT-05` |
| `CL-EVT-02` | 5-2, 5-3, 5-4 | Log/span | Other `claude_code.<event>` | Claude match; no allowlist | `gen_ai.<event>` | Signal observation | Signal row+Attrs | Yes | `SC-CL-EVT-02` |
| `CL-CONT-01` | 5-2, 5-3, 8 | Log/span attributes | Ordered content candidates | First eligible candidate | Canonical `content` | No content field | Log body or span content | Yes | `SC-CL-CONT-01` |
| `CL-USAGE-01` | 5-2 | `api_request` attributes | Token and request identity fields | Exact `api_request` | Authoritative usage | Usage counters+context | Log/span usage fields+Attrs | Yes | `SC-CL-USAGE-01` |
| `CL-USAGE-02` | 7, 7-2 | `llm_request` span/log attributes | Request identity and token fields | Exact `llm_request` | Corroborating identity; no provider-added counters | Existing counters+context | Span/log Attrs | Yes | `SC-CL-USAGE-02` |
| `CL-COST-01` | 5-2 | Any Claude event attributes | `cost_usd_micros` | Non-negative and parseable; overwrites `gen_ai.usage.cost_usd`, writes `cost_usd` only when absent | USD cost divided by 1,000,000 | None | Signal cost field+Attrs | Yes | `SC-CL-COST-01` |
| `CL-MET-01` | 6 | Gauge, Sum, Histogram point | Any `claude_code.*` metric | Supported point and Claude match | Generic `gen_ai.<suffix>` metric | One metric observation | One metric row per point | Yes | `SC-METRIC-01` |
| `CL-MET-02` | 6 | Metric | Token/cost usage metric names | Supported point | Generic metric value; no dedicated usage/cost semantics | Resource context only | Metric row+Attrs | Yes | `SC-CL-MET-01` |
| `CL-TRACE-01` | 7, 7-2 | Span | Claude semantic span | Semantic predicate succeeds | Canonical span | One span observation | One span row | Yes | `SC-TRACE-01` |
| `CL-TRACE-02` | 7, 7-2 | Span | Claude incidental span | Semantic predicate fails | None | None | None | Yes | `SC-TRACE-02` |
| `CL-TRACE-03` | 7, 7-2, 8 | Span event | Any span event | Span events are not traversed | None | None | None | Yes | `SC-TRACE-EVENT-01` |
| `CL-CTRL-01` | 8 | Producer controls | Claude telemetry environment controls | Agentmetry receives no producer setting | Received values only | Signal observation without content | Signal projection when received | Yes | `SC-CL-CTRL-01` |

#### Codex ingestion mapping

| Rule ID | Source section | Signal / OTLP location | Provider selector | Recognition / gate | Canonical result | Observation | Projection | Raw | Verification ID |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `CX-RES-01` | 4-1 | Resource / all signals | Signal name, `event.name`, `service.name` | Lowercase concatenation contains `codex` | `source=codex`; attributes remain merged | Source | Attrs | Yes | `SC-SOURCE-02` |
| `CX-RES-02` | 4-1 | Resource / all signals | Other resource attributes | Local attribute overrides the same resource key | Canonical aliases when recognized | Identity/context only | Attrs | Yes | `SC-SOURCE-03` |
| `CX-SCOPE-01` | 4-2 | Scope / all signals | Scope name and version | Not inspected for source detection or normalization | None | None | None | Yes | `SC-RAW-01` |
| `CX-ATTR-01` | 4-3 | Event/data-point attributes | Conversation, turn, model, agent, target, usage aliases | Provider match and rule-specific precedence | Canonical identity, model, agent, target, usage | Model-defined identity/context | Dedicated fields+Attrs | Yes | `SC-CX-ATTR-01` |
| `CX-ATTR-02` | 4-3 | Event attributes | Timestamp and request identity candidates | Usage exists and higher-priority identity is absent | Usage identity fallback | No usage identity field | Attrs | Yes | `SC-CX-USAGE-02` |
| `CX-LOG-01` | 5-1 | LogRecord envelope | Native fields and attributes | Every decoded log record | Canonical log envelope | One log observation | One log row | Yes | `SC-LOG-01` |
| `CX-EVT-01` | 5-2 | Log/span | `codex.sse_event` completed response | Exact event and kind | `gen_ai.response.completed` | Signal observation | Signal row | Yes | `SC-CX-EVT-01` |
| `AM-CODEX-EXT-01` | Agentmetry compatibility input | Log/span record | `codex.agent_communication` | Local compatibility event; not in upstream snapshot | Delegation/message and session aliases | Signal observation | Signal row+possible link | Yes | `SC-AM-CX-EXT-01` |
| `CX-EVT-03` | 5-2 | Log/span | Other `codex.<event>` | Codex match; no allowlist | `gen_ai.<event>` | Signal observation | Signal row+Attrs | Yes | `SC-CX-EVT-02` |
| `CX-TOOL-01` | 5-2, 8 | Tool attributes | Non-empty `tool_name` | Only tool name is required; arguments/output optional | Normalized tool, target, content | Tool activity kind | Signal content/tool fields+Attrs | Yes | `SC-CX-TOOL-02` |
| `CX-CONT-01` | 5-2, 8 | Tool attributes | Arguments message and output | Tool gate passes; arguments must be an object or valid JSON object string, while any non-empty string output is included without JSON parsing | Message plus `Result: <output>` | No content field | Signal content+Attrs | Yes | `SC-CX-CONT-01` |
| `CX-USAGE-01` | 5-2, 7-2 | Event/span/data-point attributes | Canonical or aliased counters | At least one counter exists | Authoritative completed response; otherwise corroborating | Counters+context when observed | Span/log dedicated counters; metric Attrs only | Yes | `SC-CX-USAGE-01` |
| `CX-MET-01` | 6, 6-1–6-4 | Gauge, Sum, Histogram point | Any metric | Supported point; Codex name/resource match | Generic `gen_ai.<suffix>` for `codex.*` | One metric observation | One metric row per point | Yes | `SC-METRIC-01` |
| `CX-MET-02` | 6, 6-1–6-4 | Metric metadata | Unit, temporality, bounds, buckets, exemplars | Present | None | None | None | Yes | `SC-CX-MET-01` |
| `CX-MET-03` | 6, 6-1 | Summary or ExponentialHistogram | Unsupported point type | Metric decoded | None | One metric observation | None | Yes | `SC-OBS-02` |
| `CX-TRACE-01` | 7, 7-1 | Span | Codex semantic span | Semantic predicate succeeds | Canonical span | One span observation | One span row | Yes | `SC-TRACE-01` |
| `CX-TRACE-02` | 7, 7-1 | Native span fields | IDs, status, kind, attributes | Semantic span for projection; all spans for raw | IDs/status/recognized attrs; kind has no field | Excludes status/kind | Span fields+Attrs | Yes | `SC-CX-TRACE-01` |
| `CX-TRACE-03` | 7, 7-2, 8 | Span event | Any Codex span event | Span events are not traversed | None | None | None | Yes | `SC-TRACE-EVENT-01` |
| `CX-CTRL-01` | 8 | Prompt/content | Producer prompt control | Agentmetry receives no producer setting | Received value without added redaction | Signal observation without content | Body/Attrs when received | Yes | `SC-CX-CTRL-01` |
| `CX-CTRL-02` | 8 | Tool message | Message begins `gAAAA` | Tool gate passes | Fixed placeholder; ciphertext remains in source attrs/raw | Tool activity kind | Canonical placeholder content | Yes | `SC-CX-TOOL-01` |

#### Verification: SC-MAPPING-01 — Trace a mapping rule to provider data

- **GIVEN** a mapping rule ID in either provider ingestion table
- **WHEN** a reviewer follows its source section and verification ID
- **THEN** the reviewer can determine the provider input, Agentmetry condition, canonical result, observation, projection, and raw-retention outcome
