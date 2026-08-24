export const baseUrl = (process.env.QACS_BASE_URL || "http://NAS_IP:8756").replace(/\/$/, "");
export const token = process.env.QACS_TOKEN || "";
export const defaultHttpTimeoutMs = 30_000;
export const longRequestTimeoutMs = 90_000;
export const defaultApprovalTimeoutMs = 300_000;
export const maxApprovalTimeoutMs = 540_000;

export function configuredHttpTimeoutMs() {
  const value = Number(process.env.QACS_HTTP_TIMEOUT_MS);
  if (!Number.isSafeInteger(value) || value <= 0) return null;
  return Math.min(value, 10 * 60 * 1000);
}

export function approvalRequestTimeoutMs() {
  const value = Number(process.env.QACS_APPROVAL_TIMEOUT_MS);
  if (!Number.isSafeInteger(value) || value <= 0) return defaultApprovalTimeoutMs;
  return Math.min(value, maxApprovalTimeoutMs);
}

const configuredToolsets = (process.env.QACS_TOOLSETS || "core").split(",").map((value) => value.trim().toLowerCase()).filter(Boolean);
export const toolsets = new Set(configuredToolsets.length ? configuredToolsets : ["core"]);

export function toolsetEnabled(name) {
  return toolsets.has("all") || toolsets.has(name);
}

export function requireConfiguration() {
  if (!token) throw new Error("QACS_TOKEN is required");
  if (baseUrl.includes("NAS_IP")) throw new Error("QACS_BASE_URL must point to the NAS agent");
}
