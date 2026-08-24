package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigratesLegacy032(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	data := `{"listen":"0.0.0.0:8756","token_sha256":"abc","allowed_roots":["/share"],"allowed_commands":["/bin/echo"],"allow_shell":false,"audit_log":"/tmp/audit","max_read_bytes":1024,"command_timeout_seconds":9}`
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 2 || cfg.Auth.TokenSHA256 != "abc" || cfg.Files.MaxInlineBytes != 1024 || cfg.Command.TimeoutSeconds != 9 || cfg.Profile != "full_trust" || !cfg.Permissions.AllowAnyCommand || !cfg.Permissions.AllowShell || cfg.Approval.Mode != "sensitive_only" {
		t.Fatalf("migration failed: %+v", cfg)
	}
}
func TestFullTrustNormalizes(t *testing.T) {
	cfg, err := Normalize(Config{Auth: Auth{TokenSHA256: "abc"}, Profile: "full_trust"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Permissions.AllowAnyCommand || !cfg.Permissions.AllowShell || cfg.Approval.Mode != "sensitive_only" || cfg.Audit.RedactSecrets == nil || !*cfg.Audit.RedactSecrets || len(cfg.Permissions.AllowedRoots) != 1 || cfg.Permissions.AllowedRoots[0] != "/" {
		t.Fatalf("unexpected full trust: %+v", cfg)
	}
}

func TestV1ConfirmationOffMigratesToSensitiveOnlyApproval(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	data := `{"version":1,"auth":{"type":"bearer","token_sha256":"abc"},"profile":"full_trust","permissions":{"allowed_roots":["/"],"allow_any_command":true,"allow_shell":true},"confirmation":{"mode":"off","ttl_seconds":120}}`
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 2 || cfg.Approval.Mode != "sensitive_only" || cfg.Approval.TTLSeconds != 120 || cfg.Audit.RedactSecrets == nil || !*cfg.Audit.RedactSecrets {
		t.Fatalf("unexpected v1 migration: %#v", cfg)
	}
}

func TestApprovalAndJobDefaultsAreIndependent(t *testing.T) {
	cfg, err := Normalize(Config{Auth: Auth{TokenSHA256: "abc"}, Permissions: Permissions{AllowedRoots: []string{"/share"}}, Approval: Approval{Mode: "all_write", TTLSeconds: 30}, Jobs: Jobs{MaxHistory: 1, MaxConcurrent: 2, JournalPath: "/tmp/jobs.jsonl"}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Approval.Mode != "all_write" || cfg.Jobs.MaxConcurrent != 2 || cfg.Jobs.JournalPath != "/tmp/jobs.jsonl" {
		t.Fatalf("defaults overwrote explicit policy: %#v", cfg)
	}
}
func TestRejectsInvalidProfile(t *testing.T) {
	_, err := Normalize(Config{Auth: Auth{TokenSHA256: "abc"}, Profile: "unsafe"})
	if err == nil {
		t.Fatal("expected invalid profile error")
	}
}

func TestNormalizeKeepsCustomDockerPathAndAddsNewDefaults(t *testing.T) {
	cfg, err := Normalize(Config{Auth: Auth{TokenSHA256: "abc"}, Permissions: Permissions{AllowedRoots: []string{"/share"}}, DockerPaths: []string{"/custom/docker"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DockerPaths) < 2 || cfg.DockerPaths[0] != "/custom/docker" {
		t.Fatalf("custom path was not preserved: %#v", cfg.DockerPaths)
	}
	found := false
	for _, path := range cfg.DockerPaths {
		if path == "/share/CACHEDEV5_DATA/.qpkg/container-station/bin/system-docker" {
			found = true
		}
	}
	if !found {
		t.Fatalf("new Container Station default missing: %#v", cfg.DockerPaths)
	}
}

func TestNormalizeAcceptsVerifiedQNAPAdapter(t *testing.T) {
	cfg, err := Normalize(Config{
		Auth:        Auth{TokenSHA256: "abc"},
		Permissions: Permissions{AllowedRoots: []string{"/share"}},
		QNAPAdapters: map[string]QNAPAdapter{
			"hbs3":            {Commands: map[string][]string{"job_status": {"/share/CACHEDEV1_DATA/.qpkg/HBS3/bin/hbs", "status", "{id}"}}},
			"shares":          {Commands: map[string][]string{"create": {"/share/CACHEDEV1_DATA/.qpkg/share-tool", "create", "{name}"}}},
			"virtual_switch":  {Commands: map[string][]string{"list": {"/share/CACHEDEV1_DATA/.qpkg/network/bin/switch", "list"}}},
			"system_settings": {Commands: map[string][]string{"hostname": {"/share/CACHEDEV1_DATA/.qpkg/system/bin/settings", "hostname", "{name}"}}},
			"firmware":        {Commands: map[string][]string{"info": {"/share/CACHEDEV1_DATA/.qpkg/firmware/bin/fw", "info"}}},
			"notifications":   {Commands: map[string][]string{"test": {"/share/CACHEDEV1_DATA/.qpkg/notify/bin/notify", "test", "{target}"}}},
			"storage_manager": {Commands: map[string][]string{"pools": {"/share/CACHEDEV1_DATA/.qpkg/storage/bin/manager", "pools"}}},
		},
	})
	if err != nil || len(cfg.QNAPAdapters["hbs3"].Commands) != 1 || len(cfg.QNAPAdapters["shares"].Commands) != 1 || len(cfg.QNAPAdapters["virtual_switch"].Commands) != 1 || len(cfg.QNAPAdapters["system_settings"].Commands) != 1 || len(cfg.QNAPAdapters["firmware"].Commands) != 1 || len(cfg.QNAPAdapters["notifications"].Commands) != 1 || len(cfg.QNAPAdapters["storage_manager"].Commands) != 1 {
		t.Fatalf("adapter normalization failed: %#v, %v", cfg.QNAPAdapters, err)
	}
}

func TestNormalizeRejectsRelativeQNAPAdapterBinary(t *testing.T) {
	_, err := Normalize(Config{
		Auth:        Auth{TokenSHA256: "abc"},
		Permissions: Permissions{AllowedRoots: []string{"/share"}},
		QNAPAdapters: map[string]QNAPAdapter{
			"hbs3": {Commands: map[string][]string{"job_list": {"hbs", "list"}}},
		},
	})
	if err == nil {
		t.Fatal("expected relative adapter command rejection")
	}
}
