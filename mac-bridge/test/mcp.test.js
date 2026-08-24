import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import test from "node:test";
import { LATEST_PROTOCOL_VERSION } from "@modelcontextprotocol/sdk/types.js";

async function listTools(env = {}, protocolVersion = LATEST_PROTOCOL_VERSION) {
  const child = spawn(process.execPath, ["src/server.js"], { cwd: new URL("..", import.meta.url), stdio: ["pipe", "pipe", "pipe"], env: { ...process.env, ...env } });
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
  child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion, capabilities: {}, clientInfo: { name: "test", version: "1" } } })}\n`);
  child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/list", params: {} })}\n`);
  await responses;
  child.kill("SIGTERM");
  await once(child, "exit");
  const messages = lines.slice(0, 2).map(JSON.parse);
  return { child, messages };
}

test("negotiates supported 2025 protocol and falls back for an unknown future version", async () => {
  const supported = await listTools({}, "2025-03-26");
  assert.equal(supported.messages[0].result.protocolVersion, "2025-03-26");
  const future = await listTools({}, "2026-07-28");
  assert.equal(future.messages[0].result.protocolVersion, LATEST_PROTOCOL_VERSION);
});

test("default MCP toolset starts from a Unicode path and exposes core only", async () => {
  const { messages } = await listTools();
  assert.equal(messages[0].result.serverInfo.version, "2.1.0");
  const names = messages[1].result.tools.map((tool) => tool.name);
  assert.ok(names.includes("nas_health"));
  assert.ok(names.includes("nas_capabilities"));
  assert.ok(names.includes("nas_status_snapshot"));
  assert.ok(!names.includes("nas_approval_decide"));
  assert.ok(names.includes("nas_job_start"));
  assert.ok(!names.includes("nas_exec"));
  assert.ok(!names.includes("nas_file_read"));
  assert.ok(!names.includes("nas_docker_command"));
  assert.ok(!names.includes("nas_qpkg_manage"));
  assert.ok(!names.includes("nas_disks"));
  assert.ok(!names.includes("nas_qnap_ecosystem"));
  assert.ok(!names.includes("nas_firmware_action"));
  assert.ok(!names.includes("nas_system_overview"));
  assert.ok(messages[1].result.tools.every((tool) => tool.outputSchema));
});

test("optional toolsets and compatibility aliases are opt-in", async () => {
  const { messages } = await listTools({ QACS_TOOLSETS: "core,raw,files,docker,storage,network,qnap,admin,compat" });
  const names = messages[1].result.tools.map((tool) => tool.name);
  assert.ok(names.includes("nas_exec"));
  assert.ok(names.includes("nas_file_read"));
  assert.ok(names.includes("nas_file_read_text"));
  assert.ok(names.includes("nas_file_grep"));
  assert.ok(names.includes("nas_docker_command"));
  assert.ok(names.includes("nas_docker_health"));
  assert.ok(names.includes("nas_docker_compose_projects"));
  assert.ok(names.includes("nas_qpkg_manage"));
  assert.ok(names.includes("nas_disks"));
  assert.ok(names.includes("nas_disk_io"));
  assert.ok(names.includes("nas_raid_manage"));
  assert.ok(names.includes("nas_job_start"));
  assert.ok(names.includes("nas_qnap_probe"));
  assert.ok(names.includes("nas_virtual_switch_action"));
  assert.ok(names.includes("nas_system_config_action"));
  assert.ok(names.includes("nas_firmware_action"));
  assert.ok(names.includes("nas_notification_action"));
  assert.ok(names.includes("nas_storage_manager_action"));
  assert.ok(names.includes("nas_users"));
  assert.ok(names.includes("nas_log_tail"));
  assert.ok(names.includes("nas_network_manage"));
  assert.ok(names.includes("nas_network_ipv6_routes"));
  assert.ok(names.includes("nas_smb_status"));
  assert.ok(names.includes("nas_ups"));
  assert.ok(names.includes("nas_job_get"));
  assert.ok(names.includes("nas_system_overview"));
  assert.ok(names.includes("nas_command_run"));
  assert.ok(!names.includes("nas_approval_decide"));
});
