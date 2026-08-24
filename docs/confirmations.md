# 审批模式

v2 将权限与审批分开：`full_trust` 仍允许任意命令和 root 文件路径，但默认只对 `SENSITIVE` 操作暂停。

- `off`：不暂停；仍写统一审计。
- `sensitive_only`：默认。READ 直通，WRITE 直通并审计，SENSITIVE 返回 `409 approval_required`。
- `all_write`：所有非读取操作都需要批准。

敏感响应包含 10 分钟有效的单次 `approval_id` 和请求摘要。询问用户并得到文字批准后，使用**同一个工具、相同参数**加 `approval_id` 重试；不可通过换路径、改参数或重复使用票据绕过。`dry_run` 同样经过风险判断。

敏感操作包括电源、固件安装、破坏性存储/快照恢复、关键网络变更、QPKG remove、管理员删除或重置密码、Docker destructive prune，以及识别出的 `mkfs`、`wipefs`、块设备 `dd`、`zfs destroy`、`mdadm --zero-superblock` 等 raw command。
