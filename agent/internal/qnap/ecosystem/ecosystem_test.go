package ecosystem

import "testing"

func TestParseUPS(t *testing.T) {
	got := parseUPS("battery.charge: 98\nups.status: OL\n")
	if got["battery.charge"] != "98" || got["ups.status"] != "OL" {
		t.Fatalf("unexpected values: %#v", got)
	}
}

func TestKnownQPKGNamesAreDetected(t *testing.T) {
	if !has([]string{"QKVM", "HybridBackup"}, "qkvm") || !has([]string{"QKVM", "HybridBackup"}, "hybridbackup") {
		t.Fatal("expected known QTS package names to be detected")
	}
}
