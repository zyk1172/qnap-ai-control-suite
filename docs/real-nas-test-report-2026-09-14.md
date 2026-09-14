# QACS 真实 QNAP NAS 系统性测试与复核报告

测试日期：2026-09-13 至 2026-09-14（Asia/Shanghai）  
测试方式：通过已安装的 QACS MCP bridge 连接真实生产 NAS；Agent 场景使用 Pi，以自然语言目标驱动，不预先指定工具名。  
安全边界：只重启 QACS 自身；只创建并清理 `qacs-test-*` 文件、目录、Job 和 disposable Container；未重启 NAS、Container Station、生产 QPKG 或生产容器，未执行网络、RAID、Pool、Volume、Snapshot、VM、HBS、固件等生产写操作。

所有 Token、密码和容器环境变量值均未写入本报告。Token 只在 NAS 上以 hash 形式比较，报告只保留脱敏后的状态和 hash 前缀。原始结构化采集结果保留在本机临时证据目录 `/private/tmp/qacs-real-nas-test.unpMPj/`，未提交到仓库。

## 1. Executive Summary

总体结论：**PASS WITH ISSUES**。

当前已合并的 `main`（PR #8 合并后的 `b8c2baa`）在真实 NAS 上已经证明：QACS 控制面、MCP toolset、Job 持久化、interrupted recovery、Docker 只读 inventory、文件 CAS、网络/存储只读观测和 Token rotation 可以工作，并且本轮没有发现生产 NAS 配置被意外修改。

但本轮又确认了两个需要进入后续 PR 的真实问题：

1. 2.1.2 的 Storage Manager parser 会把真实 QTS `qcli_storage -v` 的标题行 `VolID VolName Mount_Path` 当成一个 volume；修复已放在 [PR #9](https://github.com/zyk1172/qnap-ai-control-suite/pull/9)，尚未合并，因此当前 NAS 仍可复现该问题。
2. 2.1.2 的 QACS 自重启请求同步执行 stop，客户端在 HTTP acknowledgement 之前被进程终止，MCP 看到 `fetch failed`；QACS 实际会重新启动，但调用方无法得到确定的 accepted 状态。本 PR 增加 scheduled acknowledgement 和回归测试；修复包尚未被 QTS 安装队列真正部署到 NAS。

因此，当前 `main` 可以继续用于受控的只读观测、低风险测试写和已有 Job 控制，但不应在 Storage Manager typed output 或 QACS self-restart 的 transport 语义修复合并、部署前，宣称四个控制面整改已经完全闭环。QACS 仍然只是 MCP/控制层；本轮没有在 QACS 内加入 Planner、Agent、自然语言决策或自动 replay。

| PR | 结论 | 说明 |
| --- | --- | --- |
| #1 控制面 | PASS WITH ISSUES | Approval、Audit/Redaction、Job 幂等/锁/并发/取消、状态快照、文件 CAS、Docker inventory 和兼容 alias 通过；Storage header bug 和 self-restart acknowledgement 仍需后续 PR。 |
| #4 Capability Resolver | PASS | 已安装不等于可用；没有 verified backend 的能力均返回 degraded/unavailable，未伪装成 available。Storage read path 与 UPS read path 已 verified。 |
| #5 Structured MCP | PASS WITH ISSUES | typed tools、toolset、enum/schema validation、generic fallback 和 Agent 自主选择通过；Storage Manager 当前版本的标题行解析错误已由 PR #9 修复但尚未部署。 |
| #6 Persistence / Recovery | PASS WITH ISSUES | durable journal、history/log/result 保留、interrupted/no-replay 和 process-group cleanup 通过；self-restart 请求本身的 transport acknowledgement 由本 PR 修复。 |
| #2 WebUI / Token compatibility | PASS WITH ISSUES | API/static/config generation、Token rotation 和 MCP client 回归通过；高风险生产写和真人浏览器交互按安全边界未执行。 |

## 2. Environment

| 项目 | 观测值 |
| --- | --- |
| NAS 型号 | QNAP TS-264C |
| 平台 / QTS | QTS 5.2.10 |
| Kernel | 5.10.60-qnap |
| Architecture | amd64 / x86_64 |
| CPU / Memory | 4 cores / 约 8.1 GiB |
| Container Station | 3.1.2.1742 |
| Docker | 27.1.2-qnap8 |
| 测试前 QACS | 2.1.0 baseline |
| 当前 NAS QACS | 2.1.2，QPKG `complete`、enabled、process running |
| 当前 `main` | `b8c2baa878b3064f826815ca07ef8d948acfd890`，PR #8 已合并 |
| 本地候选包 | `QnapAIControl_2.1.3.qpkg`，已复制到桌面；当前 SHA-256 前缀 `f3766e4d6cc0` |
| 2.1.3 live 状态 | 未部署；候选包已确认不含 QTS code-signing 区域，QTS 日志停在 `code_signing_check`，health/QPKG 仍为 2.1.2 |

### QPKG baseline

测试前记录了全部 26 个 QPKG 的名称、版本、enabled 和 process state。生产 QPKG 的版本和状态在测试后没有持久差异。

| QPKG | 版本 | enabled | process |
| --- | --- | --- | --- |
| netmgr | 2.5.10 | true | running |
| QuLog | 1.8.2.956 | true | running |
| NotificationCenter | 1.10.0.3331 | true | running |
| ResourceMonitor | 1.2.0 | true | stopped |
| MalwareRemover | 6.6.10 | false | stopped |
| HybridBackup | 26.4.4.788 | false | stopped |
| CloudLink | 2.4.73 | false | stopped |
| PlexMediaServer | 1.43.3 | true | running |
| qBittorrent | 4.4.5.0 | true | running |
| TextEditor | 1.1.5 | false | stopped |
| MultimediaConsole | 2.11.1 | false | stopped |
| Tailscale | 1.40.0-1 | true | running |
| QsyncServer | 5.0.0.7 | true | running |
| container-station | 3.1.2.1742 | true | running |
| MyCloudNas | 1.1.100 | true | stopped |
| helpdesk | 4.0.1 | false | stopped |
| LicenseCenter | 1.9.57 | true | running |
| Samba | 4.15.008 | true | stopped |
| CacheMount | 1.17.5691 | false | stopped |
| qufirewall | 2.5.0 | true | running |
| FileStation6 | 6.0.4.7789 | true | running |
| Qboost | 1.6.3 | true | running |
| QKVM | 4.2.0.281 | false | stopped |
| QnapAIControl | 2.1.0 → 2.1.2 | true | running |
| QcloudSSLCertificate | 2.2.76 | true | stopped |
| QuFTP | 1.4.6 | true | running |

### QACS baseline fields

- Config：canonical `/etc/config/qnap-ai-control-agent/config.json`，mode 0600；只保存 hash。
- Token：mode 0600；只保存 hash，未记录明文。
- Approval：`sensitive_only`。
- Profile：`full_trust`；测试策略仍阻止所有不在允许范围内的生产写。
- Jobs：canonical `/etc/config/qnap-ai-control-agent/jobs/jobs.jsonl`，mode 0600；jobs directory mode 0700；legacy journal 保留只读兼容。
- QNAP adapters：通过 `nas_capabilities` / `nas_qnap_ecosystem` 采集完整结构，包含 installed、supported、status、verified、backend、provider、persistent、reason。

## 3. PR Coverage Matrix

| PR | Capability | Test | Result | Evidence |
| --- | --- | --- | --- | --- |
| #1 | Operation Registry / Approval | READ 只读能力、WRITE 测试文件、SENSITIVE 不存在 QPKG dry-run；ticket approve/retry/one-shot/mismatch | PASS | MCP approval flow；本地 registry tests |
| #1 | Audit / Redaction | 假 secret/password 进入 Job result、Job log、Audit、service log、journal | PASS | redaction live flow；未发现明文 marker |
| #1 | Jobs | 幂等、资源锁、独立资源并发、max concurrent、cancel/process group | PASS | max running peak=4，配置上限=4；无 orphan |
| #1 | Status / Network / Storage | snapshot、interfaces、IPv4/IPv6、routes、neighbors、DNS、disk/RAID/volume/IO | PASS WITH DEGRADED | 只读交叉验证；SMART/SMB capability 明确 unavailable |
| #1 | Files / CAS | write/read/grep/tail/checksum/append/backup/rename/copy/tree/sync/CAS/delete | PASS | `qacs-test-*` 唯一目录；旧 SHA CAS 覆盖被拒绝 |
| #1 | Docker | 生产 inventory/health/compose/inspect/log/reconstruction；disposable Container 全生命周期 | PASS | 生产只读；`qacs-test-container-*` 已删除 |
| #4 | Capability Resolver | HBS、VM、iSCSI、Virtual Switch、System、Firmware、Notifications、Shares、Certificates、Storage、UPS | PASS | installed 与 verified/available 正确分离 |
| #4 | Storage verified backend | `qcli_storage -p/-v` 只读探测和 inventory | PASS WITH ISSUE | backend verified、exit 0；2.1.2 parser 额外返回标题行，PR #9 修复 |
| #5 | Structured MCP | typed VM/HBS/Storage/Virtual Switch/System/Firmware/Notification tools，generic fallback，compat alias | PASS WITH ISSUE | schema/error/toolset 通过；Storage header issue 尚未部署修复 |
| #5 | Toolsets | core 默认最小暴露；raw 显式启用；compat 保留 | PASS | core 17 tools；core+compat 21；full 120 |
| #5 | Invalid input | 非法 VM/HBS/Firmware action | PASS | MCP validation 或 `INVALID_ARGUMENT`，未透传 shell |
| #6 | Persistence | config/token/approval/profile/adapters/capabilities/history/log/result 保留 | PASS | QACS-only restart 后可读 |
| #6 | Interrupted recovery | `sleep 120` running 后只重启 QACS | PASS | `interrupted`、`recovered=true`、`needs_inspection`、无 replay、无 orphan |
| #6 | Compaction | 本地 threshold=1；NAS 不制造大日志 | LOCAL PASS / NAS BLOCKED_BY_SAFETY | fsync/temp/atomic rename 由本地测试证明 |
| #2 | WebUI / config generation | Overview/Connection/System/Logs/Token，Generic/Codex/Hermes/OpenClaw 配置 | PASS WITH ISSUE | API/static/config contract 通过；真人浏览器交互未执行 |
| #2 | Token rotation | A→B→restart→C，旧 token 失效，新 token 生效，客户端同步 | PASS | 仅记录 hash；最终 bridge/gateway health 成功 |
| #1/#2 | v1/v2 compatibility | system/storage/network/docker/qpkg read-only routes、compat aliases | PASS | 选定 `/v1/*` 均 200、envelope 正常 |
| #5/#6 | Pi Agent usability | 只给用户目标，不指定工具名，检查 storage/HBS/VM/network | PASS WITH ISSUE | capability-first、structured-first；storage 结果暴露 parser issue；无 raw/write |

## 4. Test Cases

| ID | 目标 / 操作 | 预期 | 实际结果 | 修改 / 清理 |
| --- | --- | --- | --- | --- |
| T-01 | 采集 system/QPKG/Docker/storage/network/share/QACS baseline | 只读、可复现、不含明文 Token | PASS；基线完整保存，敏感值只留 hash/结构 | 无生产修改 |
| T-02 | 对不存在的 `qacs-test-nonexistent-qpkg-*` 执行 remove+dry-run，完成 approval 生命周期 | 未批准拒绝；批准后只允许原参数一次 | PASS；重复 ticket 和 mismatch 均拒绝 | 未删除真实对象 |
| T-03 | 假 `qacs-test-secret-123` / `qacs-test-password-123` 进入 command output/log | Audit、Job、API、journal、service log 不出现明文 | PASS；出现 `[REDACTED]` 或等价 mask | 仅 QACS 测试历史 |
| T-04 | 相同 idempotency key 提交两个 `sleep 3` Job | 只产生一个 Job、只执行一次 | PASS；返回同一 Job ID | Job history 允许保留 |
| T-05 | 同 resource 的 `sleep 5` + `sleep 1`；不同 resource 的 C/D | 同 resource 串行；无关 resource 可并发 | PASS | 仅 QACS Job history |
| T-06 | 多个轻量 sleep Job | running 不超过 max_concurrent=4 | PASS；峰值 4 | 测试 Job 进程清理 |
| T-07 | `sh -c 'sleep 60 & wait'` running 后 cancel | 主 shell 和子 sleep 都结束 | PASS；无匹配 orphan process |
| T-08 | `nas_status_snapshot` | 单个模块不可用时返回 partial/degraded，而不是整包失败 | PASS；总体 snapshot 可返回 | 无生产修改 |
| T-09 | network structured output 与 `ip addr`、`ip -6 route`、`ip -6 neigh`、`/proc/net/*` 交叉读取 | 全部只读且结果一致 | PASS；read errors=0；未执行 network write |
| T-10 | disk/RAID/volume/IO/mount/SMART | 只读；SMART 不可用要明确说明 | PASS WITH DEGRADED；4 disks、7 RAID groups、71 volumes；未启动 SMART test |
| T-11 | SMB/Cron/process/service/socket/QPKG-process 观测 | 只读且明确不可用项 | PASS WITH DEGRADED；SMB session 因缺 `smbstatus` unavailable |
| T-12 | 唯一 `qacs-test-*` 文件目录执行文件 CRUD、tree、backup、atomic write、CAS | 旧 SHA 不能覆盖新版本 | PASS；CAS A→B 成功，旧 A→C 失败；目录已删除 |
| T-13 | 生产 Docker list/info/images/health/compose/inspect/log/reconstruction | 不 stop/start/recreate，不输出 env value | PASS；reconstruction 明确 `lossy/equivalent=false`；无生产写 |
| T-14 | `qacs-test-container-*` alpine sleep：create/start/inspect/stop/restart/logs/delete | none network、无 mount/privileged/host pid，最后删除 | PASS；最终 0 个匹配容器 |
| T-15 | capability resolver 逐项读取 | QPKG/可执行文件存在不能单独推出 write availability | PASS；未验证能力保持 degraded/unavailable |
| T-16 | typed 非法 VM/HBS/Firmware action | schema/validation 直接拒绝 | PASS；machine-readable code/reason，无 shell stdout-only error |
| T-17 | QACS-only restart 后读取 config/token/approval/profile/adapters/jobs/log/result | durable 状态不丢 | PASS；只重启 QACS 自身 |
| T-18 | `sleep 120` running 后 QACS-only restart | interrupted/recovered/needs_inspection，不 replay | PASS；无 orphan；没有自动执行旧意图 |
| T-19 | Pi 用户目标：检查存储/HBS/VM/网络，不指定工具名 | Agent 先读 capability、优先 structured、必要时再 fallback | PASS WITH ISSUE；Pi 未调用 raw/write；Storage parser issue 仍可见 |
| T-20 | Token A→B→restart→C | A 失效；B/C 生效；重启后仍有效；客户端配置同步 | PASS；只记录 hash，未输出 Token |
| T-21 | 上传 2.1.3 QPKG，经 `nas_qpkg_manage install_file` 等待 QTS 队列 | 只有确认 QPKG version/health/process 后才能算安装完成 | BLOCKED / NOT DEPLOYED；exit 0 仅为 queue acknowledgement；独立解析确认包没有 QDK/code-signing 区域，QTS 日志停在 code-signing check，仍为 2.1.2 | 包和目录已删除；桌面保留最新本地包 |
| T-22 | final sanity：test root、containers、sleep processes、QACS health/QPKG | 专用资源全清理，生产状态仍稳定 | PASS；test root not found，0 test containers，0 test sleep；QACS 2.1.2 running |

## 5. Baseline Diff

安全归一化 diff 结果：`production_domains_equal=true`，`production_diffs=[]`。

| 域 | 测试前后摘要 | 结论 |
| --- | --- | --- |
| Network | 27 interfaces、32 routes、22 IPv6 routes、7 neighbors、1 DNS server、1 search domain | 无 default gateway、DNS、IP/IPv6、MTU、VLAN、Bond、Virtual Switch 写入 |
| Storage | 4 disks、7 RAID groups、71 volumes、0 snapshots、358 disk-IO keys | 无 RAID scrub/repair、SMART test、Pool/Volume/Snapshot 写入 |
| QPKG | 26 个生产 QPKG 的版本/enabled/process state 未变化 | 只升级/重启 QACS 自身；2.1.3 因缺少签名未部署 |
| Docker | 21 containers、5 Compose projects；production inventory 未变化 | 只创建并删除 disposable `qacs-test-*` Container，无生产 mutation |
| Users / groups / shares | users 6、groups 3、shares 13、NFS exports 0 | 无用户、组、ACL、share、HBS、VM、firmware 修改 |
| QACS config | canonical path、mode 0600；journal migration/job history 变化 | 允许的 QACS 自身持久化变化 |
| QACS token | mode 0600；A→B→C rotation 后 hash 改变 | 允许的 Token rotation；明文未记录 |
| QACS jobs/logs | 产生测试 Job history、per-job logs、capability cache | 允许；正式历史未删除 |

测试期间观察到 `br0` 的动态 IPv6 privacy/SLAAC 地址变化；没有 QACS network write，baseline diff 未把它归因于 QACS。

## 6. Bugs Found

### B1 — P1/P2：Storage Manager 标题行被解析为 volume

- Component：`mac-bridge/src/tools/contracts.js`，影响 PR #4/#5 的 Storage Manager structured contract。
- Reproduction：真实 NAS `qcli_storage -v` 输出：

  ```text
  VolID   VolName                 Mount_Path
  1       DataVol1                /share/CACHEDEV1_DATA
  2       SSD                     /share/CACHEDEV5_DATA
  ```

  2.1.2 的 `nas_storage_manager_inventory` 返回 3 个 item，第一项为 `{id: ..., name: "VolName"}` 的伪 volume。
- Expected：只返回 2 个真实 volume，标题保留在 raw output 而不是 items。
- Root cause：header detector 识别了 `volume name`，但没有识别 QTS 此版本的紧凑标题 `VolID VolName Mount_Path`。
- Fix：PR #9 增加 compact header detection，并加入真实 NAS 行 fixture；PR #9 的本地 JS/Go/persistence tests 已通过，尚未合并和 live deploy。
- Severity：数据正确性问题，不会自动执行 storage write，但会误导 Agent/调用方的 inventory 判断。
- PR：[PR #9](https://github.com/zyk1172/qnap-ai-control-suite/pull/9)。

### B2 — P1：QACS 自重启的 MCP acknowledgement 在进程退出前未发送

- Component：`agent/internal/api/server.go`，影响 QACS self-restart 的 control-plane transport 语义。
- Reproduction：2.1.2 通过 MCP 调用 `nas_qpkg_manage`，`name=QnapAIControl`、`action=restart`。客户端收到 `fetch failed`；重新连接后 `nas_health` 成功，说明 QACS 实际完成了 stop/start，但原请求没有确定性响应。
- Expected：先返回结构化 `status=scheduled`、`accepted=true`、`completion_verified=false`，调用方重连后再用 `nas_health` 验证完成。
- Root cause：HTTP handler 同步调用 `QPKG.Manage`；QPKG stop 会终止正在提供 HTTP 响应的 QACS 进程。
- Fix：本分支让 self-restart 走确定性的 scheduled acknowledgement，flush response 后延迟 250ms 再启动 QPKG restart；增加 `TestQPKGSelfRestartAcknowledgesBeforeScheduling`。该修复没有引入 Planner、Agent 或自动 replay。
- Live boundary：2.1.3 QPKG 经过 `install_file` 后只停在 QTS code-signing check，未完成部署，因此本修复尚未有真机 live proof。
- PR：[PR #10](https://github.com/zyk1172/qnap-ai-control-suite/pull/10)。

### B3 — P1：发布 QPKG 未包含 QTS code-signing 区域，导致更新未完成

- Component：QPKG 发布/签名流水线，影响 QTS 5.2.10 上的安装更新；不是 QACS runtime 操作逻辑本身。
- Reproduction：将桌面上的 `QnapAIControl_2.1.3.qpkg` 通过 `nas_qpkg_manage install_file` 提交到 NAS。CLI 返回的是队列接受，不代表安装完成；QTS `/var/log/log.qpkg` 只出现 `qpkgd_code_signing_check`，随后 health/QPKG 仍为 2.1.2。
- Independent evidence：对候选包做无密钥二进制结构检查，尾部为 `QNAPQPKG`，没有 `QDK` 区域，也没有 type 254 的 code-signing area；本地 `scripts/package_qpkg.sh` 只调用 `qbuild`，没有启用 `QNAP_CODE_SIGNING=1`、`--add-code-signing`，也没有注入证书/私钥/HSM。
- Expected：正式发布给该 QTS 版本的 QPKG 应通过 QNAP 官方签名流程，或使用目标 QTS 接受的第三方 code-signing 证书/HSM 生成签名区域；发布检查应在缺少签名时 fail closed。
- Root cause：构建产物是 unsigned QPKG；QTS 的 code-signing check 没有让它进入已安装版本，因此之前的“更新失败”根因已确认，不是 MCP 将 `exit 0` 误认为安装完成。
- Can it be solved：技术上可以。需要 QNAP 官方签名服务/授权，或 QNAP 接受的第三方签名证书、私钥/HSM，并在受保护的 CI 发布阶段执行签名和无密钥验签。不能生成任意自签名证书来冒充已解决，也不能把签名材料写入仓库、QPKG、日志或 PR。
- Current status：根因已确认；实际修复部署被签名授权/材料阻塞。当前 PR #10 的自重启修复因此只能报告为本地/CI 通过、真机 live deployment 未验证。

### 已验证关闭的问题

- v1 legacy Job journal migration 已切换到 canonical persistent path，并保留 legacy history。
- Job metadata/log/result 与 audit redaction 已统一；假 secret/password 未泄露。
- QNAP Docker JSON formatter hang 已由 scalar formatter 修复；真实 21-container inventory 成功返回。
- QACS-only interrupted recovery 已做到事实恢复、不自动 replay，并在当前 candidate 上清除 executor process group。

## 7. Unsupported / Degraded Capabilities

以下是能力状态，不应直接当作 QACS bug：

| Capability | Live state | 原因 |
| --- | --- | --- |
| Storage Manager pools / volumes | available, verified=true | `/sbin/qcli_storage` read-only probe exit 0；backend `qcli_storage` |
| Storage Manager snapshots/create/delete/expand/restore/schedule | degraded, verified=false | 没有 verified backend；只验证 read path |
| UPS | available, verified=true | NUT `/sbin/upsc` 可读 |
| HBS 3 | installed=true, supported=false, degraded | QPKG 存在，但没有 verified backend |
| Virtualization Station | installed=true, supported=false, degraded | QKVM 存在，但没有 verified backend |
| iSCSI | unavailable | 未检测到 iSCSI/LUN subsystem/backend |
| Virtual Switch | installed=true, supported=false, degraded | QTS private API 随 firmware 变化，没有 verified backend |
| System Settings | installed=true, supported=false, degraded | 没有 verified persistent local backend |
| Firmware | installed=true, supported=false, degraded | 没有 verified local backend；本轮不执行 firmware 操作 |
| Notifications | installed=true, supported=false, degraded | Notification Center 存在，但无 verified backend |
| Shares write actions | installed=true, supported=false, degraded | 只读 share inventory 可读，没有 verified shared-folder write backend |
| Certificates | installed=true, supported=false, degraded | 没有 verified certificate command backend |
| SMART | unavailable/unsupported | QNAP runtime probe/smartctl backend 不满足；没有启动 short/long test |
| SMB sessions | unavailable | `smbstatus` 不存在；SMB service/share inventory 仍可读取 |
| Snapshot management | partial/degraded | 需要 QTS auth/backend；没有执行 snapshot mutation |

## 8. MCP / Agent Usability

- 默认 `core` 只暴露 17 个工具，不无条件暴露 `nas_exec` / `nas_shell`。
- `core+compat` 暴露 21 个工具，兼容 alias 仍有效；显式加入 `raw` 后才暴露 `nas_exec` / `nas_shell`；全 toolset 共 120 个工具。
- Pi 的用户目标没有包含工具名：存储和网络优先选择 structured tool；HBS/VM 在 capability degraded 时返回 `BACKEND_UNAVAILABLE`，没有错误地假装可用；本轮未观察到 raw shell 或生产写。
- typed 非法 action 由 MCP schema 或 QACS validation 直接拒绝，错误包含 machine-readable code/reason，不以底层 stdout 作为唯一错误信息。
- `nas_qpkg_manage` 的 queue-like 操作返回 `operation_phase=queued_or_processing`、`completion_verified=false`，能避免把 QTS CLI exit 0 误报为已安装；本轮 2.1.3 安装结果证明这个约束是必要的。
- Storage Manager 的标题行 bug 会影响 Agent 对 volume 数量的判断，待 PR #9 合并后重新做 Pi live regression。

## 9. Persistence / Recovery

| 项目 | 结果 |
| --- | --- |
| QACS restart | PASS；config/token hash、approval mode、profile、adapter manifest、Job history/log/result metadata 保留 |
| Durable journal | PASS；canonical journal 位于 `/etc/config/qnap-ai-control-agent/jobs/jobs.jsonl`，mode 0600；jobs dir 0700 |
| Legacy compatibility | PASS；旧 `/var/lib/qnap-ai-control-agent/jobs.jsonl` 保留并可读取，未覆盖 canonical 运行路径 |
| Interrupted job | PASS；running `sleep 120` 重启后为 interrupted/recovered/needs_inspection，不 replay |
| Process group | PASS on installed 2.1.2 candidate；主 shell 和 child sleep 均结束，无 orphan |
| Compaction | LOCAL PASS；NAS 真实 threshold 32 MiB，按安全要求未降低 threshold 或制造大日志，故 NAS compaction 为 `BLOCKED_BY_SAFETY` |
| Token | PASS；A→B→restart→C，旧值失效，新值生效，最终 bridge/Hermes gateway 使用 C 并通过 health；只记录 hash |
| Self-restart transport | FAIL on current 2.1.2；修复已写入本分支但 2.1.3 未部署 |

## 10. Safety-blocked Coverage

以下项目没有为了覆盖率强行执行，均标记 `BLOCKED_BY_SAFETY`：

- NAS reboot/shutdown、Container Station/生产 QPKG/生产容器 restart。
- default gateway、DNS、MTU、VLAN、Bond/LACP、Virtual Switch、生产 interface write。
- RAID scrub/repair、Pool/Volume create/delete/expand、Snapshot restore/delete、SMART short/long test。
- HBS production task、VM start/stop/restart/snapshot/clone、firmware download/install。
- 生产 share/user/group/ACL 修改。
- NAS 真实 journal compaction threshold 调低或制造大规模日志。

这些项目应在隔离 NAS、snapshot clone 或专用 QTS 测试环境补充验证；本生产 NAS 不执行。

## 11. Final Verdict

| Area | Verdict |
| --- | --- |
| Control Plane | PASS WITH ISSUES |
| MCP Contract | PASS WITH ISSUES |
| Capability Discovery | PASS |
| Persistence | PASS |
| Restart Recovery | PASS WITH ISSUES |
| Docker | PASS |
| Files | PASS |
| Storage Read Path | PASS WITH ISSUES |
| Network Read Path | PASS |
| WebUI Compatibility | PASS WITH ISSUES |

最终判断：**PASS WITH ISSUES**。当前版本适合继续做受控生产只读观测、低风险 `qacs-test-*` 验证和经 approval/回滚边界保护的 QACS 自身操作；在 PR #9 的 Storage parser 修复和本 PR 的 self-restart acknowledgement 修复分别合并并在 NAS 上确认版本/health/process 后，再重新给出“控制面整改完全闭环”的结论。
