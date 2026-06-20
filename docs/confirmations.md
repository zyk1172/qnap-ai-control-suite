# 敏感操作确认机制

## 为什么需要确认

NAS 控制面可以影响下载器、媒体库、容器、套件和共享文件。为了避免模型误操作，MCP 桥接器默认把高风险操作拆成两步：

1. prepare：生成待确认操作。
2. confirm：用户检查 summary 后再执行。

## 当前敏感操作

- 写文件：`file_write`
- 执行 allowlist 命令：`command_run`
- QPKG 启停重启：`qpkg_action`
- Docker 容器启停、重启、暂停、恢复：`docker_action`

## 默认行为

调用 `nas_qpkg_action` 且 `dry_run: false`：

```json
{
  "name": "MoviePilot",
  "action": "restart",
  "reason": "应用配置后重启"
}
```

Docker 容器动作同理。调用 `nas_docker_action` 且 `dry_run: false` 只会创建 `docker_action` 待确认操作：

```json
{
  "name": "moviepilot",
  "action": "restart",
  "reason": "应用配置后重启容器"
}
```

返回：

```json
{
  "confirmation_required": true,
  "operation": {
    "id": "abc...",
    "operation": "qpkg_action",
    "summary": "restart QPKG MoviePilot",
    "confirmation_phrase": "CONFIRM abc...",
    "expires_at": "..."
  }
}
```

执行确认：

```json
{
  "tool": "nas_confirm_operation",
  "arguments": {
    "id": "abc...",
    "confirmation_phrase": "CONFIRM abc..."
  }
}
```

## 过期和审计

- 待确认操作默认 10 分钟过期。
- agent 重启后所有 pending operation 失效。
- prepare 和 confirm 都会写入 `/var/log/qnap-ai-control-agent/audit.jsonl`。

## 建议操作习惯

- 破坏性操作先设置 `dry_run: true`。
- 确认前阅读 `summary` 和 `reason`。
- 对文件写入先读原文件，再写入，再读回验证。
- 不要把 `/bin/sh`、`/bin/bash`、`rm` 加入默认 allowlist。
