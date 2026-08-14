#!/usr/bin/env node
// Backward-compatible entrypoint for existing Codex, Hermes, and OpenClaw configs.
import { main } from "./server.js";

main().catch((error) => {
  console.error(error.stack || error.message);
  process.exitCode = 1;
});
