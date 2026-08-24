package discovery

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	qexec "qnap-ai-control-suite/agent/internal/exec"
)

type Service struct {
	Exec qexec.Executor

	// The roots and utility paths are injectable for deterministic tests and
	// for staging a read-only probe. Empty values preserve the QTS/Linux
	// defaults used by the running agent.
	ProcRoot     string
	SysRoot      string
	EtcRoot      string
	VarRoot      string
	UtilityPaths []string
}
type Result struct {
	Model       string             `json:"model,omitempty"`
	Firmware    string             `json:"firmware,omitempty"`
	Hostname    string             `json:"hostname,omitempty"`
	Arch        string             `json:"arch"`
	CPUCount    int                `json:"cpu_count"`
	MemoryBytes uint64             `json:"memory_bytes,omitempty"`
	DiskCount   int                `json:"disk_count"`
	Platform    string             `json:"platform"`
	Features    map[string]Feature `json:"features"`
	Utilities   map[string]string  `json:"utilities"`
	QPKGConfig  bool               `json:"qpkg_config"`
	QPKGs       []string           `json:"qpkgs"`
}
type Feature struct {
	Supported bool     `json:"supported"`
	Reason    string   `json:"reason,omitempty"`
	ReadOnly  bool     `json:"read_only,omitempty"`
	Evidence  []string `json:"evidence,omitempty"`
}

func (s Service) Discover(ctx context.Context) Result {
	host, _ := os.Hostname()
	qpkgConfig := filepath.Join(s.etcRoot(), "config", "qpkg.conf")
	r := Result{
		Hostname:    host,
		Arch:        runtime.GOARCH,
		CPUCount:    runtime.NumCPU(),
		MemoryBytes: memoryBytesAt(s.procRoot()),
		DiskCount:   diskCountAt(s.sysRoot()),
		Platform:    "qts_or_linux",
		Features:    map[string]Feature{},
		Utilities:   map[string]string{},
		QPKGConfig:  fileExists(qpkgConfig),
		QPKGs:       installedQPKGs(qpkgConfig),
	}
	for _, name := range []string{"getcfg", "setcfg", "qpkg_cli", "getsysinfo", "docker", "smartctl", "mdadm", "zpool", "zfs", "ip", "systemctl", "systemd", "systemd-run", "crond", "cron", "crontab", "anacron", "at", "atd", "upsc"} {
		if path := s.utility(name); path != "" {
			r.Utilities[name] = path
		}
	}
	if _, ok := r.Utilities["docker"]; !ok && len(s.UtilityPaths) == 0 {
		if path := containerStationDocker(); path != "" {
			r.Utilities["docker"] = path
		}
	}
	if path, ok := r.Utilities["getsysinfo"]; ok {
		if out, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "model"}}); err == nil {
			r.Model = strings.TrimSpace(out.Stdout)
		}
		if out, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "version"}}); err == nil {
			r.Firmware = strings.TrimSpace(out.Stdout)
		}
	}
	if _, ok := r.Utilities["zfs"]; ok {
		r.Platform = "quts_hero"
	} else if r.QPKGConfig {
		r.Platform = "qts"
	}
	for key, needs := range map[string][]string{"docker": {"docker"}, "smart": {"smartctl"}, "raid": {"mdadm"}, "zfs": {"zpool", "zfs"}, "virtual_switch": {"getcfg"}} {
		supported := true
		for _, need := range needs {
			if _, ok := r.Utilities[need]; !ok {
				supported = false
			}
		}
		reason := ""
		if !supported {
			reason = "QNAP runtime probe required"
		}
		r.Features[key] = Feature{Supported: supported, Reason: reason}
	}
	r.Features["virtualization_station"] = qpkgFeature(r.QPKGs, []string{"virtualizationstation", "virtualization station", "qkvm"})
	r.Features["hbs3"] = qpkgFeature(r.QPKGs, []string{"hybrid backup", "hybridbackup", "hbs"})
	r.Features["container_station"] = qpkgFeature(r.QPKGs, []string{"container-station", "container station"})
	r.Features["qsirch"] = qpkgFeature(r.QPKGs, []string{"qsirch"})
	r.Features["multimedia_console"] = qpkgFeature(r.QPKGs, []string{"multimedia console", "multimediaconsole"})
	r.Features["iscsi"] = Feature{Supported: false, Reason: "QNAP runtime probe required for stable iSCSI adapter"}
	r.Features["certificates"] = Feature{Supported: false, Reason: "QNAP runtime probe required for certificate inventory adapter"}
	if path := s.utility("upsc"); path != "" {
		r.Features["ups"] = Feature{Supported: true, Reason: "NUT upsc found at " + path}
	} else {
		r.Features["ups"] = Feature{Supported: false, Reason: "NUT upsc utility not found"}
	}
	r.Features["scheduler"] = s.schedulerFeature(r.Utilities)
	r.Features["cron"] = s.cronFeature(r.Utilities)
	r.Features["systemd_timers"] = s.systemdTimersFeature(r.Utilities)
	r.Features["scheduled_tasks"] = scheduledTasksFeature(r.Features["cron"], r.Features["systemd_timers"])
	return r
}

func (s Service) utility(name string) string {
	if len(s.UtilityPaths) > 0 {
		return findIn(name, s.UtilityPaths)
	}
	return find(name)
}

func (s Service) procRoot() string {
	if strings.TrimSpace(s.ProcRoot) != "" {
		return s.ProcRoot
	}
	return "/proc"
}

func (s Service) sysRoot() string {
	if strings.TrimSpace(s.SysRoot) != "" {
		return s.SysRoot
	}
	return "/sys"
}

func (s Service) etcRoot() string {
	if strings.TrimSpace(s.EtcRoot) != "" {
		return s.EtcRoot
	}
	return "/etc"
}

func (s Service) varRoot() string {
	if strings.TrimSpace(s.VarRoot) != "" {
		return s.VarRoot
	}
	return "/var"
}

func memoryBytes() uint64 { return memoryBytesAt("/proc") }

func memoryBytesAt(procRoot string) uint64 {
	b, err := os.ReadFile(filepath.Join(procRoot, "meminfo"))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "MemTotal:" {
			n, _ := strconv.ParseUint(f[1], 10, 64)
			return n * 1024
		}
	}
	return 0
}

func diskCount() int { return diskCountAt("/sys") }

func diskCountAt(sysRoot string) int {
	entries, err := os.ReadDir(filepath.Join(sysRoot, "block"))
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		n := entry.Name()
		if strings.HasPrefix(n, "sd") || strings.HasPrefix(n, "nvme") {
			count++
		}
	}
	return count
}

func find(name string) string {
	if path := findIn(name, strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))); path != "" {
		return path
	}
	return findStandard(name)
}

func findIn(name string, directories []string) string {
	for _, dir := range directories {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return ""
}

func findStandard(name string) string {
	for _, path := range []string{"/sbin/" + name, "/usr/sbin/" + name, "/bin/" + name, "/usr/bin/" + name} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return ""
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
func installedQPKGs(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return []string{}
	}
	defer f.Close()
	out := []string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) > 2 && line[0] == '[' && line[len(line)-1] == ']' {
			out = append(out, line[1:len(line)-1])
		}
	}
	return out
}
func qpkgFeature(installed, needles []string) Feature {
	for _, item := range installed {
		lower := strings.ToLower(item)
		for _, needle := range needles {
			if strings.Contains(lower, needle) {
				return Feature{Supported: false, Reason: "QPKG detected; stable local adapter requires runtime probe"}
			}
		}
	}
	return Feature{Supported: false, Reason: "QPKG is not installed"}
}

func (s Service) schedulerFeature(utilities map[string]string) Feature {
	evidence := []string{}
	for _, name := range []string{"crond", "cron"} {
		if path := utilities[name]; path != "" {
			evidence = append(evidence, "utility:"+name+"="+path)
		}
	}
	if init := s.pidOneName(); init != "" {
		if init == "systemd" {
			evidence = append(evidence, "pid1=systemd")
		}
	}
	for _, name := range []string{"crond", "cron"} {
		if s.processExists(name) {
			evidence = append(evidence, "process:"+name)
		}
	}
	if len(evidence) == 0 {
		return Feature{
			Supported: false,
			ReadOnly:  true,
			Reason:    "capability unavailable: no cron daemon or systemd runtime evidence",
		}
	}
	return Feature{
		Supported: true,
		ReadOnly:  true,
		Evidence:  evidence,
		Reason:    "read-only scheduler evidence found; active schedules are not asserted",
	}
}

func (s Service) cronFeature(utilities map[string]string) Feature {
	evidence := []string{}
	if path := utilities["crontab"]; path != "" {
		evidence = append(evidence, "utility:crontab="+path)
	}
	for _, path := range s.cronPaths() {
		if readablePath(path) {
			evidence = append(evidence, "path:"+path)
		}
	}
	if len(evidence) == 0 {
		return Feature{
			Supported: false,
			ReadOnly:  true,
			Reason:    "capability unavailable: no crontab utility or readable cron configuration",
		}
	}
	return Feature{
		Supported: true,
		ReadOnly:  true,
		Evidence:  evidence,
		Reason:    "read-only cron inventory evidence found; no schedule changes are executed",
	}
}

func (s Service) systemdTimersFeature(utilities map[string]string) Feature {
	init := s.pidOneName()
	path := utilities["systemctl"]
	if init != "systemd" || path == "" {
		return Feature{
			Supported: false,
			ReadOnly:  true,
			Reason:    "capability unavailable: systemd PID 1 and systemctl were not both verified",
		}
	}
	return Feature{
		Supported: true,
		ReadOnly:  true,
		Evidence:  []string{"pid1=systemd", "utility:systemctl=" + path},
		Reason:    "systemd timer inventory is available through read-only systemctl queries",
	}
}

func scheduledTasksFeature(cron, timers Feature) Feature {
	evidence := append([]string{}, cron.Evidence...)
	evidence = append(evidence, timers.Evidence...)
	if !cron.Supported && !timers.Supported {
		return Feature{
			Supported: false,
			ReadOnly:  true,
			Reason:    "capability unavailable: no verifiable cron or systemd timer inventory",
		}
	}
	return Feature{
		Supported: true,
		ReadOnly:  true,
		Evidence:  evidence,
		Reason:    "read-only scheduled-task inventory is available",
	}
}

func (s Service) cronPaths() []string {
	etc := s.etcRoot()
	return []string{
		filepath.Join(etc, "config", "crontab"),
		filepath.Join(etc, "crontab"),
		filepath.Join(etc, "cron.d"),
		filepath.Join(etc, "cron.hourly"),
		filepath.Join(etc, "cron.daily"),
		filepath.Join(etc, "cron.weekly"),
		filepath.Join(etc, "cron.monthly"),
		filepath.Join(s.varRoot(), "spool", "cron"),
		filepath.Join(s.varRoot(), "spool", "cron", "crontabs"),
	}
}

func readablePath(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return info.IsDir() || info.Mode().IsRegular()
}

func (s Service) pidOneName() string {
	for _, name := range []string{"comm", "cmdline"} {
		data, err := os.ReadFile(filepath.Join(s.procRoot(), "1", name))
		if err != nil {
			continue
		}
		value := strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", " "))
		if fields := strings.Fields(value); len(fields) > 0 {
			return strings.ToLower(filepath.Base(fields[0]))
		}
	}
	return ""
}

func (s Service) processExists(needle string) bool {
	entries, err := os.ReadDir(s.procRoot())
	if err != nil {
		return false
	}
	needle = strings.ToLower(needle)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		for _, name := range []string{"comm", "cmdline"} {
			data, err := os.ReadFile(filepath.Join(s.procRoot(), entry.Name(), name))
			if err != nil {
				continue
			}
			if strings.Contains(strings.ToLower(strings.ReplaceAll(string(data), "\x00", " ")), needle) {
				return true
			}
		}
	}
	return false
}

func containerStationDocker() string {
	for i := 1; i <= 8; i++ {
		root := "/share/CACHEDEV" + strconv.Itoa(i) + "_DATA/.qpkg/container-station"
		for _, relative := range []string{"bin/system-docker", "bin/docker", "usr/bin/docker", "usr/bin/.libs/docker"} {
			path := filepath.Join(root, relative)
			if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
				return path
			}
		}
	}
	return ""
}
