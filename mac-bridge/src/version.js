import { readFileSync } from "node:fs";

export const version = readFileSync(new URL("../../VERSION", import.meta.url), "utf8").trim();
