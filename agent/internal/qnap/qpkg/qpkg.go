package qpkg

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	Exec     qexec.Executor
	Path     string
	ProcRoot string
}
type Inventory struct {
	Packages []map[string]string `json:"packages"`
	CLI      *qexec.Result       `json:"qpkg_cli,omitempty"`
	CLIError string              `json:"qpkg_cli_error,omitempty"`
}

type procProcess struct {
	pid     int
	pidText string
	exe     string
	cmdline string
}

type procSnapshot struct {
	processes []procProcess
}

type procScanner func(string) (procSnapshot, error)

func (s Service) List(ctx context.Context) ([]map[string]string, error) {
	path := s.Path
	if path == "" {
		path = "/etc/config/qpkg.conf"
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f), nil
}
func (s Service) Inventory(ctx context.Context) (Inventory, error) {
	packages, err := s.List(ctx)
	if err != nil {
		return Inventory{}, err
	}
	out := Inventory{Packages: packages}
	result, runErr := s.Exec.Run(ctx, qexec.Request{Argv: []string{"/sbin/qpkg_cli", "-l"}, Timeout: 20 * time.Second, MaxOutput: s.Exec.MaxOutput})
	if runErr != nil {
		out.CLIError = runErr.Error()
	} else {
		out.CLI = &result
	}
	populateProcessStates(out.Packages, s.procRoot(), scanProc)
	for _, pkg := range out.Packages {
		if root := pkg["Install_Path"]; root != "" {
			script := filepath.Join(root, ".install")
			if _, err := os.Stat(script); err == nil {
				pkg["init_script"] = script
			}
		}
		if value := pkg["Enable"]; value != "" {
			pkg["enabled"] = strconv.FormatBool(strings.EqualFold(value, "TRUE"))
		}
	}
	return out, nil
}

func (s Service) procRoot() string {
	if s.ProcRoot != "" {
		return s.ProcRoot
	}
	return "/proc"
}

func populateProcessStates(packages []map[string]string, procRoot string, scan procScanner) {
	needsScan := false
	for _, pkg := range packages {
		if pkg["Install_Path"] != "" {
			needsScan = true
			break
		}
	}
	if !needsScan {
		return
	}

	snapshot, procErr := scan(procRoot)
	for _, pkg := range packages {
		root := pkg["Install_Path"]
		if root == "" {
			continue
		}
		if procErr != nil {
			pkg["process_state"] = "unknown"
			pkg["process_state_error"] = procErr.Error()
			continue
		}
		pids := snapshot.matchingPIDs(root)
		if len(pids) > 0 {
			pkg["process_state"] = "running"
			pkg["process_pids"] = strings.Join(pids, ",")
		} else {
			pkg["process_state"] = "stopped"
		}
	}
}

func scanProc(procRoot string) (procSnapshot, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return procSnapshot{}, err
	}
	processes := make([]procProcess, 0, len(entries))
	for _, entry := range entries {
		pidText := entry.Name()
		pid, ok := parsePID(pidText)
		if !entry.IsDir() || !ok {
			continue
		}
		base := filepath.Join(procRoot, pidText)
		exe, _ := os.Readlink(filepath.Join(base, "exe"))
		cmdline, _ := os.ReadFile(filepath.Join(base, "cmdline"))
		processes = append(processes, procProcess{
			pid:     pid,
			pidText: pidText,
			exe:     exe,
			cmdline: string(cmdline),
		})
	}
	sort.Slice(processes, func(i, j int) bool {
		if processes[i].pid == processes[j].pid {
			return processes[i].pidText < processes[j].pidText
		}
		return processes[i].pid < processes[j].pid
	})
	return procSnapshot{processes: processes}, nil
}

func (snapshot procSnapshot) matchingPIDs(installPath string) []string {
	root := filepath.Clean(installPath)
	pids := make([]string, 0)
	for _, process := range snapshot.processes {
		if matchesInstallPath(root, process.exe) || matchesCmdline(root, process.cmdline) {
			pids = append(pids, process.pidText)
		}
	}
	return pids
}

func runningPIDs(installPath, procRoot string) ([]string, error) {
	snapshot, err := scanProc(procRoot)
	if err != nil {
		return nil, err
	}
	return snapshot.matchingPIDs(installPath), nil
}

func isPID(value string) bool {
	_, ok := parsePID(value)
	return ok
}

func parsePID(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, false
		}
	}
	number, err := strconv.Atoi(value)
	return number, err == nil
}

func matchesCmdline(root, cmdline string) bool {
	for _, argument := range strings.Split(cmdline, "\x00") {
		if matchesInstallPath(root, argument) {
			return true
		}
	}
	return false
}

func matchesInstallPath(root, value string) bool {
	return value == root || strings.Contains(value, root+string(os.PathSeparator))
}
func Parse(r interface{ Read([]byte) (int, error) }) []map[string]string {
	scanner := bufio.NewScanner(r)
	out := []map[string]string{}
	var current map[string]string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) > 2 && line[0] == '[' && line[len(line)-1] == ']' {
			current = map[string]string{"name": line[1 : len(line)-1]}
			out = append(out, current)
			continue
		}
		if current == nil || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		current[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out
}
func (s Service) Manage(ctx context.Context, name, action, path, url string) (qexec.Result, error) {
	if action == "restart" {
		// qpkg_cli has no restart flag. Preserve argv boundaries and execute the
		// documented stop/start pair instead of falling back to a shell string.
		stopArgs, err := CommandArgs(name, "stop", path, url)
		if err != nil {
			return qexec.Result{}, err
		}
		if _, err := s.run(ctx, stopArgs); err != nil {
			return qexec.Result{}, err
		}
		startArgs, err := CommandArgs(name, "start", path, url)
		if err != nil {
			return qexec.Result{}, err
		}
		return s.run(ctx, startArgs)
	}
	args, err := CommandArgs(name, action, path, url)
	if err != nil {
		return qexec.Result{}, err
	}
	return s.run(ctx, args)
}

func (s Service) run(ctx context.Context, args []string) (qexec.Result, error) {
	return s.Exec.Run(ctx, qexec.Request{Argv: append([]string{"/sbin/qpkg_cli"}, args...), Timeout: s.Exec.DefaultTimeout, MaxOutput: s.Exec.MaxOutput})
}

// CommandArgs maps the public action names to the flags accepted by QTS
// qpkg_cli. It intentionally does not accept arbitrary flags from callers.
func CommandArgs(name, action, path, url string) ([]string, error) {
	requireName := func() error {
		if strings.TrimSpace(name) == "" {
			return errors.New("name is required")
		}
		return nil
	}
	switch action {
	case "start", "stop", "enable", "disable", "status", "download", "cancel", "remove":
		if err := requireName(); err != nil {
			return nil, err
		}
		flag := map[string]string{"start": "--start", "stop": "--stop", "enable": "--enable", "disable": "--disable", "status": "--status", "download": "--download", "cancel": "--cancel", "remove": "--remove"}[action]
		return []string{flag, name}, nil
	case "install_file":
		if strings.TrimSpace(path) == "" {
			return nil, errors.New("path is required for install_file")
		}
		return []string{"--manually", path}, nil
	case "install_url":
		if strings.TrimSpace(url) == "" {
			return nil, errors.New("url is required for install_url")
		}
		return []string{"--url", url}, nil
	case "update_all":
		return []string{"--update_all"}, nil
	case "clean":
		return []string{"--clean"}, nil
	case "add":
		if err := requireName(); err != nil {
			return nil, err
		}
		return []string{"--add", name}, nil
	default:
		return nil, errors.New("unsupported qpkg action")
	}
}
func Destructive(action string) bool {
	switch action {
	case "add", "install_file", "install_url", "remove", "update_all":
		return true
	}
	return false
}
