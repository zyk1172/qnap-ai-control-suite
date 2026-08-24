package storage

import (
	"testing"
)

func TestParseSMARTJSONATAFixture(t *testing.T) {
	fixture := []byte(`{
  "smart_support": {"available": true, "enabled": true},
  "smart_status": {"passed": true},
  "temperature": {"current": 34},
  "power_on_time": {"hours": 1234},
  "ata_smart_attributes": {"table": [
    {"id": 5, "name": "Reallocated_Sector_Ct", "value": 100, "raw": {"value": 2}},
    {"id": 197, "name": "Current_Pending_Sector", "value": 100, "raw": {"value": 1}},
    {"id": 198, "name": "Offline_Uncorrectable", "value": 100, "raw": {"value": 0}},
    {"id": 199, "name": "UDMA_CRC_Error_Count", "value": 200, "raw": {"value": 3}}
  ]},
  "ata_smart_self_test": {"status": {"string": "Completed without error"}}
}`)

	summary, raw, err := ParseSMARTJSON(fixture)
	if err != nil || raw == nil {
		t.Fatalf("parse fixture: summary=%#v raw=%#v err=%v", summary, raw, err)
	}
	if !summary.Available || !summary.Supported || summary.Enabled == nil || !*summary.Enabled || summary.Passed == nil || !*summary.Passed || summary.Health != "passed" {
		t.Fatalf("unexpected health summary: %#v", summary)
	}
	if summary.TemperatureC == nil || *summary.TemperatureC != 34 || summary.PowerOnHours == nil || *summary.PowerOnHours != 1234 {
		t.Fatalf("unexpected basic SMART fields: %#v", summary)
	}
	if summary.ReallocatedSectors == nil || *summary.ReallocatedSectors != 2 || summary.PendingSectors == nil || *summary.PendingSectors != 1 || summary.OfflineUncorrectable == nil || *summary.OfflineUncorrectable != 0 || summary.UDMACRCErrors == nil || *summary.UDMACRCErrors != 3 {
		t.Fatalf("unexpected ATA attributes: %#v", summary)
	}
	if summary.SelfTestStatus != "Completed without error" {
		t.Fatalf("unexpected self-test status: %#v", summary)
	}
}

func TestParseSMARTJSONNVMeFixture(t *testing.T) {
	fixture := []byte(`{
  "smart_support": {"available": true},
  "smart_status": {"passed": true},
  "nvme_smart_health_information_log": {
    "critical_warning": 0,
    "temperature": 35,
    "percentage_used": 7,
    "power_on_hours": 200,
    "media_errors": 2,
    "num_err_log_entries": 1
  }
}`)
	summary, _, err := ParseSMARTJSON(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if summary.TemperatureC == nil || *summary.TemperatureC != 35 || summary.PowerOnHours == nil || *summary.PowerOnHours != 200 || summary.PercentageUsed == nil || *summary.PercentageUsed != 7 || summary.CriticalWarning == nil || *summary.CriticalWarning != 0 || summary.MediaAndDataErrors == nil || *summary.MediaAndDataErrors != 2 || summary.ErrorCount == nil || *summary.ErrorCount != 1 {
		t.Fatalf("unexpected NVMe summary: %#v", summary)
	}
}

func TestParseSMARTJSONToleratesSparseAndInvalidFixtures(t *testing.T) {
	summary, _, err := ParseSMARTJSON([]byte(`{"smart_support":{"available":false},"smart_status":{"passed":false}}`))
	if err != nil || summary.Supported || summary.Health != "failed" || summary.Reason == "" {
		t.Fatalf("unexpected unsupported summary: %#v err=%v", summary, err)
	}

	if _, _, err := ParseSMARTJSON([]byte(`{"smart_support":`)); err == nil {
		t.Fatal("expected invalid JSON error")
	}

	// A vendor may return an object instead of the standard attribute table;
	// parsing should remain safe and retain the high-level health result.
	summary = ParseSMARTSummary(map[string]any{
		"smart_support":        map[string]any{"available": true},
		"smart_status":         map[string]any{"passed": true},
		"ata_smart_attributes": map[string]any{"table": map[string]any{"unexpected": true}},
	})
	if !summary.Supported || summary.Passed == nil || !*summary.Passed || summary.Health != "passed" {
		t.Fatalf("unexpected sparse vendor summary: %#v", summary)
	}
}
