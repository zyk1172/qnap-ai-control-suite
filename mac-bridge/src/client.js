import { baseUrl, token, requireConfiguration } from "./config.js";

export async function request(method, path, body) {
  requireConfiguration();
  const response = await fetch(`${baseUrl}${path}`, {
    method,
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body)
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok || !payload.ok) {
    const error = new Error(payload.error?.message || `${response.status} ${response.statusText}`);
    error.code = payload.error?.code;
    error.details = payload.error?.details;
    throw error;
  }
  return payload.data;
}

export function toolResult(data) {
  const structuredContent = data && typeof data === "object" && !Array.isArray(data) ? data : { value: data };
  return { content: [{ type: "text", text: JSON.stringify(structuredContent, null, 2) }], structuredContent };
}
