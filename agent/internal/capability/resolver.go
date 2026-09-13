package capability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"qnap-ai-control-suite/agent/internal/config"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"qnap-ai-control-suite/agent/internal/qnap/discovery"
)

type Resolver struct {
	Discovery      discovery.Service
	Exec           qexec.Executor
	Adapters       map[string]config.QNAPAdapter
	FindExecutable func(string) string
	Verify         func(context.Context, []string) error
	Now            func() time.Time
}

type adapterSpec struct {
	Domain     string
	Actions    []string
	Persistent bool
	Installed  func(discovery.Result) bool
}

var adapterSpecs = map[string]adapterSpec{
	"virtualization_station": {Domain: "virtualization", Actions: []string{"list", "info", "start", "stop", "restart", "force_stop", "snapshot", "clone"}, Persistent: true, Installed: func(d discovery.Result) bool { return hasQPKG(d.QPKGs, "virtualizationstation", "virtualization station", "qkvm") }},
	"hbs3":                  {Domain: "hbs", Actions: []string{"job_list", "job_status", "run", "stop", "logs"}, Persistent: true, Installed: func(d discovery.Result) bool { return hasQPKG(d.QPKGs, "hybrid backup", "hybridbackup", "hbs") }},
	"iscsi":                 {Domain: "iscsi", Actions: []string{"targets", "luns", "mapping", "status", "snapshots", "online", "offline", "expand", "clone"}, Persistent: true, Installed: func(d discovery.Result) bool { return d.Features["iscsi"].Supported }},
	"certificates":          {Domain: "certificates", Actions: []string{"list", "current", "expiry", "issuer", "subject", "san", "import", "replace"}, Persistent: true, Installed: func(d discovery.Result) bool { return d.Platform == "qts" || d.Platform == "quts_hero" }},
	"shares":                {Domain: "shares", Actions: []string{"create", "delete", "rename", "set_path", "quota", "hidden", "recycle_bin", "nfs_export"}, Persistent: true, Installed: func(d discovery.Result) bool { return d.Platform == "qts" || d.Platform == "quts_hero" }},
	"virtual_switch":        {Domain: "network.virtual_switch", Actions: []string{"list", "info", "create", "delete", "configure", "vlan", "bond", "bridge"}, Persistent: true, Installed: func(d discovery.Result) bool { return d.Features["virtual_switch"].Supported }},
	"system_settings":       {Domain: "system.settings", Actions: []string{"info", "hostname", "timezone", "ntp", "service"}, Persistent: true, Installed: func(d discovery.Result) bool { return d.Platform == "qts" || d.Platform == "quts_hero" }},
	"firmware":              {Domain: "system.firmware", Actions: []string{"info", "check", "download", "install"}, Persistent: true, Installed: func(d discovery.Result) bool { return d.Platform == "qts" || d.Platform == "quts_hero" }},
	"notifications":         {Domain: "system.notifications", Actions: []string{"list", "history", "test", "configure"}, Persistent: true, Installed: func(d discovery.Result) bool { return d.Platform == "qts" || d.Platform == "quts_hero" }},
	"storage_manager":       {Domain: "storage.manager", Actions: []string{"pools", "volumes", "snapshots", "create", "delete", "expand", "restore", "schedule"}, Persistent: true, Installed: func(d discovery.Result) bool { return d.Platform == "qts" || d.Platform == "quts_hero" }},
}

func (r Resolver) Resolve(ctx context.Context) Manifest {
	return r.ResolveResult(ctx, r.Discovery.Discover(ctx))
}

func (r Resolver) ResolveResult(ctx context.Context, d discovery.Result) Manifest {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	manifest := Manifest{
		SchemaVersion: 1,
		GeneratedAt:   now().UTC(),
		Fingerprint:   fingerprint(d),
		Device:        Device{Model: d.Model, Firmware: d.Firmware, Platform: d.Platform, Arch: d.Arch},
	}

	add := func(item Capability) {
		manifest.Capabilities = append(manifest.Capabilities, item)
	}

	add(Capability{ID: "system.discovery", Domain: "system", Action: "discovery", Status: Available, Backend: "builtin", Provider: "qnap_discovery", Verified: true, ReadOnly: true, Reason: "runtime discovery is implemented by the agent"})
	if d.QPKGConfig {
		add(Capability{ID: "qpkg.list", Domain: "qpkg", Action: "list", Status: Available, Backend: "qnap_config", Provider: "qpkg.conf", Verified: true, ReadOnly: true, Persistent: true, Reason: "QPKG inventory file detected"})
	} else {
		add(Capability{ID: "qpkg.list", Domain: "qpkg", Action: "list", Status: Unavailable, Reason: "QPKG inventory file not detected"})
	}

	if path := r.executable(d, "qpkg_cli"); path != "" {
		item := Capability{ID: "qpkg.manage", Domain: "qpkg", Action: "manage", Status: Degraded, Backend: "qnap_cli", Provider: path, Persistent: true, Evidence: []string{"executable:" + path}, Reason: "qpkg_cli discovered but read-only verification failed"}
		if err := r.verify(ctx, []string{path, "-l"}); err == nil {
			item.Status, item.Verified, item.Reason = Available, true, "qpkg_cli inventory probe succeeded"
		} else {
			item.Reason = "qpkg_cli discovered; verification failed: " + err.Error()
		}
		add(item)
	} else {
		add(Capability{ID: "qpkg.manage", Domain: "qpkg", Action: "manage", Status: Unavailable, Persistent: true, Reason: "qpkg_cli executable not found"})
	}

	if path := r.executable(d, "docker"); path != "" {
		add(Capability{ID: "container.docker", Domain: "container", Action: "docker", Status: Available, Backend: "docker_cli", Provider: path, Verified: true, Evidence: []string{"executable:" + path}, Reason: "Docker CLI discovered by runtime discovery"})
	} else {
		add(Capability{ID: "container.docker", Domain: "container", Action: "docker", Status: Unavailable, Reason: "Docker CLI not found"})
	}

	if path := r.executable(d, "smartctl"); path != "" {
		add(Capability{ID: "storage.smart", Domain: "storage", Action: "smart", Status: Available, Backend: "smartctl", Provider: path, Verified: true, ReadOnly: false, Evidence: []string{"executable:" + path}, Reason: "smartctl discovered"})
	}
	if path := r.executable(d, "ip"); path != "" {
		add(Capability{ID: "network.transient", Domain: "network", Action: "transient", Status: Available, Backend: "linux_iproute2", Provider: path, Verified: true, Persistent: false, Evidence: []string{"executable:" + path}, Reason: "Linux ip utility discovered; changes are not QTS-persistent"})
	}
	if d.Features["ups"].Supported {
		add(Capability{ID: "ups.read", Domain: "ups", Action: "read", Status: Available, Backend: "nut", Provider: r.executable(d, "upsc"), Verified: true, ReadOnly: true, Reason: d.Features["ups"].Reason, Evidence: d.Features["ups"].Evidence})
	}

	for name, spec := range adapterSpecs {
		installed := spec.Installed(d)
		configured := r.Adapters[name]
		for _, action := range spec.Actions {
			id := spec.Domain + "." + action
			item := Capability{ID: id, Domain: spec.Domain, Action: action, Adapter: name, Persistent: spec.Persistent}
			if command, ok := configured.Commands[action]; ok && len(command) > 0 {
				item.Status = Available
				item.Backend = "manual_argv"
				item.Provider = "qnap_adapters"
				item.Verified = true
				item.Reason = "explicit verified qnap_adapters override configured"
				item.Evidence = []string{"config:qnap_adapters." + name + ".commands." + action}
			} else if installed {
				item.Status = Degraded
				item.Reason = "QNAP subsystem detected but no verified backend is bound for this action"
			} else {
				item.Status = Unavailable
				item.Reason = "QNAP subsystem was not detected"
			}
			add(item)
		}
	}

	r.bindStorageManager(ctx, d, &manifest)
	sort.Slice(manifest.Capabilities, func(i, j int) bool { return manifest.Capabilities[i].ID < manifest.Capabilities[j].ID })
	return manifest
}

func (r Resolver) bindStorageManager(ctx context.Context, d discovery.Result, manifest *Manifest) {
	// A configured adapter is authoritative for the whole adapter. This keeps
	// capability reporting aligned with Service.Command, which historically
	// rejects actions missing from an explicitly configured command map.
	if configured := r.Adapters["storage_manager"]; len(configured.Commands) > 0 {
		return
	}
	path := r.executable(d, "qcli_storage")
	if path == "" {
		return
	}
	for action, flag := range map[string]string{"pools": "-p", "volumes": "-v"} {
		id := "storage.manager." + action
		for index := range manifest.Capabilities {
			if manifest.Capabilities[index].ID != id || manifest.Capabilities[index].Status == Available {
				continue
			}
			item := &manifest.Capabilities[index]
			item.Backend = "qnap_cli"
			item.Provider = path
			item.Evidence = append(item.Evidence, "executable:"+path)
			if err := r.verify(ctx, []string{path, flag}); err != nil {
				item.Status = Degraded
				item.Reason = "qcli_storage discovered; read-only verification failed: " + err.Error()
				continue
			}
			item.Status = Available
			item.Verified = true
			item.ReadOnly = true
			item.Reason = "qcli_storage read-only probe succeeded"
		}
	}
}

func (r Resolver) executable(d discovery.Result, name string) string {
	if path := strings.TrimSpace(d.Utilities[name]); path != "" {
		return path
	}
	if r.FindExecutable != nil {
		return strings.TrimSpace(r.FindExecutable(name))
	}
	for _, root := range []string{"/sbin", "/usr/sbin", "/bin", "/usr/bin"} {
		path := filepath.Join(root, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return ""
}

func (r Resolver) verify(ctx context.Context, argv []string) error {
	if r.Verify != nil {
		return r.Verify(ctx, argv)
	}
	if len(argv) == 0 {
		return fmt.Errorf("empty verification command")
	}
	_, err := r.Exec.Run(ctx, qexec.Request{Argv: argv, Timeout: 15 * time.Second, MaxOutput: 2 * 1024 * 1024})
	return err
}

func fingerprint(d discovery.Result) string {
	qpkg := append([]string(nil), d.QPKGs...)
	sort.Strings(qpkg)
	utilities := make([]string, 0, len(d.Utilities))
	for name, path := range d.Utilities {
		utilities = append(utilities, name+"="+path)
	}
	sort.Strings(utilities)
	payload := struct {
		Model     string   `json:"model"`
		Firmware  string   `json:"firmware"`
		Platform  string   `json:"platform"`
		Arch      string   `json:"arch"`
		QPKGs     []string `json:"qpkgs"`
		Utilities []string `json:"utilities"`
	}{d.Model, d.Firmware, d.Platform, d.Arch, qpkg, utilities}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func hasQPKG(items []string, needles ...string) bool {
	for _, item := range items {
		lower := strings.ToLower(item)
		for _, needle := range needles {
			if strings.Contains(lower, strings.ToLower(needle)) {
				return true
			}
		}
	}
	return false
}
