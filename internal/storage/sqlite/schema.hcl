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
  column "usage_id" {
    type    = text
    default = ""
  }
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
  column "activity_id" {
    type = text
    null = true
  }
  column "projection_sequence" {
    type    = integer
    default = 0
  }

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
  index "spans_source_run_agent_idx" {
    columns = [table.spans.column.source, table.spans.column.run_id, table.spans.column.agent_id]
  }
  index "spans_source_run_usage_idx" {
    columns = [table.spans.column.source, table.spans.column.run_id, table.spans.column.usage_id]
  }
  index "spans_source_run_agent_parent_idx" {
    columns = [table.spans.column.source, table.spans.column.run_id, table.spans.column.agent_id, table.spans.column.parent_span_id, table.spans.column.trace_id, table.spans.column.span_id]
  }
  index "spans_trace_started_at_idx" {
    columns = [table.spans.column.trace_id, table.spans.column.started_at, table.spans.column.ended_at]
  }
  index "spans_trace_parent_idx" {
    columns = [table.spans.column.trace_id, table.spans.column.parent_span_id]
  }
  index "spans_projection_sequence_idx" {
    columns = [table.spans.column.projection_sequence]
  }
  index "spans_activity_id_idx" {
    unique  = true
    columns = [table.spans.column.activity_id]
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
  column "cost_reported" {
    type    = integer
    # -1 marks rows upgraded from a schema that did not track cost presence.
    # Open() replaces the marker once, while new rollups always write 0 or 1.
    default = -1
  }

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

table "session_links" {
  schema = schema.main
  column "source"            { type = text }
  column "parent_session_id" { type = text }
  column "child_session_id"  { type = text }
  column "observed_at"       { type = text }
  primary_key {
    columns = [table.session_links.column.source, table.session_links.column.parent_session_id, table.session_links.column.child_session_id]
  }
  index "session_links_source_child_idx" {
    columns = [table.session_links.column.source, table.session_links.column.child_session_id]
  }
}

table "session_memberships" {
  schema = schema.main
  column "source"            { type = text }
  column "session_id"        { type = text }
  column "root_session_id"   { type = text }
  column "parent_session_id" { type = text }
  primary_key {
    columns = [table.session_memberships.column.source, table.session_memberships.column.session_id]
  }
  index "session_memberships_source_root_idx" {
    columns = [table.session_memberships.column.source, table.session_memberships.column.root_session_id]
  }
}

table "session_agents" {
  schema = schema.main

  column "source"   { type = text }
  column "run_id"   { type = text }
  column "agent_id" { type = text }
  column "agent_definition" {
    type = text
    default = ""
  }
  column "agent_type" {
    type = text
    default = ""
  }
  column "parent_agent_id" {
    type = text
    default = ""
  }
  column "model" {
    type = text
    default = ""
  }
  column "activity_count" {
    type = integer
    default = 0
  }
  column "input_tokens" {
    type = integer
    default = 0
  }
  column "output_tokens" {
    type = integer
    default = 0
  }
  column "cache_read_tokens" {
    type = integer
    default = 0
  }
  column "cache_write_tokens" {
    type = integer
    default = 0
  }
  column "reasoning_tokens" {
    type = integer
    default = 0
  }
  column "input_reported" {
    type = integer
    default = 0
  }
  column "output_reported" {
    type = integer
    default = 0
  }
  column "cache_read_reported" {
    type = integer
    default = 0
  }
  column "cache_write_reported" {
    type = integer
    default = 0
  }
  column "reasoning_reported" {
    type = integer
    default = 0
  }

  primary_key {
    columns = [table.session_agents.column.source, table.session_agents.column.run_id, table.session_agents.column.agent_id]
  }
}

table "session_traces" {
  schema = schema.main

  column "source" { type = text }
  column "run_id" { type = text }
  column "trace_id" { type = text }

  primary_key {
    columns = [table.session_traces.column.source, table.session_traces.column.run_id, table.session_traces.column.trace_id]
  }
}

table "trace_rollups" {
  schema = schema.main

  column "trace_id" { type = text }
  column "started_at" { type = text }
  column "ended_at" { type = text }
  column "status_rank" { type = integer }
  column "activity_count" { type = integer }
  column "root_span_count" { type = integer }
  column "missing_parent_count" { type = integer }

  primary_key {
    columns = [table.trace_rollups.column.trace_id]
  }
}

table "trace_conversations" {
  schema = schema.main

  column "trace_id" { type = text }
  column "source" { type = text }
  column "run_id" { type = text }

  primary_key {
    columns = [table.trace_conversations.column.trace_id, table.trace_conversations.column.source, table.trace_conversations.column.run_id]
  }
}

table "trace_agents" {
  schema = schema.main

  column "trace_id" { type = text }
  column "source" { type = text }
  column "run_id" { type = text }
  column "agent_id" { type = text }
  column "agent_definition" {
    type = text
    default = ""
  }
  column "agent_type" {
    type = text
    default = ""
  }
  column "parent_agent_id" {
    type = text
    default = ""
  }
  column "model" {
    type = text
    default = ""
  }

  primary_key {
    columns = [table.trace_agents.column.trace_id, table.trace_agents.column.source, table.trace_agents.column.run_id, table.trace_agents.column.agent_id]
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
  column "usage_id" {
    type    = text
    default = ""
  }
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
  column "activity_id" {
    type = text
    null = true
  }
  column "projection_sequence" {
    type    = integer
    default = 0
  }

  primary_key {
    columns = [table.logs.column.id]
  }
  index "logs_observed_at_idx" {
    columns = [table.logs.column.observed_at]
  }
  index "logs_source_run_observed_at_idx" {
    columns = [table.logs.column.source, table.logs.column.run_id, table.logs.column.observed_at]
  }
  index "logs_source_run_agent_idx" {
    columns = [table.logs.column.source, table.logs.column.run_id, table.logs.column.agent_id]
  }
  index "logs_source_run_usage_idx" {
    columns = [table.logs.column.source, table.logs.column.run_id, table.logs.column.usage_id]
  }
  index "logs_trace_observed_at_idx" {
    columns = [table.logs.column.trace_id, table.logs.column.observed_at]
  }
  index "logs_projection_sequence_idx" {
    columns = [table.logs.column.projection_sequence]
  }
  index "logs_activity_id_idx" {
    unique  = true
    columns = [table.logs.column.activity_id]
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
  column "projection_sequence" {
    type    = integer
    default = 0
  }

  primary_key {
    columns = [table.metrics.column.id]
  }
  index "metrics_observed_at_idx" {
    columns = [table.metrics.column.observed_at]
  }
}

table "projection_feed_state" {
  schema = schema.main

  column "id"         { type = integer }
  column "generation" { type = text }

  primary_key {
    columns = [table.projection_feed_state.column.id]
  }
}

table "projection_changes" {
  schema = schema.main

  column "sequence" {
    type           = integer
    null           = true
    auto_increment = true
  }
  column "committed_at" { type = text }
  column "targets_json" { type = text }
  column "target_bytes" { type = integer }
  column "mutation_bytes" {
    type    = integer
    default = 0
  }

  primary_key {
    columns = [table.projection_changes.column.sequence]
  }
  index "projection_changes_committed_at_idx" {
    columns = [table.projection_changes.column.committed_at]
  }
}

table "activity_changes" {
  schema = schema.main

  column "id" {
    type           = integer
    null           = true
    auto_increment = true
  }
  column "sequence"    { type = integer }
  column "scope_kind"  { type = text }
  column "source"      { type = text }
  column "scope_id"    { type = text }
  column "activity_id" { type = text }
  column "operation"   { type = text }

  primary_key {
    columns = [table.activity_changes.column.id]
  }
  foreign_key "activity_changes_projection" {
    columns     = [table.activity_changes.column.sequence]
    ref_columns = [table.projection_changes.column.sequence]
    on_delete   = CASCADE
  }
  index "activity_changes_scope_sequence_idx" {
    columns = [
      table.activity_changes.column.scope_kind,
      table.activity_changes.column.source,
      table.activity_changes.column.scope_id,
      table.activity_changes.column.sequence,
    ]
  }
  index "activity_changes_sequence_idx" {
    columns = [table.activity_changes.column.sequence]
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
  column "payload_codec" {
    type    = text
    default = "identity"
  }
  column "payload_sha256"      { type = text }
  column "payload_size"        { type = integer }
  column "source"              { type = text }
  column "normalizer_version"  { type = integer }
  column "normalization_status" { type = text }
  column "normalization_error" { type = text }
  column "harness_receipt_state" {
    type    = text
    default = "unreported"
  }
  column "harness_scope" {
    type    = text
    default = ""
  }
  column "harness_fingerprint" {
    type    = text
    default = ""
  }
  column "harness_label" {
    type    = text
    default = ""
  }

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
  index "observations_harness_evidence_idx" {
    columns = [
      table.observations.column.source,
      table.observations.column.session_id,
      table.observations.column.kind,
      table.observations.column.signal,
      table.observations.column.export_id,
    ]
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
