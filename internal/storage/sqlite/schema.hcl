schema "main" {
}

table "spans" {
  schema = schema.main

  column "source" {
    type    = text
    default = "unknown"
  }
  column "trace_id"          { type = text }
  column "span_id"           { type = text }
  column "parent_span_id"    { type = text }
  column "name"              { type = text }
  column "started_at"        { type = text }
  column "ended_at"          { type = text }
  column "status"            { type = text }
  column "activity_kind"     { type = text }
  column "tool_name"         { type = text }
  column "target_agent_id"   { type = text }
  column "target_agent_type" {
    type    = text
    default = ""
  }
  column "content"           { type = text }
  column "agent_id"          { type = text }
  column "agent_definition"  {
    type    = text
    default = ""
  }
  column "agent_type"        { type = text }
  column "parent_agent_id"   { type = text }
  column "run_id"            { type = text }
  column "model"             { type = text }
  column "cost_usd" {
    type = real
    null = true
  }
  column "input_tokens"      { type = integer }
  column "output_tokens"     { type = integer }
  column "cache_read_tokens" { type = integer }
  column "cache_write_tokens" { type = integer }
  column "reasoning_tokens"  { type = integer }
  column "input_tokens_reported" {
    type = integer
    default = 0
  }
  column "output_tokens_reported" {
    type = integer
    default = 0
  }
  column "cache_read_tokens_reported" {
    type = integer
    default = 0
  }
  column "cache_write_tokens_reported" {
    type = integer
    default = 0
  }
  column "reasoning_tokens_reported" {
    type = integer
    default = 0
  }
  column "attributes_json"   { type = text }

  primary_key {
    columns = [table.spans.column.trace_id, table.spans.column.span_id]
  }
  index "spans_ended_at_idx" {
    columns = [table.spans.column.ended_at]
  }
  index "spans_agent_id_idx" {
    columns = [table.spans.column.agent_id]
  }
  index "spans_run_id_idx" {
    columns = [table.spans.column.run_id]
  }
  index "spans_source_run_ended_at_idx" {
    columns = [table.spans.column.source, table.spans.column.run_id, table.spans.column.ended_at]
  }
}

table "session_rollups" {
  schema = schema.main

  column "source"       { type = text }
  column "run_id"       { type = text }
  column "started_at"   { type = text }
  column "ended_at"     { type = text }
  column "activity_count" { type = integer }
  column "trace_count" {
    type    = integer
    default = 0
  }
  column "log_count" {
    type    = integer
    default = 0
  }
  column "agent_count"    { type = integer }
  column "input_tokens"   { type = integer }
  column "output_tokens"  { type = integer }
  column "cache_read_tokens" { type = integer }
  column "cache_write_tokens" { type = integer }
  column "reasoning_tokens" { type = integer }
  column "input_reported" { type = integer }
  column "output_reported" { type = integer }
  column "cache_read_reported" { type = integer }
  column "cache_write_reported" { type = integer }
  column "reasoning_reported" { type = integer }
  column "cost_usd" { type = real }

  primary_key {
    columns = [table.session_rollups.column.source, table.session_rollups.column.run_id]
  }
  index "session_rollups_ended_at_idx" {
    columns = [table.session_rollups.column.ended_at]
  }
  index "session_rollups_source_ended_at_idx" {
    columns = [table.session_rollups.column.source, table.session_rollups.column.ended_at]
  }
}

table "logs" {
  schema = schema.main

  column "source" {
    type    = text
    default = "unknown"
  }

  column "id" {
    type           = integer
    null           = true
    auto_increment = true
  }
  column "observed_at"        { type = text }
  column "severity"           { type = text }
  column "name"               { type = text }
  column "body"               { type = text }
  column "trace_id"           { type = text }
  column "span_id"            { type = text }
  column "activity_kind"      { type = text }
  column "tool_name"          { type = text }
  column "target_agent_id"    { type = text }
  column "target_agent_type" {
    type    = text
    default = ""
  }
  column "agent_id"           { type = text }
  column "agent_definition"  {
    type    = text
    default = ""
  }
  column "agent_type"         { type = text }
  column "parent_agent_id"    { type = text }
  column "run_id"             { type = text }
  column "model"              { type = text }
  column "cost_usd" {
    type = real
    null = true
  }
  column "input_tokens" {
    type    = integer
    default = 0
  }
  column "output_tokens" {
    type    = integer
    default = 0
  }
  column "cache_read_tokens" {
    type    = integer
    default = 0
  }
  column "cache_write_tokens" {
    type    = integer
    default = 0
  }
  column "reasoning_tokens" {
    type    = integer
    default = 0
  }
  column "input_tokens_reported" {
    type = integer
    default = 0
  }
  column "output_tokens_reported" {
    type = integer
    default = 0
  }
  column "cache_read_tokens_reported" {
    type = integer
    default = 0
  }
  column "cache_write_tokens_reported" {
    type = integer
    default = 0
  }
  column "reasoning_tokens_reported" {
    type = integer
    default = 0
  }
  column "attributes_json"    { type = text }

  primary_key {
    columns = [table.logs.column.id]
  }
  index "logs_observed_at_idx" {
    columns = [table.logs.column.observed_at]
  }
  index "logs_source_run_observed_at_idx" {
    columns = [table.logs.column.source, table.logs.column.run_id, table.logs.column.observed_at]
  }
}

table "metrics" {
  schema = schema.main

  column "source" {
    type    = text
    default = "unknown"
  }

  column "id" {
    type           = integer
    null           = true
    auto_increment = true
  }
  column "observed_at"     { type = text }
  column "name"            { type = text }
  column "kind"            { type = text }
  column "value"           { type = real }
  column "agent_id"        { type = text }
  column "agent_definition" {
    type    = text
    default = ""
  }
  column "agent_type"      { type = text }
  column "parent_agent_id" { type = text }
  column "run_id"          { type = text }
  column "model"           { type = text }
  column "cost_usd" {
    type = real
    null = true
  }
  column "attributes_json" { type = text }

  primary_key {
    columns = [table.metrics.column.id]
  }
  index "metrics_observed_at_idx" {
    columns = [table.metrics.column.observed_at]
  }
}

table "otlp_exports" {
  schema = schema.main

  column "id" {
    type           = integer
    null           = true
    auto_increment = true
  }
  column "received_at"         { type = text }
  column "signal"              { type = text }
  column "transport"           { type = text }
  column "payload_protobuf"    { type = blob }
  column "payload_json"        { type = text }
  column "payload_sha256"      { type = text }
  column "payload_size"        { type = integer }
  column "source"              { type = text }
  column "normalizer_version"  { type = integer }
  column "normalization_status" { type = text }
  column "normalization_error" { type = text }

  primary_key {
    columns = [table.otlp_exports.column.id]
  }
  index "otlp_exports_received_at_idx" {
    columns = [table.otlp_exports.column.received_at]
  }
  index "otlp_exports_signal_idx" {
    columns = [table.otlp_exports.column.signal]
  }
  index "otlp_exports_source_idx" {
    columns = [table.otlp_exports.column.source]
  }
}

table "observations" {
  schema = schema.main

  column "id" {
    type           = integer
    null           = true
    auto_increment = true
  }
  column "export_id"           { type = integer }
  column "ordinal"             { type = integer }
  column "signal"              { type = text }
  column "kind"                { type = text }
  column "source"              { type = text }
  column "source_event_name"   { type = text }
  column "occurred_at"         { type = text }
  column "observed_at"         { type = text }
  column "trace_id"            { type = text }
  column "span_id"             { type = text }
  column "parent_span_id"      { type = text }
  column "session_id"          { type = text }
  column "agent_id"            { type = text }
  column "agent_definition"    {
    type    = text
    default = ""
  }
  column "agent_type"          { type = text }
  column "parent_agent_id"     { type = text }
  column "model"               { type = text }
  column "input_tokens"        { type = integer }
  column "output_tokens"       { type = integer }
  column "cache_read_tokens"   { type = integer }
  column "cache_write_tokens"  { type = integer }
  column "reasoning_tokens"    { type = integer }
  column "input_tokens_reported" {
    type = integer
    default = 0
  }
  column "output_tokens_reported" {
    type = integer
    default = 0
  }
  column "cache_read_tokens_reported" {
    type = integer
    default = 0
  }
  column "cache_write_tokens_reported" {
    type = integer
    default = 0
  }
  column "reasoning_tokens_reported" {
    type = integer
    default = 0
  }
  column "payload_json"        { type = text }
  column "attributes_json"     { type = text }
  column "normalizer_version"  { type = integer }

  primary_key {
    columns = [table.observations.column.id]
  }
  foreign_key "observations_export" {
    columns     = [table.observations.column.export_id]
    ref_columns = [table.otlp_exports.column.id]
    on_delete   = CASCADE
  }
  index "observations_export_ordinal_uq" {
    unique  = true
    columns = [table.observations.column.export_id, table.observations.column.ordinal]
  }
  index "observations_occurred_at_idx" {
    columns = [table.observations.column.occurred_at]
  }
  index "observations_trace_id_idx" {
    columns = [table.observations.column.trace_id]
  }
  index "observations_session_id_idx" {
    columns = [table.observations.column.session_id]
  }
  index "observations_source_idx" {
    columns = [table.observations.column.source]
  }
  index "observations_model_idx" {
    columns = [table.observations.column.model]
  }
}

table "plan_usage_snapshots" {
  schema = schema.main

  column "id" {
    type           = integer
    null           = true
    auto_increment = true
  }
  column "source"                  { type = text }
  column "account_id"              { type = text }
  column "plan"                    { type = text }
  column "window_id"               { type = text }
  column "window_duration_minutes" { type = integer }
  column "used_percent"            { type = real }
  column "resets_at" {
    type = text
    null = true
  }
  column "captured_at" { type = text }
  column "authority"   { type = text }
  column "raw_json"    { type = text }

  primary_key {
    columns = [table.plan_usage_snapshots.column.id]
  }
  index "plan_usage_snapshots_identity_uq" {
    unique = true
    columns = [
      table.plan_usage_snapshots.column.source,
      table.plan_usage_snapshots.column.account_id,
      table.plan_usage_snapshots.column.window_id,
      table.plan_usage_snapshots.column.captured_at,
    ]
  }
  index "plan_usage_snapshots_captured_at_idx" {
    columns = [table.plan_usage_snapshots.column.captured_at]
  }
}
