# 网络

`nas_network_info` 返回 interfaces、routes 和 DNS 配置。QNAP Virtual Switch 的私有命令或本机 API 不同版本差异较大，先通过 `nas_discovery` 和 `scripts/qnap_probe.sh` 确认实际 utilities 后，再通过 `nas_exec` 执行对应的已验证命令。

远程接入应使用 VPN 或私有隧道。不要把 agent HTTP 端口直接暴露到公网。
