# Agentmetry API contract

## Decision

The read API uses Protocol Buffers as its source of truth. Buf owns linting,
generation, and breaking-change checks. Connect exposes the generated service to
the Web client and other HTTP/gRPC-compatible clients.

The schema lives in
[`proto/agentmetry/v1/agentmetry.proto`](../../proto/agentmetry/v1/agentmetry.proto).

## Resource boundaries

- `GetDashboard` returns dashboard aggregates and bounded recent evidence only.
- `ListSessions` returns bounded session summaries and never operation lists.
- `ListSessions` may return `agent_count` without loading per-agent metadata.
- `GetSession` returns one session summary and agent topology, without operations.
- `ListSessionActivities` returns one bounded, opaque-cursor page of operations.
- `GetTrace` returns trace-scoped evidence and remains separate from sessions.
- `GetTraceOverview` returns at most 5,000 body-free timing/identity rows plus
  stable full-trace total and extent. Coverage is `partial` when the cap is hit.
  Missing-parent checks use all retained native span IDs, including IDs outside
  the returned overview.
- `GetTraceWindow` applies an inclusive interval-overlap, kind, and observed
  error predicate to the full trace before paging. Its trace summary keeps the
  full extent/total while `matching_activities` reports the filtered count. It
  returns ordinary selected activity details and therefore remains separate
  from the body-free overview. Each returned native trace activity carries an
  optional `missing_parent`: present `true` means its parent ID is absent;
  present `false` means its parent is retained or the activity is a root.
  Absence means the endpoint did not assess the relationship, including logs
  and activities returned by older endpoints.
- `CompareRework` compares an explicit baseline/current session pair. One read
  transaction resolves both canonical session roots and loads their timing,
  token totals, full retained diagnostic evidence, coverage, and harness context.
  The response contains five diagnostic metric rows and summaries, without
  prompts, tool bodies, raw attributes, or per-activity evidence.

Default dashboard and session list reads use aggregate projections. Structured
session conditions stream the required activity metadata before pagination;
observed-failure detection may inspect stored attributes, while content/body
columns are not read. Diagnostic calculations may read retained operations
internally; their summary responses do not return operation content.

`ListSessions.conditions` combines observed failure, inclusive minimum/maximum
elapsed milliseconds, exact model, and exact tool with the existing source,
time, and text conditions. Model and tool may match different activities among
the same canonical conversation's members. Each root appears once, and all
conditions apply before pagination. Duration is the earliest activity start to
the latest activity end across those members, not summed activity effort. Missing
or invalid endpoints exclude a conversation from a duration condition. Numeric
bounds must be finite, nonnegative, and ordered; model/tool values are capped at
200 UTF-8 bytes.

A non-default request succeeds as the requested query only when
`applied_conditions` acknowledges its exact validated conditions. Adapters relay
the storage acknowledgement rather than constructing it from the request. Old
servers may ignore additive request fields; clients must treat absent or
mismatched acknowledgement as unsupported. Default requests omit the new field.

## MCP boundary

MCP tools are semantic adapters over the same application query service. RPCs are
not automatically exposed as tools. Each exposed tool has an explicit name,
description, safety/read-only policy, output bound, and pagination policy.

Where useful, MCP input/output JSON Schema may be derived from protobuf
descriptors, but tool metadata and exposure remain explicit.

The read-only `compare_rework` tool requires `baseline` and `current`, each with
`source` and `runId`. It shares the `CompareRework` calculation and returns raw
numeric operands, nullable values, coverage, and harness context. Invalid pairs
(including the same canonical root, different sources, or overlapping periods)
return an eligibility reason and no metric rows. Missing sessions are errors.
Clients may round values for display but must retain the returned precision.

`compare_runs` remains a separate aggregate tool: it accepts 1–10 runs, permits
cross-source or overlapping runs, and preserves its existing dimension defaults.
Neither comparison tool returns captured bodies. Timeline and trace tools retain
their separate content opt-in and page-size limit of 100.

`list_runs` and its `get_agent_sessions` alias accept the same `conditions` and
return `appliedConditions`. These conditions apply to the listed sessions;
dashboard aggregates retain their existing range/source/search scope.

## Compatibility rules

- `page_token` is opaque and must not be parsed by clients.
- `PageInfo.start_offset` is informational for rendering a retained window;
  it is not a cursor and clients still pass `next_page_token` or
  `previous_page_token` unchanged.
- Servers cap `page_size` at 100.
- A trace window supplies both `started_at` and `ended_at` or neither; the
  ordered endpoints are inclusive. An empty kind means all kinds; other values
  use the existing prompt/response/tool/delegation/message/reasoning/unknown
  vocabulary. Unknown outcome does not satisfy `errors_only`.
- New fields must be additive and use fresh field numbers.
- Removed fields require reservation in protobuf.
- Every contract change runs `buf lint` and `buf breaking` against the prior
  schema image in CI.
