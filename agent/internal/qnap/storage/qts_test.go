package storage

import "testing"

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
