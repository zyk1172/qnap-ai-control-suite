package audit

import (
	"os"
	"strings"
	"testing"
)

func TestLoggerRedactsNestedSecrets(t *testing.T) {
	path := t.TempDir() + "/audit.jsonl"
	logger := Logger{Enabled: true, Path: path, RedactSecrets: true}
	logger.Write(Event{Action: "command.exec", Status: "failed", Args: map[string]any{
		"token":  "top-secret",
		"env":    map[string]string{"DB_PASSWORD": "top-secret"},
		"argv":   []string{"tool", "--password=top-secret"},
		"script": "echo top-secret",
	}, Error: "Bearer top-secret token=top-secret"})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "top-secret") || !strings.Contains(text, "[REDACTED]") || strings.Contains(text, "echo top-secret") {
		t.Fatalf("audit was not redacted: %s", text)
	}
}

func TestSanitizeKeepsOperationalTarget(t *testing.T) {
	value := Sanitize(map[string]any{"path": "/etc/config/app.conf", "content_base64": "AAE="}).(map[string]any)
	if value["path"] != "/etc/config/app.conf" || value["content_base64"] != "[REDACTED]" {
		t.Fatalf("unexpected sanitized value: %#v", value)
	}
}
