# 网络

`nas_network_info` 返回综合视图；更适合 agent 的结构化工具包括 `nas_network_interfaces`、`nas_network_routes`、`nas_network_dns` 和 `nas_virtual_switches`。它们读取真实接口、MAC、地址、link/speed/duplex、内核路由、DNS 以及 bridge/bond/VLAN 设备。

QNAP Virtual Switch 的私有命令或本机 API 不同版本差异较大，先通过 `nas_discovery` 和 `scripts/qnap_probe.sh` 确认实际 utilities 后，再通过 `nas_exec` 执行对应的已验证命令。

远程接入应使用 VPN 或私有隧道。不要把 agent HTTP 端口直接暴露到公网。
