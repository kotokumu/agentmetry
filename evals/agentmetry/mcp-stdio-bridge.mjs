import readline from 'node:readline';

const targetURL = process.argv[2];
if (!targetURL) throw new Error('usage: mcp-stdio-bridge.mjs <streamable-http-mcp-url>');

const input = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
for await (const line of input) {
  if (!line.trim()) continue;
  const request = JSON.parse(line);
  const response = await fetch(targetURL, {
    method: 'POST',
    headers: { 'content-type': 'application/json', accept: 'application/json, text/event-stream' },
    body: JSON.stringify(request),
  });
  const payload = await response.text();
  if (!response.ok) {
    throw new Error(`MCP HTTP ${response.status}: ${payload}`);
  }
  if (request.id === undefined) continue;
  if (!payload) throw new Error(`MCP request ${request.method} returned an empty response`);
  process.stdout.write(`${payload}\n`);
}
