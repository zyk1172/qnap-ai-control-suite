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
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	Status         Status     `json:"status"`
	Progress       float64    `json:"progress,omitempty"`
	Resource       string     `json:"resource,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	ExitCode       *int       `json:"exit_code,omitempty"`
	Result         any        `json:"result,omitempty"`
	Error          string     `json:"error,omitempty"`
	LogCount       int        `json:"log_count"`
	LogBytes       int        `json:"log_bytes"`
	LogsTruncated  bool       `json:"logs_truncated"`
	Logs           []string   `json:"-"`
	RequestID      string     `json:"-"`
	Operation      string     `json:"-"`
	IdempotencyKey string     `json:"-"`
	cancel         context.CancelFunc
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
	OnEvent       func(Event)
}

type Manager struct {
	mu            sync.RWMutex
	jobs          map[string]*Job
	maxHistory    int
	maxLogBytes   int
	maxConcurrent int
	journalPath   string
	onEvent       func(Event)
	semaphore     chan struct{}
	resourceLocks map[string]chan struct{}
	next          uint64
}

const (
	maxLogLines        = 1000
	defaultMaxLogBytes = 16 * 1024 * 1024
)

// New keeps the v1 constructor usable for callers that do not need a journal.
func New(maxHistory int) *Manager { return NewWithOptions(Options{MaxHistory: maxHistory}) }

func NewWithOptions(options Options) *Manager {
	if options.MaxHistory <= 0 {
		options.MaxHistory = 200
	}
	if options.MaxConcurrent <= 0 {
		options.MaxConcurrent = 4
	}
	manager := &Manager{
		jobs:          map[string]*Job{},
		maxHistory:    options.MaxHistory,
		maxLogBytes:   defaultMaxLogBytes,
		maxConcurrent: options.MaxConcurrent,
		journalPath:   options.JournalPath,
		onEvent:       options.OnEvent,
		semaphore:     make(chan struct{}, options.MaxConcurrent),
		resourceLocks: map[string]chan struct{}{},
	}
	manager.loadJournal()
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
	m.next++
	id := time.Now().UTC().Format("20060102T150405.000000000") + "-" + stringID(m.next)
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{ID: id, Kind: options.Kind, Status: Queued, Resource: options.Resource, CreatedAt: time.Now().UTC(), RequestID: options.RequestID, Operation: options.Operation, IdempotencyKey: idempotencyKey, cancel: cancel}
	m.jobs[id] = job
	m.trimLocked()
	m.persistLocked(job)
	snapshot := clone(*job)
	m.mu.Unlock()
	m.emit(Event{Job: snapshot})
	go m.run(job, ctx, fn)
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

func (m *Manager) Logs(id string, cursor, limit int) ([]string, int, bool, bool) {
	if cursor < 0 {
		cursor = 0
	}
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	if !ok {
		return nil, 0, false, false
	}
	if cursor > len(job.Logs) {
		cursor = len(job.Logs)
	}
	end := cursor + limit
	if end > len(job.Logs) {
		end = len(job.Logs)
	}
	return append([]string(nil), job.Logs[cursor:end]...), end, job.LogsTruncated, true
}

func (m *Manager) markRunning(job *Job) {
	m.mu.Lock()
	if job.Status != Queued {
		m.mu.Unlock()
		return
	}
	previous := job.Status
	now := time.Now().UTC()
	job.Status, job.StartedAt = Running, &now
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
	previous := job.Status
	now := time.Now().UTC()
	job.FinishedAt = &now
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		job.Status = Cancelled
		job.Error = context.Canceled.Error()
	} else if err != nil {
		job.Status = Failed
		job.Error = audit.RedactText(err.Error())
	} else {
		job.Status = Succeeded
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
	}
}

func (m *Manager) appendLog(job *Job, line string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job.LogsTruncated || len(job.Logs) >= maxLogLines {
		job.LogsTruncated = true
		return
	}
	remaining := m.maxLogBytes - job.LogBytes
	if remaining <= 0 {
		job.LogsTruncated = true
		return
	}
	if len(line) > remaining {
		line = line[:remaining]
		job.LogsTruncated = true
	}
	job.Logs = append(job.Logs, line)
	job.LogCount = len(job.Logs)
	job.LogBytes += len(line)
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

// journalJob excludes command output and log lines. Those can be large or
// secret-bearing; recovery needs lifecycle state only.
type journalJob struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	Status         Status     `json:"status"`
	Resource       string     `json:"resource,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	ExitCode       *int       `json:"exit_code,omitempty"`
	Error          string     `json:"error,omitempty"`
	LogCount       int        `json:"log_count"`
	LogBytes       int        `json:"log_bytes"`
	LogsTruncated  bool       `json:"logs_truncated"`
	RequestID      string     `json:"request_id,omitempty"`
	Operation      string     `json:"operation,omitempty"`
	IdempotencyKey string     `json:"idempotency_key,omitempty"`
}

func (m *Manager) persistLocked(job *Job) {
	if m.journalPath == "" {
		return
	}
	record := journalRecord{Version: 1, Job: journalJob{ID: job.ID, Kind: job.Kind, Status: job.Status, Resource: job.Resource, CreatedAt: job.CreatedAt, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt, ExitCode: job.ExitCode, Error: audit.RedactText(job.Error), LogCount: job.LogCount, LogBytes: job.LogBytes, LogsTruncated: job.LogsTruncated, RequestID: job.RequestID, Operation: job.Operation, IdempotencyKey: job.IdempotencyKey}}
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
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err == nil {
		_ = f.Sync()
	}
}

func (m *Manager) loadJournal() {
	if m.journalPath == "" {
		return
	}
	f, err := os.Open(m.journalPath)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var record journalRecord
		if json.Unmarshal(scanner.Bytes(), &record) != nil || record.Job.ID == "" {
			continue
		}
		item := record.Job
		m.jobs[item.ID] = &Job{ID: item.ID, Kind: item.Kind, Status: item.Status, Resource: item.Resource, CreatedAt: item.CreatedAt, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt, ExitCode: item.ExitCode, Error: item.Error, LogCount: item.LogCount, LogBytes: item.LogBytes, LogsTruncated: item.LogsTruncated, RequestID: item.RequestID, Operation: item.Operation, IdempotencyKey: item.IdempotencyKey, cancel: func() {}}
	}
	now := time.Now().UTC()
	for _, job := range m.jobs {
		if job.Status == Queued || job.Status == Running {
			job.Status = Interrupted
			job.FinishedAt = &now
			job.Error = "agent restarted before job completed"
			m.persistLocked(job)
		}
	}
	m.trimLocked()
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

// idempotencyHash keeps the caller's opaque retry key out of the in-memory
// journal representation while preserving equality checks across restarts.
func idempotencyHash(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
