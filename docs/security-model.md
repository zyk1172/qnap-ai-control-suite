# 安全模型

所有 `/v1/*` 路由均要求 Bearer token，并记录 JSONL audit event。token 只保存 SHA-256 hash；首次 token 文件权限为 `0600`。

`full_trust` 明确选择了完整控制，不做强制 secret redaction 或 destructive confirmation。审计默认记录完整参数。该模式仅适合物理可信或 VPN 保护的 LAN。

受限 profile 可设置：

- `allowed_roots`：文件系统边界，实际路径会解析 symlink 后验证。
- `allow_any_command`/`allowed_commands`：argv command 边界。
- `allow_shell`：是否允许 `/bin/sh -c`。
- `redact_secrets`：Docker 与审计隐私显示策略。
- `confirmation.mode`：`off`、`destructive_only`、`all_write`。

无论 profile 如何，所有操作均应保留 agent audit log。不要将 token、容器环境变量、私钥或备份数据提交到 GitHub。
