package logs

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Service struct{ AuditPath, ServicePath string }
type Source struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
}
type Page struct {
	Lines            []string `json:"lines"`
	NextCursor       int      `json:"next_cursor"`
	Total            int      `json:"total"`
	TimeFiltered     bool     `json:"time_filtered"`
	UnparseableLines int      `json:"unparseable_lines,omitempty"`
}

func (s Service) Sources() []Source {
	items := []Source{{Name: "audit", Path: s.AuditPath}, {Name: "service", Path: s.ServicePath}, {Name: "system", Path: "/var/log/messages"}, {Name: "kernel", Path: "/var/log/kern.log"}, {Name: "syslog", Path: "/var/log/syslog"}}
	for i := range items {
		_, err := os.Stat(items[i].Path)
		items[i].Available = err == nil
	}
	return items
}
func (s Service) Tail(name string, limit int) ([]string, error) {
	page, err := s.Page(name, limit, 0, "", time.Time{}, time.Time{})
	return page.Lines, err
}
func (s Service) Page(name string, limit, cursor int, query string, since, until time.Time) (Page, error) {
	if !since.IsZero() && !until.IsZero() && until.Before(since) {
		return Page{}, errors.New("until must not be before since")
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	path := ""
	for _, source := range s.Sources() {
		if source.Name == name {
			path = source.Path
			break
		}
	}
	if path == "" {
		return Page{}, errors.New("unknown log source")
	}
	lines, err := tail(path, 2000, 2*1024*1024)
	if err != nil {
		return Page{}, err
	}
	filtered := make([]string, 0, len(lines))
	unparseable := 0
	for _, line := range lines {
		if query != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
			continue
		}
		if !since.IsZero() || !until.IsZero() {
			ts, ok := lineTimestamp(line)
			if !ok {
				unparseable++
				continue
			}
			if (!since.IsZero() && ts.Before(since)) || (!until.IsZero() && ts.After(until)) {
				continue
			}
		}
		filtered = append(filtered, line)
	}
	lines = filtered
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(lines) {
		cursor = len(lines)
	}
	end := cursor + limit
	if end > len(lines) {
		end = len(lines)
	}
	return Page{Lines: lines[cursor:end], NextCursor: end, Total: len(lines), TimeFiltered: !since.IsZero() || !until.IsZero(), UnparseableLines: unparseable}, nil
}

func lineTimestamp(line string) (time.Time, bool) {
	var event struct {
		TS string `json:"ts"`
	}
	if json.Unmarshal([]byte(line), &event) == nil && event.TS != "" {
		if ts, err := time.Parse(time.RFC3339Nano, event.TS); err == nil {
			return ts.UTC(), true
		}
	}
	if len(line) >= 15 {
		if ts, err := time.ParseInLocation("Jan _2 15:04:05", line[:15], time.Local); err == nil {
			now := time.Now()
			ts = time.Date(now.Year(), ts.Month(), ts.Day(), ts.Hour(), ts.Minute(), ts.Second(), 0, time.Local)
			if ts.After(now.Add(24 * time.Hour)) {
				ts = ts.AddDate(-1, 0, 0)
			}
			return ts.UTC(), true
		}
	}
	return time.Time{}, false
}
func tail(path string, limit int, maxBytes int64) ([]string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := info.Size() - maxBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, 0); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}

// TailPath reads at most 2 MiB from a known local log path.
func TailPath(path string, limit int) ([]string, error) { return tail(path, limit, 2*1024*1024) }
