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

## 更新已安装的客户端

如果某个智能体已经安装过本 MCP，NAS 更新后不需要改 MCP 协议配置，但需要让该智能体使用当前版本的 Mac bridge：

1. 更新 Mac 上的仓库到 v1.0.16 发布分支：

   ```bash
   cd /path/to/qnap-ai-control-suite
   git fetch origin
   git checkout release/v1.0.16-test-report-fixes
   git pull
   cd mac-bridge
   npm install
   ```

2. 确认该智能体的 MCP 配置仍指向同一目录下的 `mac-bridge/src/server.js` 或 `mac-bridge/src/mcp-server.js`，不要继续指向旧目录里复制出来的旧 bridge。

3. 重启智能体或重启其 MCP 子进程。MCP 的 `tools/list` 在进程启动时加载，不重启不会拿到 v1.0.16 的新工具和修复。

4. 验证顺序：

   1. `nas_health`，确认返回 `"version":"1.0.16"`。
   2. `nas_process_list`，确认不再返回空数组。
   3. `nas_service_list`，确认返回 QNAP QPKG 服务列表。
   4. `nas_acl_get`，确认能读取 ACL 或返回 stat fallback。
   5. `nas_qnap_ecosystem`，确认 UPS reason 与状态一致。

如果 `QACS_BASE_URL` 或 `QACS_TOKEN` 没有变化，不需要修改。NAS 上的 Token 改变时，才需要同步更新每个客户端的 `QACS_TOKEN`。

验证顺序：

1. `nas_health`
2. `nas_discovery`
3. `nas_system_thermal`
4. `nas_docker_containers`
5. `nas_exec`，参数 `{ "argv": ["/bin/df", "-h"] }`

完整 shell pipeline 使用 `nas_shell`，例如 `{ "shell": "df -h | sort" }`。在 `full_trust` 下无需 prepare/confirm；每次调用仍写入 audit log。

## 长任务与 Job

对 docker build、compose pull、备份、归档、SMART long test 或需要较长时间的 QNAP 命令，使用 `nas_job_start`，而不是让 MCP 单次调用等待到超时。

argv 任务示例：

```json
{
  "kind": "compose.pull",
  "command": {
    "argv": ["/bin/sh", "-c", "docker compose pull"],
    "cwd": "/share/Container/moviepilot",
    "timeout_sec": 1800
  }
}
```

显式 shell 与脚本示例：

```json
{
  "kind": "storage.check",
  "shell": "/bin/sh",
  "script": "cat /proc/mdstat; df -h",
  "command": {"timeout_sec": 60}
}
```

启动响应给出 `id` 后，按以下顺序跟踪：

1. `nas_job_get`，传入 `id`，读取状态、退出码和结果。
2. `nas_job_logs`，传入 `id`、可选 `cursor` 和 `limit`，从 `next_cursor` 继续读取下一页。
3. 必要时调用 `nas_job_cancel`。取消请求会向该 Job 的 context 发信号。

Job metadata 不内嵌日志；日志保存最多 1,000 条且最多 16 MiB，响应会标记 `logs_truncated`。这不影响命令本身的 stdout/stderr 结果和审计记录。

`nas_log_tail` 可按 `since` / `until` 传 RFC3339 时间窗口。开启时间窗口时，无法解析时间戳的行会被排除并计数。

对 Virtualization Station、HBS 3、iSCSI/LUN 或证书管理，先调用 `nas_qnap_ecosystem`。只有在 NAS probe 验证并配置 `qnap_adapters` 后，才调用 `nas_vm_action`、`nas_hbs_action`、`nas_iscsi_action` 或 `nas_certificate_action`；先使用 `dry_run: true`。配置格式见 [ecosystem-adapters.md](ecosystem-adapters.md)。
