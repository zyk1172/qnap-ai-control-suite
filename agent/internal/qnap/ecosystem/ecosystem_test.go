package ecosystem

import (
	"strings"
	"testing"

	"qnap-ai-control-suite/agent/internal/config"
)

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

func TestCommandExpandsOnlyKnownArgvPlaceholders(t *testing.T) {
	s := Service{Adapters: map[string]config.QNAPAdapter{
		"hbs3": {Commands: map[string][]string{"run": {"/opt/hbs", "run", "{id}", "{args}"}}, TimeoutSeconds: 45},
	}}
	argv, timeout, err := s.Command("hbs3", "run", map[string]string{"id": "daily-backup"}, []string{"--force"})
	if err != nil || strings.Join(argv, " ") != "/opt/hbs run daily-backup --force" || timeout.Seconds() != 45 {
		t.Fatalf("unexpected command: %#v timeout=%v err=%v", argv, timeout, err)
	}
	_, _, err = s.Command("hbs3", "run", map[string]string{}, nil)
	if err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("expected missing id error, got %v", err)
	}
	_, _, err = s.Command("hbs3", "run", map[string]string{"id": "daily-backup"}, []string{"--force"})
	if err != nil {
		t.Fatalf("args placeholder should accept extra args: %v", err)
	}
	_, _, err = (Service{Adapters: map[string]config.QNAPAdapter{"hbs3": {Commands: map[string][]string{"list": {"/opt/hbs", "list"}}}}}).Command("hbs3", "list", nil, []string{"--json"})
	if err == nil || !strings.Contains(err.Error(), "does not accept args") {
		t.Fatalf("expected rejected unused args, got %v", err)
	}
}

func TestParseCertificateMetadata(t *testing.T) {
	certificate := parseCertificate("/share/cert.pem", "subject=CN = nas.local\nissuer=CN = Local CA\nnotBefore=Aug 14 00:00:00 2026 GMT\nnotAfter=Aug 14 00:00:00 2027 GMT\nserial=ABCD\nsha256 Fingerprint=AA:BB\nX509v3 Subject Alternative Name:\n    DNS:nas.local, IP Address:192.168.1.2\n")
	if certificate.Path != "/share/cert.pem" || certificate.Subject != "CN = nas.local" || certificate.Issuer != "CN = Local CA" || certificate.NotAfter == "" || certificate.Fingerprint != "AA:BB" || len(certificate.SAN) != 2 {
		t.Fatalf("unexpected certificate: %#v", certificate)
	}
}
