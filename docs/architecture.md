# 架构

```text
MCP client -> official Node MCP bridge -> authenticated HTTP -> Go agent
                                                   |-> executor
                                                   |-> files
                                                   |-> jobs/audit
                                                   |-> capability resolver
                                                   |-> QNAP discovery/docker/qpkg/ecosystem adapters
```

QACS 是 Hermes、OpenClaw、Codex 等 Agent 的确定性工具/控制层，不包含第二层 Agent、Planner 或自主 Desired-State 循环。上层 Agent 负责理解目标和选择工具，QACS 负责发现能力、执行操作、验证底层结果并保存必要的运行状态。

Go agent 不依赖外部数据库。短操作直接通过 HTTP 执行；长操作由 Job manager 管理，并使用受限 JSONL journal、原子 snapshot 和逐 Job 日志文件持久化生命周期状态。journal 达到阈值后会压缩为 snapshot，避免无限追加。QPKG 默认把 Job durable state 放在 `/etc/config/qnap-ai-control-agent/jobs/`，使 QPKG restart 和 NAS reboot 后仍可加载。

Agent 启动时会加载 snapshot + journal。此前处于 `queued` 或 `running` 的任务不会被盲目重新执行，而是转为：

```text
status=interrupted
recovered=true
recovery_status=needs_inspection
retriable=false
```

这表示 QACS 已恢复“曾经执行过什么”的事实，但需要上层 Agent 根据真实资源状态决定是否重新调用、验证或采取其他动作。对于固件、存储删除、snapshot restore 等操作尤其不会自动重放。

Job 日志和 command 输出都有上限。持久化的普通结构化 Result 会先经过现有 audit sanitize 规则并限制大小；原始 command stdout/stderr 不写入 Job journal，只保存 exit code、耗时、截断状态和字节数等摘要。HTTP 使用读写超时和 SIGTERM/SIGINT graceful shutdown。

所有 API 都使用同一 envelope：

```json
{"ok":true,"data":{},"meta":{"request_id":"...","duration_ms":1}}
```

失败响应为 `{ "ok": false, "error": { "code", "message", "details" }, "meta" }`。command 失败会区分 `non_zero_exit`、`timeout`、`not_found`、`start_failed`。
