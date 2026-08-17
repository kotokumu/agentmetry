# Codex OpenTelemetry Data Specification

---

## 1. Snapshot metadata

| Field | Value |
| --- | --- |
| Provider | Codex |
| Snapshot date | 2026-08-17 |
| Evidence boundary | Official documentation plus pinned first-party implementation |
| Primary source | `CODEX-ADVANCED` |

Published facts and implementation-derived facts are identified separately
because OpenAI does not publish a complete stable event and span schema.

---

## 2. Source references

| ID | Authority | Requested URL | Final URL | Retrieved | Content-Type | SHA-256 | Pinned commit | Scope |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `CODEX-ADVANCED` | Official OpenAI documentation | [Advanced configuration](https://developers.openai.com/codex/config-advanced/#observability-and-telemetry) | `https://learn.chatgpt.com/docs/config-file/config-advanced` | `2026-08-17T18:06:14+09:00` | `text/html` | `aca543c6c62798507e5f11f7bfd8248f88812e958c25e8ec56d98b8099d499b5` | — | OTel configuration, representative events, and published metric catalog |
| `CODEX-CONFIG` | Official OpenAI documentation | [Configuration reference](https://developers.openai.com/codex/config-reference/) | `https://learn.chatgpt.com/docs/config-file/config-reference` | `2026-08-17T18:06:14+09:00` | `text/html` | `cc3593215e1d2ad8f95589c3d2819b0063de2d114a515f86ec9148ae2b312692` | — | Exporter keys and accepted values |
| `CODEX-EVENTS-IMPL` | OpenAI first-party implementation | [Pinned session telemetry](https://raw.githubusercontent.com/openai/codex/c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc/codex-rs/otel/src/events/session_telemetry.rs) | `https://raw.githubusercontent.com/openai/codex/c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc/codex-rs/otel/src/events/session_telemetry.rs` | `2026-08-17T18:06:14+09:00` | `text/plain` | `b71995c35cfb42978b8100091af064a8d311cc74a3476ada13dfa68921703d53` | `c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc` | Central session log and trace events and their field types |
| `CODEX-EVENT-MACROS-IMPL` | OpenAI first-party implementation | [Pinned event macros](https://raw.githubusercontent.com/openai/codex/c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc/codex-rs/otel/src/events/shared.rs) | `https://raw.githubusercontent.com/openai/codex/c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc/codex-rs/otel/src/events/shared.rs` | `2026-08-17T18:06:14+09:00` | `text/plain` | `51f87632e384a0a4c728ffa533ecd5c4afe8615ee3bdd5edeb2e55fadd73ac91` | `c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc` | Signal-specific shared attributes |
| `CODEX-PROVIDER-IMPL` | OpenAI first-party implementation | [Pinned OTel provider](https://raw.githubusercontent.com/openai/codex/c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc/codex-rs/otel/src/provider.rs) | `https://raw.githubusercontent.com/openai/codex/c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc/codex-rs/otel/src/provider.rs` | `2026-08-17T18:06:14+09:00` | `text/plain` | `8d68b8335937156509a8ae3f84e8d8e754677b900b2db973467bdba2c44c7c51` | `c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc` | Resources, exporters, filters, and span attributes |
| `CODEX-METRICS-IMPL` | OpenAI first-party implementation | [Pinned metric client](https://raw.githubusercontent.com/openai/codex/c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc/codex-rs/otel/src/metrics/client.rs) | `https://raw.githubusercontent.com/openai/codex/c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc/codex-rs/otel/src/metrics/client.rs` | `2026-08-17T18:06:14+09:00` | `text/plain` | `b381b27bd09ca44fcc549ce742d638c0bc45b6e71b1ae1473d7ebd3fbb740cee` | `c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc` | Instrument types, meter, units, boundaries, and data-point types |
| `CODEX-METRIC-NAMES-IMPL` | OpenAI first-party implementation | [Pinned metric names](https://raw.githubusercontent.com/openai/codex/c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc/codex-rs/otel/src/metrics/names.rs) | `https://raw.githubusercontent.com/openai/codex/c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc/codex-rs/otel/src/metrics/names.rs` | `2026-08-17T18:06:14+09:00` | `text/plain` | `f5a979d97486476392fa5d2cd8dc26f12aefcd18f69b5271593a147ab15e8902` | `c6058ccaa91ab17159cf805bf4d6d4edd87fe5fc` | Central metric-name constants |

The two documentation URLs currently redirect to `learn.chatgpt.com`. Their
hashes cover the retrieved canonical HTML. Implementation hashes cover the
exact raw bytes at the pinned commit.

---

## 3. OTLP export contract

| Signal | OTLP container | Gate/condition | Exporter and protocol | Delivery | Evidence | Source |
| --- | --- | --- | --- | --- | --- | --- |
| Logs | `ExportLogsServiceRequest.resource_logs` | `otel.exporter` set to `otlp-http` or `otlp-grpc` | HTTP setting `binary` encodes OTLP protobuf; `json` encodes OTLP JSON; or OTLP/gRPC | Asynchronous batches; flush on shutdown | Published | `CODEX-ADVANCED`, `CODEX-CONFIG` |
| Metrics | `ExportMetricsServiceRequest.resource_metrics` | `otel.metrics_exporter` set to `otlp-http` or `otlp-grpc` | HTTP setting `binary` encodes OTLP protobuf; `json` encodes OTLP JSON; or OTLP/gRPC | Periodic SDK export; exact interval not published | Published | `CODEX-CONFIG`, `CODEX-METRICS-IMPL` |
| Traces | `ExportTraceServiceRequest.resource_spans` | `otel.trace_exporter` set to `otlp-http` or `otlp-grpc` | HTTP setting `binary` encodes OTLP protobuf; `json` encodes OTLP JSON; or OTLP/gRPC | Batch span processor | Implemented | `CODEX-CONFIG`, `CODEX-PROVIDER-IMPL` |

Logs and traces are disabled when their exporter is `none`. Metrics default to
`statsig`, which resolves to Codex's built-in Statsig OTLP/HTTP destination
rather than a user-selected OTLP destination. In debug builds it resolves to
`none`.

---

## 4. Resource and scope schema

### 4-1. Resource attributes

| OTLP location | Name or field | OTLP type | Presence | Value/unit/meaning | Gate/condition | Evidence | Source |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `Resource.attributes` | `service.name` | string | Always | Configured Codex OTel service name | — | Implemented | `CODEX-PROVIDER-IMPL` |
| `Resource.attributes` | `service.version` | string | Always | Configured Codex service version | — | Implemented | `CODEX-PROVIDER-IMPL` |
| `Resource.attributes` | `env` | string | Always | `otel.environment`; default `dev` | — | Implemented | `CODEX-CONFIG`, `CODEX-PROVIDER-IMPL` |
| `Resource.attributes` | `host.name` | string | Logs only, when non-empty | Detected host name | — | Implemented | `CODEX-PROVIDER-IMPL` |
| `Resource.attributes` | `os` | string | Metrics only, unless `unspecified` | Detected operating system | — | Implemented | `CODEX-METRICS-IMPL` |
| `Resource.attributes` | `os_version` | string | Metrics only, unless `unspecified` | Detected operating-system version | — | Implemented | `CODEX-METRICS-IMPL` |

### 4-2. Instrumentation scope

| OTLP location | Name or field | OTLP type | Presence | Value/unit/meaning | Gate/condition | Evidence | Source |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `ScopeMetrics.scope.name` | Instrumentation scope name | string | Metrics | `codex` | — | Implemented | `CODEX-METRICS-IMPL` |
| `ScopeLogs.scope` | Name and version | unspecified | Unspecified | Not defined by the retrieved sources | — | Not established | `CODEX-ADVANCED`, `CODEX-EVENTS-IMPL` |
| `ScopeSpans.scope.name` | Instrumentation scope name | string | Traces | Configured `service_name` | — | Implemented | `CODEX-PROVIDER-IMPL` |
| `ScopeSpans.scope.version` | Instrumentation scope version | unspecified | Unspecified | Not established by the retrieved sources | — | Not established | `CODEX-PROVIDER-IMPL` |

### 4-3. Shared event attributes

The first-party event macros append these values to event attributes. `Log`
and `trace event` are different field sets.

| OTLP location | Name or field | OTLP type | Log | Trace event | Presence | Gate/condition | Evidence | Source |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Event attributes | `event.timestamp` | string | Yes | Yes | Always; RFC 3339 UTC with milliseconds | — | Implemented | `CODEX-EVENT-MACROS-IMPL` |
| Event attributes | `conversation.id` | string | Yes | Yes | Always | — | Implemented | `CODEX-EVENT-MACROS-IMPL` |
| Event attributes | `app.version` | string | Yes | Yes | Always | — | Implemented | `CODEX-ADVANCED`, `CODEX-EVENT-MACROS-IMPL` |
| Event attributes | `auth_mode` | string | Yes | Yes | Conditional | — | Implemented | `CODEX-ADVANCED`, `CODEX-EVENT-MACROS-IMPL` |
| Event attributes | `originator` | string | Yes | Yes | Always | — | Implemented | `CODEX-ADVANCED`, `CODEX-EVENT-MACROS-IMPL` |
| Event attributes | `terminal.type` | string | Yes | Yes | Always | — | Implemented | `CODEX-EVENT-MACROS-IMPL` |
| Event attributes | `model` | string | Yes | Yes | Always | — | Implemented | `CODEX-ADVANCED`, `CODEX-EVENT-MACROS-IMPL` |
| Event attributes | `slug` | string | Yes | Yes | Always | — | Implemented | `CODEX-EVENT-MACROS-IMPL` |
| Event attributes | `user.account_id`, `user.email` | string | Yes | No | Conditional | — | Implemented | `CODEX-EVENT-MACROS-IMPL` |

Published metric context includes `auth_mode`, `originator`, `session_source`,
`model`, and `app.version`. The normal `SessionTelemetry` path attaches these
tags and can add sanitized `service_name`; the pinned implementation also has
an explicit path that disables metadata tags.

---

## 5. Log record schema

### 5-1. Common log envelope

| OTLP location | Name or field | OTLP type | Presence | Value/unit/meaning | Gate/condition | Evidence | Source |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `LogRecord.attributes` | `event.name` | string | Always for central session events | Fully qualified `codex.<event>` identifier | — | Implemented | `CODEX-EVENTS-IMPL` |
| `LogRecord.attributes` | Shared log attributes | See section 4 | Every central session event | Session and identity context | — | Implemented | `CODEX-EVENT-MACROS-IMPL` |
| `LogRecord.severity_number` | Severity | enum | Central events | `INFO` through the tracing bridge | — | Implemented | `CODEX-EVENT-MACROS-IMPL` |
| `LogRecord.event_name`, `body` | Native event name and body | unspecified | Unspecified | Not defined as a public contract | — | Not established | `CODEX-ADVANCED`, `CODEX-EVENTS-IMPL` |

OpenAI documentation calls the following list representative rather than
exhaustive: `codex.conversation_starts`, `codex.api_request`,
`codex.sse_event`, `codex.websocket_request`, `codex.websocket_event`,
`codex.user_prompt`, `codex.tool_decision`, and `codex.tool_result`.

### 5-2. Implemented central event records

Types reflect the pinned tracing callsite. `string boolean` and `string
integer` mean formatting converts the value to an OTLP string.

| `event.name` | Additional log attributes and types | Presence and values | Gate/condition | Evidence | Source |
| --- | --- | --- | --- | --- | --- |
| `codex.startup_phase` | `startup.phase`, `startup.status` string; `duration_ms` string integer | Status conditional | Coarse startup phase finishes | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.turn_ttft` | `duration_ms` string integer | Always | First token observed | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.plugin_install_elicitation_sent` | `plugin_install.tool_type`, `.tool_id`, `.tool_name` string | Always | Install elicitation sent | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.plugin_install_suggestion` | Tool fields and `response_action` string; `user_confirmed`, `completed` boolean | Always | Suggestion outcome recorded | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.conversation_starts` | Provider/reasoning/policy strings; auth-environment booleans and optional strings; context limits integer; `mcp_servers` comma-separated string | Optional values omitted when absent | Conversation begins | Implemented | `CODEX-ADVANCED`, `CODEX-EVENTS-IMPL` |
| `codex.api_request` | `duration_ms` string integer; `http.response.status_code` integer; `error.message` string; `attempt` integer; auth booleans/strings; endpoint and request identity strings | Status and errors conditional; success is represented by the paired metric, not a log field in this implementation | API attempt finishes | Implemented | `CODEX-ADVANCED`, `CODEX-EVENTS-IMPL` |
| `codex.websocket_connect` | API/auth fields; `success` string boolean; `auth.connection_reused` boolean | Status/error conditional | WebSocket connection attempt finishes | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.websocket_request` | `duration_ms`, `success`, `error.message`; auth environment and agent/task identity | Duration and success are strings; error conditional | WebSocket request finishes | Implemented | `CODEX-ADVANCED`, `CODEX-EVENTS-IMPL` |
| `codex.auth_recovery` | `auth.mode`, `.step`, `.outcome`, request/CF/error/reason strings; `auth.state_changed` boolean | Optional values omitted | Authentication recovery step recorded | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.sse_event` | `event.kind`, `duration_ms`, `error.message`; completed-response token fields, `ttft_ms`, `service_tier`, `model_reasoning_effort` | Kind/error/usage fields depend on stream outcome | SSE message processed or failed | Implemented | `CODEX-ADVANCED`, `CODEX-EVENTS-IMPL` |
| `codex.user_prompt` | `prompt_length` string integer; `prompt` string | Prompt is `[REDACTED]` unless enabled | User prompt recorded | Implemented | `CODEX-ADVANCED`, `CODEX-EVENTS-IMPL` |
| `codex.tool_decision` | `tool_name`, `call_id`, `decision`, `source` string | Source conditional | Tool approval decision recorded | Implemented | `CODEX-ADVANCED`, `CODEX-EVENTS-IMPL` |
| `codex.sandbox_outcome` | `tool_name`, `call_id`, `outcome` string; initial/escalated duration integer | Escalated duration conditional | Sandbox execution outcome recorded | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.tool_result` | `tool_name`, `call_id`, `arguments`, `duration_ms`, `success`, `output`, `mcp_server`, `mcp_server_origin` string | `call_id`/arguments can be absent on immediate failure | Tool execution finishes or fails before execution | Implemented | `CODEX-ADVANCED`, `CODEX-EVENTS-IMPL` |

For `codex.sse_event` with `event.kind=response.completed`, the pinned field
types are:

| Field | OTLP attribute type | Meaning |
| --- | --- | --- |
| `input_token_count` | string integer | Input tokens |
| `output_token_count` | string integer | Output tokens |
| `cached_token_count` | integer | Cached input tokens |
| `cache_write_token_count` | integer | Cache-write input tokens |
| `reasoning_token_count` | integer | Reasoning output tokens |
| `tool_token_count` | string integer | Total tokens |
| `ttft_ms` | integer | Time to first token; conditional |
| `service_tier` | string | Service tier; conditional |
| `model_reasoning_effort` | string | Reasoning effort; conditional |

`codex.websocket_event` is published as a representative event type, but the
pinned central implementation records it as metrics only. The retrieved
sources do not establish a log-record field set for it.

---

## 6. Metric schema

### 6-1. Instrument encoding

| OTLP property | Counter | General histogram | Duration histogram | Gate/condition | Evidence | Source |
| --- | --- | --- | --- | --- | --- | --- |
| SDK instrument | `Counter<u64>` | `Histogram<f64>` | `Histogram<f64>` | — | Implemented | `CODEX-METRICS-IMPL` |
| OTLP data | Sum | Histogram | Histogram | — | Implemented | `CODEX-METRICS-IMPL` |
| Unit | Unset | Unset | `ms` or `s` | — | Implemented | `CODEX-METRICS-IMPL` |
| Value | Non-negative integer increment | Integer converted to float | Floating duration | — | Implemented | `CODEX-METRICS-IMPL` |
| Temporality | Delta | Delta | Delta | OTLP export | Implemented | `CODEX-METRICS-IMPL` |
| Histogram boundaries | — | SDK default | Fixed provider boundaries | — | Implemented | `CODEX-METRICS-IMPL` |

The same client also implements `Gauge<i64>` and `ObservableGauge<i64>`, which
serialize as OTLP Gauge data. Temporality does not apply to gauges.

The explicit OTel table includes the `codex.` prefix. The separate analytics
catalog omits the prefix by documentation convention. The pinned central names
file uses full `codex.`-prefixed names, although the generic metric-name
validator does not enforce that prefix.

The explicit OTel table contains the API, SSE, WebSocket, and tool count and
duration metrics below. The broader analytics/health catalog is a separate
published surface. At the pinned commit, an OTLP metrics exporter uses the
shared `MetricsClient` without a ten-name allowlist, so it can export broader
catalog metrics when their callsites record them. The analytics catalog alone
is not a stable OTLP compatibility contract.

### 6-2. Runtime and transport metrics

| `Metric.name` | Instrument | Unit | Data-point attributes | Meaning | Evidence | Source |
| --- | --- | --- | --- | --- | --- | --- |
| `codex.api_request` | Counter | unset | `status`, `success` | API request count | Published | `CODEX-ADVANCED` |
| `codex.api_request.duration_ms` | Histogram | `ms` | `status`, `success` | API request duration | Implemented | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| `codex.sse_event` | Counter | unset | `kind`, `success` | SSE event count | Published | `CODEX-ADVANCED` |
| `codex.sse_event.duration_ms` | Histogram | `ms` | `kind`, `success` | SSE processing duration | Implemented | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| `codex.websocket.request` | Counter | unset | `success` | WebSocket request count | Published | `CODEX-ADVANCED` |
| `codex.websocket.request.duration_ms` | Histogram | `ms` | `success` | WebSocket request duration | Implemented | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| `codex.websocket.event` | Counter | unset | `kind`, `success` | WebSocket event count | Published | `CODEX-ADVANCED` |
| `codex.websocket.event.duration_ms` | Histogram | `ms` | `kind`, `success` | WebSocket event duration | Implemented | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| `codex.responses_api_overhead.duration_ms` | Histogram | `ms` | Shared metric context | Response overhead | Implemented | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| `codex.responses_api_inference_time.duration_ms` | Histogram | `ms` | Shared metric context | Inference time | Implemented | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| `codex.responses_api_engine_iapi_ttft.duration_ms` | Histogram | `ms` | Shared metric context | Engine IAPI TTFT | Implemented | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| `codex.responses_api_engine_service_ttft.duration_ms` | Histogram | `ms` | Shared metric context | Engine service TTFT | Implemented | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| `codex.responses_api_engine_iapi_tbt.duration_ms` | Histogram | `ms` | Shared metric context | Engine IAPI TBT | Implemented | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| `codex.responses_api_engine_service_tbt.duration_ms` | Histogram | `ms` | Shared metric context | Engine service TBT | Implemented | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| `codex.transport.fallback_to_http` | Counter | unset | `from_wire_api` | WebSocket-to-HTTP fallback | Published | `CODEX-ADVANCED` |
| `codex.remote_models.fetch_update.duration_ms`, `codex.remote_models.load_cache.duration_ms` | Histogram | `ms` | Shared metric context | Model definition fetch/cache latency | Published | `CODEX-ADVANCED` |
| `codex.startup_prewarm.duration_ms`, `codex.startup_prewarm.age_at_first_turn_ms` | Histogram | `ms` | `status` | Startup prewarm timing | Implemented | `CODEX-ADVANCED`, `CODEX-METRIC-NAMES-IMPL` |
| `codex.cloud_requirements.fetch.duration_ms` | Histogram | `ms` | Shared context | Cloud requirements fetch duration | Published | `CODEX-ADVANCED` |
| `codex.cloud_requirements.fetch_attempt`, `codex.cloud_requirements.fetch_final`, `codex.cloud_requirements.load` | Counter | unset | Published trigger/outcome/attempt/status fields | Cloud requirements outcomes | Published | `CODEX-ADVANCED` |

### 6-3. Turn, tool, and workflow metrics

| `Metric.name` | Instrument | Unit | Data-point attributes | Meaning | Evidence | Source |
| --- | --- | --- | --- | --- | --- | --- |
| `codex.turn.e2e_duration_ms`, `codex.turn.ttft.duration_ms`, `codex.turn.ttfm.duration_ms` | Histogram | `ms` | Shared context | Turn latency measurements | Implemented | `CODEX-ADVANCED`, `CODEX-METRIC-NAMES-IMPL` |
| `codex.turn.network_proxy` | Counter | unset | `active`, `tmp_mem_enabled` | Per-turn proxy state | Published | `CODEX-ADVANCED` |
| `codex.turn.memory` | Counter | unset | `read_allowed`, `feature_enabled`, `config_use_memories`, `has_citations` | Per-turn memory state | Published | `CODEX-ADVANCED` |
| `codex.turn.tool.call` | Histogram | unset | `tmp_mem_enabled` | Tool calls in a turn | Implemented | `CODEX-ADVANCED`, `CODEX-METRIC-NAMES-IMPL` |
| `codex.turn.token_usage` | Histogram | unset | `token_type`, `tmp_mem_enabled` | Token usage; type is `total`, `input`, `cached_input`, `output`, or `reasoning_output` | Implemented | `CODEX-ADVANCED`, `CODEX-METRIC-NAMES-IMPL` |
| `codex.tool.call`, `codex.tool.call.duration_ms` | Counter, histogram | unset, `ms` | `tool`, `success` | Tool volume and duration | Implemented | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| `codex.tool.unified_exec` | Counter | unset | `tty` | Unified exec calls | Implemented | `CODEX-ADVANCED`, `CODEX-METRIC-NAMES-IMPL` |
| `codex.approval.requested` | Counter | unset | `tool`, `approved` | Approval outcome | Published | `CODEX-ADVANCED` |
| `codex.mcp.call`, `codex.mcp.call.duration_ms` | Counter, histogram | unset, `ms` | `status`; optionally tool/connector identity | MCP call volume and duration | Published | `CODEX-ADVANCED` |
| `codex.mcp.tools.list.duration_ms`, `codex.mcp.tools.fetch_uncached.duration_ms`, `codex.mcp.tools.cache_write.duration_ms` | Histogram | `ms` | `cache` on list | MCP discovery/cache timing | Published | `CODEX-ADVANCED` |
| `codex.hooks.run`, `codex.hooks.run.duration_ms` | Counter, histogram | unset, `ms` | `hook_name`, `source`, `status` | Hook volume and duration | Implemented | `CODEX-ADVANCED`, `CODEX-METRIC-NAMES-IMPL` |

### 6-4. Published analytics catalog and OTLP implementation boundary

| Group | Published catalog names | Instrument and dimensions | OTLP evidence | Source |
| --- | --- | --- | --- | --- |
| Feature/process | `codex.feature.state`, `codex.status_line`, `codex.model_warning` | Counters; feature metric has `feature`, `value` | Shared `MetricsClient` can export recorded catalog metrics; cited source does not verify every callsite | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| Threads | `codex.thread.started`, `codex.thread.fork`, `codex.thread.rename`, `codex.thread.side`, `codex.thread.skills.enabled_total`, `codex.thread.skills.kept_total`, `codex.thread.skills.truncated` | Lifecycle counters and skill histograms; fields are `is_git` or `source` where published | Same boundary | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| Tasks | `codex.task.compact`, `codex.task.review`, `codex.task.undo`, `codex.task.user_shell`, `codex.shell_snapshot`, `codex.shell_snapshot.duration_ms`, `codex.skill.injected` | Counters and duration histogram; `type`, `success`, `failure_reason`, `status`, and `skill` apply where published | Same boundary | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| Plugins/agents | `codex.plugins.startup_sync`, `codex.plugins.startup_sync.final`, `codex.multi_agent.spawn`, `codex.multi_agent.resume`, `codex.multi_agent.nickname_pool_reset` | Counters; `transport`, `status`, or `role` apply where published | Same boundary | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| Memory | `codex.memory.phase1`, `codex.memory.phase1.e2e_ms`, `codex.memory.phase1.output`, `codex.memory.phase1.token_usage`, `codex.memory.phase2`, `codex.memory.phase2.e2e_ms`, `codex.memory.phase2.input`, `codex.memory.phase2.token_usage`, `codex.memories.usage` | Counters and histograms; `status`, `token_type`, `kind`, `tool`, and `success` apply where published | Same boundary | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| Local state | `codex.external_agent_config.detect`, `codex.external_agent_config.import`, `codex.db.backfill`, `codex.db.backfill.duration_ms`, `codex.db.error` | Counters and duration histogram; `migration_type`, `skills_count`, `status`, and `stage` apply where published | Same boundary | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |
| Windows sandbox | `codex.windows_sandbox.setup_success`, `codex.windows_sandbox.setup_failure`, `codex.windows_sandbox.setup_duration_ms`, `codex.windows_sandbox.elevated_setup_success`, `codex.windows_sandbox.elevated_setup_failure`, `codex.windows_sandbox.elevated_setup_canceled`, `codex.windows_sandbox.elevated_setup_duration_ms`, `codex.windows_sandbox.elevated_prompt_shown`, `codex.windows_sandbox.elevated_prompt_accept`, `codex.windows_sandbox.elevated_prompt_use_legacy`, `codex.windows_sandbox.elevated_prompt_quit`, `codex.windows_sandbox.fallback_prompt_shown`, `codex.windows_sandbox.fallback_retry_elevated`, `codex.windows_sandbox.fallback_use_legacy`, `codex.windows_sandbox.fallback_prompt_quit`, `codex.windows_sandbox.legacy_setup_preflight_failed`, `codex.windows_sandbox.setup_elevated_sandbox_command`, `codex.windows_sandbox.createprocessasuserw_failed` | Counters and duration histograms; published result/originator/mode/error/path fields apply | Same boundary | `CODEX-ADVANCED`, `CODEX-METRICS-IMPL` |

The pinned central name file additionally defines metrics not present in the
retrieved published catalog, including `codex.process.start`,
`codex.artifact.operation.*`, `codex.guardian.review*`, `codex.goal.*`,
`codex.startup.phase.duration_ms`,
`codex.plugins.install_elicitation.sent`, and
`codex.plugins.install_suggestion`. Their names are implementation evidence;
their complete dimensions are not promoted to a published contract here.

---

## 7. Trace schema

The public configuration exposes trace export but does not publish a stable
span-name or parent/child catalog.

### 7-1. Span export

| OTLP location | Name or field | OTLP type | Presence | Value/unit/meaning | Gate/condition | Evidence | Source |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `ResourceSpans.resource` | Resource attributes | Resource | Every export | Section 4 resource, without `host.name` | — | Implemented | `CODEX-PROVIDER-IMPL` |
| `Span.name`, `parent_span_id`, `kind`, `status` | Native span contract | unspecified | Unspecified | No complete stable catalog in retrieved sources | — | Not established | `CODEX-ADVANCED`, `CODEX-CONFIG` |
| `Span.attributes` | Configured span attributes | string map | Every span when configured | Values added by the span processor at span start | — | Implemented | `CODEX-PROVIDER-IMPL` |
| `Span.events.attributes` | `event.name` and trace-safe fields | Attributes | Central events emitted to active spans | Uses the trace event field set below | — | Implemented | `CODEX-EVENTS-IMPL`, `CODEX-EVENT-MACROS-IMPL` |

### 7-2. Trace-safe event fields

| `event.name` | Trace event fields that differ from logs | Gate/condition | Evidence | Source |
| --- | --- | --- | --- | --- |
| `codex.conversation_starts` | Replaces comma-separated `mcp_servers` with integer `mcp_server_count` | — | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.sse_event` | Includes failure/completed fields; normal success events are log-only | — | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.user_prompt` | Omits prompt; adds integer `text_input_count`, `image_input_count`, `local_image_input_count`; prompt length remains string integer | — | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.tool_decision` | No trace event in the pinned central callsite | — | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.tool_result` | Omits arguments/output; adds integer lengths/line count, string `tool_origin`, boolean `mcp_tool`, and error on immediate failure | — | Implemented | `CODEX-EVENTS-IMPL` |
| Other `log_and_trace_event` records | Common fields plus the same event-specific fields | — | Implemented | `CODEX-EVENTS-IMPL` |

The active response-handling span receives these attributes when the matching
response item is observed:

| `Span.attributes` field | OTLP type | Gate/condition | Value/unit/meaning | Evidence | Source |
| --- | --- | --- | --- | --- | --- |
| `otel.name` | string | Every response event | Response event type | Implemented | `CODEX-EVENTS-IMPL` |
| `from` | string | Output item added/done | `output_item_added` or `output_item_done` | Implemented | `CODEX-EVENTS-IMPL` |
| `tool_name` | string | Function-call output item | Function name | Implemented | `CODEX-EVENTS-IMPL` |
| `gen_ai.usage.input_tokens` | integer | Completed response with usage | Input tokens | Implemented | `CODEX-EVENTS-IMPL` |
| `gen_ai.usage.cache_read.input_tokens` | integer | Same | Cached input tokens | Implemented | `CODEX-EVENTS-IMPL` |
| `gen_ai.usage.cache_write.input_tokens` | integer | Same | Cache-write input tokens | Implemented | `CODEX-EVENTS-IMPL` |
| `gen_ai.usage.output_tokens` | integer | Same | Output tokens | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.usage.reasoning_output_tokens` | integer | Same | Reasoning output tokens | Implemented | `CODEX-EVENTS-IMPL` |
| `codex.usage.total_tokens` | integer | Same | Total tokens | Implemented | `CODEX-EVENTS-IMPL` |

---

## 8. Content controls

| Control or rule | Affected OTLP data | Default | Behavior | Gate/condition | Evidence | Source |
| --- | --- | --- | --- | --- | --- | --- |
| `otel.log_user_prompt` | Log `codex.user_prompt.prompt` | `false` | Uses `[REDACTED]` unless enabled | `otel.log_user_prompt` | Implemented | `CODEX-ADVANCED`, `CODEX-CONFIG`, `CODEX-EVENTS-IMPL` |
| Signal-specific event macros | Log versus trace event attributes | Always active | Account identity, prompt body, and tool arguments/output stay out of trace-safe fields; failure `error.message` fields can still contain error text | Always active | Implemented | `CODEX-EVENT-MACROS-IMPL`, `CODEX-EVENTS-IMPL` |
| Tool result log record | `arguments`, `output` | Included by pinned implementation | Can contain sensitive tool input and output even when prompt logging is disabled | Pinned implementation | Implemented | `CODEX-EVENTS-IMPL` |

---

## 9. Completeness and stability

- The official log-event list is representative, not exhaustive. The pinned
  central session implementation supplies a reproducible implemented schema,
  but does not prove that no other Codex module emits events.
- The explicit OTel metric table is the documented OTel subset. The separate
  analytics/health catalog is broader. A custom OTLP exporter uses the shared
  MetricsClient and can receive catalog metrics beyond the subset, but exact
  emission depends on callsites and the selected commit.
- Trace export is public configuration. Span names, span kinds, status rules,
  and parent/child relationships are not published as a complete stable
  schema.
- Native `LogRecord.event_name`, body, log instrumentation scope, and trace
  instrumentation-scope version remain `Unspecified`. The pinned
  implementation sets trace scope name to configured `service_name` and OTLP
  metric temporality to delta.
- Pinned implementation types describe the selected commit. Rust tracing
  display formatting intentionally produces strings for some numeric and
  boolean-looking fields.
- The hashes and source IDs make documentation and implementation changes
  detectable.
