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

type Config struct {
	Listen          string        `json:"listen"`
	TokenSHA256     string        `json:"token_sha256"`
	AllowedRoots    []string      `json:"allowed_roots"`
	AllowedCommands []string      `json:"allowed_commands"`
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
		AuditLog:        "/var/log/qnap-ai-control-agent/audit.jsonl",
		MaxReadBytes:    2 * 1024 * 1024,
		TimeoutSeconds:  30,
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
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>QNAP AI Control</title>
  <style>
    body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#101820;color:#f5f7fb}
    main{max-width:760px;margin:0 auto;padding:48px 24px}
    .panel{border:1px solid #314357;background:#172331;border-radius:8px;padding:28px}
    h1{margin:0 0 12px;font-size:28px;font-weight:650}
    p{line-height:1.55;color:#c8d3df}
    code{background:#0b1118;border:1px solid #2b3b4d;border-radius:5px;padding:2px 6px;color:#b7f7d4}
    .status{display:inline-block;margin:10px 0 18px;padding:6px 10px;border-radius:999px;background:#123b2b;color:#74f0a7;font-weight:650}
    ul{padding-left:20px;color:#c8d3df}
  </style>
</head>
<body>
  <main>
    <section class="panel">
      <h1>QNAP AI Control</h1>
      <div class="status">Running on %s</div>
      <p>This NAS control agent is installed and listening on <code>%s</code>.</p>
      <ul>
        <li>API health endpoint: <code>/v1/health</code></li>
        <li>API requests require <code>Authorization: Bearer &lt;token&gt;</code>.</li>
        <li>Initial token file: <code>/etc/config/qnap-ai-control-agent/initial-token.txt</code></li>
      </ul>
    </section>
  </main>
</body>
</html>`, html.EscapeString(s.hostname), html.EscapeString(s.cfg.Listen))
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
		},
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
	default:
		return op, nil, fmt.Errorf("unsupported sensitive operation: %s", op.Operation)
	}
}

func (s *Server) runAllowedCommand(req commandRequest) (commandResponse, error) {
	start := time.Now()
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
	if req.DryRun {
		return commandResponse{Argv: req.Argv, DryRun: true}, nil
	}
	timeout := s.cfg.CommandTimeout
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
	err = cmd.Run()
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
