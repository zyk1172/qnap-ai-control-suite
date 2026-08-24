package discovery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverReportsReadOnlySchedulerCapabilities(t *testing.T) {
	root := t.TempDir()
	procRoot := filepath.Join(root, "proc")
	sysRoot := filepath.Join(root, "sys")
	etcRoot := filepath.Join(root, "etc")
	varRoot := filepath.Join(root, "var")
	binRoot := filepath.Join(root, "bin")
	for _, path := range []string{
		filepath.Join(procRoot, "1"),
		filepath.Join(procRoot, "1234"),
		filepath.Join(etcRoot, "config"),
		binRoot,
	} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeDiscoveryFile(t, filepath.Join(procRoot, "1", "comm"), "crond\n", 0644)
	writeDiscoveryFile(t, filepath.Join(procRoot, "1234", "comm"), "crond\n", 0644)
	writeDiscoveryFile(t, filepath.Join(etcRoot, "config", "crontab"), "0 * * * * /bin/true\n", 0644)
	writeDiscoveryExecutable(t, filepath.Join(binRoot, "crond"))
	writeDiscoveryExecutable(t, filepath.Join(binRoot, "crontab"))

	result := (Service{
		ProcRoot:     procRoot,
		SysRoot:      sysRoot,
		EtcRoot:      etcRoot,
		VarRoot:      varRoot,
		UtilityPaths: []string{binRoot},
	}).Discover(context.Background())

	scheduler := result.Features["scheduler"]
	if !scheduler.Supported || !scheduler.ReadOnly || len(scheduler.Evidence) == 0 || !strings.Contains(scheduler.Reason, "read-only") {
		t.Fatalf("unexpected scheduler feature: %#v", scheduler)
	}
	cron := result.Features["cron"]
	if !cron.Supported || !cron.ReadOnly || len(cron.Evidence) == 0 || !strings.Contains(cron.Reason, "no schedule changes") {
		t.Fatalf("unexpected cron feature: %#v", cron)
	}
	tasks := result.Features["scheduled_tasks"]
	if !tasks.Supported || !tasks.ReadOnly || len(tasks.Evidence) == 0 {
		t.Fatalf("unexpected scheduled tasks feature: %#v", tasks)
	}
	if result.Utilities["crond"] != filepath.Join(binRoot, "crond") || result.Utilities["crontab"] != filepath.Join(binRoot, "crontab") {
		t.Fatalf("unexpected utility inventory: %#v", result.Utilities)
	}
}

func TestDiscoverReportsSystemdTimersOnlyWhenPID1AndSystemctlAreVerified(t *testing.T) {
	root := t.TempDir()
	procRoot := filepath.Join(root, "proc")
	binRoot := filepath.Join(root, "bin")
	if err := os.MkdirAll(filepath.Join(procRoot, "1"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binRoot, 0755); err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, filepath.Join(procRoot, "1", "comm"), "systemd\n", 0644)
	writeDiscoveryExecutable(t, filepath.Join(binRoot, "systemctl"))

	result := (Service{ProcRoot: procRoot, UtilityPaths: []string{binRoot}}).Discover(context.Background())
	timers := result.Features["systemd_timers"]
	if !timers.Supported || !timers.ReadOnly || len(timers.Evidence) != 2 {
		t.Fatalf("unexpected systemd timer feature: %#v", timers)
	}
}

func TestDiscoverMarksSchedulerCapabilitiesUnavailableWithoutEvidence(t *testing.T) {
	root := t.TempDir()
	procRoot := filepath.Join(root, "proc")
	etcRoot := filepath.Join(root, "etc")
	varRoot := filepath.Join(root, "var")
	binRoot := filepath.Join(root, "bin")
	if err := os.MkdirAll(filepath.Join(procRoot, "1"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(etcRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binRoot, 0755); err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, filepath.Join(procRoot, "1", "comm"), "init\n", 0644)

	result := (Service{ProcRoot: procRoot, EtcRoot: etcRoot, VarRoot: varRoot, UtilityPaths: []string{binRoot}}).Discover(context.Background())
	for _, name := range []string{"scheduler", "cron", "systemd_timers", "scheduled_tasks"} {
		feature := result.Features[name]
		if feature.Supported || !feature.ReadOnly || !strings.Contains(feature.Reason, "capability unavailable") {
			t.Fatalf("expected unavailable %s feature, got %#v", name, feature)
		}
	}
}

func writeDiscoveryFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func writeDiscoveryExecutable(t *testing.T, path string) {
	t.Helper()
	writeDiscoveryFile(t, path, "#!/bin/sh\nexit 0\n", 0755)
}
