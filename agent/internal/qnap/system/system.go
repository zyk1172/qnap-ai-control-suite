package system

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	qpkg "qnap-ai-control-suite/agent/internal/qnap/qpkg"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	qexec "qnap-ai-control-suite/agent/internal/exec"
)

type Service struct {
	Exec           qexec.Executor
	ProcRoot       string
	QPKGConfigPath string
	QPKGCliPath    string
}
type Info struct {
	Hostname      string            `json:"hostname"`
	Kernel        string            `json:"kernel"`
	Architecture  string            `json:"architecture"`
	CPUCount      int               `json:"cpu_count"`
	Time          string            `json:"time"`
	Timezone      string            `json:"timezone"`
	UptimeSeconds float64           `json:"uptime_seconds"`
	LoadAverage   []float64         `json:"load_average"`
	Memory        map[string]uint64 `json:"memory_bytes"`
	Swap          map[string]uint64 `json:"swap_bytes"`
	Mounts        []Mount           `json:"mounts"`
	NTP           NTP               `json:"ntp"`
	QNAP          map[string]any    `json:"qnap,omitempty"`
}
type Mount struct {
	Device     string `json:"device"`
	Target     string `json:"target"`
	Filesystem string `json:"filesystem"`
	ReadOnly   bool   `json:"read_only"`
}
type Process struct {
	PID     int    `json:"pid"`
	User    string `json:"user,omitempty"`
	State   string `json:"state,omitempty"`
	Command string `json:"command,omitempty"`
	PPID    int    `json:"ppid,omitempty"`
}
type Unit struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Source  string `json:"source"`
	Script  string `json:"script,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}
type Socket struct {
	Protocol string `json:"protocol"`
	Local    string `json:"local"`
	Remote   string `json:"remote"`
	State    string `json:"state"`
	Inode    string `json:"inode,omitempty"`
}
type NTP struct {
	Configured bool     `json:"configured"`
	Servers    []string `json:"servers"`
	Source     string   `json:"source,omitempty"`
}

func (s Service) Info(ctx context.Context) (Info, error) {
	host, _ := os.Hostname()
	memory := readMemInfo()
	info := Info{Hostname: host, Architecture: runtime.GOARCH, CPUCount: runtime.NumCPU(), Time: time.Now().Format(time.RFC3339), Memory: memory, Swap: map[string]uint64{"total": memory["SwapTotal"], "free": memory["SwapFree"], "cached": memory["SwapCached"]}, Mounts: readMounts(), NTP: readNTP()}
	if out, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{"/bin/uname", "-r"}, Timeout: 5 * time.Second}); err == nil {
		info.Kernel = strings.TrimSpace(out.Stdout)
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(b))
		if len(fields) > 0 {
			info.UptimeSeconds, _ = strconv.ParseFloat(fields[0], 64)
		}
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		for _, part := range strings.Fields(string(b))[:min(3, len(strings.Fields(string(b))))] {
			v, _ := strconv.ParseFloat(part, 64)
			info.LoadAverage = append(info.LoadAverage, v)
		}
	}
	info.Timezone = timezone()
	return info, nil
}
func (s Service) Sockets() ([]Socket, error) {
	items := []Socket{}
	opened := false
	for _, input := range []struct{ protocol, path string }{{"tcp", "/proc/net/tcp"}, {"tcp6", "/proc/net/tcp6"}, {"udp", "/proc/net/udp"}, {"udp6", "/proc/net/udp6"}} {
		f, err := os.Open(input.path)
		if err != nil {
			continue
		}
		opened = true
		items = append(items, parseSockets(input.protocol, f)...)
		_ = f.Close()
	}
	if !opened {
		return nil, errors.New("/proc network socket tables are unavailable")
	}
	return items, nil
}
func (s Service) Processes(ctx context.Context) ([]Process, error) {
	return readProcesses(s.procRoot(), loadUsers("/etc/passwd"))
}
func (s Service) Signal(pid int, signal string) error {
	if pid <= 1 {
		return errors.New("refusing to signal pid <= 1")
	}
	sig, ok := map[string]syscall.Signal{"TERM": syscall.SIGTERM, "KILL": syscall.SIGKILL, "HUP": syscall.SIGHUP, "INT": syscall.SIGINT, "STOP": syscall.SIGSTOP, "CONT": syscall.SIGCONT}[strings.ToUpper(signal)]
	if !ok {
		return errors.New("unsupported signal")
	}
	return syscall.Kill(pid, sig)
}
func (s Service) Services(ctx context.Context) ([]Unit, error) {
	if path, err := exec.LookPath("systemctl"); err == nil {
		result, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "list-units", "--type=service", "--all", "--no-legend", "--no-pager"}, Timeout: 15 * time.Second})
		if err != nil {
			return nil, err
		}
		out := []Unit{}
		for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
			f := strings.Fields(line)
			if len(f) >= 4 {
				out = append(out, Unit{Name: f[0], State: f[3], Source: "systemctl"})
			}
		}
		return out, nil
	}
	if s.qpkgCLI() != "" && fileExists(s.qpkgConfig()) {
		return parseQPKGUnits(s.qpkgConfig())
	}
	return nil, errors.New("no stable service manager detected; QNAP QPKG system not found")
}
func (s Service) ServiceAction(ctx context.Context, name, action string) (qexec.Result, error) {
	if name == "" || strings.ContainsAny(name, "/\\\x00") {
		return qexec.Result{}, errors.New("invalid service name")
	}
	switch action {
	case "start", "stop", "restart", "reload", "enable", "disable":
	default:
		return qexec.Result{}, errors.New("unsupported service action")
	}
	path, err := exec.LookPath("systemctl")
	if err != nil {
		if s.qpkgCLI() == "" || !fileExists(s.qpkgConfig()) {
			return qexec.Result{}, errors.New("no stable service manager detected; QNAP QPKG system not found")
		}
		return qpkg.Service{Exec: s.Exec, Path: s.qpkgConfig()}.Manage(ctx, name, action, "", "")
	}
	return s.Exec.Run(ctx, qexec.Request{Argv: []string{path, action, name}, Timeout: 60 * time.Second, MaxOutput: s.Exec.MaxOutput})
}
func (s Service) procRoot() string {
	if s.ProcRoot != "" {
		return s.ProcRoot
	}
	return "/proc"
}
func (s Service) qpkgConfig() string {
	if s.QPKGConfigPath != "" {
		return s.QPKGConfigPath
	}
	return "/etc/config/qpkg.conf"
}
func (s Service) qpkgCLI() string {
	path := s.QPKGCliPath
	if path == "" {
		path = "/sbin/qpkg_cli"
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
		return path
	}
	return ""
}
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
func loadUsers(path string) map[string]string {
	users := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return users
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		fields := strings.Split(scan.Text(), ":")
		if len(fields) >= 3 {
			users[fields[2]] = fields[0]
		}
	}
	return users
}
func readProcesses(root string, users map[string]string) ([]Process, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := []Process{}
	for _, entry := range entries {
		if !entry.IsDir() || !isNumeric(entry.Name()) {
			continue
		}
		pid, _ := strconv.Atoi(entry.Name())
		if pid <= 0 {
			continue
		}
		base := filepath.Join(root, entry.Name())
		statBytes, statErr := os.ReadFile(filepath.Join(base, "stat"))
		if statErr != nil {
			continue
		}
		comm, state, ppid := parseProcStat(string(statBytes))
		cmdline, _ := os.ReadFile(filepath.Join(base, "cmdline"))
		status, _ := os.ReadFile(filepath.Join(base, "status"))
		out = append(out, Process{
			PID:     pid,
			PPID:    ppid,
			State:   state,
			User:    processUser(string(status), users),
			Command: processCommand(comm, string(cmdline)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out, nil
}
func parseProcStat(raw string) (comm, state string, ppid int) {
	start := strings.Index(raw, "(")
	end := strings.LastIndex(raw, ")")
	if start < 0 || end < 0 || end <= start {
		return "", "", 0
	}
	comm = strings.TrimSpace(raw[start+1 : end])
	fields := strings.Fields(raw[end+1:])
	if len(fields) >= 2 {
		state = fields[0]
		ppid, _ = strconv.Atoi(fields[1])
	}
	return comm, state, ppid
}
func processUser(status string, users map[string]string) string {
	for _, line := range strings.Split(status, "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return ""
		}
		if name, ok := users[fields[1]]; ok {
			return name
		}
		return fields[1]
	}
	return ""
}
func processCommand(comm, cmdline string) string {
	if value := strings.TrimSpace(strings.ReplaceAll(cmdline, "\x00", " ")); value != "" {
		return value
	}
	if comm != "" {
		return "[" + comm + "]"
	}
	return ""
}
func isNumeric(value string) bool {
	_, err := strconv.Atoi(value)
	return err == nil
}
func parseQPKGUnits(path string) ([]Unit, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []Unit{}
	var current *Unit
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if len(line) > 2 && line[0] == '[' && line[len(line)-1] == ']' {
			out = append(out, Unit{Name: line[1 : len(line)-1], State: "disabled", Source: "qnap-qpkg"})
			current = &out[len(out)-1]
			continue
		}
		if current == nil || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		switch strings.ToLower(key) {
		case "enable":
			current.Enabled = strings.EqualFold(value, "TRUE")
			if current.Enabled {
				current.State = "enabled"
			}
		case "shell", "alt_shell":
			if current.Script == "" {
				current.Script = value
			}
		}
	}
	return out, scan.Err()
}
func readMemInfo() map[string]uint64 {
	out := map[string]uint64{}
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(f[1], 10, 64)
		out[strings.TrimSuffix(f[0], ":")] = v * 1024
	}
	return out
}
func readMounts() []Mount {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()
	out := []Mount{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		out = append(out, Mount{Device: fields[0], Target: fields[1], Filesystem: fields[2], ReadOnly: strings.Contains(fields[3], "ro")})
	}
	return out
}
func timezone() string {
	for _, p := range []string{"/etc/timezone", "/etc/TZ"} {
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) != "" {
			return strings.TrimSpace(string(b))
		}
	}
	if target, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		return strings.TrimPrefix(target, "/usr/share/zoneinfo/")
	}
	return ""
}
func readNTP() NTP {
	for _, path := range []string{"/etc/ntp.conf", "/etc/chrony.conf", "/etc/config/ntp.conf"} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		servers := parseNTP(f)
		_ = f.Close()
		return NTP{Configured: len(servers) > 0, Servers: servers, Source: path}
	}
	return NTP{Servers: []string{}}
}
func parseNTP(r io.Reader) []string {
	servers := []string{}
	seen := map[string]bool{}
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		line := strings.TrimSpace(strings.SplitN(scan.Text(), "#", 2)[0])
		fields := strings.Fields(line)
		if len(fields) < 2 || (fields[0] != "server" && fields[0] != "pool") || seen[fields[1]] {
			continue
		}
		seen[fields[1]] = true
		servers = append(servers, fields[1])
	}
	return servers
}
func parseSockets(protocol string, r io.Reader) []Socket {
	out := []Socket{}
	scan := bufio.NewScanner(r)
	first := true
	for scan.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(scan.Text())
		if len(fields) < 10 {
			continue
		}
		out = append(out, Socket{Protocol: protocol, Local: procAddress(fields[1]), Remote: procAddress(fields[2]), State: fields[3], Inode: fields[9]})
	}
	return out
}
func procAddress(value string) string {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return value
	}
	port, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return value
	}
	host := parts[0]
	if len(host) == 8 {
		bytes := make([]string, 0, 4)
		for i := 6; i >= 0; i -= 2 {
			octet, err := strconv.ParseUint(host[i:i+2], 16, 8)
			if err != nil {
				return value
			}
			bytes = append(bytes, strconv.FormatUint(octet, 10))
		}
		host = strings.Join(bytes, ".")
	}
	return fmt.Sprintf("%s:%d", host, port)
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
