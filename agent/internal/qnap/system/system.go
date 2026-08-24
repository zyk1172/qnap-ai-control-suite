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
	PID     int           `json:"pid"`
	User    string        `json:"user,omitempty"`
	State   string        `json:"state,omitempty"`
	Command string        `json:"command,omitempty"`
	PPID    int           `json:"ppid,omitempty"`
	CPU     ProcessCPU    `json:"cpu"`
	Memory  ProcessMemory `json:"memory"`
	IO      ProcessIO     `json:"io"`
}

// ProcessCPU contains cumulative CPU time exported by /proc/<pid>/stat.
// Percent is deliberately nil for the ordinary process-list request: a
// percentage requires two samples separated by a known interval, and this
// service does not invent one from a single cumulative snapshot.
type ProcessCPU struct {
	UserJiffies        uint64   `json:"user_jiffies"`
	SystemJiffies      uint64   `json:"system_jiffies"`
	TotalJiffies       uint64   `json:"total_jiffies"`
	ChildUserJiffies   uint64   `json:"child_user_jiffies,omitempty"`
	ChildSystemJiffies uint64   `json:"child_system_jiffies,omitempty"`
	TimeBasis          string   `json:"time_basis"`
	Percent            *float64 `json:"percent"`
	PercentStatus      string   `json:"percent_status"`
}

// ProcessMemory contains byte counters from /proc/<pid>/status and the RSS
// page count in /proc/<pid>/stat as a compatibility fallback.
type ProcessMemory struct {
	Available     bool   `json:"available"`
	VirtualBytes  uint64 `json:"virtual_bytes"`
	RSSBytes      uint64 `json:"rss_bytes"`
	PeakRSSBytes  uint64 `json:"peak_rss_bytes"`
	RSSAnonBytes  uint64 `json:"rss_anon_bytes"`
	RSSFileBytes  uint64 `json:"rss_file_bytes"`
	RSSShmemBytes uint64 `json:"rss_shmem_bytes"`
	SwapBytes     uint64 `json:"swap_bytes"`
}

// ProcessIO contains the counters exposed by /proc/<pid>/io. The counters
// are cumulative for the process lifetime and may be unavailable when the
// kernel or the process permissions do not expose that file.
type ProcessIO struct {
	Available           bool   `json:"available"`
	ReadCharsBytes      uint64 `json:"read_chars_bytes"`
	WriteCharsBytes     uint64 `json:"write_chars_bytes"`
	ReadSyscalls        uint64 `json:"read_syscalls"`
	WriteSyscalls       uint64 `json:"write_syscalls"`
	ReadBytes           uint64 `json:"read_bytes"`
	WriteBytes          uint64 `json:"write_bytes"`
	CancelledWriteBytes uint64 `json:"cancelled_write_bytes"`
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
	pageSize := uint64(os.Getpagesize())
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
		stat := parseProcStatDetails(string(statBytes))
		cmdline, _ := os.ReadFile(filepath.Join(base, "cmdline"))
		status, _ := os.ReadFile(filepath.Join(base, "status"))
		ioBytes, _ := os.ReadFile(filepath.Join(base, "io"))
		out = append(out, Process{
			PID:     pid,
			PPID:    stat.PPID,
			State:   stat.State,
			User:    processUser(string(status), users),
			Command: processCommand(stat.Comm, string(cmdline)),
			CPU: ProcessCPU{
				UserJiffies:        stat.UserJiffies,
				SystemJiffies:      stat.SystemJiffies,
				TotalJiffies:       saturatingAddUint64(stat.UserJiffies, stat.SystemJiffies),
				ChildUserJiffies:   stat.ChildUserJiffies,
				ChildSystemJiffies: stat.ChildSystemJiffies,
				TimeBasis:          "cumulative",
				PercentStatus:      "unavailable_no_sample",
			},
			Memory: parseProcessMemory(string(status), stat.RSSPages, stat.VirtualBytes, pageSize),
			IO:     parseProcessIO(string(ioBytes)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out, nil
}
func parseProcStat(raw string) (comm, state string, ppid int) {
	stat := parseProcStatDetails(raw)
	return stat.Comm, stat.State, stat.PPID
}

type procStat struct {
	Comm               string
	State              string
	PPID               int
	UserJiffies        uint64
	SystemJiffies      uint64
	ChildUserJiffies   uint64
	ChildSystemJiffies uint64
	VirtualBytes       uint64
	RSSPages           int64
}

func parseProcStatDetails(raw string) procStat {
	stat := procStat{RSSPages: -1}
	start := strings.Index(raw, "(")
	end := strings.LastIndex(raw, ")")
	if start < 0 || end < 0 || end <= start {
		return stat
	}
	stat.Comm = strings.TrimSpace(raw[start+1 : end])
	fields := strings.Fields(raw[end+1:])
	if len(fields) < 2 {
		return stat
	}
	stat.State = fields[0]
	stat.PPID, _ = strconv.Atoi(fields[1])
	stat.UserJiffies = parseProcUintField(fields, 14)
	stat.SystemJiffies = parseProcUintField(fields, 15)
	stat.ChildUserJiffies = parseProcUintField(fields, 16)
	stat.ChildSystemJiffies = parseProcUintField(fields, 17)
	stat.VirtualBytes = parseProcUintField(fields, 23)
	stat.RSSPages = parseProcIntField(fields, 24, -1)
	return stat
}

// Fields after the closing comm parenthesis start at /proc stat field 3
// (state), so field N is stored at index N-3.
func parseProcUintField(fields []string, field int) uint64 {
	index := field - 3
	if index < 0 || index >= len(fields) {
		return 0
	}
	value, err := strconv.ParseUint(fields[index], 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func parseProcIntField(fields []string, field int, fallback int64) int64 {
	index := field - 3
	if index < 0 || index >= len(fields) {
		return fallback
	}
	value, err := strconv.ParseInt(fields[index], 10, 64)
	if err != nil {
		return fallback
	}
	return value
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

func parseProcessMemory(status string, rssPages int64, virtualBytes, pageSize uint64) ProcessMemory {
	memory := ProcessMemory{}
	var hasVirtual, hasRSS, hasMetric bool
	for _, line := range strings.Split(status, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, ok := parseProcMemoryValue(fields[1:])
		if !ok {
			continue
		}
		switch key {
		case "VmSize":
			memory.VirtualBytes = value
			hasVirtual = true
			hasMetric = true
		case "VmHWM":
			memory.PeakRSSBytes = value
			hasMetric = true
		case "VmRSS":
			memory.RSSBytes = value
			hasRSS = true
			hasMetric = true
		case "RssAnon":
			memory.RSSAnonBytes = value
			hasMetric = true
		case "RssFile":
			memory.RSSFileBytes = value
			hasMetric = true
		case "RssShmem":
			memory.RSSShmemBytes = value
			hasMetric = true
		case "VmSwap":
			memory.SwapBytes = value
			hasMetric = true
		}
	}

	// Older or reduced QNAP kernels may omit VmRSS/VmSize from status while
	// still exposing the stat fields. Keep the fallback explicit and in bytes.
	if !hasVirtual && virtualBytes > 0 {
		memory.VirtualBytes = virtualBytes
		hasVirtual = true
		hasMetric = true
	}
	if !hasRSS && rssPages >= 0 && pageSize > 0 {
		memory.RSSBytes = saturatingMulUint64(uint64(rssPages), pageSize)
		hasRSS = true
		hasMetric = true
	}
	memory.Available = hasMetric
	return memory
}

func parseProcMemoryValue(fields []string) (uint64, bool) {
	if len(fields) == 0 {
		return 0, false
	}
	value, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, false
	}
	multiplier := uint64(1)
	if len(fields) >= 2 {
		switch strings.ToLower(fields[1]) {
		case "b":
			multiplier = 1
		case "kb":
			multiplier = 1024
		case "mb":
			multiplier = 1024 * 1024
		case "gb":
			multiplier = 1024 * 1024 * 1024
		default:
			return 0, false
		}
	}
	if value > ^uint64(0)/multiplier {
		return 0, false
	}
	return value * multiplier, true
}

func saturatingAddUint64(left, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}

func saturatingMulUint64(left, right uint64) uint64 {
	if left != 0 && right > ^uint64(0)/left {
		return ^uint64(0)
	}
	return left * right
}

func parseProcessIO(raw string) ProcessIO {
	ioCounters := ProcessIO{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "rchar":
			ioCounters.ReadCharsBytes = value
		case "wchar":
			ioCounters.WriteCharsBytes = value
		case "syscr":
			ioCounters.ReadSyscalls = value
		case "syscw":
			ioCounters.WriteSyscalls = value
		case "read_bytes":
			ioCounters.ReadBytes = value
		case "write_bytes":
			ioCounters.WriteBytes = value
		case "cancelled_write_bytes":
			ioCounters.CancelledWriteBytes = value
		default:
			continue
		}
		ioCounters.Available = true
	}
	return ioCounters
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
