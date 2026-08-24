# MCP Client 配置

Mac 上安装 Node 20+，并让 Codex、OpenClaw、Hermes 或其他 MCP client 指向同一份 `mac-bridge/src/server.js`。WebUI 的“接入”页可以根据客户端选择生成配置；下面是脱敏模板，真实 Token 只从 WebUI 当前页面复制，不要提交 Git。

```json
{
  "mcpServers": {
    "qnap-ai-control": {
      "command": "node",
      "args": ["/path/to/qnap-ai-control-suite/mac-bridge/src/server.js"],
      "env": {
        "QACS_BASE_URL": "http://NAS_IP:8756",
        "QACS_TOKEN": "CURRENT_TOKEN",
        "QACS_TOOLSETS": "core,files,docker,storage,network,qnap,admin"
      }
    }
  }
}
```

Codex 使用 TOML `[mcp_servers.qnap-ai-control]`，Hermes 使用 YAML `mcp_servers`，OpenClaw 使用 `mcp.servers` 的 JSON5/JSON 配置。项目不会假设这三者可以直接复用通用 JSON；请使用 WebUI 为对应客户端生成的语法，并以各客户端当前文档为准：[Codex MCP 配置](https://github.com/openai/codex/blob/main/codex-rs/config/src/mcp_edit.rs)、[Hermes MCP 配置参考](https://hermes-agent.nousresearch.com/docs/reference/mcp-config-reference/)、[OpenClaw 配置参考](https://docs.openclaw.ai/gateway/configuration-reference)。

不要将真实 `QACS_TOKEN` 放入 Git 配置、截图或公共日志。旧配置中的 `mcp-server.js` 可继续使用，但新配置建议指向 `server.js`。

## 更新已安装的客户端

如果某个智能体已经安装过本 MCP，NAS 更新后不需要重新发现 NAS 端路由，但需要让该智能体使用当前版本的 Mac bridge：

1. 更新 Mac 上的仓库到 v2.1.0 分支或发布版本：

   ```bash
   cd /path/to/qnap-ai-control-suite
   git fetch origin
   git checkout codex/v2.1-webui-token-management
   git pull
   cd mac-bridge
   npm ci --ignore-scripts --no-audit --no-fund
   ```

2. 确认该智能体的 MCP 配置仍指向同一目录下的 `mac-bridge/src/server.js` 或 `mac-bridge/src/mcp-server.js`，不要继续指向旧目录里复制出来的旧 bridge。

3. 重启智能体或重启其 MCP 子进程。MCP 的 `tools/list` 在进程启动时加载，不重启不会拿到 v2 的新工具与 toolset 设置。

4. 验证顺序：

   1. `nas_health`，确认返回当前 Agent 版本。
   2. `nas_status_snapshot`，确认返回当前 Agent 状态。
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

完整 shell pipeline 使用 `nas_shell`，例如 `{ "shell": "df -h | sort" }`。在 `full_trust` 下普通操作无需 prepare/confirm。敏感操作由 NAS 返回一次性 `approval_id` 后，MCP Bridge 会发起标准 elicitation；用户批准后 Bridge 内部记录 decision，并自动以同一 method、path、序列化 body、request ID、idempotency key 和 timeout policy 重试原始请求一次。模型不拥有批准工具；拒绝不重试，不支持 elicitation 的 client 会 fail closed。每次调用都会写入 audit log。

Bridge 对 Hermes 式 `action=accept` + 空 `content` 兼容为“允许这一次”；`decline` 和 `cancel` 会记录拒绝。批准接口 `/v1/approvals/{approval_id}/decision` 是 Bridge/用户交互层的 HTTP 控制流，不会出现在 MCP tools/list 中。

审批交互默认最多等待 300 秒，并按 NAS ticket 的剩余有效期裁剪；可通过 `QACS_APPROVAL_TIMEOUT_MS` 调整，Bridge 会将其限制在 9 分钟以内。

当前仓库固定使用已验证的 `@modelcontextprotocol/sdk@1.30.0`。不要把尚未在当前 lockfile 验证过的 SDK 版本写入客户端配置。Bridge 兼容 2025-era form elicitation，包括裸 `elicitation: {}`；Hermes 式 `action=accept` + 空 `content` 解释为允许一次，`decline`/`cancel` 永远优先表示拒绝。Bridge 的审批闭环只依赖已验证的标准 elicitation 路径。

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
