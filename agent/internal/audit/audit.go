package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logger struct {
	Enabled bool
	Path    string
	mu      sync.Mutex
}
type Event struct {
	TS                                      time.Time `json:"ts"`
	RequestID, Remote, Tool, Action, Status string
	Args                                    any    `json:"args,omitempty"`
	DurationMS                              int64  `json:"duration_ms"`
	Error                                   string `json:"error,omitempty"`
}

func (l *Logger) Write(event Event) {
	if !l.Enabled || l.Path == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.Path), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	event.TS = time.Now().UTC()
	b, err := json.Marshal(event)
	if err == nil {
		_, _ = f.Write(append(b, '\n'))
	}
}
