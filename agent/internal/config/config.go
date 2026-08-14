package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DefaultPath = "/etc/config/qnap-ai-control-agent/config.json"

type Auth struct {
	Type        string `json:"type"`
	TokenSHA256 string `json:"token_sha256"`
}

type Permissions struct {
	AllowedRoots    []string `json:"allowed_roots"`
	AllowAnyCommand bool     `json:"allow_any_command"`
	AllowedCommands []string `json:"allowed_commands"`
	AllowShell      bool     `json:"allow_shell"`
}

type Privacy struct {
	RedactSecrets bool `json:"redact_secrets"`
}
type Confirmation struct {
	Mode       string `json:"mode"`
	TTLSeconds int    `json:"ttl_seconds"`
}
type Command struct {
	TimeoutSeconds int `json:"timeout_seconds"`
	MaxOutputBytes int `json:"max_output_bytes"`
}
type Files struct {
	MaxInlineBytes int64 `json:"max_inline_bytes"`
}
type Jobs struct {
	MaxHistory int `json:"max_history"`
}
type Audit struct {
	Enabled       bool   `json:"enabled"`
	Path          string `json:"path"`
	RedactSecrets *bool  `json:"redact_secrets,omitempty"`
}

type Config struct {
	Version      int          `json:"version"`
	Listen       string       `json:"listen"`
	Auth         Auth         `json:"auth"`
	Profile      string       `json:"profile"`
	Permissions  Permissions  `json:"permissions"`
	Privacy      Privacy      `json:"privacy"`
	Confirmation Confirmation `json:"confirmation"`
	Command      Command      `json:"command"`
	Files        Files        `json:"files"`
	Jobs         Jobs         `json:"jobs"`
	Audit        Audit        `json:"audit"`
	DockerPaths  []string     `json:"docker_paths,omitempty"`
}

type legacyConfig struct {
	Listen          string   `json:"listen"`
	TokenSHA256     string   `json:"token_sha256"`
	AllowedRoots    []string `json:"allowed_roots"`
	AllowedCommands []string `json:"allowed_commands"`
	DockerPaths     []string `json:"docker_paths"`
	AllowShell      bool     `json:"allow_shell"`
	AuditLog        string   `json:"audit_log"`
	MaxReadBytes    int64    `json:"max_read_bytes"`
	TimeoutSeconds  int      `json:"command_timeout_seconds"`
}

func Defaults() Config {
	return Config{
		Version: 1, Listen: "127.0.0.1:8756", Profile: "observe",
		Auth:         Auth{Type: "bearer"},
		Permissions:  Permissions{AllowedRoots: []string{"/share"}},
		Privacy:      Privacy{RedactSecrets: true},
		Confirmation: Confirmation{Mode: "destructive_only", TTLSeconds: 600},
		Command:      Command{TimeoutSeconds: 30, MaxOutputBytes: 8 * 1024 * 1024},
		Files:        Files{MaxInlineBytes: 4 * 1024 * 1024}, Jobs: Jobs{MaxHistory: 200},
		Audit:       Audit{Enabled: true, Path: "/var/log/qnap-ai-control-agent/audit.jsonl"},
		DockerPaths: defaultDockerPaths(),
	}
}

func FullTrust(tokenHash string) Config {
	cfg := Defaults()
	cfg.Listen = "0.0.0.0:8756"
	cfg.Profile = "full_trust"
	cfg.Auth.TokenSHA256 = tokenHash
	cfg.Permissions = Permissions{AllowedRoots: []string{"/"}, AllowAnyCommand: true, AllowShell: true}
	cfg.Privacy.RedactSecrets = false
	cfg.Confirmation.Mode = "off"
	cfg.Audit.RedactSecrets = boolPtr(false)
	return cfg
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return Config{}, err
	}
	var cfg Config
	if _, ok := raw["permissions"]; ok {
		cfg = Defaults()
		if err := json.Unmarshal(b, &cfg); err != nil {
			return Config{}, err
		}
	} else {
		var old legacyConfig
		if err := json.Unmarshal(b, &old); err != nil {
			return Config{}, err
		}
		cfg = migrateLegacy(old)
	}
	return Normalize(cfg)
}

func Normalize(cfg Config) (Config, error) {
	defaults := Defaults()
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Listen == "" {
		cfg.Listen = defaults.Listen
	}
	if cfg.Auth.Type == "" {
		cfg.Auth.Type = "bearer"
	}
	if cfg.Auth.Type != "bearer" {
		return cfg, errors.New("auth.type must be bearer")
	}
	if strings.TrimSpace(cfg.Auth.TokenSHA256) == "" {
		return cfg, errors.New("auth.token_sha256 is required")
	}
	if cfg.Profile == "" {
		cfg.Profile = "observe"
	}
	switch cfg.Profile {
	case "observe", "operate", "admin", "full_trust":
	default:
		return cfg, errors.New("profile must be observe, operate, admin, or full_trust")
	}
	if cfg.Profile == "full_trust" {
		cfg.Permissions.AllowedRoots = []string{"/"}
		cfg.Permissions.AllowAnyCommand = true
		cfg.Permissions.AllowShell = true
		cfg.Privacy.RedactSecrets = false
		cfg.Confirmation.Mode = "off"
	}
	cfg.Permissions.AllowedRoots = cleanPaths(cfg.Permissions.AllowedRoots)
	if len(cfg.Permissions.AllowedRoots) == 0 {
		return cfg, errors.New("permissions.allowed_roots must not be empty")
	}
	cfg.Permissions.AllowedCommands = cleanPaths(cfg.Permissions.AllowedCommands)
	if cfg.Confirmation.Mode == "" {
		cfg.Confirmation.Mode = defaults.Confirmation.Mode
	}
	switch cfg.Confirmation.Mode {
	case "off", "destructive_only", "all_write":
	default:
		return cfg, errors.New("confirmation.mode must be off, destructive_only, or all_write")
	}
	if cfg.Confirmation.TTLSeconds <= 0 {
		cfg.Confirmation.TTLSeconds = defaults.Confirmation.TTLSeconds
	}
	if cfg.Command.TimeoutSeconds <= 0 {
		cfg.Command.TimeoutSeconds = defaults.Command.TimeoutSeconds
	}
	if cfg.Command.MaxOutputBytes <= 0 {
		cfg.Command.MaxOutputBytes = defaults.Command.MaxOutputBytes
	}
	if cfg.Files.MaxInlineBytes <= 0 {
		cfg.Files.MaxInlineBytes = defaults.Files.MaxInlineBytes
	}
	if cfg.Jobs.MaxHistory <= 0 {
		cfg.Jobs.MaxHistory = defaults.Jobs.MaxHistory
	}
	if cfg.Audit.Path == "" {
		cfg.Audit.Path = defaults.Audit.Path
	}
	if cfg.Audit.RedactSecrets == nil {
		cfg.Audit.RedactSecrets = boolPtr(cfg.Privacy.RedactSecrets)
	}
	cfg.DockerPaths = cleanPaths(cfg.DockerPaths)
	if len(cfg.DockerPaths) == 0 {
		cfg.DockerPaths = defaults.DockerPaths
	}
	return cfg, nil
}

func (c Config) Timeout() time.Duration { return time.Duration(c.Command.TimeoutSeconds) * time.Second }

func migrateLegacy(old legacyConfig) Config {
	cfg := Defaults()
	cfg.Listen, cfg.Auth.TokenSHA256 = old.Listen, old.TokenSHA256
	cfg.Permissions.AllowedRoots, cfg.Permissions.AllowedCommands = old.AllowedRoots, old.AllowedCommands
	cfg.Permissions.AllowShell, cfg.DockerPaths = old.AllowShell, old.DockerPaths
	cfg.Audit.Path, cfg.Files.MaxInlineBytes = old.AuditLog, old.MaxReadBytes
	cfg.Command.TimeoutSeconds = old.TimeoutSeconds
	if old.AllowShell {
		cfg.Profile = "admin"
	}
	return cfg
}

func cleanPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if !filepath.IsAbs(path) || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}
func boolPtr(v bool) *bool { return &v }
func defaultDockerPaths() []string {
	return []string{"/share/CACHEDEV1_DATA/.qpkg/container-station/bin/docker", "/share/CACHEDEV1_DATA/.qpkg/container-station/usr/bin/docker", "/share/CACHEDEV2_DATA/.qpkg/container-station/bin/docker", "/share/CACHEDEV2_DATA/.qpkg/container-station/usr/bin/docker", "/share/CACHEDEV3_DATA/.qpkg/container-station/bin/docker", "/share/CACHEDEV3_DATA/.qpkg/container-station/usr/bin/docker", "/share/CACHEDEV4_DATA/.qpkg/container-station/bin/docker", "/share/CACHEDEV4_DATA/.qpkg/container-station/usr/bin/docker", "/share/CACHEDEV5_DATA/.qpkg/container-station/bin/docker", "/share/CACHEDEV5_DATA/.qpkg/container-station/usr/bin/docker", "/usr/bin/docker", "/usr/local/bin/docker", "/bin/docker"}
}
