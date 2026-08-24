# 安全模型

所有 `/v1/*` 路由均要求 Bearer token，并记录 JSONL audit event。运行时认证使用线程安全的 AuthManager；配置文件只保存 SHA-256 hash，当前明文 Token 单独保存于 `/etc/config/qnap-ai-control-agent/token`，文件权限为 `0600`，QPKG 专用目录为 `0700`。新安装不创建旧版 `initial-token.txt`；升级时该文件只有在 hash 校验通过后才用于迁移。

WebUI 的 Token 管理 API 也受当前 Bearer Token 保护。QPKG 当前通过 8756 直接监听，尚未验证 QNAP App Center 能向该服务传递可验证的管理员 session；本项目不会伪造或猜测 QTS 登录态，也不会开放匿名 root Token reveal。没有当前 Token 时，hash-only 状态无法显示明文，只能在受信任环境读取或生成新 Token。

设置或生成 Token 前，TokenStore 会在同一目录创建、写入、同步并删除临时文件，检查目录是否可写以及 `config.json` 是否可被原子替换。真正更新时先原子写入 Token，再原子替换只含 hash 的配置；配置写入失败会恢复旧 Token 和旧配置，并重新读取验证。并发轮换在 Server 级别串行化，第二个已经通过外层认证但使用旧 Token 的请求会收到 `stale_token`，不会提交第二次轮换。进程崩溃留下的短暂不一致会在下次启动时由 TokenStore 根据正式明文 Token 重新对齐。运行时只有持久化成功后才切换 AuthManager，因此普通权限/路径错误不会把服务锁死。

包含明文 Token 的响应带有 `Cache-Control: no-store, private` 和 `Pragma: no-cache`。

`full_trust` 明确选择了完整控制：可访问 `/`、任意 argv 和 shell；它不再隐式关闭审批或日志脱敏。默认 `approval.mode=sensitive_only`，审计保留脱敏摘要。该模式仅适合物理可信或 VPN 保护的 LAN。

受限 profile 可设置：

- `allowed_roots`：文件系统边界，实际路径会解析 symlink 后验证。
- `allow_any_command`/`allowed_commands`：argv command 边界。
- `allow_shell`：是否允许 `/bin/sh -c`。
- `redact_secrets`：Docker 与审计隐私显示策略。
- `approval.mode`：`off`、`sensitive_only`、`all_write`，独立于权限 profile。

v1 的 `confirmation` 仅为迁移兼容字段。没有 `approval` 的 v1 配置（包括 `full_trust` 的 `confirmation.mode=off`）会迁移为 `approval.mode=sensitive_only`。

敏感请求返回一次性 `approval_id`，它绑定 method、path 和 canonical JSON 参数，10 分钟后过期且只能消费一次。MCP Bridge 将 `approval_required` 转为标准 MCP elicitation，用户批准后由 Bridge 内部调用 decision endpoint，再自动重试一次完全相同的原始 HTTP 请求；模型不能调用批准接口或自行批准。篡改、重放、过期和拒绝均会失败。不支持交互审批的客户端会 fail closed。

`raw` toolset 是有意保留的 break-glass 逃生舱。`nas_exec`/`nas_shell` 在 full_trust 下拥有完整 root 命令能力，当前危险命令识别只能覆盖已知模式，不能保证识别所有通过其他 QNAP 命令、解释器或脚本实现的破坏路径。若需要绝对审批边界，必须将 raw 命令整体归类为 SENSITIVE。

无论 profile 如何，所有操作均应保留 agent audit log。不要将 token、容器环境变量、私钥或备份数据提交到 GitHub。
