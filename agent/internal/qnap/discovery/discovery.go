package discovery

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
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
	QPKGs      []string           `json:"qpkgs"`
}
type Feature struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

func (s Service) Discover(ctx context.Context) Result {
	r := Result{Platform: "qts_or_linux", Features: map[string]Feature{}, Utilities: map[string]string{}, QPKGConfig: fileExists("/etc/config/qpkg.conf"), QPKGs: installedQPKGs("/etc/config/qpkg.conf")}
	for _, name := range []string{"getcfg", "setcfg", "qpkg_cli", "getsysinfo", "docker", "smartctl", "mdadm", "zpool", "zfs", "ip", "systemctl"} {
		if path := find(name); path != "" {
			r.Utilities[name] = path
		}
	}
	if _, ok := r.Utilities["docker"]; !ok {
		if path := containerStationDocker(); path != "" {
			r.Utilities["docker"] = path
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
	r.Features["iscsi"] = Feature{Supported: false, Reason: "QNAP runtime probe required for stable iSCSI adapter"}
	r.Features["certificates"] = Feature{Supported: false, Reason: "QNAP runtime probe required for certificate inventory adapter"}
	r.Features["ups"] = Feature{Supported: find("upsc") != "", Reason: "NUT upsc utility not found"}
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
