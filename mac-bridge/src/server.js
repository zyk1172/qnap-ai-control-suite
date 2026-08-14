#!/usr/bin/env node
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { registerSystemTools } from "./tools/system.js";
import { registerFileTools } from "./tools/files.js";
import { registerDockerTools } from "./tools/docker.js";
import { registerQNAPTools } from "./tools/qnap.js";
import { registerJobTools } from "./tools/jobs.js";
import { version } from "./version.js";

export function createServer() {
  const server = new McpServer({ name: "qnap-ai-control-mcp", version }, { capabilities: { logging: {} } });
  registerSystemTools(server); registerFileTools(server); registerDockerTools(server); registerQNAPTools(server); registerJobTools(server);
  return server;
}
export async function main() { const server = createServer(); await server.connect(new StdioServerTransport()); }
if (import.meta.url === `file://${process.argv[1]}`) main().catch((error) => { console.error(error.stack || error.message); process.exitCode = 1; });
