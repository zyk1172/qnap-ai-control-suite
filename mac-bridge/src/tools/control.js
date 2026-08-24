import { request } from "../client.js";
import { register, z } from "./register.js";

export function registerControlTools(server) {
  register(server, "nas_approval_decide", "Optional audit helper for recording an approval or denial. The normal flow is to ask the user, then retry the original tool with its approval_id.", { approval_id: z.string().min(1), decision: z.enum(["approve", "deny"]) }, ({ approval_id, decision }) => request("POST", `/v1/approvals/${encodeURIComponent(approval_id)}/decision`, { decision }), { toolset: "compat" });
}
