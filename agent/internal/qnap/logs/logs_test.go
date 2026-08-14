package logs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPageFiltersTimestampedAuditLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	content := strings.Join([]string{
		`{"ts":"2026-08-14T00:00:00Z","action":"before"}`,
		`{"ts":"2026-08-14T01:00:00Z","action":"match"}`,
		`unstructured service message`,
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 8, 14, 0, 30, 0, 0, time.UTC)
	until := time.Date(2026, 8, 14, 1, 30, 0, 0, time.UTC)
	page, err := (Service{AuditPath: path}).Page("audit", 100, 0, "", since, until)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Lines) != 1 || !strings.Contains(page.Lines[0], "match") {
		t.Fatalf("unexpected page: %#v", page)
	}
	if !page.TimeFiltered || page.UnparseableLines != 1 {
		t.Fatalf("expected timestamp filter accounting: %#v", page)
	}
}

func TestPageRejectsInvalidTimeWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := (Service{AuditPath: path}).Page("audit", 1, 0, "", time.Now(), time.Now().Add(-time.Second))
	if err == nil || !strings.Contains(err.Error(), "until") {
		t.Fatalf("expected invalid window error, got %v", err)
	}
}

func TestLineTimestampParsesSyslogPrefix(t *testing.T) {
	prefix := time.Now().Format("Jan _2 15:04:05")
	if _, ok := lineTimestamp(prefix + " host service: ready"); !ok {
		t.Fatal("expected syslog timestamp to parse")
	}
}
