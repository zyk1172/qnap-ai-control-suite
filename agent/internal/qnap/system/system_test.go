package system

import (
	"encoding/json"
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

func TestReadProcessesParsesProcResourceCountersWithoutInventingCPUPercent(t *testing.T) {
	items, err := readProcesses(filepath.Join("testdata", "proc"), map[string]string{"1000": "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one process, got %#v", items)
	}
	item := items[0]
	if item.CPU.UserJiffies != 123 || item.CPU.SystemJiffies != 45 || item.CPU.TotalJiffies != 168 || item.CPU.ChildUserJiffies != 6 || item.CPU.ChildSystemJiffies != 7 {
		t.Fatalf("unexpected CPU counters: %#v", item.CPU)
	}
	if item.CPU.TimeBasis != "cumulative" || item.CPU.Percent != nil || item.CPU.PercentStatus != "unavailable_no_sample" {
		t.Fatalf("CPU percent must be explicitly unavailable without a sample window: %#v", item.CPU)
	}
	encodedCPU, err := json.Marshal(item.CPU)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encodedCPU), `"percent":null`) || !strings.Contains(string(encodedCPU), `"percent_status":"unavailable_no_sample"`) {
		t.Fatalf("CPU JSON must expose unavailable percentage explicitly: %s", encodedCPU)
	}
	if !item.Memory.Available || item.Memory.VirtualBytes != 16*1024 || item.Memory.RSSBytes != 8*1024 || item.Memory.PeakRSSBytes != 12*1024 || item.Memory.RSSAnonBytes != 4*1024 || item.Memory.RSSFileBytes != 2*1024 || item.Memory.RSSShmemBytes != 2*1024 || item.Memory.SwapBytes != 1024 {
		t.Fatalf("unexpected memory counters: %#v", item.Memory)
	}
	if !item.IO.Available || item.IO.ReadCharsBytes != 100 || item.IO.WriteCharsBytes != 200 || item.IO.ReadSyscalls != 3 || item.IO.WriteSyscalls != 4 || item.IO.ReadBytes != 50 || item.IO.WriteBytes != 60 || item.IO.CancelledWriteBytes != 7 {
		t.Fatalf("unexpected I/O counters: %#v", item.IO)
	}
}

func TestParseProcessMemoryFallsBackToProcStatRSS(t *testing.T) {
	memory := parseProcessMemory("Name:\tworker\n", 3, 8192, 4096)
	if !memory.Available || memory.VirtualBytes != 8192 || memory.RSSBytes != 3*4096 {
		t.Fatalf("unexpected /proc stat memory fallback: %#v", memory)
	}
	virtualOnly := parseProcessMemory("Name:\tworker\n", -1, 8192, 4096)
	if !virtualOnly.Available || virtualOnly.VirtualBytes != 8192 || virtualOnly.RSSBytes != 0 {
		t.Fatalf("virtual memory fallback should be reported independently: %#v", virtualOnly)
	}
}

func TestParseProcessIOMarksMissingProcIOUnavailable(t *testing.T) {
	ioCounters := parseProcessIO("rchar: not-a-number\nunknown: 10\n")
	if ioCounters.Available {
		t.Fatalf("malformed or unknown I/O fields must remain unavailable: %#v", ioCounters)
	}
}

func TestParseProcMemoryValueRejectsUnknownUnitAndOverflow(t *testing.T) {
	if value, ok := parseProcMemoryValue([]string{"2", "kB"}); !ok || value != 2*1024 {
		t.Fatalf("expected kB conversion, got value=%d ok=%t", value, ok)
	}
	if _, ok := parseProcMemoryValue([]string{"2", "pages"}); ok {
		t.Fatal("unknown memory units must not be interpreted as bytes")
	}
	if _, ok := parseProcMemoryValue([]string{"18446744073709551615", "kB"}); ok {
		t.Fatal("overflowing memory conversion must be rejected")
	}
}

func TestProcessCountersSaturateOnUint64Overflow(t *testing.T) {
	if got := saturatingAddUint64(^uint64(0)-1, 2); got != ^uint64(0) {
		t.Fatalf("unexpected saturated sum: %d", got)
	}
	if got := saturatingMulUint64(^uint64(0), 2); got != ^uint64(0) {
		t.Fatalf("unexpected saturated product: %d", got)
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
