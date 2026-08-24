package storage

import (
	"bufio"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DiskIOMetrics contains the cumulative counters exported by Linux
// /proc/diskstats plus metrics derived from those counters. The counters are
// cumulative from the kernel's point of view; consumers that need a current
// rate should compare two observations of the same disk.
//
// average_latency_ms is the cumulative average request latency derived from
// weighted_io_time_ms / completed operations. average_queue_depth is the
// average number of requests in flight while the device was busy, derived
// from weighted_io_time_ms / io_time_ms. busy_ratio is the fraction of time
// the device has been busy since the kernel boot time reported by
// /proc/uptime, not a short-term utilization sample.
type DiskIOMetrics struct {
	Available bool   `json:"available"`
	Source    string `json:"source,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Major     uint64 `json:"major"`
	Minor     uint64 `json:"minor"`

	ReadsCompleted uint64 `json:"reads_completed"`
	ReadsMerged    uint64 `json:"reads_merged"`
	SectorsRead    uint64 `json:"sectors_read"`
	ReadBytes      uint64 `json:"read_bytes"`
	ReadTimeMS     uint64 `json:"read_time_ms"`

	WritesCompleted uint64 `json:"writes_completed"`
	WritesMerged    uint64 `json:"writes_merged"`
	SectorsWritten  uint64 `json:"sectors_written"`
	WriteBytes      uint64 `json:"write_bytes"`
	WriteTimeMS     uint64 `json:"write_time_ms"`

	IOInProgress      uint64   `json:"io_in_progress"`
	IOTimeMS          uint64   `json:"io_time_ms"`
	WeightedIOTimeMS  uint64   `json:"weighted_io_time_ms"`
	DiscardsCompleted uint64   `json:"discards_completed,omitempty"`
	DiscardsMerged    uint64   `json:"discards_merged,omitempty"`
	SectorsDiscarded  uint64   `json:"sectors_discarded,omitempty"`
	DiscardBytes      uint64   `json:"discard_bytes,omitempty"`
	DiscardTimeMS     uint64   `json:"discard_time_ms,omitempty"`
	FlushesCompleted  uint64   `json:"flushes_completed,omitempty"`
	FlushTimeMS       uint64   `json:"flush_time_ms,omitempty"`
	CurrentlyBusy     bool     `json:"currently_busy"`
	AverageLatencyMS  *float64 `json:"average_latency_ms,omitempty"`
	AverageQueueDepth *float64 `json:"average_queue_depth,omitempty"`
	BusyRatio         *float64 `json:"busy_ratio,omitempty"`
	BusyRatioWindow   string   `json:"busy_ratio_window,omitempty"`
	ObservedAt        string   `json:"observed_at,omitempty"`
}

// DiskIO reads and explains all valid rows in /proc/diskstats. A malformed
// individual row is ignored so one firmware-specific or transient line does
// not make the entire storage inventory unavailable.
func (s Service) DiskIO() (map[string]DiskIOMetrics, error) {
	path := filepath.Join(s.procRoot(), "diskstats")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	items := ParseDiskstats(string(b))
	uptimeMS, uptimeOK := readProcUptimeMS(filepath.Join(s.procRoot(), "uptime"))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for name, item := range items {
		item.Source = "/proc/diskstats"
		item.ObservedAt = now
		item.CurrentlyBusy = item.IOInProgress > 0
		item.AverageLatencyMS = averageLatency(item)
		item.AverageQueueDepth = averageQueueDepth(item)
		if uptimeOK && uptimeMS > 0 {
			ratio := float64(item.IOTimeMS) / uptimeMS
			if ratio > 1 {
				ratio = 1
			}
			item.BusyRatio = &ratio
			item.BusyRatioWindow = "since_boot"
		}
		items[name] = item
	}
	return items, nil
}

// ParseDiskstats parses the stable first eleven fields of a Linux
// /proc/diskstats row and the optional discard/flush fields introduced by
// newer kernels. It intentionally returns a map without an error: callers
// can still use all valid devices when a row is truncated or malformed.
func ParseDiskstats(input string) map[string]DiskIOMetrics {
	items := map[string]DiskIOMetrics{}
	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 {
			continue
		}
		major, okMajor := parseUint(fields[0])
		minor, okMinor := parseUint(fields[1])
		if !okMajor || !okMinor || fields[2] == "" {
			continue
		}
		values := make([]uint64, len(fields)-3)
		valid := true
		for i, field := range fields[3:] {
			values[i], valid = parseUint(field)
			if !valid {
				break
			}
		}
		if !valid || len(values) < 11 {
			continue
		}
		item := DiskIOMetrics{
			Available:        true,
			Major:            major,
			Minor:            minor,
			ReadsCompleted:   values[0],
			ReadsMerged:      values[1],
			SectorsRead:      values[2],
			ReadBytes:        sectorsToBytes(values[2]),
			ReadTimeMS:       values[3],
			WritesCompleted:  values[4],
			WritesMerged:     values[5],
			SectorsWritten:   values[6],
			WriteBytes:       sectorsToBytes(values[6]),
			WriteTimeMS:      values[7],
			IOInProgress:     values[8],
			IOTimeMS:         values[9],
			WeightedIOTimeMS: values[10],
		}
		if len(values) >= 15 {
			item.DiscardsCompleted = values[11]
			item.DiscardsMerged = values[12]
			item.SectorsDiscarded = values[13]
			item.DiscardBytes = sectorsToBytes(values[13])
			item.DiscardTimeMS = values[14]
		}
		if len(values) >= 17 {
			item.FlushesCompleted = values[15]
			item.FlushTimeMS = values[16]
		}
		items[fields[2]] = item
	}
	return items
}

func averageLatency(item DiskIOMetrics) *float64 {
	completed := saturatingAdd(item.ReadsCompleted, item.WritesCompleted)
	completed = saturatingAdd(completed, item.DiscardsCompleted)
	completed = saturatingAdd(completed, item.FlushesCompleted)
	if completed == 0 {
		return nil
	}
	value := float64(item.WeightedIOTimeMS) / float64(completed)
	return &value
}

func averageQueueDepth(item DiskIOMetrics) *float64 {
	if item.IOTimeMS == 0 {
		return nil
	}
	value := float64(item.WeightedIOTimeMS) / float64(item.IOTimeMS)
	return &value
}

func readProcUptimeMS(path string) (float64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds <= 0 {
		return 0, false
	}
	return seconds * 1000, true
}

func parseUint(value string) (uint64, bool) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	return parsed, err == nil
}

func sectorsToBytes(sectors uint64) uint64 {
	const sectorSize = uint64(512)
	if sectors > math.MaxUint64/sectorSize {
		return math.MaxUint64
	}
	return sectors * sectorSize
}

func saturatingAdd(a, b uint64) uint64 {
	if math.MaxUint64-a < b {
		return math.MaxUint64
	}
	return a + b
}

func sysfsSmartSupported(base string) bool {
	for _, name := range []string{"device/smart_supported", "device/smart_support", "smart_supported"} {
		value := strings.ToLower(strings.TrimSpace(read(base, name)))
		switch value {
		case "1", "true", "yes", "supported", "available":
			return true
		case "0", "false", "no", "unsupported", "unavailable":
			return false
		}
	}
	return false
}
