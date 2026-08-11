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

The server must not load all session operations to construct dashboard or session
list responses. Operation pages are the only API surface that loads operation
content for the Web UI.

## MCP boundary

MCP tools are semantic adapters over the same application query service. RPCs are
not automatically exposed as tools. Each exposed tool has an explicit name,
description, safety/read-only policy, output bound, and pagination policy.

Where useful, MCP input/output JSON Schema may be derived from protobuf
descriptors, but tool metadata and exposure remain explicit.

## Compatibility rules

- `page_token` is opaque and must not be parsed by clients.
- `PageInfo.start_offset` is informational for rendering a retained window;
  it is not a cursor and clients still pass `next_page_token` or
  `previous_page_token` unchanged.
- Servers cap `page_size` at 100.
- New fields must be additive and use fresh field numbers.
- Removed fields require reservation in protobuf.
- Every contract change runs `buf lint` and `buf breaking` against the prior
  schema image in CI.
