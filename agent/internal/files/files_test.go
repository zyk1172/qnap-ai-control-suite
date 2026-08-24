package files

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBinaryRangeRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "binary")
	data := []byte{0, 255, 1, 2, 3, 4}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := (Service{Roots: []string{root}, MaxInlineBytes: 4}).Read(path, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	got, err := base64.StdEncoding.DecodeString(r.ContentBase64)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data[3:]) || r.Truncated {
		t.Fatalf("bad read %+v %v", r, got)
	}
}
func TestSymlinkEscapeRejected(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("no"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	_, err := (Service{Roots: []string{root}, MaxInlineBytes: 100}).Read(filepath.Join(root, "escape"), 0, 100)
	if err == nil {
		t.Fatal("expected symlink escape rejection")
	}
}

func TestWriteAtomicCASBackupAndModePreservation(t *testing.T) {
	root := t.TempDir()
	s := Service{Roots: []string{root}, MaxInlineBytes: 1024}
	path := filepath.Join(root, "config", "agent.conf")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	old := []byte("old\n")
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	expected, err := s.Checksum(path)
	if err != nil {
		t.Fatal(err)
	}

	result, err := s.WriteAtomic(path, []byte{0, 255, 1, 2}, WriteOptions{
		Mode:           0644,
		ExpectedSHA256: expected,
		Backup:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolvedPath, err := s.Resolve(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != resolvedPath || result.BackupPath != resolvedPath+".bak" || result.Bytes != 4 || !strings.HasPrefix(result.SHA256, "sha256:") {
		t.Fatalf("unexpected write result: %+v", result)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string([]byte{0, 255, 1, 2}) {
		t.Fatalf("written data=%v err=%v", got, err)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil || string(backup) != string(old) {
		t.Fatalf("backup=%q err=%v", backup, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}

	if _, err := s.WriteAtomic(path, []byte("must not write"), WriteOptions{ExpectedSHA256: expected}); !errors.Is(err, ErrCASMismatch) {
		t.Fatalf("expected CAS mismatch, got %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != string([]byte{0, 255, 1, 2}) {
		t.Fatalf("CAS changed data=%v err=%v", got, err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary file was left behind: %s", entry.Name())
		}
	}
}

func TestWriteAtomicCreateParentsAndRejectsSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	s := Service{Roots: []string{root}, MaxInlineBytes: 1024}
	path := filepath.Join(root, "new", "nested", "file")
	if _, err := s.Write(path, []byte("created"), 0640, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("created file mode=%v err=%v", info.Mode(), err)
	}

	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(link, []byte("must not follow"), 0644, false); !errors.Is(err, ErrSymlink) {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "safe" {
		t.Fatalf("outside target changed: %q err=%v", got, err)
	}
}

func TestCopyTreePreservesDestinationExtrasAndSkipsSymlink(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "text.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "binary"), []byte{0, 255, 2}, 0640); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "extra"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	s := Service{Roots: []string{root}, MaxInlineBytes: 1024}
	result, err := s.CopyTree(source, target)
	if err != nil {
		t.Fatal(err)
	}
	if result.FilesCopied != 2 || len(result.SkippedSymlinks) != 1 {
		t.Fatalf("unexpected copy result: %+v", result)
	}
	got, err := os.ReadFile(filepath.Join(target, "nested", "text.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("copied text=%q err=%v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(target, "binary"))
	if err != nil || string(got) != string([]byte{0, 255, 2}) {
		t.Fatalf("copied binary=%v err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(target, "extra")); err != nil {
		t.Fatalf("destination extra was not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "escape")); !os.IsNotExist(err) {
		t.Fatalf("source symlink should not be copied: %v", err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "outside" {
		t.Fatalf("outside symlink target changed: %q err=%v", got, err)
	}
}

func TestSyncTreeDefaultRetainsExtrasAndExplicitDeleteRemovesThem(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "keep"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "extra"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	s := Service{Roots: []string{root}, MaxInlineBytes: 1024}
	if result, err := s.SyncTree(source, target, false); err != nil {
		t.Fatal(err)
	} else if result.EntriesDeleted != 0 {
		t.Fatalf("default sync deleted entries: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(target, "extra")); err != nil {
		t.Fatalf("default sync removed extra: %v", err)
	}

	result, err := s.SyncTree(source, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.EntriesDeleted != 1 || len(result.DeletedPaths) != 1 {
		t.Fatalf("unexpected delete result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(target, "extra")); !os.IsNotExist(err) {
		t.Fatalf("explicit sync did not delete extra: %v", err)
	}
}

func TestTreeRejectsSymlinkTargetAndOverlappingDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "target-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	s := Service{Roots: []string{root}, MaxInlineBytes: 1024}
	if _, err := s.CopyTree(source, link); !errors.Is(err, ErrSymlink) {
		t.Fatalf("expected target symlink rejection, got %v", err)
	}

	overlapping := filepath.Join(source, "child")
	if _, err := s.CopyTree(source, overlapping); err == nil {
		t.Fatal("expected overlapping tree rejection")
	}
	if _, err := os.Stat(overlapping); !os.IsNotExist(err) {
		t.Fatalf("overlap rejection created destination: %v", err)
	}
}

func TestSyncTreeCanReplaceTypeOnlyWhenDeletionEnabled(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "file"), []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "nested"), []byte("blocking file"), 0644); err != nil {
		t.Fatal(err)
	}
	s := Service{Roots: []string{root}, MaxInlineBytes: 1024}
	if _, err := s.SyncTree(source, target, false); err == nil {
		t.Fatal("expected type conflict without deletion")
	}
	if _, err := s.SyncTree(source, target, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, "nested", "file"))
	if err != nil || string(got) != "source" {
		t.Fatalf("replaced tree=%q err=%v", got, err)
	}
}
func TestManageCopyMoveDelete(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "from")
	if err := os.WriteFile(from, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	s := Service{Roots: []string{root}, MaxInlineBytes: 100}
	copy := filepath.Join(root, "copy")
	if err := s.Manage("copy", from, copy, 0, false); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(root, "moved")
	if err := s.Manage("move", copy, moved, 0, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Manage("delete", moved, "", 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(moved); !os.IsNotExist(err) {
		t.Fatalf("delete failed: %v", err)
	}
}

func TestAppendTailSearchAndDU(t *testing.T) {
	root := t.TempDir()
	s := Service{Roots: []string{root}, MaxInlineBytes: 1024}
	path := filepath.Join(root, "logs", "agent.log")
	if _, err := s.Append(path, []byte("one\ntwo\nthree\n"), 0644, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(path, []byte("four\n"), 0644, false); err != nil {
		t.Fatal(err)
	}
	lines, err := s.Tail(path, 2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "three" || lines[1] != "four" {
		t.Fatalf("tail=%#v", lines)
	}
	results, err := s.Search(root, "agent", 10)
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if err != nil || len(results) != 1 || results[0].Path != resolvedPath {
		t.Fatalf("results=%#v err=%v", results, err)
	}
	bytes, err := s.DU(root)
	if err != nil || bytes != int64(len("one\ntwo\nthree\nfour\n")) {
		t.Fatalf("du=%d err=%v", bytes, err)
	}
}

func TestArchiveAndExtractZIP(t *testing.T) {
	root := t.TempDir()
	s := Service{Roots: []string{root}, MaxInlineBytes: 1024}
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "data.bin"), []byte{0, 255, 2}, 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "backup.zip")
	if err := s.Manage("archive", source, archive, 0, false); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "restore")
	if err := s.Manage("extract", archive, destination, 0, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "source", "nested", "data.bin"))
	if err != nil || string(got) != string([]byte{0, 255, 2}) {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestArchiveDestinationRejectsTraversal(t *testing.T) {
	if _, err := archiveDestination("/safe", "../../etc/passwd"); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestManageRecursiveChmodSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(filepath.Join(root, "child"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	s := Service{Roots: []string{root}, MaxInlineBytes: 1024}
	if err := s.Manage("chmod", root, "", 0750, true); err != nil {
		t.Fatal(err)
	}
	child, err := os.Stat(filepath.Join(root, "child"))
	if err != nil || child.Mode().Perm() != 0750 {
		t.Fatalf("child mode=%v err=%v", child.Mode(), err)
	}
	outsideInfo, err := os.Stat(outside)
	if err != nil || outsideInfo.Mode().Perm() != 0600 {
		t.Fatalf("outside mode=%v err=%v", outsideInfo.Mode(), err)
	}
}
