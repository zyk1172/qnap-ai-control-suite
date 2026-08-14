# Ecosystem Adapter Commands

Virtualization Station, HBS 3, iSCSI/LUN, certificates, Virtual Switch, QTS persistent system settings, firmware, notifications and deep Storage Manager commands are not stable across QTS and QuTS hero releases. The agent therefore does not invent private QNAP CLI syntax. Instead, `qnap_adapters` binds commands verified on this NAS to domain MCP tools.

1. Call MCP `nas_qnap_probe` after installing the relevant QPKG, with an absolute `output_path` such as `/share/Public/qnap-probe.json`. It runs the QPKG-bundled script and records actual executable paths without reading private keys or certificate contents.
2. Verify each command and its `--help` output on the NAS shell.
3. Add only the exact absolute argv templates to `/etc/config/qnap-ai-control-agent/config.json`, then restart the QPKG.
4. Read `nas_qnap_ecosystem`; each configured adapter reports `supported: true` and its action names.
5. Call the matching MCP action with `dry_run: true` before a real action.

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

Supported placeholders are `{id}`, `{name}`, `{target}`, and a standalone `{args}`. Each value is inserted as argv, not interpreted by a shell. Any unknown placeholder, relative executable path, missing configured action, or missing required value is rejected.

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

Recommended action names are domain vocabulary, not claims about a QTS command syntax: `virtual_switch` may expose `list`, `configure`, `vlan`, `bond`, `bridge`; `system_settings` may expose `hostname`, `timezone`, `ntp`; `firmware` may expose `info`, `check`, `download`, `install`; `notifications` may expose `list`, `history`, `test`, `configure`; `storage_manager` may expose `pools`, `volumes`, `snapshots`, `expand`, `restore`, `schedule`. The configured action names in `nas_qnap_ecosystem` are authoritative.

For a command not yet verified, use `nas_exec` or `nas_shell` in `full_trust` only after inspecting the NAS-local command help. This is the fallback path; it does not make the corresponding adapter supported.
