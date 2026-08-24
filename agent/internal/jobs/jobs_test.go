package jobs

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartReturnsQueuedSnapshot(t *testing.T) {
	m := New(10)
	release := make(chan struct{})
	job := m.Start("test", func(context.Context, func(string)) (any, error) {
		<-release
		return "done", nil
	})
	if job.Status != Queued || job.StartedAt != nil || job.FinishedAt != nil {
		t.Fatalf("expected immutable queued snapshot, got %#v", job)
	}
	close(release)
	waitForStatus(t, m, job.ID, Succeeded)
}

func TestLogsAreBoundedAndPagedOutsideJobMetadata(t *testing.T) {
	m := New(10)
	job := m.Start("test", func(_ context.Context, log func(string)) (any, error) {
		log("first")
		log("second")
		return "done", nil
	})
	waitForStatus(t, m, job.ID, Succeeded)
	current, ok := m.Get(job.ID)
	if !ok || current.LogCount != 2 || len(current.Logs) != 0 || current.LogBytes != len("first")+len("second") {
		t.Fatalf("metadata unexpectedly includes logs: %#v", current)
	}
	lines, next, truncated, ok := m.Logs(job.ID, 0, 1)
	if !ok || truncated || next != 1 || len(lines) != 1 || lines[0] != "first" {
		t.Fatalf("page=%#v next=%d truncated=%v ok=%v", lines, next, truncated, ok)
	}
}

func TestIdempotencyAndResourceLock(t *testing.T) {
	m := NewWithOptions(Options{MaxHistory: 10, MaxConcurrent: 4})
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	var running int32
	var peak int32
	fn := func(context.Context, func(string)) (any, error) {
		current := atomic.AddInt32(&running, 1)
		for {
			old := atomic.LoadInt32(&peak)
			if current <= old || atomic.CompareAndSwapInt32(&peak, old, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		atomic.AddInt32(&running, -1)
		return "done", nil
	}
	first, reused := m.StartWithOptions(StartOptions{Kind: "one", Resource: "docker", IdempotencyKey: "same"}, fn)
	if reused {
		t.Fatal("first start reused")
	}
	if duplicate, reused := m.StartWithOptions(StartOptions{Kind: "one", Resource: "docker", IdempotencyKey: "same"}, fn); !reused || duplicate.ID != first.ID {
		t.Fatalf("duplicate=%#v reused=%v", duplicate, reused)
	}
	second, _ := m.StartWithOptions(StartOptions{Kind: "two", Resource: "docker"}, fn)
	<-started
	select {
	case <-started:
		t.Fatal("same resource ran concurrently")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	waitForStatus(t, m, first.ID, Succeeded)
	waitForStatus(t, m, second.ID, Succeeded)
	if peak != 1 {
		t.Fatalf("peak concurrent resource jobs=%d", peak)
	}
}

func TestCancellationWinsOverWrappedCommandFailure(t *testing.T) {
	m := New(10)
	started := make(chan struct{})
	job := m.Start("cancel", func(ctx context.Context, _ func(string)) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, errors.New("process exited with status 137")
	})
	<-started
	if !m.Cancel(job.ID) {
		t.Fatal("cancel returned false")
	}
	waitForStatus(t, m, job.ID, Cancelled)
}

func TestJournalMarksUnfinishedJobsInterruptedAfterRestart(t *testing.T) {
	path := t.TempDir() + "/jobs.jsonl"
	manager := NewWithOptions(Options{MaxHistory: 10, JournalPath: path})
	release := make(chan struct{})
	job, reused := manager.StartWithOptions(StartOptions{Kind: "long", IdempotencyKey: "opaque-retry-key"}, func(context.Context, func(string)) (any, error) {
		<-release
		return nil, nil
	})
	if reused {
		t.Fatal("first job unexpectedly reused")
	}
	waitForStatus(t, manager, job.ID, Running)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(journal), "opaque-retry-key") {
		t.Fatal("journal persisted the raw idempotency key")
	}
	restarted := NewWithOptions(Options{MaxHistory: 10, JournalPath: path})
	current, ok := restarted.Get(job.ID)
	if !ok || current.Status != Interrupted || current.FinishedAt == nil {
		t.Fatalf("recovered job=%#v exists=%v", current, ok)
	}
	close(release)
	waitForStatus(t, manager, job.ID, Succeeded)
}

func TestConcurrencyLimit(t *testing.T) {
	m := NewWithOptions(Options{MaxHistory: 10, MaxConcurrent: 1})
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	fn := func(context.Context, func(string)) (any, error) { started <- struct{}{}; <-release; return nil, nil }
	first := m.Start("one", fn)
	second := m.Start("two", fn)
	<-started
	select {
	case <-started:
		t.Fatal("max concurrent jobs was exceeded")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	waitForStatus(t, m, first.ID, Succeeded)
	waitForStatus(t, m, second.ID, Succeeded)
}

func waitForStatus(t *testing.T, manager *Manager, id string, wanted Status) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if job, ok := manager.Get(id); ok && job.Status == wanted {
			return
		}
		time.Sleep(time.Millisecond)
	}
	job, _ := manager.Get(id)
	t.Fatalf("job %s status=%s, wanted=%s", id, job.Status, wanted)
}
