import { access, cp, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { spawn } from 'node:child_process';
import { createServer } from 'node:net';
import { fileURLToPath } from 'node:url';

import { assertRequiredAgentMCPTools, assertRequiredProviderMCPTools, buildPromptfooConfig } from './core.mjs';

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

    if (provider === 'codex') await prepareCodexHome(codexHome, urls);
    const config = buildPromptfooConfig(provider, {
      urls,
      codexHome,
      bridgePath: join(evalRoot, 'mcp-stdio-bridge.mjs'),
      repoRoot,
      nodePath: process.execPath,
    });
    await writeFile(promptfooConfigPath, JSON.stringify(config, null, 2));
    const providerToolCalls = await runPromptfoo(promptfooConfigPath, promptfooResultPath, provider);
    return await verifyAgentmetry(urls, provider, providerToolCalls);
  } finally {
    if (agentmetry && !agentmetry.killed) {
      agentmetry.kill('SIGTERM');
      await waitForExit(agentmetry, 5_000);
    }
    await rm(workspace, { recursive: true, force: true });
  }
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

async function runPromptfoo(configPath, resultPath, provider) {
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
  const report = JSON.parse(await readFile(resultPath, 'utf8'));
  return assertRequiredProviderMCPTools(provider, report);
}

async function verifyAgentmetry(urls, provider, providerToolCalls) {
  const client = await MCPClient.connect(urls.mcp);
  const tools = await client.listTools();
  for (const required of ['get_agent_context', 'get_source_capabilities', 'list_runs']) {
    if (!tools.some((tool) => tool.name === required)) throw new Error(`${provider}: MCP tools/list omitted ${required}`);
  }
  const result = await waitForRuns(client, 30_000);
  const sessions = result.overview?.sessions ?? [];
  if (sessions.length === 0) throw new Error(`${provider}: no observed runs`);
  await client.callTool('get_agent_context', {});
  await client.callTool('get_source_capabilities', { source: provider });
  for (const session of sessions) {
    if (session.sourceId !== provider) {
      throw new Error(`${provider}: expected source ${provider}, observed ${session.sourceId}`);
    }
    const args = { source: session.sourceId, runId: session.id };
    await client.callTool('get_run_context', args);
    await client.callTool('get_run_summary', args);
    await client.callTool('get_token_usage', args);
    const timeline = await client.callTool('get_run_timeline', { ...args, pageSize: 100, includeContent: false });
    if (!Array.isArray(timeline.activities) || timeline.activities.length === 0) {
      throw new Error(`${provider}: run ${session.id} returned no timeline activities`);
    }
  }
  const agentmetryToolCalls = await waitForRequiredAgentMCPCalls(client, provider, sessions, 30_000);
  const requiredAgentMCPCalls = assertRequiredAgentMCPTools(provider, agentmetryToolCalls);
  return {
    provider,
    runCount: result.overview?.runCount ?? 0,
    sessions: sessions.map(({ sourceId, id, activityCount }) => ({ sourceId, id, activityCount })),
    agentMCPCallsRequired: requiredAgentMCPCalls,
    providerToolCalls,
    agentmetryToolCalls: agentmetryToolCalls.map(({ source, runId, toolName }) => ({ source, runId, toolName })),
  };
}

async function waitForRequiredAgentMCPCalls(client, provider, sessions, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let observed = [];
  while (Date.now() < deadline) {
    observed = [];
    for (const session of sessions) {
      const timeline = await client.callTool('get_run_timeline', {
        source: session.sourceId,
        runId: session.id,
        pageSize: 100,
        includeContent: false,
      });
      observed.push(...(timeline.activities ?? []).filter((activity) =>
        ['get_agent_context', 'get_source_capabilities'].some((tool) => activity.toolName?.endsWith(tool)),
      ));
    }
    try {
      assertRequiredAgentMCPTools(provider, observed);
      return observed;
    } catch {
      await delay(250);
    }
  }
  assertRequiredAgentMCPTools(provider, observed);
}

async function waitForRuns(client, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let last;
  while (Date.now() < deadline) {
    last = await client.callTool('list_runs', { range: '1h', pageSize: 100 });
    if ((last.overview?.runCount ?? 0) > 0 && (last.overview?.sessions ?? []).some((session) => session.activityCount > 0)) {
      return last;
    }
    await delay(250);
  }
  throw new Error(`Agentmetry did not observe a run: ${JSON.stringify(last)}`);
}

class MCPClient {
  constructor(url) {
    this.url = url;
    this.nextID = 1;
  }

  static async connect(url) {
    const client = new MCPClient(url);
    await client.request('initialize', {
      protocolVersion: '2025-11-25',
      capabilities: {},
      clientInfo: { name: 'agentmetry-e2e-runner', version: 'v1' },
    });
    await client.notify('notifications/initialized', {});
    return client;
  }

  async listTools() {
    const result = await this.request('tools/list', {});
    return result.tools ?? [];
  }

  async callTool(name, argumentsValue) {
    const result = await this.request('tools/call', { name, arguments: argumentsValue });
    if (result.isError) throw new Error(`MCP ${name} returned a tool error: ${JSON.stringify(result)}`);
    if (result.structuredContent === undefined) throw new Error(`MCP ${name} omitted structuredContent`);
    return result.structuredContent;
  }

  async request(method, params) {
    const id = this.nextID++;
    const payload = await this.post({ jsonrpc: '2.0', id, method, params });
    if (payload.id !== id || payload.error) throw new Error(`MCP ${method} failed: ${JSON.stringify(payload)}`);
    return payload.result ?? {};
  }

  async notify(method, params) {
    await this.post({ jsonrpc: '2.0', method, params }, false);
  }

  async post(message, expectBody = true) {
    const response = await fetch(this.url, {
      method: 'POST',
      headers: { 'content-type': 'application/json', accept: 'application/json, text/event-stream' },
      body: JSON.stringify(message),
    });
    const body = await response.text();
    if (!response.ok) throw new Error(`MCP HTTP ${response.status}: ${body}`);
    if (!expectBody || body.length === 0) return {};
    return JSON.parse(body);
  }
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
