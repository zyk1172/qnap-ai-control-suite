package api

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"qnap-ai-control-suite/agent/internal/auth"
)

func (s *Server) adminRoute(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/admin/token":
		s.adminToken(w, r)
	case "/v1/admin/token/reveal":
		s.adminTokenReveal(w, r)
	case "/v1/admin/token/generate":
		s.adminTokenGenerate(w, r)
	case "/v1/admin/settings":
		s.adminSettings(w, r)
	default:
		s.fail(w, r, http.StatusNotFound, "not_found", "admin route not found", nil)
	}
}

func (s *Server) adminToken(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.ok(w, r, s.tokenMetadata())
	case http.MethodPut:
		var req struct {
			Token string `json:"token"`
		}
		if !decode(w, r, &req) {
			return
		}
		s.updateToken(w, r, req.Token)
	default:
		s.fail(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "GET or PUT required", nil)
	}
}

func (s *Server) adminTokenReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.fail(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "POST required", nil)
		return
	}
	if s.TokenStore == nil {
		s.fail(w, r, http.StatusServiceUnavailable, "token_store_unavailable", "token store is not configured", nil)
		return
	}
	token, err := s.TokenStore.Read()
	if errors.Is(err, os.ErrNotExist) {
		s.fail(w, r, http.StatusConflict, "token_unrecoverable", "the current token is configured but its plaintext is not recoverable; generate a new token", nil)
		return
	}
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "token_read_failed", err.Error(), nil)
		return
	}
	if auth.HashToken(token) != s.Auth.CurrentHash() {
		s.fail(w, r, http.StatusConflict, "token_unrecoverable", "the token file does not match the active authentication hash; generate a new token", nil)
		return
	}
	s.ok(w, r, map[string]any{"token": token, "metadata": s.tokenMetadata()})
}

func (s *Server) adminTokenGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.fail(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "POST required", nil)
		return
	}
	token, err := auth.GenerateToken()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "token_generate_failed", err.Error(), nil)
		return
	}
	s.updateToken(w, r, token)
}

func (s *Server) updateToken(w http.ResponseWriter, r *http.Request, token string) {
	if s.TokenStore == nil || s.ConfigPath == "" {
		s.fail(w, r, http.StatusServiceUnavailable, "token_store_unavailable", "token store is not configured", nil)
		return
	}
	if err := s.TokenStore.CheckWritable(); err != nil {
		s.fail(w, r, http.StatusServiceUnavailable, "token_store_not_writable", "token/config storage is not writable: "+err.Error(), nil)
		return
	}
	cfg := s.Config
	updatedToken, err := s.TokenStore.Update(&cfg, token)
	if err != nil {
		status := http.StatusBadRequest
		if !strings.Contains(err.Error(), "length") && !strings.Contains(err.Error(), "whitespace") {
			status = http.StatusInternalServerError
		}
		s.fail(w, r, status, "token_update_failed", err.Error(), nil)
		return
	}
	if err := s.Auth.SetTokenHash(cfg.Auth.TokenSHA256); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "auth_reload_failed", err.Error(), nil)
		return
	}
	s.audit(r, "admin.token.update", "succeeded", map[string]any{"credential_rotated": true}, 0, "")
	s.ok(w, r, map[string]any{"token": updatedToken, "metadata": s.tokenMetadata()})
}

func (s *Server) tokenMetadata() auth.Metadata {
	if s.TokenStore == nil {
		return auth.Metadata{Configured: s.Auth != nil && s.Auth.CurrentHash() != "", Recoverable: false}
	}
	return s.TokenStore.Metadata(s.Auth.CurrentHash())
}

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.fail(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "GET required; settings are read-only in v2.1.0", nil)
		return
	}
	s.ok(w, r, map[string]any{
		"profile":         s.Config.Profile,
		"approval":        s.Config.Approval,
		"jobs":            s.Config.Jobs,
		"permissions":     s.Config.Permissions,
		"audit_enabled":   s.Config.Audit.Enabled,
		"audit_redaction": s.Config.Audit.RedactSecrets == nil || *s.Config.Audit.RedactSecrets,
		"docker_paths":    len(s.Config.DockerPaths),
		"qnap_adapters":   s.Config.QNAPAdapters,
	})
}
