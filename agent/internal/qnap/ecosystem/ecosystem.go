package ecosystem

import (
	"context"
	"errors"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"qnap-ai-control-suite/agent/internal/qnap/discovery"
	"strings"
)

type Service struct{ Discovery discovery.Service }
type Adapter struct {
	Name         string   `json:"name"`
	Installed    bool     `json:"installed"`
	Supported    bool     `json:"supported"`
	Reason       string   `json:"reason,omitempty"`
	Capabilities []string `json:"capabilities"`
}

func (s Service) Inventory(ctx context.Context) []Adapter {
	d := s.Discovery.Discover(ctx)
	return []Adapter{{Name: "virtualization_station", Installed: has(d.QPKGs, "virtualization"), Supported: false, Reason: "stable local VM adapter requires NAS runtime probe", Capabilities: []string{"list", "info", "start", "stop", "restart", "snapshot", "clone"}}, {Name: "hbs3", Installed: has(d.QPKGs, "hybrid backup") || has(d.QPKGs, "hbs"), Supported: false, Reason: "stable HBS job API requires NAS runtime probe", Capabilities: []string{"job_list", "job_status", "run", "stop", "logs"}}, {Name: "iscsi", Installed: d.Features["iscsi"].Supported, Supported: false, Reason: "stable iSCSI/LUN adapter requires NAS runtime probe", Capabilities: []string{"targets", "luns", "mapping", "snapshots"}}, {Name: "certificates", Installed: true, Supported: false, Reason: "certificate paths require NAS runtime probe", Capabilities: []string{"list", "expiry", "issuer", "subject", "san", "import", "replace"}}, {Name: "ups", Installed: d.Features["ups"].Supported, Supported: d.Features["ups"].Supported, Reason: d.Features["ups"].Reason, Capabilities: []string{"state", "battery", "runtime", "input", "configuration"}}}
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
