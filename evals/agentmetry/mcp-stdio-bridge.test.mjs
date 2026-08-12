import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import readline from 'node:readline';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const evalRoot = dirname(fileURLToPath(import.meta.url));

test('stdio bridge forwards initialize, tools/list, and tools/call including request id zero', async (t) => {
  const methods = [];
  const server = createServer(async (request, response) => {
    let body = '';
    for await (const chunk of request) body += chunk;
    const message = JSON.parse(body);
    methods.push(message.method);
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({ jsonrpc: '2.0', id: message.id, result: resultFor(message.method) }));
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  t.after(() => server.close());

  const { port } = server.address();
  const child = spawn(process.execPath, [join(evalRoot, 'mcp-stdio-bridge.mjs'), `http://127.0.0.1:${port}`], {
    stdio: ['pipe', 'pipe', 'pipe'],
  });
  t.after(() => child.kill('SIGTERM'));
  const output = readline.createInterface({ input: child.stdout, crlfDelay: Infinity });
  const lines = output[Symbol.asyncIterator]();

  child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id: 0, method: 'initialize', params: {} })}\n`);
  assert.equal((await nextLine(lines)).id, 0);
  child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/list', params: {} })}\n`);
  assert.equal((await nextLine(lines)).id, 1);
  child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'get_agent_context', arguments: {} } })}\n`);
  assert.equal((await nextLine(lines)).id, 2);

  assert.deepEqual(methods, ['initialize', 'tools/list', 'tools/call']);
});

function resultFor(method) {
  if (method === 'initialize') return { protocolVersion: '2025-11-25', capabilities: {}, serverInfo: { name: 'test', version: '1' } };
  if (method === 'tools/list') return { tools: [{ name: 'get_agent_context', inputSchema: { type: 'object' } }] };
  return { content: [{ type: 'text', text: '{}' }], structuredContent: {} };
}

async function nextLine(iterator) {
  const result = await Promise.race([
    iterator.next(),
    new Promise((_, reject) => setTimeout(() => reject(new Error('timed out waiting for bridge response')), 2_000)),
  ]);
  assert.equal(result.done, false);
  return JSON.parse(result.value);
}
