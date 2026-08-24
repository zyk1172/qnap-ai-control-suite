package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
)

// AuthManager keeps the active bearer-token hash independent from the loaded
// config value so a token rotation takes effect without restarting the agent.
type AuthManager struct {
	mu        sync.RWMutex
	tokenHash string
}

func NewAuthManager(tokenHash string) *AuthManager {
	return &AuthManager{tokenHash: strings.TrimSpace(tokenHash)}
}

func (m *AuthManager) Verify(token string) bool {
	sum := sha256.Sum256([]byte(token))
	actual := hex.EncodeToString(sum[:])
	m.mu.RLock()
	expected := m.tokenHash
	m.mu.RUnlock()
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func (m *AuthManager) SetTokenHash(tokenHash string) error {
	if !isHash(tokenHash) {
		return errors.New("token hash must be a 64-character hexadecimal SHA-256 value")
	}
	m.mu.Lock()
	m.tokenHash = strings.ToLower(tokenHash)
	m.mu.Unlock()
	return nil
}

func (m *AuthManager) CurrentHash() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tokenHash
}

func isHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
