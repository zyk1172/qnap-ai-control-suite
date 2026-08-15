package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"qnap-ai-control-suite/agent/internal/config"
	"qnap-ai-control-suite/agent/internal/jobs"
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

func TestCommandOutputCannotExceedConfiguredLimit(t *testing.T) {
	s, token := testServer(t)
	s.Config.Command.MaxOutputBytes = 4
	w := request(t, s, token, http.MethodPost, "/v1/exec", `{"argv":["/bin/sh","-c","printf 123456789"],"max_output_bytes":4096}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"stdout":"1234"`) || !strings.Contains(w.Body.String(), `"stdout_truncated":true`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
func TestFullTrustShell(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodPost, "/v1/shell", `{"shell":"printf works"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "works") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestShellSupportsExplicitShellAndScript(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodPost, "/v1/shell", `{"shell":"/bin/sh","script":"printf works"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "works") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestShellRejectsNonExecutableInterpreter(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodPost, "/v1/shell", `{"shell":"relative-shell","script":"echo no"}`)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "shell_unavailable") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestStructuredSystemResourcesAndJobs(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodGet, "/v1/system/resources", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "memory_bytes") || !strings.Contains(w.Body.String(), "swap_bytes") {
		t.Fatalf("resources status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodGet, "/v1/system/sockets", "")
	if (w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "sockets")) && (w.Code != http.StatusNotImplemented || !strings.Contains(w.Body.String(), "socket_inventory_unavailable")) {
		t.Fatalf("sockets status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodGet, "/v1/system/ntp", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "configured") {
		t.Fatalf("ntp status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodPost, "/v1/jobs", `{"kind":"test","command":{"argv":["/bin/echo","job"],"dry_run":true}}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "queued") {
		t.Fatalf("job status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestJobLogsArePagedAndHiddenFromMetadata(t *testing.T) {
	s, token := testServer(t)
	job := s.Jobs.Start("logs", func(_ context.Context, log func(string)) (any, error) {
		log("one")
		log("two")
		return "done", nil
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, ok := s.Jobs.Get(job.ID)
		if ok && current.Status == jobs.Succeeded {
			break
		}
		time.Sleep(time.Millisecond)
	}
	w := request(t, s, token, http.MethodGet, "/v1/jobs/"+job.ID, "")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"logs":`) || !strings.Contains(w.Body.String(), `"log_count":2`) {
		t.Fatalf("metadata status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodGet, "/v1/jobs/"+job.ID+"/logs?limit=1", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"lines":["one"]`) || !strings.Contains(w.Body.String(), `"next_cursor":1`) {
		t.Fatalf("logs status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestJobStartSupportsExplicitShellAndBase64Stdin(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodPost, "/v1/jobs", `{"kind":"shell-test","shell":"/bin/sh","script":"read value; printf %s \"$value\"","command":{"stdin_base64":"aGVsbG8="}}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"kind":"shell-test"`) || !strings.Contains(w.Body.String(), `"status":"queued"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSystemInfoIncludesQNAPDiscoverySummary(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodGet, "/v1/system/info", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"qnap"`) || !strings.Contains(w.Body.String(), `"cpu_count"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestQNAPProbeUsesBundledScriptPathAndValidatesOutput(t *testing.T) {
	s, token := testServer(t)
	s.ProbePath = "/bin/echo"
	w := request(t, s, token, http.MethodPost, "/v1/qnap/probe", `{"output_path":"/share/Public/qnap-probe.json","dry_run":true}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"argv":["/bin/echo","/share/Public/qnap-probe.json"]`) || !strings.Contains(w.Body.String(), `"dry_run":true`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodPost, "/v1/qnap/probe", `{"output_path":"relative.json","dry_run":true}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "output_path must be an absolute path") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodGet, "/v1/qnap/probe", "")
	if w.Code != http.StatusMethodNotAllowed || !strings.Contains(w.Body.String(), "POST required") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
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

func TestRAIDActionRequiresAvailableSyncControl(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodPost, "/v1/storage/raid-groups/md0/action", `{"action":"scrub_start","dry_run":true}`)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "raid_action_failed") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodGet, "/v1/storage/raid-groups/md0/action", "")
	if w.Code != http.StatusMethodNotAllowed || !strings.Contains(w.Body.String(), "POST required") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestLogTailAcceptsRFC3339Window(t *testing.T) {
	s, token := testServer(t)
	if err := os.WriteFile(s.Config.Audit.Path, []byte(`{"ts":"2026-08-14T01:00:00Z","action":"match"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := request(t, s, token, http.MethodPost, "/v1/logs/tail", `{"name":"audit","since":"2026-08-14T00:00:00Z","until":"2026-08-14T02:00:00Z"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"time_filtered":true`) || !strings.Contains(w.Body.String(), "match") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodPost, "/v1/logs/tail", `{"name":"audit","since":"invalid"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "RFC3339") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestConfiguredEcosystemAdapterDryRun(t *testing.T) {
	s, token := testServer(t)
	s.Config.QNAPAdapters = map[string]config.QNAPAdapter{
		"hbs3":            {Commands: map[string][]string{"job_status": {"/bin/echo", "status", "{id}"}}},
		"shares":          {Commands: map[string][]string{"rename": {"/bin/echo", "share", "rename", "{name}", "{target}"}}},
		"virtual_switch":  {Commands: map[string][]string{"list": {"/bin/echo", "virtual-switch", "list"}}},
		"system_settings": {Commands: map[string][]string{"hostname": {"/bin/echo", "system", "hostname", "{name}"}}},
		"firmware":        {Commands: map[string][]string{"info": {"/bin/echo", "firmware", "info"}}},
		"notifications":   {Commands: map[string][]string{"test": {"/bin/echo", "notification", "test", "{target}"}}},
		"storage_manager": {Commands: map[string][]string{"pools": {"/bin/echo", "storage", "pools"}}},
	}
	s.Ecosystem.Adapters = s.Config.QNAPAdapters
	w := request(t, s, token, http.MethodPost, "/v1/qnap/hbs/action", `{"action":"job_status","id":"backup-1","dry_run":true}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"argv":["/bin/echo","status","backup-1"]`) || !strings.Contains(w.Body.String(), `"adapter":"hbs3"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodPost, "/v1/qnap/vm/action", `{"action":"list","dry_run":true}`)
	if w.Code != http.StatusNotImplemented || !strings.Contains(w.Body.String(), "adapter_unavailable") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodPost, "/v1/shares/manage", `{"action":"rename","name":"old","target":"new","dry_run":true}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"argv":["/bin/echo","share","rename","old","new"]`) || !strings.Contains(w.Body.String(), `"adapter":"shares"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	for _, item := range []struct {
		path string
		body string
		want string
	}{
		{"/v1/qnap/virtual-switch/action", `{"action":"list","dry_run":true}`, `"argv":["/bin/echo","virtual-switch","list"]`},
		{"/v1/qnap/system-settings/action", `{"action":"hostname","name":"nas","dry_run":true}`, `"argv":["/bin/echo","system","hostname","nas"]`},
		{"/v1/qnap/firmware/action", `{"action":"info","dry_run":true}`, `"argv":["/bin/echo","firmware","info"]`},
		{"/v1/qnap/notifications/action", `{"action":"test","target":"mail","dry_run":true}`, `"argv":["/bin/echo","notification","test","mail"]`},
		{"/v1/qnap/storage/action", `{"action":"pools","dry_run":true}`, `"argv":["/bin/echo","storage","pools"]`},
	} {
		w = request(t, s, token, http.MethodPost, item.path, item.body)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), item.want) {
			t.Fatalf("path=%s status=%d body=%s", item.path, w.Code, w.Body.String())
		}
	}
}
