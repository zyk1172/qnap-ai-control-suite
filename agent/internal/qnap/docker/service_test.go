package docker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	qexec "qnap-ai-control-suite/agent/internal/exec"
)

func TestHealthAggregatesInspectHealthAndNoHealthcheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("QNAP Docker service uses a Unix executable")
	}
	service := fixtureDocker(t, "testdata/health-inspect.json", "testdata/compose-projects.json")
	report, err := service.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Supported || report.Summary.Total != 2 || report.Summary.Healthy != 1 || report.Summary.NoHealthcheck != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Containers[0].Name != "healthy-web" || report.Containers[0].Health != "healthy" || report.Containers[0].Healthy == nil || !*report.Containers[0].Healthy {
		t.Fatalf("unexpected healthy container: %+v", report.Containers[0])
	}
	if len(report.Containers[0].Log) != 1 || report.Containers[0].Log[0].ExitCode != 0 {
		t.Fatalf("health log was not preserved: %+v", report.Containers[0].Log)
	}
	if report.Containers[1].Health != "none" || report.Containers[1].HasHealthcheck {
		t.Fatalf("unexpected no-healthcheck container: %+v", report.Containers[1])
	}
}

func TestComposeProjectsParsesJSONAndPreservesConfigFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("QNAP Docker service uses a Unix executable")
	}
	service := fixtureDocker(t, "testdata/health-inspect.json", "testdata/compose-projects.json")
	inventory, err := service.ComposeProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !inventory.Supported || len(inventory.Projects) != 2 {
		t.Fatalf("unexpected inventory: %+v", inventory)
	}
	if inventory.Projects[0].Name != "media" || inventory.Projects[0].WorkingDir != "/share/Container/media" || len(inventory.Projects[0].ConfigFiles) != 1 {
		t.Fatalf("unexpected first project: %+v", inventory.Projects[0])
	}
	if inventory.Projects[1].Name != "backup" || len(inventory.Projects[1].ConfigFiles) != 2 {
		t.Fatalf("unexpected second project: %+v", inventory.Projects[1])
	}
}

func TestBuildReconstructionEquivalentForRepresentedRuntimeConfiguration(t *testing.T) {
	raw := readFixtureMap(t, "testdata/reconstruct-equivalent.json")
	reconstruction, err := BuildReconstruction(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reconstruction.Equivalent || reconstruction.Status != ReconstructionEquivalent || len(reconstruction.LossReport) != 0 {
		t.Fatalf("expected equivalent reconstruction: %+v", reconstruction)
	}
	for _, pair := range [][2]string{{"--entrypoint", "/usr/local/bin/web"}, {"--env", "APP_MODE=test"}, {"--volume", "/share/web:/data:rw"}, {"--publish", "127.0.0.1:18080:8080/tcp"}, {"--restart", "unless-stopped"}, {"--health-cmd", "wget -q -O - http://127.0.0.1/health"}} {
		if !hasArgPair(reconstruction.DockerRun, pair[0], pair[1]) {
			t.Fatalf("docker_run missing %q %q: %v", pair[0], pair[1], reconstruction.DockerRun)
		}
	}
	if reconstruction.Compose.Image != "example/web:1" || reconstruction.Compose.NetworkMode != "default" || reconstruction.Compose.Memory != "268435456" {
		t.Fatalf("compose fields were not preserved: %+v", reconstruction.Compose)
	}
}

func TestBuildReconstructionReportsLossAndDoesNotClaimEquivalent(t *testing.T) {
	raw := readFixtureMap(t, "testdata/reconstruct-lossy.json")
	reconstruction, err := BuildReconstruction(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if reconstruction.Equivalent || reconstruction.Status != ReconstructionLossy || len(reconstruction.LossReport) == 0 {
		t.Fatalf("expected lossy reconstruction: %+v", reconstruction)
	}
	for _, field := range []string{"Config.Entrypoint", "Config.Env[API_TOKEN]", "HostConfig.DeviceRequests", "NetworkSettings.Networks.custom"} {
		if !contains(reconstruction.Unsupported, field) {
			t.Fatalf("missing loss for %q: %+v", field, reconstruction.LossReport)
		}
	}
	encoded, err := json.Marshal(reconstruction)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "fixture-secret") {
		t.Fatalf("redacted reconstruction leaked secret: %s", encoded)
	}
}

func readFixtureMap(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func fixtureDocker(t *testing.T, inspectFixture, composeFixture string) Service {
	t.Helper()
	if _, err := os.Stat(inspectFixture); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "docker-fixture")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"ps\" ]; then printf 'abc123\\nworker456\\n'; exit 0; fi\n" +
		"if [ \"$1\" = \"inspect\" ]; then cat " + shellQuote(inspectFixture) + "; exit 0; fi\n" +
		"if [ \"$1\" = \"compose\" ]; then cat " + shellQuote(composeFixture) + "; exit 0; fi\n" +
		"exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return Service{Exec: qexec.Executor{}, Paths: []string{path}}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func hasArgPair(args []string, flag, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == flag && args[index+1] == value {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
