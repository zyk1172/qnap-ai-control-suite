package capability

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"qnap-ai-control-suite/agent/internal/config"
	"qnap-ai-control-suite/agent/internal/qnap/discovery"
)

func TestResolverBindsVerifiedStorageInventory(t *testing.T) {
	d := discovery.Result{
		Model: "TS-Test", Firmware: "5.2.0", Platform: "qts", Arch: "amd64",
		Features: map[string]discovery.Feature{}, Utilities: map[string]string{},
	}
	r := Resolver{
		FindExecutable: func(name string) string {
			if name == "qcli_storage" {
				return "/sbin/qcli_storage"
			}
			return ""
		},
		Verify: func(_ context.Context, argv []string) error {
			if reflect.DeepEqual(argv, []string{"/sbin/qcli_storage", "-p"}) || reflect.DeepEqual(argv, []string{"/sbin/qcli_storage", "-v"}) {
				return nil
			}
			return errors.New("unexpected command")
		},
		Now: func() time.Time { return time.Unix(1, 0) },
	}
	manifest := r.ResolveResult(context.Background(), d)
	for _, id := range []string{"storage.manager.pools", "storage.manager.volumes"} {
		item, ok := manifest.Get(id)
		if !ok || item.Status != Available || !item.Verified || item.Provider != "/sbin/qcli_storage" || !item.ReadOnly {
			t.Fatalf("unexpected %s capability: %#v", id, item)
		}
	}
	item, _ := manifest.Get("storage.manager.expand")
	if item.Status != Degraded || item.Verified {
		t.Fatalf("write action must remain degraded without verified backend: %#v", item)
	}
}

func TestManualStorageAdapterDisablesAutomaticBindingForThatAdapter(t *testing.T) {
	d := discovery.Result{Platform: "qts", Features: map[string]discovery.Feature{}, Utilities: map[string]string{}}
	verifyCalls := 0
	r := Resolver{
		Adapters: map[string]config.QNAPAdapter{
			"storage_manager": {Commands: map[string][]string{"snapshots": {"/opt/qnap/storage", "snapshots"}}},
		},
		FindExecutable: func(name string) string {
			if name == "qcli_storage" {
				return "/sbin/qcli_storage"
			}
			return ""
		},
		Verify: func(context.Context, []string) error {
			verifyCalls++
			return nil
		},
	}
	manifest := r.ResolveResult(context.Background(), d)
	pools, _ := manifest.Get("storage.manager.pools")
	if pools.Status != Degraded || pools.Backend != "" {
		t.Fatalf("explicit adapter config must suppress automatic binding: %#v", pools)
	}
	snapshots, _ := manifest.Get("storage.manager.snapshots")
	if snapshots.Status != Available || snapshots.Backend != "manual_argv" {
		t.Fatalf("configured action must remain available: %#v", snapshots)
	}
	if verifyCalls != 0 {
		t.Fatalf("automatic qcli verification should not run when adapter override exists: %d", verifyCalls)
	}
}

func TestManualAdapterOverrideWinsWithoutRuntimeProbe(t *testing.T) {
	d := discovery.Result{Platform: "qts", Features: map[string]discovery.Feature{}, Utilities: map[string]string{}}
	r := Resolver{Adapters: map[string]config.QNAPAdapter{
		"virtualization_station": {Commands: map[string][]string{"start": {"/opt/qnap/vmctl", "start", "{id}"}}},
	}, FindExecutable: func(string) string { return "" }}
	manifest := r.ResolveResult(context.Background(), d)
	start, ok := manifest.Get("virtualization.start")
	if !ok || start.Status != Available || !start.Verified || start.Backend != "manual_argv" {
		t.Fatalf("manual override should be available: %#v", start)
	}
	stop, _ := manifest.Get("virtualization.stop")
	if stop.Status != Unavailable {
		t.Fatalf("unconfigured action should remain unavailable when package is not detected: %#v", stop)
	}
}

func TestDetectedPrivateSubsystemStaysDegradedUntilVerified(t *testing.T) {
	d := discovery.Result{
		Platform: "qts", QPKGs: []string{"QKVM", "HybridBackup"},
		Features: map[string]discovery.Feature{}, Utilities: map[string]string{},
	}
	r := Resolver{FindExecutable: func(string) string { return "" }}
	manifest := r.ResolveResult(context.Background(), d)
	for _, id := range []string{"virtualization.start", "hbs.run"} {
		item, ok := manifest.Get(id)
		if !ok || item.Status != Degraded || item.Verified {
			t.Fatalf("detected private capability must remain degraded: %s %#v", id, item)
		}
	}
}

func TestFingerprintIgnoresQPKGAndUtilityMapOrder(t *testing.T) {
	a := discovery.Result{Model: "A", Firmware: "1", Platform: "qts", Arch: "amd64", QPKGs: []string{"B", "A"}, Utilities: map[string]string{"z": "/z", "a": "/a"}}
	b := discovery.Result{Model: "A", Firmware: "1", Platform: "qts", Arch: "amd64", QPKGs: []string{"A", "B"}, Utilities: map[string]string{"a": "/a", "z": "/z"}}
	if fingerprint(a) != fingerprint(b) {
		t.Fatal("fingerprint should be stable across map/list ordering")
	}
}
