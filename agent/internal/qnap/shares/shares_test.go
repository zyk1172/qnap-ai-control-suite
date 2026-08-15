package shares

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNFS(t *testing.T) {
	items := ParseNFS(strings.NewReader("# comment\n/share/Public 192.0.2.0/24(rw) host(ro)\n"))
	if len(items) != 1 || items[0].Path != "/share/Public" || len(items[0].Hosts) != 2 {
		t.Fatalf("%#v", items)
	}
}

func TestListMarksEmptyPathQNAPShareAsSystemShare(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smb.conf")
	content := "[Public]\npath = /share/Public\ncomment = Public\n\n[TMBackup]\nstrict sync = yes\ncreate time = 2026:02:02:18:58:58:08\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	items, err := parseShares(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[1].Name != "TMBackup" || !items[1].SystemShare || items[1].Path != "" {
		t.Fatalf("unexpected shares: %#v", items)
	}
}

func TestParseACLKeepsEntriesAndDropsComments(t *testing.T) {
	entries := parseACL("# file: /tmp/x\n# owner: admin\nuser::rw-\nuser:zyk:r--\ngroup::r--\nmask::rw-\nother::---\n")
	if len(entries) != 5 || entries[1] != "user:zyk:r--" {
		t.Fatalf("entries=%#v", entries)
	}
}

func TestACLFallbackReturnsStatPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "example")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := aclFallback(path, "getfacl not found")
	if err != nil || !result.Fallback || result.Mode == "" || result.Path != path {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
