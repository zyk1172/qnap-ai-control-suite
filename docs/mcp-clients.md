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

`nas_log_tail` 可按 `since` / `until` 传 RFC3339 时间窗口。开启时间窗口时，无法解析时间戳的行会被排除并计数。

对 Virtualization Station、HBS 3、iSCSI/LUN 或证书管理，先调用 `nas_qnap_ecosystem`。只有在 NAS probe 验证并配置 `qnap_adapters` 后，才调用 `nas_vm_action`、`nas_hbs_action`、`nas_iscsi_action` 或 `nas_certificate_action`；先使用 `dry_run: true`。配置格式见 [ecosystem-adapters.md](ecosystem-adapters.md)。
