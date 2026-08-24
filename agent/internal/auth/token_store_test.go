package auth

import (
	"errors"
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
	directoryInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	expectedDirectoryMode := directoryInfo.Mode().Perm()
	cfg := testConfig(path, token)
	if err := config.SaveAtomic(path, cfg); err != nil {
		t.Fatal(err)
	}
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
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy token was not retired: %v", err)
	}
	directoryInfo, err = os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if directoryInfo.Mode().Perm() != expectedDirectoryMode {
		t.Fatalf("existing token directory mode changed from %o to %o", expectedDirectoryMode, directoryInfo.Mode().Perm())
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

func TestTokenStoreRejectsStaleInitialToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	configuredToken := "configured-token-with-enough-length-123456"
	staleToken := "stale-token-with-enough-length-654321"
	legacyPath := filepath.Join(dir, InitialTokenFileName)
	if err := os.WriteFile(legacyPath, []byte(staleToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(path, configuredToken)
	store := NewTokenStore(path)
	metadata, err := store.Ensure(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.Configured || metadata.Recoverable || metadata.Masked != "" {
		t.Fatalf("metadata=%+v", metadata)
	}
	if _, err := os.Stat(filepath.Join(dir, TokenFileName)); !os.IsNotExist(err) {
		t.Fatalf("stale token was migrated: %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy token should remain available for explicit recovery: %v", err)
	}
	if cfg.Auth.TokenSHA256 != HashToken(configuredToken) {
		t.Fatalf("configured hash changed: %q", cfg.Auth.TokenSHA256)
	}
}

func TestTokenStoreDoesNotChangeExistingParentPermissions(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "custom-config.json")
	if err := os.WriteFile(path, []byte(`{"auth":{"token_sha256":"placeholder"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(path, "parent-permission-token-with-enough-length-123456")
	store := NewTokenStore(path)
	if _, err := store.Ensure(&cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("existing parent mode changed to %o", info.Mode().Perm())
	}
	configInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if configInfo.Mode().Perm() != 0600 {
		t.Fatalf("config mode=%o", configInfo.Mode().Perm())
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
	store.saveConfig = func(string, config.Config) error {
		return errors.New("injected config persistence failure")
	}
	if _, err := store.Update(&cfg, newToken); err == nil {
		t.Fatal("update unexpectedly succeeded with injected config failure")
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
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Auth.TokenSHA256 != HashToken(oldToken) {
		t.Fatalf("config hash changed after rollback: %q", loaded.Auth.TokenSHA256)
	}
}

func TestTokenStoreReportsRecoveryFailure(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	oldToken := "old-token-with-enough-length-123456"
	newToken := "new-token-with-enough-length-654321"
	cfg := testConfig(configPath, oldToken)
	store := NewTokenStore(configPath)
	if _, err := store.Update(&cfg, oldToken); err != nil {
		t.Fatal(err)
	}
	store.saveConfig = func(string, config.Config) error {
		return errors.New("injected config persistence failure")
	}
	writes := 0
	store.writeFileAtomic = func(path string, data []byte, mode os.FileMode) error {
		writes++
		if writes == 2 {
			return errors.New("injected token rollback failure")
		}
		return writeAtomic(path, data, mode)
	}
	if _, err := store.Update(&cfg, newToken); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("expected recovery-required error, got %v", err)
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
