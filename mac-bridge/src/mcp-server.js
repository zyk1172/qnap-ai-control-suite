#!/usr/bin/env node

import { Buffer } from "node:buffer";
import { stdin, stdout, stderr, env } from "node:process";
import readline from "node:readline";

const baseUrl = (env.QACS_BASE_URL || "http://NAS_IP:8756").replace(/\/$/, "");
const token = env.QACS_TOKEN || "";

const tools = [
  tool("nas_health", "Check whether the QNAP AI control agent is reachable."),
  tool("nas_capabilities", "Show allowed roots, allowlisted commands, profile, and confirmation policy."),
  tool("nas_system_overview", "Read host, uptime, uname, and disk overview from QNAP."),
  tool("nas_processes", "Read process list from QNAP through ps -ef."),
  tool("nas_system_thermal", "Read QNAP CPU, system, disk, fan, and hwmon temperature information."),
  tool("nas_audit_tail", "Read recent QNAP AI Control audit log entries.", {
    lines: { type: "number", description: "Number of log lines, 1-500." }
  }),
  tool("nas_file_list", "List files under an allowed NAS path.", {
    path: { type: "string" }
  }, ["path"]),
  tool("nas_file_stat", "Stat a file or directory under an allowed NAS path.", {
    path: { type: "string" }
  }, ["path"]),
  tool("nas_file_read", "Read a file under an allowed NAS path. Returns UTF-8 text when possible.", {
    path: { type: "string" },
    max_bytes: { type: "number" }
  }, ["path"]),
  tool("nas_file_write", "Prepare or dry-run writing a file under an allowed NAS path. Non-dry-run writes require confirmation.", {
    path: { type: "string" },
    content: { type: "string" },
    mode: { type: "string", description: "Octal mode, for example 0644." },
    create_parents: { type: "boolean" },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["path", "content"]),
  tool("nas_command_run", "Prepare or dry-run an allowlisted NAS command without shell expansion. Non-dry-run commands require confirmation.", {
    argv: { type: "array", items: { type: "string" } },
    timeout_sec: { type: "number" },
    stdin: { type: "string" },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["argv"]),
  tool("nas_qpkg_list", "List QNAP QPKG packages through qpkg_cli."),
  tool("nas_qpkg_action", "Start, stop, or restart a QNAP QPKG package. Use nas_qpkg_manage for install/remove/update workflows.", {
    name: { type: "string" },
    action: { type: "string", enum: ["start", "stop", "restart"] },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["name", "action"]),
  tool("nas_qpkg_manage", "Manage QPKG packages: start, stop, restart, enable, disable, status, download, add, install_file, install_url, remove, clean, cancel, update_all. Install/remove/update actions require confirmation.", {
    name: { type: "string" },
    action: { type: "string", enum: ["start", "stop", "restart", "enable", "disable", "status", "download", "add", "install_file", "install_url", "remove", "clean", "cancel", "update_all"] },
    path: { type: "string", description: "QPKG file path for install_file." },
    url: { type: "string", description: "QPKG URL for install_url." },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["action"]),
  tool("nas_docker_info", "Read QNAP Container Station / Docker engine version and runtime information."),
  tool("nas_docker_containers", "List Docker containers through Container Station / docker ps -a."),
  tool("nas_docker_images", "List Docker images through Container Station / docker images."),
  tool("nas_docker_inspect", "Inspect one Docker container or image by name or id.", {
    name: { type: "string", description: "Container/image name or id." }
  }, ["name"]),
  tool("nas_docker_logs", "Read recent Docker container logs with a bounded tail.", {
    name: { type: "string", description: "Container name or id." },
    tail: { type: "number", description: "Number of lines, default 200, max 2000." }
  }, ["name"]),
  tool("nas_docker_action", "Start, stop, restart, pause, or unpause a Docker container.", {
    name: { type: "string", description: "Container name or id." },
    action: { type: "string", enum: ["start", "stop", "restart", "pause", "unpause"] },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["name", "action"]),
  tool("nas_docker_command", "Run an allowlisted Docker subcommand with raw arguments and no shell. Highest-risk subcommands are prepared for confirmation.", {
    subcommand: { type: "string", enum: ["run", "create", "exec", "pull", "push", "build", "images", "ps", "inspect", "logs", "stats", "top", "port", "diff", "start", "stop", "restart", "pause", "unpause", "kill", "rename", "update", "rm", "rmi", "tag", "save", "load", "cp", "commit", "export", "import", "history", "network", "volume", "system", "compose"] },
    args: { type: "array", items: { type: "string" } },
    timeout_sec: { type: "number" },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["subcommand"]),
  tool("nas_docker_run", "Prepare or dry-run docker run with raw docker run arguments. Non-dry-run requires confirmation.", {
    args: { type: "array", items: { type: "string" }, description: "Arguments after docker run, for example ['-d','--name','web','nginx:latest']." },
    timeout_sec: { type: "number" },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["args"]),
  tool("nas_docker_create", "Prepare or dry-run docker create with raw docker create arguments. Non-dry-run requires confirmation.", {
    args: { type: "array", items: { type: "string" } },
    timeout_sec: { type: "number" },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["args"]),
  tool("nas_docker_remove", "Prepare or dry-run docker rm. Non-dry-run requires confirmation.", {
    args: { type: "array", items: { type: "string" }, description: "Arguments after docker rm, for example ['-f','container_name']." },
    timeout_sec: { type: "number" },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["args"]),
  tool("nas_docker_exec", "Run docker exec with raw arguments and no shell wrapping.", {
    args: { type: "array", items: { type: "string" }, description: "Arguments after docker exec." },
    timeout_sec: { type: "number" },
    dry_run: { type: "boolean" }
  }, ["args"]),
  tool("nas_docker_pull", "Pull a Docker image.", {
    args: { type: "array", items: { type: "string" }, description: "Arguments after docker pull, usually ['image:tag']." },
    timeout_sec: { type: "number" },
    dry_run: { type: "boolean" }
  }, ["args"]),
  tool("nas_docker_image_remove", "Prepare or dry-run docker rmi. Non-dry-run requires confirmation.", {
    args: { type: "array", items: { type: "string" } },
    timeout_sec: { type: "number" },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["args"]),
  tool("nas_docker_network", "Manage Docker networks with raw docker network arguments. remove/prune requires confirmation.", {
    args: { type: "array", items: { type: "string" }, description: "Arguments after docker network." },
    timeout_sec: { type: "number" },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["args"]),
  tool("nas_docker_volume", "Manage Docker volumes with raw docker volume arguments. remove/prune requires confirmation.", {
    args: { type: "array", items: { type: "string" }, description: "Arguments after docker volume." },
    timeout_sec: { type: "number" },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["args"]),
  tool("nas_docker_compose", "Run docker compose with raw compose arguments. down/rm requires confirmation.", {
    args: { type: "array", items: { type: "string" }, description: "Arguments after docker compose, for example ['-f','compose.yml','up','-d']." },
    timeout_sec: { type: "number" },
    dry_run: { type: "boolean" },
    reason: { type: "string" }
  }, ["args"]),
  tool("nas_docker_stats", "Read Docker stats with --no-stream by default unless custom args are supplied.", {
    args: { type: "array", items: { type: "string" } },
    timeout_sec: { type: "number" }
  }),
  tool("nas_qnap_getcfg", "Read a QNAP config value with getcfg. File is limited to /etc/config/*.conf.", {
    section: { type: "string" },
    key: { type: "string" },
    file: { type: "string", description: "Defaults to /etc/config/qpkg.conf." }
  }, ["section", "key"]),
  tool("nas_prepare_operation", "Prepare a sensitive operation manually. Use for file_write, command_run, docker_run_create, docker_destroy, or qpkg_install_remove.", {
    operation: { type: "string", enum: ["file_write", "command_run", "docker_run_create", "docker_destroy", "qpkg_install_remove"] },
    arguments: { type: "object" },
    reason: { type: "string" }
  }, ["operation", "arguments"]),
  tool("nas_pending_operations", "List sensitive operations waiting for confirmation."),
  tool("nas_confirm_operation", "Execute a prepared sensitive operation using its confirmation phrase.", {
    id: { type: "string" },
    confirmation_phrase: { type: "string" }
  }, ["id", "confirmation_phrase"])
];

const rl = readline.createInterface({ input: stdin, crlfDelay: Infinity });

rl.on("line", async (line) => {
  if (!line.trim()) return;
  let msg;
  try {
    msg = JSON.parse(line);
    const result = await handleMessage(msg);
    if (msg.id !== undefined) write({ jsonrpc: "2.0", id: msg.id, result });
  } catch (error) {
    const id = msg?.id ?? null;
    write({
      jsonrpc: "2.0",
      id,
      error: { code: -32000, message: error.message || String(error) }
    });
  }
});

async function handleMessage(msg) {
  switch (msg.method) {
    case "initialize":
      return {
        protocolVersion: "2024-11-05",
        capabilities: { tools: {} },
        serverInfo: { name: "qnap-ai-control-mcp", version: "0.3.0" }
      };
    case "notifications/initialized":
      return {};
    case "tools/list":
      return { tools };
    case "tools/call":
      return callTool(msg.params?.name, msg.params?.arguments || {});
    default:
      throw new Error(`unsupported method: ${msg.method}`);
  }
}

async function callTool(name, args) {
  requireToken();
  switch (name) {
    case "nas_health":
      return textResult(await request("GET", "/v1/health"));
    case "nas_capabilities":
      return textResult(await request("GET", "/v1/capabilities"));
    case "nas_system_overview":
      return textResult(await request("GET", "/v1/system/overview"));
    case "nas_processes":
      return textResult(await request("GET", "/v1/system/processes"));
    case "nas_system_thermal":
      return textResult(await request("GET", "/v1/system/thermal"));
    case "nas_audit_tail": {
      const lines = args.lines ? `?lines=${encodeURIComponent(args.lines)}` : "";
      return textResult(await request("GET", `/v1/audit/tail${lines}`));
    }
    case "nas_file_list":
      return textResult(await request("GET", `/v1/files/list?path=${encodeURIComponent(args.path)}`));
    case "nas_file_stat":
      return textResult(await request("GET", `/v1/files/stat?path=${encodeURIComponent(args.path)}`));
    case "nas_file_read": {
      const result = await request("POST", "/v1/files/read", {
        path: args.path,
        max_bytes: args.max_bytes
      });
      const text = Buffer.from(result.content_base64, "base64").toString("utf8");
      return textResult({ ...result, content: text, content_base64: undefined });
    }
    case "nas_file_write": {
      const payload = {
        path: args.path,
        content_base64: Buffer.from(args.content, "utf8").toString("base64"),
        mode: args.mode,
        create_parents: Boolean(args.create_parents),
        dry_run: Boolean(args.dry_run)
      };
      if (payload.dry_run) return textResult(await request("POST", "/v1/files/write", payload));
      return preparedResult(await prepare("file_write", payload, args.reason));
    }
    case "nas_command_run": {
      const payload = {
        argv: args.argv,
        timeout_sec: args.timeout_sec,
        stdin: args.stdin,
        dry_run: Boolean(args.dry_run)
      };
      if (payload.dry_run) return textResult(await request("POST", "/v1/command/run", payload));
      return preparedResult(await prepare("command_run", payload, args.reason));
    }
    case "nas_qpkg_list":
      return textResult(await request("GET", "/v1/qnap/qpkg"));
    case "nas_qpkg_action": {
      const payload = {
        name: args.name,
        action: args.action,
        dry_run: Boolean(args.dry_run)
      };
      return textResult(await request("POST", "/v1/qnap/qpkg/action", payload));
    }
    case "nas_qpkg_manage": {
      const payload = {
        name: args.name,
        action: args.action,
        path: args.path,
        url: args.url,
        dry_run: Boolean(args.dry_run)
      };
      if (!payload.dry_run && isRiskyQpkgManage(payload.action)) {
        return preparedResult(await prepare("qpkg_install_remove", payload, args.reason));
      }
      return textResult(await request("POST", "/v1/qnap/qpkg/manage", payload));
    }
    case "nas_docker_info":
      return textResult(await request("GET", "/v1/docker/info"));
    case "nas_docker_containers":
      return textResult(await request("GET", "/v1/docker/containers"));
    case "nas_docker_images":
      return textResult(await request("GET", "/v1/docker/images"));
    case "nas_docker_inspect":
      return textResult(await request("POST", "/v1/docker/inspect", {
        name: args.name
      }));
    case "nas_docker_logs":
      return textResult(await request("POST", "/v1/docker/logs", {
        name: args.name,
        tail: args.tail
      }));
    case "nas_docker_action": {
      const payload = {
        name: args.name,
        action: args.action,
        dry_run: Boolean(args.dry_run)
      };
      return textResult(await request("POST", "/v1/docker/action", payload));
    }
    case "nas_docker_command":
      return dockerCommandResult(args.subcommand, args.args || [], args.timeout_sec, Boolean(args.dry_run), args.reason);
    case "nas_docker_run":
      return dockerCommandResult("run", args.args || [], args.timeout_sec, Boolean(args.dry_run), args.reason);
    case "nas_docker_create":
      return dockerCommandResult("create", args.args || [], args.timeout_sec, Boolean(args.dry_run), args.reason);
    case "nas_docker_remove":
      return dockerCommandResult("rm", args.args || [], args.timeout_sec, Boolean(args.dry_run), args.reason);
    case "nas_docker_exec":
      return dockerCommandResult("exec", args.args || [], args.timeout_sec, Boolean(args.dry_run), args.reason);
    case "nas_docker_pull":
      return dockerCommandResult("pull", args.args || [], args.timeout_sec, Boolean(args.dry_run), args.reason);
    case "nas_docker_image_remove":
      return dockerCommandResult("rmi", args.args || [], args.timeout_sec, Boolean(args.dry_run), args.reason);
    case "nas_docker_network":
      return dockerCommandResult("network", args.args || [], args.timeout_sec, Boolean(args.dry_run), args.reason);
    case "nas_docker_volume":
      return dockerCommandResult("volume", args.args || [], args.timeout_sec, Boolean(args.dry_run), args.reason);
    case "nas_docker_compose":
      return dockerCommandResult("compose", args.args || [], args.timeout_sec, Boolean(args.dry_run), args.reason);
    case "nas_docker_stats": {
      const statsArgs = args.args && args.args.length ? args.args : ["--no-stream"];
      return dockerCommandResult("stats", statsArgs, args.timeout_sec, false, args.reason);
    }
    case "nas_qnap_getcfg":
      return textResult(await request("POST", "/v1/qnap/getcfg", {
        section: args.section,
        key: args.key,
        file: args.file
      }));
    case "nas_prepare_operation":
      return preparedResult(await prepare(args.operation, args.arguments, args.reason));
    case "nas_pending_operations":
      return textResult(await request("GET", "/v1/operations/pending"));
    case "nas_confirm_operation":
      return textResult(await request("POST", "/v1/operations/confirm", {
        id: args.id,
        confirmation_phrase: args.confirmation_phrase
      }));
    default:
      throw new Error(`unknown tool: ${name}`);
  }
}

async function prepare(operation, argumentsPayload, reason) {
  return request("POST", "/v1/operations/prepare", {
    operation,
    arguments: argumentsPayload,
    reason
  });
}

async function dockerCommandResult(subcommand, args = [], timeout_sec, dry_run = false, reason) {
  const payload = {
    subcommand,
    args,
    timeout_sec,
    dry_run
  };
  if (!dry_run && isRiskyDockerCommand(subcommand, args)) {
    const operation = subcommand === "run" || subcommand === "create" ? "docker_run_create" : "docker_destroy";
    return preparedResult(await prepare(operation, payload, reason));
  }
  return textResult(await request("POST", "/v1/docker/command", payload));
}

function isRiskyDockerCommand(subcommand, args = []) {
  if (subcommand === "run" || subcommand === "create") return true;
  if (subcommand === "rm" || subcommand === "rmi") return true;
  if (subcommand === "system") return args.includes("prune");
  if (subcommand === "volume" || subcommand === "network") {
    return args.includes("rm") || args.includes("prune");
  }
  if (subcommand === "compose") return args.includes("down") || args.includes("rm");
  return false;
}

function isRiskyQpkgManage(action) {
  return ["add", "install_file", "install_url", "remove", "update_all"].includes(action);
}

function preparedResult(operation) {
  return textResult({
    confirmation_required: true,
    instruction: "Review the summary. To execute, call nas_confirm_operation with id and confirmation_phrase exactly as shown.",
    operation
  });
}

async function request(method, path, body) {
  const res = await fetch(`${baseUrl}${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json"
    },
    body: body === undefined ? undefined : JSON.stringify(body)
  });
  const text = await res.text();
  let json;
  try {
    json = text ? JSON.parse(text) : {};
  } catch {
    json = { raw: text };
  }
  if (!res.ok) {
    throw new Error(json.error || `${res.status} ${res.statusText}`);
  }
  return json;
}

function tool(name, description, properties = {}, required = []) {
  return {
    name,
    description,
    inputSchema: {
      type: "object",
      properties,
      required
    }
  };
}

function textResult(value) {
  return {
    content: [
      {
        type: "text",
        text: JSON.stringify(value, null, 2)
      }
    ]
  };
}

function requireToken() {
  if (!token) {
    throw new Error("QACS_TOKEN is required");
  }
}

function write(obj) {
  stdout.write(`${JSON.stringify(obj)}\n`);
}

process.on("uncaughtException", (error) => {
  stderr.write(`${error.stack || error.message}\n`);
});
