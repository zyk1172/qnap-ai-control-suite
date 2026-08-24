package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"qnap-ai-control-suite/agent/internal/config"
)

const (
	TokenFileName        = "token"
	InitialTokenFileName = "initial-token.txt"
	MinimumTokenLength   = 24
	MaximumTokenLength   = 4096
)

var (
	// ErrNotWritable is returned when the token/config state directory cannot
	// complete the same-directory atomic write sequence required for rotation.
	ErrNotWritable = errors.New("token store is not writable")
	// ErrRecoveryRequired means the persistence transaction failed and at
	// least one compensating write could not be verified. The running
	// AuthManager is deliberately not changed in that case.
	ErrRecoveryRequired = errors.New("token recovery required")
)

type TokenStore struct {
	mu              sync.Mutex
	configPath      string
	directory       string
	tokenPath       string
	saveConfig      func(string, config.Config) error
	writeFileAtomic func(string, []byte, os.FileMode) error
}

type Metadata struct {
	Configured  bool       `json:"configured"`
	Recoverable bool       `json:"recoverable"`
	Writable    bool       `json:"writable"`
	Masked      string     `json:"masked"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

func NewTokenStore(configPath string) *TokenStore {
	directory := filepath.Dir(configPath)
	return &TokenStore{
		configPath:      configPath,
		directory:       directory,
		tokenPath:       filepath.Join(directory, TokenFileName),
		saveConfig:      config.SaveAtomic,
		writeFileAtomic: writeAtomic,
	}
}

// Ensure migrates the old plaintext file when present and reconciles the
// config hash with the token file. A legacy hash without a plaintext token is
// preserved and reported as configured but unrecoverable.
func (s *TokenStore) Ensure(cfg *config.Config) (Metadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDirectory(); err != nil {
		return Metadata{}, err
	}
	if err := s.secureConfigFile(); err != nil {
		return Metadata{}, err
	}
	legacyPath := filepath.Join(s.directory, InitialTokenFileName)
	if info, err := os.Stat(legacyPath); err == nil {
		if info.IsDir() {
			return Metadata{}, fmt.Errorf("legacy token path is a directory: %s", legacyPath)
		}
		if err := os.Chmod(legacyPath, 0600); err != nil {
			return Metadata{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Metadata{}, err
	}
	token, found, err := s.readTokenLocked()
	if err != nil {
		return Metadata{}, err
	}
	if !found {
		if legacy, legacyErr := os.ReadFile(legacyPath); legacyErr == nil {
			token = strings.TrimSuffix(string(legacy), "\n")
			if err := validateToken(token); err != nil {
				return Metadata{}, fmt.Errorf("invalid initial token: %w", err)
			}
			// The legacy file is not authoritative. Only migrate it when it
			// matches the hash already stored in config.json. This prevents a
			// stale initial-token.txt left by an old upgrade from resurrecting
			// credentials that were rotated or replaced later.
			if !strings.EqualFold(strings.TrimSpace(cfg.Auth.TokenSHA256), HashToken(token)) {
				return hashOnlyMetadata(s.checkWritableLocked(), cfg.Auth.TokenSHA256), nil
			}
			if err := s.writeFileAtomicFn()(s.tokenPath, []byte(token+"\n"), 0600); err != nil {
				return Metadata{}, err
			}
			// New installs do not create this file. Once a legacy token has
			// been verified and migrated, remove the legacy plaintext so it
			// cannot be mistaken for the current credential by older tooling.
			if err := os.Remove(legacyPath); err != nil {
				return Metadata{}, fmt.Errorf("retire legacy token: %w", err)
			}
			if err := syncDir(s.directory); err != nil {
				return Metadata{}, fmt.Errorf("sync retired legacy token: %w", err)
			}
			found = true
		} else if !errors.Is(legacyErr, os.ErrNotExist) {
			return Metadata{}, legacyErr
		}
	}
	if !found {
		return hashOnlyMetadata(s.checkWritableLocked(), cfg.Auth.TokenSHA256), nil
	}
	if err := os.Chmod(s.tokenPath, 0600); err != nil {
		return Metadata{}, err
	}
	hash := HashToken(token)
	if cfg.Auth.TokenSHA256 != hash {
		updated := *cfg
		updated.Auth.TokenSHA256 = hash
		if err := s.saveConfigFn()(s.configPath, updated); err != nil {
			return Metadata{}, err
		}
		cfg.Auth.TokenSHA256 = hash
	}
	metadata := metadataFor(token, hash, s.tokenPath)
	metadata.Writable = s.checkWritableLocked() == nil
	return metadata, nil
}

func (s *TokenStore) Metadata(tokenHash string) Metadata {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, found, err := s.readTokenLocked()
	if err != nil || !found || HashToken(token) != tokenHash {
		return Metadata{Configured: strings.TrimSpace(tokenHash) != "", Recoverable: false, Writable: s.checkWritableLocked() == nil}
	}
	metadata := metadataFor(token, tokenHash, s.tokenPath)
	metadata.Writable = s.checkWritableLocked() == nil
	return metadata
}

// CheckWritable performs a same-directory temporary write. It catches the
// common QPKG deployment failure where the service can read config.json but
// cannot atomically replace it. It never changes the token or config files.
func (s *TokenStore) CheckWritable() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkWritableLocked()
}

func (s *TokenStore) Read() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, found, err := s.readTokenLocked()
	if err != nil {
		return "", err
	}
	if !found {
		return "", os.ErrNotExist
	}
	return token, nil
}

// Update writes the token before the config hash and restores the old token if
// config persistence fails. A process crash between the two atomic renames is
// reconciled by Ensure on the next start, so the service cannot remain locked
// to a hash that no longer matches its token file.
func (s *TokenStore) Update(cfg *config.Config, token string) (string, error) {
	if err := validateToken(token); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDirectory(); err != nil {
		return "", err
	}
	if err := s.checkWritableLocked(); err != nil {
		return "", err
	}
	oldToken, found, err := s.readTokenLocked()
	if err != nil {
		return "", err
	}
	oldConfig, err := os.ReadFile(s.configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if errors.Is(err, os.ErrNotExist) {
		oldConfig = nil
	}
	configFound := err == nil

	writeErr := s.writeFileAtomicFn()(s.tokenPath, []byte(token+"\n"), 0600)
	if writeErr != nil {
		if rollbackErr := s.restoreStateLocked(oldConfig, configFound, oldToken, found); rollbackErr != nil {
			return "", recoveryError(writeErr, rollbackErr)
		}
		return "", writeErr
	}
	updated := *cfg
	updated.Auth.TokenSHA256 = HashToken(token)
	if err := s.saveConfigFn()(s.configPath, updated); err != nil {
		if rollbackErr := s.restoreStateLocked(oldConfig, configFound, oldToken, found); rollbackErr != nil {
			return "", recoveryError(err, rollbackErr)
		}
		return "", err
	}
	cfg.Auth.TokenSHA256 = updated.Auth.TokenSHA256
	return token, nil
}

func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func MaskToken(token string) string {
	runes := []rune(token)
	if len(runes) <= 8 {
		return strings.Repeat("•", len(runes))
	}
	return string(runes[:4]) + strings.Repeat("•", 12) + string(runes[len(runes)-4:])
}

func validateToken(token string) error {
	if token != strings.TrimSpace(token) || strings.ContainsAny(token, "\r\n") {
		return errors.New("token must not contain leading, trailing, or line-break whitespace")
	}
	if len(token) < MinimumTokenLength || len(token) > MaximumTokenLength {
		return fmt.Errorf("token length must be between %d and %d characters", MinimumTokenLength, MaximumTokenLength)
	}
	return nil
}

func (s *TokenStore) ensureDirectory() error {
	info, err := os.Stat(s.directory)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("token store path is not a directory: %s", s.directory)
		}
		// Never chmod an existing parent supplied through a custom config
		// path. The QPKG's dedicated directory is secured by installation;
		// here we only ensure files created by this store are 0600.
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(s.directory, 0700); err != nil {
		return err
	}
	return os.Chmod(s.directory, 0700)
}

func (s *TokenStore) secureConfigFile() error {
	info, err := os.Stat(s.configPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("config path is a directory, not a file: %s", s.configPath)
	}
	return os.Chmod(s.configPath, 0600)
}

func (s *TokenStore) checkWritableLocked() error {
	if info, err := os.Stat(s.configPath); err == nil && info.IsDir() {
		return fmt.Errorf("%w: config path is a directory, not a writable file", ErrNotWritable)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(s.directory, ".qacs-write-check-*")
	if err != nil {
		return fmt.Errorf("%w: token store directory is not writable: %v", ErrNotWritable, err)
	}
	tmpName := tmp.Name()
	cleanupPath := tmpName
	defer func() {
		_ = tmp.Close()
		if cleanupPath != "" {
			_ = os.Remove(cleanupPath)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		return fmt.Errorf("%w: chmod write check: %v", ErrNotWritable, err)
	}
	if _, err := tmp.Write([]byte("qacs-write-check\n")); err != nil {
		return fmt.Errorf("%w: write check: %v", ErrNotWritable, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("%w: sync write check: %v", ErrNotWritable, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%w: close write check: %v", ErrNotWritable, err)
	}
	replacementName := tmpName + ".renamed"
	if err := os.Rename(tmpName, replacementName); err != nil {
		return fmt.Errorf("%w: token store directory does not support atomic replacement: %v", ErrNotWritable, err)
	}
	cleanupPath = replacementName
	if err := os.Remove(replacementName); err != nil {
		return fmt.Errorf("%w: remove write check: %v", ErrNotWritable, err)
	}
	cleanupPath = ""
	if err := syncDir(s.directory); err != nil {
		return fmt.Errorf("%w: token store directory cannot be synced: %v", ErrNotWritable, err)
	}
	return nil
}

func (s *TokenStore) saveConfigFn() func(string, config.Config) error {
	if s.saveConfig != nil {
		return s.saveConfig
	}
	return config.SaveAtomic
}

func (s *TokenStore) writeFileAtomicFn() func(string, []byte, os.FileMode) error {
	if s.writeFileAtomic != nil {
		return s.writeFileAtomic
	}
	return writeAtomic
}

func (s *TokenStore) restoreStateLocked(oldConfig []byte, configFound bool, oldToken string, tokenFound bool) error {
	var rollbackErrors []error
	if configFound {
		if err := writeAtomic(s.configPath, oldConfig, 0600); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore config: %w", err))
		}
	} else if err := os.Remove(s.configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("remove new config: %w", err))
	}
	if tokenFound {
		if err := s.writeFileAtomicFn()(s.tokenPath, []byte(oldToken+"\n"), 0600); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore token: %w", err))
		}
	} else if err := os.Remove(s.tokenPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("remove new token: %w", err))
	}
	if err := syncDir(s.directory); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("sync rollback: %w", err))
	}
	if err := s.verifyStateLocked(oldConfig, configFound, oldToken, tokenFound); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	return errors.Join(rollbackErrors...)
}

func (s *TokenStore) verifyStateLocked(expectedConfig []byte, configFound bool, expectedToken string, tokenFound bool) error {
	actualConfig, err := os.ReadFile(s.configPath)
	if configFound {
		if err != nil {
			return fmt.Errorf("verify restored config: %w", err)
		}
		if !bytes.Equal(actualConfig, expectedConfig) {
			return errors.New("verify restored config: content does not match the pre-rotation state")
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("verify removed config: %w", err)
	} else if err == nil {
		return errors.New("verify removed config: file still exists")
	}

	actualToken, actualFound, err := s.readTokenLocked()
	if err != nil {
		return fmt.Errorf("verify restored token: %w", err)
	}
	if actualFound != tokenFound {
		return errors.New("verify restored token: presence does not match the pre-rotation state")
	}
	if tokenFound && actualToken != expectedToken {
		return errors.New("verify restored token: content does not match the pre-rotation state")
	}
	return nil
}

func recoveryError(cause, rollback error) error {
	return fmt.Errorf("%w: update failed: %v; rollback failed: %v", ErrRecoveryRequired, cause, rollback)
}

func hashOnlyMetadata(writableErr error, tokenHash string) Metadata {
	return Metadata{Configured: strings.TrimSpace(tokenHash) != "", Recoverable: false, Writable: writableErr == nil}
}

func (s *TokenStore) readTokenLocked() (string, bool, error) {
	b, err := os.ReadFile(s.tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	token := strings.TrimSuffix(string(b), "\n")
	if err := validateToken(token); err != nil {
		return "", false, fmt.Errorf("invalid token file: %w", err)
	}
	return token, true, nil
}

func metadataFor(token, tokenHash, path string) Metadata {
	info, err := os.Stat(path)
	if err != nil {
		return Metadata{Configured: tokenHash != "", Recoverable: true, Masked: MaskToken(token)}
	}
	updated := info.ModTime().UTC()
	return Metadata{Configured: tokenHash != "", Recoverable: true, Masked: MaskToken(token), UpdatedAt: &updated}
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".token-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	keep = true
	return syncDir(dir)
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
