package network

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"strconv"
	"strings"
	"time"
)

type Service struct{ Exec qexec.Executor }
type Interface struct {
	Name, MAC, State, Speed, Duplex string
	Index                           int
	Flags                           []string
	Addresses                       []string
	Virtual                         bool
}
type Route struct {
	Destination, Gateway, Interface string
	Metric                          int
}
type DNS struct{ Servers, Search []string }

func (s Service) Interfaces() ([]Interface, error) {
	list, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]Interface, 0, len(list))
	for _, iface := range list {
		entry := Interface{Name: iface.Name, MAC: iface.HardwareAddr.String(), Index: iface.Index, Flags: strings.Fields(iface.Flags.String())}
		if addrs, err := iface.Addrs(); err == nil {
			for _, addr := range addrs {
				entry.Addresses = append(entry.Addresses, addr.String())
			}
		}
		entry.State = read(filepath.Join("/sys/class/net", iface.Name, "operstate"))
		entry.Speed = read(filepath.Join("/sys/class/net", iface.Name, "speed"))
		entry.Duplex = read(filepath.Join("/sys/class/net", iface.Name, "duplex"))
		entry.Virtual = strings.HasPrefix(iface.Name, "br") || strings.HasPrefix(iface.Name, "bond") || strings.HasPrefix(iface.Name, "vlan")
		out = append(out, entry)
	}
	return out, nil
}
func (s Service) Routes() ([]Route, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []Route{}
	scan := bufio.NewScanner(f)
	scan.Scan()
	for scan.Scan() {
		p := strings.Fields(scan.Text())
		if len(p) < 8 {
			continue
		}
		gateway := decodeIPv4(p[2])
		destination := decodeIPv4(p[1])
		metric, _ := strconv.Atoi(p[6])
		out = append(out, Route{Destination: destination, Gateway: gateway, Interface: p[0], Metric: metric})
	}
	return out, nil
}
func (s Service) DNS() DNS {
	out := DNS{}
	b, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "nameserver":
			out.Servers = append(out.Servers, f[1])
		case "search":
			out.Search = append(out.Search, f[1:]...)
		}
	}
	return out
}
func (s Service) RunIP(ctx context.Context, args []string) (qexec.Result, error) {
	path, err := findIP()
	if err != nil {
		return qexec.Result{}, err
	}
	return s.Exec.Run(ctx, qexec.Request{Argv: append([]string{path}, args...), Timeout: 30 * time.Second, MaxOutput: s.Exec.MaxOutput})
}
func read(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func decodeIPv4(hexValue string) string {
	if len(hexValue) != 8 {
		return hexValue
	}
	parts := []string{}
	for i := 6; i >= 0; i -= 2 {
		v, _ := strconv.ParseUint(hexValue[i:i+2], 16, 8)
		parts = append(parts, strconv.FormatUint(v, 10))
	}
	return strings.Join(parts, ".")
}
func findIP() (string, error) {
	for _, p := range []string{"/sbin/ip", "/usr/sbin/ip", "/bin/ip", "/usr/bin/ip"} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("ip utility unavailable; use QNAP runtime probe")
}
