import assert from "node:assert/strict";
import { createServer } from "node:http";
import { spawn } from "node:child_process";
import { once } from "node:events";
import test from "node:test";
import { LATEST_PROTOCOL_VERSION } from "@modelcontextprotocol/sdk/types.js";

const bridgeCwd = new URL("..", import.meta.url);

test("sensitive tool uses elicitation, records approval, and retries the exact request once", async () => {
  const fixture = await createFixture();
  try {
    const harness = await startBridge(fixture.baseUrl);
    try {
      await initialize(harness, { elicitation: { form: {} } });
      const call = harness.request("tools/call", { name: "nas_power", arguments: { action: "reboot", idempotency_key: "idem-1" } });
      const elicitation = await harness.waitFor("elicitation/create");
      assert.equal(elicitation.params.mode, "form");
      assert.match(elicitation.params.message, /system\.power/);
      assert.match(elicitation.params.message, /sensitive/i);
      assert.deepEqual(elicitation.params.requestedSchema.properties.decision.enum, ["approve", "deny"]);
      harness.respond(elicitation.id, { action: "accept", content: {} });

      const result = (await call).result;
      assert.equal(result.isError, false);
      assert.deepEqual(result.structuredContent, { action: "reboot", executed: true });
      assert.deepEqual(fixture.records.map((record) => record.url), [
        "/v1/system/power",
        "/v1/approvals/ticket-1/decision",
        "/v1/system/power"
      ]);
      assert.deepEqual(JSON.parse(fixture.records[0].body), { action: "reboot" });
      assert.deepEqual(JSON.parse(fixture.records[2].body), { action: "reboot" });
      assert.deepEqual(JSON.parse(fixture.records[1].body), { decision: "approve" });
      assert.equal(fixture.records[0].headers["x-qacs-approval-id"], undefined);
      assert.equal(fixture.records[2].headers["x-qacs-approval-id"], "ticket-1");
      assert.equal(fixture.records[0].headers["idempotency-key"], "idem-1");
      assert.equal(fixture.records[2].headers["idempotency-key"], "idem-1");
      assert.equal(fixture.records[0].headers["x-request-id"], fixture.records[2].headers["x-request-id"]);
      assert.notEqual(fixture.records[0].headers["x-request-id"], fixture.records[1].headers["x-request-id"]);
    } finally {
      await harness.close();
    }
  } finally {
    await fixture.close();
  }
});

test("elicitation decline records deny and never retries the sensitive request", async () => {
  const fixture = await createFixture();
  try {
    const harness = await startBridge(fixture.baseUrl);
    try {
      await initialize(harness, { elicitation: { form: {} } });
      const call = harness.request("tools/call", { name: "nas_power", arguments: { action: "shutdown" } });
      const elicitation = await harness.waitFor("elicitation/create");
      harness.respond(elicitation.id, { action: "decline" });
      const result = (await call).result;
      assert.equal(result.isError, true, JSON.stringify(result));
      assert.match(result.content[0].text, /denied/i);
      assert.deepEqual(fixture.records.map((record) => record.url), [
        "/v1/system/power",
        "/v1/approvals/ticket-1/decision"
      ]);
      assert.deepEqual(JSON.parse(fixture.records[1].body), { decision: "deny" });
    } finally {
      await harness.close();
    }
  } finally {
    await fixture.close();
  }
});

test("unsupported elicitation fails closed without deciding or retrying", async () => {
  const fixture = await createFixture();
  try {
    const harness = await startBridge(fixture.baseUrl);
    try {
      await initialize(harness, {});
      const result = (await harness.request("tools/call", { name: "nas_power", arguments: { action: "reboot" } })).result;
      assert.equal(result.isError, true, JSON.stringify(result));
      assert.match(result.content[0].text, /current MCP client does not support interactive approval/);
      assert.deepEqual(fixture.records.map((record) => record.url), ["/v1/system/power"]);
    } finally {
      await harness.close();
    }
  } finally {
    await fixture.close();
  }
});

async function createFixture() {
  const records = [];
  let powerAttempts = 0;
  const server = createServer(async (request, response) => {
    const body = await readBody(request);
    records.push({ url: request.url, method: request.method, headers: request.headers, body });
    if (request.url === "/v1/system/power") {
      powerAttempts += 1;
      if (powerAttempts === 1) {
        return sendJson(response, 409, {
          ok: false,
          error: {
            code: "approval_required",
            message: "operation requires one-time approval",
            details: {
              approval_id: "ticket-1",
              request_id: "request-1",
              operation: "system.power",
              risk: "sensitive",
              target: "reboot",
              summary: "sensitive system.power target=reboot",
              state: "pending",
              created_at: "2026-08-24T00:00:00Z",
              expires_at: "2026-08-24T00:10:00Z"
            }
          }
        });
      }
      return sendJson(response, 200, { ok: true, data: { action: "reboot", executed: true } });
    }
    if (request.url === "/v1/approvals/ticket-1/decision") {
      const decision = JSON.parse(body).decision;
      return sendJson(response, 200, { ok: true, data: { state: decision === "approve" ? "approved" : "denied" } });
    }
    return sendJson(response, 404, { ok: false, error: { code: "not_found", message: "not found" } });
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  return {
    baseUrl: `http://127.0.0.1:${address.port}`,
    records,
    close: () => new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))
  };
}

async function startBridge(baseUrl) {
  const child = spawn(process.execPath, ["src/server.js"], {
    cwd: bridgeCwd,
    stdio: ["pipe", "pipe", "pipe"],
    env: { ...process.env, QACS_BASE_URL: baseUrl, QACS_TOKEN: "test-token", QACS_TOOLSETS: "core,admin", QACS_HTTP_TIMEOUT_MS: "1000" }
  });
  let buffer = "";
  let nextID = 1;
  const responses = new Map();
  const notifications = [];
  const waiters = new Map();
  let stderr = "";
  child.stdout.on("data", (chunk) => {
    buffer += chunk;
    let newline;
    while ((newline = buffer.indexOf("\n")) >= 0) {
      const line = buffer.slice(0, newline);
      buffer = buffer.slice(newline + 1);
      if (!line.trim()) continue;
      const message = JSON.parse(line);
      if (message.id !== undefined && responses.has(message.id)) {
        responses.get(message.id).resolve(message);
        responses.delete(message.id);
        continue;
      }
      const waiter = waiters.get(message.method);
      if (waiter) {
        waiters.delete(message.method);
        waiter.resolve(message);
      } else {
        notifications.push(message);
      }
    }
  });
  child.stderr.on("data", (chunk) => { stderr += chunk.toString(); });
  child.once("error", (error) => {
    for (const pending of responses.values()) pending.reject(error);
    for (const pending of waiters.values()) pending.reject(error);
  });
  return {
    request(method, params) {
      const id = nextID++;
      const promise = new Promise((resolve, reject) => responses.set(id, { resolve, reject }));
      child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", id, method, params })}\n`);
      return promise;
    },
    notify(method, params) {
      child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", method, params })}\n`);
    },
    respond(id, result) {
      child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", id, result })}\n`);
    },
    waitFor(method, timeoutMs = 2000) {
      const existingIndex = notifications.findIndex((message) => message.method === method);
      if (existingIndex >= 0) return Promise.resolve(notifications.splice(existingIndex, 1)[0]);
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          waiters.delete(method);
          reject(new Error(`Timed out waiting for ${method}; bridge stderr: ${stderr}`));
        }, timeoutMs);
        waiters.set(method, { resolve: (message) => { clearTimeout(timer); resolve(message); }, reject });
      });
    },
    async close() {
      if (child.exitCode === null) {
        child.kill("SIGTERM");
        await once(child, "exit");
      }
    }
  };
}

async function initialize(harness, capabilities) {
  const response = await harness.request("initialize", {
    protocolVersion: LATEST_PROTOCOL_VERSION,
    capabilities,
    clientInfo: { name: "qacs-approval-test", version: "1" }
  });
  assert.equal(response.error, undefined, JSON.stringify(response));
  harness.notify("notifications/initialized", {});
}

function readBody(request) {
  return new Promise((resolve) => {
    let body = "";
    request.on("data", (chunk) => { body += chunk; });
    request.on("end", () => resolve(body));
  });
}

function sendJson(response, status, payload) {
  response.writeHead(status, { "Content-Type": "application/json" });
  response.end(JSON.stringify(payload));
}
