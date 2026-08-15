# QNAP AI Control Suite

QNAP AI Control Suite v1 是面向 Codex、OpenClaw、Hermes 与其他 MCP client 的 QNAP 本地控制平面。它在 NAS 上运行一个单二进制 Go agent，并在 Mac 上通过官方 MCP SDK 提供 stdio bridge。

## v1.0.16

v1.0.16 将旧版受限 NAS API 包装器升级为可信局域网内的 QNAP 本机控制平面：保留 Bearer Token 和 JSONL 审计，同时在 `full_trust` 下支持根文件系统、任意 argv、shell、Docker、QPKG 与系统管理。

- 进程清单改为直接读取 `/proc`，不再依赖 QNAP busybox `ps -o` 的不兼容输出。
- `nas_service_list` 在无 systemd 的 QTS 上返回 QPKG 服务清单，`nas_service_action` 走已验证的 `qpkg_cli`。
- ACL 自动查找 QNAP 的 `/usr/bin/getfacl`/`setfacl`；工具缺失时返回 stat 降级而不是原始 `start_failed`。
- UPS 能力状态与 reason 一致；SMART 路径扩展并返回 sysfs 降级与真实缺失说明。
- QTS 快照能力如实区分 `snapshot_util` create 与需要认证 QCLI 会话的 list/delete/restore。
- QNAP 无 path 的系统共享标记为 `system_share`，不再让 `TMBackup` 看起来像普通空路径。
- 97 个 MCP 工具：文件、系统、进程、服务、Docker、QPKG、存储、RAID、SMART、网络、账户、共享目录、ACL、日志、UPS、异步 Job 与 QTS adapter。
- 统一 command executor：明确非零退出、超时和启动错误；支持 cwd、环境变量、二进制 stdin、dry run 及有界输出。
- binary-safe 文件读写、复制/移动/归档、符号链接边界校验，以及 `/proc` 驱动的 QPKG 运行状态与 PID 证据。
- Container Station wrapper 发现、Docker/Compose 泛化调用与长任务 Job；QTS `qpkg_cli` 使用已验证的 `--start`、`--stop`、`--enable`、`--disable` 等参数。
- QTS 私有功能不猜测命令：Virtual Switch、系统设置、固件、通知、Storage Manager、VM、HBS、iSCSI/LUN 与证书动作必须先经过 `nas_qnap_probe` 验证后配置 adapter。
- QPKG 升级会结束 PID 文件遗失的旧 Agent，避免旧 0.3.x 服务继续占用端口。

## 与旧版的差异

| 范围 | v0.3.2 | v1.0.16 |
| --- | --- | --- |
| 系统命令 | 7 个 allowlist executable，禁止 shell | 任意 argv、shell pipeline、cwd/env/stdin；可区分失败、超时和输出截断 |
| 文件 | 仅 `/share`，文本导向 | 根文件系统、base64 二进制 range I/O、目录/权限/归档/校验和 |
| Docker | 基础容器操作与确认流程 | 运行时 wrapper 发现、完整 Docker/Compose argv、重建、网络/卷和异步任务 |
| QPKG | 基础列表与动作 | 已验证的 QTS 参数、安装/移除/更新、运行 PID 和异步操作跟踪 |
| NAS 管理 | 概览、进程、温度 | 存储、RAID/SMART、网络、用户/组、共享/ACL/NFS、日志、UPS、服务和电源 |

完整逐项对比、真机验证范围及未声明支持的边界见 [v0.3 到 v1.0.16 对比](docs/v0.3-v1.0.15-comparison.md)。

## 安装

构建正式 QPKG：

```bash
./scripts/package_qpkg.sh amd64
```

将 `dist/QnapAIControl_1.0.16.qpkg` 上传到 App Center 手动安装。首次启动会生成 bearer token 和 `full_trust` 配置。打开：

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

旧的 `mac-bridge/src/mcp-server.js` 仍可用作兼容入口。先调用 `nas_health`、`nas_discovery`，再按需使用 `nas_exec` 或 `nas_shell`。`nas_exec` 是 argv 形式；需要重定向、管道或变量展开时使用 `nas_shell`。

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
