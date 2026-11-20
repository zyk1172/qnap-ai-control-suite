package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLineReaderContinuesPastSeekLimitAndInvalidatesReplacedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	longLine := strings.Repeat("x", 64*1024-1) + "\n"
	deepLine := maxLineSeekBytes/len(longLine) + 1
	prefix := strings.Repeat(longLine, deepLine)
	if err := os.WriteFile(path, []byte(prefix+"end\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service := Service{Roots: []string{dir}, MaxInlineBytes: 1024}
	ctx := context.Background()
	read := func(r *LineReader, start int) (LinePage, error) {
		return r.Read(ctx, service, path, start, 1, 0)
	}

	if _, err := read(&LineReader{}, deepLine); !errors.Is(err, ErrLineSeekLimit) {
		t.Fatalf("cold read past seek limit returned %v", err)
	}
	var reader LineReader
	for start := 0; start < deepLine; {
		page, err := read(&reader, start)
		if err != nil || page.Next != start+1 || !page.Truncated {
			t.Fatalf("sequential page at %d returned next=%d truncated=%v err=%v", start, page.Next, page.Truncated, err)
		}
		start = page.Next
	}
	page, err := read(&reader, deepLine)
	if err != nil || len(page.Lines) != 1 || page.Lines[0] != "end" {
		t.Fatalf("cached deep page: %+v, error %v", page, err)
	}

	// A rotation can preserve size and timestamps. The open file identity
	// must still match before a saved byte position can be reused.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(dir, "replacement.txt")
	if err := os.WriteFile(replacement, []byte(prefix+"new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if _, err := read(&reader, deepLine); !errors.Is(err, ErrLineSeekLimit) {
		t.Fatalf("read reused an old checkpoint after replacement: %v", err)
	}
}
