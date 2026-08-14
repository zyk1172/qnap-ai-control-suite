package logs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Service struct{ AuditPath, ServicePath string }
type Source struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
}
type Page struct {
	Lines      []string `json:"lines"`
	NextCursor int      `json:"next_cursor"`
	Total      int      `json:"total"`
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
	page, err := s.Page(name, limit, 0, "")
	return page.Lines, err
}
func (s Service) Page(name string, limit, cursor int, query string) (Page, error) {
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
	if query != "" {
		filtered := []string{}
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
				filtered = append(filtered, line)
			}
		}
		lines = filtered
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
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
	return Page{Lines: lines[cursor:end], NextCursor: end, Total: len(lines)}, nil
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
