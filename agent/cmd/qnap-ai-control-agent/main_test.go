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
