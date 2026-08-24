package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"qnap-ai-control-suite/agent/internal/approval"
	"qnap-ai-control-suite/agent/internal/audit"
	"qnap-ai-control-suite/agent/internal/auth"
	"qnap-ai-control-suite/agent/internal/config"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"qnap-ai-control-suite/agent/internal/files"
	"qnap-ai-control-suite/agent/internal/jobs"
	"qnap-ai-control-suite/agent/internal/operations"
	"qnap-ai-control-suite/agent/internal/qnap/discovery"
	"qnap-ai-control-suite/agent/internal/qnap/docker"
	"qnap-ai-control-suite/agent/internal/qnap/ecosystem"
	"qnap-ai-control-suite/agent/internal/qnap/logs"
	qnetwork "qnap-ai-control-suite/agent/internal/qnap/network"
	"qnap-ai-control-suite/agent/internal/qnap/qpkg"
	"qnap-ai-control-suite/agent/internal/qnap/shares"
	"qnap-ai-control-suite/agent/internal/qnap/storage"
	qsystem "qnap-ai-control-suite/agent/internal/qnap/system"
	"qnap-ai-control-suite/agent/internal/qnap/users"
)

type Server struct {
	Config          config.Config
	Auth            *auth.AuthManager
	TokenStore      *auth.TokenStore
	ConfigPath      string
	Exec            qexec.Executor
	Files           files.Service
	Jobs            *jobs.Manager
	Audit           *audit.Logger
	Operations      operations.Registry
	Approvals       *approval.Manager
	Docker          docker.Service
	QPKG            qpkg.Service
	Discovery       discovery.Service
	System          qsystem.Service
	Network         qnetwork.Service
	Storage         storage.Service
	Users           users.Service
	Shares          shares.Service
	Logs            logs.Service
	Ecosystem       ecosystem.Service
	ProbePath       string
	started         time.Time
	hostname        string
	tokenRotationMu sync.Mutex
	thermalRun      func(context.Context, qexec.Request) (qexec.Result, error)
}

// Version is injected by scripts/build_agent.sh from the repository VERSION file.
var Version = "1.0.0"

type envelope struct {
	OK    bool     `json:"ok"`
	Data  any      `json:"data,omitempty"`
	Error *problem `json:"error,omitempty"`
	Meta  meta     `json:"meta"`
}
type problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}
type meta struct {
	RequestID  string `json:"request_id"`
	DurationMS int64  `json:"duration_ms"`
}
type requestContext struct {
	id      string
	started time.Time
}

func New(cfg config.Config) *Server {
	executor := qexec.Executor{DefaultTimeout: cfg.Timeout(), MaxOutput: cfg.Command.MaxOutputBytes}
	host, _ := os.Hostname()
	auditRedaction := true
	if cfg.Audit.RedactSecrets != nil {
		auditRedaction = *cfg.Audit.RedactSecrets
	}
	server := &Server{Config: cfg, Auth: auth.NewAuthManager(cfg.Auth.TokenSHA256), Exec: executor, Files: files.Service{Roots: cfg.Permissions.AllowedRoots, MaxInlineBytes: cfg.Files.MaxInlineBytes}, Audit: &audit.Logger{Enabled: cfg.Audit.Enabled, Path: cfg.Audit.Path, RedactSecrets: auditRedaction}, Operations: operations.New(), Approvals: approval.New(time.Duration(cfg.Approval.TTLSeconds) * time.Second), Docker: docker.Service{Exec: executor, Paths: cfg.DockerPaths, RedactSecrets: cfg.Privacy.RedactSecrets}, QPKG: qpkg.Service{Exec: executor}, Discovery: discovery.Service{Exec: executor}, System: qsystem.Service{Exec: executor}, Network: qnetwork.Service{Exec: executor}, Storage: storage.Service{Exec: executor}, Users: users.Service{Exec: executor}, Shares: shares.Service{Exec: executor}, Logs: logs.Service{AuditPath: cfg.Audit.Path, ServicePath: "/var/log/qnap-ai-control-agent/service.log"}, Ecosystem: ecosystem.Service{Discovery: discovery.Service{Exec: executor}, Exec: executor, Adapters: cfg.QNAPAdapters}, ProbePath: defaultProbePath(), started: time.Now(), hostname: host}
	server.Jobs = jobs.NewWithOptions(jobs.Options{MaxHistory: cfg.Jobs.MaxHistory, MaxConcurrent: cfg.Jobs.MaxConcurrent, JournalPath: cfg.Jobs.JournalPath, OnEvent: server.auditJobEvent})
	server.thermalRun = func(ctx context.Context, req qexec.Request) (qexec.Result, error) {
		return server.Exec.Run(ctx, req)
	}
	return server
}

func NewWithTokenStore(cfg config.Config, configPath string, store *auth.TokenStore) *Server {
	server := New(cfg)
	server.ConfigPath = configPath
	server.TokenStore = store
	return server
}

func defaultProbePath() string {
	binary, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(binary), "qnap-ai-control-probe")
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", s.webHandler())
	mux.Handle("/v1/", s.auth(s.operationControl(http.HandlerFunc(s.routes))))
	return s.requestID(mux)
}
func (s *Server) Run(ctx context.Context) error {
	server := &http.Server{Addr: s.Config.Listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 120 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
func SignalContext() context.Context {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	_ = cancel
	return ctx
}
func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = randomID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestKey{}, requestContext{id: id, started: time.Now()})))
	})
}

type requestKey struct{}

func rc(r *http.Request) requestContext {
	v, _ := r.Context().Value(requestKey{}).(requestContext)
	return v
}
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual := bearerToken(r)
		if actual == "" {
			s.fail(w, r, http.StatusUnauthorized, "unauthorized", "missing bearer token", nil)
			return
		}
		if s.Auth == nil || !s.Auth.Verify(actual) {
			s.audit(r, "auth.denied", "failed", nil, 0, "invalid bearer token")
			s.fail(w, r, http.StatusUnauthorized, "unauthorized", "invalid bearer token", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(header, "Bearer ")
}
func (s *Server) routes(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/health":
		s.health(w, r)
	case "/v1/capabilities":
		s.capabilities(w, r)
	case "/v1/qnap/discovery":
		s.discovery(w, r)
	case "/v1/qnap/probe":
		s.qnapProbe(w, r)
	case "/v1/system/overview":
		s.systemOverview(w, r)
	case "/v1/status/snapshot":
		s.statusSnapshot(w, r)
	case "/v1/system/info":
		s.systemInfo(w, r)
	case "/v1/system/resources":
		s.systemResources(w, r)
	case "/v1/system/sockets":
		s.systemSockets(w, r)
	case "/v1/system/ntp":
		s.systemNTP(w, r)
	case "/v1/system/processes":
		s.systemProcesses(w, r)
	case "/v1/system/process/action":
		s.processAction(w, r)
	case "/v1/system/services":
		s.services(w, r)
	case "/v1/system/services/action":
		s.serviceAction(w, r)
	case "/v1/system/reboot", "/v1/system/shutdown":
		s.powerRoute(w, r)
	case "/v1/system/thermal":
		s.thermal(w, r)
	case "/v1/system/power":
		s.power(w, r)
	case "/v1/network/info":
		s.network(w, r)
	case "/v1/network/interfaces":
		s.networkInterfaces(w, r)
	case "/v1/network/routes":
		s.networkRoutes(w, r)
	case "/v1/network/ipv6/routes":
		s.networkIPv6Routes(w, r)
	case "/v1/network/ipv6/neighbors":
		s.networkIPv6Neighbors(w, r)
	case "/v1/network/dns":
		s.networkDNS(w, r)
	case "/v1/network/virtual-switches":
		s.virtualSwitches(w, r)
	case "/v1/network/manage":
		s.networkManage(w, r)
	case "/v1/storage/overview":
		s.storage(w, r)
	case "/v1/audit/tail":
		s.auditTail(w, r)
	case "/v1/logs":
		s.logSources(w, r)
	case "/v1/logs/tail":
		s.logTail(w, r)
	case "/v1/qnap/ecosystem":
		s.ecosystem(w, r)
	case "/v1/qnap/ups":
		s.ups(w, r)
	case "/v1/qnap/vm/action":
		s.ecosystemCommand(w, r, "virtualization_station")
	case "/v1/qnap/hbs/action":
		s.ecosystemCommand(w, r, "hbs3")
	case "/v1/qnap/iscsi/action":
		s.ecosystemCommand(w, r, "iscsi")
	case "/v1/qnap/certificates/action":
		s.ecosystemCommand(w, r, "certificates")
	case "/v1/qnap/virtual-switch/action":
		s.ecosystemCommand(w, r, "virtual_switch")
	case "/v1/qnap/system-settings/action":
		s.ecosystemCommand(w, r, "system_settings")
	case "/v1/qnap/firmware/action":
		s.ecosystemCommand(w, r, "firmware")
	case "/v1/qnap/notifications/action":
		s.ecosystemCommand(w, r, "notifications")
	case "/v1/qnap/storage/action":
		s.ecosystemCommand(w, r, "storage_manager")
	case "/v1/qnap/certificates/inspect":
		s.certificateInspect(w, r)
	case "/v1/admin/token", "/v1/admin/token/reveal", "/v1/admin/token/generate", "/v1/admin/settings":
		s.adminRoute(w, r)
	case "/v1/files/list":
		s.fileList(w, r)
	case "/v1/files/stat":
		s.fileStat(w, r)
	case "/v1/files/read":
		s.fileRead(w, r)
	case "/v1/files/read-text":
		s.fileReadText(w, r)
	case "/v1/files/read-lines":
		s.fileReadLines(w, r)
	case "/v1/files/grep":
		s.fileGrep(w, r)
	case "/v1/files/write":
		s.fileWrite(w, r)
	case "/v1/files/append":
		s.fileAppend(w, r)
	case "/v1/files/manage":
		s.fileManage(w, r)
	case "/v1/files/tree":
		s.fileTree(w, r)
	case "/v1/files/search":
		s.fileSearch(w, r)
	case "/v1/files/tail":
		s.fileTail(w, r)
	case "/v1/files/du":
		s.fileDU(w, r)
	case "/v1/files/checksum":
		s.fileChecksum(w, r)
	case "/v1/exec", "/v1/command/run":
		s.exec(w, r, false)
	case "/v1/shell":
		s.exec(w, r, true)
	case "/v1/jobs":
		s.jobListOrStart(w, r)
	case "/v1/docker/info":
		s.dockerCall(w, r, []string{"info", "--format", "{{json .}}"}, 30)
	case "/v1/docker/containers":
		s.dockerCall(w, r, []string{"ps", "-a", "--format", "{{json .}}"}, 30)
	case "/v1/docker/images":
		s.dockerCall(w, r, []string{"images", "--format", "{{json .}}"}, 30)
	case "/v1/docker/health":
		s.dockerHealth(w, r)
	case "/v1/docker/compose-projects":
		s.dockerComposeProjects(w, r)
	case "/v1/docker/reconstruct":
		s.dockerReconstruct(w, r)
	case "/v1/docker/command":
		s.dockerCommand(w, r)
	case "/v1/qnap/qpkg":
		s.qpkgList(w, r)
	case "/v1/qnap/qpkg/manage", "/v1/qnap/qpkg/action":
		s.qpkgManage(w, r)
	default:
		if strings.HasPrefix(r.URL.Path, "/v1/approvals/") {
			s.approvalRoute(w, r)
			return
		}
		if s.storageRoute(w, r) || s.userRoute(w, r) || s.shareRoute(w, r) {
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/jobs/") {
			s.jobByID(w, r)
			return
		}
		s.fail(w, r, http.StatusNotFound, "not_found", "route not found", nil)
	}
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	s.ok(w, r, map[string]any{"version": Version, "host": s.hostname, "uptime_s": int(time.Since(s.started).Seconds()), "profile": s.Config.Profile})
}
func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	s.ok(w, r, map[string]any{"profile": s.Config.Profile, "permissions": s.Config.Permissions, "privacy": s.Config.Privacy, "confirmation": s.Config.Confirmation, "approval": s.Config.Approval, "operations": s.Operations.Catalog(), "command": s.Config.Command, "files": s.Config.Files, "jobs": s.Config.Jobs, "docker_paths": s.Config.DockerPaths})
}
func (s *Server) discovery(w http.ResponseWriter, r *http.Request) {
	s.ok(w, r, s.Discovery.Discover(r.Context()))
}
func (s *Server) qnapProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.fail(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "POST required", nil)
		return
	}
	if s.Config.Profile != "full_trust" {
		s.fail(w, r, http.StatusForbidden, "forbidden", "QNAP probe requires full_trust", nil)
		return
	}
	var req struct {
		OutputPath string `json:"output_path"`
		DryRun     bool   `json:"dry_run"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !filepath.IsAbs(req.OutputPath) || strings.ContainsRune(req.OutputPath, 0) {
		s.fail(w, r, http.StatusBadRequest, "invalid_request", "output_path must be an absolute path", nil)
		return
	}
	if s.ProbePath == "" {
		s.fail(w, r, http.StatusServiceUnavailable, "qnap_probe_unavailable", "QPKG probe script path could not be determined", nil)
		return
	}
	if !req.DryRun {
		info, err := os.Stat(s.ProbePath)
		if err != nil || info.IsDir() || info.Mode()&0111 == 0 {
			s.fail(w, r, http.StatusServiceUnavailable, "qnap_probe_unavailable", "QPKG probe script is not installed", nil)
			return
		}
	}
	result, err := s.run(r, []string{s.ProbePath, req.OutputPath}, qexec.Request{Timeout: 90 * time.Second, DryRun: req.DryRun})
	if err == nil {
		s.audit(r, "qnap.probe", "success", map[string]any{"output_path": req.OutputPath, "dry_run": req.DryRun}, result.DurationMS, "")
	}
	s.respondCommand(w, r, result, err)
}
func (s *Server) systemOverview(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"host": s.hostname, "os": runtime.GOOS, "arch": runtime.GOARCH, "started_at": s.started.UTC()}
	for name, args := range map[string][]string{"uname": {"/bin/uname", "-a"}, "uptime": {"/usr/bin/uptime"}, "filesystem": {"/bin/df", "-h"}} {
		result, err := s.run(r, args, qexec.Request{})
		out[name] = commandData(result, err)
	}
	s.ok(w, r, out)
}
func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	info, err := s.System.Info(r.Context())
	if err != nil {
		s.fail(w, r, 500, "system_info_failed", err.Error(), nil)
		return
	}
	d := s.Discovery.Discover(r.Context())
	info.QNAP = map[string]any{"platform": d.Platform, "model": d.Model, "firmware": d.Firmware, "arch": d.Arch, "cpu_count": d.CPUCount, "memory_bytes": d.MemoryBytes, "disk_count": d.DiskCount}
	s.ok(w, r, info)
}
func (s *Server) systemResources(w http.ResponseWriter, r *http.Request) {
	info, err := s.System.Info(r.Context())
	if err != nil {
		s.fail(w, r, 500, "system_resources_failed", err.Error(), nil)
		return
	}
	s.ok(w, r, map[string]any{"uptime_seconds": info.UptimeSeconds, "load_average": info.LoadAverage, "memory_bytes": info.Memory, "swap_bytes": info.Swap, "time": info.Time, "timezone": info.Timezone, "cpu_count": info.CPUCount})
}
func (s *Server) systemSockets(w http.ResponseWriter, r *http.Request) {
	items, err := s.System.Sockets()
	if err != nil {
		s.fail(w, r, 501, "socket_inventory_unavailable", err.Error(), nil)
		return
	}
	s.ok(w, r, map[string]any{"sockets": items})
}
func (s *Server) systemNTP(w http.ResponseWriter, r *http.Request) {
	info, err := s.System.Info(r.Context())
	if err != nil {
		s.fail(w, r, 500, "system_info_failed", err.Error(), nil)
		return
	}
	s.ok(w, r, info.NTP)
}
func (s *Server) systemProcesses(w http.ResponseWriter, r *http.Request) {
	items, err := s.System.Processes(r.Context())
	if err != nil {
		s.respondCommand(w, r, qexec.Result{}, err)
		return
	}
	s.ok(w, r, map[string]any{"processes": items})
}
func (s *Server) processAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PID    int    `json:"pid"`
		Signal string `json:"signal"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.System.Signal(req.PID, req.Signal); err != nil {
		s.fail(w, r, 400, "process_action_failed", err.Error(), nil)
		return
	}
	s.audit(r, "system.process.action", "success", req, 0, "")
	s.ok(w, r, map[string]any{"pid": req.PID, "signal": strings.ToUpper(req.Signal), "applied": true})
}
func (s *Server) services(w http.ResponseWriter, r *http.Request) {
	units, err := s.System.Services(r.Context())
	if err != nil {
		s.fail(w, r, 501, "service_discovery_unavailable", err.Error(), nil)
		return
	}
	s.ok(w, r, map[string]any{"services": units})
}
func (s *Server) serviceAction(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name, Action string }
	if !decode(w, r, &req) {
		return
	}
	result, err := s.System.ServiceAction(r.Context(), req.Name, req.Action)
	s.respondCommand(w, r, result, err)
}
func (s *Server) powerRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.fail(w, r, 405, "method_not_allowed", "POST required", nil)
		return
	}
	action := "reboot"
	if strings.HasSuffix(r.URL.Path, "/shutdown") {
		action = "shutdown"
	}
	r.Body = io.NopCloser(strings.NewReader(fmt.Sprintf(`{"action":%q}`, action)))
	s.power(w, r)
}
func (s *Server) thermal(w http.ResponseWriter, r *http.Request) {
	s.ok(w, r, s.thermalSnapshot(r.Context()))
}

func (s *Server) thermalSnapshot(ctx context.Context) map[string]any {
	return s.thermalSnapshotWithBudget(ctx, 8*time.Second)
}

func (s *Server) thermalSnapshotWithBudget(ctx context.Context, budget time.Duration) map[string]any {
	if budget <= 0 {
		budget = 8 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	out := map[string]any{"available": true, "partial": false, "sensors": []any{}, "qnap": map[string]any{}}
	add := func(kind, name string, value float64, unit string, raw string) {
		out["sensors"] = append(out["sensors"].([]any), map[string]any{"type": kind, "name": name, "value": value, "unit": unit, "raw": raw})
	}
	for _, path := range glob("/sys/class/hwmon/hwmon*/temp*_input") {
		if b, err := os.ReadFile(path); err == nil {
			raw := strings.TrimSpace(string(b))
			value, _ := strconv.ParseFloat(raw, 64)
			add("temperature", filepath.Base(path), value/1000, "C", raw)
		}
	}
	qnap := out["qnap"].(map[string]any)
	run := s.thermalRun
	if run == nil {
		run = func(runCtx context.Context, req qexec.Request) (qexec.Result, error) {
			return s.Exec.Run(runCtx, req)
		}
	}
	get := func(args ...string) string {
		key := strings.Join(args, ".")
		if probeCtx.Err() != nil {
			qnap[key] = map[string]any{"available": false, "reason": "thermal probe timed out"}
			return ""
		}
		result, err := run(probeCtx, qexec.Request{Argv: append([]string{"/sbin/getsysinfo"}, args...), Timeout: 8 * time.Second, MaxOutput: s.Config.Command.MaxOutputBytes})
		qnap[key] = commandData(result, err)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(result.Stdout)
	}
	for key, label := range map[string]string{"cputmp": "CPU", "systmp": "System"} {
		raw := get(key)
		if value, err := strconv.ParseFloat(raw, 64); err == nil {
			add("temperature", label, value, "C", raw)
		}
	}
	fanCount, _ := strconv.Atoi(get("sysfannum"))
	for i := 1; i <= fanCount && i <= 32; i++ {
		raw := get("sysfan", strconv.Itoa(i))
		if value, err := strconv.ParseFloat(raw, 64); err == nil {
			add("fan", "Fan "+strconv.Itoa(i), value, "RPM", raw)
		}
	}
	diskCount, _ := strconv.Atoi(get("hdnum"))
	for i := 1; i <= diskCount && i <= 64; i++ {
		raw := get("hdtmp", strconv.Itoa(i))
		if value, err := strconv.ParseFloat(raw, 64); err == nil {
			add("temperature", "Disk "+strconv.Itoa(i), value, "C", raw)
		}
	}
	if err := probeCtx.Err(); err != nil {
		out["partial"] = true
		if errors.Is(err, context.DeadlineExceeded) {
			out["reason"] = "thermal probe timed out"
		} else {
			out["reason"] = "thermal probe cancelled"
		}
		out["available"] = len(out["sensors"].([]any)) > 0
	}
	return out
}
func (s *Server) power(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.ok(w, r, map[string]any{"actions": []string{"reboot", "shutdown"}})
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Action != "reboot" && req.Action != "shutdown" {
		s.fail(w, r, 400, "invalid_action", "action must be reboot or shutdown", nil)
		return
	}
	if s.Config.Profile != "full_trust" {
		s.fail(w, r, 403, "forbidden", "power actions require full_trust", nil)
		return
	}
	args := []string{"/sbin/reboot"}
	if req.Action == "shutdown" {
		args = []string{"/sbin/poweroff"}
	}
	result, err := s.run(r, args, qexec.Request{})
	s.respondCommand(w, r, result, err)
}
func (s *Server) network(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	for name, args := range map[string][]string{"interfaces": {"/sbin/ifconfig", "-a"}, "routes": {"/bin/netstat", "-rn"}} {
		result, err := s.run(r, args, qexec.Request{})
		out[name] = commandData(result, err)
	}
	if b, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		out["dns"] = string(b)
	}
	s.ok(w, r, out)
}
func (s *Server) networkInterfaces(w http.ResponseWriter, r *http.Request) {
	items, err := s.Network.Interfaces()
	if err != nil {
		s.fail(w, r, 500, "network_interfaces_failed", err.Error(), nil)
		return
	}
	s.ok(w, r, map[string]any{"interfaces": items})
}
func (s *Server) networkRoutes(w http.ResponseWriter, r *http.Request) {
	items, err := s.Network.Routes()
	if err != nil {
		s.fail(w, r, 500, "network_routes_failed", err.Error(), nil)
		return
	}
	s.ok(w, r, map[string]any{"routes": items})
}
func (s *Server) networkDNS(w http.ResponseWriter, r *http.Request) { s.ok(w, r, s.Network.DNS()) }
func (s *Server) networkManage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action    string `json:"action"`
		Interface string `json:"interface"`
		Value     string `json:"value"`
		Gateway   string `json:"gateway"`
		Metric    int    `json:"metric"`
		DryRun    bool   `json:"dry_run"`
	}
	if !decode(w, r, &req) {
		return
	}
	args, err := qnetwork.CommandArgs(req.Action, req.Interface, req.Value, req.Gateway, req.Metric)
	if err != nil {
		s.fail(w, r, 400, "invalid_network_action", err.Error(), nil)
		return
	}
	previous, _ := s.Network.Interfaces()
	previousRoutes, _ := s.Network.Routes()
	if req.DryRun {
		s.ok(w, r, map[string]any{"argv": append([]string{"ip"}, args...), "previous": map[string]any{"interfaces": previous, "routes": previousRoutes}, "dry_run": true, "persistence": "transient_linux_ip"})
		return
	}
	result, err := s.Network.RunIP(r.Context(), args)
	if err != nil {
		s.respondCommand(w, r, result, err)
		return
	}
	current, _ := s.Network.Interfaces()
	currentRoutes, _ := s.Network.Routes()
	s.audit(r, "network.manage", "success", req, result.DurationMS, "")
	s.ok(w, r, map[string]any{"command": result, "previous": map[string]any{"interfaces": previous, "routes": previousRoutes}, "new": map[string]any{"interfaces": current, "routes": currentRoutes}, "applied": true, "persistence": "transient_linux_ip"})
}
func (s *Server) virtualSwitches(w http.ResponseWriter, r *http.Request) {
	items, err := s.Network.Interfaces()
	if err != nil {
		s.fail(w, r, 500, "network_interfaces_failed", err.Error(), nil)
		return
	}
	switches := []qnetwork.Interface{}
	for _, item := range items {
		if item.Virtual {
			switches = append(switches, item)
		}
	}
	s.ok(w, r, map[string]any{"virtual_switches": switches, "source": "linux bridge/bond/vlan discovery; QNAP private adapter requires probe"})
}
func (s *Server) storage(w http.ResponseWriter, r *http.Request) {
	disks, disksErr := s.Storage.Disks()
	raid, raidErr := s.Storage.RAID()
	pools, poolsErr := s.Storage.Pools(r.Context())
	volumes, volumesErr := s.Storage.Volumes()
	snapshots, snapshotsErr := s.Storage.Snapshots(r.Context())
	qts, qtsErr := s.Storage.QTSInventory(r.Context())
	s.ok(w, r, map[string]any{"qts": qts, "qts_error": errorText(qtsErr), "disks": disks, "disks_error": errorText(disksErr), "raid_groups": raid, "raid_error": errorText(raidErr), "pools": pools, "pools_error": errorText(poolsErr), "volumes": volumes, "volumes_error": errorText(volumesErr), "snapshots": snapshots, "snapshots_error": errorText(snapshotsErr), "qts_snapshot_backend": s.Storage.QTSSnapshotCapabilities()})
}
func (s *Server) storageRoute(w http.ResponseWriter, r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/v1/storage/")
	if path == r.URL.Path {
		return false
	}
	if path == "disks" {
		items, err := s.Storage.Disks()
		if err != nil {
			s.fail(w, r, 500, "disks_inventory_failed", err.Error(), nil)
		} else {
			s.ok(w, r, map[string]any{"disks": items})
		}
		return true
	}
	if path == "disk-io" {
		items, err := s.Storage.DiskIO()
		if err != nil {
			s.fail(w, r, http.StatusServiceUnavailable, "disk_io_unavailable", err.Error(), nil)
		} else {
			s.ok(w, r, map[string]any{"disk_io": items})
		}
		return true
	}
	if path == "raid-groups" {
		items, err := s.Storage.RAID()
		if err != nil {
			s.fail(w, r, 500, "raid_inventory_failed", err.Error(), nil)
		} else {
			s.ok(w, r, map[string]any{"raid_groups": items})
		}
		return true
	}
	if strings.HasPrefix(path, "raid-groups/") {
		parts := strings.Split(path, "/")
		if len(parts) == 3 && parts[2] == "action" {
			s.raidAction(w, r, parts[1])
			return true
		}
	}
	if path == "pools" {
		items, err := s.Storage.Pools(r.Context())
		if err != nil {
			s.fail(w, r, 500, "pools_inventory_failed", err.Error(), nil)
		} else {
			s.ok(w, r, map[string]any{"pools": items})
		}
		return true
	}
	if path == "volumes" {
		items, err := s.Storage.Volumes()
		if err != nil {
			s.fail(w, r, 500, "volumes_inventory_failed", err.Error(), nil)
		} else {
			s.ok(w, r, map[string]any{"volumes": items})
		}
		return true
	}
	if path == "snapshots" {
		items, err := s.Storage.Snapshots(r.Context())
		if err != nil {
			s.fail(w, r, 500, "snapshots_inventory_failed", err.Error(), nil)
		} else {
			s.ok(w, r, map[string]any{"snapshots": items, "qts_backend": s.Storage.QTSSnapshotCapabilities()})
		}
		return true
	}
	if path == "snapshots/capabilities" {
		s.ok(w, r, s.Storage.QTSSnapshotCapabilities())
		return true
	}
	if strings.HasPrefix(path, "disks/") {
		parts := strings.Split(path, "/")
		id := parts[1]
		if len(parts) == 2 {
			item, err := s.Storage.Disk(id)
			if err != nil {
				s.fail(w, r, 404, "disk_not_found", err.Error(), nil)
			} else {
				s.ok(w, r, item)
			}
			return true
		}
		if len(parts) == 3 && parts[2] == "smart" {
			item, err := s.Storage.Smart(r.Context(), id)
			if err != nil {
				s.respondCommand(w, r, qexec.Result{}, err)
			} else {
				s.ok(w, r, item)
			}
			return true
		}
		if len(parts) == 3 && parts[2] == "smart-test" {
			var req struct {
				Kind string `json:"kind"`
			}
			if !decode(w, r, &req) {
				return true
			}
			job, _ := s.startJob(r, "smart-test", "storage:disk:"+id, func(ctx context.Context, log func(string)) (any, error) {
				result, err := s.Storage.StartSmart(ctx, id, req.Kind)
				log(result.Stdout)
				log(result.Stderr)
				return result, err
			})
			s.ok(w, r, job)
			return true
		}
	}
	if path == "snapshots/action" {
		var req struct {
			Action string `json:"action"`
			Name   string `json:"name"`
			Target string `json:"target"`
			Volume string `json:"volume"`
		}
		if !decode(w, r, &req) {
			return true
		}
		job, _ := s.startJob(r, "snapshot-"+req.Action, "storage:snapshot", func(ctx context.Context, log func(string)) (any, error) {
			if req.Volume != "" {
				if req.Action != "create" {
					return nil, errors.New("QTS snapshot adapter currently supports create only")
				}
				return s.Storage.CreateQTSSnapshot(ctx, req.Volume, req.Name)
			}
			result, err := s.Storage.SnapshotAction(ctx, req.Action, req.Name, req.Target)
			log(result.Stdout)
			log(result.Stderr)
			return result, err
		})
		s.ok(w, r, job)
		return true
	}
	return false
}

func (s *Server) raidAction(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		s.fail(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "POST required", nil)
		return
	}
	if s.Config.Profile != "full_trust" {
		s.fail(w, r, http.StatusForbidden, "forbidden", "RAID scrub actions require full_trust", nil)
		return
	}
	var req struct {
		Action string `json:"action"`
		DryRun bool   `json:"dry_run"`
	}
	if !decode(w, r, &req) {
		return
	}
	result, err := s.Storage.RAIDAction(name, req.Action, req.DryRun)
	if err != nil {
		s.fail(w, r, http.StatusUnprocessableEntity, "raid_action_failed", err.Error(), nil)
		return
	}
	if !req.DryRun && !result.Applied {
		s.fail(w, r, http.StatusConflict, "raid_action_not_applied", "mdraid did not report the requested sync action", result)
		return
	}
	s.audit(r, "storage.raid."+req.Action, "success", result, 0, "")
	s.ok(w, r, result)
}
func (s *Server) userRoute(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/v1/users":
		items, err := s.Users.List()
		if err != nil {
			s.fail(w, r, 500, "users_inventory_failed", err.Error(), nil)
		} else {
			s.ok(w, r, map[string]any{"users": items})
		}
		return true
	case "/v1/groups":
		items, err := s.Users.Groups()
		if err != nil {
			s.fail(w, r, 500, "groups_inventory_failed", err.Error(), nil)
		} else {
			s.ok(w, r, map[string]any{"groups": items})
		}
		return true
	case "/v1/users/manage":
		var req struct {
			Action string   `json:"action"`
			Name   string   `json:"name"`
			Args   []string `json:"args"`
		}
		if !decode(w, r, &req) {
			return true
		}
		result, err := s.Users.ManageUser(r.Context(), req.Action, req.Name, req.Args)
		s.respondCommand(w, r, result, err)
		return true
	case "/v1/groups/manage":
		var req struct {
			Action string   `json:"action"`
			Name   string   `json:"name"`
			Args   []string `json:"args"`
		}
		if !decode(w, r, &req) {
			return true
		}
		result, err := s.Users.ManageGroup(r.Context(), req.Action, req.Name, req.Args)
		s.respondCommand(w, r, result, err)
		return true
	}
	return false
}
func (s *Server) shareRoute(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/v1/shares":
		items, err := s.Shares.List()
		if err != nil {
			s.fail(w, r, 501, "shares_inventory_unavailable", err.Error(), nil)
		} else {
			s.ok(w, r, map[string]any{"shares": items})
		}
		return true
	case "/v1/shares/nfs":
		items, err := s.Shares.NFS()
		if err != nil {
			s.fail(w, r, 501, "nfs_inventory_unavailable", err.Error(), nil)
		} else {
			s.ok(w, r, map[string]any{"exports": items})
		}
		return true
	case "/v1/shares/smb-status":
		report, err := s.Shares.SMBStatus(r.Context())
		if err != nil {
			s.fail(w, r, http.StatusServiceUnavailable, "smb_status_unavailable", err.Error(), nil)
		} else {
			s.ok(w, r, report)
		}
		return true
	case "/v1/shares/manage":
		s.ecosystemCommand(w, r, "shares")
		return true
	case "/v1/acl":
		var req struct {
			Path string `json:"path"`
		}
		if !decode(w, r, &req) {
			return true
		}
		result, err := s.Shares.ACL(r.Context(), req.Path)
		if err != nil {
			s.respondCommand(w, r, qexec.Result{}, err)
			return true
		}
		s.ok(w, r, result)
		return true
	case "/v1/acl/set":
		var req struct {
			Path  string `json:"path"`
			Entry string `json:"entry"`
		}
		if !decode(w, r, &req) {
			return true
		}
		result, err := s.Shares.SetACL(r.Context(), req.Path, req.Entry)
		s.respondCommand(w, r, result, err)
		return true
	}
	return false
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func (s *Server) command(w http.ResponseWriter, r *http.Request, args []string) {
	result, err := s.run(r, args, qexec.Request{})
	s.respondCommand(w, r, result, err)
}

type execRequest struct {
	Argv        []string          `json:"argv"`
	Shell       string            `json:"shell"`
	Script      string            `json:"script"`
	CWD         string            `json:"cwd"`
	Env         map[string]string `json:"env"`
	StdinBase64 string            `json:"stdin_base64"`
	Stdin       string            `json:"stdin"`
	TimeoutSec  int               `json:"timeout_sec"`
	MaxOutput   int               `json:"max_output_bytes"`
	DryRun      bool              `json:"dry_run"`
}

func (s *Server) exec(w http.ResponseWriter, r *http.Request, shell bool) {
	if r.Method != http.MethodPost {
		s.fail(w, r, 405, "method_not_allowed", "POST required", nil)
		return
	}
	var req execRequest
	if !decode(w, r, &req) {
		return
	}
	if shell {
		if !s.Config.Permissions.AllowShell {
			s.fail(w, r, 403, "shell_disabled", "shell execution is disabled", nil)
			return
		}
		if req.Script != "" {
			path := req.Shell
			if path == "" {
				path = detectShell()
			}
			if !validShell(path) {
				s.fail(w, r, 503, "shell_unavailable", "no supported shell was found", nil)
				return
			}
			req.Argv = []string{path, "-c", req.Script}
		} else if req.Shell == "" {
			s.fail(w, r, 400, "invalid_request", "script is required", nil)
			return
		} else {
			// Legacy request shape: shell is the script and /bin/sh is selected.
			path := detectShell()
			if path == "" {
				s.fail(w, r, 503, "shell_unavailable", "no supported shell was found", nil)
				return
			}
			req.Argv = []string{path, "-c", req.Shell}
		}
	}
	stdin := []byte(req.Stdin)
	if req.StdinBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(req.StdinBase64)
		if err != nil {
			s.fail(w, r, 400, "invalid_request", "stdin_base64 is invalid", nil)
			return
		}
		stdin = decoded
	}
	result, err := s.run(r, req.Argv, qexec.Request{CWD: req.CWD, Env: req.Env, Stdin: stdin, Timeout: time.Duration(req.TimeoutSec) * time.Second, MaxOutput: req.MaxOutput, DryRun: req.DryRun})
	s.respondCommand(w, r, result, err)
}
func detectShell() string {
	for _, path := range []string{"/bin/sh", "/bin/ash", "/bin/bash"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return ""
}
func validShell(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}
func (s *Server) run(r *http.Request, argv []string, req qexec.Request) (qexec.Result, error) {
	if len(argv) == 0 {
		return qexec.Result{}, errors.New("argv is required")
	}
	if !s.Config.Permissions.AllowAnyCommand && !allowed(argv[0], s.Config.Permissions.AllowedCommands) {
		return qexec.Result{}, &qexec.CommandError{Kind: qexec.StartFailed, Err: errors.New("command is not permitted by profile")}
	}
	req.Argv = argv
	if req.Timeout <= 0 {
		req.Timeout = s.Config.Timeout()
	}
	if req.MaxOutput <= 0 || req.MaxOutput > s.Config.Command.MaxOutputBytes {
		req.MaxOutput = s.Config.Command.MaxOutputBytes
	}
	return s.Exec.Run(r.Context(), req)
}
func allowed(binary string, list []string) bool {
	for _, allowed := range list {
		if binary == allowed {
			return true
		}
	}
	return false
}
func (s *Server) fileList(w http.ResponseWriter, r *http.Request) {
	entries, err := s.Files.List(r.URL.Query().Get("path"))
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	s.ok(w, r, map[string]any{"entries": entries})
}
func (s *Server) fileStat(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	info, err := s.Files.Stat(path)
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	s.ok(w, r, map[string]any{"path": path, "mode": info.Mode().String(), "size": info.Size(), "modified": info.ModTime(), "is_dir": info.IsDir()})
}
func (s *Server) fileRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string `json:"path"`
		Offset   int64  `json:"offset"`
		MaxBytes int64  `json:"max_bytes"`
	}
	if !decode(w, r, &req) {
		return
	}
	result, err := s.Files.Read(req.Path, req.Offset, req.MaxBytes)
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	s.ok(w, r, result)
}
func (s *Server) fileWrite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path           string `json:"path"`
		ContentBase64  string `json:"content_base64"`
		Mode           string `json:"mode"`
		CreateParents  bool   `json:"create_parents"`
		ExpectedSHA256 string `json:"expected_sha256"`
		Backup         bool   `json:"backup"`
		BackupPath     string `json:"backup_path"`
		DryRun         bool   `json:"dry_run"`
	}
	if !decode(w, r, &req) {
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		s.fail(w, r, 400, "invalid_request", "content_base64 is invalid", nil)
		return
	}
	mode := os.FileMode(0644)
	if req.Mode != "" {
		value, err := strconv.ParseUint(req.Mode, 8, 32)
		if err != nil {
			s.fail(w, r, 400, "invalid_request", "mode must be octal", nil)
			return
		}
		mode = os.FileMode(value)
	}
	if req.DryRun {
		s.ok(w, r, map[string]any{"path": req.Path, "bytes": len(data), "dry_run": true})
		return
	}
	result, err := s.Files.WriteAtomic(req.Path, data, files.WriteOptions{Mode: mode, CreateParents: req.CreateParents, ExpectedSHA256: req.ExpectedSHA256, Backup: req.Backup, BackupPath: req.BackupPath})
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	s.audit(r, "file.write", "success", map[string]any{"path": result.Path, "bytes": len(data)}, 0, "")
	s.ok(w, r, result)
}
func (s *Server) fileAppend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path          string `json:"path"`
		ContentBase64 string `json:"content_base64"`
		Mode          string `json:"mode"`
		CreateParents bool   `json:"create_parents"`
	}
	if !decode(w, r, &req) {
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		s.fail(w, r, 400, "invalid_request", "content_base64 is invalid", nil)
		return
	}
	mode := os.FileMode(0644)
	if req.Mode != "" {
		v, err := strconv.ParseUint(req.Mode, 8, 32)
		if err != nil {
			s.fail(w, r, 400, "invalid_request", "mode must be octal", nil)
			return
		}
		mode = os.FileMode(v)
	}
	path, err := s.Files.Append(req.Path, data, mode, req.CreateParents)
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	s.ok(w, r, map[string]any{"path": path, "bytes": len(data), "appended": true})
}
func (s *Server) fileManage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action    string `json:"action"`
		Path      string `json:"path"`
		Target    string `json:"target"`
		Mode      string `json:"mode"`
		Recursive bool   `json:"recursive"`
	}
	if !decode(w, r, &req) {
		return
	}
	mode := os.FileMode(0)
	if req.Mode != "" {
		value, err := strconv.ParseUint(req.Mode, 8, 32)
		if err != nil {
			s.fail(w, r, 400, "invalid_request", "mode must be octal", nil)
			return
		}
		mode = os.FileMode(value)
	}
	if err := s.Files.Manage(req.Action, req.Path, req.Target, mode, req.Recursive); err != nil {
		s.fileErr(w, r, err)
		return
	}
	s.ok(w, r, map[string]any{"action": req.Action, "path": req.Path, "target": req.Target})
}
func (s *Server) fileChecksum(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &req) {
		return
	}
	sum, err := s.Files.Checksum(req.Path)
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	s.ok(w, r, map[string]any{"path": req.Path, "checksum": sum})
}
func (s *Server) fileSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path  string `json:"path"`
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if !decode(w, r, &req) {
		return
	}
	items, err := s.Files.Search(req.Path, req.Query, req.Limit)
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	s.ok(w, r, map[string]any{"results": items})
}
func (s *Server) fileTail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string `json:"path"`
		Lines    int    `json:"lines"`
		MaxBytes int64  `json:"max_bytes"`
	}
	if !decode(w, r, &req) {
		return
	}
	lines, err := s.Files.Tail(req.Path, req.Lines, req.MaxBytes)
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	s.ok(w, r, map[string]any{"path": req.Path, "lines": lines})
}
func (s *Server) fileDU(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &req) {
		return
	}
	bytes, err := s.Files.DU(req.Path)
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	s.ok(w, r, map[string]any{"path": req.Path, "bytes": bytes})
}
func (s *Server) jobListOrStart(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.ok(w, r, map[string]any{"jobs": s.Jobs.List()})
		return
	}
	if r.Method != http.MethodPost {
		s.fail(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST required", nil)
		return
	}
	var req struct {
		Kind    string      `json:"kind"`
		Command execRequest `json:"command"`
		Shell   string      `json:"shell"`
		Script  string      `json:"script"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Kind == "" {
		req.Kind = "exec"
	}
	script := req.Script
	shellPath := req.Shell
	if script == "" && shellPath != "" {
		// Keep the existing shell-as-script body compatible with earlier API
		// clients while accepting the explicit shell + script form.
		script, shellPath = shellPath, ""
	}
	if script != "" {
		if !s.Config.Permissions.AllowShell {
			s.fail(w, r, 403, "shell_disabled", "shell execution is disabled", nil)
			return
		}
		if shellPath == "" {
			shellPath = detectShell()
		}
		if !validShell(shellPath) {
			s.fail(w, r, http.StatusServiceUnavailable, "shell_unavailable", "no executable shell was found", nil)
			return
		}
		req.Command.Argv = []string{shellPath, "-c", script}
	}
	if len(req.Command.Argv) == 0 {
		s.fail(w, r, 400, "invalid_request", "command.argv or shell is required", nil)
		return
	}
	stdin := []byte(req.Command.Stdin)
	var err error
	if req.Command.StdinBase64 != "" {
		stdin, err = decodeBase64(req.Command.StdinBase64)
	} else {
		err = nil
	}
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, "invalid_stdin_base64", err.Error(), nil)
		return
	}
	resource := "command"
	if len(req.Command.Argv) > 0 {
		resource += ":" + req.Command.Argv[0]
	}
	job, _ := s.startJob(r, req.Kind, resource, func(ctx context.Context, log func(string)) (any, error) {
		request := (&http.Request{}).WithContext(ctx)
		result, err := s.run(request, req.Command.Argv, qexec.Request{CWD: req.Command.CWD, Env: req.Command.Env, Stdin: stdin, Timeout: time.Duration(req.Command.TimeoutSec) * time.Second, MaxOutput: req.Command.MaxOutput, DryRun: req.Command.DryRun})
		if result.Stdout != "" {
			log(result.Stdout)
		}
		if result.Stderr != "" {
			log(result.Stderr)
		}
		return result, err
	})
	s.audit(r, "jobs.start", "queued", map[string]any{"kind": req.Kind, "argv": req.Command.Argv, "cwd": req.Command.CWD, "dry_run": req.Command.DryRun}, 0, "")
	s.ok(w, r, job)
}
func (s *Server) jobByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/jobs/")
	if strings.HasSuffix(id, "/logs") {
		id = strings.TrimSuffix(id, "/logs")
		if r.Method != http.MethodGet {
			s.fail(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "GET required", nil)
			return
		}
		cursor, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		lines, next, truncated, ok := s.Jobs.Logs(id, cursor, limit)
		if !ok {
			s.fail(w, r, 404, "not_found", "job not found", nil)
			return
		}
		s.ok(w, r, map[string]any{"id": id, "lines": lines, "next_cursor": next, "logs_truncated": truncated})
		return
	}
	if strings.HasSuffix(id, "/cancel") {
		id = strings.TrimSuffix(id, "/cancel")
		if r.Method != http.MethodPost {
			s.fail(w, r, 405, "method_not_allowed", "POST required", nil)
			return
		}
		if !s.Jobs.Cancel(id) {
			s.fail(w, r, 404, "not_found", "job not found", nil)
			return
		}
		s.ok(w, r, map[string]any{"id": id, "cancelled": true})
		return
	}
	job, ok := s.Jobs.Get(id)
	if !ok {
		s.fail(w, r, 404, "not_found", "job not found", nil)
		return
	}
	s.ok(w, r, job)
}
func (s *Server) dockerCall(w http.ResponseWriter, r *http.Request, args []string, timeout int) {
	result, err := s.Docker.Run(r.Context(), args, timeout)
	s.respondCommand(w, r, result, err)
}
func (s *Server) dockerCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Subcommand  string            `json:"subcommand"`
		Args        []string          `json:"args"`
		TimeoutSec  int               `json:"timeout_sec"`
		CWD         string            `json:"cwd"`
		Env         map[string]string `json:"env"`
		StdinBase64 string            `json:"stdin_base64"`
		Async       bool              `json:"async"`
		DryRun      bool              `json:"dry_run"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !docker.Allowed(req.Subcommand) {
		s.fail(w, r, 400, "invalid_docker_subcommand", "unsupported docker subcommand", nil)
		return
	}
	stdin, err := decodeBase64(req.StdinBase64)
	if err != nil {
		s.fail(w, r, 400, "invalid_stdin_base64", err.Error(), nil)
		return
	}
	if req.DryRun {
		s.ok(w, r, map[string]any{"argv": append([]string{"docker", req.Subcommand}, req.Args...), "cwd": req.CWD, "env": req.Env, "stdin_bytes": len(stdin), "dry_run": true})
		return
	}
	args := append([]string{req.Subcommand}, req.Args...)
	if req.Async {
		job, _ := s.startJob(r, "docker."+req.Subcommand, "docker", func(ctx context.Context, log func(string)) (any, error) {
			result, err := s.Docker.RunWith(ctx, args, req.TimeoutSec, req.CWD, req.Env, stdin)
			if result.Stdout != "" {
				log(result.Stdout)
			}
			if result.Stderr != "" {
				log(result.Stderr)
			}
			return result, err
		})
		s.audit(r, "docker.command.async", "queued", req, 0, "")
		s.ok(w, r, job)
		return
	}
	result, err := s.Docker.RunWith(r.Context(), args, req.TimeoutSec, req.CWD, req.Env, stdin)
	s.respondCommand(w, r, result, err)
}
func (s *Server) qpkgList(w http.ResponseWriter, r *http.Request) {
	inventory, err := s.QPKG.Inventory(r.Context())
	if err != nil {
		s.fail(w, r, 500, "qpkg_inventory_failed", err.Error(), nil)
		return
	}
	s.ok(w, r, inventory)
}
func (s *Server) qpkgManage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Action string `json:"action"`
		Path   string `json:"path"`
		URL    string `json:"url"`
		Async  bool   `json:"async"`
		DryRun bool   `json:"dry_run"`
	}
	if !decode(w, r, &req) {
		return
	}
	if _, err := qpkg.CommandArgs(req.Name, req.Action, req.Path, req.URL); err != nil && req.Action != "restart" {
		s.fail(w, r, 400, "invalid_qpkg_action", err.Error(), nil)
		return
	}
	if req.Action == "restart" {
		if _, err := qpkg.CommandArgs(req.Name, "start", req.Path, req.URL); err != nil {
			s.fail(w, r, 400, "invalid_qpkg_action", err.Error(), nil)
			return
		}
	}
	if req.DryRun {
		if req.Action == "restart" {
			s.ok(w, r, map[string]any{"argv": [][]string{{"qpkg_cli", "--stop", req.Name}, {"qpkg_cli", "--start", req.Name}}, "dry_run": true})
			return
		}
		args, _ := qpkg.CommandArgs(req.Name, req.Action, req.Path, req.URL)
		s.ok(w, r, map[string]any{"argv": append([]string{"qpkg_cli"}, args...), "dry_run": true})
		return
	}
	if req.Async {
		job, _ := s.startJob(r, "qpkg."+req.Action, "qpkg:"+req.Name, func(ctx context.Context, log func(string)) (any, error) {
			result, err := s.QPKG.Manage(ctx, req.Name, req.Action, req.Path, req.URL)
			if result.Stdout != "" {
				log(result.Stdout)
			}
			if result.Stderr != "" {
				log(result.Stderr)
			}
			if err != nil {
				return result, err
			}
			inventory, inventoryErr := s.QPKG.Inventory(ctx)
			if inventoryErr != nil {
				return map[string]any{"command": result}, inventoryErr
			}
			return map[string]any{"command": result, "packages_after": inventory}, nil
		})
		s.audit(r, "qpkg.manage.async", "queued", req, 0, "")
		s.ok(w, r, job)
		return
	}
	result, err := s.QPKG.Manage(r.Context(), req.Name, req.Action, req.Path, req.URL)
	if err == nil {
		packages, listErr := s.QPKG.Inventory(r.Context())
		if listErr == nil {
			s.ok(w, r, map[string]any{"command": result, "packages_after": packages})
			return
		}
	}
	s.respondCommand(w, r, result, err)
}
func (s *Server) auditTail(w http.ResponseWriter, r *http.Request) {
	page, err := s.logPage(r, "audit", 200, 0, "", "", "")
	if err != nil {
		s.fail(w, r, 500, "audit_read_failed", err.Error(), nil)
		return
	}
	s.ok(w, r, page)
}
func (s *Server) logSources(w http.ResponseWriter, r *http.Request) {
	s.ok(w, r, map[string]any{"sources": s.Logs.Sources()})
}
func (s *Server) logTail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Limit  int    `json:"limit"`
		Cursor int    `json:"cursor"`
		Query  string `json:"query"`
		Since  string `json:"since"`
		Until  string `json:"until"`
	}
	if r.Method == http.MethodGet {
		req.Name = r.URL.Query().Get("name")
		req.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
		req.Cursor, _ = strconv.Atoi(r.URL.Query().Get("cursor"))
		req.Query = r.URL.Query().Get("query")
		req.Since = r.URL.Query().Get("since")
		req.Until = r.URL.Query().Get("until")
	} else if !decode(w, r, &req) {
		return
	}
	page, err := s.logPage(r, req.Name, req.Limit, req.Cursor, req.Query, req.Since, req.Until)
	if err != nil {
		s.fail(w, r, 400, "log_tail_failed", err.Error(), nil)
		return
	}
	s.ok(w, r, map[string]any{"name": req.Name, "query": req.Query, "since": req.Since, "until": req.Until, "lines": page.Lines, "next_cursor": page.NextCursor, "total": page.Total, "time_filtered": page.TimeFiltered, "unparseable_lines": page.UnparseableLines})
}

func (s *Server) logPage(_ *http.Request, name string, limit, cursor int, query, sinceText, untilText string) (logs.Page, error) {
	since, err := parseLogTime(sinceText)
	if err != nil {
		return logs.Page{}, err
	}
	until, err := parseLogTime(untilText)
	if err != nil {
		return logs.Page{}, err
	}
	return s.Logs.Page(name, limit, cursor, query, since, until)
}

func parseLogTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errors.New("since and until must be RFC3339 timestamps")
	}
	return parsed.UTC(), nil
}
func (s *Server) ecosystem(w http.ResponseWriter, r *http.Request) {
	s.ok(w, r, map[string]any{"adapters": s.Ecosystem.Inventory(r.Context())})
}
func (s *Server) ups(w http.ResponseWriter, r *http.Request) {
	item, err := s.Ecosystem.UPS(r.Context())
	if err != nil {
		s.fail(w, r, 503, "ups_unavailable", err.Error(), nil)
		return
	}
	s.ok(w, r, item)
}
func (s *Server) ecosystemCommand(w http.ResponseWriter, r *http.Request, adapter string) {
	if r.Method != http.MethodPost {
		s.fail(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "POST required", nil)
		return
	}
	var req struct {
		Action     string   `json:"action"`
		ID         string   `json:"id"`
		Name       string   `json:"name"`
		Target     string   `json:"target"`
		Args       []string `json:"args"`
		TimeoutSec int      `json:"timeout_sec"`
		Async      bool     `json:"async"`
		DryRun     bool     `json:"dry_run"`
	}
	if !decode(w, r, &req) {
		return
	}
	argv, configuredTimeout, err := s.Ecosystem.Command(adapter, req.Action, map[string]string{"id": req.ID, "name": req.Name, "target": req.Target}, req.Args)
	if err != nil {
		s.fail(w, r, http.StatusNotImplemented, "adapter_unavailable", err.Error(), nil)
		return
	}
	if !s.Config.Permissions.AllowAnyCommand && !allowed(argv[0], s.Config.Permissions.AllowedCommands) {
		s.fail(w, r, http.StatusForbidden, "forbidden", "configured adapter executable is not permitted by active profile", map[string]any{"adapter": adapter, "action": req.Action})
		return
	}
	timeout := configuredTimeout
	if req.TimeoutSec > 0 {
		timeout = time.Duration(req.TimeoutSec) * time.Second
	}
	if timeout <= 0 {
		timeout = s.Config.Timeout()
	}
	if req.DryRun {
		s.ok(w, r, map[string]any{"adapter": adapter, "action": req.Action, "argv": argv, "timeout_seconds": int(timeout.Seconds()), "dry_run": true})
		return
	}
	command := qexec.Request{Argv: argv, Timeout: timeout, MaxOutput: s.Config.Command.MaxOutputBytes}
	if req.Async {
		job, _ := s.startJob(r, "qnap."+adapter+"."+req.Action, "qnap:"+adapter, func(ctx context.Context, log func(string)) (any, error) {
			result, err := s.Exec.Run(ctx, command)
			if result.Stdout != "" {
				log(result.Stdout)
			}
			if result.Stderr != "" {
				log(result.Stderr)
			}
			return result, err
		})
		s.audit(r, "qnap."+adapter+"."+req.Action, "queued", map[string]any{"argv": argv}, 0, "")
		s.ok(w, r, job)
		return
	}
	result, err := s.run(r, argv, command)
	s.respondCommand(w, r, result, err)
}
func (s *Server) certificateInspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.fail(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "POST required", nil)
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &req) {
		return
	}
	if _, err := s.Files.Stat(req.Path); err != nil {
		s.fileErr(w, r, err)
		return
	}
	certificate, command, err := s.Ecosystem.Certificate(r.Context(), req.Path)
	if err != nil {
		s.respondCommand(w, r, command, err)
		return
	}
	s.ok(w, r, map[string]any{"certificate": certificate, "command": command})
}
func (s *Server) respondCommand(w http.ResponseWriter, r *http.Request, result qexec.Result, err error) {
	if err == nil {
		s.audit(r, "command", "success", result.Argv, result.DurationMS, "")
		s.ok(w, r, result)
		return
	}
	var commandErr *qexec.CommandError
	if errors.As(err, &commandErr) {
		status := http.StatusBadGateway
		code := string(commandErr.Kind)
		if commandErr.Kind == qexec.NonZeroExit {
			status = http.StatusUnprocessableEntity
		}
		if commandErr.Kind == qexec.TimedOut {
			status = http.StatusGatewayTimeout
		}
		if commandErr.Kind == qexec.Cancelled {
			status = http.StatusRequestTimeout
		}
		if commandErr.Kind == qexec.StartFailed {
			status = http.StatusForbidden
		}
		if commandErr.Kind == qexec.NotFound {
			status = http.StatusNotFound
		}
		s.audit(r, "command", "failed", result.Argv, result.DurationMS, err.Error())
		s.fail(w, r, status, code, err.Error(), result)
		return
	}
	s.fail(w, r, 500, "internal_error", err.Error(), nil)
}
func commandData(result qexec.Result, err error) any {
	if err == nil {
		return result
	}
	return map[string]any{"result": result, "error": err.Error()}
}
func (s *Server) fileErr(w http.ResponseWriter, r *http.Request, err error) {
	status := 500
	code := "file_error"
	if errors.Is(err, os.ErrNotExist) {
		status = 404
		code = "not_found"
	}
	if strings.Contains(err.Error(), "outside allowed roots") {
		status = 403
		code = "path_not_allowed"
	}
	s.fail(w, r, status, code, err.Error(), nil)
}
func (s *Server) ok(w http.ResponseWriter, r *http.Request, data any) {
	s.write(w, http.StatusOK, envelope{OK: true, Data: data, Meta: s.meta(r)})
}
func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	s.write(w, status, envelope{OK: false, Error: &problem{Code: code, Message: message, Details: details}, Meta: s.meta(r)})
}
func (s *Server) write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Server) meta(r *http.Request) meta {
	ctx := rc(r)
	return meta{RequestID: ctx.id, DurationMS: time.Since(ctx.started).Milliseconds()}
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
		return true
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16*1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(w, fmt.Sprintf(`{"ok":false,"error":{"code":"invalid_json","message":%q}}`, err.Error()), http.StatusBadRequest)
		return false
	}
	return true
}
func decodeBase64(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("stdin_base64 must be valid base64: %w", err)
	}
	return decoded, nil
}
func randomID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}
func glob(pattern string) []string { matches, _ := filepath.Glob(pattern); return matches }
