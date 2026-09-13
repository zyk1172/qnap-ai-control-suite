import { request } from "../client.js";
import { register, z } from "./register.js";

const asyncControls = {
  async: z.boolean().optional(),
  dry_run: z.boolean().optional(),
  timeout_sec: z.number().int().positive().optional()
};
const qnapRead = { readOnlyHint: true, toolset: "qnap" };
const qnapWrite = { destructiveHint: true, toolset: "qnap" };

export function registerStructuredQNAPTools(server) {
  register(server, "nas_vm_list", "List Virtualization Station VMs through the verified QNAP adapter. Use nas_capabilities first when availability is uncertain.", {}, () => request("POST", "/v1/qnap/vm/action", { action: "list" }), qnapRead);
  register(server, "nas_vm_info", "Read one Virtualization Station VM by id through the verified QNAP adapter.", { id: z.string().min(1) }, ({ id }) => request("POST", "/v1/qnap/vm/action", { action: "info", id }), qnapRead);
  register(server, "nas_vm_manage", "Start, stop, restart, force-stop, snapshot, or clone a Virtualization Station VM. This tool uses structured fields instead of arbitrary argv.", {
    id: z.string().min(1),
    action: z.enum(["start", "stop", "restart", "force_stop", "snapshot", "clone"]),
    name: z.string().optional(),
    target: z.string().optional(),
    ...asyncControls
  }, (args) => request("POST", "/v1/qnap/vm/action", args), qnapWrite);

  register(server, "nas_hbs_jobs", "List HBS 3 jobs through the verified QNAP adapter.", {}, () => request("POST", "/v1/qnap/hbs/action", { action: "job_list" }), qnapRead);
  register(server, "nas_hbs_job_info", "Read one HBS 3 job status by id.", { id: z.string().min(1) }, ({ id }) => request("POST", "/v1/qnap/hbs/action", { action: "job_status", id }), qnapRead);
  register(server, "nas_hbs_job_manage", "Run or stop one HBS 3 job. Long runs should use async=true so the NAS job subsystem tracks them.", {
    id: z.string().min(1),
    action: z.enum(["run", "stop"]),
    ...asyncControls
  }, (args) => request("POST", "/v1/qnap/hbs/action", args), qnapWrite);
  register(server, "nas_hbs_logs", "Read HBS 3 logs for a job through the verified adapter.", { id: z.string().min(1) }, ({ id }) => request("POST", "/v1/qnap/hbs/action", { action: "logs", id }), qnapRead);

  register(server, "nas_storage_manager_inventory", "Read QTS Storage Manager pools and volumes through verified native or manual backends. The two reads are returned separately so partial support remains visible.", {}, async () => {
    const results = await Promise.allSettled([
      request("POST", "/v1/qnap/storage/action", { action: "pools" }),
      request("POST", "/v1/qnap/storage/action", { action: "volumes" })
    ]);
    const normalize = (result) => result.status === "fulfilled"
      ? { ok: true, data: result.value }
      : { ok: false, code: result.reason?.code || "execution_failed", message: result.reason?.message || String(result.reason) };
    return { pools: normalize(results[0]), volumes: normalize(results[1]), partial: results.some((result) => result.status !== "fulfilled") };
  }, qnapRead);

  register(server, "nas_virtual_switch_manage", "Create, delete, configure, or change VLAN/bond/bridge settings through the verified QTS Virtual Switch adapter. Adapter-specific argv remains available only through nas_virtual_switch_action.", {
    action: z.enum(["create", "delete", "configure", "vlan", "bond", "bridge"]),
    id: z.string().optional(),
    name: z.string().optional(),
    target: z.string().optional(),
    ...asyncControls
  }, (args) => request("POST", "/v1/qnap/virtual-switch/action", args), qnapWrite);

  register(server, "nas_system_settings_manage", "Change a persistent QTS system setting through a verified adapter. Adapter-specific argv remains available only through nas_system_config_action.", {
    action: z.enum(["hostname", "timezone", "ntp", "service"]),
    name: z.string().optional(),
    target: z.string().optional(),
    ...asyncControls
  }, (args) => request("POST", "/v1/qnap/system-settings/action", args), qnapWrite);

  register(server, "nas_firmware_manage", "Check, download, or install QTS firmware through a verified adapter. Install/download should normally use async=true. Adapter-specific argv remains available only through nas_firmware_action.", {
    action: z.enum(["check", "download", "install"]),
    target: z.string().optional(),
    ...asyncControls
  }, (args) => request("POST", "/v1/qnap/firmware/action", args), qnapWrite);

  register(server, "nas_notification_manage", "Test or configure QTS notifications through a verified adapter. Adapter-specific argv remains available only through nas_notification_action.", {
    action: z.enum(["test", "configure"]),
    name: z.string().optional(),
    target: z.string().optional(),
    ...asyncControls
  }, (args) => request("POST", "/v1/qnap/notifications/action", args), qnapWrite);
}
