import assert from 'node:assert/strict';
import test from 'node:test';

import {
  REQUIRED_AGENT_MCP_TOOLS,
  assertRequiredAgentMCPTools,
  assertRequiredProviderMCPTools,
  buildPromptfooConfig,
} from './core.mjs';

const urls = {
  mcp: 'http://127.0.0.1:17890/mcp',
  otlp: 'http://127.0.0.1:4318',
};

test('Claude config exposes and requires the two read-only Agentmetry tools', () => {
  const config = buildPromptfooConfig('claude', {
    urls,
    codexHome: '/tmp/codex-home',
    bridgePath: '/tmp/mcp-stdio-bridge.mjs',
    repoRoot: '/tmp/repo',
    nodePath: '/usr/bin/node',
  });

  const provider = config.providers[0].config;
  assert.equal(provider.strict_mcp_config, true);
  assert.equal(provider.mcp.strict_mcp_config, undefined);
  assert.deepEqual(
    provider.custom_allowed_tools.filter((name) => name.startsWith('mcp__agentmetry__')),
    REQUIRED_AGENT_MCP_TOOLS.map((name) => `mcp__agentmetry__${name}`),
  );
  assert.match(config.prompts[0], /get_agent_context/);
  assert.match(config.prompts[0], /get_source_capabilities/);
  assert.doesNotMatch(config.prompts[0], /do not call MCP/i);
});

test('Codex config uses an allowlisted subprocess environment and requires MCP calls', () => {
  const config = buildPromptfooConfig('codex', {
    urls,
    codexHome: '/tmp/codex-home',
    bridgePath: '/tmp/mcp-stdio-bridge.mjs',
    repoRoot: '/tmp/repo',
    nodePath: '/usr/bin/node',
  });

  const provider = config.providers[0].config;
  assert.equal(provider.inherit_process_env, false);
  assert.deepEqual(provider.cli_env, { CODEX_HOME: '/tmp/codex-home' });
  assert.match(config.prompts[0], /get_agent_context/);
  assert.match(config.prompts[0], /get_source_capabilities/);
});

test('required MCP tool validation requires each distinct successful call', () => {
  const activities = [
    { toolName: 'mcp__agentmetry__get_agent_context', status: 'OK' },
    { toolName: 'mcp__agentmetry__get_source_capabilities', status: 'OK' },
  ];

  assert.deepEqual(assertRequiredAgentMCPTools('codex', activities), REQUIRED_AGENT_MCP_TOOLS);
  assert.throws(
    () => assertRequiredAgentMCPTools('codex', [activities[0], activities[0]]),
    /get_source_capabilities/,
  );
  assert.throws(
    () => assertRequiredAgentMCPTools('codex', [{ ...activities[0], status: 'Error' }, activities[1]]),
    /get_agent_context/,
  );
});

test('Promptfoo provider evidence rejects failed or missing MCP calls', () => {
  const claudeReport = reportWithResponse({
    output: '{}',
    metadata: { toolCalls: [
      { name: 'mcp__agentmetry__get_agent_context', is_error: false },
      { name: 'mcp__agentmetry__get_source_capabilities', is_error: false },
    ] },
  });
  assert.equal(assertRequiredProviderMCPTools('claude', claudeReport).length, 2);
  const incompleteClaude = reportWithResponse({
    output: '{}',
    metadata: { toolCalls: [
      { name: 'mcp__agentmetry__get_agent_context' },
      { name: 'mcp__agentmetry__get_source_capabilities', is_error: false },
    ] },
  });
  assert.throws(() => assertRequiredProviderMCPTools('claude', incompleteClaude), /get_agent_context/);

  const codexReport = reportWithResponse({
    output: '{}',
    raw: JSON.stringify({ items: [
      { type: 'mcp_tool_call', server: 'agentmetry', tool: 'get_agent_context', status: 'completed' },
      { type: 'mcp_tool_call', server: 'agentmetry', tool: 'get_source_capabilities', status: 'completed' },
    ] }),
  });
  assert.equal(assertRequiredProviderMCPTools('codex', codexReport).length, 2);

  const failed = reportWithResponse({
    output: '{}',
    raw: JSON.stringify({ items: [
      { type: 'mcp_tool_call', server: 'agentmetry', tool: 'get_agent_context', status: 'failed' },
      { type: 'mcp_tool_call', server: 'agentmetry', tool: 'get_source_capabilities', status: 'completed' },
    ] }),
  });
  assert.throws(() => assertRequiredProviderMCPTools('codex', failed), /get_agent_context/);
});

function reportWithResponse(response) {
  return { results: { outputs: [{ response }] } };
}
