package main

import (
	"os"
	"path/filepath"
	"testing"

	"qnap-ai-control-suite/agent/internal/auth"
	"qnap-ai-control-suite/agent/internal/config"
)

func TestTokenHashDeterministic(t *testing.T) {
	if hashToken("token") != hashToken("token") || hashToken("token") == hashToken("other") {
		t.Fatal("token hash is not deterministic")
	}
}

func TestResetPersistedTokenRotatesHashAndPlaintext(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	oldToken := "old-token-with-enough-length-123456"
	cfg := config.FullTrust(auth.HashToken(oldToken))
	if err := config.SaveAtomic(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	newToken, err := resetPersistedToken(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if newToken == oldToken || len(newToken) != 64 {
		t.Fatalf("unexpected reset token length/value: %d", len(newToken))
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Auth.TokenSHA256 != auth.HashToken(newToken) {
		t.Fatalf("hash=%q", loaded.Auth.TokenSHA256)
	}
	stored, err := os.ReadFile(filepath.Join(dir, auth.TokenFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != newToken+"\n" {
		t.Fatalf("stored token does not match reset result")
	}
}
