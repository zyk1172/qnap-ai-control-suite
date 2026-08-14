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
	"sort"
	"strconv"
	"strings"
	"time"
)

type Service struct{ Exec qexec.Executor }
type Disk struct {
	ID, Path, Name, Model, Serial, Firmware, Transport string
	SizeBytes                                          uint64
	Rotational                                         bool
	Temperature                                        *float64
	SmartSupported                                     bool
	Raw                                                map[string]any
}
type RAID struct {
	Name, Level, State, Raw string
	Members                 []string
	Degraded                bool
}
type Pool struct {
	Name, Backend, Health, Size, Alloc, Free string
	Raw                                      map[string]any
}
type Volume struct {
	Name, Mountpoint, Filesystem, Source, Size, Used, Available string
	Backend                                                     string
}
type Snapshot struct {
	Name, Dataset, Backend, Created string
	Raw                             map[string]any
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
	out := []RAID{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !strings.HasPrefix(f[0], "md") {
			continue
		}
		r := RAID{Name: f[0], State: f[1], Level: f[3], Raw: line}
		for _, member := range f[4:] {
			if i := strings.Index(member, "["); i > 0 {
				r.Members = append(r.Members, member[:i])
			}
		}
		r.Degraded = strings.Contains(line, "_")
		out = append(out, r)
	}
	return out, nil
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
