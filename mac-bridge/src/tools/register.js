import * as z from "zod/v4";
import { toolResult } from "../client.js";

export { z };
export const outputSchema = z.object({}).passthrough();
export function register(server, name, description, inputSchema, call, annotations = {}) {
  server.registerTool(name, { description, inputSchema, outputSchema, annotations }, async (args) => toolResult(await call(args)));
}
