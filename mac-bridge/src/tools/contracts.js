const qpkgQueueActions = new Set(["install_file", "install_url", "update_all", "download"]);

export function normalizeToolOutput(name, args, data) {
  if (name !== "nas_qpkg_manage" || args?.dry_run === true || !qpkgQueueActions.has(args?.action)) return data;
  const base = data && typeof data === "object" && !Array.isArray(data) ? { ...data } : { value: data };
  return {
    ...base,
    operation_phase: "queued_or_processing",
    completion_verified: false,
    verification_required: true,
    verification: {
      tool: "nas_qpkg_list",
      reason: "QTS qpkg_cli exit 0 acknowledges queue acceptance and is not proof that installation or update has completed.",
      check: "Wait for the QTS install queue, then confirm package registration/version, process state, QACS health and capability state before treating the operation as complete."
    }
  };
}

export function structuredToolError(error) {
  const reason = error?.message || String(error || "unknown QACS error");
  const sourceCode = String(error?.code || "").trim();
  const combined = `${sourceCode} ${reason}`.toLowerCase();
  let status = "failed";
  let code = "EXECUTION_FAILED";
  let retriable = Boolean(error?.details?.retriable);

  if (/no verified|backend unavailable|adapter.*(unavailable|not configured|no verified)|no .*backend|backend.*not supported/.test(combined)) {
    status = "unavailable";
    code = "BACKEND_UNAVAILABLE";
  } else if (/capability.*unavailable|capability.*not supported/.test(combined)) {
    status = "unavailable";
    code = "CAPABILITY_UNAVAILABLE";
  } else if (/unsupported action|invalid|bad[_ -]?request/.test(combined)) {
    status = "failed";
    code = "INVALID_ARGUMENT";
  } else if (/\bunsupported\b|not supported/.test(combined)) {
    status = "unavailable";
    code = "CAPABILITY_UNAVAILABLE";
  } else if (/not[_ -]?found|404/.test(combined)) {
    status = "unavailable";
    code = "RESOURCE_NOT_FOUND";
  } else if (/timeout|timed out|abort/.test(combined)) {
    status = "blocked";
    code = "TIMEOUT";
    retriable = true;
  } else if (/\bbusy\b/.test(combined)) {
    status = "blocked";
    code = "BUSY";
    retriable = true;
  } else if (/conflict/.test(combined)) {
    status = "blocked";
    code = "CONFLICT";
  } else if (/fetch failed|econnreset|econnrefused|socket hang up/.test(combined)) {
    status = "unknown";
    code = "EXECUTION_FAILED";
    retriable = true;
  }

  const output = { ok: false, status, code, reason, retriable };
  if (sourceCode) output.source_code = sourceCode;
  return output;
}

export function normalizeStorageSettledResult(result, kind) {
  if (result?.status !== "fulfilled") return structuredToolError(result?.reason);
  return normalizeStorageCommandResult(result.value, kind);
}

export function normalizeStorageCommandResult(value, kind) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return {
      ok: false,
      status: "failed",
      code: "INVALID_BACKEND_RESPONSE",
      reason: "Storage Manager backend returned a non-object response",
      retriable: false,
      items: []
    };
  }

  const command = commandMetadata(value);
  const stdout = typeof value.stdout === "string" ? value.stdout : "";
  const executable = command.argv?.[0] || "";
  const qcli = /(^|\/)qcli_storage$/.test(executable);
  if (!qcli) {
    return {
      ok: true,
      status: "available",
      backend: "manual_adapter",
      verified: true,
      parser: "raw_adapter_result",
      items: [],
      parse_status: "unparsed",
      loss: {
        lossy: true,
        reason: "The verified manual adapter does not expose the qcli_storage table contract; raw command data is preserved."
      },
      command,
      raw: value
    };
  }

  const items = parseQCLIStorage(stdout, kind);
  return {
    ok: true,
    status: "available",
    backend: "qcli_storage",
    verified: true,
    parser: "qcli_storage_table_v1",
    parse_status: "parsed",
    items,
    loss: {
      lossy: true,
      reason: "QTS qcli_storage column layouts vary by firmware. Stable fields are extracted while every original row is retained in fields/raw."
    },
    command,
    raw: {
      stdout,
      stderr: typeof value.stderr === "string" ? value.stderr : ""
    }
  };
}

export function parseQCLIStorage(stdout, kind) {
  return qcliRows(stdout).map((fields) => {
    const item = { fields, raw: fields.join(" ") };
    if (kind === "volumes") {
      if (/^\d+$/.test(fields[0] || "")) item.id = fields[0];
      if (fields.length > 1) item.name = fields[1];
      const mountpoint = fields.find((field) => field.startsWith("/"));
      if (mountpoint) item.mountpoint = mountpoint;
    } else if (kind === "pools") {
      const device = fields.find((field) => field.startsWith("/dev/"));
      if (device) item.device = device;
    }
    return item;
  });
}

function qcliRows(stdout) {
  const rows = [];
  for (const rawLine of String(stdout || "").split("\n")) {
    const line = rawLine.trim();
    if (!line || /^[\-_=+\s]+$/.test(line)) continue;
    const fields = line.split(/\s+/).filter(Boolean);
    if (!fields.length || looksLikeQCLIHeader(fields)) continue;
    rows.push(fields);
  }
  return rows;
}

function looksLikeQCLIHeader(fields) {
  const line = fields.join(" ").toLowerCase();
  const compact = line.replace(/[^a-z0-9]/g, "");
  return (line.includes("volume") && line.includes("name")) ||
    (line.includes("disk") && line.includes("model")) ||
    (line.includes("pool") && line.includes("name")) ||
    (compact.includes("volid") && compact.includes("volname"));
}

function commandMetadata(value) {
  const metadata = {};
  if (Array.isArray(value.argv)) metadata.argv = [...value.argv];
  if (Number.isInteger(value.exit_code)) metadata.exit_code = value.exit_code;
  if (typeof value.dry_run === "boolean") metadata.dry_run = value.dry_run;
  if (Number.isFinite(value.duration_ms)) metadata.duration_ms = value.duration_ms;
  return metadata;
}
