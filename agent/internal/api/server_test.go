package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"qnap-ai-control-suite/agent/internal/approval"
	"qnap-ai-control-suite/agent/internal/config"
	"qnap-ai-control-suite/agent/internal/jobs"
)

func testServer(t *testing.T) (*Server, string) {
	t.Helper()
	token := "test-token"
	sum := sha256.Sum256([]byte(token))
	cfg := config.FullTrust(hex.EncodeToString(sum[:]))
	cfg.Audit.Path = t.TempDir() + "/audit.jsonl"
	cfg.Jobs.JournalPath = t.TempDir() + "/jobs.jsonl"
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

func TestV2StatusSnapshotAndTextFileRoutes(t *testing.T) {
	s, token := testServer(t)
	w := request(t, s, token, http.MethodGet, "/v1/status/snapshot", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"resources"`) || !strings.Contains(w.Body.String(), `"jobs"`) {
		t.Fatalf("snapshot status=%d body=%s", w.Code, w.Body.String())
	}

	path := t.TempDir() + "/notes.txt"
	data := base64.StdEncoding.EncodeToString([]byte("first line\nneedle line\nlast line\n"))
	w = request(t, s, token, http.MethodPost, "/v1/files/write", `{"path":"`+path+`","content_base64":"`+data+`","backup":true}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"sha256"`) {
		t.Fatalf("write status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodPost, "/v1/files/read-text", `{"path":"`+path+`"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "needle line") || strings.Contains(w.Body.String(), "content_base64") {
		t.Fatalf("read-text status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodPost, "/v1/files/read-lines", `{"path":"`+path+`","start":1,"limit":1}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"needle line"`) {
		t.Fatalf("read-lines status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(t, s, token, http.MethodPost, "/v1/files/grep", `{"path":"`+path+`","query":"needle"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"line":2`) {
		t.Fatalf("grep status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestV2OptionalTelemetryRoutesAreRegistered(t *testing.T) {
	s, token := testServer(t)
	for _, path := range []string{"/v1/storage/disk-io", "/v1/network/ipv6/routes", "/v1/network/ipv6/neighbors", "/v1/shares/smb-status"} {
		w := request(t, s, token, http.MethodGet, path, "")
		if w.Code == http.StatusNotFound {
			t.Fatalf("route %s was not registered: %s", path, w.Body.String())
		}
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
	var response struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Data.ID == "" {
		t.Fatalf("unable to read queued job id: err=%v body=%s", err, w.Body.String())
	}
	waitForAPIJobStatus(t, s, response.Data.ID, jobs.Succeeded)
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
	var response struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Data.ID == "" {
		t.Fatalf("unable to read queued job id: err=%v body=%s", err, w.Body.String())
	}
	current := waitForAPIJobStatus(t, s, response.Data.ID, jobs.Succeeded)
	lines, _, _, _ := s.Jobs.Logs(current.ID, 0, 10)
	if strings.Join(lines, "") != "hello" {
		t.Fatalf("job output=%q", lines)
	}
}

func waitForAPIJobStatus(t *testing.T, s *Server, id string, wanted jobs.Status) jobs.Job {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, ok := s.Jobs.Get(id)
		if ok && current.Status == wanted {
			return current
		}
		time.Sleep(time.Millisecond)
	}
	current, _ := s.Jobs.Get(id)
	t.Fatalf("job %s did not finish with %s: %#v", id, wanted, current)
	return jobs.Job{}
}

func waitForAPIJobTerminal(t *testing.T, s *Server, id string) jobs.Job {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, ok := s.Jobs.Get(id)
		if ok && current.Status != jobs.Queued && current.Status != jobs.Running {
			return current
		}
		time.Sleep(time.Millisecond)
	}
	current, _ := s.Jobs.Get(id)
	t.Fatalf("job %s did not reach a terminal state: %#v", id, current)
	return jobs.Job{}
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
	// Hold the matching resource lock so this unit test proves queueing without
	// ever invoking a real QPKG command. Disable test-only audit output so the
	// terminal journal write is the only filesystem lifecycle to synchronize.
	s.Audit.Enabled = false
	release := make(chan struct{})
	blocker, reused := s.Jobs.StartWithOptions(jobs.StartOptions{Kind: "test-resource-lock", Resource: "qpkg:container-station"}, func(ctx context.Context, _ func(string)) (any, error) {
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if reused {
		t.Fatal("resource-lock job unexpectedly reused")
	}
	waitForAPIJobStatus(t, s, blocker.ID, jobs.Running)
	var qpkgJobID string
	defer func() {
		if qpkgJobID != "" {
			s.Jobs.Cancel(qpkgJobID)
			waitForAPIJobTerminal(t, s, qpkgJobID)
		}
		s.Jobs.Cancel(blocker.ID)
		close(release)
		waitForAPIJobTerminal(t, s, blocker.ID)
	}()
	w := request(t, s, token, http.MethodPost, "/v1/qnap/qpkg/manage", `{"name":"container-station","action":"start","async":true}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"kind":"qpkg.start"`) || !strings.Contains(w.Body.String(), `"status":"queued"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Data.ID == "" {
		t.Fatalf("unable to read queued job id: err=%v body=%s", err, w.Body.String())
	}
	qpkgJobID = response.Data.ID
	if current, ok := s.Jobs.Get(qpkgJobID); !ok || current.Status != jobs.Queued {
		t.Fatalf("qpkg job ran before response: %#v exists=%v", current, ok)
	}
	if !s.Jobs.Cancel(qpkgJobID) {
		t.Fatal("unable to cancel queued qpkg job")
	}
	if completed := waitForAPIJobTerminal(t, s, qpkgJobID); completed.Status != jobs.Cancelled {
		t.Fatalf("qpkg job status=%s, want cancelled", completed.Status)
	}
}

func TestSensitiveDryRunRequiresApprovalThenConsumesTicket(t *testing.T) {
	s, token := testServer(t)
	body := `{"name":"example","action":"remove","dry_run":true}`
	w := request(t, s, token, http.MethodPost, "/v1/qnap/qpkg/manage", body)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected approval required, status=%d body=%s", w.Code, w.Body.String())
	}
	var first struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				ID string `json:"approval_id"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.OK || first.Error.Code != "approval_required" || first.Error.Details.ID == "" {
		t.Fatalf("unexpected approval response: %s", w.Body.String())
	}
	// A pending ticket must not authorize execution. Approval is a user-side
	// decision, then the original request retries with the same one-time ID.
	r := httptest.NewRequest(http.MethodPost, "/v1/qnap/qpkg/manage", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set(approvalHeader, first.Error.Details.ID)
	pending := httptest.NewRecorder()
	s.Handler().ServeHTTP(pending, r)
	if pending.Code != http.StatusConflict || !strings.Contains(pending.Body.String(), `"approval_not_approved"`) {
		t.Fatalf("pending retry status=%d body=%s", pending.Code, pending.Body.String())
	}
	decision := request(t, s, token, http.MethodPost, "/v1/approvals/"+first.Error.Details.ID+"/decision", `{"decision":"approve"}`)
	if decision.Code != http.StatusOK || !strings.Contains(decision.Body.String(), `"state":"approved"`) {
		t.Fatalf("decision status=%d body=%s", decision.Code, decision.Body.String())
	}
	r = httptest.NewRequest(http.MethodPost, "/v1/qnap/qpkg/manage", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set(approvalHeader, first.Error.Details.ID)
	approved := httptest.NewRecorder()
	s.Handler().ServeHTTP(approved, r)
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), `"dry_run":true`) {
		t.Fatalf("approved retry status=%d body=%s", approved.Code, approved.Body.String())
	}
	r = httptest.NewRequest(http.MethodPost, "/v1/qnap/qpkg/manage", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set(approvalHeader, first.Error.Details.ID)
	replayed := httptest.NewRecorder()
	s.Handler().ServeHTTP(replayed, r)
	if replayed.Code != http.StatusConflict || !strings.Contains(replayed.Body.String(), `"approval_used"`) {
		t.Fatalf("replay status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	auditData, err := os.ReadFile(s.Config.Audit.Path)
	if err != nil {
		t.Fatal(err)
	}
	auditText := string(auditData)
	for _, event := range []string{"approval_requested", "approval_not_approved", "approval_approved", "approval_executed", "approval_used"} {
		if !strings.Contains(auditText, `"approval_event":"`+event+`"`) || !strings.Contains(auditText, first.Error.Details.ID) {
			t.Fatalf("audit missing %s for approval %s: %s", event, first.Error.Details.ID, auditText)
		}
	}
	type approvalAuditRecord struct {
		RequestID          string     `json:"request_id"`
		ApprovalID         string     `json:"approval_id"`
		ApprovalEvent      string     `json:"approval_event"`
		ApprovalStatus     string     `json:"approval_status"`
		Status             string     `json:"status"`
		ApprovalCreatedAt  *time.Time `json:"approval_created_at"`
		ApprovalDecisionAt *time.Time `json:"approval_decision_at"`
		ApprovalExecutedAt *time.Time `json:"approval_executed_at"`
	}
	records := map[string]approvalAuditRecord{}
	for _, line := range strings.Split(strings.TrimSpace(auditText), "\n") {
		var record approvalAuditRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("invalid audit record %q: %v", line, err)
		}
		if record.ApprovalID == first.Error.Details.ID {
			records[record.ApprovalEvent] = record
		}
	}
	requestedRecord := records["approval_requested"]
	approvedRecord := records["approval_approved"]
	executedRecord := records["approval_executed"]
	if requestedRecord.RequestID == "" || approvedRecord.RequestID != requestedRecord.RequestID || executedRecord.RequestID != requestedRecord.RequestID || requestedRecord.ApprovalCreatedAt == nil || approvedRecord.ApprovalDecisionAt == nil || executedRecord.ApprovalExecutedAt == nil || executedRecord.Status != "success" || executedRecord.ApprovalStatus != "used" {
		t.Fatalf("approval audit lifecycle is not correlated: requested=%+v approved=%+v executed=%+v", requestedRecord, approvedRecord, executedRecord)
	}
}

func TestSensitiveApprovalDecisionDenyPreventsRetry(t *testing.T) {
	s, token := testServer(t)
	body := `{"name":"example","action":"remove","dry_run":true}`
	first := request(t, s, token, http.MethodPost, "/v1/qnap/qpkg/manage", body)
	if first.Code != http.StatusConflict {
		t.Fatalf("initial status=%d body=%s", first.Code, first.Body.String())
	}
	var envelope struct {
		Error struct {
			Details struct {
				ID string `json:"approval_id"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &envelope); err != nil || envelope.Error.Details.ID == "" {
		t.Fatalf("approval response=%s err=%v", first.Body.String(), err)
	}
	decision := request(t, s, token, http.MethodPost, "/v1/approvals/"+envelope.Error.Details.ID+"/decision", `{"decision":"deny"}`)
	if decision.Code != http.StatusOK || !strings.Contains(decision.Body.String(), `"state":"denied"`) {
		t.Fatalf("decision status=%d body=%s", decision.Code, decision.Body.String())
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/qnap/qpkg/manage", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set(approvalHeader, envelope.Error.Details.ID)
	retry := httptest.NewRecorder()
	s.Handler().ServeHTTP(retry, r)
	if retry.Code != http.StatusConflict || !strings.Contains(retry.Body.String(), `"approval_denied"`) {
		t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	auditData, err := os.ReadFile(s.Config.Audit.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(auditData), `"status":"approval_denied"`) || !strings.Contains(string(auditData), envelope.Error.Details.ID) {
		t.Fatalf("deny decision was not audited: %s", auditData)
	}
}

func TestSensitiveApprovalAuditPreservesExecutionFailure(t *testing.T) {
	s, token := testServer(t)
	body := `{"argv":["/definitely/missing/mkfs.qacs-test"]}`
	first := request(t, s, token, http.MethodPost, "/v1/exec", body)
	var envelope struct {
		Error struct {
			Details struct {
				ID string `json:"approval_id"`
			} `json:"details"`
		} `json:"error"`
	}
	if first.Code != http.StatusConflict || json.Unmarshal(first.Body.Bytes(), &envelope) != nil || envelope.Error.Details.ID == "" {
		t.Fatalf("approval response status=%d body=%s", first.Code, first.Body.String())
	}
	decision := request(t, s, token, http.MethodPost, "/v1/approvals/"+envelope.Error.Details.ID+"/decision", `{"decision":"approve"}`)
	if decision.Code != http.StatusOK {
		t.Fatalf("decision status=%d body=%s", decision.Code, decision.Body.String())
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/exec", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set(approvalHeader, envelope.Error.Details.ID)
	retry := httptest.NewRecorder()
	s.Handler().ServeHTTP(retry, r)
	if retry.Code != http.StatusForbidden || !strings.Contains(retry.Body.String(), `"start_failed"`) {
		t.Fatalf("failed execution status=%d body=%s", retry.Code, retry.Body.String())
	}

	auditData, err := os.ReadFile(s.Config.Audit.Path)
	if err != nil {
		t.Fatal(err)
	}
	var executed struct {
		ApprovalEvent  string `json:"approval_event"`
		ApprovalStatus string `json:"approval_status"`
		Status         string `json:"status"`
	}
	for _, line := range strings.Split(strings.TrimSpace(string(auditData)), "\n") {
		var record struct {
			ApprovalID     string `json:"approval_id"`
			ApprovalEvent  string `json:"approval_event"`
			ApprovalStatus string `json:"approval_status"`
			Status         string `json:"status"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("invalid audit record %q: %v", line, err)
		}
		if record.ApprovalID == envelope.Error.Details.ID && record.ApprovalEvent == "approval_executed" {
			executed.ApprovalEvent = record.ApprovalEvent
			executed.ApprovalStatus = record.ApprovalStatus
			executed.Status = record.Status
		}
	}
	if executed.ApprovalEvent != "approval_executed" || executed.ApprovalStatus != "used" || executed.Status != "failed" {
		t.Fatalf("execution failure audit was misclassified: %+v", executed)
	}
}

func TestSensitiveApprovalRejectsModifiedRequest(t *testing.T) {
	s, token := testServer(t)
	initialBody := `{"name":"example","action":"remove","dry_run":true}`
	first := request(t, s, token, http.MethodPost, "/v1/qnap/qpkg/manage", initialBody)
	var envelope struct {
		Error struct {
			Details struct {
				ID string `json:"approval_id"`
			} `json:"details"`
		} `json:"error"`
	}
	if first.Code != http.StatusConflict || json.Unmarshal(first.Body.Bytes(), &envelope) != nil || envelope.Error.Details.ID == "" {
		t.Fatalf("approval response status=%d body=%s", first.Code, first.Body.String())
	}
	decision := request(t, s, token, http.MethodPost, "/v1/approvals/"+envelope.Error.Details.ID+"/decision", `{"decision":"approve"}`)
	if decision.Code != http.StatusOK {
		t.Fatalf("decision status=%d body=%s", decision.Code, decision.Body.String())
	}
	modifiedBody := `{"name":"different","action":"remove","dry_run":true}`
	r := httptest.NewRequest(http.MethodPost, "/v1/qnap/qpkg/manage", strings.NewReader(modifiedBody))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set(approvalHeader, envelope.Error.Details.ID)
	retry := httptest.NewRecorder()
	s.Handler().ServeHTTP(retry, r)
	if retry.Code != http.StatusConflict || !strings.Contains(retry.Body.String(), `"approval_mismatch"`) {
		t.Fatalf("modified retry status=%d body=%s", retry.Code, retry.Body.String())
	}
}

func TestSensitiveApprovalExpiresBeforeRetry(t *testing.T) {
	s, token := testServer(t)
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	s.Approvals = approval.NewWithOptions(approval.Options{TTL: time.Minute, Now: func() time.Time { return now }, ID: func() string { return "expired-ticket" }})
	body := `{"name":"example","action":"remove","dry_run":true}`
	first := request(t, s, token, http.MethodPost, "/v1/qnap/qpkg/manage", body)
	if first.Code != http.StatusConflict {
		t.Fatalf("initial status=%d body=%s", first.Code, first.Body.String())
	}
	now = now.Add(time.Minute)
	r := httptest.NewRequest(http.MethodPost, "/v1/qnap/qpkg/manage", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set(approvalHeader, "expired-ticket")
	retry := httptest.NewRecorder()
	s.Handler().ServeHTTP(retry, r)
	if retry.Code != http.StatusConflict || !strings.Contains(retry.Body.String(), `"approval_expired"`) {
		t.Fatalf("expired retry status=%d body=%s", retry.Code, retry.Body.String())
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
