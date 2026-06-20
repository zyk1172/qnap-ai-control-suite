package main

import "testing"

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
