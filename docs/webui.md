# WebUI、Token 与权限

WebUI 地址是 `http://NAS_IP:8756/`。它是 Go binary 内嵌的静态页面，不需要 Node、CDN 或互联网。

## 访问边界

页面本身可以打开，但概览、接入、系统和日志数据都通过受 Bearer Token 保护的 `/v1/*` API 读取。当前 QPKG 配置是直接监听 `8756`，没有已验证的 QTS 反向代理 session 传递机制；QNAP App Center 打开的页面不会被本项目自动当作已登录 QTS 管理员，项目不虚构或猜测 QTS session 能力。首次打开后输入当前 Token，点击“连接”。

Token 只保存在当前页面运行时状态。Bridge 路径可以选择保存在浏览器 `localStorage`，仅用于生成 Mac 上的配置，不上传 NAS。

## 概览和按需加载

“概览”优先读取 `/v1/status/snapshot`，显示 Agent 版本、运行时间、CPU/Load、内存、温度、磁盘、RAID/卷、Docker、QPKG、Jobs 和最近异常。快照是 best-effort：Docker、SMART、SMB 或 QNAP 私有能力不可用时，对应模块显示“不可用”，不会让整页失败。

“日志”页面才读取审计、服务日志和 Jobs；页面只读取 tail/分页数据，不把整份日志加载进浏览器。浏览器端对状态快照使用短缓存，避免每次切换页面都重复扫描 NAS。

## Token 管理

在“接入”页可以：

- 查看当前 Token 是否已配置、是否可恢复、是否可写，以及脱敏后的值；
- 显示或复制当前明文 Token（仅当 Token 文件存在且 hash 与当前认证一致）；
- 设置自定义 Token；
- 生成新的随机 Token。

QPKG v2.1 使用以下文件：

```text
/etc/config/qnap-ai-control-agent/       0700（QPKG 专用目录）
/etc/config/qnap-ai-control-agent/token  0600  当前明文 Token
/etc/config/qnap-ai-control-agent/config.json  0600  只保存 token_sha256
```

新安装只写入正式的 `token` 文件。旧版本的 `initial-token.txt` 只作为迁移来源：它必须先通过 `config.json` 现有 hash 校验，不会覆盖已经存在的 `token`，校验失败也不会随机替换现有认证；成功迁移后旧文件被移除。只有 hash、没有匹配明文 Token 的旧安装会显示“仅 hash 可用”；此时不能恢复旧明文，只能使用“重新生成”。

TokenStore 只会把 `0700` 应用于自己新创建的专用目录。通过自定义配置路径使用一个已经存在的父目录时，不会强制修改整个父目录权限，只会将 Token 和配置文件写成 `0600`。

### 写入权限和失败行为

保存 Token 前，Agent 会在同一目录创建、写入、同步并删除临时文件，同时检查 `config.json` 是否是可原子替换的普通文件。页面或 API 返回“存储不可写”时，表示目录权限、只读挂载、路径类型或底层文件系统不允许完成安全更新；这时不会切换 AuthManager 的运行时 hash，也不会删除旧 Token。

实际更新顺序是：校验 Token → 原子写入 `token` → 原子替换只含 hash 的 `config.json` → 切换运行时认证。配置写入失败会恢复旧 Token 和旧配置，并重新读取验证；如果补偿写入或验证也失败，API 返回 `token_recovery_required`，不会切换运行时认证，必须立即通过受信任的本机维护方式检查两个文件。更新成功后旧 Token 立即失效，已配置的 Codex、Hermes、OpenClaw 和其他 MCP client 需要同步新 Token 并重启 MCP 子进程。

包含明文 Token 的响应带有 `Cache-Control: no-store, private` 和 `Pragma: no-cache`，避免浏览器或代理缓存 root bearer credential。

Token 管理 API 也要求当前 Token。当前 QPKG 直接端口没有可验证的 QTS 管理员 session，因此本版本不开放未经认证的“取回 Token”端点；如果既没有可用 Token、又不能从受信任安装环境取得初始 Token，需在停止 QPKG 后通过 NAS 本机维护命令重新生成：

```bash
/path/to/qnap-ai-control-agent -config /etc/config/qnap-ai-control-agent/config.json -reset-token
```

该命令以当前系统用户权限执行原子 Token/config 更新，并只把新 Token 输出到当前终端；随后重新启动 QPKG。把 root Token 永久公开给整个 LAN 会破坏本项目的认证边界，不能用一个猜测的 Referer、Cookie 或 App Center URL 冒充授权。

## MCP 配置生成

选择客户端后，接入页生成对应语法：

- 通用 MCP：JSON 根对象 `mcpServers`；
- Codex：TOML `[mcp_servers.qnap-ai-control]`；
- Hermes：YAML `mcp_servers`；
- OpenClaw：JSON5/JSON `mcp.servers`。

配置里的 `QACS_BASE_URL` 使用当前页面地址，`QACS_TOKEN` 使用当前页面内存中的 Token，`QACS_TOOLSETS` 使用推荐、最小、完整或自定义选择。Mac bridge 路径是运行 MCP bridge 的电脑上的路径，不是 NAS 路径，默认值只是占位符。

复制包含真实 Token 的配置后，不要提交 Git、截图或公共日志。修改 Token 后应重新打开接入页、复制新配置并重启各个 MCP client。

## 系统和完整控制

“系统”页显示 profile、审批模式、审批 TTL、Job 并发、权限根目录、shell/任意命令、审计和已验证 QNAP adapter。v2.1.0 先提供只读设置展示；配置修改仍通过受控配置文件/升级流程完成，避免 UI 保存半套运行时配置。

`raw` toolset 仍是 break-glass 完整控制能力。它保留 `nas_exec`/`nas_shell` 等 root 能力，但有限危险命令识别不能覆盖所有通过脚本、解释器或其他 QTS 命令实现的破坏路径。详见 [安全模型](security-model.md) 和 [完整控制能力](qnap-full-control.md)。
