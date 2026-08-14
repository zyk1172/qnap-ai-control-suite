# 存储和硬件

`nas_storage_overview` 读取 filesystem、mount 和 `/proc/mdstat`。`nas_system_thermal` 同时读取 QNAP `getsysinfo` 和 Linux hwmon。

`nas_discovery` 会报告 SMART、mdadm、ZFS/zpool 是否被真机探测到。QTS/QuTS hero 私有存储接口在 probe 证明前不会伪造写操作；需要深度 RAID、pool、volume、snapshot、iSCSI 或 LUN 操作时，使用 `nas_exec`/`nas_shell` 调用已由该 NAS 实际发现的工具，并先运行 probe 保存证据。
