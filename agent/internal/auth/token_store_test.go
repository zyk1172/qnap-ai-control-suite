package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qnap-ai-control-suite/agent/internal/config"
)

func testConfig(path, token string) config.Config {
	cfg := config.FullTrust(HashToken(token))
	cfg.Audit.Path = filepath.Join(filepath.Dir(path), "audit.jsonl")
	cfg.Jobs.JournalPath = filepath.Join(filepath.Dir(path), "jobs.jsonl")
	return cfg
}

func TestTokenStoreMigratesInitialToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	token := "legacy-token-with-enough-length-123456"
	legacyPath := filepath.Join(dir, InitialTokenFileName)
	if err := os.WriteFile(legacyPath, []byte(token+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(path, "different-token-with-enough-length-654321")
	store := NewTokenStore(path)
	metadata, err := store.Ensure(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.Configured || !metadata.Recoverable || metadata.Masked != MaskToken(token) {
		t.Fatalf("metadata=%+v", metadata)
	}
	if got, err := store.Read(); err != nil || got != token {
		t.Fatalf("migrated token=%q err=%v", got, err)
	}
	if cfg.Auth.TokenSHA256 != HashToken(token) {
		t.Fatalf("cfg hash=%q", cfg.Auth.TokenSHA256)
	}
	info, err := os.Stat(filepath.Join(dir, TokenFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("token mode=%o", info.Mode().Perm())
	}
	legacyInfo, err := os.Stat(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if legacyInfo.Mode().Perm() != 0600 {
		t.Fatalf("legacy token mode=%o", legacyInfo.Mode().Perm())
	}
	directoryInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if directoryInfo.Mode().Perm() != 0700 {
		t.Fatalf("token directory mode=%o", directoryInfo.Mode().Perm())
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Auth.TokenSHA256 != HashToken(token) {
		t.Fatalf("persisted hash=%q", loaded.Auth.TokenSHA256)
	}
}

func TestTokenStorePreservesExistingToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	token := "existing-token-with-enough-length-123456"
	if err := os.WriteFile(filepath.Join(dir, TokenFileName), []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(path, token)
	store := NewTokenStore(path)
	if _, err := store.Ensure(&cfg); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Read(); err != nil || got != token {
		t.Fatalf("token=%q err=%v", got, err)
	}
}

func TestTokenStoreHashOnlyLegacyStateIsNotReset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	tokenHash := HashToken("unrecoverable-token-with-enough-length-123456")
	cfg := testConfig(path, "unrecoverable-token-with-enough-length-123456")
	cfg.Auth.TokenSHA256 = tokenHash
	store := NewTokenStore(path)
	metadata, err := store.Ensure(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.Configured || metadata.Recoverable {
		t.Fatalf("metadata=%+v", metadata)
	}
	if _, err := os.Stat(filepath.Join(dir, TokenFileName)); !os.IsNotExist(err) {
		t.Fatalf("token file unexpectedly created: %v", err)
	}
}

func TestTokenStoreUpdateValidatesAndMatchesConfigHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	oldToken := "old-token-with-enough-length-123456"
	newToken := "new-token-with-enough-length-654321"
	cfg := testConfig(path, oldToken)
	store := NewTokenStore(path)
	if _, err := store.Update(&cfg, oldToken); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(&cfg, "too short"); err == nil {
		t.Fatal("short token was accepted")
	}
	if _, err := store.Update(&cfg, " "+newToken); err == nil {
		t.Fatal("whitespace-prefixed token was accepted")
	}
	if _, err := store.Update(&cfg, newToken); err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.TokenSHA256 != HashToken(newToken) {
		t.Fatalf("cfg hash=%q", cfg.Auth.TokenSHA256)
	}
	if got, err := store.Read(); err != nil || got != newToken {
		t.Fatalf("token=%q err=%v", got, err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Auth.TokenSHA256 != HashToken(newToken) {
		t.Fatalf("loaded hash=%q", loaded.Auth.TokenSHA256)
	}
}

func TestTokenStoreUpdateRestoresTokenWhenConfigWriteFails(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	oldToken := "old-token-with-enough-length-123456"
	newToken := "new-token-with-enough-length-654321"
	cfg := testConfig(configPath, oldToken)
	store := NewTokenStore(configPath)
	if _, err := store.Update(&cfg, oldToken); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(configPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckWritable(); err == nil {
		t.Fatal("directory config path was reported writable")
	}
	if _, err := store.Update(&cfg, newToken); err == nil {
		t.Fatal("update unexpectedly succeeded with directory config path")
	}
	got, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got != oldToken {
		t.Fatalf("token was not restored: %q", got)
	}
	if strings.Contains(got, newToken) {
		t.Fatal("new token leaked into restored token")
	}
}

func TestGenerateTokenHasCryptographicLength(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 64 || strings.TrimSpace(token) != token {
		t.Fatalf("generated token=%q", token)
	}
}
