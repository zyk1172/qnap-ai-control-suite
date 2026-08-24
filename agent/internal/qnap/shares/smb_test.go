package shares

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const smbStatusFixture = `Samba version 4.17.12
PID     Username     Group        Machine                                   Protocol Version  Encryption           Signing
-----------------------------------------------------------------------------------------------------------------------------
22551   admin        administrators 192.0.2.10 (ipv4:192.0.2.10:49822)      SMB3_11           -                    partial

Service      pid     Machine      Connected at                     Encryption   Signing
-----------------------------------------------------------------------------------------
Public       22551   192.0.2.10   Wed Jan 21 18:41:03 2026       -            -

Locked files:
Pid          Uid        DenyMode   Access      R/W        Oplock      SharePath      Name      Time
----------------------------------------------------------------------------------------------------
22551        1000       DENY_NONE  0x100001    RDONLY     EXCLUSIVE    /share/Public  notes.txt 2026-01-21T18:41:04
`

func TestParseSMBStatus(t *testing.T) {
	report := ParseSMBStatus(smbStatusFixture)
	if !report.Supported || len(report.Sessions) != 1 || len(report.Locks) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	session := report.Sessions[0]
	if session.PID != 22551 || session.Username != "admin" || session.Group != "administrators" || session.Machine != "192.0.2.10 (ipv4:192.0.2.10:49822)" || session.Protocol != "SMB3_11" {
		t.Fatalf("unexpected session: %#v", session)
	}
	lock := report.Locks[0]
	if lock.PID != 22551 || lock.UID != "1000" || lock.DenyMode != "DENY_NONE" || lock.SharePath != "/share/Public" || !strings.Contains(lock.Raw, "notes.txt") {
		t.Fatalf("unexpected lock: %#v", lock)
	}
}

func TestSMBStatusMissingUtilityIsCapabilityUnavailable(t *testing.T) {
	report, err := (Service{SMBStatusPath: filepath.Join(t.TempDir(), "missing-smbstatus")}).SMBStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Supported || !strings.Contains(report.Reason, "capability unavailable") || len(report.Sessions) != 0 || len(report.Locks) != 0 {
		t.Fatalf("unexpected unavailable report: %#v", report)
	}
}

func TestSMBStatusExecutesOnlyInventoryCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "smbstatus")
	script := "#!/bin/sh\nif [ \"$#\" -ne 0 ]; then exit 9; fi\nprintf '%s\\n' '" + strings.ReplaceAll(smbStatusFixture, "'", "'\\''") + "'\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	report, err := (Service{SMBStatusPath: path}).SMBStatus(context.Background())
	if err != nil || !report.Supported || report.Source != path || report.Command == nil {
		t.Fatalf("unexpected report=%#v err=%v", report, err)
	}
	if len(report.Command.Argv) != 1 || report.Command.Argv[0] != path {
		t.Fatalf("smbstatus received unexpected argv: %#v", report.Command.Argv)
	}
	if len(report.Sessions) != 1 || len(report.Locks) != 1 {
		t.Fatalf("unexpected parsed report: %#v", report)
	}
}
