package jobs

import (
	"context"
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
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, ok := m.Get(job.ID)
		if ok && current.Status == Succeeded {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not complete")
}

func TestLogsAreBoundedAndPagedOutsideJobMetadata(t *testing.T) {
	m := New(10)
	job := m.Start("test", func(_ context.Context, log func(string)) (any, error) {
		log("first")
		log("second")
		return "done", nil
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, ok := m.Get(job.ID)
		if ok && current.Status == Succeeded {
			if current.LogCount != 2 || len(current.Logs) != 0 || current.LogBytes != len("first")+len("second") {
				t.Fatalf("metadata unexpectedly includes logs: %#v", current)
			}
			lines, next, truncated, ok := m.Logs(job.ID, 0, 1)
			if !ok || truncated || next != 1 || len(lines) != 1 || lines[0] != "first" {
				t.Fatalf("page=%#v next=%d truncated=%v ok=%v", lines, next, truncated, ok)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not complete")
}
