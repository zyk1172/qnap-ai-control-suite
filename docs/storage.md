# 存储和硬件

`nas_storage_overview` 汇总 disks、RAID、ZFS pools、mount volumes 和 snapshots。单独工具包括 `nas_disks`、`nas_disk_smart`、`nas_disk_smart_test`、`nas_raid`、`nas_storage_pool`、`nas_volume`、`nas_snapshots` 和 `nas_snapshot_manage`。SMART long/short test 与 snapshot action 都以 Job 返回，可通过 `nas_job_get` 和 `nas_job_logs` 跟踪。

`nas_system_thermal` 同时读取 QNAP `getsysinfo` 和 Linux hwmon，循环读取所有可报告的 CPU、system、fan 与 disk sensors。

`nas_discovery` 会报告 SMART、mdadm、ZFS/zpool 是否被真机探测到。QTS/QuTS hero 私有存储接口在 probe 证明前不会伪造写操作；需要深度 RAID、pool、volume、snapshot、iSCSI 或 LUN 操作时，使用 `nas_exec`/`nas_shell` 调用已由该 NAS 实际发现的工具，并先运行 probe 保存证据。
