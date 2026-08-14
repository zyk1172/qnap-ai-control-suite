# 架构

```text
MCP client -> official Node MCP bridge -> authenticated HTTP -> Go agent
                                                   |-> executor
                                                   |-> files
                                                   |-> jobs/audit
                                                   |-> QNAP discovery/docker/qpkg adapters
```

Go agent 不依赖数据库。长操作由内存 Job manager 管理，日志和 command 输出都有上限。HTTP 使用读写超时和 SIGTERM/SIGINT graceful shutdown。

所有 API 都使用同一 envelope：

```json
{"ok":true,"data":{},"meta":{"request_id":"...","duration_ms":1}}
```

失败响应为 `{ "ok": false, "error": { "code", "message", "details" }, "meta" }`。command 失败会区分 `non_zero_exit`、`timeout`、`not_found`、`start_failed`。
