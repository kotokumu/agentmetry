export const REQUIRED_AGENT_MCP_TOOLS = ['get_agent_context', 'get_source_capabilities'];

export function buildPromptfooConfig(provider, { urls, codexHome, bridgePath, repoRoot, nodePath }) {
  const requiredToolNames = REQUIRED_AGENT_MCP_TOOLS.map((name) => `mcp__agentmetry__${name}`);
  const providerConfig = provider === 'claude'
    ? {
      id: 'anthropic:claude-agent-sdk',
      config: {
        apiKeyRequired: false,
        working_dir: repoRoot,
        permission_mode: 'plan',
        max_turns: 4,
        custom_allowed_tools: ['Read', 'Grep', 'Glob', 'LS', ...requiredToolNames],
        mcp: {
          servers: [{ name: 'agentmetry', command: nodePath, args: [bridgePath, urls.mcp] }],
        },
        strict_mcp_config: true,
        env: claudeTelemetryEnvironment(urls),
      },
    }
    : {
      id: 'openai:codex-sdk',
      config: {
        working_dir: repoRoot,
        sandbox_mode: 'read-only',
        approval_policy: 'never',
        enable_streaming: true,
        deep_tracing: true,
        inherit_process_env: false,
        cli_env: { CODEX_HOME: codexHome },
        cli_config: {
          mcp_servers: { agentmetry: { command: nodePath, args: [bridgePath, urls.mcp] } },
        },
      },
    };
  return {
    description: `Agentmetry real-${provider}-agent telemetry and MCP integration E2E`,
    providers: [providerConfig],
    prompts: [
      `You are running an Agentmetry ${provider} integration check. `
      + 'Do not edit files or run shell commands. '
      + 'You must call the Agentmetry MCP tools get_agent_context and get_source_capabilities exactly once each. '
      + `For get_source_capabilities, set source to ${provider}. `
      + 'After both calls succeed, return a compact JSON object confirming completion.',
    ],
    tests: [{
      assert: [{ type: 'javascript', value: 'typeof output === "string" && output.length > 0' }],
    }],
    maxConcurrency: 1,
    commandLineOptions: { cache: false },
  };
}

export function assertRequiredAgentMCPTools(provider, activities) {
  const successful = new Set();
  for (const activity of activities) {
    const tool = REQUIRED_AGENT_MCP_TOOLS.find((name) => activity.toolName?.endsWith(name));
    if (tool && !isFailedActivity(activity)) successful.add(tool);
  }
  const missing = REQUIRED_AGENT_MCP_TOOLS.filter((tool) => !successful.has(tool));
  if (missing.length > 0) {
    throw new Error(`${provider}: agent did not successfully call required Agentmetry MCP tools: ${missing.join(', ')}; observed ${JSON.stringify(activities)}`);
  }
  return REQUIRED_AGENT_MCP_TOOLS.filter((tool) => successful.has(tool));
}

export function assertRequiredProviderMCPTools(provider, report) {
  const calls = provider === 'claude' ? claudeToolCalls(report) : codexToolCalls(report);
  const successful = new Set();
  for (const call of calls) {
    const tool = REQUIRED_AGENT_MCP_TOOLS.find((name) => call.name?.endsWith(name));
    if (tool && !call.failed) successful.add(tool);
  }
  const missing = REQUIRED_AGENT_MCP_TOOLS.filter((tool) => !successful.has(tool));
  if (missing.length > 0) {
    throw new Error(`${provider}: Promptfoo did not report successful required MCP tools: ${missing.join(', ')}; observed ${JSON.stringify(calls)}`);
  }
  return calls;
}

function isFailedActivity(activity) {
  const status = `${activity.status ?? ''} ${activity.name ?? ''}`.toLowerCase();
  return ['error', 'failed', 'failure', 'cancelled', 'canceled'].some((marker) => status.includes(marker));
}

function claudeToolCalls(report) {
  return providerResponses(report).flatMap((response) => response.metadata?.toolCalls ?? []).map((call) => ({
    name: call.name,
    failed: call.is_error !== false,
  }));
}

function codexToolCalls(report) {
  return providerResponses(report).flatMap((response) => {
    if (typeof response.raw !== 'string') return [];
    let turn;
    try {
      turn = JSON.parse(response.raw);
    } catch {
      return [];
    }
    return (turn.items ?? []).filter((item) => item.type === 'mcp_tool_call' && item.server === 'agentmetry').map((item) => ({
      name: item.tool,
      failed: Boolean(item.error) || item.status !== 'completed',
    }));
  });
}

function providerResponses(value) {
  const responses = [];
  const seen = new Set();
  const visit = (candidate) => {
    if (!candidate || typeof candidate !== 'object' || seen.has(candidate)) return;
    seen.add(candidate);
    if ('output' in candidate && ('metadata' in candidate || 'raw' in candidate)) responses.push(candidate);
    if (Array.isArray(candidate)) {
      candidate.forEach(visit);
    } else {
      Object.values(candidate).forEach(visit);
    }
  };
  visit(value);
  return responses;
}

function claudeTelemetryEnvironment(urls) {
  return {
    CLAUDE_CODE_ENABLE_TELEMETRY: '1',
    OTEL_TRACES_EXPORTER: 'otlp',
    OTEL_LOGS_EXPORTER: 'otlp',
    OTEL_METRICS_EXPORTER: 'otlp',
    OTEL_EXPORTER_OTLP_ENDPOINT: urls.otlp,
    OTEL_EXPORTER_OTLP_TRACES_ENDPOINT: `${urls.otlp}/v1/traces`,
    OTEL_EXPORTER_OTLP_LOGS_ENDPOINT: `${urls.otlp}/v1/logs`,
    OTEL_EXPORTER_OTLP_METRICS_ENDPOINT: `${urls.otlp}/v1/metrics`,
    OTEL_EXPORTER_OTLP_PROTOCOL: 'http/protobuf',
    OTEL_EXPORTER_OTLP_TRACES_PROTOCOL: 'http/protobuf',
    OTEL_EXPORTER_OTLP_LOGS_PROTOCOL: 'http/json',
    OTEL_EXPORTER_OTLP_METRICS_PROTOCOL: 'http/protobuf',
    OTEL_RESOURCE_ATTRIBUTES: 'service.name=claude-code',
    OTEL_LOG_USER_PROMPTS: '1',
    OTEL_LOG_ASSISTANT_RESPONSES: '1',
    OTEL_LOG_TOOL_DETAILS: '1',
  };
}
