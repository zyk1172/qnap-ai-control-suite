# Ecosystem Adapter Commands

Virtualization Station, HBS 3, iSCSI/LUN, certificates, Virtual Switch, QTS persistent system settings, firmware, notifications and deep Storage Manager commands are not stable across QTS and QuTS hero releases. The agent therefore does not invent private QNAP CLI syntax.

The capability resolver now distinguishes three states per action:

- `available`: a backend is bound and verified for that action.
- `degraded`: the QNAP subsystem is detected, but no backend has been verified for that action.
- `unavailable`: the subsystem or required runtime component was not detected.

`nas_qnap_ecosystem` keeps the existing adapter fields for compatibility and adds `capability_states`, `verified`, `backend`, `provider`, `partial`, and `persistent` metadata. An adapter can therefore be partially supported without pretending that every advertised action works.

## Backend priority

For ecosystem actions the intended priority is:

1. Explicit `qnap_adapters` override verified on this NAS.
2. Built-in QNAP backend whose argv/API contract is known and verified read-only by the agent.
3. Existing Linux/QNAP subsystem-specific implementation where applicable.
4. `nas_exec` / `nas_shell` as the full-trust fallback.

The first built-in deep-QNAP binding is Storage Manager inventory. If `qcli_storage` is found, the agent runs read-only probes before advertising these capabilities as available:

- `storage.manager.pools` -> `qcli_storage -p`
- `storage.manager.volumes` -> `qcli_storage -v`

If either probe fails, that action remains `degraded`. Storage write actions such as create/delete/expand/restore are **not** inferred from executable names and remain degraded until a verified backend or explicit override exists.

## Manual overrides

`qnap_adapters` remains fully supported and has higher priority than automatic bindings. It is now an override mechanism rather than the only path to ecosystem support.

1. Call MCP `nas_qnap_probe` after installing the relevant QPKG, with an absolute `output_path` such as `/share/Public/qnap-probe.json`.
2. Inspect the discovered executable and its help/read-only behavior on the NAS.
3. Add only the exact absolute argv templates that have been verified to `/etc/config/qnap-ai-control-agent/config.json`, then restart the QPKG.
4. Read `nas_qnap_ecosystem` and inspect each action's `capability_states` entry.
5. Call the matching MCP action with `dry_run: true` before a real write when the tool supports it.

Example schema. Paths and subcommands below are placeholders, not QNAP command claims:

```json
{
  "qnap_adapters": {
    "hbs3": {
      "timeout_seconds": 120,
      "commands": {
        "job_list": ["/absolute/path/from-probe/hbs-cli", "job", "list"],
        "job_status": ["/absolute/path/from-probe/hbs-cli", "job", "status", "{id}"],
        "run": ["/absolute/path/from-probe/hbs-cli", "job", "run", "{id}"],
        "stop": ["/absolute/path/from-probe/hbs-cli", "job", "stop", "{id}"]
      }
    }
  }
}
```

Supported placeholders are `{id}`, `{name}`, `{target}`, and a standalone `{args}`. Each value is inserted as argv, not interpreted by a shell. Any unknown placeholder, relative executable path, missing required value, or extra args without `{args}` are rejected.

`nas_certificate_inspect` is available without a private QNAP adapter. Pass a PEM/CRT path returned by the probe to receive public X.509 subject, issuer, SAN, validity, serial and SHA-256 fingerprint metadata. The active file-root policy applies. Private-key material is not returned by this tool; in `full_trust`, use the existing binary-safe `nas_file_read` only when the agent explicitly needs that file.

MCP mappings:

- `nas_vm_action` -> `virtualization_station`
- `nas_hbs_action` -> `hbs3`
- `nas_iscsi_action` -> `iscsi`
- `nas_certificate_action` -> `certificates`
- `nas_share_manage` -> `shares`
- `nas_virtual_switch_action` -> `virtual_switch`
- `nas_system_config_action` -> `system_settings`
- `nas_firmware_action` -> `firmware`
- `nas_notification_action` -> `notifications`
- `nas_storage_manager_action` -> `storage_manager`

Recommended action names are domain vocabulary, not claims about a QTS command syntax. On QTS, snapshot list/delete/restore commands discovered through `qcli_volumesnapshot` can require an authenticated QCLI `sid`; they must not be auto-bound merely because an executable is present.

For a command not yet verified, use `nas_exec` or `nas_shell` in `full_trust` after inspecting the NAS-local command help. This remains the break-glass fallback and does not make the corresponding structured adapter capability available.
