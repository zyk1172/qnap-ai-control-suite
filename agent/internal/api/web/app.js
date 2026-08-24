(() => {
  "use strict";

  const MIN_TOKEN_LENGTH = 24;
  const SNAPSHOT_CACHE_MS = 20_000;
  const API_TIMEOUT_MS = 30_000;
  const RECOMMENDED_TOOLSETS = ["core", "files", "docker", "storage", "network", "qnap", "admin"];
  const CUSTOM_TOOLSETS = ["core", "files", "docker", "storage", "network", "qnap", "admin", "raw", "compat"];
  const VIEW_TITLES = { overview: "概览", connect: "接入", system: "系统", logs: "日志" };
  const CLIENT_LABELS = { generic: "通用 MCP", codex: "Codex", hermes: "Hermes", openclaw: "OpenClaw" };

  const icons = {
    grid: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="3" y="3" width="5" height="5" rx="1"/><rect x="12" y="3" width="5" height="5" rx="1"/><rect x="3" y="12" width="5" height="5" rx="1"/><rect x="12" y="12" width="5" height="5" rx="1"/></svg>',
    key: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="7" cy="13" r="3"/><path d="m9.2 10.8 6.5-6.5 1.3 1.3-1.6 1.6 1.2 1.2-1.5 1.5-1.2-1.2-2.3 2.3"/></svg>',
    sliders: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M4 5h12M4 10h12M4 15h12"/><circle cx="8" cy="5" r="1.5" fill="currentColor"/><circle cx="12" cy="10" r="1.5" fill="currentColor"/><circle cx="7" cy="15" r="1.5" fill="currentColor"/></svg>',
    list: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M7 5h10M7 10h10M7 15h10"/><path d="M3.5 5h.01M3.5 10h.01M3.5 15h.01" stroke-linecap="round" stroke-width="2.5"/></svg>',
    menu: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M3 5h14M3 10h14M3 15h14"/></svg>',
    refresh: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M16 7.5A6 6 0 1 0 17 12"/><path d="M16 3.5v4h-4"/></svg>',
    arrow: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M4 10h11M11 5l5 5-5 5"/></svg>',
    eye: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2.8 10s2.5-4 7.2-4 7.2 4 7.2 4-2.5 4-7.2 4-7.2-4-7.2-4Z"/><circle cx="10" cy="10" r="1.8"/></svg>',
    copy: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="6.5" y="6.5" width="9" height="10" rx="1.5"/><path d="M13.5 6.5V4.8A1.3 1.3 0 0 0 12.2 3.5H5A1.5 1.5 0 0 0 3.5 5v8A1.5 1.5 0 0 0 5 14.5h1.5"/></svg>',
    edit: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5"><path d="m4 14.8-.6 2.3 2.3-.6L15.8 6.4a1.8 1.8 0 0 0-2.5-2.5L4 14.8Z"/><path d="m12.5 5.5 2 2"/></svg>',
    spark: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5"><path d="m10 2 1.3 5.4L17 9l-5.7 1.6L10 16l-1.3-5.4L3 9l5.7-1.6L10 2Z"/><path d="m16 14 .5 2 2 .5-2 .5-.5 2-.5-2-2-.5 2-.5.5-2Z"/></svg>',
    close: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6"><path d="m5 5 10 10M15 5 5 15"/></svg>'
  };

  const state = {
    token: "",
    view: "overview",
    client: "generic",
    toolset: "recommended",
    customToolsets: [...RECOMMENDED_TOOLSETS],
    snapshot: null,
    settings: null,
    tokenMetadata: null,
    snapshotAt: 0,
    logTab: "audit",
    logs: { audit: [], service: [], jobs: [] },
    logQuery: ""
  };

  class APIError extends Error {
    constructor(status, code, message, details) {
      super(message || code || "API request failed");
      this.name = "APIError";
      this.status = status;
      this.code = code || "request_failed";
      this.details = details;
    }
  }

  const $ = (id) => document.getElementById(id);
  const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const text = (value) => value === null || value === undefined ? "—" : String(value);

  function escapeHTML(value) {
    return String(value === null || value === undefined ? "" : value)
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#39;");
  }

  function formatBytes(value) {
    const number = Number(value);
    if (!Number.isFinite(number) || number < 0) return "—";
    if (number < 1024) return `${number} B`;
    const units = ["KiB", "MiB", "GiB", "TiB"];
    let current = number;
    let index = -1;
    do { current /= 1024; index += 1; } while (current >= 1024 && index < units.length - 1);
    return `${current.toFixed(current >= 10 ? 0 : 1)} ${units[index]}`;
  }

  function formatDuration(seconds) {
    const total = Number(seconds);
    if (!Number.isFinite(total) || total < 0) return "—";
    const days = Math.floor(total / 86400);
    const hours = Math.floor((total % 86400) / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    if (days > 0) return `${days}天 ${hours}小时`;
    if (hours > 0) return `${hours}小时 ${minutes}分钟`;
    return `${minutes}分钟`;
  }

  function formatTime(value) {
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString();
  }

  function formatJSON(value) {
    return JSON.stringify(value, null, 2);
  }

  function memoryValues(memory = {}) {
    const totalCandidates = [memory.total, memory.MemTotal, memory.mem_total];
    const availableCandidates = [memory.available, memory.MemAvailable, memory.mem_available, memory.free, memory.MemFree];
    const firstNumber = (values, requirePositive) => {
      for (const value of values) {
        const number = Number(value);
        if (Number.isFinite(number) && number >= 0 && (!requirePositive || number > 0)) return number;
      }
      return 0;
    };
    return {
      total: firstNumber(totalCandidates, true),
      available: firstNumber(availableCandidates, false)
    };
  }

  function setHTML(id, html) {
    const node = $(id);
    if (node) node.innerHTML = html;
  }

  function setText(id, value) {
    const node = $(id);
    if (node) node.textContent = text(value);
  }

  function renderIcons(root = document) {
    $$('[data-icon]', root).forEach((node) => {
      const svg = icons[node.dataset.icon];
      if (svg) node.innerHTML = svg;
    });
  }

  function setConnection(connected, label) {
    ["sidebar-dot", "top-dot"].forEach((id) => {
      const dot = $(id);
      if (!dot) return;
      dot.classList.toggle("is-ok", connected);
      dot.classList.toggle("is-bad", !connected && Boolean(label));
    });
    setText("sidebar-status", label || (connected ? "已连接" : "未连接"));
    setText("top-status", label || (connected ? "已连接" : "未连接"));
  }

  function showToast(message, kind = "normal") {
    const toast = $("toast");
    if (!toast) return;
    toast.textContent = message;
    toast.hidden = false;
    toast.dataset.kind = kind;
    window.clearTimeout(showToast.timer);
    showToast.timer = window.setTimeout(() => { toast.hidden = true; }, 4200);
  }

  function currentToolsets() {
    switch (state.toolset) {
      case "minimal": return ["core"];
      case "all": return ["all"];
      case "custom": return state.customToolsets.length ? state.customToolsets : ["core"];
      default: return RECOMMENDED_TOOLSETS;
    }
  }

  function agentURL() {
    const input = $("agent-url");
    return input && input.value.trim() ? input.value.trim() : window.location.origin;
  }

  function bridgePath() {
    const input = $("bridge-path");
    return input && input.value.trim() ? input.value.trim() : "/path/to/qnap-ai-control-suite/mac-bridge/src/server.js";
  }

  function buildMCPConfig() {
    return {
      command: "node",
      args: [bridgePath()],
      env: {
        QACS_BASE_URL: agentURL(),
        QACS_TOKEN: state.token || "ENTER_CURRENT_TOKEN",
        QACS_TOOLSETS: currentToolsets().join(",")
      }
    };
  }

  function quoted(value) {
    return JSON.stringify(String(value));
  }

  function buildConfigText() {
    const server = buildMCPConfig();
    const serverName = "qnap-ai-control";
    if (state.client === "codex") {
      const args = server.args.map(quoted).join(", ");
      const env = Object.entries(server.env).map(([key, value]) => `${key} = ${quoted(value)}`).join(", ");
      return `[mcp_servers.${serverName}]\ncommand = ${quoted(server.command)}\nargs = [${args}]\nenv = { ${env} }\n`;
    }
    if (state.client === "hermes") {
      return `mcp_servers:\n  ${serverName}:\n    command: ${quoted(server.command)}\n    args:\n      - ${quoted(server.args[0])}\n    env:\n      QACS_BASE_URL: ${quoted(server.env.QACS_BASE_URL)}\n      QACS_TOKEN: ${quoted(server.env.QACS_TOKEN)}\n      QACS_TOOLSETS: ${quoted(server.env.QACS_TOOLSETS)}\n`;
    }
    if (state.client === "openclaw") {
      return formatJSON({ mcp: { servers: { [serverName]: server } } });
    }
    return formatJSON({ mcpServers: { [serverName]: server } });
  }

  function renderConfig() {
    const preview = $("config-preview");
    if (preview) preview.textContent = buildConfigText();
    const format = state.client === "codex" ? "Codex：TOML · mcp_servers" : state.client === "hermes" ? "Hermes：YAML · mcp_servers" : state.client === "openclaw" ? "OpenClaw：JSON5/JSON · mcp.servers" : "通用 MCP：JSON · mcpServers";
    setText("config-format-hint", format);
    $("custom-toolsets")?.toggleAttribute("hidden", state.toolset !== "custom");
    $$("#toolset-choices .choice").forEach((choice) => {
      const input = choice.querySelector("input");
      choice.classList.toggle("is-selected", input?.value === state.toolset);
    });
    $$("#client-choices .choice").forEach((choice) => {
      const input = choice.querySelector("input");
      choice.classList.toggle("is-selected", input?.value === state.client);
    });
  }

  function errorMessage(error) {
    if (!(error instanceof APIError)) return "网络请求失败，请检查 Agent 是否运行。";
    if (error.status === 401 || error.code === "unauthorized") return "Token 无效或已失效，请重新输入。";
    if (error.code === "token_unrecoverable") return "当前 hash-only 配置没有可读取的明文 Token，请重新生成。";
    if (error.code === "token_store_not_writable") return "Token 存储不可写，请检查 QPKG 配置目录权限或以管理员身份运行 Agent。";
    if (error.code === "capability_unavailable") return "该 QNAP 能力当前不可用。";
    return error.message || "请求失败。";
  }

  async function apiFetch(path, options = {}) {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), options.timeout || API_TIMEOUT_MS);
    const headers = new Headers(options.headers || {});
    headers.set("Accept", "application/json");
    if (state.token) headers.set("Authorization", `Bearer ${state.token}`);
    let body = options.body;
    if (body !== undefined && body !== null && typeof body !== "string") {
      headers.set("Content-Type", "application/json");
      body = JSON.stringify(body);
    }
    try {
      const response = await fetch(path, { ...options, body, headers, signal: controller.signal });
      let payload = null;
      try { payload = await response.json(); } catch (_) { payload = null; }
      if (!response.ok || !payload?.ok) {
        const problem = payload?.error || {};
        throw new APIError(response.status, problem.code, problem.message || response.statusText, problem.details);
      }
      return payload.data;
    } catch (error) {
      if (error.name === "AbortError") throw new APIError(408, "request_timeout", "请求超时，请检查 Agent 或网络。");
      throw error;
    } finally {
      window.clearTimeout(timeout);
    }
  }

  async function loadSnapshot(force = false) {
    if (!state.token) throw new APIError(401, "unauthorized", "需要 Bearer Token");
    if (!force && state.snapshot && Date.now() - state.snapshotAt < SNAPSHOT_CACHE_MS) {
      renderSnapshot(state.snapshot);
      return state.snapshot;
    }
    const snapshot = await apiFetch("/v1/status/snapshot", { timeout: 45_000 });
    state.snapshot = snapshot || {};
    state.snapshotAt = Date.now();
    renderSnapshot(state.snapshot);
    return state.snapshot;
  }

  function renderSnapshot(snapshot) {
    const agent = snapshot?.agent || {};
    setText("sidebar-version", `Agent ${agent.version || "—"}`);
    setText("overview-updated", `更新于 ${new Date().toLocaleTimeString()}`);
    renderMetrics(agent, snapshot);
    renderResources(snapshot.resources);
    renderStorage(snapshot.storage);
    renderServices(snapshot);
    renderJobs(snapshot.jobs?.jobs || []);
    renderErrors(snapshot);
    setConnection(true, "已连接");
  }

  function renderMetrics(agent, snapshot) {
    const resources = snapshot.resources || {};
    const memory = memoryValues(resources.memory_bytes);
    const storage = snapshot.storage || {};
    const disks = Array.isArray(storage.disks) ? storage.disks : [];
    const docker = snapshot.docker || {};
    const dockerTotal = docker.summary?.total ?? (docker.containers || []).length;
    setHTML("metrics-grid", [
      metric("Agent", agent.version || "—", agent.profile || "profile 未知"),
      metric("运行时间", formatDuration(agent.uptime_s), agent.host || resources.hostname || "主机未知"),
      metric("内存", formatBytes(memory.total), memory.total ? `${formatBytes(memory.available ?? memory.free)} 可用` : "不可用"),
      metric("服务", docker.supported === false ? "不可用" : `${dockerTotal} 个容器`, `${disks.length} 块磁盘`)
    ].join(""));
  }

  function metric(label, value, detail) {
    return `<div class="metric"><div class="metric-label">${escapeHTML(label)}</div><div class="metric-value">${escapeHTML(value)}</div><div class="metric-detail">${escapeHTML(detail)}</div></div>`;
  }

  function renderResources(resources) {
    const panel = $("resource-panel");
    const status = $("resource-status");
    if (!resources || resources.available === false) {
      if (panel) panel.innerHTML = `<div class="empty-state">${escapeHTML(resources?.reason || "资源数据不可用")}</div>`;
      setBadge(status, "不可用", "warn");
      return;
    }
    const memory = memoryValues(resources.memory_bytes);
    const total = memory.total;
    const available = memory.available;
    const used = total > available ? total - available : 0;
    const percent = total ? Math.min(100, Math.round(used / total * 100)) : 0;
    const loads = Array.isArray(resources.load_average) ? resources.load_average.map((value) => Number(value).toFixed(2)).join(" / ") : "—";
    if (panel) panel.innerHTML = `<div class="resource-main">
      <div><div class="resource-value"><strong>${escapeHTML(formatBytes(used))}</strong><span>${escapeHTML(total ? `${percent}% 已使用` : "不可用")}</span></div><progress class="progress-meter" max="100" value="${percent}" aria-label="内存使用率"></progress></div>
      <div class="mini-list"><div class="mini-row"><span>总内存</span><span>${escapeHTML(formatBytes(total))}</span></div><div class="mini-row"><span>可用内存</span><span>${escapeHTML(formatBytes(available))}</span></div></div>
    </div><div class="mini-list"><div class="mini-row"><span>CPU</span><span>${escapeHTML(resources.cpu_count || "—")} 核</span></div><div class="mini-row"><span>Load</span><span>${escapeHTML(loads)}</span></div><div class="mini-row"><span>Kernel</span><span>${escapeHTML(resources.kernel || "—")}</span></div><div class="mini-row"><span>温度</span><span>${escapeHTML(temperatureSummary())}</span></div></div>`;
    setBadge(status, "正常", "ok");
  }

  function temperatureSummary() {
    const thermal = state.snapshot?.thermal;
    if (!thermal || thermal.available === false) return "未读取";
    const values = (thermal.sensors || []).filter((item) => item.type === "temperature" && Number.isFinite(Number(item.value))).map((item) => Number(item.value));
    return values.length ? `${Math.max(...values).toFixed(1)} °C 峰值` : "未发现传感器";
  }

  function renderStorage(storage) {
    const panel = $("storage-panel");
    if (!storage || storage.available === false) {
      if (panel) panel.innerHTML = `<div class="empty-state">${escapeHTML(storage?.reason || "存储数据不可用")}</div>`;
      setBadge($("storage-status"), "不可用", "warn");
      return;
    }
    const disks = Array.isArray(storage.disks) ? storage.disks : [];
    const raids = Array.isArray(storage.raid_groups) ? storage.raid_groups : [];
    const volumes = Array.isArray(storage.volumes) ? storage.volumes : [];
    const rows = [];
    rows.push(stackRow("磁盘", `${disks.length} 块`));
    if (raids.length) rows.push(stackRow("RAID", raids.map((item) => `${item.name || item.level || "RAID"} ${item.state || "未知"}`).join(" · ")));
    if (volumes.length) rows.push(stackRow("卷", `${volumes.length} 个`));
    if (!rows.length) rows.push(`<div class="empty-state">未发现存储条目</div>`);
    if (panel) panel.innerHTML = rows.join("");
    setBadge($("storage-status"), "已读取", "ok");
  }

  function renderServices(snapshot) {
    const panel = $("service-panel");
    const docker = snapshot.docker;
    const qpkg = snapshot.qpkg;
    const rows = [];
    if (docker?.supported === false) rows.push(stackRow("Docker", "不可用", "warn"));
    else rows.push(stackRow("Docker", `${docker?.summary?.healthy ?? 0} healthy / ${docker?.summary?.unhealthy ?? 0} unhealthy`));
    if (qpkg?.available === false) rows.push(stackRow("QPKG", "不可用", "warn"));
    else rows.push(stackRow("QPKG", `${Array.isArray(qpkg?.packages) ? qpkg.packages.length : 0} 个包`));
    if (panel) panel.innerHTML = rows.join("");
    setBadge($("service-status"), snapshot.partial ? "部分可用" : "正常", snapshot.partial ? "warn" : "ok");
  }

  function stackRow(label, value, kind = "") {
    return `<div class="stack-row"><span>${escapeHTML(label)}</span><span>${kind ? `<span class="status-badge ${kind}">${escapeHTML(value)}</span>` : escapeHTML(value)}</span></div>`;
  }

  function setBadge(node, value, kind) {
    if (!node) return;
    node.textContent = value;
    node.className = `status-badge ${kind || "neutral"}`;
  }

  function renderJobs(jobs) {
    const tbody = $("jobs-table");
    if (!tbody) return;
    if (!Array.isArray(jobs) || !jobs.length) {
      tbody.innerHTML = '<tr><td colspan="4" class="empty-cell">当前没有运行中的 Job</td></tr>';
      return;
    }
    tbody.innerHTML = jobs.slice(0, 20).map((job) => `<tr><td>${escapeHTML(job.kind || job.id || "Job")}</td><td>${escapeHTML(job.resource || "—")}</td><td><span class="status-badge ${job.status === "failed" ? "bad" : job.status === "running" ? "ok" : "neutral"}">${escapeHTML(job.status || "未知")}</span></td><td>${escapeHTML(formatTime(job.started_at || job.created_at))}</td></tr>`).join("");
  }

  function renderErrors(snapshot) {
    const panel = $("errors-panel");
    if (!panel) return;
    const errors = [];
    ["docker", "qpkg", "smb", "storage", "network"].forEach((name) => {
      const value = snapshot[name];
      if (value?.available === false || value?.supported === false && value.reason) errors.push(`${name}: ${value.reason}`);
    });
    if (!errors.length) {
      panel.innerHTML = '<div class="empty-state">暂无异常</div>';
      return;
    }
    panel.innerHTML = errors.slice(0, 5).map((item) => `<div class="stack-row"><span class="status-badge warn">注意</span><span>${escapeHTML(item)}</span></div>`).join("");
  }

  async function connect(force = false) {
    const input = $("token-input");
    const entered = input?.value.trim() || "";
    if (entered) state.token = entered;
    if (!state.token) {
      showToast("请输入当前 Bearer Token。", "error");
      setConnection(false, "需要 Token");
      return;
    }
    try {
      await loadSnapshot(force);
      showToast("已连接到 QNAP AI Control。", "success");
      if (state.view === "connect") await loadTokenMetadata();
      if (state.view === "system") await loadSettings();
    } catch (error) {
      setConnection(false, error instanceof APIError && error.status === 401 ? "Token 无效" : "连接失败");
      showToast(errorMessage(error), "error");
    }
  }

  async function loadTokenMetadata() {
    if (!state.token) return;
    try {
      const metadata = await apiFetch("/v1/admin/token");
      state.tokenMetadata = metadata;
      if (metadata.writable === false) setBadge($("token-status"), "存储不可写", "bad");
      else setBadge($("token-status"), metadata.recoverable ? "已配置" : "仅 hash 可用", metadata.recoverable ? "ok" : "warn");
      $("managed-token").value = metadata.masked || "••••••••••••";
      $("managed-token").type = "text";
      return metadata;
    } catch (error) {
      setBadge($("token-status"), "读取失败", "bad");
      throw error;
    }
  }

  async function revealToken() {
    try {
      const data = await apiFetch("/v1/admin/token/reveal", { method: "POST" });
      state.token = data.token;
      $("token-input").value = state.token;
      const managed = $("managed-token");
      managed.value = state.token;
      managed.type = "text";
      setBadge($("token-status"), "已读取", "ok");
      renderConfig();
      showToast("Token 已读取，仅保留在当前页面。", "success");
    } catch (error) {
      showToast(errorMessage(error), "error");
    }
  }

  async function updateToken(token) {
    if (token.length < MIN_TOKEN_LENGTH) throw new Error(`Token 至少需要 ${MIN_TOKEN_LENGTH} 个字符。`);
    const data = await apiFetch("/v1/admin/token", { method: "PUT", body: { token }, timeout: 30_000 });
    state.token = data.token;
    state.tokenMetadata = data.metadata;
    $("token-input").value = state.token;
    $("managed-token").value = state.token;
    $("managed-token").type = "text";
    renderConfig();
    setBadge($("token-status"), "已更新", "ok");
    showToast("Token 已更新，旧 Token 已立即失效。", "success");
  }

  async function generateToken() {
    const data = await apiFetch("/v1/admin/token/generate", { method: "POST", timeout: 30_000 });
    state.token = data.token;
    state.tokenMetadata = data.metadata;
    $("token-input").value = state.token;
    $("managed-token").value = state.token;
    $("managed-token").type = "text";
    renderConfig();
    setBadge($("token-status"), "已更新", "ok");
    showToast("已生成新的 API Token，请复制并同步客户端。", "success");
  }

  async function loadSettings() {
    if (!state.token) return;
    try {
      state.settings = await apiFetch("/v1/admin/settings");
      renderSettings(state.settings);
    } catch (error) {
      setHTML("settings-panel", `<div class="empty-state">${escapeHTML(errorMessage(error))}</div>`);
    }
  }

  function renderSettings(settings) {
    const approval = settings.approval || {};
    const jobs = settings.jobs || {};
    const permissions = settings.permissions || {};
    setHTML("settings-panel", [
      setting("Profile", settings.profile), setting("Approval mode", approval.mode), setting("Approval TTL", `${approval.ttl_seconds || "—"} 秒`), setting("Job max concurrent", jobs.max_concurrent), setting("Audit", settings.audit_enabled ? "enabled" : "disabled"), setting("Audit redaction", settings.audit_redaction ? "enabled" : "disabled")
    ].join(""));
    setHTML("permission-panel", [
      stackRow("Allowed roots", (permissions.allowed_roots || []).join(", ") || "—"),
      stackRow("Any command", permissions.allow_any_command ? "enabled" : "disabled"),
      stackRow("Shell", permissions.allow_shell ? "enabled" : "disabled")
    ].join(""));
    const adapters = state.snapshot?.adapters || settings.qnap_adapters;
    setHTML("adapter-panel", adapters ? stackRow("已验证适配器", Object.keys(adapters).length) : '<div class="empty-state">未配置已验证适配器</div>');
  }

  function setting(label, value) {
    return `<div class="setting-row"><span>${escapeHTML(label)}</span><span>${escapeHTML(value)}</span></div>`;
  }

  async function loadLogs() {
    if (!state.token) {
      setText("log-output", "连接后读取日志。");
      return;
    }
    try {
      if (state.logTab === "audit") {
        const page = await apiFetch("/v1/audit/tail");
        state.logs.audit = page?.lines || page?.events || [];
      } else if (state.logTab === "service") {
        const page = await apiFetch("/v1/logs/tail", { method: "POST", body: { name: "service", limit: 200 } });
        state.logs.service = page?.lines || [];
      } else {
        const data = await apiFetch("/v1/jobs");
        state.logs.jobs = data?.jobs || [];
      }
      renderLogs();
    } catch (error) {
      setText("log-output", errorMessage(error));
      setText("log-count", "读取失败");
    }
  }

  function renderLogs() {
    const items = state.logs[state.logTab] || [];
    const query = state.logQuery.trim().toLowerCase();
    const filtered = items.filter((item) => {
      const value = typeof item === "string" ? item : formatJSON(item);
      return !query || value.toLowerCase().includes(query);
    });
    const output = filtered.map((item) => typeof item === "string" ? item : formatJSON(item)).join("\n");
    setText("log-output", output || "没有匹配的日志。");
    setText("log-count", `${filtered.length} / ${items.length} 条`);
  }

  async function copyText(value, success = "已复制") {
    if (!value) {
      showToast("没有可复制的内容。", "error");
      return;
    }
    try {
      await navigator.clipboard.writeText(value);
    } catch (_) {
      const area = document.createElement("textarea");
      area.value = value;
      area.className = "copy-fallback";
      document.body.appendChild(area);
      area.select();
      document.execCommand("copy");
      area.remove();
    }
    showToast(`${success} ✓`, "success");
  }

  function setView(view) {
    if (!VIEW_TITLES[view]) return;
    state.view = view;
    setText("page-title", VIEW_TITLES[view]);
    $$('[data-view]').forEach((section) => {
      const active = section.dataset.view === view;
      section.classList.toggle("is-active", active);
      section.hidden = !active;
    });
    $$('[data-view-target]').forEach((item) => {
      const active = item.dataset.viewTarget === view;
      item.classList.toggle("is-active", active);
      if (item.matches(".nav-item")) {
        if (active) item.setAttribute("aria-current", "page");
        else item.removeAttribute("aria-current");
      }
    });
    closeMenu();
    if (view === "connect" && state.token) loadTokenMetadata().catch((error) => showToast(errorMessage(error), "error"));
    if (view === "system" && state.token) loadSettings();
    if (view === "logs") loadLogs();
  }

  function closeMenu() {
    $("sidebar")?.classList.remove("is-open");
    $("scrim")?.classList.remove("is-visible");
    $("menu-button")?.setAttribute("aria-expanded", "false");
  }

  function openMenu() {
    $("sidebar")?.classList.add("is-open");
    $("scrim")?.classList.add("is-visible");
    $("menu-button")?.setAttribute("aria-expanded", "true");
  }

  function bindEvents() {
    $$('[data-view-target]').forEach((element) => element.addEventListener("click", () => setView(element.dataset.viewTarget)));
    $("menu-button")?.addEventListener("click", () => $("sidebar")?.classList.contains("is-open") ? closeMenu() : openMenu());
    $("scrim")?.addEventListener("click", closeMenu);
    $("connect-form")?.addEventListener("submit", (event) => { event.preventDefault(); connect(true); });
    $("token-input")?.addEventListener("keydown", (event) => { if (event.key === "Enter") connect(true); });
    $("refresh-button")?.addEventListener("click", () => connect(true));
    $("reveal-token")?.addEventListener("click", revealToken);
    $("toggle-token")?.addEventListener("click", () => {
      const input = $("managed-token");
      input.type = input.type === "password" ? "text" : "password";
    });
    $("copy-token")?.addEventListener("click", () => copyText(state.token, "Token 已复制"));
    $("copy-agent-url")?.addEventListener("click", () => copyText(agentURL(), "Agent 地址已复制"));
    $("copy-config")?.addEventListener("click", () => copyText(buildConfigText(), "MCP 配置已复制"));
    $("agent-url")?.addEventListener("input", renderConfig);
    $("bridge-path")?.addEventListener("input", () => { localStorage.setItem("qacs.bridgePath", $("bridge-path").value); renderConfig(); });
    $$('input[name="client"]').forEach((input) => input.addEventListener("change", () => { state.client = input.value; renderConfig(); }));
    $$('input[name="toolset"]').forEach((input) => input.addEventListener("change", () => { state.toolset = input.value; renderConfig(); }));
    $$("#custom-toolsets input[type=checkbox]").forEach((input) => input.addEventListener("change", () => {
      state.customToolsets = $$("#custom-toolsets input:checked").map((item) => item.value);
      renderConfig();
    }));
    $("set-token")?.addEventListener("click", () => { $("token-form-error").hidden = true; $("new-token").value = ""; $("new-token-confirm").value = ""; $("token-dialog").showModal(); });
    $("token-form")?.addEventListener("submit", async (event) => {
      if (event.submitter?.id !== "save-token") return;
      event.preventDefault();
      const token = $("new-token").value;
      const confirm = $("new-token-confirm").value;
      const error = $("token-form-error");
      if (token !== confirm) { error.textContent = "两次输入的 Token 不一致。"; error.hidden = false; return; }
      if (token.length < MIN_TOKEN_LENGTH) { error.textContent = `Token 至少需要 ${MIN_TOKEN_LENGTH} 个字符。`; error.hidden = false; return; }
      try { await updateToken(token); $("token-dialog").close(); } catch (requestError) { error.textContent = errorMessage(requestError); error.hidden = false; }
    });
    $("generate-token")?.addEventListener("click", () => $("confirm-dialog").showModal());
    $("confirm-form")?.addEventListener("submit", async (event) => {
      if (event.submitter?.id !== "confirm-generate") return;
      event.preventDefault();
      try { await generateToken(); $("confirm-dialog").close(); } catch (error) { $("confirm-dialog").close(); showToast(errorMessage(error), "error"); }
    });
    $$('[data-log-tab]').forEach((tab) => tab.addEventListener("click", () => {
      state.logTab = tab.dataset.logTab;
      $$('[data-log-tab]').forEach((item) => { const active = item === tab; item.classList.toggle("is-active", active); item.setAttribute("aria-selected", String(active)); });
      loadLogs();
    }));
    $("logs-refresh")?.addEventListener("click", loadLogs);
    $("log-query")?.addEventListener("input", (event) => { state.logQuery = event.target.value; renderLogs(); });
  }

  function boot() {
    const savedPath = localStorage.getItem("qacs.bridgePath");
    if (savedPath) $("bridge-path").value = savedPath;
    $("agent-url").value = window.location.origin;
    renderIcons();
    bindEvents();
    renderConfig();
    setConnection(false, "未连接");
    setView("overview");
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", boot);
  else boot();
})();
