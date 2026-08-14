# 存储和硬件

`nas_storage_overview` 汇总 disks、RAID、ZFS pools、mount volumes、snapshots 与 QTS snapshot backend。单独工具包括 `nas_disks`、`nas_disk_smart`、`nas_disk_smart_test`、`nas_raid`、`nas_raid_manage`、`nas_storage_pool`、`nas_volume`、`nas_snapshots`、`nas_snapshot_backend` 和 `nas_snapshot_manage`。SMART long/short test 与 snapshot action 都以 Job 返回，可通过 `nas_job_get` 和 `nas_job_logs` 跟踪。

`nas_raid` 解析完整的 `/proc/mdstat` array record，返回成员、`[total/active]` 位图、降级状态，以及正在进行的 recovery/resync/reshape/check/repair 的百分比、速度和预估完成时间。`nas_raid_manage` 仅在发现标准 Linux mdraid `sync_action` 时提供 `scrub_start`（写入 `check`）和 `scrub_stop`（写入 `idle`）；它不声称控制 QTS pool、volume 或 RAID 创建/迁移。操作前后会返回 `previous`、`current` 和 `applied`。

在 ZFS/QuTS hero 上，`nas_snapshot_manage` 支持 `create`、`delete`、`clone` 与 `restore`；`restore` 执行标准 `zfs rollback` 并始终作为 Job 追踪。在 QTS 上，`nas_snapshot_backend` 在本机发现 `snapshot_util` 时声明 `create`；创建时传绝对 `volume` mount path 与 `name`，agent 按已验证的 `get_volume_id`、`check_volume`、`create_snapshot_for_app` 调用。QTS `qcli_volumesnapshot` 提供 list/delete/restore/clone，但这些命令必须先登录 QCLI 获得 `sid` 或保存 auth 会话；未配置前 MCP 不会自动调用，也不会伪造可用性。

`nas_system_thermal` 同时读取 QNAP `getsysinfo` 和 Linux hwmon，循环读取所有可报告的 CPU、system、fan 与 disk sensors。

`nas_discovery` 会报告 SMART、mdadm、ZFS/zpool 是否被真机探测到。QTS/QuTS hero 私有存储接口在 probe 证明前不会伪造写操作；需要深度 RAID、pool、volume、snapshot、iSCSI 或 LUN 操作时，使用 `nas_exec`/`nas_shell` 调用已由该 NAS 实际发现的工具，并先运行 probe 保存证据。
