package storage

import (
	"encoding/json"
	"strconv"
	"strings"
)

// SMARTSummary is a stable, small summary of the fields most useful to an
// agent. The original smartctl JSON remains available from Smart's raw field
// so firmware- or vendor-specific attributes are not discarded.
type SMARTSummary struct {
	Available            bool     `json:"available"`
	Supported            bool     `json:"supported"`
	Enabled              *bool    `json:"enabled,omitempty"`
	Passed               *bool    `json:"passed,omitempty"`
	Health               string   `json:"health,omitempty"`
	Reason               string   `json:"reason,omitempty"`
	TemperatureC         *float64 `json:"temperature_c,omitempty"`
	PowerOnHours         *uint64  `json:"power_on_hours,omitempty"`
	ReallocatedSectors   *uint64  `json:"reallocated_sectors,omitempty"`
	PendingSectors       *uint64  `json:"pending_sectors,omitempty"`
	OfflineUncorrectable *uint64  `json:"offline_uncorrectable,omitempty"`
	UDMACRCErrors        *uint64  `json:"udma_crc_errors,omitempty"`
	MediaAndDataErrors   *uint64  `json:"media_and_data_errors,omitempty"`
	PercentageUsed       *float64 `json:"percentage_used,omitempty"`
	CriticalWarning      *uint64  `json:"critical_warning,omitempty"`
	ErrorCount           *uint64  `json:"error_count,omitempty"`
	SelfTestStatus       string   `json:"self_test_status,omitempty"`
}

// ParseSMARTJSON parses smartctl's JSON while preserving the distinction
// between invalid output and a valid-but-non-zero smartctl status.
func ParseSMARTJSON(data []byte) (SMARTSummary, map[string]any, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return SMARTSummary{Available: false, Supported: false, Reason: "smartctl returned invalid JSON"}, nil, err
	}
	return ParseSMARTSummary(raw), raw, nil
}

// ParseSMARTSummary tolerates ATA, SCSI and NVMe smartctl JSON variants.
// Missing or vendor-specific fields are omitted rather than guessed.
func ParseSMARTSummary(raw map[string]any) SMARTSummary {
	summary := SMARTSummary{}
	if raw == nil {
		summary.Reason = "smartctl returned an empty JSON object"
		return summary
	}

	if available, ok := boolAt(raw, "smart_support", "available"); ok {
		summary.Available = available
	}
	if enabled, ok := boolAt(raw, "smart_support", "enabled"); ok {
		summary.Enabled = &enabled
	}
	hasSMARTData := hasObject(raw, "ata_smart_data") || hasObject(raw, "ata_smart_attributes") || hasObject(raw, "nvme_smart_health_information_log")
	summary.Supported = summary.Available || hasSMARTData
	if !summary.Available && hasSMARTData {
		summary.Available = true
	}

	if passed, ok := boolAt(raw, "smart_status", "passed"); ok {
		summary.Passed = &passed
		if passed {
			summary.Health = "passed"
		} else {
			summary.Health = "failed"
		}
	}
	if summary.Health == "" {
		if summary.Supported {
			summary.Health = "unknown"
		} else {
			summary.Health = "unsupported"
		}
	}

	summary.TemperatureC = firstFloat(raw,
		[]string{"temperature", "current"},
		[]string{"nvme_smart_health_information_log", "temperature"},
	)
	summary.PowerOnHours = firstUint(raw,
		[]string{"power_on_time", "hours"},
		[]string{"nvme_smart_health_information_log", "power_on_hours"},
	)
	summary.PercentageUsed = firstFloat(raw,
		[]string{"nvme_smart_health_information_log", "percentage_used"},
		[]string{"nvme_smart_health_information_log", "percent_used"},
	)
	summary.CriticalWarning = firstUint(raw, []string{"nvme_smart_health_information_log", "critical_warning"})
	summary.ErrorCount = firstUint(raw,
		[]string{"nvme_smart_health_information_log", "num_err_log_entries"},
		[]string{"nvme_smart_health_information_log", "error_count"},
		[]string{"ata_smart_error_log", "summary", "count"},
	)
	summary.MediaAndDataErrors = firstUint(raw,
		[]string{"nvme_smart_health_information_log", "media_errors"},
		[]string{"nvme_smart_health_information_log", "media_and_data_integrity_errors"},
	)

	attrs := smartAttributes(raw)
	if summary.TemperatureC == nil {
		summary.TemperatureC = attributeFloat(attrs, []string{"temperature_celsius", "airflow_temperature_cel", "temperature"}, []int{194, 190})
	}
	if summary.PowerOnHours == nil {
		summary.PowerOnHours = attributeUint(attrs, []string{"power_on_hours"}, []int{9})
	}
	summary.ReallocatedSectors = attributeUint(attrs, []string{"reallocated_sector_ct", "reallocated_event_count"}, []int{5, 196})
	summary.PendingSectors = attributeUint(attrs, []string{"current_pending_sector"}, []int{197})
	summary.OfflineUncorrectable = attributeUint(attrs, []string{"offline_uncorrectable"}, []int{offlineUncorrectableID})
	summary.UDMACRCErrors = attributeUint(attrs, []string{"udma_crc_error_count"}, []int{199})
	if summary.SelfTestStatus == "" {
		summary.SelfTestStatus = smartSelfTestStatus(raw)
	}

	switch {
	case !summary.Supported:
		summary.Reason = "smartctl reported that SMART data is unavailable"
	case summary.Enabled != nil && !*summary.Enabled:
		summary.Reason = "SMART is supported but disabled"
	case summary.Passed != nil && !*summary.Passed:
		summary.Reason = "SMART health check failed"
	}
	return summary
}

const offlineUncorrectableID = 198

type smartAttribute struct {
	ID    int
	Name  string
	Raw   any
	Value any
}

func smartAttributes(raw map[string]any) []smartAttribute {
	container, ok := raw["ata_smart_attributes"].(map[string]any)
	if !ok {
		return nil
	}
	table, ok := container["table"].([]any)
	if !ok {
		return nil
	}
	attrs := make([]smartAttribute, 0, len(table))
	for _, entry := range table {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := intValue(item["id"])
		name, _ := stringValue(item["name"])
		attrs = append(attrs, smartAttribute{ID: id, Name: normalizeSmartName(name), Raw: item["raw"], Value: item["value"]})
	}
	return attrs
}

func attributeUint(attrs []smartAttribute, names []string, ids []int) *uint64 {
	for _, attr := range attrs {
		if !attributeMatches(attr, names, ids) {
			continue
		}
		if value, ok := smartRawValue(attr.Raw); ok {
			if parsed, ok := uintValue(value); ok {
				return &parsed
			}
		}
		if value, ok := uintValue(attr.Raw); ok {
			return &value
		}
		if value, ok := uintValue(attr.Value); ok {
			return &value
		}
	}
	return nil
}

func attributeFloat(attrs []smartAttribute, names []string, ids []int) *float64 {
	for _, attr := range attrs {
		if !attributeMatches(attr, names, ids) {
			continue
		}
		if value, ok := smartRawValue(attr.Raw); ok {
			if parsed, ok := floatValue(value); ok {
				return &parsed
			}
		}
		if value, ok := floatValue(attr.Raw); ok {
			return &value
		}
		if value, ok := floatValue(attr.Value); ok {
			return &value
		}
	}
	return nil
}

func smartRawValue(raw any) (any, bool) {
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	value, ok := object["value"]
	return value, ok
}

func attributeMatches(attr smartAttribute, names []string, ids []int) bool {
	for _, name := range names {
		if attr.Name == normalizeSmartName(name) {
			return true
		}
	}
	for _, id := range ids {
		if attr.ID == id {
			return true
		}
	}
	return false
}

func smartSelfTestStatus(raw map[string]any) string {
	container, ok := raw["ata_smart_self_test"].(map[string]any)
	if !ok {
		return ""
	}
	if status, ok := stringAt(container, "status", "string"); ok {
		return status
	}
	if status, ok := stringAt(container, "status", "value"); ok {
		return status
	}
	return ""
}

func hasObject(root map[string]any, path ...string) bool {
	value, ok := valueAt(root, path...)
	if !ok {
		return false
	}
	_, ok = value.(map[string]any)
	return ok
}

func boolAt(root map[string]any, path ...string) (bool, bool) {
	value, ok := valueAt(root, path...)
	if !ok {
		return false, false
	}
	return boolValue(value)
}

func stringAt(root map[string]any, path ...string) (string, bool) {
	value, ok := valueAt(root, path...)
	if !ok {
		return "", false
	}
	return stringValue(value)
}

func firstFloat(root map[string]any, paths ...[]string) *float64 {
	for _, path := range paths {
		value, ok := valueAt(root, path...)
		if !ok {
			continue
		}
		if parsed, ok := floatValue(value); ok {
			return &parsed
		}
	}
	return nil
}

func firstUint(root map[string]any, paths ...[]string) *uint64 {
	for _, path := range paths {
		value, ok := valueAt(root, path...)
		if !ok {
			continue
		}
		if parsed, ok := uintValue(value); ok {
			return &parsed
		}
	}
	return nil
}

func valueAt(root map[string]any, path ...string) (any, bool) {
	var current any = root
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[key]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func boolValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return false, false
	}
}

func stringValue(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case json.Number:
		return typed.String(), true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	default:
		return "", false
	}
}

func uintValue(value any) (uint64, bool) {
	switch typed := value.(type) {
	case uint64:
		return typed, true
	case uint:
		return uint64(typed), true
	case uint32:
		return uint64(typed), true
	case int:
		if typed >= 0 {
			return uint64(typed), true
		}
	case int64:
		if typed >= 0 {
			return uint64(typed), true
		}
	case float64:
		if typed >= 0 && typed <= float64(^uint64(0)) {
			return uint64(typed), true
		}
	case json.Number:
		parsed, err := strconv.ParseUint(typed.String(), 10, 64)
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		return parsed, err == nil
	}
	return 0, false
}

func floatValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func intValue(value any) (int, bool) {
	parsed, ok := uintValue(value)
	if !ok || parsed > uint64(^uint(0)>>1) {
		return 0, false
	}
	return int(parsed), true
}

func normalizeSmartName(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}
