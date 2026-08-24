# 审批模式

v2 将权限与审批分开：`full_trust` 仍允许任意命令和 root 文件路径，但默认只对 `SENSITIVE` 操作暂停。

- `off`：不暂停；仍写统一审计。
- `sensitive_only`：默认。READ 直通，WRITE 直通并审计，SENSITIVE 返回 `409 approval_required`。
- `all_write`：所有非读取操作都需要批准。

敏感响应包含 10 分钟有效的单次 `approval_id` 和请求摘要。MCP Bridge 捕获该响应后，通过标准 MCP form-mode elicitation 向用户显示操作、目标、风险和摘要，并提供“允许这一次/拒绝”。用户批准后，由 **Bridge 内部**调用 `POST /v1/approvals/{approval_id}/decision` 写入决定，再自动重试一次完全相同的原始 HTTP 请求并携带 `X-QACS-Approval-ID`；模型不能调用批准工具，也不能自行把 `approval_id` 当成批准。用户拒绝时不重试。不支持交互 elicitation 的 MCP client 会明确 fail closed。不可通过换路径、改参数或重复使用票据绕过。`dry_run` 同样经过风险判断。

Hermes 等客户端如果返回 `action=accept` 且 `content={}`，Bridge 将其解释为“允许这一次”；`decline`/`cancel` 解释为拒绝，顶层拒绝动作优先于任何矛盾的 `content` 字段。审批票据仍由 NAS 端校验绑定、过期和 single-use。审批等待默认 300 秒，并按票据剩余 TTL 进行上限裁剪；可用 `QACS_APPROVAL_TIMEOUT_MS` 配置，最大 9 分钟。

`raw` toolset 是 break-glass 模式：`nas_exec` 和 `nas_shell` 保留 full-trust 的 root 控制能力。Agent 可以通过未被有限危险命令模式识别的替代命令或脚本实现同等破坏效果，因此 raw 的模式识别不是绝对安全边界。若要求所有 raw 行为都必须审批，应将整个 raw toolset 视为 SENSITIVE，但这会牺牲完整控制下的低打扰体验。

审计中，`status` 表示实际 HTTP 操作结果；审批生命周期另由 `approval_event`（例如 `approval_requested`、`approval_approved`、`approval_executed`）和 `approval_status` 表示。因此审批已消费但 QNAP handler 返回 5xx 时，记录会是 `status=failed`、`approval_event=approval_executed`，不会伪装成成功。

敏感操作包括电源、固件安装、破坏性存储/快照恢复、关键网络变更、QPKG remove、管理员删除或重置密码、Docker destructive prune，以及识别出的 `mkfs`、`wipefs`、块设备 `dd`、`zfs destroy`、`mdadm --zero-superblock` 等 raw command。
