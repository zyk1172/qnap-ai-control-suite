# Container Station / Docker

先运行 `nas_discovery` 确认 Docker CLI 路径。然后使用 `nas_docker_info`、`nas_docker_containers`、`nas_docker_images`，或完整的 `nas_docker_command`。

```json
{"subcommand":"run","args":["-d","--name","web","-p","8080:80","nginx:latest"]}
```

`nas_docker_command` 传递 argv，不执行 shell 字符串。需要复杂恢复时，`nas_docker_inspect` 读取真实 inspect，修改参数后再以 `nas_docker_command` 重建。`full_trust` 不强制隐藏 inspect 中的 secret；请勿复制到公共渠道。
