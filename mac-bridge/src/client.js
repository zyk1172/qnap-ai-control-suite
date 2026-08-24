import { baseUrl, token, requireConfiguration } from "./config.js";

export async function request(method, path, body) {
  requireConfiguration();
  const payload = body && typeof body === "object" && !Array.isArray(body) ? { ...body } : body;
  const approvalId = payload?.approval_id;
  const idempotencyKey = payload?.idempotency_key;
  if (payload) {
    delete payload.approval_id;
    delete payload.idempotency_key;
  }
  const headers = { Authorization: `Bearer ${token}`, "Content-Type": "application/json" };
  if (approvalId) headers["X-QACS-Approval-ID"] = approvalId;
  if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey;
  const response = await fetch(`${baseUrl}${path}`, {
    method,
    headers,
    body: payload === undefined ? undefined : JSON.stringify(payload)
  });
  const responsePayload = await response.json().catch(() => ({}));
  if (!response.ok || !responsePayload.ok) {
    const error = new Error(responsePayload.error?.message || `${response.status} ${response.statusText}`);
    error.code = responsePayload.error?.code;
    error.details = responsePayload.error?.details;
    throw error;
  }
  return responsePayload.data;
}

export function toolResult(data, isError = false) {
  const structuredContent = data && typeof data === "object" && !Array.isArray(data) ? data : { value: data };
  return { content: [{ type: "text", text: summarize(structuredContent) }], structuredContent, isError };
}

function summarize(data) {
  if (data.approval_id) return `Approval required: ${data.summary || data.operation || "sensitive operation"}. Ask the user, then retry the same tool with this approval_id.`;
  if (data.id && data.status && String(data.id).includes("-")) return `Job ${data.id}: ${data.status}${data.kind ? ` (${data.kind})` : ""}.`;
  if (Array.isArray(data.jobs)) return `Returned ${data.jobs.length} jobs.`;
  if (Array.isArray(data.entries)) return `Returned ${data.entries.length} file entries.`;
  if (Array.isArray(data.processes)) return `Returned ${data.processes.length} processes.`;
  if (Array.isArray(data.packages)) return `Returned ${data.packages.length} QPKG packages.`;
  if (Array.isArray(data.disks)) return `Returned ${data.disks.length} disks.`;
  if (data.argv) return `Command completed (exit ${data.exit_code ?? 0}; stdout ${String(data.stdout || "").length} bytes; stderr ${String(data.stderr || "").length} bytes).`;
  const keys = Object.keys(data);
  return keys.length ? `Completed. Structured fields: ${keys.slice(0, 8).join(", ")}${keys.length > 8 ? "…" : ""}.` : "Completed.";
}
