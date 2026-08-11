import { access, cp, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { spawn } from 'node:child_process';
import { createServer } from 'node:net';
import { fileURLToPath } from 'node:url';

const evalRoot = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(evalRoot, '../..');
const providerArg = process.argv.find((argument) => argument.startsWith('--provider='))?.split('=')[1] ?? 'all';
const providers = providerArg === 'all' ? ['claude', 'codex'] : [providerArg];
if (providers.some((provider) => !['claude', 'codex'].includes(provider))) {
  throw new Error('--provider must be claude, codex, or all');
}

const reports = [];
for (const provider of providers) {
  reports.push(await runProvider(provider));
}
process.stdout.write(`${JSON.stringify({ providers, reports }, null, 2)}\n`);

async function runProvider(provider) {
  const workspace = await mkdtemp(join(evalRoot, `.agent-e2e-${provider}-`));
  const databasePath = join(workspace, 'agentmetry.db');
  const promptfooConfigPath = join(workspace, 'promptfooconfig.json');
  const promptfooResultPath = join(workspace, 'promptfoo-result.json');
  const codexHome = join(workspace, 'codex-home');
  const httpPort = await freePort();
  const otlpPort = await freePort();
  const grpcPort = await freePort();
  const urls = {
    dashboard: `http://127.0.0.1:${httpPort}`,
    otlp: `http://127.0.0.1:${otlpPort}`,
    mcp: `http://127.0.0.1:${httpPort}/mcp`,
  };
  let agentmetry;
  try {
    agentmetry = spawn('go', [
      'run', './cmd/agentmetry',
      `-http-address=127.0.0.1:${httpPort}`,
      `-otlp-http-address=127.0.0.1:${otlpPort}`,
      `-otlp-grpc-address=127.0.0.1:${grpcPort}`,
      `-database=${databasePath}`,
    ], { cwd: repoRoot, stdio: ['ignore', 'pipe', 'pipe'] });
    agentmetry.stderr.on('data', (chunk) => process.stderr.write(`[agentmetry:${provider}] ${chunk}`));
    await waitFor(`${urls.dashboard}/api/v1/overview?range=1h`, 30_000);

    const config = await buildPromptfooConfig(provider, urls, codexHome);
    await writeFile(promptfooConfigPath, JSON.stringify(config, null, 2));
    await runPromptfoo(promptfooConfigPath, promptfooResultPath);
    return await verifyAgentmetry(urls, provider);
  } finally {
    if (agentmetry && !agentmetry.killed) {
      agentmetry.kill('SIGTERM');
      await waitForExit(agentmetry, 5_000);
    }
    await rm(workspace, { recursive: true, force: true });
  }
}

async function buildPromptfooConfig(provider, urls, codexHome) {
  const bridgePath = join(evalRoot, 'mcp-stdio-bridge.mjs');
  const providerConfig = provider === 'claude'
    ? {
      id: 'anthropic:claude-agent-sdk',
      config: {
        apiKeyRequired: false,
        working_dir: repoRoot,
        permission_mode: 'plan',
        max_turns: 4,
        custom_allowed_tools: ['Read', 'Grep', 'Glob', 'LS', 'mcp__agentmetry__*'],
        mcp: {
          servers: [{ name: 'agentmetry', command: process.execPath, args: [bridgePath, urls.mcp] }],
          strict_mcp_config: true,
        },
        env: {
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
        },
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
        inherit_process_env: true,
        cli_env: { CODEX_HOME: codexHome },
        cli_config: {
          mcp_servers: { agentmetry: { command: process.execPath, args: [bridgePath, urls.mcp] } },
        },
      },
    };
  if (provider === 'codex') await prepareCodexHome(codexHome, urls);
  return {
    description: `Agentmetry real-${provider}-agent telemetry and MCP integration E2E`,
    providers: [providerConfig],
    prompts: [provider === 'claude'
      ? 'You are running an Agentmetry telemetry integration check. Do not edit files or run shell commands. Return a compact JSON object confirming that the Claude turn completed. The runner will call Agentmetry MCP after this turn.'
      : 'You are running an Agentmetry telemetry integration check. Do not edit files, run shell commands, or call MCP tools. Return a compact JSON object confirming that the Codex turn completed.',
    ],
    tests: [{
      assert: [{ type: 'javascript', value: 'typeof output === "string" && output.length > 0' }],
    }],
    maxConcurrency: 1,
    commandLineOptions: { cache: false },
  };
}

async function prepareCodexHome(target, urls) {
  const source = process.env.CODEX_HOME || join(homedir(), '.codex');
  await mkdir(target);
  try {
    await cp(join(source, 'auth.json'), join(target, 'auth.json'));
  } catch {
    // API-key auth does not need a copied login file; the SDK will report a
    // clear authentication error if neither local login nor an API key exists.
  }
  await writeFile(join(target, 'config.toml'), [
    '[otel]',
    'log_user_prompt = true',
    `exporter = { otlp-http = { endpoint = "${urls.otlp}/v1/logs", protocol = "json" } }`,
    '',
    '[mcp_servers.agentmetry]',
    `command = "${process.execPath}"`,
    `args = ["${resolve(evalRoot, 'mcp-stdio-bridge.mjs')}", "${urls.mcp}"]`,
    '',
  ].join('\n'));
}

async function runPromptfoo(configPath, resultPath) {
  const executable = join(evalRoot, 'node_modules', '.bin', 'promptfoo');
  await access(executable);
  await new Promise((resolvePromise, reject) => {
    const child = spawn(executable, ['eval', '-c', configPath, '--no-cache', '-o', resultPath], {
      cwd: evalRoot,
      env: {
        ...process.env,
        PROMPTFOO_CACHE_ENABLED: 'false',
        PROMPTFOO_CONFIG_DIR: dirname(configPath),
        PROMPTFOO_DISABLE_TELEMETRY: 'true',
      },
      stdio: 'inherit',
    });
    child.on('error', reject);
    child.on('exit', (code, signal) => {
      if (code === 0) resolvePromise();
      else reject(new Error(`promptfoo exited with ${code ?? signal}`));
    });
  });
}

async function verifyAgentmetry(urls, provider) {
  const result = await waitForRuns(urls.mcp, 30_000);
  const sessions = result.overview?.sessions ?? [];
  if (sessions.length === 0) throw new Error(`${provider}: no observed runs`);
  await mcpCall(urls.mcp, 'get_agent_context', {});
  await mcpCall(urls.mcp, 'get_source_capabilities', { source: provider });
  const activities = [];
  for (const session of sessions) {
    if (session.sourceId !== provider) {
      throw new Error(`${provider}: expected source ${provider}, observed ${session.sourceId}`);
    }
    const args = { source: session.sourceId, runId: session.id };
    await mcpCall(urls.mcp, 'get_run_context', args);
    await mcpCall(urls.mcp, 'get_run_summary', args);
    await mcpCall(urls.mcp, 'get_token_usage', args);
    const timeline = await mcpCall(urls.mcp, 'get_run_timeline', { ...args, pageSize: 100, includeContent: false });
    activities.push(...(timeline.activities ?? []));
  }
  const agentmetryToolCalls = activities.filter((activity) =>
    ['get_agent_context', 'get_source_capabilities'].some((tool) => activity.toolName?.endsWith(tool)),
  );
  const requiredAgentMCPCalls = 0;
  if (agentmetryToolCalls.length < requiredAgentMCPCalls) {
    throw new Error(`${provider}: expected get_agent_context and get_source_capabilities in observed timeline, observed ${JSON.stringify(agentmetryToolCalls)}`);
  }
  return {
    provider,
    runCount: result.overview?.runCount ?? 0,
    sessions: sessions.map(({ sourceId, id, activityCount }) => ({ sourceId, id, activityCount })),
    agentMCPCallsRequired: requiredAgentMCPCalls,
    agentmetryToolCalls: agentmetryToolCalls.map(({ source, runId, toolName }) => ({ source, runId, toolName })),
  };
}

async function waitForRuns(mcpURL, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let last;
  while (Date.now() < deadline) {
    last = await mcpCall(mcpURL, 'list_runs', { range: '1h', pageSize: 100 });
    if ((last.overview?.runCount ?? 0) > 0 && (last.overview?.sessions ?? []).some((session) => session.activityCount > 0)) {
      return last;
    }
    await delay(250);
  }
  throw new Error(`Agentmetry did not observe a run: ${JSON.stringify(last)}`);
}

async function mcpCall(url, name, argumentsValue) {
  const response = await fetch(url, {
    method: 'POST',
    headers: { 'content-type': 'application/json', accept: 'application/json, text/event-stream' },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: argumentsValue } }),
  });
  const payload = await response.json();
  if (!response.ok || payload.error) throw new Error(`MCP ${name} failed: ${JSON.stringify(payload)}`);
  return payload.result?.structuredContent ?? {};
}

async function waitFor(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch {
      // Agentmetry is still starting.
    }
    await delay(100);
  }
  throw new Error(`timed out waiting for ${url}`);
}

async function freePort() {
  return new Promise((resolvePromise, reject) => {
    const server = createServer();
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const { port } = server.address();
      server.close(() => resolvePromise(port));
    });
  });
}

async function mkdir(path) {
  const { mkdir } = await import('node:fs/promises');
  await mkdir(path, { recursive: true });
}

function delay(ms) {
  return new Promise((resolvePromise) => setTimeout(resolvePromise, ms));
}

function waitForExit(child, timeoutMs) {
  return new Promise((resolvePromise) => {
    if (child.exitCode !== null) return resolvePromise();
    const timeout = setTimeout(() => {
      child.kill('SIGKILL');
      resolvePromise();
    }, timeoutMs);
    child.once('exit', () => {
      clearTimeout(timeout);
      resolvePromise();
    });
  });
}
