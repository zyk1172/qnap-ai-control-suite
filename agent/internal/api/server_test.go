package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qnap-ai-control-suite/agent/internal/config"
)

func testServer(t *testing.T) (*Server, string) {
	t.Helper()
	token := "test-token"
	sum := sha256.Sum256([]byte(token))
	cfg := config.FullTrust(hex.EncodeToString(sum[:]))
	cfg.Audit.Path = t.TempDir() + "/audit.jsonl"
	return New(cfg), token
}
func request(t *testing.T, s *Server, token, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestAuthAndEnvelope(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, "", http.MethodGet, "/v1/health", "")
	if w.Code != 401 {
		t.Fatalf("status=%d", w.Code)
	}
	var bad envelope
	if err := json.Unmarshal(w.Body.Bytes(), &bad); err != nil {
		t.Fatal(err)
	}
	if bad.OK || bad.Error == nil || bad.Error.Code != "unauthorized" {
		t.Fatalf("bad=%+v", bad)
	}
	w = request(t, s, token, http.MethodGet, "/v1/health", "")
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var ok envelope
	if err := json.Unmarshal(w.Body.Bytes(), &ok); err != nil {
		t.Fatal(err)
	}
	if !ok.OK || ok.Data == nil || ok.Meta.RequestID == "" {
		t.Fatalf("ok=%+v", ok)
	}
}
func TestCommandNonZeroIsNotSuccess(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodPost, "/v1/exec", `{"argv":["/bin/sh","-c","exit 3"]}`)
	if w.Code != 422 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var result envelope
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Error.Code != "non_zero_exit" {
		t.Fatalf("result=%+v", result)
	}
}
func TestFullTrustShell(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodPost, "/v1/shell", `{"shell":"printf works"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "works") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestStructuredSystemResourcesAndJobs(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodGet, "/v1/system/resources", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "memory_bytes") {
		t.Fatalf("resources status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodPost, "/v1/jobs", `{"kind":"test","command":{"argv":["/bin/echo","job"],"dry_run":true}}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "queued") {
		t.Fatalf("job status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestQPKGDryRunUsesDocumentedFlags(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodPost, "/v1/qnap/qpkg/manage", `{"name":"container-station","action":"start","dry_run":true}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"--start"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodPost, "/v1/qnap/qpkg/manage", `{"action":"invalid","dry_run":true}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_qpkg_action") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
