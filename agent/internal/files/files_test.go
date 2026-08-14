package files

import (
	"encoding/base64"
	"os"
	"path/filepath"
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
