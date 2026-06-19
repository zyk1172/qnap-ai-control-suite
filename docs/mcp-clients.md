# MCP 客户端配置

## MCP server 命令

```bash
node /path/to/qnap-ai-control-suite/mac-bridge/src/mcp-server.js
```

环境变量：

```bash
QACS_BASE_URL=http://NAS_IP:8756
QACS_TOKEN=...
```

## Codex / OpenClaw / Hermes 通用配置形状

不同客户端配置文件位置不同，但 server 定义通常类似：

```json
{
  "mcpServers": {
    "qnap-ai-control": {
      "command": "node",
      "args": [
        "/path/to/qnap-ai-control-suite/mac-bridge/src/mcp-server.js"
      ],
      "env": {
        "QACS_BASE_URL": "http://NAS_IP:8756",
        "QACS_TOKEN": "REPLACE_WITH_TOKEN"
      }
    }
  }
}
```

## 工具列表

只读工具：

- `nas_health`
- `nas_capabilities`
- `nas_system_overview`
- `nas_processes`
- `nas_audit_tail`
- `nas_file_list`
- `nas_file_stat`
- `nas_file_read`
- `nas_qpkg_list`
- `nas_qnap_getcfg`
- `nas_pending_operations`

敏感工具：

- `nas_file_write`
- `nas_command_run`
- `nas_qpkg_action`
- `nas_prepare_operation`
- `nas_confirm_operation`

`nas_file_write`、`nas_command_run`、`nas_qpkg_action` 在 `dry_run: false` 时只会创建待确认操作，不会直接执行。

## 示例

列出共享目录：

```json
{
  "tool": "nas_file_list",
  "arguments": {
    "path": "/share"
  }
}
```

查看 MoviePilot 安装路径：

```json
{
  "tool": "nas_qnap_getcfg",
  "arguments": {
    "section": "MoviePilot",
    "key": "Install_Path"
  }
}
```

dry-run 重启套件：

```json
{
  "tool": "nas_qpkg_action",
  "arguments": {
    "name": "MoviePilot",
    "action": "restart",
    "dry_run": true,
    "reason": "验证将要执行的 QPKG restart 命令"
  }
}
```

