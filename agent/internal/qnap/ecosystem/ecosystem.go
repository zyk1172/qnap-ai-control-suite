package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	stdexec "os/exec"
	"path/filepath"
	"qnap-ai-control-suite/agent/internal/config"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"qnap-ai-control-suite/agent/internal/qnap/discovery"
	"sort"
	"strings"
	"time"
)

type Service struct {
	Discovery discovery.Service
	Exec      qexec.Executor
	Adapters  map[string]config.QNAPAdapter
}
type Adapter struct {
	Name         string   `json:"name"`
	Installed    bool     `json:"installed"`
	Supported    bool     `json:"supported"`
	Reason       string   `json:"reason,omitempty"`
	Capabilities []string `json:"capabilities"`
}
type Certificate struct {
	Path        string   `json:"path"`
	Subject     string   `json:"subject,omitempty"`
	Issuer      string   `json:"issuer,omitempty"`
	NotBefore   string   `json:"not_before,omitempty"`
	NotAfter    string   `json:"not_after,omitempty"`
	Serial      string   `json:"serial,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	SAN         []string `json:"san,omitempty"`
}

func (s Service) Inventory(ctx context.Context) []Adapter {
	d := s.Discovery.Discover(ctx)
	vmInstalled := has(d.QPKGs, "virtualization") || has(d.QPKGs, "qkvm")
	hbsInstalled := has(d.QPKGs, "hybrid backup") || has(d.QPKGs, "hybridbackup") || has(d.QPKGs, "hbs")
	sharesInstalled := pathExists("/etc/config/smb.conf") || pathExists("/etc/samba/smb.conf")
	return []Adapter{
		s.adapter("virtualization_station", vmInstalled, "QKVM/Virtualization Station detected; configure verified commands from a NAS runtime probe", []string{"list", "info", "start", "stop", "restart", "force_stop", "snapshot", "clone"}),
		s.adapter("hbs3", hbsInstalled, "HBS package detected; configure verified commands from a NAS runtime probe", []string{"job_list", "job_status", "run", "stop", "logs"}),
		s.adapter("iscsi", d.Features["iscsi"].Supported, "configure verified iSCSI/LUN commands from a NAS runtime probe", []string{"targets", "luns", "mapping", "status", "snapshots", "online", "offline", "expand", "clone"}),
		s.adapter("certificates", true, "configure verified certificate commands from a NAS runtime probe", []string{"list", "current", "expiry", "issuer", "subject", "san", "import", "replace"}),
		s.adapter("shares", sharesInstalled, "SMB/NFS configuration found; configure verified QNAP shared-folder commands from a NAS runtime probe", []string{"create", "delete", "rename", "set_path", "quota", "hidden", "recycle_bin", "nfs_export"}),
		{Name: "ups", Installed: d.Features["ups"].Supported, Supported: d.Features["ups"].Supported, Reason: d.Features["ups"].Reason, Capabilities: []string{"state", "battery", "runtime", "input", "configuration"}},
	}
}

func (s Service) adapter(name string, installed bool, reason string, capabilities []string) Adapter {
	configured := s.Adapters[name]
	if len(configured.Commands) > 0 {
		return Adapter{Name: name, Installed: true, Supported: true, Reason: "verified command adapter configured", Capabilities: sortedKeys(configured.Commands)}
	}
	return Adapter{Name: name, Installed: installed, Supported: false, Reason: reason, Capabilities: capabilities}
}

// Command expands one configured command without invoking a shell. A caller
// must still enforce its active agent profile before executing the result.
func (s Service) Command(adapter, action string, values map[string]string, args []string) ([]string, time.Duration, error) {
	setting, ok := s.Adapters[adapter]
	if !ok || len(setting.Commands) == 0 {
		return nil, 0, fmt.Errorf("%s adapter has no verified command configuration; run qnap_probe and configure qnap_adapters", adapter)
	}
	template, ok := setting.Commands[action]
	if !ok {
		return nil, 0, fmt.Errorf("%s adapter does not configure action %q", adapter, action)
	}
	argv := make([]string, 0, len(template)+len(args))
	usesArgs := false
	for _, item := range template {
		if item == "{args}" {
			usesArgs = true
			argv = append(argv, args...)
			continue
		}
		expanded := item
		for _, key := range []string{"id", "name", "target"} {
			placeholder := "{" + key + "}"
			if strings.Contains(expanded, placeholder) {
				if strings.TrimSpace(values[key]) == "" {
					return nil, 0, fmt.Errorf("%s is required by configured %s action", key, action)
				}
				expanded = strings.ReplaceAll(expanded, placeholder, values[key])
			}
		}
		if strings.Contains(expanded, "{") || strings.Contains(expanded, "}") {
			return nil, 0, fmt.Errorf("configured %s action contains an unknown placeholder", action)
		}
		argv = append(argv, expanded)
	}
	if len(args) > 0 && !usesArgs {
		return nil, 0, fmt.Errorf("configured %s action does not accept args", action)
	}
	if len(argv) == 0 || !filepath.IsAbs(argv[0]) {
		return nil, 0, errors.New("configured adapter command must start with an absolute executable path")
	}
	timeout := time.Duration(setting.TimeoutSeconds) * time.Second
	return argv, timeout, nil
}

// Certificate reads public X.509 metadata from a caller-selected certificate
// file. It does not read or return private-key material.
func (s Service) Certificate(ctx context.Context, path string) (Certificate, qexec.Result, error) {
	if strings.TrimSpace(path) == "" {
		return Certificate{}, qexec.Result{}, errors.New("certificate path is required")
	}
	openssl, err := stdexec.LookPath("openssl")
	if err != nil {
		return Certificate{}, qexec.Result{}, errors.New("openssl utility not found")
	}
	result, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{openssl, "x509", "-in", path, "-noout", "-subject", "-issuer", "-startdate", "-enddate", "-serial", "-fingerprint", "-sha256", "-ext", "subjectAltName"}, Timeout: 20 * time.Second, MaxOutput: s.Exec.MaxOutput})
	if err != nil {
		return Certificate{}, result, err
	}
	certificate := parseCertificate(path, result.Stdout)
	return certificate, result, nil
}

func parseCertificate(path, stdout string) Certificate {
	certificate := Certificate{Path: path}
	lines := strings.Split(stdout, "\n")
	inSAN := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "subject="):
			certificate.Subject = strings.TrimSpace(strings.TrimPrefix(line, "subject="))
			inSAN = false
		case strings.HasPrefix(line, "issuer="):
			certificate.Issuer = strings.TrimSpace(strings.TrimPrefix(line, "issuer="))
			inSAN = false
		case strings.HasPrefix(line, "notBefore="):
			certificate.NotBefore = strings.TrimSpace(strings.TrimPrefix(line, "notBefore="))
			inSAN = false
		case strings.HasPrefix(line, "notAfter="):
			certificate.NotAfter = strings.TrimSpace(strings.TrimPrefix(line, "notAfter="))
			inSAN = false
		case strings.HasPrefix(line, "serial="):
			certificate.Serial = strings.TrimSpace(strings.TrimPrefix(line, "serial="))
			inSAN = false
		case strings.Contains(strings.ToLower(line), "fingerprint="):
			certificate.Fingerprint = strings.TrimSpace(line[strings.Index(strings.ToLower(line), "fingerprint=")+len("fingerprint="):])
			inSAN = false
		case strings.Contains(line, "Subject Alternative Name"):
			inSAN = true
		case inSAN:
			for _, value := range strings.Split(line, ",") {
				if value = strings.TrimSpace(value); value != "" {
					certificate.SAN = append(certificate.SAN, value)
				}
			}
		}
	}
	return certificate
}

// UPS returns the NUT daemon inventory and key/value status for every UPS
// reported by upsc. QTS commonly ships this client even when the daemon is off.
func (s Service) UPS(ctx context.Context) (map[string]any, error) {
	path := executable("upsc")
	if path == "" {
		return map[string]any{"supported": false, "reason": "NUT upsc utility not found"}, nil
	}
	list, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "-l"}, Timeout: 15 * time.Second, MaxOutput: s.Exec.MaxOutput})
	if err != nil {
		return map[string]any{"supported": false, "command": list, "reason": "NUT daemon did not return UPS inventory"}, nil
	}
	ups := []map[string]any{}
	for _, name := range strings.Fields(list.Stdout) {
		result, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{path, name}, Timeout: 15 * time.Second, MaxOutput: s.Exec.MaxOutput})
		item := map[string]any{"name": name, "command": result, "values": parseUPS(result.Stdout)}
		if err != nil {
			item["error"] = err.Error()
		}
		ups = append(ups, item)
	}
	return map[string]any{"supported": true, "ups": ups}, nil
}
func (s Service) Unsupported(name string) (qexec.Result, error) {
	return qexec.Result{}, errors.New(name + " adapter is not available; inspect nas_discovery and use nas_exec after qnap probe")
}
func has(items []string, needle string) bool {
	for _, item := range items {
		if strings.Contains(strings.ToLower(item), needle) {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string][]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func executable(name string) string {
	for _, path := range []string{"/sbin/" + name, "/usr/sbin/" + name, "/bin/" + name, "/usr/bin/" + name} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return ""
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func parseUPS(stdout string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(stdout, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
			values[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return values
}
