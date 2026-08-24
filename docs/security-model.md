# 安全模型

所有 `/v1/*` 路由均要求 Bearer token，并记录 JSONL audit event。token 只保存 SHA-256 hash；首次 token 文件权限为 `0600`。

`full_trust` 明确选择了完整控制：可访问 `/`、任意 argv 和 shell；它不再隐式关闭审批或日志脱敏。默认 `approval.mode=sensitive_only`，审计保留脱敏摘要。该模式仅适合物理可信或 VPN 保护的 LAN。

受限 profile 可设置：

- `allowed_roots`：文件系统边界，实际路径会解析 symlink 后验证。
- `allow_any_command`/`allowed_commands`：argv command 边界。
- `allow_shell`：是否允许 `/bin/sh -c`。
- `redact_secrets`：Docker 与审计隐私显示策略。
- `approval.mode`：`off`、`sensitive_only`、`all_write`，独立于权限 profile。

v1 的 `confirmation` 仅为迁移兼容字段。没有 `approval` 的 v1 配置（包括 `full_trust` 的 `confirmation.mode=off`）会迁移为 `approval.mode=sensitive_only`。

敏感请求返回一次性 `approval_id`，它绑定 method、path 和 canonical JSON 参数，10 分钟后过期且只能消费一次。MCP Bridge 将 `approval_required` 转为标准 MCP elicitation，用户批准后由 Bridge 内部调用 decision endpoint，再自动重试一次完全相同的原始 HTTP 请求；模型不能调用批准接口或自行批准。篡改、重放、过期和拒绝均会失败。不支持交互审批的客户端会 fail closed。

无论 profile 如何，所有操作均应保留 agent audit log。不要将 token、容器环境变量、私钥或备份数据提交到 GitHub。
