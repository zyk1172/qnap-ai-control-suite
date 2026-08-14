# WebUI

WebUI 地址是 `http://NAS_IP:8756/`。输入 Bearer token 后点击连接与刷新。

仪表盘显示：agent profile、NAS hostname、QTS/QuTS hero 发现结果、Docker/SMART capability，以及完整配置摘要。token 只保存浏览器当前 tab 的输入状态，不会写入 NAS 或页面源码。

WebUI 的 MCP 段落会生成当前地址对应的 bridge 配置。API 保持 full-trust 的真实数据能力；WebUI 不主动展示 container secret、私钥和 token。
