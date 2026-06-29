# Container Station / Docker 管理

`0.3.0` 开始，QNAP AI Control Suite 的 Docker 能力从“查询和启停”扩展到接近完整的 Container Station / Docker CLI 控制。agent 仍然不经过 shell，而是用参数数组调用 Docker CLI。

![Container Docker tools](images/container-docker-tools.svg)

## 能做什么

查询和诊断：

- `nas_docker_info`：Docker engine 版本和运行状态。
- `nas_docker_containers`：列出所有容器。
- `nas_docker_images`：列出镜像。
- `nas_docker_inspect`：inspect 容器或镜像。
- `nas_docker_logs`：读取容器日志。
- `nas_docker_stats`：读取容器资源占用，默认 `--no-stream`。

容器操作：

- `nas_docker_action`：`start`、`stop`、`restart`、`pause`、`unpause`。
- `nas_docker_run`：执行 `docker run`，需要确认。
- `nas_docker_create`：执行 `docker create`，需要确认。
- `nas_docker_remove`：执行 `docker rm`，需要确认。
- `nas_docker_exec`：执行 `docker exec`。
- `nas_docker_pull`：执行 `docker pull`。
- `nas_docker_image_remove`：执行 `docker rmi`，需要确认。

更底层的 Docker 能力：

- `nas_docker_network`：执行 `docker network ...`，`rm/prune` 需要确认。
- `nas_docker_volume`：执行 `docker volume ...`，`rm/prune` 需要确认。
- `nas_docker_compose`：执行 `docker compose ...`，`down/rm` 需要确认。
- `nas_docker_command`：受控通用入口，允许常见 Docker 子命令。

## 确认策略

为了减少使用阻力，普通启停、pull、exec、logs、stats、compose up/restart 不需要确认。只有最高风险的 Docker 操作需要确认：

- `docker run`
- `docker create`
- `docker rm`
- `docker rmi`
- `docker system prune`
- `docker volume rm/prune`
- `docker network rm/prune`
- `docker compose down/rm`

## 安全边界

agent 不开放 shell。所有 Docker 调用都使用参数数组，例如：

```json
{
  "args": ["-d", "--name", "web", "nginx:latest"]
}
```

不要传入一整段 shell 字符串，例如 `docker run ... && rm -rf ...`。这类输入不会被 shell 展开，但也不应该作为工具参数使用。

## Docker CLI 路径

agent 默认查找常见 Container Station 路径：

```json
[
  "/share/CACHEDEV1_DATA/.qpkg/container-station/bin/docker",
  "/share/CACHEDEV1_DATA/.qpkg/container-station/usr/bin/docker",
  "/share/CACHEDEV5_DATA/.qpkg/container-station/bin/docker",
  "/share/CACHEDEV5_DATA/.qpkg/container-station/usr/bin/docker",
  "/usr/bin/docker",
  "/usr/local/bin/docker",
  "/bin/docker"
]
```

如果路径不同，编辑：

```text
/etc/config/qnap-ai-control-agent/config.json
```

修改：

```json
{
  "docker_paths": [
    "/你的/container-station/bin/docker",
    "/usr/bin/docker"
  ]
}
```

然后重启 QPKG。

## MCP 调用示例

列出容器：

```json
{
  "tool": "nas_docker_containers",
  "arguments": {}
}
```

查看 stats：

```json
{
  "tool": "nas_docker_stats",
  "arguments": {}
}
```

拉取镜像：

```json
{
  "tool": "nas_docker_pull",
  "arguments": {
    "args": ["nginx:latest"]
  }
}
```

执行容器内命令：

```json
{
  "tool": "nas_docker_exec",
  "arguments": {
    "args": ["moviepilot", "python", "--version"]
  }
}
```

创建并运行容器，需要确认：

```json
{
  "tool": "nas_docker_run",
  "arguments": {
    "args": ["-d", "--name", "web", "-p", "8080:80", "nginx:latest"],
    "reason": "创建测试 Web 容器"
  }
}
```

Docker Compose up：

```json
{
  "tool": "nas_docker_compose",
  "arguments": {
    "args": ["-f", "/share/Container/app/docker-compose.yml", "up", "-d"]
  }
}
```

Docker Compose down，需要确认：

```json
{
  "tool": "nas_docker_compose",
  "arguments": {
    "args": ["-f", "/share/Container/app/docker-compose.yml", "down"],
    "reason": "停止并移除 compose 项目"
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

## 故障排查

`docker CLI was not found`：

- Container Station 没安装或没启动。
- Docker CLI 不在默认路径里。
- 需要把实际 Docker 路径加入 `docker_paths`。

`permission denied`：

- QPKG 运行用户没有权限访问 Docker socket。
- Container Station 自身未启动完成。

命令超时：

- 构建、拉取、compose up 可能较慢，给工具传 `timeout_sec`。
- `nas_docker_stats` 默认使用 `--no-stream`，如果传自定义 args，避免长期 streaming。
