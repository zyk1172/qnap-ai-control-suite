package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
