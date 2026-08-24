package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Logger struct {
	Enabled       bool
	Path          string
	RedactSecrets bool
	mu            sync.Mutex
}
type Event struct {
	TS         time.Time `json:"ts"`
	RequestID  string    `json:"request_id,omitempty"`
	Remote     string    `json:"remote,omitempty"`
	Tool       string    `json:"tool,omitempty"`
	Action     string    `json:"action,omitempty"`
	Risk       string    `json:"risk,omitempty"`
	Target     string    `json:"target,omitempty"`
	JobID      string    `json:"job_id,omitempty"`
	Status     string    `json:"status"`
	Args       any       `json:"args,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
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
	if l.RedactSecrets {
		event.Args = Sanitize(event.Args)
		event.Error = RedactText(event.Error)
	}
	b, err := json.Marshal(event)
	if err == nil {
		_, _ = f.Write(append(b, '\n'))
	}
}

// Sanitize converts arbitrary request data into JSON-compatible values and
// removes values that are not useful in an audit trail but could be secrets.
// It is deliberately conservative for script, stdin, encoded content, and env.
func Sanitize(value any) any {
	if value == nil {
		return nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return "[UNSERIALIZABLE]"
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return "[UNSERIALIZABLE]"
	}
	return sanitizeValue(decoded, "")
}

func sanitizeValue(value any, key string) any {
	if secretKey(key) {
		return "[REDACTED]"
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for name, item := range typed {
			out[name] = sanitizeValue(item, name)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = sanitizeValue(item, key)
		}
		return out
	case string:
		return RedactText(typed)
	default:
		return value
	}
}

func secretKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	if key == "script" || key == "stdin" || key == "stdin_base64" || key == "content_base64" || key == "env" || key == "authorization" || key == "cookie" {
		return true
	}
	for _, needle := range []string{"token", "password", "passwd", "secret", "credential", "api_key", "apikey", "private_key"} {
		if strings.Contains(key, needle) {
			return true
		}
	}
	return false
}

var (
	bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/-]+`)
	flagPattern   = regexp.MustCompile(`(?i)(--?(?:token|password|passwd|secret|api[_-]?key|credential)(?:=|\s+))[^\s]+`)
	assignPattern = regexp.MustCompile(`(?i)\b(?:token|password|passwd|secret|api[_-]?key|credential)\s*[:=]\s*[^\s,;]+`)
)

// RedactText is exported so jobs and API error paths can apply identical rules
// before persisting their own small metadata records.
func RedactText(value string) string {
	value = bearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = flagPattern.ReplaceAllString(value, "$1[REDACTED]")
	return assignPattern.ReplaceAllStringFunc(value, func(match string) string {
		if index := strings.IndexAny(match, ":="); index >= 0 {
			return match[:index+1] + "[REDACTED]"
		}
		return "[REDACTED]"
	})
}
