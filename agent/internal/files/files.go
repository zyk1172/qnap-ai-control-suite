package files

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

var (
	// ErrCASMismatch is returned when expected_sha256 does not match the
	// current contents of the destination file.
	ErrCASMismatch = errors.New("expected_sha256 mismatch")
	// ErrSymlink is returned when an operation would have to follow or replace
	// a symbolic link. Tree operations deliberately do not follow links.
	ErrSymlink = errors.New("symbolic link is not allowed")
)

type Service struct {
	Roots          []string
	MaxInlineBytes int64
}

// WriteOptions controls an atomic file replacement. Mode is used for new
// files; an existing file keeps its current mode, matching the old Write API.
// ExpectedSHA256 accepts either a raw 64-character hash or the sha256:<hash>
// format returned by Checksum. When Backup is true, the previous file is
// atomically copied to BackupPath, or to <path>.bak when BackupPath is empty.
type WriteOptions struct {
	Mode           os.FileMode
	CreateParents  bool
	ExpectedSHA256 string
	Backup         bool
	BackupPath     string
}

type WriteResult struct {
	Path       string `json:"path"`
	BackupPath string `json:"backup_path,omitempty"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
}

// TreeOptions controls SyncTree. DeleteExtraneous is intentionally false by
// default so a sync cannot remove destination-only files unless the caller
// explicitly opts in.
type TreeOptions struct {
	DeleteExtraneous bool
}

type TreeResult struct {
	Source             string   `json:"source"`
	Target             string   `json:"target"`
	FilesCopied        int      `json:"files_copied"`
	DirectoriesCreated int      `json:"directories_created"`
	EntriesDeleted     int      `json:"entries_deleted"`
	DeletedPaths       []string `json:"deleted_paths,omitempty"`
	SkippedSymlinks    []string `json:"skipped_symlinks,omitempty"`
}

// All writes performed by one agent process are serialized. This makes the
// expected-hash check and final rename a compare-and-swap transaction for the
// process that owns the agent. The final rename remains atomic across
// processes; callers that coordinate writes from multiple processes should
// use the service as the single writer.
var fileWriteMu sync.Mutex

type Entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	IsDir     bool   `json:"is_dir"`
	IsSymlink bool   `json:"is_symlink"`
	Size      int64  `json:"size"`
}
type ReadResult struct {
	Path          string `json:"path"`
	ContentBase64 string `json:"content_base64"`
	Bytes         int64  `json:"bytes"`
	Offset        int64  `json:"offset"`
	Truncated     bool   `json:"truncated"`
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
		if info, err := os.Lstat(clean); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("%w: %s", ErrSymlink, clean)
			}
		} else if !os.IsNotExist(err) {
			return "", err
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
	result, err := s.WriteAtomic(path, data, WriteOptions{Mode: mode, CreateParents: parents})
	if err != nil {
		return "", err
	}
	return result.Path, nil
}

// WriteAtomic atomically replaces path with data. The temporary file is
// created in the destination directory, synced, renamed, and followed by a
// parent-directory sync so a successful return means the rename is durable on
// filesystems that support directory fsync.
func (s Service) WriteAtomic(path string, data []byte, options WriteOptions) (WriteResult, error) {
	return s.writeAtomic(path, bytes.NewReader(data), int64(len(data)), options)
}

// WriteWithOptions is an explicit alias for callers that prefer the options
// terminology. Write remains the backwards-compatible four-argument API.
func (s Service) WriteWithOptions(path string, data []byte, options WriteOptions) (WriteResult, error) {
	return s.WriteAtomic(path, data, options)
}

// AtomicWrite is kept as a discoverable alias for the new atomic write
// capability without changing the existing Write signature.
func (s Service) AtomicWrite(path string, data []byte, options WriteOptions) (WriteResult, error) {
	return s.WriteAtomic(path, data, options)
}

func (s Service) writeAtomic(path string, source io.Reader, bytesCount int64, options WriteOptions) (WriteResult, error) {
	if options.Mode == 0 {
		options.Mode = 0644
	}
	if options.CreateParents {
		if err := s.authorizeParent(path); err != nil {
			return WriteResult{}, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return WriteResult{}, err
		}
	}

	fileWriteMu.Lock()
	defer fileWriteMu.Unlock()

	resolved, err := s.Resolve(path, true)
	if err != nil {
		return WriteResult{}, err
	}
	info, err := os.Lstat(resolved)
	if err != nil && !os.IsNotExist(err) {
		return WriteResult{}, err
	}
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return WriteResult{}, fmt.Errorf("%w: %s", ErrSymlink, resolved)
		}
		if !info.Mode().IsRegular() {
			return WriteResult{}, fmt.Errorf("write target is not a regular file: %s", resolved)
		}
	}

	expected, err := normalizeSHA256(options.ExpectedSHA256)
	if err != nil {
		return WriteResult{}, err
	}
	if expected != "" {
		if err := verifyExpectedSHA256(resolved, expected); err != nil {
			return WriteResult{}, err
		}
	}

	mode := options.Mode
	if info != nil {
		mode = info.Mode()
	}
	backupPath := ""
	if options.Backup && info != nil {
		backupPath = options.BackupPath
		if backupPath == "" {
			backupPath = resolved + ".bak"
		}
		backupPath, err = s.Resolve(backupPath, true)
		if err != nil {
			return WriteResult{}, fmt.Errorf("resolve backup: %w", err)
		}
		if filepath.Clean(backupPath) == filepath.Clean(resolved) {
			return WriteResult{}, errors.New("backup path must differ from write path")
		}
		if err := copyFileAtomicUnlocked(resolved, backupPath, info.Mode()); err != nil {
			return WriteResult{}, fmt.Errorf("backup existing file: %w", err)
		}
	}
	if err := atomicReplaceReader(resolved, source, mode); err != nil {
		return WriteResult{}, err
	}

	digest, err := hashFile(resolved)
	if err != nil {
		return WriteResult{}, err
	}
	return WriteResult{Path: resolved, BackupPath: backupPath, Bytes: bytesCount, SHA256: "sha256:" + digest}, nil
}

func normalizeSHA256(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", nil
	}
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) != sha256.Size*2 {
		return "", errors.New("expected_sha256 must be a 64-character SHA-256 hash")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("expected_sha256 is invalid: %w", err)
	}
	return value, nil
}

func verifyExpectedSHA256(path, expected string) error {
	actual, err := hashFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: target does not exist", ErrCASMismatch)
		}
		return err
	}
	if actual != expected {
		return fmt.Errorf("%w: expected %s, got %s", ErrCASMismatch, expected, actual)
	}
	return nil
}

func hashFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: %s", ErrSymlink, path)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("cannot hash non-regular file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func atomicReplaceReader(target string, source io.Reader, mode os.FileMode) error {
	dir := filepath.Dir(target)
	base := filepath.Base(target)
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(mode.Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, source); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	removeTemp = false
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		// macOS filesystems may reject directory fsync even though the rename
		// itself is durable enough for local tests. QNAP runs Linux, where a
		// directory fsync error remains fatal.
		if runtime.GOOS != "linux" && (errors.Is(syncErr, syscall.EINVAL) || errors.Is(syncErr, syscall.ENOTSUP)) {
			syncErr = nil
		}
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
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

// CopyTree recursively copies a directory into target. Existing destination
// files are replaced atomically, destination-only entries are preserved, and
// source symbolic links are skipped rather than followed.
func (s Service) CopyTree(source, target string) (TreeResult, error) {
	return s.SyncTreeWithOptions(source, target, TreeOptions{})
}

// SyncTree mirrors source into target. deleteExtraneous is explicit because
// the safe default is to retain destination-only files.
func (s Service) SyncTree(source, target string, deleteExtraneous bool) (TreeResult, error) {
	return s.SyncTreeWithOptions(source, target, TreeOptions{DeleteExtraneous: deleteExtraneous})
}

// SyncTreeWithOptions is the options form used by callers that need the
// default non-destructive sync or want to opt in to deleting extras.
func (s Service) SyncTreeWithOptions(source, target string, options TreeOptions) (TreeResult, error) {
	sourceResolved, err := s.Resolve(source, false)
	if err != nil {
		return TreeResult{}, err
	}
	sourceInfo, err := os.Lstat(sourceResolved)
	if err != nil {
		return TreeResult{}, err
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return TreeResult{}, fmt.Errorf("%w: %s", ErrSymlink, sourceResolved)
	}
	if !sourceInfo.IsDir() {
		return TreeResult{}, fmt.Errorf("tree source is not a directory: %s", sourceResolved)
	}

	targetClean, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return TreeResult{}, err
	}
	targetForOverlap := targetClean
	if parentResolved, err := filepath.EvalSymlinks(filepath.Dir(targetClean)); err == nil {
		targetForOverlap = filepath.Join(parentResolved, filepath.Base(targetClean))
	}
	if within(sourceResolved, targetForOverlap) || within(targetForOverlap, sourceResolved) {
		return TreeResult{}, errors.New("tree source and target must not overlap")
	}
	targetResolved, err := s.prepareTreeTarget(target)
	if err != nil {
		return TreeResult{}, err
	}
	if within(sourceResolved, targetResolved) || within(targetResolved, sourceResolved) {
		return TreeResult{}, errors.New("tree source and target must not overlap")
	}

	result := TreeResult{Source: sourceResolved, Target: targetResolved}
	sourceEntries := map[string]struct{}{".": {}}
	err = filepath.WalkDir(sourceResolved, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == sourceResolved {
			return nil
		}
		rel, err := filepath.Rel(sourceResolved, current)
		if err != nil {
			return err
		}
		rel = filepath.Clean(rel)
		if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return errors.New("tree entry escapes source")
		}
		sourceEntries[rel] = struct{}{}
		targetPath := filepath.Join(targetResolved, rel)
		if !within(targetPath, targetResolved) {
			return errors.New("tree entry escapes target")
		}

		if entry.Type()&os.ModeSymlink != 0 {
			result.SkippedSymlinks = append(result.SkippedSymlinks, current)
			return nil
		}
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			created, err := ensureTreeDirectory(targetResolved, targetPath, info.Mode(), options.DeleteExtraneous, &result)
			if err != nil {
				return err
			}
			if created {
				result.DirectoriesCreated++
			}
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported special file in tree: %s", current)
		}
		if err := prepareTreeFileDestination(targetResolved, targetPath, options.DeleteExtraneous, &result); err != nil {
			return err
		}
		if err := copyFileAtomic(current, targetPath, info.Mode()); err != nil {
			return err
		}
		result.FilesCopied++
		return nil
	})
	if err != nil {
		return result, err
	}

	if options.DeleteExtraneous {
		if err := deleteTreeExtras(targetResolved, sourceEntries, &result); err != nil {
			return result, err
		}
	}
	if err := syncDirectory(targetResolved); err != nil {
		return result, err
	}
	sort.Strings(result.DeletedPaths)
	sort.Strings(result.SkippedSymlinks)
	return result, nil
}

func (s Service) prepareTreeTarget(target string) (string, error) {
	clean, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(clean); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: %s", ErrSymlink, clean)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("tree target is not a directory: %s", clean)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	} else {
		if err := s.authorizeParent(clean); err != nil {
			return "", err
		}
		if err := os.MkdirAll(clean, 0755); err != nil {
			return "", err
		}
	}
	resolved, err := s.Resolve(clean, true)
	if err != nil {
		return "", err
	}
	resolved, err = s.Resolve(resolved, false)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: %s", ErrSymlink, resolved)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("tree target is not a directory: %s", resolved)
	}
	return resolved, nil
}

func ensureTreeDirectory(root, path string, mode os.FileMode, replace bool, result *TreeResult) (bool, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false, errors.New("tree directory escapes target")
	}
	if rel == "." {
		return false, os.Chmod(root, mode.Perm())
	}
	current := root
	created := false
	parts := strings.Split(rel, string(os.PathSeparator))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if err := os.Mkdir(current, mode.Perm()); err != nil {
				return created, err
			}
			created = true
			continue
		}
		if statErr != nil {
			return created, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return created, fmt.Errorf("%w: %s", ErrSymlink, current)
		}
		if !info.IsDir() {
			if !replace {
				return created, fmt.Errorf("tree destination is not a directory: %s", current)
			}
			if err := os.RemoveAll(current); err != nil {
				return created, err
			}
			result.EntriesDeleted++
			result.DeletedPaths = append(result.DeletedPaths, current)
			if err := os.Mkdir(current, mode.Perm()); err != nil {
				return created, err
			}
			created = true
		}
	}
	return created, os.Chmod(path, mode.Perm())
}

func prepareTreeFileDestination(root, path string, replace bool, result *TreeResult) error {
	if err := ensureTreeParent(root, filepath.Dir(path), replace, result); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrSymlink, path)
	}
	if info.IsDir() {
		if !replace {
			return fmt.Errorf("tree destination is a directory: %s", path)
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		result.EntriesDeleted++
		result.DeletedPaths = append(result.DeletedPaths, path)
	}
	return nil
}

func ensureTreeParent(root, path string, replace bool, result *TreeResult) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("tree parent escapes target")
	}
	if rel == "." {
		return nil
	}
	current := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if err := os.Mkdir(current, 0755); err != nil {
				return err
			}
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlink, current)
		}
		if !info.IsDir() {
			if !replace {
				return fmt.Errorf("tree parent is not a directory: %s", current)
			}
			if err := os.RemoveAll(current); err != nil {
				return err
			}
			result.EntriesDeleted++
			result.DeletedPaths = append(result.DeletedPaths, current)
			if err := os.Mkdir(current, 0755); err != nil {
				return err
			}
		}
	}
	return nil
}

func deleteTreeExtras(root string, sourceEntries map[string]struct{}, result *TreeResult) error {
	return filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		if _, ok := sourceEntries[filepath.Clean(rel)]; ok {
			return nil
		}
		if err := os.RemoveAll(current); err != nil {
			return err
		}
		result.EntriesDeleted++
		result.DeletedPaths = append(result.DeletedPaths, current)
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			return filepath.SkipDir
		}
		return nil
	})
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
	writeActions := map[string]bool{"mkdir": true, "touch": true, "copy": true, "move": true, "rename": true, "delete": true, "chmod": true, "chown": true, "truncate": true, "symlink": true, "hardlink": true, "archive": true, "extract": true}
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
		if mode == 0 {
			return errors.New("mode is required for chmod")
		}
		return applyTree(resolved, recursive, func(current string) error { return os.Chmod(current, mode) })
	}
	if action == "chown" {
		uid, gid, err := parseOwner(target)
		if err != nil {
			return err
		}
		return applyTree(resolved, recursive, func(current string) error { return os.Chown(current, uid, gid) })
	}
	if action == "truncate" {
		return os.Truncate(resolved, 0)
	}
	if target == "" {
		return errors.New("target is required")
	}
	targetResolved, err := s.Resolve(target, action == "copy" || action == "move" || action == "rename" || action == "archive" || action == "extract")
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
	case "archive":
		return archivePath(resolved, targetResolved)
	case "extract":
		if err := s.authorizeParent(target); err != nil {
			return err
		}
		if err := os.MkdirAll(targetResolved, 0755); err != nil {
			return err
		}
		return extractPath(resolved, targetResolved)
	}
	return nil
}
func (s Service) Checksum(path string) (string, error) {
	resolved, err := s.Resolve(path, false)
	if err != nil {
		return "", err
	}
	digest, err := hashFile(resolved)
	if err != nil {
		return "", err
	}
	return "sha256:" + digest, nil
}
func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func copyFile(from, to string) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrSymlink, from)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("copy source is not a regular file: %s", from)
	}
	return copyFileAtomic(from, to, info.Mode())
}

func copyFileAtomic(from, to string, mode os.FileMode) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrSymlink, from)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("copy source is not a regular file: %s", from)
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	if info, err := in.Stat(); err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("copy source is not a regular file: %s", from)
	}
	fileWriteMu.Lock()
	defer fileWriteMu.Unlock()
	return copyFileAtomicUnlockedWithReader(in, from, to, mode)
}

func copyFileAtomicUnlocked(from, to string, mode os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	return copyFileAtomicUnlockedWithReader(in, from, to, mode)
}

func copyFileAtomicUnlockedWithReader(in *os.File, from, to string, mode os.FileMode) error {
	if info, err := in.Stat(); err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("copy source is not a regular file: %s", from)
	}
	return atomicReplaceReader(to, in, mode)
}

func parseOwner(value string) (int, int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, errors.New("target must be uid:gid")
	}
	uid, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	gid, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

// applyTree keeps recursive metadata changes inside the resolved root and
// never follows descendant symlinks.
func applyTree(root string, recursive bool, apply func(string) error) error {
	if !recursive {
		return apply(root)
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		return apply(path)
	})
}

func archivePath(source, target string) error {
	switch {
	case strings.HasSuffix(strings.ToLower(target), ".zip"):
		return writeZip(source, target)
	case strings.HasSuffix(strings.ToLower(target), ".tar"), strings.HasSuffix(strings.ToLower(target), ".tar.gz"), strings.HasSuffix(strings.ToLower(target), ".tgz"):
		return writeTar(source, target)
	default:
		return errors.New("archive target must end in .zip, .tar, .tar.gz, or .tgz")
	}
}

func extractPath(source, target string) error {
	lower := strings.ToLower(source)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(source, target)
	case strings.HasSuffix(lower, ".tar"), strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTar(source, target)
	default:
		return errors.New("archive source must end in .zip, .tar, .tar.gz, or .tgz")
	}
}

func writeZip(source, target string) error {
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	err = walkArchive(source, func(path, name string, info os.FileInfo) error {
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = name
		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		writer, err := zw.CreateHeader(header)
		if err != nil || info.IsDir() {
			return err
		}
		return copyPath(path, writer)
	})
	closeErr := zw.Close()
	fileErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return fileErr
}

func writeTar(source, target string) error {
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	var writer io.Writer = out
	var gz *gzip.Writer
	if strings.HasSuffix(strings.ToLower(target), ".tar.gz") || strings.HasSuffix(strings.ToLower(target), ".tgz") {
		gz = gzip.NewWriter(out)
		writer = gz
	}
	tw := tar.NewWriter(writer)
	err = walkArchive(source, func(path, name string, info os.FileInfo) error {
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		if err := tw.WriteHeader(header); err != nil || info.IsDir() {
			return err
		}
		return copyPath(path, tw)
	})
	closeErr := tw.Close()
	if gz != nil {
		if err := gz.Close(); closeErr == nil {
			closeErr = err
		}
	}
	fileErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return fileErr
}

func walkArchive(source string, visit func(path, name string, info os.FileInfo) error) error {
	rootInfo, err := os.Stat(source)
	if err != nil {
		return err
	}
	base := filepath.Base(source)
	if !rootInfo.IsDir() {
		return visit(source, base, rootInfo)
	}
	return filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(filepath.Dir(source), path)
		if err != nil {
			return err
		}
		return visit(path, filepath.ToSlash(rel), info)
	})
}

func extractZip(source, target string) error {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		path, err := archiveDestination(target, file.Name)
		if err != nil {
			return err
		}
		info := file.FileInfo()
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("archive symlink entries are not supported")
		}
		if info.IsDir() {
			if err := os.MkdirAll(path, info.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err == nil {
			_, err = io.Copy(out, in)
			closeErr := out.Close()
			if err == nil {
				err = closeErr
			}
		}
		in.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTar(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	var reader io.Reader = in
	if strings.HasSuffix(strings.ToLower(source), ".tar.gz") || strings.HasSuffix(strings.ToLower(source), ".tgz") {
		gz, err := gzip.NewReader(in)
		if err != nil {
			return err
		}
		defer gz.Close()
		reader = gz
	}
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		path, err := archiveDestination(target, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(path, os.FileMode(header.Mode))
		case tar.TypeReg, tar.TypeRegA:
			if err = os.MkdirAll(filepath.Dir(path), 0755); err == nil {
				var out *os.File
				out, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
				if err == nil {
					_, err = io.Copy(out, tr)
					closeErr := out.Close()
					if err == nil {
						err = closeErr
					}
				}
			}
		default:
			return errors.New("archive link and special entries are not supported")
		}
		if err != nil {
			return err
		}
	}
}

func archiveDestination(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("archive entry escapes destination")
	}
	path := filepath.Join(root, clean)
	if !within(path, root) {
		return "", errors.New("archive entry escapes destination")
	}
	return path, nil
}

func copyPath(path string, writer io.Writer) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(writer, in)
	return err
}
