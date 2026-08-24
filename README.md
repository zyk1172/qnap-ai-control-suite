# QNAP AI Control Suite

QNAP AI Control Suite v2 是面向 Codex、OpenClaw、Hermes 与其他 MCP client 的 QNAP 本地控制平面。它在 NAS 上运行一个单二进制 Go agent，并在 Mac 上通过 MCP bridge 提供 stdio 工具。

## v2.0.0

v2.0.0 在保留可信 LAN `full_trust` 控制能力的基础上，增加独立的 Operation Registry：权限、审批、审计、资源锁和 Job 生命周期不再由各 handler 分散处理。

- `full_trust` 仅定义权限；默认 `approval.mode=sensitive_only`。读取直通，普通写入执行并审计，关机、固件、存储破坏、危险 raw command 等敏感操作返回 10 分钟、单次且绑定原请求的 `approval_id`。
- 敏感操作由 MCP Bridge 捕获 `approval_required`，通过标准 MCP elicitation 请求用户“允许这一次/拒绝”；批准后 Bridge 记录 decision，并只自动重试一次完全相同的原始请求。模型没有批准工具；不支持交互审批的客户端会 fail closed。所有操作记录 request ID、风险、目标、审批/Job、状态、耗时和脱敏摘要。
- Job 最多并发 4 个，支持资源锁、idempotency key、JSONL journal、重启后 `interrupted`，并以进程组 `SIGTERM` → 5 秒 → `SIGKILL` 取消子进程树。
- MCP 默认只公开 `core`；通过 `QACS_TOOLSETS=files,docker,storage,network,qnap,admin,raw,compat` 按需启用其余工具与旧 alias。
- 新增 `nas_status_snapshot`、文本/按行/grep 文件读取、原子写/CAS/backup/目录树同步、IPv6、进程与磁盘指标、SMB 状态、Docker health/Compose inventory。Docker reconstruction 明确报告是否无损以及缺失字段。
- QPKG inventory 单次扫描 `/proc`；QTS 私有写能力仍只能在真机 probe 和显式 argv 模板均验证后注册。

## 与旧版的差异

| 范围 | v1.0.16 | v2.0.0 |
| --- | --- | --- |
| 控制面 | handler 局部确认与审计 | Operation Registry、统一风险/审批/审计/Job 资源锁 |
| MCP | 全量工具和 alias 默认暴露 | `core` 默认面，按 toolset 启用其他能力 |
| 长任务 | 内存 Job | 并发上限、journal、重启恢复、进程组取消 |
| 文件 | 直接写与 base64 读取 | 原子 CAS 写/backup、text/lines/grep、tree copy/sync |
| NAS 诊断 | 多次分散调用 | 状态快照、IPv6、SMB、Docker health、磁盘 I/O/SMART 摘要 |

v1 的历史能力与边界仍见 [v0.3 到 v1.0.16 对比](docs/v0.3-v1.0.15-comparison.md)。

## 安装

构建正式 QPKG：

```bash
./scripts/package_qpkg.sh amd64
```

将 `dist/QnapAIControl_2.0.0.qpkg` 上传到 App Center 手动安装。首次启动会生成 bearer token、`full_trust` 权限和 `sensitive_only` 审批配置。打开：

```text
http://NAS_IP:8756/
```

WebUI 只在当前页面输入状态保存 token；仪表盘显示 profile、平台和运行时发现能力。

## MCP

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

旧的 `mac-bridge/src/mcp-server.js` 仍可用作兼容入口。默认先调用 `nas_health`、`nas_status_snapshot`；需要更多能力时设置 `QACS_TOOLSETS`。`nas_exec` 是 argv 形式；需要重定向、管道或变量展开时使用 `nas_shell`。

## 安全模型

`full_trust` 是可信 LAN/root 管理模式，功能不会因 UI 隐私展示策略被削减。不要将 `8756` 暴露到公网；使用 VPN、Tailscale、WireGuard 或 SSH tunnel。详细说明见 [安全模型](docs/security-model.md)。

## 文档

- [安装和升级](docs/install.md)
- [WebUI 使用](docs/webui.md)
- [MCP client 教程](docs/mcp-clients.md)
- [完整控制能力](docs/qnap-full-control.md)
- [生态适配器配置](docs/ecosystem-adapters.md)
- [存储与硬件](docs/storage.md)
- [网络](docs/network.md)
- [架构](docs/architecture.md)
- [v1 审计](docs/v1-audit.md)
- [v0.3 到 v1.0.16 对比](docs/v0.3-v1.0.15-comparison.md)
