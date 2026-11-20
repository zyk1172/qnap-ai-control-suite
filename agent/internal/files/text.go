package files

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
)

type LinePage struct {
	Path      string   `json:"path"`
	Start     int      `json:"start"`
	Next      int      `json:"next"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
}

type LineMatch struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

type GrepResult struct {
	Path      string      `json:"path"`
	Query     string      `json:"query"`
	Matches   []LineMatch `json:"matches"`
	Truncated bool        `json:"truncated"`
}

const maxLineSeekBytes = 16 * 1024 * 1024

var ErrLineSeekLimit = errors.New("line position exceeds the scan limit; page sequentially or use a byte offset with nas_file_read_text")

type linePosition struct {
	line    int
	offset  int64
	partial bool
}

type lineCheckpoint struct {
	path string
	info os.FileInfo
	linePosition
}

// LineReader remembers a bounded number of continuation positions. An entry
// only applies to the same file version, and never substitutes for openRead.
type LineReader struct {
	mu     sync.Mutex
	recent [64]lineCheckpoint
	next   int
}

func (r *LineReader) Read(ctx context.Context, s Service, path string, start, limit int, maxBytes int64) (LinePage, error) {
	f, err := s.openText(path)
	if err != nil {
		return LinePage{}, err
	}
	defer f.Close()
	start = max(start, 0)
	limit = lineLimit(limit)
	info, err := f.Stat()
	if err != nil {
		return LinePage{}, err
	}
	pos := r.position(f.Name(), info, start)
	if pos.offset != 0 {
		if _, err := f.Seek(pos.offset, io.SeekStart); err != nil {
			return LinePage{}, err
		}
	}
	reader := bufio.NewReader(f)
	from := pos.line
	if pos.partial {
		// The preceding page returned this oversized line's prefix.
		from--
	}
	skipped, bytesSkipped, err := skipLines(ctx, reader, start-from)
	if err != nil && err != io.EOF {
		return LinePage{}, err
	}
	actualStart := from + skipped
	page := LinePage{Path: f.Name(), Start: actualStart, Next: actualStart, Lines: make([]string, 0, limit)}
	if err == io.EOF {
		return page, nil
	}
	pageOffset := pos.offset + bytesSkipped
	next := linePosition{line: actualStart, offset: pageOffset}
	var eof bool
	page.Truncated, eof, err = scanLines(ctx, reader, s.inlineByteLimit(maxBytes), func(_ int, line []byte, consumed int64, partial bool) bool {
		// Defer a partial row to the next page if that page can give it a
		// fresh byte budget. An oversized first row is returned once, clipped.
		if partial && len(page.Lines) > 0 {
			return false
		}
		page.Lines = append(page.Lines, string(line))
		page.Next++
		next = linePosition{line: page.Next, offset: pageOffset + consumed, partial: partial}
		return len(page.Lines) < limit
	})
	if err == nil && !eof && page.Next > page.Start && info.Mode().IsRegular() && next.offset <= info.Size() {
		r.remember(lineCheckpoint{path: f.Name(), info: info, linePosition: next})
	}
	return page, err
}

func (r *LineReader) position(path string, info os.FileInfo, start int) linePosition {
	if start == 0 {
		return linePosition{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var best linePosition
	for _, entry := range r.recent {
		better := entry.line > best.line || (entry.line == best.line && entry.offset > best.offset)
		if entry.path == path && better && entry.line <= start &&
			os.SameFile(entry.info, info) && entry.info.Size() == info.Size() &&
			entry.info.ModTime().Equal(info.ModTime()) {
			best = entry.linePosition
		}
	}
	return best
}

func (r *LineReader) remember(checkpoint lineCheckpoint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recent[r.next] = checkpoint
	r.next = (r.next + 1) % len(r.recent)
}

func (s Service) Grep(ctx context.Context, path, query string, limit int, maxBytes int64) (GrepResult, error) {
	f, err := s.openText(path)
	if err != nil {
		return GrepResult{}, err
	}
	defer f.Close()
	limit = lineLimit(limit)
	maxBytes = s.inlineByteLimit(maxBytes)
	result := GrepResult{Path: f.Name(), Query: query, Matches: make([]LineMatch, 0)}
	needle := []byte(query)
	result.Truncated, _, err = scanLines(ctx, bufio.NewReader(f), maxBytes, func(index int, line []byte, _ int64, _ bool) bool {
		if bytes.Contains(line, needle) {
			result.Matches = append(result.Matches, LineMatch{Line: index + 1, Text: string(line)})
		}
		return len(result.Matches) < limit
	})
	if err == nil && !result.Truncated && len(result.Matches) == limit {
		// As before, report whether the search window is smaller than the
		// file, even when the requested number of matches is found early.
		if info, statErr := f.Stat(); statErr == nil {
			result.Truncated = info.Size() > maxBytes
		}
	}
	return result, err
}

func (s Service) openText(path string) (*os.File, error) {
	f, err := s.openRead(path)
	if err != nil {
		return nil, err
	}
	// Preserve the range read's requirement that the input support seeking.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func lineLimit(limit int) int {
	if limit <= 0 || limit > 2000 {
		return 200
	}
	return limit
}

// skipLines advances by logical lines without retaining the skipped text.
// The final empty row after a newline is consistent with splitting on '\n'.
func skipLines(ctx context.Context, reader *bufio.Reader, count int) (int, int64, error) {
	var consumed int64
	for skipped := 0; skipped < count; {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		part, err := reader.ReadSlice('\n')
		consumed += int64(len(part))
		switch err {
		case nil:
			skipped++
		case bufio.ErrBufferFull:
		case io.EOF:
			return skipped + 1, consumed, io.EOF
		default:
			return 0, 0, err
		}
		if skipped < count && consumed >= maxLineSeekBytes {
			return 0, 0, ErrLineSeekLimit
		}
	}
	return count, consumed, nil
}

// scanLines visits only the bounded window. Short lines borrow the read
// buffer, so grep only allocates strings for rows it will return.
func scanLines(ctx context.Context, reader *bufio.Reader, maxBytes int64, visit func(index int, line []byte, consumed int64, partial bool) bool) (truncated, eof bool, err error) {
	window := &io.LimitedReader{R: reader, N: maxBytes}
	bounded := bufio.NewReader(window)
	var fragments []byte
	var consumed int64
	for index := 0; ; {
		if err := ctx.Err(); err != nil {
			return false, false, err
		}
		part, err := bounded.ReadSlice('\n')
		consumed += int64(len(part))
		if err == bufio.ErrBufferFull {
			fragments = append(fragments, part...)
			continue
		}
		if err != nil && err != io.EOF {
			return false, false, err
		}
		line := part
		if len(fragments) > 0 {
			line = append(fragments, part...)
			fragments = line[:0]
		}
		if err == io.EOF {
			truncated, err := lineWindowTruncated(reader, consumed, maxBytes)
			if err != nil {
				return false, false, err
			}
			if !truncated || len(line) > 0 {
				visit(index, line, consumed, truncated)
			}
			return truncated, !truncated, nil
		}
		if !visit(index, line[:len(line)-1], consumed, false) {
			truncated, err := lineWindowTruncated(reader, consumed, maxBytes)
			return truncated, false, err
		}
		index++
	}
}

func lineWindowTruncated(reader *bufio.Reader, consumed, maxBytes int64) (bool, error) {
	if consumed < maxBytes {
		return false, nil
	}
	_, err := reader.Peek(1)
	if err == io.EOF {
		return false, nil
	}
	return err == nil, err
}
