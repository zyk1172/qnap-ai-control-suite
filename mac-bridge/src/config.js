export const baseUrl = (process.env.QACS_BASE_URL || "http://NAS_IP:8756").replace(/\/$/, "");
export const token = process.env.QACS_TOKEN || "";

export function requireConfiguration() {
  if (!token) throw new Error("QACS_TOKEN is required");
  if (baseUrl.includes("NAS_IP")) throw new Error("QACS_BASE_URL must point to the NAS agent");
}
