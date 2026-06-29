# Hermes 和 OpenClaw 接入 QNAP AI Control MCP

这篇教程说明如何把 QNAP AI Control Suite 作为 MCP server 接入 Hermes 和 OpenClaw。文档使用占位符，不包含个人路径、真实 NAS 地址或真实 token。

## 前置条件

- QNAP AI Control QPKG 已安装并启动。
- WebUI 可以打开：

```text
http://NAS_IP:8756/
```

- WebUI 中的 `测试连接` 成功。
- Mac 上可以运行 `node`。
- Mac 上已有本仓库源码，下面用 `/path/to/qnap-ai-control-suite` 表示仓库路径。

## 通用 MCP 信息

MCP server 命令：

```bash
node /path/to/qnap-ai-control-suite/mac-bridge/src/mcp-server.js
```

环境变量：

```bash
QACS_BASE_URL=http://NAS_IP:8756
QACS_TOKEN=REPLACE_WITH_TOKEN
```

`QACS_TOKEN` 可以从 WebUI 的 MCP 配置页复制，或从 NAS 端读取：

```text
/etc/config/qnap-ai-control-agent/initial-token.txt
```

不要把真实 token 提交到 GitHub、文档、截图或公开日志。

## OpenClaw 接入

把 token 放入当前 shell。建议使用隐藏输入：

```bash
read -rsp "QACS_TOKEN: " QACS_TOKEN
echo
```

添加 MCP server：

```bash
openclaw mcp add qnap-ai-control \
  --command node \
  --arg /path/to/qnap-ai-control-suite/mac-bridge/src/mcp-server.js \
  --env QACS_BASE_URL=http://NAS_IP:8756 \
  --env QACS_TOKEN="$QACS_TOKEN" \
  --connect-timeout 20 \
  --timeout 60 \
  --parallel
```

验证配置：

```bash
openclaw mcp list
openclaw mcp probe qnap-ai-control
openclaw mcp reload
openclaw doctor
```

验证问题示例：

```text
使用 qnap-ai-control MCP 调用 nas_health，然后调用 nas_docker_containers 查看 NAS 上运行的容器。
```

## Hermes 接入

把 token 放入当前 shell：

```bash
read -rsp "QACS_TOKEN: " QACS_TOKEN
echo
```

添加 MCP server：

```bash
hermes mcp add qnap-ai-control \
  --command node \
  --env QACS_BASE_URL=http://NAS_IP:8756 QACS_TOKEN="$QACS_TOKEN" \
  --args /path/to/qnap-ai-control-suite/mac-bridge/src/mcp-server.js
```

验证配置：

```bash
hermes mcp list
hermes mcp test qnap-ai-control
hermes status
hermes gateway status
```

如果 Hermes gateway 已经在后台运行，刷新或重启 gateway：

```bash
hermes gateway restart
```

验证问题示例：

```bash
hermes -z "使用 qnap-ai-control MCP 调用 nas_health，再调用 nas_docker_info 和 nas_docker_containers。"
```

## 应该出现的工具

接入成功后，工具列表中应包含：

```text
nas_health
nas_capabilities
nas_system_overview
nas_system_thermal
nas_processes
nas_audit_tail
nas_file_list
nas_file_stat
nas_file_read
nas_file_write
nas_command_run
nas_qpkg_list
nas_qpkg_action
nas_qpkg_manage
nas_qnap_getcfg
nas_docker_info
nas_docker_containers
nas_docker_images
nas_docker_inspect
nas_docker_logs
nas_docker_action
nas_docker_command
nas_docker_run
nas_docker_create
nas_docker_remove
nas_docker_exec
nas_docker_pull
nas_docker_image_remove
nas_docker_network
nas_docker_volume
nas_docker_compose
nas_docker_stats
nas_prepare_operation
nas_pending_operations
nas_confirm_operation
```

## 敏感操作确认

普通启停、日志、stats、pull、exec 默认直接执行。只有以下 5 类最高风险操作会先创建待确认操作：

- `file_write`
- `command_run`
- `docker_run_create`
- `docker_destroy`
- `qpkg_install_remove`

执行流程：

1. 先调用敏感工具，工具返回 `confirmation_required: true`。
2. 阅读 `summary`，确认目标、动作和原因正确。
3. 使用返回的 `id` 和 `confirmation_phrase` 调用 `nas_confirm_operation`。
4. agent 执行操作并写入审计日志。

示例：准备创建容器：

```json
{
  "tool": "nas_docker_run",
  "arguments": {
    "args": ["-d", "--name", "CONTAINER_NAME", "IMAGE:TAG"],
    "reason": "Create container"
  }
}
```

确认执行：

```json
{
  "tool": "nas_confirm_operation",
  "arguments": {
    "id": "OPERATION_ID",
    "confirmation_phrase": "CONFIRM OPERATION_ID"
  }
}
```

## 常见问题

### 找不到 node

确认 Mac 上安装了 Node.js：

```bash
node --version
```

### MCP server 能启动但工具调用失败

检查：

- `QACS_BASE_URL` 是否是 NAS 的实际地址。
- `QACS_TOKEN` 是否为真实 token，没有多余空格或换行。
- QNAP AI Control WebUI 的 `测试连接` 是否成功。
- NAS 和 Mac 是否在同一网络，或已经通过 VPN / 隧道互通。

### Docker 工具返回找不到 docker

确认 NAS 已安装并启动 Container Station。必要时在 NAS 端配置中补充 Docker CLI 路径：

```text
/etc/config/qnap-ai-control-agent/config.json
```

字段示例：

```json
{
  "docker_paths": [
    "/share/CACHEDEV1_DATA/.qpkg/container-station/bin/docker",
    "/usr/bin/docker"
  ]
}
```

修改后重启 QPKG。
