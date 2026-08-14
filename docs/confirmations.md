# 确认模式

v1 由配置控制确认，不在 MCP bridge 中硬编码 prepare/confirm：

- `off`：直接执行，QPKG 默认 `full_trust` 使用此模式。
- `destructive_only`：Docker/QPKG destructive action 返回 `409 confirmation_required`。
- `all_write`：保留给需要额外审批层的受限部署。

无论模式如何，认证和 JSONL audit 均持续生效。
