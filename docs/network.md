# 网络

`nas_network_info` 返回综合视图；更适合 agent 的结构化工具包括 `nas_network_interfaces`、`nas_network_routes`、`nas_network_dns` 和 `nas_virtual_switches`。它们读取真实接口、MAC、地址、link/speed/duplex、内核路由、DNS 以及 bridge/bond/VLAN 设备。

QNAP Virtual Switch 的私有命令或本机 API 不同版本差异较大，先通过 `nas_discovery` 和 `scripts/qnap_probe.sh` 确认实际 utilities 后，再通过 `nas_exec` 执行对应的已验证命令。

远程接入应使用 VPN 或私有隧道。不要把 agent HTTP 端口直接暴露到公网。
`nas_network_manage` 使用已经探测到的 Linux `ip` CLI，支持 `set_mtu`、`set_state`、`address_add`、`address_delete`、`route_add` 和 `route_delete`。接口返回操作前后的 interface/route 快照及实际 argv。此接口明确标记为 `transient_linux_ip`：它不会假装修改了 QTS Network & Virtual Switch 的持久化配置。需要 QTS 专有的 DHCP、DNS、bond 或 Virtual Switch 配置时，先用 `nas_discovery` 检查 runtime capability，再使用 `nas_exec` 或 `nas_shell` 调用经真机验证的 QNAP 工具。
