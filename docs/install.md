# 安装和升级

## 构建

QNAP TS-264C 使用 amd64：

```bash
./scripts/build_agent.sh
./scripts/package_qpkg.sh amd64
```

脚本从仓库 `VERSION` 读取版本，并注入 agent、QPKG 元数据和 artifact 名。正式包输出为：

```text
dist/QnapAIControl_1.0.13.qpkg
dist/QnapAIControl_1.0.13.qpkg.md5
```

需要 QNAP QDK `qbuild`。没有 QDK 时只会生成 staging archive，不能上传 App Center。

## App Center

1. 上传正式 `.qpkg` 并安装。
2. 启动 `QNAP AI Control`。
3. 打开 `http://NAS_IP:8756/`。
4. 从 `/etc/config/qnap-ai-control-agent/initial-token.txt` 读取 token，填入 WebUI。

首次安装生成 `full_trust` v1 配置。原有 0.3.x 配置会保留 Bearer Token hash、监听地址、日志路径、文件大小上限、命令超时和 Docker 路径，并自动迁移为 `full_trust` v1 设置；升级不会因旧字段而启动失败，也不会继续保留旧 allowlist 的受限行为。

## 真机验证与集成检查

先通过 MCP 调用 `nas_qnap_probe` 并传入 `{ "output_path": "/share/Public/qnap-probe.json" }`。脚本现已随 QPKG 安装在 agent binary 同一目录，无需手工复制仓库文件。也可在 NAS shell 以 QPKG install path 下的 `bin/qnap-ai-control-probe /share/Public/qnap-probe.json` 运行。

安装并从 WebUI 复制 Bearer Token 后，在可信 LAN 的 Mac 或 NAS shell 运行：

```bash
export QNAP_AI_CONTROL_URL="http://NAS_IP:8756"
export QNAP_AI_CONTROL_TOKEN="从 WebUI 复制的 token"
./scripts/qnap_integration_test.sh
```

兼容旧自动化：`QACS_BASE_URL` 和 `QACS_TOKEN` 仍可替代上述环境变量。probe 用于确认 QTS/QuTS hero、Container Station、SMART、RAID、ZFS 和 QNAP 命令可用性，并为私有 adapter 提供真实可执行路径证据。集成脚本只读取核心 API，并对 executor 和 shell 执行 `dry_run`；不会修改 NAS。Docker、QPKG、UPS、账户、共享和生态适配器会在对应组件不存在时显示 `SKIP`，核心端点失败则返回非零状态。
