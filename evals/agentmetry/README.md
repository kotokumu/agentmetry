# Agentmetry real-agent E2E

This opt-in evaluation starts Agentmetry with a temporary SQLite database,
invokes Claude Agent SDK and/or OpenAI Codex SDK through Promptfoo, and then
uses Agentmetry's MCP endpoint to verify the real agent run and retrieve its
analysis.

It is intentionally separate from `go test -tags=integration ./...`: that
suite uses deterministic OTLP fixtures and never spends model credits.

## Setup

Use Node.js `>=22.22.0`, install the eval-only dependencies, and authenticate
the local SDKs using the normal Claude Code/Codex login or API-key flow:

```sh
npm --prefix evals/agentmetry ci
```

Claude Agent SDK can reuse a local Claude Code session with
`apiKeyRequired: false`. Codex SDK can reuse the existing Codex/ChatGPT login
when `OPENAI_API_KEY` and `CODEX_API_KEY` are unset. The runner copies only the
local Codex `auth.json` into a temporary `CODEX_HOME` and deletes it after the
run.

## Run

The command is deliberately opt-in because it invokes real agents and may
consume subscription or API quota:

```sh
npm --prefix evals/agentmetry run e2e
npm --prefix evals/agentmetry run e2e -- --provider=claude
npm --prefix evals/agentmetry run e2e -- --provider=codex
```

The agents run read-only and are instructed not to edit files. Each agent must
discover and successfully call `get_agent_context` and
`get_source_capabilities`; the run fails if either distinct call is absent or
reported as failed in the observed timeline. The runner then performs
source-qualified analysis after the SDK turn. It establishes the MCP lifecycle
with `initialize`, `notifications/initialized`, and `tools/list` before calling
tools. The fixture-backed Go integration test also exercises that lifecycle
through the official MCP client and verifies the semantic response values.
