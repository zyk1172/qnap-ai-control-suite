import assert from "node:assert/strict";
import { EventEmitter, once } from "node:events";
import { createServer } from "node:http";
import test from "node:test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { CallToolResultSchema, ElicitRequestSchema } from "@modelcontextprotocol/sdk/types.js";

test("MCP cancellation closes the initial HTTP request and the approved retry", async (t) => {
  const events = new EventEmitter();
  const api = createServer(async (req, res) => {
    let body = "";
    for await (const chunk of req) body += chunk;
    const send = (status, data) => {
      res.writeHead(status, { "Content-Type": "application/json" });
      res.end(JSON.stringify(data));
    };
    if (req.url === "/v1/approvals/test-approval/decision") {
      assert.equal(JSON.parse(body).decision, "approve");
      return send(200, { ok: true, data: { state: "approved" } });
    }
    const { argv } = JSON.parse(body);
    const approved = req.headers["x-qacs-approval-id"] === "test-approval";
    if (argv[1] === "after-approval" && !approved) {
      return send(409, { ok: false, error: {
        code: "approval_required",
        details: {
          approval_id: "test-approval",
          operation: "review.fixture",
          risk: "sensitive",
          expires_at: new Date(Date.now() + 60_000).toISOString()
        }
      } });
    }
    events.emit("dispatched", res, approved);
  });
  await new Promise((resolve) => api.listen(0, "127.0.0.1", resolve));
  t.after(async () => {
    api.closeAllConnections();
    await new Promise((resolve) => api.close(resolve));
  });

  const env = {
    QACS_BASE_URL: `http://127.0.0.1:${api.address().port}`,
    QACS_TOKEN: "test-token",
    QACS_TOOLSETS: "raw",
    QACS_HTTP_TIMEOUT_MS: "120000"
  };
  const previous = Object.fromEntries(Object.keys(env).map((key) => [key, process.env[key]]));
  Object.assign(process.env, env);
  t.after(() => {
    for (const [key, value] of Object.entries(previous)) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
  });
  const { createServer: createBridge } = await import("../src/server.js");
  const bridge = createBridge();
  const client = new Client({ name: "cancel-test", version: "1" }, { capabilities: { elicitation: { form: {} } } });
  client.setRequestHandler(ElicitRequestSchema, () => ({ action: "accept", content: {} }));
  const [clientSide, serverSide] = InMemoryTransport.createLinkedPair();
  await bridge.connect(serverSide);
  await client.connect(clientSide);
  t.after(async () => {
    await client.close();
    await bridge.close();
  });

  for (const stage of ["initial", "after-approval"]) {
    const dispatched = once(events, "dispatched", { signal: AbortSignal.timeout(3000) });
    const controller = new AbortController();
    const call = client.request({
      method: "tools/call",
      params: {
        name: "nas_exec",
        arguments: { argv: ["/bin/echo", stage], timeout_sec: 120 }
      }
    }, CallToolResultSchema, { signal: controller.signal, timeout: 5000 });
    const rejected = assert.rejects(call, /cancelled by client/);
    const [response, approved] = await dispatched;
    assert.equal(approved, stage === "after-approval");
    const closed = once(response, "close", { signal: AbortSignal.timeout(3000) });
    controller.abort(new Error("cancelled by client"));
    await Promise.all([rejected, closed]);
    assert.equal(response.writableEnded, false);
  }
});
