package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseQTSVolumesKeepsFieldsAndExtractsStableColumns(t *testing.T) {
	items := ParseQTSVolumes("Volume ID  Name  Mount Path\n---------------------------\n1 DataVol1 /share/CACHEDEV1_DATA\n2 SSD /share/CACHEDEV5_DATA\n")
	if len(items) != 2 || items[0]["id"] != "1" || items[0]["name"] != "DataVol1" || items[0]["mountpoint"] != "/share/CACHEDEV1_DATA" {
		t.Fatalf("unexpected volumes: %#v", items)
	}
}

func TestParseQTSRowsDropsTableHeaders(t *testing.T) {
	pools := ParseQTSPools("Pool Name Device Type\n=====================\nPool1 /dev/md1 RAID\n")
	if len(pools) != 1 || pools[0]["device"] != "/dev/md1" {
		t.Fatalf("unexpected pools: %#v", pools)
	}
	disks := ParseQTSDisks("Disk Model Size\n=====================\n1 /dev/sda ExampleDisk 4TB\n")
	if len(disks) != 1 || disks[0]["device"] != "/dev/sda" {
		t.Fatalf("unexpected disks: %#v", disks)
	}
}

func TestParseRAIDStatusIncludesBitmapAndRecoveryProgress(t *testing.T) {
	items := ParseRAIDStatus(`Personalities : [raid1]
md0 : active raid1 sda3[0] sdb3[1]
      3906885440 blocks super 1.2 [2/2] [UU]

md1 : active raid1 sdc3[0] sdd3[1]
      1024 blocks [2/1] [_U]
      [===>.................]  recovery = 17.4% (178/1024) finish=10.0min speed=12345K/sec
`)
	if len(items) != 2 {
		t.Fatalf("items=%#v", items)
	}
	if items[0].State != "active" || items[0].Level != "raid1" || items[0].TotalDevices != 2 || items[0].ActiveDevices != 2 || items[0].Degraded || strings.Join(items[0].Members, ",") != "sda3,sdb3" {
		t.Fatalf("unexpected healthy array: %#v", items[0])
	}
	if !items[1].Degraded || items[1].Layout != "_U" || items[1].Sync == nil || items[1].Sync.Action != "recovery" || items[1].Sync.Progress == nil || *items[1].Sync.Progress != 17.4 || items[1].Sync.Finish != "10.0min" || items[1].Sync.Speed != "12345K/sec" {
		t.Fatalf("unexpected recovering array: %#v", items[1])
	}
}

func TestRAIDActionWritesOnlyDiscoveredSyncAction(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "md0", "md", "sync_action")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("idle\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s := Service{SysBlockRoot: root}
	dry, err := s.RAIDAction("md0", "scrub_start", true)
	if err != nil || !dry.DryRun || dry.Previous != "idle" || dry.Current != "check" || dry.Applied {
		t.Fatalf("dry=%#v err=%v", dry, err)
	}
	result, err := s.RAIDAction("md0", "scrub_start", false)
	if err != nil || !result.Applied || result.Current != "check" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "check" {
		t.Fatalf("sync action=%q err=%v", b, err)
	}
	if _, err := s.RAIDAction("md0", "invalid", false); err == nil {
		t.Fatal("expected unsupported action error")
	}
	if _, err := s.RAIDAction("../md0", "scrub_start", false); err == nil {
		t.Fatal("expected invalid md name error")
	}
}
