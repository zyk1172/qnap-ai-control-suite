package files

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Service struct {
	Roots          []string
	MaxInlineBytes int64
}
type Entry struct {
	Name, Path, Mode string
	IsDir, IsSymlink bool
	Size             int64
}
type ReadResult struct {
	Path, ContentBase64 string
	Bytes               int64
	Offset              int64
	Truncated           bool
}
type SearchResult struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"is_dir"`
}

func (s Service) Resolve(path string, forCreate bool) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	clean, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	resolved := clean
	if forCreate {
		parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
		if err != nil {
			return "", fmt.Errorf("resolve parent: %w", err)
		}
		resolved = filepath.Join(parent, filepath.Base(clean))
	} else {
		resolved, err = filepath.EvalSymlinks(clean)
		if err != nil {
			return "", err
		}
	}
	for _, root := range s.Roots {
		rootResolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			rootResolved = filepath.Clean(root)
		}
		if within(resolved, rootResolved) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("path is outside allowed roots: %s", clean)
}

func (s Service) List(path string) ([]Entry, error) {
	resolved, err := s.Resolve(path, false)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, Entry{Name: entry.Name(), Path: filepath.Join(resolved, entry.Name()), Mode: info.Mode().String(), IsDir: entry.IsDir(), IsSymlink: entry.Type()&os.ModeSymlink != 0, Size: info.Size()})
	}
	return out, nil
}
func (s Service) Stat(path string) (os.FileInfo, error) {
	resolved, err := s.Resolve(path, false)
	if err != nil {
		return nil, err
	}
	return os.Stat(resolved)
}
func (s Service) Read(path string, offset, max int64) (ReadResult, error) {
	resolved, err := s.Resolve(path, false)
	if err != nil {
		return ReadResult{}, err
	}
	if offset < 0 {
		return ReadResult{}, errors.New("offset must be non-negative")
	}
	if max <= 0 || max > s.MaxInlineBytes {
		max = s.MaxInlineBytes
	}
	f, err := os.Open(resolved)
	if err != nil {
		return ReadResult{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ReadResult{}, err
	}
	if offset > info.Size() {
		offset = info.Size()
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ReadResult{}, err
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return ReadResult{}, err
	}
	truncated := int64(len(b)) > max
	if truncated {
		b = b[:max]
	}
	return ReadResult{Path: resolved, ContentBase64: base64.StdEncoding.EncodeToString(b), Bytes: int64(len(b)), Offset: offset, Truncated: truncated}, nil
}
func (s Service) Write(path string, data []byte, mode os.FileMode, parents bool) (string, error) {
	if mode == 0 {
		mode = 0644
	}
	if parents {
		if err := s.authorizeParent(path); err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}
	}
	resolved, err := s.Resolve(path, true)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(resolved, data, mode); err != nil {
		return "", err
	}
	return resolved, nil
}
func (s Service) Append(path string, data []byte, mode os.FileMode, parents bool) (string, error) {
	if mode == 0 {
		mode = 0644
	}
	if parents {
		if err := s.authorizeParent(path); err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}
	}
	resolved, err := s.Resolve(path, true)
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(resolved, os.O_CREATE|os.O_APPEND|os.O_WRONLY, mode)
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return "", writeErr
	}
	return resolved, closeErr
}
func (s Service) Tail(path string, limit int, maxBytes int64) ([]string, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	if maxBytes <= 0 || maxBytes > s.MaxInlineBytes {
		maxBytes = s.MaxInlineBytes
	}
	info, err := s.Stat(path)
	if err != nil {
		return nil, err
	}
	offset := info.Size() - maxBytes
	if offset < 0 {
		offset = 0
	}
	result, err := s.Read(path, offset, maxBytes)
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(result.ContentBase64)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}
func (s Service) Search(path, query string, limit int) ([]SearchResult, error) {
	resolved, err := s.Resolve(path, false)
	if err != nil {
		return nil, err
	}
	if query == "" {
		return nil, errors.New("query is required")
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	out := []SearchResult{}
	err = filepath.WalkDir(resolved, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if len(out) >= limit {
			return filepath.SkipDir
		}
		if strings.Contains(strings.ToLower(entry.Name()), strings.ToLower(query)) {
			info, err := entry.Info()
			if err == nil {
				out = append(out, SearchResult{Path: current, Size: info.Size(), IsDir: entry.IsDir()})
			}
		}
		return nil
	})
	return out, err
}
func (s Service) DU(path string) (int64, error) {
	resolved, err := s.Resolve(path, false)
	if err != nil {
		return 0, err
	}
	var total int64
	err = filepath.WalkDir(resolved, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

// authorizeParent verifies the closest existing parent before mkdir can create
// any directories. This keeps create_parents inside the configured roots.
func (s Service) authorizeParent(path string) error {
	clean, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	ancestor := filepath.Dir(clean)
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return err
			}
			for _, root := range s.Roots {
				rootResolved, err := filepath.EvalSymlinks(root)
				if err != nil {
					rootResolved = filepath.Clean(root)
				}
				if within(resolved, rootResolved) {
					return nil
				}
			}
			return fmt.Errorf("path is outside allowed roots: %s", clean)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return fmt.Errorf("no existing parent for %s", clean)
		}
		ancestor = parent
	}
}
func (s Service) Manage(action, path, target string, mode os.FileMode, recursive bool) error {
	writeActions := map[string]bool{"mkdir": true, "touch": true, "copy": true, "move": true, "rename": true, "delete": true, "chmod": true, "chown": true, "truncate": true, "symlink": true, "hardlink": true}
	if !writeActions[action] {
		return fmt.Errorf("unsupported file action: %s", action)
	}
	resolved, err := s.Resolve(path, action == "mkdir" || action == "touch" || action == "symlink" || action == "hardlink")
	if err != nil {
		return err
	}
	if action == "delete" {
		if recursive {
			return os.RemoveAll(resolved)
		}
		return os.Remove(resolved)
	}
	if action == "mkdir" {
		if mode == 0 {
			mode = 0755
		}
		return os.MkdirAll(resolved, mode)
	}
	if action == "touch" {
		f, err := os.OpenFile(resolved, os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			err = f.Close()
		}
		return err
	}
	if action == "chmod" {
		return os.Chmod(resolved, mode)
	}
	if action == "chown" {
		parts := strings.Split(target, ":")
		if len(parts) != 2 {
			return errors.New("target must be uid:gid")
		}
		uid, err := strconv.Atoi(parts[0])
		if err != nil {
			return err
		}
		gid, err := strconv.Atoi(parts[1])
		if err != nil {
			return err
		}
		return os.Chown(resolved, uid, gid)
	}
	if action == "truncate" {
		return os.Truncate(resolved, 0)
	}
	if target == "" {
		return errors.New("target is required")
	}
	targetResolved, err := s.Resolve(target, action == "copy" || action == "move" || action == "rename")
	if err != nil {
		return err
	}
	switch action {
	case "copy":
		return copyFile(resolved, targetResolved)
	case "move", "rename":
		return os.Rename(resolved, targetResolved)
	case "symlink":
		return os.Symlink(targetResolved, resolved)
	case "hardlink":
		return os.Link(targetResolved, resolved)
	}
	return nil
}
func (s Service) Checksum(path string) (string, error) {
	resolved, err := s.Resolve(path, false)
	if err != nil {
		return "", err
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), err
}
func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
