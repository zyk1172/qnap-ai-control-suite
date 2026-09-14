package jobs

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"qnap-ai-control-suite/agent/internal/audit"
	qexec "qnap-ai-control-suite/agent/internal/exec"
)

type Status string

const (
	Queued      Status = "queued"
	Running     Status = "running"
	Succeeded   Status = "succeeded"
	Failed      Status = "failed"
	Cancelled   Status = "cancelled"
	Interrupted Status = "interrupted"
)

type Job struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Status          Status     `json:"status"`
	Progress        float64    `json:"progress,omitempty"`
	Resource        string     `json:"resource,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ExitCode        *int       `json:"exit_code,omitempty"`
	Result          any        `json:"result,omitempty"`
	ResultTruncated bool       `json:"result_truncated,omitempty"`
	Error           string     `json:"error,omitempty"`
	LogCount        int        `json:"log_count"`
	LogBytes        int        `json:"log_bytes"`
	LogsTruncated   bool       `json:"logs_truncated"`
	Recovered       bool       `json:"recovered,omitempty"`
	RecoveryStatus  string     `json:"recovery_status,omitempty"`
	Retriable       bool       `json:"retriable,omitempty"`
	Logs            []string   `json:"-"`
	RequestID       string     `json:"-"`
	Operation       string     `json:"-"`
	IdempotencyKey  string     `json:"-"`
	cancel          context.CancelFunc
}

type StartOptions struct {
	Kind           string
	Resource       string
	RequestID      string
	Operation      string
	IdempotencyKey string
}

type Event struct {
	Job      Job
	Previous Status
}

type Options struct {
	MaxHistory    int
	MaxConcurrent int
	JournalPath   string
	SnapshotPath  string
	LogDir        string
	CompactBytes  int64
	RedactSecrets bool
	OnEvent       func(Event)
}

type Manager struct {
	mu             sync.RWMutex
	jobs           map[string]*Job
	maxHistory     int
	maxLogBytes    int
	maxConcurrent  int
	journalPath    string
	snapshotPath   string
	logDir         string
	compactBytes   int64
	redactSecrets  bool
	journalRecords int
	onEvent        func(Event)
	semaphore      chan struct{}
	resourceLocks  map[string]chan struct{}
	next           uint64
	runWG          sync.WaitGroup
	stopping       bool
}

const (
	maxLogLines             = 1000
	defaultMaxLogBytes      = 16 * 1024 * 1024
	defaultCompactBytes     = 32 * 1024 * 1024
	maxPersistedResultBytes = 256 * 1024
)

// New keeps the v1 constructor usable for callers that do not need durable state.
func New(maxHistory int) *Manager { return NewWithOptions(Options{MaxHistory: maxHistory}) }

func NewWithOptions(options Options) *Manager {
	if options.MaxHistory <= 0 {
		options.MaxHistory = 200
	}
	if options.MaxConcurrent <= 0 {
		options.MaxConcurrent = 4
	}
	if options.CompactBytes <= 0 {
		options.CompactBytes = defaultCompactBytes
	}
	if options.JournalPath != "" {
		if options.SnapshotPath == "" {
			options.SnapshotPath = options.JournalPath + ".snapshot.json"
		}
		if options.LogDir == "" {
			options.LogDir = options.JournalPath + ".logs"
		}
	}
	manager := &Manager{
		jobs:          map[string]*Job{},
		maxHistory:    options.MaxHistory,
		maxLogBytes:   defaultMaxLogBytes,
		maxConcurrent: options.MaxConcurrent,
		journalPath:   options.JournalPath,
		snapshotPath:  options.SnapshotPath,
		logDir:        options.LogDir,
		compactBytes:  options.CompactBytes,
		redactSecrets: options.RedactSecrets,
		onEvent:       options.OnEvent,
		semaphore:     make(chan struct{}, options.MaxConcurrent),
		resourceLocks: map[string]chan struct{}{},
	}
	manager.loadState()
	return manager
}

func (m *Manager) Start(kind string, fn func(context.Context, func(string)) (any, error)) Job {
	job, _ := m.StartWithOptions(StartOptions{Kind: kind}, fn)
	return job
}

// StartWithOptions returns reused=true when a live or retained job has the
// same caller-provided idempotency key.
func (m *Manager) StartWithOptions(options StartOptions, fn func(context.Context, func(string)) (any, error)) (Job, bool) {
	if options.Kind == "" {
		options.Kind = "job"
	}
	idempotencyKey := idempotencyHash(options.IdempotencyKey)
	m.mu.Lock()
	if idempotencyKey != "" {
		for _, existing := range m.jobs {
			if existing.IdempotencyKey == idempotencyKey {
				snapshot := clone(*existing)
				m.mu.Unlock()
				return snapshot, true
			}
		}
	}
	if m.stopping {
		m.next++
		now := time.Now().UTC()
		job := Job{ID: now.Format("20060102T150405.000000000") + "-" + stringID(m.next), Kind: options.Kind, Status: Failed, CreatedAt: now, FinishedAt: &now, UpdatedAt: now, Error: "job manager is shutting down"}
		m.mu.Unlock()
		return job, false
	}
	m.next++
	now := time.Now().UTC()
	id := now.Format("20060102T150405.000000000") + "-" + stringID(m.next)
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{ID: id, Kind: options.Kind, Status: Queued, Resource: options.Resource, CreatedAt: now, UpdatedAt: now, RequestID: options.RequestID, Operation: options.Operation, IdempotencyKey: idempotencyKey, cancel: cancel}
	m.jobs[id] = job
	m.trimLocked()
	m.persistLocked(job)
	m.runWG.Add(1)
	snapshot := clone(*job)
	m.mu.Unlock()
	m.emit(Event{Job: snapshot})
	go func() {
		defer m.runWG.Done()
		m.run(job, ctx, fn)
	}()
	return snapshot, false
}

func (m *Manager) run(job *Job, ctx context.Context, fn func(context.Context, func(string)) (any, error)) {
	if !m.acquireResource(ctx, job.Resource) {
		m.finish(job, nil, context.Canceled, ctx)
		return
	}
	if job.Resource != "" {
		defer m.releaseResource(job.Resource)
	}
	select {
	case m.semaphore <- struct{}{}:
		defer func() { <-m.semaphore }()
	case <-ctx.Done():
		m.finish(job, nil, context.Canceled, ctx)
		return
	}
	m.markRunning(job)
	result, err := fn(ctx, func(line string) { m.appendLog(job, line) })
	m.finish(job, result, err, ctx)
}

func (m *Manager) Get(id string) (Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return clone(*job), true
}

func (m *Manager) List() []Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		out = append(out, clone(*job))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok || terminal(job.Status) {
		m.mu.Unlock()
		return false
	}
	job.cancel()
	m.mu.Unlock()
	return true
}

// Shutdown cancels active Job contexts and waits for their executors to exit.
// It intentionally leaves queued/running Job records unchanged so the next
// process can recover them as interrupted instead of recording a misleading
// successful or cancelled result during service restart.
func (m *Manager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	m.stopping = true
	cancels := make([]context.CancelFunc, 0)
	for _, job := range m.jobs {
		if !terminal(job.Status) && job.cancel != nil {
			cancels = append(cancels, job.cancel)
		}
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		m.runWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Logs(id string, cursor, limit int) ([]string, int, bool, bool) {
	if cursor < 0 {
		cursor = 0
	}
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	m.mu.RLock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.RUnlock()
		return nil, 0, false, false
	}
	logs := append([]string(nil), job.Logs...)
	count := job.LogCount
	truncated := job.LogsTruncated
	m.mu.RUnlock()
	if len(logs) == 0 && count > 0 && m.logDir != "" {
		logs = m.readLogFile(id)
	}
	if cursor > len(logs) {
		cursor = len(logs)
	}
	end := cursor + limit
	if end > len(logs) {
		end = len(logs)
	}
	return append([]string(nil), logs[cursor:end]...), end, truncated, true
}

func (m *Manager) markRunning(job *Job) {
	m.mu.Lock()
	if job.Status != Queued {
		m.mu.Unlock()
		return
	}
	previous := job.Status
	now := time.Now().UTC()
	job.Status, job.StartedAt, job.UpdatedAt = Running, &now, now
	m.persistLocked(job)
	event := Event{Job: clone(*job), Previous: previous}
	m.mu.Unlock()
	m.emit(event)
}

func (m *Manager) finish(job *Job, result any, err error, ctx context.Context) {
	m.mu.Lock()
	if terminal(job.Status) {
		m.mu.Unlock()
		return
	}
	if m.stopping {
		m.mu.Unlock()
		return
	}
	previous := job.Status
	now := time.Now().UTC()
	job.FinishedAt, job.UpdatedAt = &now, now
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		job.Status = Cancelled
		job.Error = context.Canceled.Error()
	} else if err != nil {
		job.Status = Failed
		job.Error = audit.RedactText(err.Error())
	} else {
		job.Status = Succeeded
		if command, ok := result.(qexec.Result); ok {
			if m.redactSecrets {
				command.Argv = redactStrings(command.Argv)
				command.Stdout = audit.RedactText(command.Stdout)
				command.Stderr = audit.RedactText(command.Stderr)
			}
			result = command
		} else if m.redactSecrets {
			result = audit.Sanitize(result)
		}
		job.Result = result
	}
	if command, ok := result.(qexec.Result); ok {
		exitCode := command.ExitCode
		job.ExitCode = &exitCode
	}
	m.trimLocked()
	m.persistLocked(job)
	event := Event{Job: clone(*job), Previous: previous}
	m.mu.Unlock()
	m.emit(event)
}

func (m *Manager) acquireResource(ctx context.Context, resource string) bool {
	if resource == "" {
		return true
	}
	m.mu.Lock()
	lock := m.resourceLocks[resource]
	if lock == nil {
		lock = make(chan struct{}, 1)
		m.resourceLocks[resource] = lock
	}
	m.mu.Unlock()
	select {
	case lock <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (m *Manager) releaseResource(resource string) {
	if resource == "" {
		return
	}
	m.mu.RLock()
	lock := m.resourceLocks[resource]
	m.mu.RUnlock()
	if lock != nil {
		<-lock
	}
}

func (m *Manager) trimLocked() {
	for len(m.jobs) > m.maxHistory {
		var oldest *Job
		for _, job := range m.jobs {
			if !terminal(job.Status) {
				continue
			}
			if oldest == nil || job.CreatedAt.Before(oldest.CreatedAt) {
				oldest = job
			}
		}
		if oldest == nil {
			return
		}
		delete(m.jobs, oldest.ID)
		if m.logDir != "" {
			_ = os.Remove(m.logPath(oldest.ID))
		}
	}
}

func (m *Manager) appendLog(job *Job, line string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.redactSecrets {
		line = audit.RedactText(line)
	}
	if job.LogsTruncated || len(job.Logs) >= maxLogLines {
		if !job.LogsTruncated {
			job.LogsTruncated = true
			job.UpdatedAt = time.Now().UTC()
			m.persistLocked(job)
		}
		return
	}
	remaining := m.maxLogBytes - job.LogBytes
	if remaining <= 0 {
		job.LogsTruncated = true
		job.UpdatedAt = time.Now().UTC()
		m.persistLocked(job)
		return
	}
	if len(line) > remaining {
		line = line[:remaining]
		job.LogsTruncated = true
	}
	job.Logs = append(job.Logs, line)
	job.LogCount = len(job.Logs)
	job.LogBytes += len(line)
	job.UpdatedAt = time.Now().UTC()
	m.appendPersistentLogLocked(job.ID, line)
	m.persistLocked(job)
}

func (m *Manager) emit(event Event) {
	if m.onEvent != nil {
		m.onEvent(event)
	}
}

func terminal(status Status) bool {
	return status == Succeeded || status == Failed || status == Cancelled || status == Interrupted
}

type journalRecord struct {
	Version int        `json:"version"`
	Job     journalJob `json:"job"`
}

type journalJob struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Status          Status     `json:"status"`
	Resource        string     `json:"resource,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ExitCode        *int       `json:"exit_code,omitempty"`
	Result          any        `json:"result,omitempty"`
	ResultTruncated bool       `json:"result_truncated,omitempty"`
	Error           string     `json:"error,omitempty"`
	LogCount        int        `json:"log_count"`
	LogBytes        int        `json:"log_bytes"`
	LogsTruncated   bool       `json:"logs_truncated"`
	Recovered       bool       `json:"recovered,omitempty"`
	RecoveryStatus  string     `json:"recovery_status,omitempty"`
	Retriable       bool       `json:"retriable,omitempty"`
	RequestID       string     `json:"request_id,omitempty"`
	Operation       string     `json:"operation,omitempty"`
	IdempotencyKey  string     `json:"idempotency_key,omitempty"`
}

type snapshotState struct {
	Version int          `json:"version"`
	Jobs    []journalJob `json:"jobs"`
}

func (m *Manager) journalJob(job *Job) journalJob {
	result, truncated := persistedResult(job.Result)
	return journalJob{ID: job.ID, Kind: job.Kind, Status: job.Status, Resource: job.Resource, CreatedAt: job.CreatedAt, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt, UpdatedAt: job.UpdatedAt, ExitCode: job.ExitCode, Result: result, ResultTruncated: job.ResultTruncated || truncated, Error: audit.RedactText(job.Error), LogCount: job.LogCount, LogBytes: job.LogBytes, LogsTruncated: job.LogsTruncated, Recovered: job.Recovered, RecoveryStatus: job.RecoveryStatus, Retriable: job.Retriable, RequestID: job.RequestID, Operation: job.Operation, IdempotencyKey: job.IdempotencyKey}
}

func persistedResult(value any) (any, bool) {
	if value == nil {
		return nil, false
	}
	if command, ok := value.(qexec.Result); ok {
		return map[string]any{"exit_code": command.ExitCode, "duration_ms": command.DurationMS, "dry_run": command.DryRun, "stdout_bytes": len(command.Stdout), "stderr_bytes": len(command.Stderr), "stdout_truncated": command.StdoutTruncated, "stderr_truncated": command.StderrTruncated}, false
	}
	sanitized := audit.Sanitize(value)
	b, err := json.Marshal(sanitized)
	if err != nil {
		return nil, true
	}
	if len(b) > maxPersistedResultBytes {
		return nil, true
	}
	return sanitized, false
}

func (m *Manager) persistLocked(job *Job) {
	if m.journalPath == "" {
		return
	}
	record := journalRecord{Version: 2, Job: m.journalJob(job)}
	b, err := json.Marshal(record)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.journalPath), 0700); err != nil {
		return
	}
	f, err := os.OpenFile(m.journalPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	_, writeErr := f.Write(append(b, '\n'))
	if writeErr == nil {
		_ = f.Sync()
		m.journalRecords++
	}
	_ = f.Close()
	if writeErr == nil {
		m.compactIfNeededLocked()
	}
}

func (m *Manager) compactIfNeededLocked() {
	if m.snapshotPath == "" || m.compactBytes <= 0 {
		return
	}
	info, err := os.Stat(m.journalPath)
	if err != nil || info.Size() < m.compactBytes {
		return
	}
	_ = m.compactLocked()
}

func (m *Manager) compactLocked() error {
	if m.snapshotPath == "" || m.journalPath == "" {
		return nil
	}
	jobs := make([]journalJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, m.journalJob(job))
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.Before(jobs[j].CreatedAt) })
	state := snapshotState{Version: 2, Jobs: jobs}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.snapshotPath), 0700); err != nil {
		return err
	}
	tmp := m.snapshotPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, m.snapshotPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	jf, err := os.OpenFile(m.journalPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_ = jf.Sync()
	if err := jf.Close(); err != nil {
		return err
	}
	m.journalRecords = 0
	return nil
}

func (m *Manager) loadState() {
	if m.journalPath == "" {
		return
	}
	m.loadSnapshot()
	m.loadJournal()
	now := time.Now().UTC()
	for _, job := range m.jobs {
		if job.Status == Queued || job.Status == Running {
			job.Status = Interrupted
			job.FinishedAt = &now
			job.UpdatedAt = now
			job.Error = "agent restarted before job completed"
			job.Recovered = true
			job.RecoveryStatus = "needs_inspection"
			job.Retriable = false
			m.persistLocked(job)
		}
	}
	m.trimLocked()
}

func (m *Manager) loadSnapshot() {
	if m.snapshotPath == "" {
		return
	}
	data, err := os.ReadFile(m.snapshotPath)
	if err != nil {
		return
	}
	var state snapshotState
	if json.Unmarshal(data, &state) != nil {
		return
	}
	for _, item := range state.Jobs {
		m.restoreJournalJob(item)
	}
}

func (m *Manager) loadJournal() {
	f, err := os.Open(m.journalPath)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 2*1024*1024)
	for scanner.Scan() {
		var record journalRecord
		if json.Unmarshal(scanner.Bytes(), &record) != nil || record.Job.ID == "" {
			continue
		}
		m.restoreJournalJob(record.Job)
		m.journalRecords++
	}
}

func (m *Manager) restoreJournalJob(item journalJob) {
	updated := item.UpdatedAt
	if updated.IsZero() {
		updated = item.CreatedAt
	}
	m.jobs[item.ID] = &Job{ID: item.ID, Kind: item.Kind, Status: item.Status, Resource: item.Resource, CreatedAt: item.CreatedAt, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt, UpdatedAt: updated, ExitCode: item.ExitCode, Result: item.Result, ResultTruncated: item.ResultTruncated, Error: item.Error, LogCount: item.LogCount, LogBytes: item.LogBytes, LogsTruncated: item.LogsTruncated, Recovered: item.Recovered, RecoveryStatus: item.RecoveryStatus, Retriable: item.Retriable, RequestID: item.RequestID, Operation: item.Operation, IdempotencyKey: item.IdempotencyKey, cancel: func() {}}
}

func (m *Manager) appendPersistentLogLocked(id, line string) {
	if m.logDir == "" {
		return
	}
	if err := os.MkdirAll(m.logDir, 0700); err != nil {
		return
	}
	f, err := os.OpenFile(m.logPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	data, err := json.Marshal(line)
	if err == nil {
		_, _ = f.Write(append(data, '\n'))
	}
	_ = f.Close()
}

func (m *Manager) readLogFile(id string) []string {
	f, err := os.Open(m.logPath(id))
	if err != nil {
		return nil
	}
	defer f.Close()
	out := make([]string, 0)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), m.maxLogBytes+1024)
	for scanner.Scan() && len(out) < maxLogLines {
		var line string
		if json.Unmarshal(scanner.Bytes(), &line) == nil {
			if m.redactSecrets {
				line = audit.RedactText(line)
			}
			out = append(out, line)
		}
	}
	return out
}

func (m *Manager) logPath(id string) string { return filepath.Join(m.logDir, id+".jsonl") }

func redactStrings(values []string) []string {
	redacted := make([]string, len(values))
	for index, value := range values {
		redacted[index] = audit.RedactText(value)
	}
	return redacted
}

func clone(job Job) Job {
	job.cancel = nil
	job.Logs = nil
	job.IdempotencyKey = ""
	return job
}

func stringID(value uint64) string {
	const chars = "0123456789abcdef"
	if value == 0 {
		return "0"
	}
	b := make([]byte, 0, 16)
	for value > 0 {
		b = append([]byte{chars[value&15]}, b...)
		value >>= 4
	}
	return string(b)
}

// idempotencyHash keeps the caller's opaque retry key out of durable state
// while preserving equality checks across restarts.
func idempotencyHash(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
