package discovery

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	qexec "qnap-ai-control-suite/agent/internal/exec"
)

type Service struct{ Exec qexec.Executor }
type Result struct {
	Model      string             `json:"model,omitempty"`
	Platform   string             `json:"platform"`
	Features   map[string]Feature `json:"features"`
	Utilities  map[string]string  `json:"utilities"`
	QPKGConfig bool               `json:"qpkg_config"`
}
type Feature struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

func (s Service) Discover(ctx context.Context) Result {
	r := Result{Platform: "qts_or_linux", Features: map[string]Feature{}, Utilities: map[string]string{}, QPKGConfig: fileExists("/etc/config/qpkg.conf")}
	for _, name := range []string{"getcfg", "setcfg", "qpkg_cli", "getsysinfo", "docker", "smartctl", "mdadm", "zpool", "zfs", "ip", "systemctl"} {
		if path := find(name); path != "" {
			r.Utilities[name] = path
		}
	}
	if out, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{"/sbin/getsysinfo", "model"}}); err == nil {
		r.Model = strings.TrimSpace(out.Stdout)
	}
	if _, ok := r.Utilities["zfs"]; ok {
		r.Platform = "quts_hero"
	} else if r.QPKGConfig {
		r.Platform = "qts"
	}
	for key, needs := range map[string][]string{"docker": {"docker"}, "smart": {"smartctl"}, "raid": {"mdadm"}, "zfs": {"zpool", "zfs"}, "virtualization_station": {"qpkg_cli"}, "hbs3": {"qpkg_cli"}, "virtual_switch": {"getcfg"}} {
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
	return r
}
func find(name string) string {
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	for _, path := range []string{"/sbin/" + name, "/usr/sbin/" + name, "/bin/" + name, "/usr/bin/" + name} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return ""
}
func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
