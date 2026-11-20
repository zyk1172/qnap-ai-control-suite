package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusRecorderPreservesFlusher(t *testing.T) {
	underlying := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: underlying}
	var _ http.Flusher = recorder

	if _, err := recorder.Write([]byte("ack")); err != nil {
		t.Fatal(err)
	}
	recorder.Flush()

	if !underlying.Flushed {
		t.Fatal("statusRecorder did not flush the underlying response writer")
	}
	if recorder.status != http.StatusOK {
		t.Fatalf("status=%d", recorder.status)
	}
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}

func TestCommandWriteDeadlineThroughOperationMiddleware(t *testing.T) {
	s, token := testServer(t)
	for _, tc := range []struct {
		name string
		body string
		want time.Duration
	}{
		{"default", `{"argv":["/bin/echo","ok"]}`, 0},
		{"explicit", `{"argv":["/bin/echo","ok"],"timeout_sec":120}`, 127 * time.Second},
		{"capped", `{"argv":["/bin/echo","ok"],"timeout_sec":1200}`, 10 * time.Minute},
		{"dry_run", `{"argv":["/bin/echo","ok"],"timeout_sec":120,"dry_run":true}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/exec", strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer "+token)
			w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			before := time.Now()
			s.Handler().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.want == 0 {
				if !w.deadline.IsZero() {
					t.Fatalf("unexpected write deadline %v", w.deadline)
				}
			} else if w.deadline.Before(before.Add(tc.want)) || w.deadline.After(time.Now().Add(tc.want)) {
				t.Fatalf("write deadline=%v, expected %v from dispatch", w.deadline, tc.want)
			}
		})
	}
}
