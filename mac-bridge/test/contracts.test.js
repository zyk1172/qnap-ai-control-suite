import assert from "node:assert/strict";
import test from "node:test";

import {
  normalizeStorageCommandResult,
  normalizeStorageSettledResult,
  normalizeToolOutput,
  parseQCLIStorage,
  structuredToolError
} from "../src/tools/contracts.js";

test("parses qcli_storage pools into stable items while retaining raw fields", () => {
  const stdout = `Pool  Name  Status  Device\n-----------------------------\n1 pool1 Ready /dev/mapper/cachedev1\n`;
  assert.deepEqual(parseQCLIStorage(stdout, "pools"), [
    {
      fields: ["1", "pool1", "Ready", "/dev/mapper/cachedev1"],
      raw: "1 pool1 Ready /dev/mapper/cachedev1",
      device: "/dev/mapper/cachedev1"
    }
  ]);
});

test("parses qcli_storage volumes into typed id/name/mountpoint", () => {
  const stdout = `Volume Name Status Mount\n------------------------\n2 DataVol1 Ready /share/CACHEDEV1_DATA\n`;
  assert.deepEqual(parseQCLIStorage(stdout, "volumes"), [
    {
      fields: ["2", "DataVol1", "Ready", "/share/CACHEDEV1_DATA"],
      raw: "2 DataVol1 Ready /share/CACHEDEV1_DATA",
      id: "2",
      name: "DataVol1",
      mountpoint: "/share/CACHEDEV1_DATA"
    }
  ]);
});

test("ignores qcli_storage volume headers using VolID and VolName columns", () => {
  const stdout = `VolID   VolName                 Mount_Path\n1       DataVol1                /share/CACHEDEV1_DATA\n2       SSD                     /share/CACHEDEV5_DATA\n`;
  assert.deepEqual(parseQCLIStorage(stdout, "volumes"), [
    {
      fields: ["1", "DataVol1", "/share/CACHEDEV1_DATA"],
      raw: "1 DataVol1 /share/CACHEDEV1_DATA",
      id: "1",
      name: "DataVol1",
      mountpoint: "/share/CACHEDEV1_DATA"
    },
    {
      fields: ["2", "SSD", "/share/CACHEDEV5_DATA"],
      raw: "2 SSD /share/CACHEDEV5_DATA",
      id: "2",
      name: "SSD",
      mountpoint: "/share/CACHEDEV5_DATA"
    }
  ]);
});

test("normalizes verified qcli_storage command results into items plus loss metadata", () => {
  const result = normalizeStorageCommandResult({
    argv: ["/sbin/qcli_storage", "-v"],
    exit_code: 0,
    stdout: "Volume Name Status Mount\n1 Public Ready /share/Public\n",
    stderr: ""
  }, "volumes");
  assert.equal(result.ok, true);
  assert.equal(result.backend, "qcli_storage");
  assert.equal(result.parse_status, "parsed");
  assert.equal(result.items.length, 1);
  assert.equal(result.items[0].mountpoint, "/share/Public");
  assert.equal(result.loss.lossy, true);
  assert.equal(result.command.exit_code, 0);
});

test("manual adapter output stays explicit instead of being falsely parsed as qcli", () => {
  const result = normalizeStorageCommandResult({ argv: ["/opt/custom/storagectl", "list"], exit_code: 0, stdout: "opaque" }, "pools");
  assert.equal(result.ok, true);
  assert.equal(result.backend, "manual_adapter");
  assert.equal(result.parse_status, "unparsed");
  assert.deepEqual(result.items, []);
  assert.equal(result.raw.stdout, "opaque");
});

test("backend failures return stable status/code/reason fields", () => {
  const error = new Error("virtualization_station adapter has no verified command configuration");
  error.code = "bad_request";
  const normalized = structuredToolError(error);
  assert.equal(normalized.ok, false);
  assert.equal(normalized.status, "unavailable");
  assert.equal(normalized.code, "BACKEND_UNAVAILABLE");
  assert.equal(normalized.source_code, "bad_request");
  assert.match(normalized.reason, /no verified/i);
});

test("unsupported actions remain invalid arguments rather than backend failures", () => {
  const error = new Error("unsupported qpkg action");
  error.code = "bad_request";
  const normalized = structuredToolError(error);
  assert.equal(normalized.status, "failed");
  assert.equal(normalized.code, "INVALID_ARGUMENT");
});

test("settled storage failures use the same structured backend error contract", () => {
  const error = new Error("backend unavailable");
  error.code = "execution_failed";
  const normalized = normalizeStorageSettledResult({ status: "rejected", reason: error }, "pools");
  assert.equal(normalized.status, "unavailable");
  assert.equal(normalized.code, "BACKEND_UNAVAILABLE");
});

test("QPKG queue-like operations are acknowledged but not reported complete", () => {
  const output = normalizeToolOutput("nas_qpkg_manage", { action: "install_file", path: "/share/qacs-test.qpkg" }, { argv: ["/sbin/qpkg_cli", "--manually", "/share/qacs-test.qpkg"], exit_code: 0 });
  assert.equal(output.operation_phase, "queued_or_processing");
  assert.equal(output.completion_verified, false);
  assert.equal(output.verification_required, true);
  assert.equal(output.verification.tool, "nas_qpkg_list");
});

test("QPKG dry-run is not mislabeled as queued", () => {
  const input = { action: "install_file", path: "/share/qacs-test.qpkg", dry_run: true };
  const output = normalizeToolOutput("nas_qpkg_manage", input, { dry_run: true, argv: ["/sbin/qpkg_cli", "--manually", "/share/qacs-test.qpkg"] });
  assert.equal(output.operation_phase, undefined);
  assert.equal(output.verification_required, undefined);
});

test("transport timeouts are machine-readable and retriable", () => {
  const error = new Error("QACS HTTP request timed out after 30000 ms");
  error.code = "qacs_http_timeout";
  const normalized = structuredToolError(error);
  assert.equal(normalized.status, "blocked");
  assert.equal(normalized.code, "TIMEOUT");
  assert.equal(normalized.retriable, true);
});
