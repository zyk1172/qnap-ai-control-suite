package auth

import (
	"sync"
	"testing"
)

func TestAuthManagerRotatesTokenImmediately(t *testing.T) {
	oldToken := "old-token-with-enough-length-123456"
	newToken := "new-token-with-enough-length-654321"
	manager := NewAuthManager(HashToken(oldToken))

	if !manager.Verify(oldToken) || manager.Verify(newToken) {
		t.Fatal("initial token verification is incorrect")
	}
	if err := manager.SetTokenHash(HashToken(newToken)); err != nil {
		t.Fatal(err)
	}
	if manager.Verify(oldToken) || !manager.Verify(newToken) {
		t.Fatal("token rotation was not applied immediately")
	}
}

func TestAuthManagerRejectsInvalidHash(t *testing.T) {
	manager := NewAuthManager("")
	if err := manager.SetTokenHash("not-a-hash"); err == nil {
		t.Fatal("invalid hash was accepted")
	}
}

func TestAuthManagerConcurrentVerifyAndSet(t *testing.T) {
	manager := NewAuthManager(HashToken("token-a-with-enough-length-123456"))
	const iterations = 500
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = manager.Verify("token-a-with-enough-length-123456")
			_ = manager.Verify("token-b-with-enough-length-654321")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			hash := HashToken("token-a-with-enough-length-123456")
			if i%2 == 1 {
				hash = HashToken("token-b-with-enough-length-654321")
			}
			if err := manager.SetTokenHash(hash); err != nil {
				t.Errorf("set hash: %v", err)
				return
			}
		}
	}()
	wg.Wait()
}
