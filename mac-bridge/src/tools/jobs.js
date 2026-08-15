import { request } from "../client.js";
import { register, z } from "./register.js";

const command = z.object({
  argv: z.array(z.string()).min(1),
  cwd: z.string().optional(),
  env: z.record(z.string(), z.string()).optional(),
  stdin_base64: z.string().optional(),
  timeout_sec: z.number().int().positive().optional(),
  max_output_bytes: z.number().int().positive().optional(),
  dry_run: z.boolean().optional(),
});
const jobStart = z.object({
  kind: z.string().min(1).max(80).optional(),
  command: command.optional(),
  shell: z.string().optional(),
  script: z.string().optional(),
}).refine((value) => value.command || value.shell || value.script, "command, script, or legacy shell is required");

export function registerJobTools(server) {
  register(server, "nas_job_start", "Start an asynchronous arbitrary argv command or shell script. Use for long Docker, backup, archive, storage, firmware, or QNAP commands; poll nas_job_get and nas_job_logs.", jobStart, (args) => request("POST", "/v1/jobs", args), { destructiveHint: true });
  register(server, "nas_job_list", "List asynchronous NAS jobs without embedding their logs.", {}, () => request("GET", "/v1/jobs"), { readOnlyHint: true });
  register(server, "nas_job_get", "Get asynchronous NAS job metadata and result without embedding logs.", { id: z.string().min(1) }, ({ id }) => request("GET", `/v1/jobs/${encodeURIComponent(id)}`), { readOnlyHint: true });
  register(server, "nas_job_logs", "Read one bounded page of a job's logs from an optional cursor.", { id: z.string().min(1), cursor: z.number().int().nonnegative().optional(), limit: z.number().int().positive().max(2000).optional() }, ({ id, cursor, limit }) => { const params = new URLSearchParams(); if (cursor !== undefined) params.set("cursor", String(cursor)); if (limit !== undefined) params.set("limit", String(limit)); const query = params.size ? `?${params}` : ""; return request("GET", `/v1/jobs/${encodeURIComponent(id)}/logs${query}`); }, { readOnlyHint: true });
  register(server, "nas_job_cancel", "Cancel an asynchronous NAS job.", { id: z.string().min(1) }, ({ id }) => request("POST", `/v1/jobs/${encodeURIComponent(id)}/cancel`, {}), { destructiveHint: true });
}
