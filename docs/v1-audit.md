# v1 审计

审计基线为 `v0.3.2`。仓库当时包含单体 Go handler、手写 MCP JSON-RPC、QPKG shell service、文档和打包脚本。面向使用者的当前能力清单见 [v0.3 到 v1.0.15 对比](v0.3-v1.0.15-comparison.md)。

## 原有能力

- 系统概览、进程、温度、审计尾部。
- `/share` 文件读写、allowlist command、QPKG 和 Docker 子集。
- prepare/confirm 以及 Mac stdio bridge。

## 修复的问题

- `runCommand` 将 `exec.ExitError` 当作可成功序列化的响应，导致非零退出缺少明确失败语义。
- command 不支持 `cwd`/env/二进制 stdin，输出无可靠截断状态。
- 文件桥接读取后强制 UTF-8 解码，破坏 binary 内容。
- 仅清理字符串路径，未解析 symlink 后验证允许根目录。
- QPKG 启动记录 sub-shell PID，可能出现 App Center stopped 但旧 agent 占用端口。
- MCP 固定声明 `2024-11-05`，版本号散落在 README、QPKG、package 和脚本。

## v1 结论

基础层已替换为 `internal/config`、`exec`、`files`、`jobs`、`audit`、`api` 和 QNAP adapter。QNAP 私有功能只在 discovery/probe 证明可用后声明支持；未知接口不会伪造实现。
