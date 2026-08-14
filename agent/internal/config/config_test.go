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
	if cfg.Version != 1 || cfg.Auth.TokenSHA256 != "abc" || cfg.Files.MaxInlineBytes != 1024 || cfg.Command.TimeoutSeconds != 9 {
		t.Fatalf("migration failed: %+v", cfg)
	}
}
func TestFullTrustNormalizes(t *testing.T) {
	cfg, err := Normalize(Config{Auth: Auth{TokenSHA256: "abc"}, Profile: "full_trust"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Permissions.AllowAnyCommand || !cfg.Permissions.AllowShell || cfg.Privacy.RedactSecrets || cfg.Confirmation.Mode != "off" || len(cfg.Permissions.AllowedRoots) != 1 || cfg.Permissions.AllowedRoots[0] != "/" {
		t.Fatalf("unexpected full trust: %+v", cfg)
	}
}
func TestRejectsInvalidProfile(t *testing.T) {
	_, err := Normalize(Config{Auth: Auth{TokenSHA256: "abc"}, Profile: "unsafe"})
	if err == nil {
		t.Fatal("expected invalid profile error")
	}
}
