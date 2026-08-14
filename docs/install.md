# 安装和升级

## 构建

QNAP TS-264C 使用 amd64：

```bash
./scripts/build_agent.sh
./scripts/package_qpkg.sh amd64
```

脚本从仓库 `VERSION` 读取版本，并注入 agent、QPKG 元数据和 artifact 名。正式包输出为：

```text
dist/QnapAIControl_1.0.4.qpkg
dist/QnapAIControl_1.0.4.qpkg.md5
```

需要 QNAP QDK `qbuild`。没有 QDK 时只会生成 staging archive，不能上传 App Center。

## App Center

1. 上传正式 `.qpkg` 并安装。
2. 启动 `QNAP AI Control`。
3. 打开 `http://NAS_IP:8756/`。
4. 从 `/etc/config/qnap-ai-control-agent/initial-token.txt` 读取 token，填入 WebUI。

首次安装生成 `full_trust` v1 配置。原有 0.3.x 配置可被 agent 自动读取并迁移为内存 v1 设置；升级不会因旧字段而启动失败。

## 真机验证

在 NAS 上运行：

```sh
./scripts/qnap_probe.sh /share/Public/qnap-probe.json
QACS_BASE_URL=http://127.0.0.1:8756 QACS_TOKEN='...' ./scripts/qnap_integration_test.sh
```

probe 用于确认 QTS/QuTS hero、Container Station、SMART、RAID、ZFS 和 QNAP 命令可用性。
