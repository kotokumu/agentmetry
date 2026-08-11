# MCP Agent User Research

- Status: Research baseline for MCP design
- Date: 2026-08-12
- Audience: Claude Code, Codex, and other AI agents using Agentmetry as a read-only MCP

## Research question

What does an AI agent need to know and provide in order to measure its own
development efficiency and improve a multi-agent organization through
Agentmetry?

The question is about the MCP consumer's usable context, not only about which
fields Claude Code or Codex can emit over OTLP.

## User jobs

1. Identify the current work unit without accidentally selecting a sibling or
   parent agent's run.
2. Inspect the evidence for the current run: elapsed time, activities, agent
   topology, token usage, errors, and delegation.
3. Evaluate whether parallel agents improved throughput or only increased
   coordination and token overhead.
4. Compare comparable runs and produce a next-iteration organization change
   backed by evidence.
5. Understand which conclusions are unavailable because of redaction,
   sampling, missing source events, or partial pagination.

## Current usable information

The current application query layer exposes these source-neutral facts:

| Area | Usable facts | Important limitation |
|---|---|---|
| Run/session | source, session ID, time range, activity count, trace IDs | Session is not necessarily a user goal or complete run |
| Agent | ID, definition/type, parent, model, activity count, typed tokens | Outcome and intent are not guaranteed |
| Activity | kind, tool, target agent, status, timestamps, trace/span/parent IDs | Event availability depends on source and capture settings |
| Trace | participants, root count, missing-parent count, chronological activities | Trace is causal evidence, not a session identity |
| Tokens | input/output/cache/reasoning with missing-vs-zero semantics | Vendor totals may be absent or overlapping; no inference is allowed |
| Content | normalized activity body when captured | It may be absent, redacted, encrypted, or unsafe to return by default |
| Plan usage | latest source-specific account-window snapshots | Not a per-run efficiency metric |

The current application does not yet expose a reliable, source-neutral record
for git diffs, commits, test outcomes, artifact conflicts, explicit task
success criteria, or human quality judgments.

## Hands-on MCP validation

On 2026-08-12, the real `/mcp` HTTP endpoint was exercised against a temporary
SQLite database populated through the OTLP HTTP receivers. The caller executed
`get_agent_context`, `get_source_capabilities`, `list_runs`, then used the
explicit `source` and `runId` returned by discovery to call
`get_run_context`, `get_run_summary`, `get_token_usage`,
`get_run_timeline`, `find_bottlenecks`, `find_coordination_risks`, and
`compare_runs`.

The flow succeeded without repository-internal access. It discovered two
source-qualified runs, returned observed totals of 18 tokens (15 input, 3
output), and identified a 1,000 ms observed response bottleneck in the parent
run. The parent run's derived wall time was 3,000 ms, observed active time was
1,000 ms, and the parallelism factor was 0.333. No explicit coordination risk
was reported because the delegation had a target and no error event was
present.

The exercise also confirmed two important user-facing constraints: caller
identity is unavailable through this stateless MCP request, so the agent must
select a run explicitly; and `list_runs` is discovery-only, so detailed token
and agent data is obtained from the subsequent run-specific tools. The result
metadata reported `sourceCoverage: unknown`, so `complete` must be interpreted
as completeness of the observed activity projection, not proof that all source
events or development outcomes were captured.

## Identity research

The MCP server cannot infer the caller's current Agentmetry run merely from an
MCP connection. A caller must provide or establish a correlation context.

- Claude Code hook events include `session_id`, `agent_type`, and `cwd`; this is
  available to configured hooks, but must not be assumed to be automatically
  present in every MCP tool request.
- Codex App Server exposes thread/session identifiers to clients. A local MCP
  server should not assume that the current thread is injected into its process
  environment, and nested execution can make inherited thread identifiers
  ambiguous.
- Therefore “latest run” is a discovery result, not the caller's identity.
  The MCP contract must require an explicit source-qualified identity for
  self-analysis, or accept a caller-provided correlation token established by
  an integration layer.

References:

- [Claude Code hooks reference](https://code.claude.com/docs/en/hooks)
- [Claude Code session management](https://code.claude.com/docs/en/sessions)
- [Codex App Server protocol](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)
- [Codex thread ID environment discussion](https://github.com/openai/codex/issues/8923)

## Product implications

The first MCP interaction must be a discoverable context contract, not an
analysis guess. It must tell the agent:

- what data domains and fields are available;
- which identifiers are required to select a run;
- whether a result is complete or paged;
- which values are observed, estimated, missing, redacted, or encrypted;
- which analysis rules are heuristic and what evidence they cite;
- which useful development signals are not currently available.

The intended workflow is:

```text
discover usable context
  -> establish source-qualified run context
  -> inspect summary and completeness
  -> retrieve bounded evidence pages
  -> request token/efficiency/coordination analysis
  -> compare only comparable runs
  -> propose an organization change with evidence
```

## Acceptance criteria derived from research

1. An agent can discover the MCP's usable data without reading repository
   documentation.
2. The API never silently treats the latest run as the caller's own run.
3. Every analysis result identifies its run context, rule version, evidence,
   confidence, and source completeness.
4. A missing capability is represented as unavailable/unknown, never as zero.
5. Analysis remains useful without message bodies and does not return bodies by
   default.
6. The contract distinguishes telemetry-observable efficiency from
   development outcome, which requires optional source or project integrations.
