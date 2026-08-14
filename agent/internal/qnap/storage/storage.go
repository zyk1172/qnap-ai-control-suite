package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	Exec         qexec.Executor
	SysBlockRoot string
}
type Disk struct {
	ID             string         `json:"id"`
	Path           string         `json:"path"`
	Name           string         `json:"name"`
	Model          string         `json:"model"`
	Serial         string         `json:"serial"`
	Firmware       string         `json:"firmware"`
	Transport      string         `json:"transport"`
	SizeBytes      uint64         `json:"size_bytes"`
	Rotational     bool           `json:"rotational"`
	Temperature    *float64       `json:"temperature,omitempty"`
	SmartSupported bool           `json:"smart_supported"`
	Raw            map[string]any `json:"raw"`
}
type RAID struct {
	Name          string    `json:"name"`
	Level         string    `json:"level"`
	State         string    `json:"state"`
	Raw           string    `json:"raw"`
	Members       []string  `json:"members"`
	TotalDevices  int       `json:"total_devices,omitempty"`
	ActiveDevices int       `json:"active_devices,omitempty"`
	Layout        string    `json:"layout,omitempty"`
	Degraded      bool      `json:"degraded"`
	Sync          *RAIDSync `json:"sync,omitempty"`
}
type RAIDSync struct {
	Action   string   `json:"action"`
	Progress *float64 `json:"progress_percent,omitempty"`
	Finish   string   `json:"finish,omitempty"`
	Speed    string   `json:"speed,omitempty"`
	Raw      string   `json:"raw"`
}
type RAIDActionResult struct {
	Name     string `json:"name"`
	Action   string `json:"action"`
	Previous string `json:"previous"`
	Current  string `json:"current"`
	Applied  bool   `json:"applied"`
	DryRun   bool   `json:"dry_run"`
}
type Pool struct {
	Name    string         `json:"name"`
	Backend string         `json:"backend"`
	Health  string         `json:"health"`
	Size    string         `json:"size"`
	Alloc   string         `json:"alloc"`
	Free    string         `json:"free"`
	Raw     map[string]any `json:"raw"`
}
type Volume struct {
	Name       string `json:"name"`
	Mountpoint string `json:"mountpoint"`
	Filesystem string `json:"filesystem"`
	Source     string `json:"source"`
	Size       string `json:"size"`
	Used       string `json:"used"`
	Available  string `json:"available"`
	Backend    string `json:"backend"`
}
type Snapshot struct {
	Name    string         `json:"name"`
	Dataset string         `json:"dataset"`
	Backend string         `json:"backend"`
	Created string         `json:"created"`
	Raw     map[string]any `json:"raw"`
}

func (s Service) QTSInventory(ctx context.Context) (map[string]any, error) {
	path := qcli()
	if path == "" {
		return map[string]any{"supported": false, "reason": "qcli_storage was not discovered"}, nil
	}
	out := map[string]any{"supported": true, "backend": "qts-qcli", "commands": map[string]qexec.Result{}}
	commands := out["commands"].(map[string]qexec.Result)
	for key, args := range map[string][]string{"pools": {"-p"}, "volumes": {"-v"}, "disks": {"-d"}} {
		result, err := s.Exec.Run(ctx, qexec.Request{Argv: append([]string{path}, args...), Timeout: 30 * time.Second, MaxOutput: s.Exec.MaxOutput})
		if err != nil {
			return out, err
		}
		commands[key] = result
		switch key {
		case "pools":
			out[key] = ParseQTSPools(result.Stdout)
		case "volumes":
			out[key] = ParseQTSVolumes(result.Stdout)
		case "disks":
			out[key] = ParseQTSDisks(result.Stdout)
		}
	}
	return out, nil
}

// ParseQTSPools preserves rows from qcli_storage -p without assuming a
// firmware-specific column layout. QTS changes this table between releases.
func ParseQTSPools(stdout string) []map[string]any {
	items := []map[string]any{}
	for _, fields := range qcliRows(stdout) {
		item := map[string]any{"fields": fields, "raw": strings.Join(fields, " ")}
		for _, field := range fields {
			if strings.HasPrefix(field, "/dev/") {
				item["device"] = field
				break
			}
		}
		items = append(items, item)
	}
	return items
}

// ParseQTSVolumes extracts the stable leading volume id/name and mount path
// emitted by qcli_storage -v while keeping every original table cell.
func ParseQTSVolumes(stdout string) []map[string]any {
	items := []map[string]any{}
	for _, fields := range qcliRows(stdout) {
		item := map[string]any{"fields": fields, "raw": strings.Join(fields, " ")}
		if len(fields) > 0 {
			if _, err := strconv.Atoi(fields[0]); err == nil {
				item["id"] = fields[0]
			}
		}
		if len(fields) > 1 {
			item["name"] = fields[1]
		}
		for _, field := range fields {
			if strings.HasPrefix(field, "/") {
				item["mountpoint"] = field
				break
			}
		}
		items = append(items, item)
	}
	return items
}

// ParseQTSDisks emits QTS disk-table rows and locates a device path when one
// is present. Model/slot columns remain in fields because their order varies.
func ParseQTSDisks(stdout string) []map[string]any {
	items := []map[string]any{}
	for _, fields := range qcliRows(stdout) {
		item := map[string]any{"fields": fields, "raw": strings.Join(fields, " ")}
		for _, field := range fields {
			if strings.HasPrefix(field, "/dev/") {
				item["device"] = field
				break
			}
		}
		items = append(items, item)
	}
	return items
}

func qcliRows(stdout string) [][]string {
	rows := [][]string{}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Trim(line, "-_=+ ") == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || looksLikeQCLIHeader(fields) {
			continue
		}
		rows = append(rows, fields)
	}
	return rows
}

func looksLikeQCLIHeader(fields []string) bool {
	line := strings.ToLower(strings.Join(fields, " "))
	return strings.Contains(line, "volume") && strings.Contains(line, "name") ||
		strings.Contains(line, "disk") && strings.Contains(line, "model") ||
		strings.Contains(line, "pool") && strings.Contains(line, "name")
}

func (s Service) Disks() ([]Disk, error) {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil, err
	}
	out := []Disk{}
	for _, e := range entries {
		name := e.Name()
		if !(strings.HasPrefix(name, "sd") || strings.HasPrefix(name, "nvme") || strings.HasPrefix(name, "vd")) {
			continue
		}
		base := filepath.Join("/sys/block", name)
		sectors, _ := strconv.ParseUint(read(base, "size"), 10, 64)
		rot := read(base, "queue/rotational") == "1"
		d := Disk{ID: name, Name: name, Path: "/dev/" + name, Model: read(base, "device/model"), Serial: read(base, "device/serial"), Firmware: read(base, "device/firmware_rev"), SizeBytes: sectors * 512, Rotational: rot, Transport: transport(name), Raw: map[string]any{"sysfs": base}}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (s Service) Disk(id string) (Disk, error) {
	disks, err := s.Disks()
	if err != nil {
		return Disk{}, err
	}
	for _, d := range disks {
		if d.ID == id {
			return d, nil
		}
	}
	return Disk{}, errors.New("disk not found")
}
func (s Service) Smart(ctx context.Context, id string) (map[string]any, error) {
	d, err := s.Disk(id)
	if err != nil {
		return nil, err
	}
	path := smartctl()
	if path == "" {
		return map[string]any{"supported": false, "reason": "smartctl not found by runtime probe", "disk": d}, nil
	}
	result, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "-j", "-a", d.Path}, Timeout: 45 * time.Second, MaxOutput: s.Exec.MaxOutput})
	if err != nil {
		return map[string]any{"disk": d, "command": result}, err
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(result.Stdout), &raw); err != nil {
		return map[string]any{"disk": d, "command": result}, err
	}
	return map[string]any{"supported": true, "disk": d, "raw": raw}, nil
}
func (s Service) StartSmart(ctx context.Context, id, kind string) (qexec.Result, error) {
	if kind != "short" && kind != "long" {
		return qexec.Result{}, errors.New("kind must be short or long")
	}
	d, err := s.Disk(id)
	if err != nil {
		return qexec.Result{}, err
	}
	path := smartctl()
	if path == "" {
		return qexec.Result{}, errors.New("smartctl not found by runtime probe")
	}
	return s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "-t", kind, d.Path}, Timeout: 30 * time.Second, MaxOutput: s.Exec.MaxOutput})
}
func (s Service) RAID() ([]RAID, error) {
	b, err := os.ReadFile("/proc/mdstat")
	if err != nil {
		return nil, err
	}
	return ParseRAIDStatus(string(b)), nil
}

var (
	raidLayoutPattern   = regexp.MustCompile(`\[(\d+)/(\d+)\]\s+\[([U_]+)\]`)
	raidProgressPattern = regexp.MustCompile(`(?i)(recovery|resync|reshape|check|repair|resilver)\s*=\s*([0-9]+(?:\.[0-9]+)?)%`)
	raidFieldPattern    = regexp.MustCompile(`(?:^|\s)(finish|speed)=([^\s]+)`)
	raidNamePattern     = regexp.MustCompile(`^md[0-9]+$`)
)

// ParseRAIDStatus parses /proc/mdstat as multi-line array records. The
// original one-line parser lost the bitmap and resync/rebuild progress that
// Linux emits on the following lines.
func ParseRAIDStatus(input string) []RAID {
	items := []RAID{}
	var current *RAID
	flush := func() {
		if current != nil {
			items = append(items, *current)
			current = nil
		}
	}
	for _, raw := range strings.Split(input, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			flush()
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 2 && raidNamePattern.MatchString(fields[0]) && fields[1] == ":" {
			flush()
			current = &RAID{Name: fields[0], Raw: raw}
			levelAt := -1
			for i, field := range fields[2:] {
				if strings.HasPrefix(field, "raid") {
					current.Level = field
					levelAt = i + 2
					break
				}
				if current.State == "" {
					current.State = field
				}
			}
			if levelAt >= 0 {
				for _, member := range fields[levelAt+1:] {
					if i := strings.Index(member, "["); i > 0 {
						current.Members = append(current.Members, member[:i])
					}
				}
			}
			continue
		}
		if current == nil {
			continue
		}
		current.Raw += "\n" + raw
		if match := raidLayoutPattern.FindStringSubmatch(line); len(match) == 4 {
			current.TotalDevices, _ = strconv.Atoi(match[1])
			current.ActiveDevices, _ = strconv.Atoi(match[2])
			current.Layout = match[3]
			current.Degraded = strings.Contains(match[3], "_")
		}
		if match := raidProgressPattern.FindStringSubmatch(line); len(match) == 3 {
			progress, _ := strconv.ParseFloat(match[2], 64)
			current.Sync = &RAIDSync{Action: strings.ToLower(match[1]), Progress: &progress, Raw: strings.TrimSpace(raw)}
			for _, field := range raidFieldPattern.FindAllStringSubmatch(line, -1) {
				switch field[1] {
				case "finish":
					current.Sync.Finish = field[2]
				case "speed":
					current.Sync.Speed = field[2]
				}
			}
		}
	}
	flush()
	return items
}

// RAIDAction controls only the stable Linux mdraid sync_action sysfs file.
// It intentionally does not claim QTS pool/volume management support.
func (s Service) RAIDAction(name, action string, dryRun bool) (RAIDActionResult, error) {
	if !raidNamePattern.MatchString(name) {
		return RAIDActionResult{}, errors.New("RAID name must be an md device such as md0")
	}
	value := ""
	switch action {
	case "scrub_start":
		value = "check"
	case "scrub_stop":
		value = "idle"
	default:
		return RAIDActionResult{}, errors.New("action must be scrub_start or scrub_stop")
	}
	root := s.SysBlockRoot
	if root == "" {
		root = "/sys/block"
	}
	path := filepath.Join(root, name, "md", "sync_action")
	previousBytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RAIDActionResult{}, fmt.Errorf("mdraid sync control is unavailable for %s", name)
		}
		return RAIDActionResult{}, err
	}
	result := RAIDActionResult{Name: name, Action: action, Previous: strings.TrimSpace(string(previousBytes)), Current: value, DryRun: dryRun}
	if dryRun {
		return result, nil
	}
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		return result, err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	result.Current = strings.TrimSpace(string(current))
	// A small or idle array can complete a check between the write and this
	// read. Treat that terminal state as a successful start, while still
	// returning the observed state for the caller to inspect.
	result.Applied = result.Current == value || (value == "check" && result.Current == "idle")
	return result, nil
}
func (s Service) Pools(ctx context.Context) ([]Pool, error) {
	path := zpool()
	if path == "" {
		return []Pool{}, nil
	}
	r, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "list", "-H", "-o", "name,size,alloc,free,health"}, Timeout: 20 * time.Second})
	if err != nil {
		return nil, err
	}
	out := []Pool{}
	for _, line := range strings.Split(strings.TrimSpace(r.Stdout), "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 {
			out = append(out, Pool{Name: f[0], Size: f[1], Alloc: f[2], Free: f[3], Health: f[4], Backend: "zfs"})
		}
	}
	return out, nil
}
func (s Service) Volumes() ([]Volume, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []Volume{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		v := strings.Fields(scan.Text())
		if len(v) < 3 {
			continue
		}
		out = append(out, Volume{Name: filepath.Base(v[1]), Mountpoint: v[1], Source: v[0], Filesystem: v[2], Backend: volumeBackend(v[2])})
	}
	return out, nil
}
func (s Service) Snapshots(ctx context.Context) ([]Snapshot, error) {
	path := zfs()
	if path == "" {
		return []Snapshot{}, nil
	}
	r, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "list", "-H", "-t", "snapshot", "-o", "name,creation"}, Timeout: 20 * time.Second})
	if err != nil {
		return nil, err
	}
	out := []Snapshot{}
	for _, line := range strings.Split(strings.TrimSpace(r.Stdout), "\n") {
		f := strings.SplitN(line, "\t", 2)
		if len(f) == 0 || f[0] == "" {
			continue
		}
		dataset := strings.SplitN(f[0], "@", 2)[0]
		created := ""
		if len(f) > 1 {
			created = f[1]
		}
		out = append(out, Snapshot{Name: f[0], Dataset: dataset, Created: created, Backend: "zfs"})
	}
	return out, nil
}
func (s Service) SnapshotAction(ctx context.Context, action, name, target string) (qexec.Result, error) {
	path := zfs()
	if path == "" {
		return qexec.Result{}, errors.New("no stable snapshot backend discovered; use nas_exec after qnap probe")
	}
	switch action {
	case "create":
		return s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "snapshot", name}, Timeout: 60 * time.Second, MaxOutput: s.Exec.MaxOutput})
	case "delete":
		return s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "destroy", name}, Timeout: 60 * time.Second, MaxOutput: s.Exec.MaxOutput})
	case "clone":
		return s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "clone", name, target}, Timeout: 60 * time.Second, MaxOutput: s.Exec.MaxOutput})
	case "restore":
		return s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "rollback", name}, Timeout: 60 * time.Second, MaxOutput: s.Exec.MaxOutput})
	}
	return qexec.Result{}, fmt.Errorf("unsupported snapshot action: %s", action)
}

func (s Service) QTSSnapshotCapabilities() map[string]any {
	path := snapshotUtil()
	if path == "" {
		return map[string]any{"supported": false, "reason": "QTS snapshot_util was not discovered"}
	}
	return map[string]any{"supported": true, "backend": "qts-snapshot_util", "operations": []string{"create"}, "reason": "list/delete/restore require further runtime probe"}
}

// CreateQTSSnapshot uses QTS snapshot_util commands observed on the NAS:
// get_volume_id, check_volume, and create_snapshot_for_app.
func (s Service) CreateQTSSnapshot(ctx context.Context, volume, name string) (map[string]any, error) {
	path := snapshotUtil()
	if path == "" {
		return nil, errors.New("QTS snapshot_util was not discovered")
	}
	if !filepath.IsAbs(volume) || strings.TrimSpace(name) == "" || strings.ContainsAny(name, "/\\\x00\n\r") {
		return nil, errors.New("volume must be an absolute mount path and snapshot name must be a simple name")
	}
	idResult, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "get_volume_id", volume}, Timeout: 30 * time.Second, MaxOutput: s.Exec.MaxOutput})
	if err != nil {
		return map[string]any{"volume_id_command": idResult}, err
	}
	volumeID := strings.TrimSpace(idResult.Stdout)
	if volumeID == "" {
		return map[string]any{"volume_id_command": idResult}, errors.New("snapshot_util returned an empty volume id")
	}
	checkResult, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "check_volume", volumeID}, Timeout: 30 * time.Second, MaxOutput: s.Exec.MaxOutput})
	if err != nil {
		return map[string]any{"volume_id": volumeID, "check_volume_command": checkResult}, err
	}
	createResult, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{path, "create_snapshot_for_app", volumeID, name}, Timeout: 90 * time.Second, MaxOutput: s.Exec.MaxOutput})
	return map[string]any{"backend": "qts-snapshot_util", "volume": volume, "volume_id": volumeID, "name": name, "volume_id_command": idResult, "check_volume_command": checkResult, "create_command": createResult}, err
}
func read(base, name string) string {
	b, err := os.ReadFile(filepath.Join(base, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func transport(name string) string {
	if strings.HasPrefix(name, "nvme") {
		return "nvme"
	}
	if strings.HasPrefix(name, "sd") {
		return "scsi_or_sata"
	}
	return "virtual"
}
func smartctl() string {
	return executable([]string{"/sbin/smartctl", "/usr/sbin/smartctl", "/bin/smartctl", "/usr/bin/smartctl"})
}
func zpool() string {
	return executable([]string{"/sbin/zpool", "/usr/sbin/zpool", "/bin/zpool", "/usr/bin/zpool"})
}
func zfs() string {
	return executable([]string{"/sbin/zfs", "/usr/sbin/zfs", "/bin/zfs", "/usr/bin/zfs"})
}
func qcli() string {
	return executable([]string{"/sbin/qcli_storage", "/usr/sbin/qcli_storage", "/bin/qcli_storage", "/usr/bin/qcli_storage"})
}
func snapshotUtil() string {
	return executable([]string{"/sbin/snapshot_util", "/usr/sbin/snapshot_util", "/bin/snapshot_util", "/usr/bin/snapshot_util"})
}
func executable(paths []string) string {
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return ""
}
func volumeBackend(fs string) string {
	if fs == "zfs" {
		return "zfs"
	}
	if strings.Contains(fs, "ext") || fs == "xfs" {
		return "qts_lvm_or_linux"
	}
	return "linux"
}
