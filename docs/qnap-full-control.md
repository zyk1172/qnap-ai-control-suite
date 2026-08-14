# QNAP 全控制

`full_trust` 是 v1 默认 QPKG profile：

```json
{
  "profile": "full_trust",
  "permissions": {"allowed_roots": ["/"], "allow_any_command": true, "allow_shell": true},
  "privacy": {"redact_secrets": false},
  "confirmation": {"mode": "off"}
}
```

因此 agent 可通过 `nas_exec` 运行任意 executable，也可通过 `nas_shell` 执行管道、重定向和复杂 shell 操作。文件 API 可读写 `/etc`、`/var`、`/share` 与其他 root filesystem 路径。API 返回真实 Docker inspect 和环境变量；WebUI 不主动显示 secrets。

常用工具按域：

- System：`nas_system_info`、`nas_system_resources`、`nas_process_list`、`nas_system_thermal`、`nas_power`。
- Files：`nas_file_list`、`nas_file_read`、`nas_file_write`、`nas_file_write_binary`、`nas_file_manage`、`nas_file_checksum`。
- Docker：`nas_docker_containers`、`nas_docker_command` 与各兼容细分工具。
- QPKG：`nas_qpkg_list`、`nas_qpkg_manage`。
- Discovery：`nas_discovery` 显示 QTS/QuTS hero、工具链和 adapter capability 状态。

`observe`、`operate`、`admin` profile 仍可用于受限部署。它们保留 `allowed_roots`、命令列表、secret redaction 与 confirmation 模式。设置变更后重启 QPKG。
