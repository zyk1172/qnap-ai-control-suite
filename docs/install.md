# 安装和升级

## 构建

QNAP TS-264C 使用 amd64：

```bash
./scripts/build_agent.sh
./scripts/package_qpkg.sh amd64
```

脚本从仓库 `VERSION` 读取版本，并注入 agent、QPKG 元数据和 artifact 名。正式包输出为：

```text
dist/QnapAIControl_2.1.0.qpkg
dist/QnapAIControl_2.1.0.qpkg.md5
```

需要 QNAP QDK `qbuild`。没有 QDK 时只会生成 staging archive，不能上传 App Center。

## App Center

1. 上传正式 `.qpkg` 并安装。
2. 启动 `QNAP AI Control`。
3. 打开 `http://NAS_IP:8756/`。
4. 使用当前 Bearer Token 打开 WebUI。v2.1 的正式 Token 文件是 `/etc/config/qnap-ai-control-agent/token`，权限为 `0600`；新安装不创建 `initial-token.txt`，旧版本该文件只有在与 `config.json` hash 匹配时才由 Go Agent 迁移并移除。

WebUI 管理端点要求当前 Bearer Token。本项目不会假设 QNAP App Center 打开的页面自动提供可信 QTS session；当前 QPKG 的 `QPKG_WEBUI=/` 是直接 8756 服务，没有经过已验证的 QTS 身份代理。如果旧系统只有 `config.json` 中的 hash、明文 Token 文件已丢失，则无法反推出 Token；停止 QPKG 后可在 NAS 本机以有权访问配置目录的用户执行：

```bash
/path/to/qnap-ai-control-agent -config /etc/config/qnap-ai-control-agent/config.json -reset-token
```

命令会原子更新 Token 与 hash，并将新 Token 输出到当前终端；随后重新启动 QPKG。

Token 设置前会检查 Token 目录和 `config.json` 所在目录是否能够创建、同步和原子替换临时文件。检查失败会返回“存储不可写”，不切换运行时认证 hash，也不删除旧 Token。成功更新后旧 Token 立即失效。

首次安装生成 `full_trust` v2 配置与 `approval.mode=sensitive_only`。升级会保留 Bearer Token、监听地址、日志路径、文件大小上限、命令超时和 Docker 路径；没有独立 `approval` 的 v1 配置会自动迁移为 `sensitive_only`，包括旧 `confirmation.mode=off`。升级过程中不会因缺少明文 Token 而随机重置认证。

## 真机验证与集成检查

先通过 MCP 调用 `nas_qnap_probe` 并传入 `{ "output_path": "/share/Public/qnap-probe.json" }`。脚本现已随 QPKG 安装在 agent binary 同一目录，无需手工复制仓库文件。也可在 NAS shell 以 QPKG install path 下的 `bin/qnap-ai-control-probe /share/Public/qnap-probe.json` 运行。

安装并从 WebUI 复制 Bearer Token 后，在可信 LAN 的 Mac 或 NAS shell 运行：

```bash
export QNAP_AI_CONTROL_URL="http://NAS_IP:8756"
export QNAP_AI_CONTROL_TOKEN="从 WebUI 复制的 token"
./scripts/qnap_integration_test.sh
```

兼容旧自动化：`QACS_BASE_URL` 和 `QACS_TOKEN` 仍可替代上述环境变量。probe 用于确认 QTS/QuTS hero、Container Station、SMART、RAID、ZFS 和 QNAP 命令可用性，并为私有 adapter 提供真实可执行路径证据。集成脚本只读取核心 API，并对 executor 和 shell 执行 `dry_run`；不会修改 NAS。Docker、QPKG、UPS、账户、共享和生态适配器会在对应组件不存在时显示 `SKIP`，核心端点失败则返回非零状态。
