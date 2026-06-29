package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathWithinRoot(t *testing.T) {
	tests := []struct {
		name string
		path string
		root string
		want bool
	}{
		{name: "root itself", path: "/share", root: "/share", want: true},
		{name: "child path", path: "/share/CACHEDEV1_DATA/media", root: "/share", want: true},
		{name: "prefix is not child", path: "/shareevil/file", root: "/share", want: false},
		{name: "parent escape", path: "/etc/passwd", root: "/share", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathWithinRoot(tt.path, tt.root); got != tt.want {
				t.Fatalf("pathWithinRoot(%q, %q) = %v, want %v", tt.path, tt.root, got, tt.want)
			}
		})
	}
}

func TestShellRejectedWhenDisabled(t *testing.T) {
	s := &Server{cfg: Config{
		AllowedCommands: []string{"/bin/sh"},
		AllowShell:      false,
		TimeoutSeconds:  1,
		CommandTimeout:  1,
	}}
	_, err := s.runAllowedCommand(commandRequest{Argv: []string{"/bin/sh", "-c", "echo unsafe"}})
	if err == nil {
		t.Fatal("expected shell command to be rejected")
	}
}

func TestCommandAllowList(t *testing.T) {
	s := &Server{cfg: Config{
		AllowedCommands: []string{"/bin/echo"},
		AllowShell:      false,
		TimeoutSeconds:  1,
		CommandTimeout:  1,
	}}
	_, err := s.runAllowedCommand(commandRequest{Argv: []string{"/bin/rm", "-rf", "/tmp/example"}, DryRun: true})
	if err == nil {
		t.Fatal("expected non-allowlisted command to be rejected")
	}
}

func TestDockerNameValidation(t *testing.T) {
	valid := []string{"moviepilot", "abc123", "container.name", "sha256:abcd", "name_1-2"}
	for _, name := range valid {
		if err := validateDockerName(name); err != nil {
			t.Fatalf("validateDockerName(%q) returned error: %v", name, err)
		}
	}
	invalid := []string{"", "movie pilot", "moviepilot;reboot", "$(id)", "a/b"}
	for _, name := range invalid {
		if err := validateDockerName(name); err == nil {
			t.Fatalf("validateDockerName(%q) succeeded, want error", name)
		}
	}
}

func TestDockerTailBounds(t *testing.T) {
	if got := normalizedDockerTail(0); got != 200 {
		t.Fatalf("default tail = %d, want 200", got)
	}
	if got := normalizedDockerTail(3000); got != 2000 {
		t.Fatalf("capped tail = %d, want 2000", got)
	}
	if got := normalizedDockerTail(42); got != 42 {
		t.Fatalf("tail = %d, want 42", got)
	}
}

func TestDockerRiskClassification(t *testing.T) {
	risky := []struct {
		sub  string
		args []string
	}{
		{sub: "rm", args: []string{"container"}},
		{sub: "rmi", args: []string{"image"}},
		{sub: "volume", args: []string{"rm", "data"}},
		{sub: "network", args: []string{"prune", "-f"}},
		{sub: "compose", args: []string{"-f", "compose.yml", "down"}},
		{sub: "system", args: []string{"prune", "-a"}},
	}
	for _, tt := range risky {
		if !isDockerDestroyCommand(tt.sub, tt.args) {
			t.Fatalf("expected docker %s %v to be classified as destructive", tt.sub, tt.args)
		}
	}
	if isDockerDestroyCommand("compose", []string{"-f", "compose.yml", "up", "-d"}) {
		t.Fatal("compose up should not be classified as destructive")
	}
	if isDockerDestroyCommand("volume", []string{"ls"}) {
		t.Fatal("volume ls should not be classified as destructive")
	}
}

func TestQpkgInstallRemoveClassification(t *testing.T) {
	for _, action := range []string{"add", "install_file", "install_url", "remove", "update_all"} {
		if !isQpkgInstallRemoveAction(action) {
			t.Fatalf("expected %s to require confirmation", action)
		}
	}
	for _, action := range []string{"start", "stop", "restart", "enable", "disable", "status"} {
		if isQpkgInstallRemoveAction(action) {
			t.Fatalf("expected %s to run without confirmation", action)
		}
	}
}

func TestAllowedDockerSubcommands(t *testing.T) {
	for _, sub := range []string{"run", "exec", "pull", "compose", "network", "volume", "system"} {
		if !allowedDockerSubcommand(sub) {
			t.Fatalf("expected docker subcommand %s to be allowed", sub)
		}
	}
	if allowedDockerSubcommand("attach") {
		t.Fatal("attach should not be allowed")
	}
}

func TestSummarizeDockerInspect(t *testing.T) {
	raw := `[
	  {
	    "Id":"abc123",
	    "Name":"/web",
	    "Created":"2026-06-29T12:00:00Z",
	    "Path":"/entrypoint",
	    "Args":["serve","--port","80"],
	    "Image":"sha256:imageid",
	    "State":{"Status":"running","StartedAt":"2026-06-29T12:01:00Z"},
	    "Config":{"Image":"nginx:latest"},
	    "NetworkSettings":{"Ports":{"80/tcp":[{"HostIp":"0.0.0.0","HostPort":"8080"}]}}
	  }
	]`
	items := summarizeDockerInspect(parseJSONArray(raw))
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	got := items[0].(map[string]any)
	if got["Names"] != "web" {
		t.Fatalf("Names = %v, want web", got["Names"])
	}
	if got["Image"] != "nginx:latest" {
		t.Fatalf("Image = %v, want nginx:latest", got["Image"])
	}
	if got["Command"] != "/entrypoint serve --port 80" {
		t.Fatalf("Command = %v", got["Command"])
	}
	if got["Ports"] != "0.0.0.0:8080->80/tcp" {
		t.Fatalf("Ports = %v", got["Ports"])
	}
}

func TestRedactDockerInspectEnv(t *testing.T) {
	raw := `[
	  {
	    "Config": {
	      "Env": [
	        "NORMAL=value",
	        "API_SERVER_KEY=abc",
	        "PASSWORD=secret",
	        "ACCESS_TOKEN=token"
	      ],
	      "Secret": "plain"
	    }
	  }
	]`
	redacted := redactDockerInspect(parseJSONOrRaw(raw)).([]any)[0].(map[string]any)
	config := redacted["Config"].(map[string]any)
	env := config["Env"].([]any)
	if env[0] != "NORMAL=value" {
		t.Fatalf("normal env redacted unexpectedly: %#v", env)
	}
	for _, got := range env[1:] {
		if !strings.Contains(got.(string), "[redacted]") {
			t.Fatalf("sensitive env was not redacted: %#v", env)
		}
	}
	if config["Secret"] != "[redacted]" {
		t.Fatalf("Secret field = %#v, want redacted", config["Secret"])
	}
}

func TestParseQpkgConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qpkg.conf")
	write := `[QnapAIControl]
Name = QnapAIControl
Enable = TRUE
Version = 0.3.0

[container-station]
Name = Container Station
Enable = TRUE
`
	if err := os.WriteFile(path, []byte(write), 0644); err != nil {
		t.Fatal(err)
	}
	packages, err := parseQpkgConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 2 {
		t.Fatalf("len(packages) = %d, want 2", len(packages))
	}
	if packages[0]["name"] != "QnapAIControl" || packages[0]["Version"] != "0.3.0" {
		t.Fatalf("unexpected first package: %#v", packages[0])
	}
	if packages[1]["Name"] != "Container Station" {
		t.Fatalf("unexpected second package: %#v", packages[1])
	}
}
