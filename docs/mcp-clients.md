# MCP Client 配置

Mac 上安装 Node 20+，然后在 Codex、OpenClaw 或 Hermes 中加入：

```json
{
  "mcpServers": {
    "qnap-ai-control": {
      "command": "node",
      "args": ["/path/to/qnap-ai-control-suite/mac-bridge/src/server.js"],
      "env": {
        "QACS_BASE_URL": "http://NAS_IP:8756",
        "QACS_TOKEN": "REPLACE_WITH_TOKEN"
      }
    }
  }
}
```

不要将真实 `QACS_TOKEN` 放入 Git 配置、截图或公共日志。旧配置中的 `mcp-server.js` 可继续使用。

验证顺序：

1. `nas_health`
2. `nas_discovery`
3. `nas_system_thermal`
4. `nas_docker_containers`
5. `nas_exec`，参数 `{ "argv": ["/bin/df", "-h"] }`

完整 shell pipeline 使用 `nas_shell`，例如 `{ "shell": "df -h | sort" }`。在 `full_trust` 下无需 prepare/confirm；每次调用仍写入 audit log。
