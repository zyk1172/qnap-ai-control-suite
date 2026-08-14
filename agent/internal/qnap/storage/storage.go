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
