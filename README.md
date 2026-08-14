# QNAP AI Control Suite

QNAP AI Control Suite v1 是面向 Codex、OpenClaw、Hermes 与其他 MCP client 的 QNAP 本地控制平面。它在 NAS 上运行一个单二进制 Go agent，并在 Mac 上通过官方 MCP SDK 提供 stdio bridge。

## v1.0.1

- `full_trust` profile：根文件系统、任意 executable、shell pipeline、Docker/QPKG 写操作均可直接执行，仍保留 Bearer 认证与 JSONL 审计。
- 有界 command executor：区分非零退出、超时、找不到 executable 和启动失败；支持 `cwd`、环境变量、stdin、dry run 与独立 stdout/stderr 截断标志。
- binary-safe 文件 API：base64 range read/write，符号链接解析后再做 root 边界检查。
- QPKG、Docker、QTS/QuTS hero 运行时发现、系统、硬件、存储与网络概览。
- 官方 `@modelcontextprotocol/sdk` 1.30.0，当前协议协商、JSON Schema、`structuredContent`。
- 异步 Job、优雅停止、QPKG 真实 PID、统一 API envelope、QNAP probe 与集成检查脚本。

## 安装

构建正式 QPKG：

```bash
./scripts/package_qpkg.sh amd64
```

将 `dist/QnapAIControl_1.0.1.qpkg` 上传到 App Center 手动安装。首次启动会生成 bearer token 和 `full_trust` 配置。打开：

```text
http://NAS_IP:8756/
```

WebUI 仅在浏览器 localStorage 中保存 token；仪表盘显示 profile、平台和运行时发现能力。

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
- [存储与硬件](docs/storage.md)
- [网络](docs/network.md)
- [架构](docs/architecture.md)
- [v1 审计](docs/v1-audit.md)
