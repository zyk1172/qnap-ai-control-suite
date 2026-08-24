import { baseUrl, configuredHttpTimeoutMs, defaultHttpTimeoutMs, longRequestTimeoutMs, token, requireConfiguration } from "./config.js";

export async function request(method, path, body, options = {}) {
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
  const timeoutMs = resolveTimeoutMs(method, path, payload, options.timeoutMs);
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  let response;
  let responsePayload = {};
  try {
    response = await fetch(`${baseUrl}${path}`, {
      method,
      headers,
      body: payload === undefined ? undefined : JSON.stringify(payload),
      signal: controller.signal
    });
    try {
      responsePayload = await response.json();
    } catch (error) {
      if (error?.name === "AbortError") throw error;
    }
  } catch (error) {
    if (error?.name === "AbortError") {
      const timeoutError = new Error(`QACS HTTP request timed out after ${timeoutMs} ms`);
      timeoutError.code = "qacs_http_timeout";
      timeoutError.timeoutMs = timeoutMs;
      throw timeoutError;
    }
    throw error;
  } finally {
    clearTimeout(timer);
  }
  if (!response.ok || !responsePayload.ok) {
    const error = new Error(responsePayload.error?.message || `${response.status} ${response.statusText}`);
    error.code = responsePayload.error?.code;
    error.details = responsePayload.error?.details;
    throw error;
  }
  return responsePayload.data;
}

export function resolveTimeoutMs(method, path, body, explicitTimeoutMs) {
  const configured = configuredHttpTimeoutMs();
  if (configured !== null) return configured;
  if (Number.isSafeInteger(explicitTimeoutMs) && explicitTimeoutMs > 0) return Math.min(explicitTimeoutMs, 10 * 60 * 1000);
  if (String(method).toUpperCase() === "POST" && (path === "/v1/jobs" || body?.async === true)) return longRequestTimeoutMs;
  return defaultHttpTimeoutMs;
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
