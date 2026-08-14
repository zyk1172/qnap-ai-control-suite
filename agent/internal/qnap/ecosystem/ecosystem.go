package ecosystem

import (
	"context"
	"errors"
	"os"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"qnap-ai-control-suite/agent/internal/qnap/discovery"
	"strings"
	"time"
)

type Service struct {
	Discovery discovery.Service
	Exec      qexec.Executor
}
type Adapter struct {
	Name         string   `json:"name"`
	Installed    bool     `json:"installed"`
	Supported    bool     `json:"supported"`
	Reason       string   `json:"reason,omitempty"`
	Capabilities []string `json:"capabilities"`
}

func (s Service) Inventory(ctx context.Context) []Adapter {
	d := s.Discovery.Discover(ctx)
	return []Adapter{{Name: "virtualization_station", Installed: has(d.QPKGs, "virtualization") || has(d.QPKGs, "qkvm"), Supported: false, Reason: "QKVM/Virtualization Station detected; stable VM control requires NAS runtime probe", Capabilities: []string{"list", "info", "start", "stop", "restart", "snapshot", "clone"}}, {Name: "hbs3", Installed: has(d.QPKGs, "hybrid backup") || has(d.QPKGs, "hybridbackup") || has(d.QPKGs, "hbs"), Supported: false, Reason: "HBS package detected; stable job control requires NAS runtime probe", Capabilities: []string{"job_list", "job_status", "run", "stop", "logs"}}, {Name: "iscsi", Installed: d.Features["iscsi"].Supported, Supported: false, Reason: "stable iSCSI/LUN adapter requires NAS runtime probe", Capabilities: []string{"targets", "luns", "mapping", "snapshots"}}, {Name: "certificates", Installed: true, Supported: false, Reason: "certificate paths require NAS runtime probe", Capabilities: []string{"list", "expiry", "issuer", "subject", "san", "import", "replace"}}, {Name: "ups", Installed: d.Features["ups"].Supported, Supported: d.Features["ups"].Supported, Reason: d.Features["ups"].Reason, Capabilities: []string{"state", "battery", "runtime", "input", "configuration"}}}
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

func executable(name string) string {
	for _, path := range []string{"/sbin/" + name, "/usr/sbin/" + name, "/bin/" + name, "/usr/bin/" + name} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return ""
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
