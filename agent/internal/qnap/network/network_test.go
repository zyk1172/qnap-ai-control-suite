package network

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCommandArgs(t *testing.T) {
	tests := []struct {
		action, iface, value, gateway string
		metric                        int
		want                          []string
	}{
		{"set_mtu", "eth0", "9000", "", 0, []string{"link", "set", "dev", "eth0", "mtu", "9000"}},
		{"set_state", "eth0", "down", "", 0, []string{"link", "set", "dev", "eth0", "down"}},
		{"address_add", "eth0", "192.0.2.10/24", "", 0, []string{"addr", "add", "192.0.2.10/24", "dev", "eth0"}},
		{"route_add", "eth0", "default", "192.0.2.1", 10, []string{"route", "add", "default", "via", "192.0.2.1", "dev", "eth0", "metric", "10"}},
	}
	for _, test := range tests {
		got, err := CommandArgs(test.action, test.iface, test.value, test.gateway, test.metric)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s: got=%v err=%v want=%v", test.action, got, err, test.want)
		}
	}
}

func TestCommandArgsRejectsUnsafeInput(t *testing.T) {
	for _, test := range [][4]string{{"set_state", "eth0;rm", "up", ""}, {"address_add", "eth0", "not-cidr", ""}, {"route_add", "eth0", "default", "not-ip"}} {
		if _, err := CommandArgs(test[0], test[1], test[2], test[3], 0); err == nil {
			t.Fatalf("expected error for %#v", test)
		}
	}
}

func TestRoutesReadIPv4AndIPv6FromProcRoot(t *testing.T) {
	root := t.TempDir()
	writeNetworkFixture(t, filepath.Join(root, "net", "route"), `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth0 00000000 0102A8C0 0003 0 0 10 00000000 0 0 0
`)
	writeNetworkFixture(t, filepath.Join(root, "net", "ipv6_route"), `20010db8000000000000000000000000 40 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000002 00000000 00000000 00000000 eth0
malformed line
`)

	items, err := (Service{ProcRoot: root}).Routes()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d routes: %+v", len(items), items)
	}
	if items[0].Family != "ipv4" || items[0].Destination != "0.0.0.0" || items[0].Gateway != "192.168.2.1" || items[0].PrefixLength != 0 {
		t.Fatalf("unexpected IPv4 route: %+v", items[0])
	}
	if items[1].Family != "ipv6" || items[1].Destination != "2001:db8::" || items[1].PrefixLength != 64 || items[1].Gateway != "fe80::1" || items[1].Interface != "eth0" || items[1].Metric != 2 {
		t.Fatalf("unexpected IPv6 route: %+v", items[1])
	}

	ipv6, err := (Service{ProcRoot: root}).IPv6Routes()
	if err != nil || len(ipv6) != 1 {
		t.Fatalf("IPv6Routes=%+v err=%v", ipv6, err)
	}
}

func TestRoutesTolerateMissingIPv6Table(t *testing.T) {
	root := t.TempDir()
	writeNetworkFixture(t, filepath.Join(root, "net", "route"), `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth0 00000000 00000000 0003 0 0 0 00000000 0 0 0
`)

	items, err := (Service{ProcRoot: root}).Routes()
	if err != nil || len(items) != 1 {
		t.Fatalf("routes=%+v err=%v", items, err)
	}
	if _, err := (Service{ProcRoot: root}).IPv6Routes(); !errors.Is(err, ErrIPv6RoutesUnavailable) {
		t.Fatalf("expected IPv6 unavailable, got %v", err)
	}
}

func TestRoutesReportUnavailableWhenBothTablesAreMissing(t *testing.T) {
	_, err := (Service{ProcRoot: t.TempDir()}).Routes()
	if !errors.Is(err, ErrRoutesUnavailable) {
		t.Fatalf("expected routes unavailable, got %v", err)
	}
}

func TestInterfaceCountersReadPartialSysfs(t *testing.T) {
	root := t.TempDir()
	stats := filepath.Join(root, "eth0", "statistics")
	writeNetworkFixture(t, filepath.Join(stats, "rx_bytes"), "12345\n")
	writeNetworkFixture(t, filepath.Join(stats, "rx_packets"), "67\n")
	writeNetworkFixture(t, filepath.Join(stats, "rx_errors"), "bad\n")
	writeNetworkFixture(t, filepath.Join(stats, "tx_dropped"), "9\n")

	got := (Service{SysClassNetRoot: root}).interfaceCounters("eth0")
	want := InterfaceCounters{Available: true, RXBytes: 12345, RXPackets: 67, TXDropped: 9}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counters=%+v want=%+v", got, want)
	}

	missing := (Service{SysClassNetRoot: t.TempDir()}).interfaceCounters("eth0")
	if missing.Available || missing.RXBytes != 0 || missing.TXDropped != 0 {
		t.Fatalf("missing counters should be unavailable and zero: %+v", missing)
	}
}

func TestInterfaceCountersFallBackToProcNetDev(t *testing.T) {
	procRoot := t.TempDir()
	writeNetworkFixture(t, filepath.Join(procRoot, "net", "dev"), `Inter-| Receive | Transmit
 face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed
  eth0: 100 2 3 4 0 0 0 0 200 5 6 7 0 0 0 0
`)

	got := (Service{ProcRoot: procRoot, SysClassNetRoot: t.TempDir()}).interfaceCounters("eth0")
	want := InterfaceCounters{Available: true, RXBytes: 100, RXPackets: 2, RXErrors: 3, RXDropped: 4, TXBytes: 200, TXPackets: 5, TXErrors: 6, TXDropped: 7}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("proc counters=%+v want=%+v", got, want)
	}
}

func TestIPv6NeighborsReadProcAndIgnoreMalformedLines(t *testing.T) {
	root := t.TempDir()
	writeNetworkFixture(t, filepath.Join(root, "net", "ndisc"), `IPv6 address dev lladdr state
fe80::1 dev eth0 lladdr 02:11:22:33:44:55 router REACHABLE
20010db8000000000000000000000002 dev br0 lladdr 02:aa:bb:cc:dd:ee STALE
192.0.2.1 dev eth0 lladdr 00:00:00:00:00:01 REACHABLE
not a neighbor line
`)

	items, err := (Service{ProcRoot: root, IPPath: filepath.Join(root, "missing-ip")}).IPv6Neighbors()
	if err != nil || len(items) != 2 {
		t.Fatalf("neighbors=%+v err=%v", items, err)
	}
	if items[0].Address != "fe80::1" || items[0].Interface != "eth0" || items[0].MAC != "02:11:22:33:44:55" || items[0].State != "REACHABLE" || !items[0].Router || items[0].Source != "proc" {
		t.Fatalf("unexpected first neighbor: %+v", items[0])
	}
	if items[1].Address != "2001:db8::2" || items[1].Interface != "br0" || items[1].State != "STALE" {
		t.Fatalf("unexpected second neighbor: %+v", items[1])
	}
}

func TestIPv6NeighborsFallBackToIPCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("standard ip fixture is a Unix shell script")
	}
	root := t.TempDir()
	ipPath := filepath.Join(root, "ip")
	writeNetworkFixture(t, ipPath, "#!/bin/sh\nprintf '%s\\n' 'fe80::2 dev eth1 lladdr 02:00:00:00:00:02 STALE'\n")
	if err := os.Chmod(ipPath, 0o755); err != nil {
		t.Fatal(err)
	}

	items, err := (Service{ProcRoot: root, IPPath: ipPath}).NeighborsContext(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("neighbors=%+v err=%v", items, err)
	}
	if items[0].Address != "fe80::2" || items[0].Interface != "eth1" || items[0].Source != "ip" {
		t.Fatalf("unexpected fallback neighbor: %+v", items[0])
	}
}

func TestParseIPNeighborsAcceptsCommonFlags(t *testing.T) {
	items := parseIPNeighbors(strings.Join([]string{
		"fe80::1 dev eth0 router INCOMPLETE",
		"fe80::2 dev eth0 proxy FAILED",
	}, "\n"))
	if len(items) != 2 || !items[0].Router || items[0].State != "INCOMPLETE" || !items[1].Proxy || items[1].State != "FAILED" {
		t.Fatalf("parsed neighbors=%+v", items)
	}
}

func writeNetworkFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
