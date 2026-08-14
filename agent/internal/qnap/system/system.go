package system

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	qexec "qnap-ai-control-suite/agent/internal/exec"
)

type Service struct{ Exec qexec.Executor }
type Info struct {
	Hostname      string            `json:"hostname"`
	Kernel        string            `json:"kernel"`
	Architecture  string            `json:"architecture"`
	Time          string            `json:"time"`
	Timezone      string            `json:"timezone"`
	UptimeSeconds float64           `json:"uptime_seconds"`
	LoadAverage   []float64         `json:"load_average"`
	Memory        map[string]uint64 `json:"memory_bytes"`
	Mounts        []Mount           `json:"mounts"`
}
type Mount struct {
	Device, Target, Filesystem string
	ReadOnly                   bool
}
type Process struct {
	PID     int    `json:"pid"`
	User    string `json:"user,omitempty"`
	State   string `json:"state,omitempty"`
	Command string `json:"command,omitempty"`
	PPID    int    `json:"ppid,omitempty"`
}
type Unit struct{ Name, State, Source string }

func (s Service) Info(ctx context.Context) (Info, error) {
	host, _ := os.Hostname()
	info := Info{Hostname: host, Architecture: runtime.GOARCH, Time: time.Now().Format(time.RFC3339), Memory: readMemInfo(), Mounts: readMounts()}
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
func (s Service) Processes(ctx context.Context) ([]Process, error) {
	result, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{"/bin/ps", "-eo", "pid=,ppid=,user=,state=,args="}, Timeout: 15 * time.Second, MaxOutput: 8 * 1024 * 1024})
	if err != nil {
		return nil, err
	}
	out := []Process{}
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		out = append(out, Process{PID: pid, PPID: ppid, User: f[2], State: f[3], Command: strings.Join(f[4:], " ")})
	}
	return out, nil
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
	return nil, errors.New("no stable service manager detected; use nas_exec after qnap probe")
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
		return qexec.Result{}, errors.New("no stable service manager detected; use nas_exec after qnap probe")
	}
	return s.Exec.Run(ctx, qexec.Request{Argv: []string{path, action, name}, Timeout: 60 * time.Second, MaxOutput: s.Exec.MaxOutput})
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
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = fmt.Sprintf
