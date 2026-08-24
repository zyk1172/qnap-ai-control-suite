package auth

import (
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

type TokenStore struct {
	mu         sync.Mutex
	configPath string
	directory  string
	tokenPath  string
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
	return &TokenStore{configPath: configPath, directory: directory, tokenPath: filepath.Join(directory, TokenFileName)}
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
			if err := writeAtomic(s.tokenPath, []byte(token+"\n"), 0600); err != nil {
				return Metadata{}, err
			}
			found = true
		} else if !errors.Is(legacyErr, os.ErrNotExist) {
			return Metadata{}, legacyErr
		}
	}
	if !found {
		return Metadata{Configured: strings.TrimSpace(cfg.Auth.TokenSHA256) != "", Recoverable: false, Writable: s.checkWritableLocked() == nil}, nil
	}
	if err := os.Chmod(s.tokenPath, 0600); err != nil {
		return Metadata{}, err
	}
	hash := HashToken(token)
	if cfg.Auth.TokenSHA256 != hash {
		updated := *cfg
		updated.Auth.TokenSHA256 = hash
		if err := config.SaveAtomic(s.configPath, updated); err != nil {
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
	if err := writeAtomic(s.tokenPath, []byte(token+"\n"), 0600); err != nil {
		return "", err
	}
	updated := *cfg
	updated.Auth.TokenSHA256 = HashToken(token)
	if err := config.SaveAtomic(s.configPath, updated); err != nil {
		if found {
			_ = writeAtomic(s.tokenPath, []byte(oldToken+"\n"), 0600)
		} else {
			_ = os.Remove(s.tokenPath)
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
	if err := os.MkdirAll(s.directory, 0700); err != nil {
		return err
	}
	return os.Chmod(s.directory, 0700)
}

func (s *TokenStore) checkWritableLocked() error {
	if info, err := os.Stat(s.configPath); err == nil && info.IsDir() {
		return errors.New("config path is a directory, not a writable file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(s.directory, ".qacs-write-check-*")
	if err != nil {
		return fmt.Errorf("token store directory is not writable: %w", err)
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
		return err
	}
	if _, err := tmp.Write([]byte("qacs-write-check\n")); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	replacementName := tmpName + ".renamed"
	if err := os.Rename(tmpName, replacementName); err != nil {
		return fmt.Errorf("token store directory does not support atomic replacement: %w", err)
	}
	cleanupPath = replacementName
	if err := os.Remove(replacementName); err != nil {
		return err
	}
	cleanupPath = ""
	if err := syncDir(s.directory); err != nil {
		return fmt.Errorf("token store directory cannot be synced: %w", err)
	}
	return nil
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
