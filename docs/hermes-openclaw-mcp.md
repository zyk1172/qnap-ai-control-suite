# Hermes 与 OpenClaw

两者都使用 `docs/mcp-clients.md` 的同一 stdio server：

```bash
node /path/to/qnap-ai-control-suite/mac-bridge/src/server.js
```

环境变量是 `QACS_BASE_URL` 和 `QACS_TOKEN`。添加后先执行 `nas_health`、`nas_discovery`、`nas_docker_containers`。完整 QNAP 操作使用 `nas_exec`，需要管道时使用 `nas_shell`。
