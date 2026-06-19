# QPKG 构建说明

## 当前脚本

```bash
./scripts/package_qpkg.sh amd64
```

流程：

1. 交叉编译 Linux agent。
2. 复制二进制、`qpkg.cfg`、服务脚本到 QPKG staging 目录。
3. 如果存在 `qbuild`，调用 QDK 生成正式 `.qpkg`。
4. 如果不存在 `qbuild`，生成 staging archive。

## 正式 QPKG 的前提

需要安装 QNAP QDK，并且 `qbuild` 在 `PATH` 中。

检查：

```bash
command -v qbuild
qbuild --help
```

## 当前版本信息

```text
QPKG_NAME=QnapAIControl
QPKG_VER=0.2.0
QPKG_SERVICE_PROGRAM=qnap-ai-control-agent.sh
QPKG_WEB_PORT=8756
```

## 服务脚本

服务脚本：

```text
qpkg/shared/qnap-ai-control-agent.sh
```

它负责：

- 首次启动生成 token 和 token hash。
- 写入 `/etc/config/qnap-ai-control-agent/config.json`。
- 启动 agent 并写 PID。
- 停止或重启 agent。

