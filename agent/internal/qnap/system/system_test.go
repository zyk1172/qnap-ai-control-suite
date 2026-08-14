package system

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseNTP(t *testing.T) {
	servers := parseNTP(strings.NewReader("# comment\nserver time.example.test iburst\npool pool.example.test\nserver time.example.test\n"))
	if strings.Join(servers, ",") != "time.example.test,pool.example.test" {
		t.Fatalf("unexpected servers: %#v", servers)
	}
}

func TestParseSocketsDecodesIPv4Address(t *testing.T) {
	input := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n   0: 0100007F:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000  100        0 12345 1\n"
	items := parseSockets("tcp", strings.NewReader(input))
	if len(items) != 1 || items[0].Local != "127.0.0.1:22" || items[0].Remote != "0.0.0.0:0" || items[0].State != "0A" || items[0].Inode != "12345" {
		t.Fatalf("unexpected sockets: %#v", items)
	}
}

func TestReadProcessesFromProcIgnoresBusyboxPSFormat(t *testing.T) {
	root := t.TempDir()
	for _, item := range []struct {
		pid    string
		stat   string
		cmd    string
		status string
	}{
		{"1", "1 (init) S 0 1 1 0 -1 4194560 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0", "", "Name:\tinit\nUid:\t0\t0\t0\t0\nState:\tS (sleeping)\n"},
		{"42", "42 (qnap-agent) S 1 42 42 0 -1 4194304 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0", "/usr/bin/qnap-agent\x00-config\x00/etc/config.json", "Name:\tqnap-agent\nUid:\t1000\t1000\t1000\t1000\nState:\tS (sleeping)\n"},
	} {
		dir := filepath.Join(root, item.pid)
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(item.stat), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(item.cmd), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "status"), []byte(item.status), 0644); err != nil {
			t.Fatal(err)
		}
	}
	items, err := readProcesses(root, map[string]string{"0": "root", "1000": "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].PID != 1 || items[0].User != "root" || items[0].Command != "[init]" || items[1].PID != 42 || items[1].PPID != 1 || items[1].User != "admin" || !strings.Contains(items[1].Command, "-config") {
		t.Fatalf("unexpected processes: %#v", items)
	}
}

func TestParseQPKGUnitsExposesEnabledStateAndScript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qpkg.conf")
	content := `[Example]
Name = Example
Enable = TRUE
Shell = /share/.qpkg/Example/service.sh
Alt_Shell = /share/.qpkg/Example/alt.sh

[Disabled]
Enable = FALSE
Shell = /share/.qpkg/Disabled/service.sh
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	units, err := parseQPKGUnits(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []Unit{{Name: "Example", State: "enabled", Source: "qnap-qpkg", Script: "/share/.qpkg/Example/service.sh", Enabled: true}, {Name: "Disabled", State: "disabled", Source: "qnap-qpkg", Script: "/share/.qpkg/Disabled/service.sh"}}
	if !reflect.DeepEqual(units, want) {
		t.Fatalf("units=%#v want=%#v", units, want)
	}
}
