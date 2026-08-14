package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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
	"syscall"
	"time"

	"qnap-ai-control-suite/agent/internal/audit"
	"qnap-ai-control-suite/agent/internal/config"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"qnap-ai-control-suite/agent/internal/files"
	"qnap-ai-control-suite/agent/internal/jobs"
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
	Config    config.Config
	Exec      qexec.Executor
	Files     files.Service
	Jobs      *jobs.Manager
	Audit     *audit.Logger
	Docker    docker.Service
	QPKG      qpkg.Service
	Discovery discovery.Service
	System    qsystem.Service
	Network   qnetwork.Service
	Storage   storage.Service
	Users     users.Service
	Shares    shares.Service
	Logs      logs.Service
	Ecosystem ecosystem.Service
	started   time.Time
	hostname  string
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
	return &Server{Config: cfg, Exec: executor, Files: files.Service{Roots: cfg.Permissions.AllowedRoots, MaxInlineBytes: cfg.Files.MaxInlineBytes}, Jobs: jobs.New(cfg.Jobs.MaxHistory), Audit: &audit.Logger{Enabled: cfg.Audit.Enabled, Path: cfg.Audit.Path}, Docker: docker.Service{Exec: executor, Paths: cfg.DockerPaths, RedactSecrets: cfg.Privacy.RedactSecrets}, QPKG: qpkg.Service{Exec: executor}, Discovery: discovery.Service{Exec: executor}, System: qsystem.Service{Exec: executor}, Network: qnetwork.Service{Exec: executor}, Storage: storage.Service{Exec: executor}, Users: users.Service{Exec: executor}, Shares: shares.Service{Exec: executor}, Logs: logs.Service{AuditPath: cfg.Audit.Path, ServicePath: "/var/log/qnap-ai-control-agent/service.log"}, Ecosystem: ecosystem.Service{Discovery: discovery.Service{Exec: executor}}, started: time.Now(), hostname: host}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	mux.Handle("/v1/", s.auth(http.HandlerFunc(s.routes)))
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
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			s.fail(w, r, http.StatusUnauthorized, "unauthorized", "missing bearer token", nil)
			return
		}
		actual := strings.TrimPrefix(auth, "Bearer ")
		sum := sha256.Sum256([]byte(actual))
		if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(s.Config.Auth.TokenSHA256)) != 1 {
			s.audit(r, "auth.denied", "failed", nil, 0, "invalid bearer token")
			s.fail(w, r, http.StatusUnauthorized, "unauthorized", "invalid bearer token", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) routes(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/health":
		s.health(w, r)
	case "/v1/capabilities":
		s.capabilities(w, r)
	case "/v1/qnap/discovery":
		s.discovery(w, r)
	case "/v1/system/overview":
		s.systemOverview(w, r)
	case "/v1/system/info":
		s.systemInfo(w, r)
	case "/v1/system/resources":
		s.systemResources(w, r)
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
	case "/v1/network/dns":
		s.networkDNS(w, r)
	case "/v1/network/virtual-switches":
		s.virtualSwitches(w, r)
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
	case "/v1/files/list":
		s.fileList(w, r)
	case "/v1/files/stat":
		s.fileStat(w, r)
	case "/v1/files/read":
		s.fileRead(w, r)
	case "/v1/files/write":
		s.fileWrite(w, r)
	case "/v1/files/append":
		s.fileAppend(w, r)
	case "/v1/files/manage":
		s.fileManage(w, r)
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
	case "/v1/docker/command":
		s.dockerCommand(w, r)
	case "/v1/qnap/qpkg":
		s.qpkgList(w, r)
	case "/v1/qnap/qpkg/manage", "/v1/qnap/qpkg/action":
		s.qpkgManage(w, r)
	default:
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
func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, indexPage)
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	s.ok(w, r, map[string]any{"version": Version, "host": s.hostname, "uptime_s": int(time.Since(s.started).Seconds()), "profile": s.Config.Profile})
}
func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	s.ok(w, r, map[string]any{"profile": s.Config.Profile, "permissions": s.Config.Permissions, "privacy": s.Config.Privacy, "confirmation": s.Config.Confirmation, "command": s.Config.Command, "files": s.Config.Files, "jobs": s.Config.Jobs, "docker_paths": s.Config.DockerPaths})
}
func (s *Server) discovery(w http.ResponseWriter, r *http.Request) {
	s.ok(w, r, s.Discovery.Discover(r.Context()))
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
	s.ok(w, r, info)
}
func (s *Server) systemResources(w http.ResponseWriter, r *http.Request) {
	info, err := s.System.Info(r.Context())
	if err != nil {
		s.fail(w, r, 500, "system_resources_failed", err.Error(), nil)
		return
	}
	s.ok(w, r, map[string]any{"uptime_seconds": info.UptimeSeconds, "load_average": info.LoadAverage, "memory_bytes": info.Memory, "time": info.Time, "timezone": info.Timezone})
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
	out := map[string]any{"sensors": []any{}, "qnap": map[string]any{}}
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
	get := func(args ...string) string {
		result, err := s.Exec.Run(r.Context(), qexec.Request{Argv: append([]string{"/sbin/getsysinfo"}, args...), Timeout: 8 * time.Second, MaxOutput: s.Config.Command.MaxOutputBytes})
		qnap[strings.Join(args, ".")] = commandData(result, err)
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
	/*for _, key := range []string{"cputmp", "systmp", "sysfannum"} {
		result, err := s.Exec.Run(r.Context(), qexec.Request{Argv: []string{"/sbin/getsysinfo", key}, Timeout: 8 * time.Second, MaxOutput: s.Config.Command.MaxOutputBytes})
		out["qnap"].(map[string]any)[key] = commandData(result, err)
	}*/
	s.ok(w, r, out)
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
	s.ok(w, r, map[string]any{"disks": disks, "disks_error": errorText(disksErr), "raid_groups": raid, "raid_error": errorText(raidErr), "pools": pools, "pools_error": errorText(poolsErr), "volumes": volumes, "volumes_error": errorText(volumesErr), "snapshots": snapshots, "snapshots_error": errorText(snapshotsErr)})
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
	if path == "raid-groups" {
		items, err := s.Storage.RAID()
		if err != nil {
			s.fail(w, r, 500, "raid_inventory_failed", err.Error(), nil)
		} else {
			s.ok(w, r, map[string]any{"raid_groups": items})
		}
		return true
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
			s.ok(w, r, map[string]any{"snapshots": items})
		}
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
			job := s.Jobs.Start("smart-test", func(ctx context.Context, log func(string)) (any, error) {
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
		var req struct{ Action, Name, Target string }
		if !decode(w, r, &req) {
			return true
		}
		job := s.Jobs.Start("snapshot-"+req.Action, func(ctx context.Context, log func(string)) (any, error) {
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
			Action, Name string
			Args         []string
		}
		if !decode(w, r, &req) {
			return true
		}
		result, err := s.Users.ManageUser(r.Context(), req.Action, req.Name, req.Args)
		s.respondCommand(w, r, result, err)
		return true
	case "/v1/groups/manage":
		var req struct {
			Action, Name string
			Args         []string
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
	case "/v1/acl":
		var req struct{ Path string }
		if !decode(w, r, &req) {
			return true
		}
		result, err := s.Shares.ACL(r.Context(), req.Path)
		s.respondCommand(w, r, result, err)
		return true
	case "/v1/acl/set":
		var req struct{ Path, Entry string }
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
		if req.Shell == "" {
			s.fail(w, r, 400, "invalid_request", "shell is required", nil)
			return
		}
		req.Argv = []string{"/bin/sh", "-c", req.Shell}
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
	if req.MaxOutput <= 0 {
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
		Path             string `json:"path"`
		Offset, MaxBytes int64
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
		Path, ContentBase64, Mode string
		CreateParents, DryRun     bool
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
	path, err := s.Files.Write(req.Path, data, mode, req.CreateParents)
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	s.audit(r, "file.write", "success", map[string]any{"path": path, "bytes": len(data)}, 0, "")
	s.ok(w, r, map[string]any{"path": path, "bytes": len(data)})
}
func (s *Server) fileAppend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path, ContentBase64, Mode string
		CreateParents             bool
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
		Action, Path, Target, Mode string
		Recursive                  bool
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
	var req struct{ Path string }
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
		Path, Query string
		Limit       int
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
		Path     string
		Lines    int
		MaxBytes int64
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
	var req struct{ Path string }
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
	var req struct {
		Kind    string      `json:"kind"`
		Command execRequest `json:"command"`
		Shell   string      `json:"shell"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Kind == "" {
		req.Kind = "exec"
	}
	if req.Shell != "" {
		if !s.Config.Permissions.AllowShell {
			s.fail(w, r, 403, "shell_disabled", "shell execution is disabled", nil)
			return
		}
		req.Command.Argv = []string{"/bin/sh", "-c", req.Shell}
	}
	if len(req.Command.Argv) == 0 {
		s.fail(w, r, 400, "invalid_request", "command.argv or shell is required", nil)
		return
	}
	job := s.Jobs.Start(req.Kind, func(ctx context.Context, log func(string)) (any, error) {
		request := (&http.Request{}).WithContext(ctx)
		result, err := s.run(request, req.Command.Argv, qexec.Request{CWD: req.Command.CWD, Env: req.Command.Env, Timeout: time.Duration(req.Command.TimeoutSec) * time.Second, MaxOutput: req.Command.MaxOutput, DryRun: req.Command.DryRun})
		log(result.Stdout)
		log(result.Stderr)
		return result, err
	})
	s.ok(w, r, job)
}
func (s *Server) jobByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/jobs/")
	if strings.HasSuffix(id, "/logs") {
		id = strings.TrimSuffix(id, "/logs")
		job, ok := s.Jobs.Get(id)
		if !ok {
			s.fail(w, r, 404, "not_found", "job not found", nil)
			return
		}
		cursor, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
		if cursor < 0 {
			cursor = 0
		}
		if cursor > len(job.Logs) {
			cursor = len(job.Logs)
		}
		s.ok(w, r, map[string]any{"id": id, "lines": job.Logs[cursor:], "next_cursor": len(job.Logs)})
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
		Subcommand string   `json:"subcommand"`
		Args       []string `json:"args"`
		TimeoutSec int      `json:"timeout_sec"`
		DryRun     bool     `json:"dry_run"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !docker.Allowed(req.Subcommand) {
		s.fail(w, r, 400, "invalid_docker_subcommand", "unsupported docker subcommand", nil)
		return
	}
	if docker.Destructive(req.Subcommand, req.Args) && s.Config.Confirmation.Mode != "off" {
		s.fail(w, r, 409, "confirmation_required", "operation requires confirmation in this profile", map[string]any{"subcommand": req.Subcommand})
		return
	}
	if req.DryRun {
		s.ok(w, r, map[string]any{"argv": append([]string{"docker", req.Subcommand}, req.Args...), "dry_run": true})
		return
	}
	result, err := s.Docker.Run(r.Context(), append([]string{req.Subcommand}, req.Args...), req.TimeoutSec)
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
		Name, Action, Path, URL string
		DryRun                  bool
	}
	if !decode(w, r, &req) {
		return
	}
	if qpkg.Destructive(req.Action) && s.Config.Confirmation.Mode != "off" {
		s.fail(w, r, 409, "confirmation_required", "operation requires confirmation in this profile", map[string]any{"action": req.Action})
		return
	}
	if req.DryRun {
		s.ok(w, r, map[string]any{"action": req.Action, "name": req.Name, "dry_run": true})
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
	b, err := os.ReadFile(s.Config.Audit.Path)
	if err != nil {
		s.fail(w, r, 500, "audit_read_failed", err.Error(), nil)
		return
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	s.ok(w, r, map[string]any{"lines": lines})
}
func (s *Server) logSources(w http.ResponseWriter, r *http.Request) {
	s.ok(w, r, map[string]any{"sources": s.Logs.Sources()})
}
func (s *Server) logTail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Limit int    `json:"limit"`
	}
	if r.Method == http.MethodGet {
		req.Name = r.URL.Query().Get("name")
		req.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	} else if !decode(w, r, &req) {
		return
	}
	lines, err := s.Logs.Tail(req.Name, req.Limit)
	if err != nil {
		s.fail(w, r, 400, "log_tail_failed", err.Error(), nil)
		return
	}
	s.ok(w, r, map[string]any{"name": req.Name, "lines": lines})
}
func (s *Server) ecosystem(w http.ResponseWriter, r *http.Request) {
	s.ok(w, r, map[string]any{"adapters": s.Ecosystem.Inventory(r.Context())})
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
func (s *Server) audit(r *http.Request, action, status string, args any, duration int64, errorText string) {
	s.Audit.Write(audit.Event{RequestID: rc(r).id, Remote: r.RemoteAddr, Tool: r.URL.Path, Action: action, Status: status, Args: args, DurationMS: duration, Error: errorText})
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
func randomID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}
func glob(pattern string) []string { matches, _ := filepath.Glob(pattern); return matches }

const indexPage = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>QNAP AI Control Suite</title><style>body{margin:0;background:#101820;color:#edf3f7;font:15px -apple-system,BlinkMacSystemFont,Segoe UI,sans-serif}main{max-width:1100px;margin:auto;padding:28px}h1{margin:0 0 8px}section{border-top:1px solid #38505c;padding:20px 0}input,button{background:#17252e;border:1px solid #55707c;color:#edf3f7;padding:9px;border-radius:5px}button{cursor:pointer}pre{background:#0a1117;padding:12px;overflow:auto}#grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:10px}.stat{background:#17252e;padding:12px;border-radius:6px}</style><main><h1>QNAP AI Control Suite</h1><p>v1 全系统本地控制平面</p><section><input id=t type=password placeholder="Bearer token"><button onclick="loadAll()">连接与刷新</button><span id=s></span></section><section id=grid></section><section><h2>能力与教程</h2><p>在可信 LAN 的 <code>full_trust</code> 配置中，API 可访问根文件系统、执行任意命令与 shell，并保留审计记录。不要暴露端口到公网。</p><pre id=o>输入 token 后显示状态、发现结果与工具配置。</pre></section></main><script>const o=document.querySelector('#o'),g=document.querySelector('#grid'),s=document.querySelector('#s');async function api(p){let r=await fetch(p,{headers:{Authorization:'Bearer '+t.value}}),j=await r.json();if(!r.ok)throw Error(j.error?.message||r.status);return j.data}async function loadAll(){try{let[h,c,d]=await Promise.all([api('/v1/health'),api('/v1/capabilities'),api('/v1/qnap/discovery')]);s.textContent='已连接 '+h.host;g.innerHTML=[['Profile',h.profile],['Platform',d.platform],['Docker',String(d.features.docker.supported)],['SMART',String(d.features.smart.supported)]].map(x=>'<div class=stat><b>'+x[0]+'</b><br>'+x[1]+'</div>').join('');o.textContent=JSON.stringify({health:h,capabilities:c,discovery:d,mcp:{command:'node',args:['/path/to/qnap-ai-control-suite/mac-bridge/src/server.js'],env:{QACS_BASE_URL:location.origin,QACS_TOKEN:'REPLACE_WITH_TOKEN'}}},null,2)}catch(e){s.textContent='连接失败: '+e.message}}</script></main></html>`
