import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import test from "node:test";
import { LATEST_PROTOCOL_VERSION } from "@modelcontextprotocol/sdk/types.js";

test("official MCP server negotiates and lists v1 tools", async () => {
  const child = spawn(process.execPath, ["src/server.js"], { cwd: new URL("..", import.meta.url), stdio: ["pipe", "pipe", "pipe"] });
  const lines = [];
  let buffered = "";
  const responses = new Promise((resolve, reject) => {
    child.stdout.on("data", (chunk) => {
      buffered += chunk;
      let newline;
      while ((newline = buffered.indexOf("\n")) >= 0) {
        lines.push(buffered.slice(0, newline));
        buffered = buffered.slice(newline + 1);
      }
      if (lines.length >= 2) resolve();
    });
    child.once("error", reject);
  });
  child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: LATEST_PROTOCOL_VERSION, capabilities: {}, clientInfo: { name: "test", version: "1" } } })}\n`);
  child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/list", params: {} })}\n`);
  await responses;
  child.kill("SIGTERM");
  await once(child, "exit");
  const messages = lines.slice(0, 2).map(JSON.parse);
  assert.equal(messages[0].result.serverInfo.version, "1.0.5");
  const names = messages[1].result.tools.map((tool) => tool.name);
  assert.ok(names.includes("nas_exec"));
  assert.ok(names.includes("nas_file_read"));
  assert.ok(names.includes("nas_docker_command"));
  assert.ok(names.includes("nas_qpkg_manage"));
  assert.ok(names.includes("nas_disks"));
  assert.ok(names.includes("nas_users"));
  assert.ok(names.includes("nas_log_tail"));
  assert.ok(names.includes("nas_network_manage"));
  assert.ok(names.includes("nas_ups"));
  assert.ok(names.includes("nas_job_get"));
  assert.ok(messages[1].result.tools.every((tool) => tool.outputSchema));
});
