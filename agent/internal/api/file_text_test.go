package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"qnap-ai-control-suite/agent/internal/files"
)

func TestFileReadLinesPagesAcrossByteWindows(t *testing.T) {
	s, token := testServer(t)
	root := t.TempDir()
	s.Files = files.Service{Roots: []string{root}, MaxInlineBytes: 8}
	path := filepath.Join(root, "log.txt")
	if err := os.WriteFile(path, []byte("a\nabcdef\nabcdefghijk\nd\n"), 0600); err != nil {
		t.Fatal(err)
	}
	start := 0
	for _, tc := range []struct {
		lines     []string
		next      int
		truncated bool
	}{
		{[]string{"a"}, 1, true},
		{[]string{"abcdef"}, 2, true},
		{[]string{"abcdefgh"}, 3, true},
		{[]string{"d", ""}, 5, false},
		{[]string{}, 5, false},
	} {
		body, err := json.Marshal(map[string]any{"path": path, "start": start, "limit": 4})
		if err != nil {
			t.Fatal(err)
		}
		resp := request(t, s, token, http.MethodPost, "/v1/files/read-lines", string(body))
		var decoded struct{ Data files.LinePage }
		if err := json.Unmarshal(resp.Body.Bytes(), &decoded); err != nil || resp.Code != 200 {
			t.Fatalf("page at %d: status=%d err=%v body=%s", start, resp.Code, err, resp.Body.String())
		}
		page := decoded.Data
		if page.Start != start || page.Next != tc.next || page.Truncated != tc.truncated || !reflect.DeepEqual(page.Lines, tc.lines) {
			t.Fatalf("page at %d: got %+v, want lines=%q next=%d truncated=%v", start, page, tc.lines, tc.next, tc.truncated)
		}
		start = page.Next
	}
}

func TestFileReadLinesReportsExactByteBoundaryWhenLineLimitAlsoReached(t *testing.T) {
	s, token := testServer(t)
	root := t.TempDir()
	s.Files = files.Service{Roots: []string{root}, MaxInlineBytes: 8}
	path := filepath.Join(root, "log.txt")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		maxBytes  int
		start     int
		line      string
		truncated bool
	}{
		{2, 0, "a", true},
		{2, 1, "b", false},
		{2, 2, "", false},
	} {
		body, err := json.Marshal(map[string]any{"path": path, "max_bytes": tc.maxBytes, "start": tc.start, "limit": 1})
		if err != nil {
			t.Fatal(err)
		}
		resp := request(t, s, token, http.MethodPost, "/v1/files/read-lines", string(body))
		var decoded struct{ Data files.LinePage }
		if err := json.Unmarshal(resp.Body.Bytes(), &decoded); err != nil || resp.Code != 200 {
			t.Fatalf("start=%d status=%d err=%v body=%s", tc.start, resp.Code, err, resp.Body.String())
		}
		page := decoded.Data
		if page.Truncated != tc.truncated || page.Next != tc.start+1 || !reflect.DeepEqual(page.Lines, []string{tc.line}) {
			t.Fatalf("start=%d unexpected page %+v", tc.start, page)
		}
	}
}

func TestFileGrepKeepsLineNumbersAcrossBuffers(t *testing.T) {
	s, token := testServer(t)
	root := t.TempDir()
	s.Files = files.Service{Roots: []string{root}, MaxInlineBytes: 16384}
	path := filepath.Join(root, "grep.txt")
	line := strings.Repeat("x", 4094) + "match"
	if err := os.WriteFile(path, []byte("header\n"+line+"\nmatch at end"), 0600); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"path": path, "query": "match", "max_bytes": 4110})
	if err != nil {
		t.Fatal(err)
	}
	resp := request(t, s, token, http.MethodPost, "/v1/files/grep", string(body))
	var decoded struct{ Data files.GrepResult }
	if err := json.Unmarshal(resp.Body.Bytes(), &decoded); err != nil || resp.Code != 200 {
		t.Fatalf("grep status=%d err=%v body=%s", resp.Code, err, resp.Body.String())
	}
	if !decoded.Data.Truncated || !reflect.DeepEqual(decoded.Data.Matches, []files.LineMatch{{Line: 2, Text: line}}) {
		t.Fatalf("grep returned %d matches, truncated=%v", len(decoded.Data.Matches), decoded.Data.Truncated)
	}
}
