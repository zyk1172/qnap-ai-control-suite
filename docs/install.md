# 安装和部署

## 1. 构建 agent

QNAP x86_64/amd64 机型：

```bash
cd /path/to/qnap-ai-control-suite
./scripts/build_agent.sh
```

ARM64 机型：

```bash
GOARCH=arm64 ./scripts/build_agent.sh
```

输出：

```text
dist/linux-amd64/qnap-ai-control-agent
```

## 2. 生成 QPKG

正式 `.qpkg` 需要 QNAP QDK 的 `qbuild`。

```bash
./scripts/package_qpkg.sh amd64
```

如果没有 `qbuild`，脚本只会生成：

```text
dist/QnapAIControl-0.3.2-linux-amd64.qpkg-staging.tar.gz
```

这只是检查包结构用的 staging 包，不能当正式 QPKG 安装。

## 3. 安装到 QNAP

生成正式 `.qpkg` 后：

1. 打开 QNAP App Center。
2. 选择手动安装。
3. 上传 `QnapAIControl_0.3.2.qpkg`。
4. 启动 `QNAP AI Control` 套件。

首次启动会生成：

```text
/etc/config/qnap-ai-control-agent/config.json
/etc/config/qnap-ai-control-agent/initial-token.txt
/var/log/qnap-ai-control-agent/audit.jsonl
```

## 4. WebUI 设置 token

打开：

```text
http://NAS_IP:8756/
```

把 token 粘贴到 WebUI 左侧输入框，点击 `保存到浏览器`，再点击 `测试连接`。

WebUI 只把 token 保存到当前浏览器 `localStorage`，不会写回 NAS 配置。

## 5. Mac 端连接

从 NAS 读取 token：

```bash
cat /etc/config/qnap-ai-control-agent/initial-token.txt
```

在 Mac 设置：

```bash
export QACS_BASE_URL=http://NAS_IP:8756
export QACS_TOKEN='上一步读取的 token'
```

健康检查：

```bash
curl -H "Authorization: Bearer $QACS_TOKEN" "$QACS_BASE_URL/v1/health"
```
