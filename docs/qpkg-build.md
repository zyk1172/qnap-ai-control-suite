# QPKG 构建说明

## 当前脚本

```bash
./scripts/package_qpkg.sh amd64
```

流程：

1. 交叉编译 Linux agent。
2. 复制二进制、`qpkg.cfg`、服务脚本到 QDK staging 目录。
3. 如果存在 `tools/QDK/shared/bin/qbuild` 或系统 `qbuild`，调用 QDK 生成正式 `.qpkg`。
4. 如果不存在 `qbuild`，生成 staging archive。

## 正式 QPKG 的前提

推荐直接下载 QNAP QDK 到项目目录：

```bash
cd /path/to/qnap-ai-control-suite
gh repo clone qnap-dev/QDK tools/QDK
make -C tools/QDK/src
```

QDK 来源：

```text
https://github.com/qnap-dev/QDK
```

当前验证的 QDK release 是 `v2.5.2`。

检查：

```bash
test -x tools/QDK/shared/bin/qbuild
test -x tools/QDK/src/bin/qpkg_encrypt
```

脚本会自动生成一个本机用的 `tools/qdk-macos/bin/qbuild` 兼容副本，只修正 macOS 构建时的命令路径，不改变写入 QPKG 安装脚本的 QNAP 路径。

## 生成正式 QPKG

```bash
cd /path/to/qnap-ai-control-suite
./scripts/package_qpkg.sh amd64
```

输出：

```text
dist/QnapAIControl_0.3.3.qpkg
dist/QnapAIControl_0.3.3.qpkg.md5
```

手工校验包结构：

```bash
QP=dist/QnapAIControl_0.3.3.qpkg
SCRIPT_LEN=$(LC_ALL=C grep -a -m1 '^script_len=' "$QP" | sed 's/script_len=//')
CTRL_LEN=$(LC_ALL=C grep -a -m1 'offset=.*script_len +' "$QP" | sed 's/.*script_len + \([0-9]*\)).*/\1/')
DATA_LEN=$(LC_ALL=C grep -a -m1 'f.seek(' "$QP" | sed 's/.*f.seek(\([0-9]*\)).*/\1/')
dd if="$QP" bs="$SCRIPT_LEN" skip=1 2>/dev/null | tar -xO 2>/dev/null | tar -tzf -
OFFSET=$((SCRIPT_LEN + CTRL_LEN))
dd if="$QP" bs=1 skip="$OFFSET" count="$DATA_LEN" of=/tmp/qacs-data.tar.gz 2>/dev/null
tar -tzf /tmp/qacs-data.tar.gz
```

## 当前版本信息

```text
QPKG_NAME=QnapAIControl
QPKG_VER=0.3.3
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
