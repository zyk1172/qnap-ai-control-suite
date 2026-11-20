import { AsyncLocalStorage } from "node:async_hooks";
import { randomUUID } from "node:crypto";
import { baseUrl, configuredHttpTimeoutMs, defaultHttpTimeoutMs, longRequestTimeoutMs, maxHttpTimeoutMs, requireConfiguration, token } from "./config.js";

const requestSignal = new AsyncLocalStorage();

// A tool invocation owns its initial request, approval decision, and retry.
export function withRequestSignal(signal, run) {
  return requestSignal.run(signal, run);
}

// A prepared request is transport-only data. It contains the exact serialized
// body used for the first attempt, but never the bearer token. Approval retry
// can therefore reuse it without reconstructing tool arguments or changing
// the request binding.
export function prepareRequest(method, path, body, options = {}) {
  requireConfiguration();
  const payload = body && typeof body === "object" && !Array.isArray(body) ? { ...body } : body;
  const approvalId = payload?.approval_id;
  const idempotencyKey = payload?.idempotency_key;
  if (payload && typeof payload === "object" && !Array.isArray(payload)) {
    delete payload.approval_id;
    delete payload.idempotency_key;
  }
  return {
    method: String(method).toUpperCase(),
    path: String(path),
    bodyText: payload === undefined ? undefined : JSON.stringify(payload),
    approvalId,
    idempotencyKey,
    timeoutMs: resolveTimeoutMs(method, path, payload, options.timeoutMs),
    requestId: options.requestId || randomUUID()
  };
}

export async function request(method, path, body, options = {}) {
  return sendPrepared(prepareRequest(method, path, body, options));
}

// Approval is intentionally a single retry, not a general retry loop. The
// server-side ticket is single-use, so any failed retry is surfaced to the
// caller and must not be replayed automatically.
export async function retryRequest(requestSpec, approvalId) {
  if (!requestSpec || typeof requestSpec !== "object" || !requestSpec.method || !requestSpec.path || !requestSpec.requestId) {
    const error = new Error("Cannot retry an unbound QACS request");
    error.code = "approval_request_unavailable";
    throw error;
  }
  if (typeof approvalId !== "string" || !approvalId.trim()) {
    const error = new Error("approval_id is required for an approved retry");
    error.code = "approval_id_missing";
    throw error;
  }
  return sendPrepared({ ...requestSpec, approvalId });
}

async function sendPrepared(prepared) {
  requireConfiguration();
  const callerSignal = requestSignal.getStore();
  callerSignal?.throwIfAborted();
  const headers = {
    Authorization: `Bearer ${token}`,
    "Content-Type": "application/json",
    "X-Request-ID": prepared.requestId
  };
  if (prepared.approvalId) headers["X-QACS-Approval-ID"] = prepared.approvalId;
  if (prepared.idempotencyKey) headers["Idempotency-Key"] = prepared.idempotencyKey;
  const controller = new AbortController();
  const cancel = () => controller.abort(callerSignal.reason);
  callerSignal?.addEventListener("abort", cancel, { once: true });
  const timer = setTimeout(() => {
    const error = new Error(`QACS HTTP request timed out after ${prepared.timeoutMs} ms`);
    error.code = "qacs_http_timeout";
    error.timeoutMs = prepared.timeoutMs;
    controller.abort(error);
  }, prepared.timeoutMs);
  let response;
  let responsePayload = {};
  try {
    response = await fetch(`${baseUrl}${prepared.path}`, {
      method: prepared.method,
      headers,
      body: prepared.bodyText,
      signal: controller.signal
    });
    try {
      responsePayload = await response.json();
    } catch {
      controller.signal.throwIfAborted();
    }
  } catch (error) {
    controller.signal.throwIfAborted();
    throw error;
  } finally {
    clearTimeout(timer);
    callerSignal?.removeEventListener("abort", cancel);
  }
  if (!response.ok || !responsePayload.ok) {
    const error = new Error(responsePayload.error?.message || `${response.status} ${response.statusText}`);
    error.code = responsePayload.error?.code;
    error.details = responsePayload.error?.details;
    if (error.code === "approval_required") error.requestSpec = prepared;
    throw error;
  }
  return responsePayload.data;
}

export function resolveTimeoutMs(method, path, body, explicitTimeoutMs) {
  const configured = configuredHttpTimeoutMs();
  if (configured !== null) return configured;
  if (Number.isSafeInteger(explicitTimeoutMs) && explicitTimeoutMs > 0) return Math.min(explicitTimeoutMs, maxHttpTimeoutMs);
  if (String(method).toUpperCase() !== "POST") return defaultHttpTimeoutMs;
  if (path === "/v1/jobs" || body?.async === true) return longRequestTimeoutMs;
  const commandSeconds = body?.timeout_sec;
  if (body?.dry_run !== true && Number.isSafeInteger(commandSeconds) && commandSeconds > 0) {
    // Allow time for the agent's process cancellation and the HTTP response.
    return Math.min(maxHttpTimeoutMs, Math.max(defaultHttpTimeoutMs, commandSeconds * 1000 + 10_000));
  }
  return defaultHttpTimeoutMs;
}

export function toolResult(data, isError = false) {
  const structuredContent = data && typeof data === "object" && !Array.isArray(data) ? data : { value: data };
  return { content: [{ type: "text", text: summarize(structuredContent) }], structuredContent, isError };
}

function summarize(data) {
  if (data.approval_status === "approval_denied" || data.approval_status === "denied") return data.message || "User denied this sensitive operation.";
  if (data.approval_status === "approval_interaction_unavailable") return data.message || "Sensitive operation requires explicit user approval, but the current MCP client does not support interactive approval.";
  if (data.approval_status === "approval_expired") return data.message || "Approval expired before the operation could run. Start the operation again.";
  if (data.approval_status === "approval_used") return data.message || "This approval was already used; the operation was not retried.";
  if (data.approval_status === "approval_mismatch") return data.message || "The approval did not match the original request; the operation was not retried.";
  if (data.approval_status === "approval_not_approved") return data.message || "The approval was not recorded as approved; the operation was not executed.";
  if (data.approval_status && data.message) return data.message;
  if (data.approval_id) return `Approval required: ${data.summary || data.operation || "sensitive operation"}. The MCP Bridge will request explicit user approval before retrying this operation.`;
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
