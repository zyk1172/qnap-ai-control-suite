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

func TestQPKGAsyncQueuesAJobWithoutRunningQPKGOnRequest(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodPost, "/v1/qnap/qpkg/manage", `{"name":"container-station","action":"start","async":true}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"kind":"qpkg.start"`) || !strings.Contains(w.Body.String(), `"progress":0`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWebUIDashboardIsPublicAndListsOperationalPanels(t *testing.T) {
	s, _ := testServer(t)
	w := request(t, s, "", http.MethodGet, "/", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "系统与硬件") || !strings.Contains(w.Body.String(), "/v1/storage/overview") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestNetworkManageDryRunReturnsIPArgv(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodPost, "/v1/network/manage", `{"action":"set_mtu","interface":"eth0","value":"9000","dry_run":true}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"mtu"`) || !strings.Contains(w.Body.String(), `"transient_linux_ip"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDockerDryRunPreservesExecutorInputs(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodPost, "/v1/docker/command", `{"subcommand":"compose","args":["up","-d"],"cwd":"/share/Container/moviepilot","env":{"COMPOSE_PROJECT_NAME":"moviepilot"},"stdin_base64":"aW5wdXQ=","dry_run":true}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"cwd":"/share/Container/moviepilot"`) || !strings.Contains(w.Body.String(), `"stdin_bytes":5`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestFileAPIAcceptsDocumentedSnakeCaseFields(t *testing.T) {
	s, token := testServer(t)
	path := t.TempDir() + "/nested/data.bin"
	w := request(t, s, token, http.MethodPost, "/v1/files/write", `{"path":"`+path+`","content_base64":"AP8C","create_parents":true}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"bytes":3`) {
		t.Fatalf("write status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodPost, "/v1/files/read", `{"path":"`+path+`","offset":1,"max_bytes":2}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"content_base64":"/wI="`) {
		t.Fatalf("read status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSnapshotCapabilityEndpointIsStructured(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodGet, "/v1/storage/snapshots/capabilities", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"supported"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
