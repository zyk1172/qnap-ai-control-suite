package files

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
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

func (s Service) ReadLines(ctx context.Context, path string, start, limit int, maxBytes int64) (LinePage, error) {
	f, err := s.openText(path)
	if err != nil {
		return LinePage{}, err
	}
	defer f.Close()
	start = max(start, 0)
	limit = lineLimit(limit)
	reader := bufio.NewReader(f)
	actualStart, err := skipLines(ctx, reader, start)
	if err != nil && err != io.EOF {
		return LinePage{}, err
	}
	page := LinePage{Path: f.Name(), Start: actualStart, Next: actualStart, Lines: make([]string, 0, limit)}
	if err == io.EOF {
		return page, nil
	}
	page.Truncated, err = scanLines(ctx, reader, s.inlineByteLimit(maxBytes), func(_ int, line []byte, partial bool) bool {
		// Defer a partial row to the next page if that page can give it a
		// fresh byte budget. An oversized first row is returned once, clipped.
		if partial && len(page.Lines) > 0 {
			return false
		}
		page.Lines = append(page.Lines, string(line))
		page.Next++
		return len(page.Lines) < limit
	})
	return page, err
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
	result.Truncated, err = scanLines(ctx, bufio.NewReader(f), maxBytes, func(index int, line []byte, _ bool) bool {
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
func skipLines(ctx context.Context, reader *bufio.Reader, count int) (int, error) {
	for skipped := 0; skipped < count; {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		_, err := reader.ReadSlice('\n')
		switch err {
		case nil:
			skipped++
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			return skipped + 1, io.EOF
		default:
			return 0, err
		}
	}
	return count, nil
}

// scanLines visits only the bounded window. Short lines borrow the read
// buffer, so grep only allocates strings for rows it will return.
func scanLines(ctx context.Context, reader *bufio.Reader, maxBytes int64, visit func(index int, line []byte, partial bool) bool) (bool, error) {
	window := &io.LimitedReader{R: reader, N: maxBytes}
	bounded := bufio.NewReader(window)
	var fragments []byte
	for index := 0; ; {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		part, err := bounded.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			fragments = append(fragments, part...)
			continue
		}
		if err != nil && err != io.EOF {
			return false, err
		}
		line := part
		if len(fragments) > 0 {
			line = append(fragments, part...)
			fragments = line[:0]
		}
		if err == io.EOF {
			truncated := false
			if window.N == 0 {
				_, peekErr := reader.Peek(1)
				if peekErr != nil && peekErr != io.EOF {
					return false, peekErr
				}
				truncated = peekErr == nil
			}
			if !truncated || len(line) > 0 {
				visit(index, line, truncated)
			}
			return truncated, nil
		}
		if !visit(index, line[:len(line)-1], false) {
			return false, nil
		}
		index++
	}
}
