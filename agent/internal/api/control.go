package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"qnap-ai-control-suite/agent/internal/approval"
	"qnap-ai-control-suite/agent/internal/audit"
	"qnap-ai-control-suite/agent/internal/jobs"
	"qnap-ai-control-suite/agent/internal/operations"
)

const (
	approvalHeader = "X-QACS-Approval-ID"
	maxPolicyBody  = 16*1024*1024 + 1
)

type operationContextKey struct{}
type approvalAuditContextKey struct{}

type approvalAuditContext struct {
	ID     string
	Ticket *approval.Ticket
	Status string
}

func (s *Server) operationControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, payload := operationPayload(r)
		op := s.Operations.Resolve(operations.Request{Method: r.Method, Path: r.URL.Path, Payload: payload})
		r = r.WithContext(context.WithValue(r.Context(), operationContextKey{}, op))
		recorder := &statusRecorder{ResponseWriter: w}
		defer func() { s.auditOperation(r, op, recorder.status) }()

		if s.requiresApproval(op) {
			binding := approval.BindingFor(r.Method, r.URL.Path, canonicalBody(body), op.Name)
			approvalID := strings.TrimSpace(r.Header.Get(approvalHeader))
			if approvalID == "" {
				ticket := s.Approvals.Create(rc(r).id, op, binding)
				r = withApprovalAudit(r, approvalAuditContext{ID: ticket.ID, Ticket: &ticket, Status: "approval_requested"})
				recorder.Header().Set(approvalHeader, ticket.ID)
				s.fail(recorder, r, http.StatusConflict, "approval_required", "operation requires one-time approval", ticket)
				return
			}
			consumed, err := s.Approvals.Consume(approvalID, binding)
			if err != nil {
				auditContext := approvalAuditContext{ID: approvalID, Status: approvalCode(err)}
				if previous, lookupErr := s.Approvals.Lookup(approvalID); lookupErr == nil {
					auditContext.Ticket = &previous
				}
				r = withApprovalAudit(r, auditContext)
				s.fail(recorder, r, http.StatusConflict, approvalCode(err), err.Error(), map[string]any{"approval_id": approvalID})
				return
			}
			r = withApprovalAudit(r, approvalAuditContext{ID: consumed.ID, Ticket: &consumed, Status: "approval_executed"})
		}
		next.ServeHTTP(recorder, r)
	})
}

func (s *Server) requiresApproval(op operations.Operation) bool {
	if op.Name == "approval.decision" || op.Risk == operations.Read {
		return false
	}
	switch s.Config.Approval.Mode {
	case "all_write":
		return true
	case "sensitive_only":
		return op.Risk == operations.Sensitive
	default:
		return false
	}
}

func (s *Server) approvalRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/decision") {
		s.fail(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "POST /v1/approvals/{approval_id}/decision is the optional decision endpoint", nil)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/approvals/"), "/decision")
	if id == "" || strings.Contains(id, "/") {
		s.fail(w, r, http.StatusBadRequest, "invalid_approval_id", "approval_id is required", nil)
		return
	}
	var req struct {
		Decision string `json:"decision"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Decision != "approve" && req.Decision != "deny" {
		s.fail(w, r, http.StatusBadRequest, "invalid_decision", "decision must be approve or deny", nil)
		return
	}
	ticket, err := s.Approvals.Decide(id, req.Decision == "approve")
	if err != nil {
		var previous *approval.Ticket
		if current, lookupErr := s.Approvals.Lookup(id); lookupErr == nil {
			previous = &current
		}
		s.auditApprovalDecision(r, id, req.Decision, approvalCode(err), previous)
		s.fail(w, r, http.StatusConflict, approvalCode(err), err.Error(), map[string]any{"approval_id": id})
		return
	}
	status := "approval_denied"
	if req.Decision == "approve" {
		status = "approval_approved"
	}
	s.auditApprovalDecision(r, id, req.Decision, status, &ticket)
	s.ok(w, r, ticket)
}

func (s *Server) startJob(r *http.Request, kind, resource string, fn func(context.Context, func(string)) (any, error)) (jobs.Job, bool) {
	op, _ := operationFor(r)
	if resource == "" {
		resource = op.Resource
	}
	return s.Jobs.StartWithOptions(jobs.StartOptions{Kind: kind, Resource: resource, RequestID: rc(r).id, Operation: op.Name, IdempotencyKey: strings.TrimSpace(r.Header.Get("Idempotency-Key"))}, fn)
}

func (s *Server) auditJobEvent(event jobs.Event) {
	job := event.Job
	action := job.Operation
	if action == "" {
		action = job.Kind
	}
	s.Audit.Write(audit.Event{RequestID: job.RequestID, Tool: "/v1/jobs", Action: action, Risk: string(operations.Write), Target: job.Resource, JobID: job.ID, Status: "job_" + string(job.Status), Args: map[string]any{"kind": job.Kind, "resource": job.Resource}, Error: job.Error})
}

func (s *Server) auditOperation(r *http.Request, op operations.Operation, status int) {
	if status == 0 {
		status = http.StatusOK
	}
	result := "success"
	if status == http.StatusConflict && op.Risk == operations.Sensitive {
		result = "approval_required"
	} else if status < 200 || status >= 300 {
		result = "failed"
	}
	event := audit.Event{RequestID: rc(r).id, Remote: r.RemoteAddr, Tool: r.URL.Path, Action: op.Name, Risk: string(op.Risk), Target: op.Target, Status: result, Args: map[string]any{"summary": op.Summary, "resource": op.Resource, "dry_run": op.DryRun}, DurationMS: time.Since(rc(r).started).Milliseconds()}
	if approvalEvent, ok := r.Context().Value(approvalAuditContextKey{}).(approvalAuditContext); ok {
		event.ApprovalID = approvalEvent.ID
		event.Status = approvalEvent.Status
		if approvalEvent.Ticket != nil {
			event.RequestID = approvalEvent.Ticket.RequestID
			event.ApprovalCreatedAt = &approvalEvent.Ticket.CreatedAt
			event.ApprovalDecisionAt = approvalEvent.Ticket.DecisionAt
		}
		if approvalEvent.Status == "approval_executed" {
			executedAt := time.Now().UTC()
			event.ApprovalExecutedAt = &executedAt
		}
	}
	s.Audit.Write(event)
}

func (s *Server) auditApprovalDecision(r *http.Request, id, decision, status string, ticket *approval.Ticket) {
	event := audit.Event{RequestID: rc(r).id, Remote: r.RemoteAddr, Tool: r.URL.Path, Action: "approval.decision", Status: status, ApprovalID: id, Args: map[string]any{"decision": decision}, DurationMS: time.Since(rc(r).started).Milliseconds()}
	if ticket != nil {
		event.RequestID = ticket.RequestID
		event.Action = ticket.Operation
		event.Risk = string(ticket.Risk)
		event.Target = ticket.Target
		event.ApprovalCreatedAt = &ticket.CreatedAt
		event.ApprovalDecisionAt = ticket.DecisionAt
	}
	s.Audit.Write(event)
}

func withApprovalAudit(r *http.Request, value approvalAuditContext) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), approvalAuditContextKey{}, value))
}

func (s *Server) audit(r *http.Request, action, status string, args any, duration int64, errorText string) {
	if _, managed := operationFor(r); managed {
		return
	}
	s.Audit.Write(audit.Event{RequestID: rc(r).id, Remote: r.RemoteAddr, Tool: r.URL.Path, Action: action, Status: status, Args: args, DurationMS: duration, Error: errorText})
}

func operationFor(r *http.Request) (operations.Operation, bool) {
	op, ok := r.Context().Value(operationContextKey{}).(operations.Operation)
	return op, ok
}

func operationPayload(r *http.Request) ([]byte, map[string]any) {
	if r.Body == nil {
		return nil, map[string]any{}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPolicyBody))
	if err != nil {
		return nil, map[string]any{}
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	payload := map[string]any{}
	_ = json.Unmarshal(body, &payload)
	return body, payload
}

func canonicalBody(body []byte) []byte {
	if len(body) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(body, &value) != nil {
		return body
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return body
	}
	return canonical
}

func approvalCode(err error) string {
	switch {
	case errors.Is(err, approval.ErrExpired):
		return "approval_expired"
	case errors.Is(err, approval.ErrNotApproved):
		return "approval_not_approved"
	case errors.Is(err, approval.ErrDenied):
		return "approval_denied"
	case errors.Is(err, approval.ErrUsed):
		return "approval_used"
	case errors.Is(err, approval.ErrBindingMismatch):
		return "approval_mismatch"
	default:
		return "approval_not_found"
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}
