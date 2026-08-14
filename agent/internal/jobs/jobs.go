package jobs

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

type Status string

const (
	Queued    Status = "queued"
	Running   Status = "running"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

type Job struct {
	ID, Kind              string
	Status                Status
	CreatedAt             time.Time
	StartedAt, FinishedAt *time.Time
	Result                any
	Error                 string
	Logs                  []string
	cancel                context.CancelFunc
}
type Manager struct {
	mu         sync.RWMutex
	jobs       map[string]*Job
	maxHistory int
	next       uint64
}

func New(maxHistory int) *Manager {
	if maxHistory <= 0 {
		maxHistory = 200
	}
	return &Manager{jobs: map[string]*Job{}, maxHistory: maxHistory}
}
func (m *Manager) Start(kind string, fn func(context.Context, func(string)) (any, error)) Job {
	m.mu.Lock()
	m.next++
	id := time.Now().UTC().Format("20060102T150405.000000000") + "-" + stringID(m.next)
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{ID: id, Kind: kind, Status: Queued, CreatedAt: time.Now().UTC(), cancel: cancel}
	m.jobs[id] = job
	m.trimLocked()
	m.mu.Unlock()
	go func() {
		m.mu.Lock()
		now := time.Now().UTC()
		job.Status = Running
		job.StartedAt = &now
		m.mu.Unlock()
		result, err := fn(ctx, func(line string) {
			m.mu.Lock()
			defer m.mu.Unlock()
			if len(job.Logs) < 1000 {
				job.Logs = append(job.Logs, line)
			}
		})
		m.mu.Lock()
		defer m.mu.Unlock()
		end := time.Now().UTC()
		job.FinishedAt = &end
		if errors.Is(err, context.Canceled) {
			job.Status = Cancelled
			job.Error = err.Error()
		} else if err != nil {
			job.Status = Failed
			job.Error = err.Error()
		} else {
			job.Status = Succeeded
			job.Result = result
		}
	}()
	return clone(*job)
}
func (m *Manager) Get(id string) (Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return clone(*j), true
}
func (m *Manager) List() []Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, clone(*j))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}
func (m *Manager) Cancel(id string) bool {
	m.mu.RLock()
	j, ok := m.jobs[id]
	m.mu.RUnlock()
	if ok {
		j.cancel()
	}
	return ok
}
func (m *Manager) trimLocked() {
	if len(m.jobs) <= m.maxHistory {
		return
	}
	var oldest *Job
	for _, j := range m.jobs {
		if j.Status == Running || j.Status == Queued {
			continue
		}
		if oldest == nil || j.CreatedAt.Before(oldest.CreatedAt) {
			oldest = j
		}
	}
	if oldest != nil {
		delete(m.jobs, oldest.ID)
	}
}
func clone(j Job) Job { j.cancel = nil; j.Logs = append([]string(nil), j.Logs...); return j }
func stringID(n uint64) string {
	const chars = "0123456789abcdef"
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 16)
	for n > 0 {
		b = append([]byte{chars[n&15]}, b...)
		n >>= 4
	}
	return string(b)
}
