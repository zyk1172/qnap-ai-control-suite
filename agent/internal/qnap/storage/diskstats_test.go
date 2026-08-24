package storage

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestDiskIOReadsFakeProcAndAttachesToDisks(t *testing.T) {
	root := t.TempDir()
	procRoot := filepath.Join(root, "proc")
	sysBlockRoot := filepath.Join(root, "sys", "block")
	if err := os.MkdirAll(procRoot, 0755); err != nil {
		t.Fatal(err)
	}
	diskRoot := filepath.Join(sysBlockRoot, "sda")
	if err := os.MkdirAll(filepath.Join(diskRoot, "device"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(procRoot, "diskstats"), "8 0 sda 100 10 2000 500 50 5 4000 600 3 1500 2100 0 2 25 7 4 9\n")
	writeFixture(t, filepath.Join(procRoot, "uptime"), "100.0 0.0\n")
	writeFixture(t, filepath.Join(diskRoot, "size"), "8192\n")
	writeFixture(t, filepath.Join(diskRoot, "queue", "rotational"), "1\n")
	writeFixture(t, filepath.Join(diskRoot, "device", "model"), "Fixture Disk\n")
	writeFixture(t, filepath.Join(diskRoot, "device", "serial"), "SERIAL-1\n")
	writeFixture(t, filepath.Join(diskRoot, "device", "firmware_rev"), "FW1\n")
	writeFixture(t, filepath.Join(diskRoot, "device", "smart_supported"), "1\n")

	service := Service{ProcRoot: procRoot, SysBlockRoot: sysBlockRoot}
	items, err := service.DiskIO()
	if err != nil {
		t.Fatal(err)
	}
	sda, ok := items["sda"]
	if !ok {
		t.Fatalf("sda diskstats missing: %#v", items)
	}
	if sda.ReadBytes != 2000*512 || sda.WriteBytes != 4000*512 || sda.DiscardsCompleted != 0 || sda.SectorsDiscarded != 25 || sda.FlushesCompleted != 4 {
		t.Fatalf("unexpected counters: %#v", sda)
	}
	if !sda.CurrentlyBusy || sda.BusyRatio == nil || *sda.BusyRatio != 0.015 || sda.BusyRatioWindow != "since_boot" {
		t.Fatalf("unexpected busy metrics: %#v", sda)
	}
	if sda.AverageLatencyMS == nil || *sda.AverageLatencyMS != 2100.0/154.0 {
		t.Fatalf("unexpected latency: %#v", sda.AverageLatencyMS)
	}
	if sda.AverageQueueDepth == nil || *sda.AverageQueueDepth != 1.4 {
		t.Fatalf("unexpected queue depth: %#v", sda.AverageQueueDepth)
	}

	disks, err := service.Disks()
	if err != nil {
		t.Fatal(err)
	}
	if len(disks) != 1 || disks[0].IO == nil || !disks[0].SmartSupported || disks[0].Model != "Fixture Disk" {
		t.Fatalf("unexpected disk inventory: %#v", disks)
	}
}

func TestParseDiskstatsIgnoresMalformedRowsAndKeepsShortKernelFormat(t *testing.T) {
	input := "not a diskstats row\n" +
		"8 x sdb 1 2 3 4 5 6 7 8 9 10 11\n" +
		"8 1 sdc 1 2 3 4 5 6 7 8 9 10 11\n" +
		"8 2 sdd 1 2 3 nope 5 6 7 8 9 10 11\n"
	items := ParseDiskstats(input)
	if len(items) != 1 {
		t.Fatalf("unexpected parsed items: %#v", items)
	}
	if item := items["sdc"]; item.ReadsCompleted != 1 || item.WritesCompleted != 5 || item.IOTimeMS != 10 || item.DiscardBytes != 0 {
		t.Fatalf("unexpected short-format item: %#v", item)
	}
}

func TestDiskIOOmitsBusyRatioWhenUptimeIsUnavailable(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "diskstats"), "8 0 sda 1 0 8 4 1 0 16 6 0 10 20\n")
	items, err := (Service{ProcRoot: root}).DiskIO()
	if err != nil {
		t.Fatal(err)
	}
	if item := items["sda"]; item.BusyRatio != nil || item.BusyRatioWindow != "" {
		t.Fatalf("busy ratio should be unavailable without uptime: %#v", item)
	}
}

func TestDiskIOCountersSaturateByteAndOperationCalculations(t *testing.T) {
	max := uint64(math.MaxUint64)
	items := ParseDiskstats("8 0 sda 1 0 18446744073709551615 0 1 0 18446744073709551615 0 0 1 1\n")
	item := items["sda"]
	if item.ReadBytes != max || item.WriteBytes != max {
		t.Fatalf("expected saturated byte counters: %#v", item)
	}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
