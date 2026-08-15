# Hermes 与 OpenClaw

两者都使用 `docs/mcp-clients.md` 的同一 stdio server：

```bash
node /path/to/qnap-ai-control-suite/mac-bridge/src/server.js
```

环境变量是 `QACS_BASE_URL` 和 `QACS_TOKEN`。添加后先执行 `nas_health`、`nas_discovery`、`nas_docker_containers`。完整 QNAP 操作使用 `nas_exec`，需要管道时使用 `nas_shell`。

已安装过的 Hermes/OpenClaw 不需要重写 MCP 配置。升级步骤：

1. 更新本地 `qnap-ai-control-suite` 到 `release/v1.0.16-test-report-fixes`，并在 `mac-bridge` 下执行 `npm install`。
2. 确保 MCP 命令仍指向更新后的 `mac-bridge/src/server.js`。
3. 重启 Hermes/OpenClaw 或重启对应 MCP 子进程。
4. 调用 `nas_health`，确认 `"version":"1.0.16"`。

详见 [docs/mcp-clients.md](mcp-clients.md) 的“更新已安装的客户端”。
