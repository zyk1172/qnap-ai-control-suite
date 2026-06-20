package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultConfigPath = "/etc/config/qnap-ai-control-agent/config.json"

const indexPage = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>QNAP AI Control</title>
  <style>
    :root{color-scheme:dark;--bg:#0d141b;--panel:#151f2a;--panel2:#1b2733;--line:#314357;--text:#f5f7fb;--muted:#bdc8d5;--accent:#36d399;--accent2:#64b5f6;--warn:#f5c451;--bad:#ff7676}
    *{box-sizing:border-box}
    body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Noto Sans SC",sans-serif;background:var(--bg);color:var(--text)}
    header{border-bottom:1px solid var(--line);background:#101923}
    .wrap{max-width:1120px;margin:0 auto;padding:24px}
    .top{display:flex;align-items:center;gap:16px}
    .mark{width:54px;height:54px;border-radius:8px;background:#e8f1f6;display:grid;place-items:center;flex:0 0 auto}
    .mark svg{width:42px;height:42px}
    h1{margin:0;font-size:26px;font-weight:680;letter-spacing:0}
    h2{margin:0 0 12px;font-size:20px;font-weight:650}
    h3{margin:18px 0 8px;font-size:15px;color:#dce6f2}
    p{line-height:1.58;color:var(--muted);margin:8px 0}
    main{max-width:1120px;margin:0 auto;padding:24px;display:grid;grid-template-columns:360px 1fr;gap:18px}
    section,.card{border:1px solid var(--line);background:var(--panel);border-radius:8px;padding:18px}
    .stack{display:grid;gap:18px}
    label{display:block;color:#dce6f2;font-weight:600;margin:14px 0 6px}
    input{width:100%;height:40px;border:1px solid #40556c;background:#0b1118;color:var(--text);border-radius:6px;padding:0 10px;font-size:14px}
    button{height:38px;border:1px solid #42607a;background:#233244;color:#f5f7fb;border-radius:6px;padding:0 12px;font-weight:650;cursor:pointer}
    button.primary{background:#157a55;border-color:#1b9468}
    button:disabled{opacity:.55;cursor:not-allowed}
    .row{display:flex;gap:8px;align-items:center;flex-wrap:wrap}
    .status{display:inline-flex;align-items:center;gap:7px;margin-top:12px;padding:7px 10px;border-radius:999px;background:#172331;color:var(--muted);font-weight:650}
    .dot{width:8px;height:8px;border-radius:50%;background:var(--warn)}
    .ok .dot{background:var(--accent)} .bad .dot{background:var(--bad)}
    code,pre{background:#0b1118;border:1px solid #2b3b4d;border-radius:6px;color:#b7f7d4}
    code{padding:2px 6px}
    pre{padding:12px;overflow:auto;white-space:pre-wrap;line-height:1.45}
    .tabs{display:flex;gap:6px;margin-bottom:12px}
    .tabs button{height:34px}
    .tabs button.active{background:#2d4054;border-color:#5f7893}
    .guide{display:grid;grid-template-columns:1fr 1fr;gap:14px}
    .guide svg{width:100%;height:auto;border:1px solid #2d4054;background:#101923;border-radius:8px}
    ul{padding-left:20px;color:var(--muted);line-height:1.58}
    .small{font-size:13px;color:#93a6b8}
    .hidden{display:none}
    @media(max-width:880px){main{grid-template-columns:1fr}.guide{grid-template-columns:1fr}.top{align-items:flex-start}}
  </style>
</head>
<body>
  <header>
    <div class="wrap top">
      <div class="mark" aria-hidden="true">
        <svg viewBox="0 0 64 64" fill="none">
          <rect x="11" y="23" width="42" height="27" rx="5" fill="#233244"/>
          <rect x="16" y="28" width="12" height="17" rx="2" fill="#dce6f2"/>
          <rect x="34" y="28" width="12" height="17" rx="2" fill="#dce6f2"/>
          <circle cx="49" cy="46" r="2" fill="#36d399"/>
          <path d="M32 8v9M20 15l7 6M44 15l-7 6" stroke="#157a55" stroke-width="4" stroke-linecap="round"/>
          <circle cx="32" cy="8" r="4" fill="#157a55"/>
          <circle cx="20" cy="15" r="3" fill="#64b5f6"/>
          <circle cx="44" cy="15" r="3" fill="#64b5f6"/>
        </svg>
      </div>
      <div>
        <h1>QNAP AI Control</h1>
        <p>运行在 <code>{{HOST}}</code>，监听 <code>{{LISTEN}}</code>。在这里保存浏览器 token、测试 API，并查看 Codex/OpenClaw/Hermes 的 MCP 接入教程。</p>
      </div>
    </div>
  </header>
  <main>
    <aside class="stack">
      <section>
        <h2>Token 设置</h2>
        <p>把 NAS 上的初始 token 填到这里。它只保存在当前浏览器的 <code>localStorage</code>，不会写入 NAS 配置文件。</p>
        <label for="token">Bearer token</label>
        <input id="token" type="password" autocomplete="off" placeholder="粘贴 initial-token.txt 内容">
        <div class="row" style="margin-top:10px">
          <button class="primary" id="saveToken">保存到浏览器</button>
          <button id="showToken">显示</button>
          <button id="clearToken">清除</button>
        </div>
        <div class="row" style="margin-top:10px">
          <button id="testHealth">测试连接</button>
          <button id="loadCapabilities">读取能力</button>
        </div>
        <div id="connStatus" class="status"><span class="dot"></span><span>未测试</span></div>
        <p class="small">token 文件位置：<code>/etc/config/qnap-ai-control-agent/initial-token.txt</code></p>
      </section>
      <section>
        <h2>当前能力</h2>
        <pre id="capabilities">点击“读取能力”后显示。</pre>
      </section>
    </aside>
    <div class="stack">
      <section>
        <h2>快速使用教程</h2>
        <div class="tabs">
          <button class="active" data-tab="setup">1. 设置 token</button>
          <button data-tab="mcp">2. 配置 MCP</button>
          <button data-tab="confirm">3. 确认敏感操作</button>
        </div>
        <div id="tab-setup">
          <h3>第一步：获取 token</h3>
          <p>通过 SSH 或 File Station 查看 <code>/etc/config/qnap-ai-control-agent/initial-token.txt</code>，复制完整内容，粘贴到左侧 Token 设置。</p>
          <h3>第二步：测试</h3>
          <p>点击“测试连接”。成功时会调用 <code>/v1/health</code>，返回 NAS 主机名、运行时间和安全 profile。</p>
          <div class="guide">
            <svg viewBox="0 0 560 260" role="img" aria-label="Token flow diagram">
              <defs><marker id="arrow1" markerWidth="10" markerHeight="10" refX="8" refY="3" orient="auto"><path d="M0,0 L0,6 L9,3 z" fill="#64b5f6"/></marker></defs>
              <rect x="24" y="42" width="135" height="78" rx="8" fill="#172331" stroke="#40556c"/>
              <text x="91" y="75" fill="#f5f7fb" text-anchor="middle" font-size="18">NAS token</text>
              <text x="91" y="101" fill="#bdc8d5" text-anchor="middle" font-size="13">initial-token.txt</text>
              <rect x="214" y="42" width="135" height="78" rx="8" fill="#172331" stroke="#40556c"/>
              <text x="281" y="75" fill="#f5f7fb" text-anchor="middle" font-size="18">WebUI</text>
              <text x="281" y="101" fill="#bdc8d5" text-anchor="middle" font-size="13">localStorage</text>
              <rect x="404" y="42" width="135" height="78" rx="8" fill="#172331" stroke="#40556c"/>
              <text x="471" y="75" fill="#f5f7fb" text-anchor="middle" font-size="18">API</text>
              <text x="471" y="101" fill="#bdc8d5" text-anchor="middle" font-size="13">Bearer header</text>
              <path d="M160 82 H208" stroke="#64b5f6" stroke-width="3" marker-end="url(#arrow1)"/>
              <path d="M350 82 H398" stroke="#64b5f6" stroke-width="3" marker-end="url(#arrow1)"/>
              <text x="280" y="168" fill="#36d399" text-anchor="middle" font-size="16">token 不公开显示，不写入页面源码</text>
            </svg>
            <div class="card">
              <h3>安全边界</h3>
              <ul>
                <li>WebUI 只保存你输入的 token。</li>
                <li>服务端真实 token hash 仍在 <code>config.json</code>。</li>
                <li>清除浏览器数据会移除 WebUI token。</li>
              </ul>
            </div>
          </div>
        </div>
        <div id="tab-mcp" class="hidden">
          <p>Mac 端 MCP bridge 使用同一个 NAS 地址和 token。下面的配置会根据当前 WebUI 地址生成，填到 Codex、OpenClaw 或 Hermes 的 MCP server 配置里。</p>
          <div class="row">
            <button id="copyEnv">复制环境变量</button>
            <button id="copyJson">复制 MCP JSON</button>
          </div>
          <pre id="mcpConfig"></pre>
          <div class="guide">
            <svg viewBox="0 0 560 260" role="img" aria-label="MCP flow diagram">
              <defs><marker id="arrow2" markerWidth="10" markerHeight="10" refX="8" refY="3" orient="auto"><path d="M0,0 L0,6 L9,3 z" fill="#36d399"/></marker></defs>
              <rect x="24" y="46" width="120" height="78" rx="8" fill="#172331" stroke="#40556c"/>
              <text x="84" y="80" fill="#f5f7fb" text-anchor="middle" font-size="17">Codex</text>
              <text x="84" y="104" fill="#bdc8d5" text-anchor="middle" font-size="12">OpenClaw/Hermes</text>
              <rect x="214" y="46" width="132" height="78" rx="8" fill="#172331" stroke="#40556c"/>
              <text x="280" y="80" fill="#f5f7fb" text-anchor="middle" font-size="17">MCP bridge</text>
              <text x="280" y="104" fill="#bdc8d5" text-anchor="middle" font-size="12">mac-bridge</text>
              <rect x="416" y="46" width="120" height="78" rx="8" fill="#172331" stroke="#40556c"/>
              <text x="476" y="80" fill="#f5f7fb" text-anchor="middle" font-size="17">NAS agent</text>
              <text x="476" y="104" fill="#bdc8d5" text-anchor="middle" font-size="12">8756 API</text>
              <path d="M146 86 H208" stroke="#36d399" stroke-width="3" marker-end="url(#arrow2)"/>
              <path d="M348 86 H410" stroke="#36d399" stroke-width="3" marker-end="url(#arrow2)"/>
              <text x="280" y="170" fill="#bdc8d5" text-anchor="middle" font-size="15">MCP 工具调用会转成受控 NAS API 请求</text>
            </svg>
            <div class="card">
              <h3>Agent 添加 MCP</h3>
              <ul>
                <li>server 名称填 <code>qnap-ai-control</code>。</li>
                <li>command 填 <code>node</code>。</li>
                <li>args 填 Mac 上 <code>mac-bridge/src/mcp-server.js</code> 的绝对路径。</li>
                <li>env 填 <code>QACS_BASE_URL</code> 和 <code>QACS_TOKEN</code>。</li>
                <li>重载 agent 后确认工具列表出现 <code>nas_docker_containers</code>。</li>
              </ul>
              <pre>node /path/to/qnap-ai-control-suite/mac-bridge/src/mcp-server.js</pre>
            </div>
          </div>
        </div>
        <div id="tab-confirm" class="hidden">
          <p>写文件、执行命令、启停套件、启停容器属于敏感操作。默认流程是先 prepare，确认短语正确后才 confirm 执行。</p>
          <div class="guide">
            <svg viewBox="0 0 560 280" role="img" aria-label="Sensitive operation confirmation flow">
              <defs><marker id="arrow3" markerWidth="10" markerHeight="10" refX="8" refY="3" orient="auto"><path d="M0,0 L0,6 L9,3 z" fill="#f5c451"/></marker></defs>
              <rect x="28" y="44" width="120" height="70" rx="8" fill="#172331" stroke="#40556c"/>
              <text x="88" y="76" fill="#f5f7fb" text-anchor="middle" font-size="16">prepare</text>
              <text x="88" y="99" fill="#bdc8d5" text-anchor="middle" font-size="12">生成待确认</text>
              <rect x="220" y="44" width="120" height="70" rx="8" fill="#172331" stroke="#40556c"/>
              <text x="280" y="76" fill="#f5f7fb" text-anchor="middle" font-size="16">人工检查</text>
              <text x="280" y="99" fill="#bdc8d5" text-anchor="middle" font-size="12">summary + phrase</text>
              <rect x="412" y="44" width="120" height="70" rx="8" fill="#172331" stroke="#40556c"/>
              <text x="472" y="76" fill="#f5f7fb" text-anchor="middle" font-size="16">confirm</text>
              <text x="472" y="99" fill="#bdc8d5" text-anchor="middle" font-size="12">执行并审计</text>
              <path d="M150 80 H214" stroke="#f5c451" stroke-width="3" marker-end="url(#arrow3)"/>
              <path d="M342 80 H406" stroke="#f5c451" stroke-width="3" marker-end="url(#arrow3)"/>
              <rect x="105" y="164" width="350" height="54" rx="8" fill="#201c12" stroke="#8a6a1c"/>
              <text x="280" y="197" fill="#f5c451" text-anchor="middle" font-size="16">10 分钟过期，agent 重启后失效</text>
            </svg>
            <div class="card">
              <h3>敏感操作</h3>
              <ul>
                <li><code>file_write</code> 写文件</li>
                <li><code>command_run</code> 执行 allowlist 命令</li>
                <li><code>qpkg_action</code> 启停/重启 QPKG</li>
                <li><code>docker_action</code> 启停/暂停容器</li>
              </ul>
            </div>
          </div>
        </div>
      </section>
      <section>
        <h2>API 入口</h2>
        <pre id="apiList">GET  /v1/health
GET  /v1/capabilities
GET  /v1/system/overview
GET  /v1/system/processes
POST /v1/files/list
POST /v1/files/read
POST /v1/files/write
POST /v1/command/run
GET  /v1/qnap/qpkg
POST /v1/qnap/qpkg/action
GET  /v1/docker/info
GET  /v1/docker/containers
GET  /v1/docker/images
POST /v1/docker/inspect
POST /v1/docker/logs
POST /v1/docker/action
POST /v1/operations/prepare
POST /v1/operations/confirm</pre>
      </section>
    </div>
  </main>
  <script>
    const tokenInput = document.getElementById('token');
    const connStatus = document.getElementById('connStatus');
    const capabilities = document.getElementById('capabilities');
    const mcpConfig = document.getElementById('mcpConfig');
    const baseUrl = location.origin;

    function token(){ return tokenInput.value.trim(); }
    function setStatus(kind, text){
      connStatus.className = 'status ' + kind;
      connStatus.querySelector('span:last-child').textContent = text;
    }
    function authHeaders(){ return { 'Authorization': 'Bearer ' + token() }; }
    function updateMcpConfig(){
      const tok = token() || 'REPLACE_WITH_TOKEN';
      const env = 'QACS_BASE_URL=' + baseUrl + '\nQACS_TOKEN=' + tok;
      const json = JSON.stringify({mcpServers:{'qnap-ai-control':{command:'node',args:['/path/to/qnap-ai-control-suite/mac-bridge/src/mcp-server.js'],env:{QACS_BASE_URL:baseUrl,QACS_TOKEN:tok}}}}, null, 2);
      mcpConfig.textContent = env + '\n\n' + json;
    }
    async function copyText(text){
      await navigator.clipboard.writeText(text);
      setStatus('ok', '已复制到剪贴板');
    }

    tokenInput.value = localStorage.getItem('qacsToken') || '';
    updateMcpConfig();
    tokenInput.addEventListener('input', updateMcpConfig);
    document.getElementById('saveToken').onclick = () => {
      localStorage.setItem('qacsToken', token());
      updateMcpConfig();
      setStatus('ok', 'token 已保存到此浏览器');
    };
    document.getElementById('showToken').onclick = (e) => {
      tokenInput.type = tokenInput.type === 'password' ? 'text' : 'password';
      e.target.textContent = tokenInput.type === 'password' ? '显示' : '隐藏';
    };
    document.getElementById('clearToken').onclick = () => {
      localStorage.removeItem('qacsToken');
      tokenInput.value = '';
      updateMcpConfig();
      setStatus('', 'token 已清除');
    };
    document.getElementById('testHealth').onclick = async () => {
      try {
        const res = await fetch('/v1/health', {headers: authHeaders()});
        const data = await res.json();
        if(!res.ok) throw new Error(data.error || res.statusText);
        setStatus('ok', '连接成功：' + data.host + ' / ' + data.profile);
      } catch (err) {
        setStatus('bad', '连接失败：' + err.message);
      }
    };
    document.getElementById('loadCapabilities').onclick = async () => {
      try {
        const res = await fetch('/v1/capabilities', {headers: authHeaders()});
        const data = await res.json();
        if(!res.ok) throw new Error(data.error || res.statusText);
        capabilities.textContent = JSON.stringify(data, null, 2);
        setStatus('ok', '能力读取成功');
      } catch (err) {
        capabilities.textContent = err.message;
        setStatus('bad', '能力读取失败');
      }
    };
    document.getElementById('copyEnv').onclick = () => copyText('QACS_BASE_URL=' + baseUrl + '\nQACS_TOKEN=' + (token() || 'REPLACE_WITH_TOKEN'));
    document.getElementById('copyJson').onclick = () => copyText(JSON.stringify({mcpServers:{'qnap-ai-control':{command:'node',args:['/path/to/qnap-ai-control-suite/mac-bridge/src/mcp-server.js'],env:{QACS_BASE_URL:baseUrl,QACS_TOKEN:token() || 'REPLACE_WITH_TOKEN'}}}}, null, 2));
    document.querySelectorAll('.tabs button').forEach(btn => btn.onclick = () => {
      document.querySelectorAll('.tabs button').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      ['setup','mcp','confirm'].forEach(name => document.getElementById('tab-' + name).classList.toggle('hidden', name !== btn.dataset.tab));
    });
  </script>
</body>
</html>`

type Config struct {
	Listen          string        `json:"listen"`
	TokenSHA256     string        `json:"token_sha256"`
	AllowedRoots    []string      `json:"allowed_roots"`
	AllowedCommands []string      `json:"allowed_commands"`
	DockerPaths     []string      `json:"docker_paths,omitempty"`
	AllowShell      bool          `json:"allow_shell"`
	AuditLog        string        `json:"audit_log"`
	MaxReadBytes    int64         `json:"max_read_bytes"`
	CommandTimeout  time.Duration `json:"-"`
	TimeoutSeconds  int           `json:"command_timeout_seconds"`
}

type Server struct {
	cfg       Config
	auditMu   sync.Mutex
	pending   map[string]PendingOperation
	pendingMu sync.Mutex
	started   time.Time
	hostname  string
}

type apiError struct {
	Error string `json:"error"`
}

type commandRequest struct {
	Argv       []string `json:"argv"`
	TimeoutSec int      `json:"timeout_sec,omitempty"`
	Stdin      string   `json:"stdin,omitempty"`
	DryRun     bool     `json:"dry_run,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

type commandResponse struct {
	Argv       []string `json:"argv"`
	ExitCode   int      `json:"exit_code"`
	Stdout     string   `json:"stdout"`
	Stderr     string   `json:"stderr"`
	DurationMS int64    `json:"duration_ms"`
	DryRun     bool     `json:"dry_run"`
}

type fileReadRequest struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

type fileReadResponse struct {
	Path          string `json:"path"`
	ContentBase64 string `json:"content_base64"`
	Bytes         int    `json:"bytes"`
	Truncated     bool   `json:"truncated"`
}

type fileWriteRequest struct {
	Path          string `json:"path"`
	ContentBase64 string `json:"content_base64"`
	Mode          string `json:"mode,omitempty"`
	CreateParents bool   `json:"create_parents,omitempty"`
	DryRun        bool   `json:"dry_run,omitempty"`
}

type qpkgActionRequest struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	DryRun bool   `json:"dry_run,omitempty"`
}

type dockerTargetRequest struct {
	Name string `json:"name"`
}

type dockerLogsRequest struct {
	Name string `json:"name"`
	Tail int    `json:"tail,omitempty"`
}

type dockerActionRequest struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	DryRun bool   `json:"dry_run,omitempty"`
}

type getcfgRequest struct {
	Section string `json:"section"`
	Key     string `json:"key"`
	File    string `json:"file,omitempty"`
}

type prepareOperationRequest struct {
	Operation string          `json:"operation"`
	Arguments json.RawMessage `json:"arguments"`
	Reason    string          `json:"reason,omitempty"`
}

type confirmOperationRequest struct {
	ID                 string `json:"id"`
	ConfirmationPhrase string `json:"confirmation_phrase"`
}

type PendingOperation struct {
	ID                 string          `json:"id"`
	Operation          string          `json:"operation"`
	Arguments          json.RawMessage `json:"arguments"`
	Reason             string          `json:"reason,omitempty"`
	Summary            string          `json:"summary"`
	ConfirmationPhrase string          `json:"confirmation_phrase"`
	CreatedAt          time.Time       `json:"created_at"`
	ExpiresAt          time.Time       `json:"expires_at"`
}

func main() {
	configPath := flag.String("config", envOrDefault("QACS_CONFIG", defaultConfigPath), "config file path")
	printToken := flag.Bool("print-token-hash", false, "read token from stdin and print sha256 hex")
	genToken := flag.Bool("generate-token", false, "generate a random API token")
	flag.Parse()

	if *printToken {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(hashToken(strings.TrimSpace(string(b))))
		return
	}
	if *genToken {
		token, err := randomToken()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(token)
		return
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	hostname, _ := os.Hostname()
	s := &Server{cfg: cfg, pending: map[string]PendingOperation{}, started: time.Now(), hostname: hostname}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/v1/health", s.withAuth(s.handleHealth))
	mux.HandleFunc("/v1/capabilities", s.withAuth(s.handleCapabilities))
	mux.HandleFunc("/v1/system/overview", s.withAuth(s.handleSystemOverview))
	mux.HandleFunc("/v1/system/processes", s.withAuth(s.handleSystemProcesses))
	mux.HandleFunc("/v1/audit/tail", s.withAuth(s.handleAuditTail))
	mux.HandleFunc("/v1/files/list", s.withAuth(s.handleFileList))
	mux.HandleFunc("/v1/files/stat", s.withAuth(s.handleFileStat))
	mux.HandleFunc("/v1/files/read", s.withAuth(s.handleFileRead))
	mux.HandleFunc("/v1/files/write", s.withAuth(s.handleFileWrite))
	mux.HandleFunc("/v1/command/run", s.withAuth(s.handleCommandRun))
	mux.HandleFunc("/v1/qnap/qpkg", s.withAuth(s.handleQpkgList))
	mux.HandleFunc("/v1/qnap/qpkg/action", s.withAuth(s.handleQpkgAction))
	mux.HandleFunc("/v1/qnap/getcfg", s.withAuth(s.handleGetcfg))
	mux.HandleFunc("/v1/docker/info", s.withAuth(s.handleDockerInfo))
	mux.HandleFunc("/v1/docker/containers", s.withAuth(s.handleDockerContainers))
	mux.HandleFunc("/v1/docker/images", s.withAuth(s.handleDockerImages))
	mux.HandleFunc("/v1/docker/inspect", s.withAuth(s.handleDockerInspect))
	mux.HandleFunc("/v1/docker/logs", s.withAuth(s.handleDockerLogs))
	mux.HandleFunc("/v1/docker/action", s.withAuth(s.handleDockerAction))
	mux.HandleFunc("/v1/operations/prepare", s.withAuth(s.handlePrepareOperation))
	mux.HandleFunc("/v1/operations/confirm", s.withAuth(s.handleConfirmOperation))
	mux.HandleFunc("/v1/operations/pending", s.withAuth(s.handlePendingOperations))

	log.Printf("qnap-ai-control-agent listening on %s", cfg.Listen)
	log.Fatal(http.ListenAndServe(cfg.Listen, mux))
}

func loadConfig(path string) (Config, error) {
	cfg := Config{
		Listen:          "127.0.0.1:8756",
		AllowedRoots:    []string{"/share"},
		AllowedCommands: []string{"/bin/df", "/bin/ps", "/bin/uname", "/sbin/getcfg", "/sbin/ifconfig", "/sbin/qpkg_cli", "/usr/bin/uptime"},
		DockerPaths: []string{
			"/share/CACHEDEV1_DATA/.qpkg/container-station/bin/docker",
			"/share/CACHEDEV1_DATA/.qpkg/container-station/usr/bin/docker",
			"/share/CACHEDEV2_DATA/.qpkg/container-station/bin/docker",
			"/share/CACHEDEV2_DATA/.qpkg/container-station/usr/bin/docker",
			"/share/CACHEDEV3_DATA/.qpkg/container-station/bin/docker",
			"/share/CACHEDEV3_DATA/.qpkg/container-station/usr/bin/docker",
			"/share/CACHEDEV4_DATA/.qpkg/container-station/bin/docker",
			"/share/CACHEDEV4_DATA/.qpkg/container-station/usr/bin/docker",
			"/share/CACHEDEV5_DATA/.qpkg/container-station/bin/docker",
			"/share/CACHEDEV5_DATA/.qpkg/container-station/usr/bin/docker",
			"/usr/bin/docker",
			"/usr/local/bin/docker",
			"/bin/docker",
		},
		AuditLog:       "/var/log/qnap-ai-control-agent/audit.jsonl",
		MaxReadBytes:   2 * 1024 * 1024,
		TimeoutSeconds: 30,
	}
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, err
		}
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, err
		}
	}
	if cfg.TokenSHA256 == "" {
		return cfg, errors.New("config token_sha256 is required")
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8756"
	}
	if cfg.MaxReadBytes <= 0 {
		cfg.MaxReadBytes = 2 * 1024 * 1024
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 30
	}
	cfg.CommandTimeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	cfg.AllowedRoots = cleanPathList(cfg.AllowedRoots)
	cfg.AllowedCommands = cleanPathList(cfg.AllowedCommands)
	cfg.DockerPaths = cleanPathList(cfg.DockerPaths)
	return cfg, nil
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	page := strings.NewReplacer(
		"{{HOST}}", html.EscapeString(s.hostname),
		"{{LISTEN}}", html.EscapeString(s.cfg.Listen),
	).Replace(indexPage)
	_, _ = io.WriteString(w, page)
}

func (s *Server) withAuth(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !constantTimeTokenMatch(token, s.cfg.TokenSHA256) {
			s.audit(r, "auth.denied", map[string]any{"path": r.URL.Path})
			writeError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"host":     s.hostname,
		"uptime_s": int(time.Since(s.started).Seconds()),
		"profile":  profileName(s.cfg),
	})
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"profile":          profileName(s.cfg),
		"allowed_roots":    s.cfg.AllowedRoots,
		"allowed_commands": s.cfg.AllowedCommands,
		"allow_shell":      s.cfg.AllowShell,
		"max_read_bytes":   s.cfg.MaxReadBytes,
		"sensitive_operations": []string{
			"file_write",
			"command_run",
			"qpkg_action",
			"docker_action",
		},
		"docker_paths":             s.cfg.DockerPaths,
		"confirmation_ttl_seconds": 600,
	})
}

func (s *Server) handleSystemOverview(w http.ResponseWriter, r *http.Request) {
	results := map[string]any{
		"host":       s.hostname,
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"started_at": s.started.Format(time.RFC3339),
	}
	for name, argv := range map[string][]string{
		"uname":  {"/bin/uname", "-a"},
		"uptime": {"/usr/bin/uptime"},
		"df":     {"/bin/df", "-h"},
	} {
		resp, err := s.runAllowedCommand(commandRequest{Argv: argv, TimeoutSec: 10})
		if err == nil {
			results[name] = resp.Stdout
		}
	}
	s.audit(r, "system.overview", nil)
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleSystemProcesses(w http.ResponseWriter, r *http.Request) {
	resp, err := s.runAllowedCommand(commandRequest{Argv: []string{"/bin/ps", "-ef"}, TimeoutSec: 15})
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	s.audit(r, "system.processes", nil)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAuditTail(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("lines"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 500 {
			writeError(w, http.StatusBadRequest, "lines must be between 1 and 500")
			return
		}
		limit = parsed
	}
	lines, err := tailLines(s.cfg.AuditLog, limit, 512*1024)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": s.cfg.AuditLog, "lines": lines})
}

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	clean, err := s.allowedPath(p)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	entries, err := os.ReadDir(clean)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, map[string]any{
			"name":     entry.Name(),
			"path":     filepath.Join(clean, entry.Name()),
			"is_dir":   entry.IsDir(),
			"size":     info.Size(),
			"mode":     info.Mode().String(),
			"modified": info.ModTime().Format(time.RFC3339),
		})
	}
	s.audit(r, "file.list", map[string]any{"path": clean})
	writeJSON(w, http.StatusOK, map[string]any{"path": clean, "entries": out})
}

func (s *Server) handleFileStat(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	clean, err := s.allowedPath(p)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	info, err := os.Stat(clean)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "file.stat", map[string]any{"path": clean})
	writeJSON(w, http.StatusOK, map[string]any{
		"path":     clean,
		"is_dir":   info.IsDir(),
		"size":     info.Size(),
		"mode":     info.Mode().String(),
		"modified": info.ModTime().Format(time.RFC3339),
	})
}

func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	var req fileReadRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	clean, err := s.allowedPath(req.Path)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	limit := req.MaxBytes
	if limit <= 0 || limit > s.cfg.MaxReadBytes {
		limit = s.cfg.MaxReadBytes
	}
	f, err := os.Open(clean)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := int64(len(b)) > limit
	if truncated {
		b = b[:limit]
	}
	s.audit(r, "file.read", map[string]any{"path": clean, "bytes": len(b), "truncated": truncated})
	writeJSON(w, http.StatusOK, fileReadResponse{
		Path:          clean,
		ContentBase64: base64.StdEncoding.EncodeToString(b),
		Bytes:         len(b),
		Truncated:     truncated,
	})
}

func (s *Server) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	var req fileWriteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.writeFile(req)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	s.audit(r, "file.write", result)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) writeFile(req fileWriteRequest) (map[string]any, error) {
	clean, err := s.allowedPath(req.Path)
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		return nil, errors.New("content_base64 is invalid")
	}
	mode := os.FileMode(0644)
	if req.Mode != "" {
		parsed, err := strconv.ParseUint(req.Mode, 8, 32)
		if err != nil {
			return nil, errors.New("mode must be octal, for example 0644")
		}
		mode = os.FileMode(parsed)
	}
	if !req.DryRun {
		if req.CreateParents {
			if err := os.MkdirAll(filepath.Dir(clean), 0755); err != nil {
				return nil, err
			}
		}
		if err := os.WriteFile(clean, b, mode); err != nil {
			return nil, err
		}
	}
	return map[string]any{"path": clean, "bytes": len(b), "dry_run": req.DryRun}, nil
}

func (s *Server) handleCommandRun(w http.ResponseWriter, r *http.Request) {
	var req commandRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	resp, err := s.runAllowedCommand(req)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	s.audit(r, "command.run", map[string]any{
		"argv":      redactArgv(req.Argv),
		"exit_code": resp.ExitCode,
		"dry_run":   req.DryRun,
		"reason":    req.Reason,
	})
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleQpkgList(w http.ResponseWriter, r *http.Request) {
	resp, err := s.runAllowedCommand(commandRequest{Argv: []string{"/sbin/qpkg_cli", "-l"}, TimeoutSec: 20})
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetcfg(w http.ResponseWriter, r *http.Request) {
	var req getcfgRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Section == "" || req.Key == "" {
		writeError(w, http.StatusBadRequest, "section and key are required")
		return
	}
	file := req.File
	if file == "" {
		file = "/etc/config/qpkg.conf"
	}
	clean, err := filepath.Abs(filepath.Clean(file))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !pathWithinRoot(clean, "/etc/config") || !strings.HasSuffix(clean, ".conf") {
		writeError(w, http.StatusForbidden, "getcfg file must be a .conf file under /etc/config")
		return
	}
	resp, err := s.runAllowedCommand(commandRequest{Argv: []string{"/sbin/getcfg", req.Section, req.Key, "-f", clean}, TimeoutSec: 10})
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	s.audit(r, "qnap.getcfg", map[string]any{"section": req.Section, "key": req.Key, "file": clean})
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleQpkgAction(w http.ResponseWriter, r *http.Request) {
	var req qpkgActionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	resp, err := s.runQpkgAction(req)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) runQpkgAction(req qpkgActionRequest) (commandResponse, error) {
	if req.Name == "" {
		return commandResponse{}, errors.New("name is required")
	}
	var flag string
	switch req.Action {
	case "start":
		flag = "-s"
	case "stop":
		flag = "-k"
	case "restart":
		flag = "-r"
	default:
		return commandResponse{}, errors.New("action must be start, stop, or restart")
	}
	return s.runAllowedCommand(commandRequest{Argv: []string{"/sbin/qpkg_cli", flag, req.Name}, TimeoutSec: 30, DryRun: req.DryRun})
}

func (s *Server) handleDockerInfo(w http.ResponseWriter, r *http.Request) {
	version, versionErr := s.runDockerCommand([]string{"version", "--format", "{{json .}}"}, 15, false)
	info, infoErr := s.runDockerCommand([]string{"info", "--format", "{{json .}}"}, 20, false)
	if versionErr != nil && infoErr != nil {
		writeError(w, http.StatusForbidden, versionErr.Error())
		return
	}
	out := map[string]any{}
	if versionErr == nil {
		out["version"] = parseJSONOrRaw(version.Stdout)
		out["version_command"] = version
	} else {
		out["version_error"] = versionErr.Error()
	}
	if infoErr == nil {
		out["info"] = parseJSONOrRaw(info.Stdout)
		out["info_command"] = info
	} else {
		out["info_error"] = infoErr.Error()
	}
	s.audit(r, "docker.info", nil)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDockerContainers(w http.ResponseWriter, r *http.Request) {
	resp, err := s.runDockerCommand([]string{"ps", "-a", "--no-trunc", "--format", "{{json .}}"}, 20, false)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	s.audit(r, "docker.containers", nil)
	writeJSON(w, http.StatusOK, map[string]any{"containers": parseJSONLines(resp.Stdout), "command": resp})
}

func (s *Server) handleDockerImages(w http.ResponseWriter, r *http.Request) {
	resp, err := s.runDockerCommand([]string{"images", "--no-trunc", "--format", "{{json .}}"}, 20, false)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	s.audit(r, "docker.images", nil)
	writeJSON(w, http.StatusOK, map[string]any{"images": parseJSONLines(resp.Stdout), "command": resp})
}

func (s *Server) handleDockerInspect(w http.ResponseWriter, r *http.Request) {
	var req dockerTargetRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	resp, err := s.inspectDockerTarget(req)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	s.audit(r, "docker.inspect", map[string]any{"name": req.Name})
	writeJSON(w, http.StatusOK, map[string]any{"inspect": parseJSONOrRaw(resp.Stdout), "command": resp})
}

func (s *Server) handleDockerLogs(w http.ResponseWriter, r *http.Request) {
	var req dockerLogsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	resp, err := s.readDockerLogs(req)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	s.audit(r, "docker.logs", map[string]any{"name": req.Name, "tail": normalizedDockerTail(req.Tail)})
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDockerAction(w http.ResponseWriter, r *http.Request) {
	var req dockerActionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	resp, err := s.runDockerAction(req)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	s.audit(r, "docker.action", map[string]any{"name": req.Name, "action": req.Action, "dry_run": req.DryRun})
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) inspectDockerTarget(req dockerTargetRequest) (commandResponse, error) {
	if err := validateDockerName(req.Name); err != nil {
		return commandResponse{}, err
	}
	return s.runDockerCommand([]string{"inspect", req.Name}, 20, false)
}

func (s *Server) readDockerLogs(req dockerLogsRequest) (commandResponse, error) {
	if err := validateDockerName(req.Name); err != nil {
		return commandResponse{}, err
	}
	tail := normalizedDockerTail(req.Tail)
	return s.runDockerCommand([]string{"logs", "--tail", strconv.Itoa(tail), req.Name}, 30, false)
}

func (s *Server) runDockerAction(req dockerActionRequest) (commandResponse, error) {
	if err := validateDockerName(req.Name); err != nil {
		return commandResponse{}, err
	}
	switch req.Action {
	case "start", "stop", "restart", "pause", "unpause":
	default:
		return commandResponse{}, errors.New("action must be start, stop, restart, pause, or unpause")
	}
	return s.runDockerCommand([]string{req.Action, req.Name}, 60, req.DryRun)
}

func (s *Server) runDockerCommand(args []string, timeoutSec int, dryRun bool) (commandResponse, error) {
	docker, err := s.findDockerCommand()
	if err != nil {
		return commandResponse{}, err
	}
	return runCommand(commandRequest{Argv: append([]string{docker}, args...), TimeoutSec: timeoutSec, DryRun: dryRun}, time.Duration(timeoutSec)*time.Second)
}

func (s *Server) findDockerCommand() (string, error) {
	for _, candidate := range s.cfg.DockerPaths {
		clean := filepath.Clean(candidate)
		info, err := os.Stat(clean)
		if err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return clean, nil
		}
	}
	if docker, err := exec.LookPath("docker"); err == nil {
		return filepath.Clean(docker), nil
	}
	return "", errors.New("docker CLI was not found; install/start QNAP Container Station or set docker_paths in config.json")
}

func validateDockerName(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if len(name) > 128 {
		return errors.New("name is too long")
	}
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			continue
		}
		return fmt.Errorf("name contains an invalid character at position %d", i)
	}
	return nil
}

func normalizedDockerTail(tail int) int {
	if tail <= 0 {
		return 200
	}
	if tail > 2000 {
		return 2000
	}
	return tail
}

func (s *Server) handlePrepareOperation(w http.ResponseWriter, r *http.Request) {
	var req prepareOperationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	op, err := s.prepareOperation(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "operation.prepare", map[string]any{"id": op.ID, "operation": op.Operation, "summary": op.Summary, "reason": op.Reason})
	writeJSON(w, http.StatusOK, op)
}

func (s *Server) handleConfirmOperation(w http.ResponseWriter, r *http.Request) {
	var req confirmOperationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	op, result, err := s.confirmOperation(req)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	s.audit(r, "operation.confirm", map[string]any{"id": op.ID, "operation": op.Operation, "summary": op.Summary})
	writeJSON(w, http.StatusOK, map[string]any{"operation": op, "result": result})
}

func (s *Server) handlePendingOperations(w http.ResponseWriter, r *http.Request) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	now := time.Now()
	out := []PendingOperation{}
	for id, op := range s.pending {
		if now.After(op.ExpiresAt) {
			delete(s.pending, id)
			continue
		}
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"operations": out})
}

func (s *Server) prepareOperation(req prepareOperationRequest) (PendingOperation, error) {
	if len(req.Arguments) == 0 {
		return PendingOperation{}, errors.New("arguments are required")
	}
	summary, normalized, err := s.validateSensitiveOperation(req.Operation, req.Arguments)
	if err != nil {
		return PendingOperation{}, err
	}
	id, err := randomID(12)
	if err != nil {
		return PendingOperation{}, err
	}
	op := PendingOperation{
		ID:                 id,
		Operation:          req.Operation,
		Arguments:          normalized,
		Reason:             req.Reason,
		Summary:            summary,
		ConfirmationPhrase: "CONFIRM " + id,
		CreatedAt:          time.Now(),
		ExpiresAt:          time.Now().Add(10 * time.Minute),
	}
	s.pendingMu.Lock()
	s.pending[id] = op
	s.pendingMu.Unlock()
	return op, nil
}

func (s *Server) validateSensitiveOperation(operation string, raw json.RawMessage) (string, json.RawMessage, error) {
	switch operation {
	case "file_write":
		var req fileWriteRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return "", nil, err
		}
		req.DryRun = false
		clean, err := s.allowedPath(req.Path)
		if err != nil {
			return "", nil, err
		}
		b, err := base64.StdEncoding.DecodeString(req.ContentBase64)
		if err != nil {
			return "", nil, errors.New("content_base64 is invalid")
		}
		normalized, _ := json.Marshal(req)
		return fmt.Sprintf("write %d bytes to %s", len(b), clean), normalized, nil
	case "command_run":
		var req commandRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return "", nil, err
		}
		req.DryRun = false
		if _, err := s.runAllowedCommand(commandRequest{Argv: req.Argv, TimeoutSec: req.TimeoutSec, Stdin: req.Stdin, DryRun: true}); err != nil {
			return "", nil, err
		}
		normalized, _ := json.Marshal(req)
		return "run command: " + strings.Join(redactArgv(req.Argv), " "), normalized, nil
	case "qpkg_action":
		var req qpkgActionRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return "", nil, err
		}
		req.DryRun = false
		if _, err := s.runQpkgAction(qpkgActionRequest{Name: req.Name, Action: req.Action, DryRun: true}); err != nil {
			return "", nil, err
		}
		normalized, _ := json.Marshal(req)
		return fmt.Sprintf("%s QPKG %s", req.Action, req.Name), normalized, nil
	case "docker_action":
		var req dockerActionRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return "", nil, err
		}
		req.DryRun = false
		if _, err := s.runDockerAction(dockerActionRequest{Name: req.Name, Action: req.Action, DryRun: true}); err != nil {
			return "", nil, err
		}
		normalized, _ := json.Marshal(req)
		return fmt.Sprintf("%s Docker container %s", req.Action, req.Name), normalized, nil
	default:
		return "", nil, fmt.Errorf("unsupported sensitive operation: %s", operation)
	}
}

func (s *Server) confirmOperation(req confirmOperationRequest) (PendingOperation, any, error) {
	s.pendingMu.Lock()
	op, ok := s.pending[req.ID]
	if ok && time.Now().After(op.ExpiresAt) {
		delete(s.pending, req.ID)
		ok = false
	}
	if ok && subtle.ConstantTimeCompare([]byte(req.ConfirmationPhrase), []byte(op.ConfirmationPhrase)) == 1 {
		delete(s.pending, req.ID)
	}
	s.pendingMu.Unlock()
	if !ok {
		return PendingOperation{}, nil, errors.New("operation not found or expired")
	}
	if subtle.ConstantTimeCompare([]byte(req.ConfirmationPhrase), []byte(op.ConfirmationPhrase)) != 1 {
		return PendingOperation{}, nil, errors.New("confirmation phrase does not match")
	}
	switch op.Operation {
	case "file_write":
		var writeReq fileWriteRequest
		if err := json.Unmarshal(op.Arguments, &writeReq); err != nil {
			return op, nil, err
		}
		result, err := s.writeFile(writeReq)
		return op, result, err
	case "command_run":
		var cmdReq commandRequest
		if err := json.Unmarshal(op.Arguments, &cmdReq); err != nil {
			return op, nil, err
		}
		result, err := s.runAllowedCommand(cmdReq)
		return op, result, err
	case "qpkg_action":
		var qpkgReq qpkgActionRequest
		if err := json.Unmarshal(op.Arguments, &qpkgReq); err != nil {
			return op, nil, err
		}
		result, err := s.runQpkgAction(qpkgReq)
		return op, result, err
	case "docker_action":
		var dockerReq dockerActionRequest
		if err := json.Unmarshal(op.Arguments, &dockerReq); err != nil {
			return op, nil, err
		}
		result, err := s.runDockerAction(dockerReq)
		return op, result, err
	default:
		return op, nil, fmt.Errorf("unsupported sensitive operation: %s", op.Operation)
	}
}

func (s *Server) runAllowedCommand(req commandRequest) (commandResponse, error) {
	if len(req.Argv) == 0 || strings.TrimSpace(req.Argv[0]) == "" {
		return commandResponse{}, errors.New("argv is required")
	}
	exe, err := exec.LookPath(req.Argv[0])
	if err == nil {
		req.Argv[0] = exe
	}
	req.Argv[0] = filepath.Clean(req.Argv[0])
	if !s.cfg.AllowShell && isShell(req.Argv[0]) {
		return commandResponse{}, errors.New("shell execution is disabled")
	}
	if !stringIn(req.Argv[0], s.cfg.AllowedCommands) {
		return commandResponse{}, fmt.Errorf("command is not allowed: %s", req.Argv[0])
	}
	return runCommand(req, s.cfg.CommandTimeout)
}

func runCommand(req commandRequest, defaultTimeout time.Duration) (commandResponse, error) {
	start := time.Now()
	if req.DryRun {
		return commandResponse{Argv: req.Argv, DryRun: true}, nil
	}
	timeout := defaultTimeout
	if req.TimeoutSec > 0 {
		timeout = time.Duration(req.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, req.Argv[0], req.Argv[1:]...)
	if req.Stdin != "" {
		cmd.Stdin = strings.NewReader(req.Stdin)
	}
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			stderr.WriteString(err.Error())
		}
	}
	return commandResponse{
		Argv:       req.Argv,
		ExitCode:   exitCode,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

func parseJSONOrRaw(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(trimmed), &out); err == nil {
		return out
	}
	return trimmed
}

func parseJSONLines(raw string) []any {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	out := make([]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, parseJSONOrRaw(line))
	}
	return out
}

func (s *Server) allowedPath(p string) (string, error) {
	clean, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return "", err
	}
	for _, root := range s.cfg.AllowedRoots {
		if pathWithinRoot(clean, root) {
			return clean, nil
		}
	}
	return "", fmt.Errorf("path is outside allowed roots: %s", clean)
}

func pathWithinRoot(p, root string) bool {
	p = filepath.Clean(p)
	root = filepath.Clean(root)
	if p == root {
		return true
	}
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."
}

func cleanPathList(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, p := range in {
		if strings.TrimSpace(p) == "" {
			continue
		}
		clean := filepath.Clean(p)
		if !seen[clean] {
			out = append(out, clean)
			seen[clean] = true
		}
	}
	sort.Strings(out)
	return out
}

func isShell(exe string) bool {
	base := filepath.Base(exe)
	return base == "sh" || base == "bash" || base == "zsh" || base == "ash"
}

func (s *Server) audit(r *http.Request, action string, meta map[string]any) {
	if s.cfg.AuditLog == "" {
		return
	}
	entry := map[string]any{
		"ts":      time.Now().Format(time.RFC3339),
		"remote":  r.RemoteAddr,
		"method":  r.Method,
		"path":    r.URL.Path,
		"action":  action,
		"profile": profileName(s.cfg),
	}
	if meta != nil {
		entry["meta"] = meta
	}
	b, _ := json.Marshal(entry)
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	_ = os.MkdirAll(filepath.Dir(s.cfg.AuditLog), 0755)
	f, err := os.OpenFile(s.cfg.AuditLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return false
	}
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 8*1024*1024))
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func constantTimeTokenMatch(token, expectedHash string) bool {
	actual := hashToken(token)
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expectedHash)) == 1
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomID(bytesLen int) (string, error) {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func tailLines(path string, maxLines int, maxBytes int64) ([]string, error) {
	if path == "" {
		return []string{}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	start := int64(0)
	if info.Size() > maxBytes {
		start = info.Size() - maxBytes
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return nil, err
		}
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(parts) == 1 && parts[0] == "" {
		return []string{}, nil
	}
	if len(parts) > maxLines {
		parts = parts[len(parts)-maxLines:]
	}
	return parts, nil
}

func stringIn(s string, list []string) bool {
	for _, item := range list {
		if s == item {
			return true
		}
	}
	return false
}

func redactArgv(argv []string) []string {
	out := append([]string(nil), argv...)
	for i, arg := range out {
		lower := strings.ToLower(arg)
		if strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "apikey") {
			out[i] = "[REDACTED]"
		}
	}
	return out
}

func profileName(cfg Config) string {
	if cfg.AllowShell {
		return "admin"
	}
	return "restricted"
}
