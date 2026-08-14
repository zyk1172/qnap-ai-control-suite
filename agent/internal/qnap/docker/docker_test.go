package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryFindsExecutableContainerStationWrapper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "system-docker")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := (Service{Paths: []string{path}}).Binary()
	if err != nil || got != path {
		t.Fatalf("got=%q err=%v", got, err)
	}
}
