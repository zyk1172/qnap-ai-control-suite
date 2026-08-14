import { request } from "../client.js";
import { register, z } from "./register.js";

const execSchema = { argv: z.array(z.string()).min(1), cwd: z.string().optional(), env: z.record(z.string(), z.string()).optional(), stdin_base64: z.string().optional(), timeout_sec: z.number().int().positive().optional(), max_output_bytes: z.number().int().positive().optional(), dry_run: z.boolean().optional() };
export function registerSystemTools(server) {
  register(server, "nas_health", "Read agent health and active profile.", {}, () => request("GET", "/v1/health"), { readOnlyHint: true });
  register(server, "nas_capabilities", "Read current permissions, privacy and confirmation policy.", {}, () => request("GET", "/v1/capabilities"), { readOnlyHint: true });
  register(server, "nas_discovery", "Discover QNAP platform, utilities, QPKGs and runtime capability states.", {}, () => request("GET", "/v1/qnap/discovery"), { readOnlyHint: true });
  register(server, "nas_system_info", "Read system overview.", {}, () => request("GET", "/v1/system/overview"), { readOnlyHint: true });
  register(server, "nas_system_resources", "Read load and resources.", {}, () => request("GET", "/v1/system/resources"), { readOnlyHint: true });
  register(server, "nas_process_list", "List NAS processes.", {}, () => request("GET", "/v1/system/processes"), { readOnlyHint: true });
  register(server, "nas_process_action", "Send a signal to a process. PID 1 and invalid signals are refused by the agent.", { pid: z.number().int().positive(), signal: z.enum(["TERM", "KILL", "HUP", "INT", "STOP", "CONT"]) }, (args) => request("POST", "/v1/system/process/action", args), { destructiveHint: true });
  register(server, "nas_service_list", "List services from the detected system service manager.", {}, () => request("GET", "/v1/system/services"), { readOnlyHint: true });
  register(server, "nas_service_action", "Start, stop, restart, reload, enable, or disable a detected system service.", { name: z.string().min(1), action: z.enum(["start", "stop", "restart", "reload", "enable", "disable"]) }, (args) => request("POST", "/v1/system/services/action", args), { destructiveHint: true });
  register(server, "nas_system_thermal", "Read QNAP and Linux hardware temperatures.", {}, () => request("GET", "/v1/system/thermal"), { readOnlyHint: true });
  register(server, "nas_power", "Reboot or shut down the NAS. Requires full_trust.", { action: z.enum(["reboot", "shutdown"]) }, (args) => request("POST", "/v1/system/power", args), { destructiveHint: true });
  register(server, "nas_exec", "Execute an argv command. Full trust permits any executable; otherwise the agent profile controls it.", execSchema, (args) => request("POST", "/v1/exec", args), { destructiveHint: true });
  register(server, "nas_shell", "Execute a shell script or pipeline. Use script with an optional absolute shell path; legacy shell-as-script input remains supported.", { shell: z.string().optional(), script: z.string().optional(), cwd: z.string().optional(), env: z.record(z.string(), z.string()).optional(), timeout_sec: z.number().int().positive().optional(), dry_run: z.boolean().optional() }, (args) => request("POST", "/v1/shell", args), { destructiveHint: true });
  register(server, "nas_audit_tail", "Read recent audit records.", {}, () => request("GET", "/v1/audit/tail"), { readOnlyHint: true });
  register(server, "nas_system_overview", "Compatibility alias for nas_system_info.", {}, () => request("GET", "/v1/system/overview"), { readOnlyHint: true });
  register(server, "nas_processes", "Compatibility alias for nas_process_list.", {}, () => request("GET", "/v1/system/processes"), { readOnlyHint: true });
  register(server, "nas_command_run", "Compatibility alias for nas_exec.", execSchema, (args) => request("POST", "/v1/command/run", args), { destructiveHint: true });
}
