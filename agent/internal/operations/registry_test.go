package operations

import "testing"

func TestRegistryClassifiesSensitiveOperations(t *testing.T) {
	registry := New()
	for _, request := range []Request{
		{Method: "POST", Path: "/v1/system/reboot", Payload: map[string]any{"dry_run": true}},
		{Method: "POST", Path: "/v1/exec", Payload: map[string]any{"argv": []any{"/sbin/wipefs", "/dev/sda"}, "dry_run": true}},
		{Method: "POST", Path: "/v1/shell", Payload: map[string]any{"script": "dd if=/dev/zero of=/dev/sda"}},
		{Method: "POST", Path: "/v1/docker/command", Payload: map[string]any{"subcommand": "volume", "args": []any{"prune"}}},
		{Method: "POST", Path: "/v1/network/manage", Payload: map[string]any{"action": "route_delete", "value": "default"}},
		{Method: "POST", Path: "/v1/users/manage", Payload: map[string]any{"action": "password", "name": "admin"}},
		{Method: "POST", Path: "/v1/storage/snapshots/action", Payload: map[string]any{"action": "restore", "name": "pool/snap"}},
	} {
		if operation := registry.Resolve(request); operation.Risk != Sensitive {
			t.Fatalf("%s %s risk=%s summary=%q", request.Method, request.Path, operation.Risk, operation.Summary)
		}
	}
}

func TestRegistryKeepsExpectedWritesUnblocked(t *testing.T) {
	registry := New()
	for _, request := range []Request{
		{Method: "POST", Path: "/v1/files/write", Payload: map[string]any{"path": "/etc/config/example.conf"}},
		{Method: "POST", Path: "/v1/docker/command", Payload: map[string]any{"subcommand": "restart", "args": []any{"app"}}},
		{Method: "POST", Path: "/v1/qnap/qpkg/manage", Payload: map[string]any{"action": "restart", "name": "app"}},
		{Method: "POST", Path: "/v1/users/manage", Payload: map[string]any{"action": "create", "name": "operator"}},
		{Method: "POST", Path: "/v1/exec", Payload: map[string]any{"argv": []any{"/bin/chmod", "0644", "/tmp/config"}}},
	} {
		if operation := registry.Resolve(request); operation.Risk != Write {
			t.Fatalf("%s risk=%s", request.Path, operation.Risk)
		}
	}
}

func TestRegistryMarksReadsAndRecursiveDeletes(t *testing.T) {
	registry := New()
	if operation := registry.Resolve(Request{Method: "GET", Path: "/v1/health"}); operation.Risk != Read {
		t.Fatalf("read risk=%s", operation.Risk)
	}
	operation := registry.Resolve(Request{Method: "POST", Path: "/v1/files/manage", Payload: map[string]any{"action": "delete", "path": "/tmp/work", "recursive": true}})
	if operation.Risk != Sensitive {
		t.Fatalf("recursive delete risk=%s", operation.Risk)
	}
}
