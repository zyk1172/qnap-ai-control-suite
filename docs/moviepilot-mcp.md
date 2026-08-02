# MoviePilot Agent MCP 接入

`0.3.3` 起，QNAP AI Control Agent 直接提供受 Bearer token 保护的 Streamable HTTP MCP 端点：

```text
POST http://NAS_IP:8756/mcp
Authorization: Bearer <QACS_TOKEN>
```

它不依赖 Mac、Node.js 或 `mac-bridge`，适合部署在 NAS 内网的 MoviePilot 容器。

## MoviePilot 配置

打开 MoviePilot 的 `智能体 -> MCP 服务器`，新增一个服务器并填写：

```json
{
  "id": "qnap-ai-control",
  "name": "QNAP AI Control",
  "enabled": true,
  "transport": "streamable_http",
  "description": "受控管理 NAS、Container Station 和 QPKG",
  "url": "http://NAS_IP:8756/mcp",
  "headers": {
    "Authorization": "Bearer REPLACE_WITH_QACS_TOKEN"
  },
  "timeout": 30,
  "tool_prefix": "nas",
  "require_admin": true
}
```

如果通过 MoviePilot API 保存，提交体为：

```json
{
  "servers": [
    {
      "id": "qnap-ai-control",
      "name": "QNAP AI Control",
      "enabled": true,
      "transport": "streamable_http",
      "url": "http://NAS_IP:8756/mcp",
      "headers": {
        "Authorization": "Bearer REPLACE_WITH_QACS_TOKEN"
      },
      "timeout": 30,
      "tool_prefix": "nas",
      "require_admin": true
    }
  ]
}
```

保存后先运行 MoviePilot 的 MCP 测试。成功时会发现 `nas_health`、`nas_capabilities`、`nas_docker_containers` 等工具。

## 行为与边界

- `/mcp` 与现有 `/v1/*` API 使用同一个 token hash、路径限制、命令白名单和 JSONL 审计日志。
- MCP 工具调用会复用 agent 的业务路由，不会建立绕过校验的第二套执行逻辑。
- Docker `run/create`、Docker 删除或清理，以及 QPKG 安装、移除、全量更新仍返回待确认操作；必须再调用 `nas_confirm_operation`。
- MoviePilot 会将请求头保存为 Agent MCP 配置的一部分。使用专用 QACS token，不要复用其他服务令牌。
- 不要把 `8756` 暴露到公网；MoviePilot 和 NAS 应位于同一受信任 LAN 或 VPN 中。

## 验证顺序

1. 在 MoviePilot 中测试 MCP 连接。
2. 让智能体调用 `nas_health`。
3. 调用 `nas_capabilities`，核对 profile 与允许目录。
4. 调用 `nas_docker_containers`，确认容器查询可用。
5. 仅在明确需要时测试一次高风险动作的 prepare/confirm 流程。
