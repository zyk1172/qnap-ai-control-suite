import assert from "node:assert/strict";
import { createServer } from "node:http";
import test from "node:test";

test("HTTP requests use bounded defaults, long-job startup timeout, and abort", async () => {
  const server = createServer((_request, response) => {
    setTimeout(() => {
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ ok: true, data: {} }));
    }, 100);
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  const previous = {
    baseUrl: process.env.QACS_BASE_URL,
    token: process.env.QACS_TOKEN,
    timeout: process.env.QACS_HTTP_TIMEOUT_MS
  };
  process.env.QACS_BASE_URL = `http://127.0.0.1:${address.port}`;
  process.env.QACS_TOKEN = "test-token";
  delete process.env.QACS_HTTP_TIMEOUT_MS;
  const client = await import(`../src/client.js?timeout-test=${Date.now()}`);
  try {
    assert.equal(client.resolveTimeoutMs("GET", "/v1/health"), 30_000);
    assert.equal(client.resolveTimeoutMs("POST", "/v1/jobs", {}), 90_000);
    process.env.QACS_HTTP_TIMEOUT_MS = "20";
    assert.equal(client.resolveTimeoutMs("GET", "/v1/health"), 20);
    await assert.rejects(() => client.request("GET", "/slow"), (error) => error.code === "qacs_http_timeout" && error.timeoutMs === 20);
  } finally {
    if (previous.baseUrl === undefined) delete process.env.QACS_BASE_URL;
    else process.env.QACS_BASE_URL = previous.baseUrl;
    if (previous.token === undefined) delete process.env.QACS_TOKEN;
    else process.env.QACS_TOKEN = previous.token;
    if (previous.timeout === undefined) delete process.env.QACS_HTTP_TIMEOUT_MS;
    else process.env.QACS_HTTP_TIMEOUT_MS = previous.timeout;
    await new Promise((resolve) => server.close(resolve));
  }
});
