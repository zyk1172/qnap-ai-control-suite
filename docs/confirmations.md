# 敏感操作确认机制

## 为什么需要确认

NAS 控制面可以影响下载器、媒体库、容器、套件和共享文件。`0.3.3` 的 Mac stdio bridge 和 NAS 原生 HTTP MCP 都保持相同确认策略：普通查询、启停、日志、stats、pull、exec、写文件和 allowlist 命令默认直接执行；只有最高风险的 3 类操作需要 prepare / confirm。

## 需要确认的 3 类操作

- `docker_run_create`：`docker run` / `docker create`。
- `docker_destroy`：`docker rm`、`docker rmi`、`docker system prune`、`docker volume rm/prune`、`docker network rm/prune`、`docker compose down/rm`。
- `qpkg_install_remove`：QPKG `add`、`install_file`、`install_url`、`remove`、`update_all`。

## 不再强制确认的常用操作

- QPKG `start`、`stop`、`restart`、`enable`、`disable`、`status`。
- Docker `start`、`stop`、`restart`、`pause`、`unpause`。
- Docker `pull`、`exec`、`logs`、`inspect`、`stats`。
- Docker network / volume 的列表、创建、inspect。
- Docker compose `up`、`restart`、`pull`、`logs`、`ps`。
- `nas_file_write`：直接执行，但只能写 allowed roots 下路径，并写审计日志。
- `nas_command_run`：直接执行，但只能运行 allowlist 里的命令，不经过 shell，并写审计日志。

## 默认流程

准备删除容器：

```json
{
  "tool": "nas_docker_remove",
  "arguments": {
    "args": ["-f", "moviepilot"],
    "reason": "移除旧容器"
  }
}
```

返回：

```json
{
  "confirmation_required": true,
  "operation": {
    "id": "abc...",
    "operation": "docker_destroy",
    "summary": "docker destructive command: docker rm -f moviepilot",
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

普通重启容器会直接执行：

```json
{
  "tool": "nas_docker_action",
  "arguments": {
    "name": "moviepilot",
    "action": "restart"
  }
}
```

## 过期和审计

- 待确认操作默认 10 分钟过期。
- agent 重启后所有 pending operation 失效。
- prepare 和 confirm 都会写入 `/var/log/qnap-ai-control-agent/audit.jsonl`。

## 建议操作习惯

- 删除、安装、全量更新先设置 `dry_run: true`。
- 确认前阅读 `summary` 和 `reason`。
- 对文件写入先读原文件，再写入，再读回验证。
- 不要把 `/bin/sh`、`/bin/bash`、`rm` 加入默认 allowlist。
