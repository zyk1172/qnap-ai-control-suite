package qpkg

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCommandArgsUsesQTSFlags(t *testing.T) {
	tests := []struct {
		name, action, path, url string
		want                    []string
	}{
		{"ContainerStation", "start", "", "", []string{"--start", "ContainerStation"}},
		{"ContainerStation", "stop", "", "", []string{"--stop", "ContainerStation"}},
		{"ContainerStation", "status", "", "", []string{"--status", "ContainerStation"}},
		{"", "install_file", "/share/Public/example.qpkg", "", []string{"--manually", "/share/Public/example.qpkg"}},
		{"", "install_url", "", "https://example.invalid/example.qpkg", []string{"--url", "https://example.invalid/example.qpkg"}},
		{"", "update_all", "", "", []string{"--update_all"}},
	}
	for _, test := range tests {
		got, err := CommandArgs(test.name, test.action, test.path, test.url)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s: got=%v err=%v want=%v", test.action, got, err, test.want)
		}
	}
}

func TestRunningPIDsMatchesInstallPathExecutableAndCommandLine(t *testing.T) {
	proc := t.TempDir()
	root := "/share/CACHEDEV1_DATA/.qpkg/Example"
	for _, item := range []struct {
		pid string
		exe string
		cmd string
	}{
		{"9", root + "/bin/agent", ""},
		{"10", "/bin/sh", "/bin/sh\x00" + root + "/service.sh\x00"},
		{"11", "/usr/bin/other", "/usr/bin/other\x00"},
	} {
		dir := filepath.Join(proc, item.pid)
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(item.exe, filepath.Join(dir, "exe")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(item.cmd), 0644); err != nil {
			t.Fatal(err)
		}
	}
	pids, err := runningPIDs(root, proc)
	if err != nil || !reflect.DeepEqual(pids, []string{"9", "10"}) {
		t.Fatalf("pids=%#v err=%v", pids, err)
	}
}

func TestCommandArgsRejectsMissingOrUnknownInput(t *testing.T) {
	for _, test := range [][4]string{{"", "start", "", ""}, {"", "install_file", "", ""}, {"pkg", "unknown", "", ""}} {
		if _, err := CommandArgs(test[0], test[1], test[2], test[3]); err == nil {
			t.Fatalf("expected failure for %#v", test)
		}
	}
}
