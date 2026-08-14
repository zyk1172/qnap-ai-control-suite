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

- System：`nas_system_info`、`nas_system_resources`、`nas_process_list`、`nas_process_action`、`nas_service_list`、`nas_service_action`、`nas_system_thermal`、`nas_power`。
- Storage：`nas_raid` 会报告 Linux mdraid 成员、位图、降级状态与同步进度；`nas_raid_manage` 在本机存在标准 `sync_action` 时支持 `scrub_start` / `scrub_stop`。`nas_snapshot_manage` 可对 ZFS snapshot 执行 `create`、`delete`、`clone` 与 `restore`（`zfs rollback`）；QTS 私有 restore 仍通过 probe 后的命令或 `nas_exec` 执行。
- Files：`nas_file_list`、`nas_file_read`、`nas_file_write`、`nas_file_write_binary`、`nas_file_append`、`nas_file_search`、`nas_file_tail`、`nas_file_du`、`nas_file_manage`、`nas_file_checksum`。`nas_file_manage` 还支持 ZIP/TAR/TAR.GZ 的 `archive` 与 `extract`，并拒绝归档路径穿越和链接条目。
- Docker：`nas_docker_containers`、`nas_docker_command` 与各兼容细分工具。
- QPKG：`nas_qpkg_list`、`nas_qpkg_manage`。支持 `start`、`stop`、`restart`、`enable`、`disable`、`status`、`download`、`cancel`、`remove`、`install_file`、`install_url`、`update_all`、`clean` 和 `add`。`restart` 会执行 QTS 支持的 stop/start 两条命令；`dry_run` 返回实际 argv，不执行变更。`async: true` 适用于下载、安装、移除和更新，返回 Job；完成时会重新读取 QPKG inventory。
- Discovery：`nas_discovery` 显示 QTS/QuTS hero、工具链和 adapter capability 状态。
- System：`nas_system_info` 与 `nas_system_resources` 返回 CPU、RAM、swap、load、挂载、时间、时区和 NTP 摘要；`nas_system_sockets` 返回 TCP/UDP socket inventory，`nas_system_ntp` 返回可识别的 NTP/chrony 配置源和服务器。
- Ecosystem：`nas_qnap_ecosystem` 显示 QKVM/Virtualization Station、HBS、iSCSI、certificate 与 UPS 适配器状态；`nas_ups` 在 NUT `upsc` 可用时返回实际 UPS inventory、battery、runtime、input 和 status 值。`nas_certificate_inspect` 可直接解析指定 PEM/CRT 的公开 X.509 元数据。对于已通过真机 probe 验证的私有命令，可在 `qnap_adapters` 配置后使用 `nas_vm_action`、`nas_hbs_action`、`nas_iscsi_action` 与 `nas_certificate_action`；见 [ecosystem-adapters.md](ecosystem-adapters.md)。
- Accounts and shares：`nas_users` 返回主组与附加组关系；`nas_user_manage`、`nas_groups`、`nas_group_manage`（含 `member_add` / `member_remove`，`args` 为单个用户名）、`nas_share_list`、`nas_share_manage`、`nas_acl_get`、`nas_acl_set`。`nas_share_manage` 仅执行 probe 后配置的共享目录 argv adapter；不会把文件夹当作 QNAP shared folder。只有经 runtime probe 验证存在的系统工具会执行写操作。
- POSIX metadata：`nas_file_manage` 的 `chmod` 和 `chown` 支持 `recursive: true`；递归时不会跟随目录内符号链接。`chown` 的 `target` 使用 `uid:gid`，`chmod` 使用八进制 `mode`。

`observe`、`operate`、`admin` profile 仍可用于受限部署。它们保留 `allowed_roots`、命令列表、secret redaction 与 confirmation 模式。设置变更后重启 QPKG。
