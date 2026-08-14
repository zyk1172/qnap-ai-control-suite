# QPKG 构建

`scripts/package_qpkg.sh amd64` 读取根目录 `VERSION`，调用 QNAP QDK `qbuild`，并输出同版本的 `.qpkg` 与 md5。QDK 可以放在 `tools/QDK`；没有 qbuild 时输出不可安装的 staging archive。

```bash
./scripts/package_qpkg.sh amd64
sh -n qpkg/shared/qnap-ai-control-agent.sh
```

QPKG service 以 `exec` 启动 agent，PID file 对应真实进程；停止时先发送 SIGTERM 并等待，再作为最后手段发送 SIGKILL。
