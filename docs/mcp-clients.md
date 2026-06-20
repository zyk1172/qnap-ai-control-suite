# MCP 客户端配置

这篇文档说明 Codex、OpenClaw、Hermes 这类 agent 如何把 QNAP AI Control Suite 加成 MCP server。NAS 端负责真实操作，Mac 端 `mac-bridge` 负责 stdio MCP 协议。

## 前置条件

- QPKG 已安装并启动。
- WebUI 可以打开：`http://NAS_IP:8756/`
- WebUI 已保存 token，并且 `测试连接` 成功。
- Mac 上有 Node.js，可以运行 `node`。
- 本仓库在 Mac 上的路径是 `/path/to/qnap-ai-control-suite`。

## MCP server 命令

```bash
node /path/to/qnap-ai-control-suite/mac-bridge/src/mcp-server.js
```

环境变量：

```bash
QACS_BASE_URL=http://NAS_IP:8756
QACS_TOKEN=...
```

## 从 WebUI 复制配置

1. 打开 `http://NAS_IP:8756/`。
2. 粘贴 token，点击 `保存到浏览器`。
3. 点击 `测试连接`，确认返回成功。
4. 打开 `2. 配置 MCP` 标签页。
5. 点击 `复制 MCP JSON`。
6. 把复制出的 server 定义粘贴到 agent 的 MCP 配置中。

![WebUI token setup](images/webui-token-setup.svg)

## Codex / OpenClaw / Hermes 通用配置

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

字段含义：

- `qnap-ai-control`：MCP server 名称，agent 里会显示这个名字。
- `command`：固定填 `node`。
- `args`：Mac 端 MCP bridge 的绝对路径。
- `QACS_BASE_URL`：NAS agent 的 WebUI/API 地址。
- `QACS_TOKEN`：WebUI 里保存的同一个 token。

## Codex 添加方式

Codex 桌面版如果有 MCP 设置界面，直接新增一个 server，按上面的 `command`、`args`、`env` 填入。

如果使用配置文件，把 `mcpServers.qnap-ai-control` 这段加入 Codex 的 MCP 配置，然后重启或重载 Codex。重载后让 Codex 列出工具，应该能看到：

```text
nas_health
nas_file_read
nas_qpkg_action
nas_docker_containers
nas_docker_action
nas_confirm_operation
```

## OpenClaw 添加方式

在 OpenClaw 的 MCP servers 配置中新增：

```json
{
  "name": "qnap-ai-control",
  "command": "node",
  "args": [
    "/path/to/qnap-ai-control-suite/mac-bridge/src/mcp-server.js"
  ],
  "env": {
    "QACS_BASE_URL": "http://NAS_IP:8756",
    "QACS_TOKEN": "REPLACE_WITH_TOKEN"
  }
}
```

保存后重载 OpenClaw。先让 agent 调用 `nas_health`，再调用 `nas_capabilities` 检查 NAS 返回的能力清单。

## Hermes 添加方式

Hermes 如果使用 JSON MCP 配置，直接放入：

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

保存后重启 Hermes gateway 或刷新 MCP server 列表。验证顺序：

1. `nas_health`
2. `nas_capabilities`
3. `nas_docker_containers`
4. `nas_audit_tail`

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
- `nas_docker_info`
- `nas_docker_containers`
- `nas_docker_images`
- `nas_docker_inspect`
- `nas_docker_logs`
- `nas_pending_operations`

敏感工具：

- `nas_file_write`
- `nas_command_run`
- `nas_qpkg_action`
- `nas_docker_action`
- `nas_prepare_operation`
- `nas_confirm_operation`

`nas_file_write`、`nas_command_run`、`nas_qpkg_action`、`nas_docker_action` 在 `dry_run: false` 时只会创建待确认操作，不会直接执行。

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

列出容器：

```json
{
  "tool": "nas_docker_containers",
  "arguments": {}
}
```

读取容器日志：

```json
{
  "tool": "nas_docker_logs",
  "arguments": {
    "name": "moviepilot",
    "tail": 200
  }
}
```

dry-run 重启容器：

```json
{
  "tool": "nas_docker_action",
  "arguments": {
    "name": "moviepilot",
    "action": "restart",
    "dry_run": true,
    "reason": "验证将要执行的 Docker restart 命令"
  }
}
```

真正执行重启时不要带 `dry_run: true`。工具会返回 `confirmation_required`，用户检查 `summary` 后再调用 `nas_confirm_operation`。

## 常见问题

### agent 看不到工具

检查三件事：

- MCP server 的 `args` 必须是 Mac 上真实存在的绝对路径。
- `QACS_TOKEN` 不能带多余空格或换行。
- 修改配置后需要重载或重启 agent。

### Docker 工具返回找不到 docker

先确认 QNAP 已安装并启动 Container Station。agent 会自动查找常见 Container Station 路径，也可以在 NAS 配置文件中修改 `docker_paths`：

```json
{
  "docker_paths": [
    "/share/CACHEDEV1_DATA/.qpkg/container-station/bin/docker",
    "/usr/bin/docker"
  ]
}
```

### 敏感操作没有直接执行

这是正常行为。`nas_docker_action`、`nas_qpkg_action`、`nas_file_write`、`nas_command_run` 默认先创建待确认操作。必须把返回的 `id` 和 `confirmation_phrase` 传给 `nas_confirm_operation` 才会执行。
