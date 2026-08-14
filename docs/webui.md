# WebUI

WebUI 地址是 `http://NAS_IP:8756/`。输入 Bearer token 后点击连接并刷新。token 只保留在当前页面输入状态，不会写入 NAS、浏览器存储或页面源码。

仪表盘会并发读取并显示：agent/profile、QTS/QuTS hero discovery、CPU load、内存、温度与风扇、磁盘、RAID、QTS storage inventory、Container Station 容器数量、QPKG、运行中 Job、adapter capability 和最近审计记录。Docker、QPKG 或审计不可用时，其他卡片仍会显示，避免一个子系统错误导致整个页面不可用。

页面还会生成当前地址对应的 MCP bridge 配置，并展示 Agent 操作流程：先诊断，再选择 Docker/QPKG/API 或 `nas_exec`/`nas_shell`，长操作通过 Job 和审计追踪。API 在 `full_trust` 仍保留真实数据能力；WebUI 不主动展示 container secret、私钥和 token。
