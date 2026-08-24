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
		{"12", "", ""},
	} {
		dir := filepath.Join(proc, item.pid)
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if item.exe != "" {
			if err := os.Symlink(item.exe, filepath.Join(dir, "exe")); err != nil {
				t.Fatal(err)
			}
		}
		if item.cmd != "" {
			if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(item.cmd), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	pids, err := runningPIDs(root, proc)
	if err != nil || !reflect.DeepEqual(pids, []string{"9", "10"}) {
		t.Fatalf("pids=%#v err=%v", pids, err)
	}
}

func TestPopulateProcessStatesScansProcOnceForMultipleInstallPaths(t *testing.T) {
	proc := t.TempDir()
	alpha := "/share/CACHEDEV1_DATA/.qpkg/Alpha"
	beta := "/share/CACHEDEV1_DATA/.qpkg/Beta"

	writeFakeProcProcess(t, proc, "20", beta+"/bin/service", "")
	writeFakeProcProcess(t, proc, "3", "/bin/sh", "/bin/sh\x00"+alpha+"/run.sh\x00")
	// This process has neither proc file. The scanner must tolerate the
	// per-process read errors and continue building the shared snapshot.
	if err := os.Mkdir(filepath.Join(proc, "12"), 0755); err != nil {
		t.Fatal(err)
	}

	packages := []map[string]string{
		{"name": "Alpha", "Install_Path": alpha},
		{"name": "Beta", "Install_Path": beta},
	}
	scans := 0
	scan := func(root string) (procSnapshot, error) {
		scans++
		if scans > 1 {
			t.Fatal("proc scanner called more than once")
		}
		return scanProc(root)
	}

	populateProcessStates(packages, proc, scan)

	if scans != 1 {
		t.Fatalf("proc scans=%d, want 1", scans)
	}
	if got := packages[0]["process_state"]; got != "running" {
		t.Fatalf("Alpha process_state=%q, want running", got)
	}
	if got := packages[0]["process_pids"]; got != "3" {
		t.Fatalf("Alpha process_pids=%q, want 3", got)
	}
	if got := packages[1]["process_state"]; got != "running" {
		t.Fatalf("Beta process_state=%q, want running", got)
	}
	if got := packages[1]["process_pids"]; got != "20" {
		t.Fatalf("Beta process_pids=%q, want 20", got)
	}
}

func TestPopulateProcessStatesReportsProcRootErrorForEveryPackage(t *testing.T) {
	proc := filepath.Join(t.TempDir(), "missing-proc")
	packages := []map[string]string{
		{"name": "Alpha", "Install_Path": "/share/Alpha"},
		{"name": "Beta", "Install_Path": "/share/Beta"},
	}

	populateProcessStates(packages, proc, scanProc)

	for _, pkg := range packages {
		if pkg["process_state"] != "unknown" {
			t.Fatalf("%s process_state=%q, want unknown", pkg["name"], pkg["process_state"])
		}
		if pkg["process_state_error"] == "" {
			t.Fatalf("%s missing process_state_error", pkg["name"])
		}
	}
}

func writeFakeProcProcess(t *testing.T, procRoot, pid, exe, cmdline string) {
	t.Helper()
	dir := filepath.Join(procRoot, pid)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if exe != "" {
		if err := os.Symlink(exe, filepath.Join(dir, "exe")); err != nil {
			t.Fatal(err)
		}
	}
	if cmdline != "" {
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCommandArgsRejectsMissingOrUnknownInput(t *testing.T) {
	for _, test := range [][4]string{{"", "start", "", ""}, {"", "install_file", "", ""}, {"pkg", "unknown", "", ""}} {
		if _, err := CommandArgs(test[0], test[1], test[2], test[3]); err == nil {
			t.Fatalf("expected failure for %#v", test)
		}
	}
}
