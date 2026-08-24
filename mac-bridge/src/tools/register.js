import * as z from "zod/v4";
import { toolResult } from "../client.js";
import { ApprovalFlowError, handleApprovalRequired, isApprovalRequired, unsupportedApprovalMessage } from "../approval.js";
import { toolsetEnabled } from "../config.js";

export { z };
export const commandResultSchema = z.object({ argv: z.array(z.string()).optional(), exit_code: z.number().int().optional(), stdout: z.string().optional(), stderr: z.string().optional(), dry_run: z.boolean().optional() }).passthrough();
export const jobResultSchema = z.object({ id: z.string(), status: z.enum(["queued", "running", "succeeded", "failed", "cancelled", "interrupted"]) }).passthrough();
export const approvalRequiredSchema = z.object({ approval_id: z.string(), operation: z.string(), risk: z.literal("sensitive"), state: z.enum(["pending", "approved", "denied", "used"]) }).passthrough();
export const outputSchema = z.object({}).passthrough();
export function register(server, name, description, inputSchema, call, annotations = {}) {
  const toolset = annotations.toolset || toolsetFor(name);
  if (!toolsetEnabled(toolset)) return;
  const schema = annotations.readOnlyHint ? inputSchema : withControlFields(inputSchema);
  server.registerTool(name, { description, inputSchema: schema, outputSchema: outputFor(name), annotations }, async (args) => {
    try {
      return toolResult(await call(args));
    } catch (error) {
      if (isApprovalRequired(error)) {
        try {
          return toolResult(await handleApprovalRequired({ server, error }));
        } catch (flowError) {
          if (flowError instanceof ApprovalFlowError || flowError?.isApprovalFlowError) {
            return toolResult(flowError.details, true);
          }
          throw flowError;
        }
      }
      if (error?.code === "approval_required" && error.details) {
        return toolResult({ ...error.details, approval_status: "approval_request_unavailable", message: unsupportedApprovalMessage }, true);
      }
      throw error;
    }
  });
}

function withControlFields(schema) {
  const fields = { approval_id: z.string().min(1).optional(), idempotency_key: z.string().min(1).max(256).optional() };
  if (typeof schema?.extend !== "function") return { ...schema, ...fields };
  const missing = Object.fromEntries(Object.entries(fields).filter(([key]) => !(key in schema.shape)));
  return schema.extend(missing);
}

function outputFor(name) {
  if (["nas_job_start", "nas_job_get"].includes(name)) return jobResultSchema;
  if (["nas_exec", "nas_shell", "nas_command_run", "nas_docker_command"].includes(name)) return commandResultSchema;
  return outputSchema;
}

function toolsetFor(name) {
  if (["nas_system_overview", "nas_processes", "nas_command_run", "nas_qpkg_action"].includes(name)) return "compat";
  if (name.startsWith("nas_file_")) return "files";
  if (name.startsWith("nas_docker_")) return "docker";
  if (name.startsWith("nas_network_") || name.startsWith("nas_virtual_switch")) return "network";
  if (name === "nas_exec" || name === "nas_shell" || name === "nas_qnap_probe") return "raw";
  if (name === "nas_power" || name === "nas_process_action" || name === "nas_service_action" || name.includes("_manage") || name === "nas_acl_set") return "admin";
  if (name.startsWith("nas_qpkg_") || name.startsWith("nas_storage_") || name.startsWith("nas_disk") || name.startsWith("nas_raid") || name.startsWith("nas_snapshot") || name === "nas_volume") return "storage";
  if (name.startsWith("nas_qnap_") || name.startsWith("nas_vm_") || name.startsWith("nas_hbs_") || name.startsWith("nas_iscsi_") || name.startsWith("nas_certificate_") || name.startsWith("nas_notification_") || name.startsWith("nas_ups") || name.startsWith("nas_share_") || name.startsWith("nas_smb_") || name.startsWith("nas_nfs_") || name.startsWith("nas_user") || name.startsWith("nas_group") || name.startsWith("nas_acl_") || name.startsWith("nas_log_") || name === "nas_system_config_action" || name === "nas_firmware_action") return "qnap";
  return "core";
}
