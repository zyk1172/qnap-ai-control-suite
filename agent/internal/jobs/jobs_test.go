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
