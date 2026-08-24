package network

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"strconv"
	"strings"
	"time"
)

var (
	ErrRoutesUnavailable        = errors.New("network route tables unavailable")
	ErrIPv6RoutesUnavailable    = errors.New("IPv6 route table unavailable")
	ErrIPv6NeighborsUnavailable = errors.New("IPv6 neighbor table unavailable")
)

type Service struct {
	Exec            qexec.Executor
	ProcRoot        string
	SysClassNetRoot string
	IPPath          string
}

type Interface struct {
	Name      string            `json:"name"`
	MAC       string            `json:"mac"`
	State     string            `json:"state"`
	Speed     string            `json:"speed"`
	Duplex    string            `json:"duplex"`
	Index     int               `json:"index"`
	Flags     []string          `json:"flags"`
	Addresses []string          `json:"addresses"`
	Virtual   bool              `json:"virtual"`
	Counters  InterfaceCounters `json:"counters"`
}

// InterfaceCounters contains the monotonically increasing counters exported
// by Linux under /sys/class/net/<name>/statistics. QNAP kernels can omit
// individual files, so Available indicates whether at least one counter was
// read successfully and missing fields remain zero.
type InterfaceCounters struct {
	Available bool   `json:"available"`
	RXBytes   uint64 `json:"rx_bytes"`
	RXPackets uint64 `json:"rx_packets"`
	RXErrors  uint64 `json:"rx_errors"`
	RXDropped uint64 `json:"rx_dropped"`
	TXBytes   uint64 `json:"tx_bytes"`
	TXPackets uint64 `json:"tx_packets"`
	TXErrors  uint64 `json:"tx_errors"`
	TXDropped uint64 `json:"tx_dropped"`
}

type Route struct {
	Family             string `json:"family,omitempty"`
	Destination        string `json:"destination"`
	PrefixLength       int    `json:"prefix_length,omitempty"`
	Source             string `json:"source,omitempty"`
	SourcePrefixLength int    `json:"source_prefix_length,omitempty"`
	Gateway            string `json:"gateway"`
	Interface          string `json:"interface"`
	Metric             int    `json:"metric"`
}

// Neighbor is an IPv6 neighbor-discovery entry. State and MAC are optional
// because incomplete and older procfs entries may not expose them.
type Neighbor struct {
	Family    string `json:"family,omitempty"`
	Address   string `json:"address"`
	Interface string `json:"interface,omitempty"`
	MAC       string `json:"mac,omitempty"`
	State     string `json:"state,omitempty"`
	Router    bool   `json:"router,omitempty"`
	Proxy     bool   `json:"proxy,omitempty"`
	Source    string `json:"source,omitempty"`
}

type DNS struct {
	Servers []string `json:"servers"`
	Search  []string `json:"search"`
}

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
		entry.State = read(filepath.Join(s.sysClassNetRoot(), iface.Name, "operstate"))
		entry.Speed = read(filepath.Join(s.sysClassNetRoot(), iface.Name, "speed"))
		entry.Duplex = read(filepath.Join(s.sysClassNetRoot(), iface.Name, "duplex"))
		entry.Virtual = strings.HasPrefix(iface.Name, "br") || strings.HasPrefix(iface.Name, "bond") || strings.HasPrefix(iface.Name, "vlan")
		entry.Counters = s.interfaceCounters(iface.Name)
		out = append(out, entry)
	}
	return out, nil
}

func (s Service) Routes() ([]Route, error) {
	out := make([]Route, 0)
	var opened bool
	var parseErrors []error

	for _, table := range []struct {
		name   string
		parser func(io.Reader) ([]Route, error)
	}{
		{name: "route", parser: parseIPv4Routes},
		{name: "ipv6_route", parser: parseIPv6Routes},
	} {
		items, available, err := s.readRouteTable(table.name, table.parser)
		if available {
			opened = true
			out = append(out, items...)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			parseErrors = append(parseErrors, err)
		}
	}
	if !opened {
		if len(parseErrors) == 0 {
			return out, ErrRoutesUnavailable
		}
		return out, fmt.Errorf("%w: %v", ErrRoutesUnavailable, errors.Join(parseErrors...))
	}
	if len(parseErrors) > 0 {
		return out, errors.Join(parseErrors...)
	}
	return out, nil
}

// IPv6Routes reads only the kernel IPv6 route table. Routes() is the
// compatibility-friendly aggregate used by the existing service API.
func (s Service) IPv6Routes() ([]Route, error) {
	items, available, err := s.readRouteTable("ipv6_route", parseIPv6Routes)
	if !available {
		if err == nil {
			err = os.ErrNotExist
		}
		return []Route{}, fmt.Errorf("%w: %v", ErrIPv6RoutesUnavailable, err)
	}
	return items, err
}

// Neighbors returns IPv6 neighbor-discovery entries. Older QNAP kernels may
// expose /proc/net/ndisc; newer Linux systems generally expose the same data
// through the standard iproute2 command, which is used as a fallback.
func (s Service) Neighbors() ([]Neighbor, error) {
	return s.NeighborsContext(context.Background())
}

// IPv6Neighbors is an explicit alias for callers that want the address family
// visible in the method name.
func (s Service) IPv6Neighbors() ([]Neighbor, error) {
	return s.Neighbors()
}

func (s Service) NeighborsContext(ctx context.Context) ([]Neighbor, error) {
	for _, name := range []string{"ndisc", "ndisc_cache"} {
		f, err := os.Open(filepath.Join(s.procRoot(), "net", name))
		if err != nil {
			continue
		}
		items, parseErr := parseProcNeighbors(f)
		_ = f.Close()
		if parseErr != nil {
			return items, parseErr
		}
		return items, nil
	}

	path, err := s.ipPath()
	if err == nil {
		result, runErr := s.Exec.Run(ctx, qexec.Request{
			Argv:      []string{path, "-6", "neigh", "show"},
			Timeout:   30 * time.Second,
			MaxOutput: s.Exec.MaxOutput,
		})
		if runErr == nil {
			return parseIPNeighbors(result.Stdout), nil
		}
		err = runErr
	}
	return []Neighbor{}, fmt.Errorf("%w: %v", ErrIPv6NeighborsUnavailable, err)
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
	path, err := s.ipPath()
	if err != nil {
		return qexec.Result{}, err
	}
	return s.Exec.Run(ctx, qexec.Request{Argv: append([]string{path}, args...), Timeout: 30 * time.Second, MaxOutput: s.Exec.MaxOutput})
}

// CommandArgs translates the network API's declarative actions to ip argv.
// These are transient Linux network changes; QTS persistent configuration and
// Virtual Switch internals intentionally remain separate runtime adapters.
func CommandArgs(action, iface, value, gateway string, metric int) ([]string, error) {
	if !validInterface(iface) {
		return nil, errors.New("invalid interface")
	}
	switch action {
	case "set_mtu":
		mtu, err := strconv.Atoi(value)
		if err != nil || mtu < 576 || mtu > 9216 {
			return nil, errors.New("mtu must be between 576 and 9216")
		}
		return []string{"link", "set", "dev", iface, "mtu", strconv.Itoa(mtu)}, nil
	case "set_state":
		if value != "up" && value != "down" {
			return nil, errors.New("state must be up or down")
		}
		return []string{"link", "set", "dev", iface, value}, nil
	case "address_add", "address_delete":
		if _, _, err := net.ParseCIDR(value); err != nil {
			return nil, errors.New("address must be a valid CIDR")
		}
		verb := "add"
		if action == "address_delete" {
			verb = "del"
		}
		return []string{"addr", verb, value, "dev", iface}, nil
	case "route_add", "route_delete":
		if value != "default" {
			if _, _, err := net.ParseCIDR(value); err != nil {
				return nil, errors.New("route destination must be default or a CIDR")
			}
		}
		args := []string{"route", "add", value}
		if action == "route_delete" {
			args[1] = "del"
		}
		if gateway != "" {
			if net.ParseIP(gateway) == nil {
				return nil, errors.New("gateway must be an IP address")
			}
			args = append(args, "via", gateway)
		}
		args = append(args, "dev", iface)
		if metric > 0 {
			args = append(args, "metric", strconv.Itoa(metric))
		}
		return args, nil
	default:
		return nil, errors.New("unsupported network action")
	}
}

func validInterface(name string) bool {
	if name == "" || len(name) > 15 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func (s Service) procRoot() string {
	if s.ProcRoot != "" {
		return s.ProcRoot
	}
	return "/proc"
}

func (s Service) sysClassNetRoot() string {
	if s.SysClassNetRoot != "" {
		return s.SysClassNetRoot
	}
	return "/sys/class/net"
}

func (s Service) ipPath() (string, error) {
	if s.IPPath != "" {
		info, err := os.Stat(s.IPPath)
		if err != nil {
			return "", err
		}
		if info.IsDir() || info.Mode()&0111 == 0 {
			return "", errors.New("configured ip utility is not executable")
		}
		return s.IPPath, nil
	}
	return findIP()
}

func (s Service) readRouteTable(name string, parser func(io.Reader) ([]Route, error)) ([]Route, bool, error) {
	f, err := os.Open(filepath.Join(s.procRoot(), "net", name))
	if err != nil {
		return []Route{}, false, err
	}
	defer f.Close()
	items, parseErr := parser(f)
	return items, true, parseErr
}

func (s Service) interfaceCounters(name string) InterfaceCounters {
	root := filepath.Join(s.sysClassNetRoot(), name, "statistics")
	counters := InterfaceCounters{}
	fields := []struct {
		name string
		dest *uint64
	}{
		{name: "rx_bytes", dest: &counters.RXBytes},
		{name: "rx_packets", dest: &counters.RXPackets},
		{name: "rx_errors", dest: &counters.RXErrors},
		{name: "rx_dropped", dest: &counters.RXDropped},
		{name: "tx_bytes", dest: &counters.TXBytes},
		{name: "tx_packets", dest: &counters.TXPackets},
		{name: "tx_errors", dest: &counters.TXErrors},
		{name: "tx_dropped", dest: &counters.TXDropped},
	}
	for _, field := range fields {
		value, err := strconv.ParseUint(read(filepath.Join(root, field.name)), 10, 64)
		if err != nil {
			continue
		}
		*field.dest = value
		counters.Available = true
	}
	if !counters.Available {
		if procCounters, ok := readProcNetDev(filepath.Join(s.procRoot(), "net", "dev"), name); ok {
			return procCounters
		}
	}
	return counters
}

func parseIPv4Routes(r io.Reader) ([]Route, error) {
	out := make([]Route, 0)
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) < 8 {
			continue
		}
		metric, err := strconv.Atoi(fields[6])
		if err != nil {
			continue
		}
		out = append(out, Route{
			Family:       "ipv4",
			Destination:  decodeIPv4(fields[1]),
			PrefixLength: ipv4PrefixLength(fields[7]),
			Gateway:      decodeIPv4(fields[2]),
			Interface:    fields[0],
			Metric:       metric,
		})
	}
	return out, scan.Err()
}

func readProcNetDev(path, name string) (InterfaceCounters, bool) {
	f, err := os.Open(path)
	if err != nil {
		return InterfaceCounters{}, false
	}
	defer f.Close()
	items, err := parseProcNetDev(f)
	if err != nil {
		return InterfaceCounters{}, false
	}
	item, ok := items[name]
	return item, ok
}

func parseProcNetDev(r io.Reader) (map[string]InterfaceCounters, error) {
	out := make(map[string]InterfaceCounters)
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		namePart, values, ok := strings.Cut(scan.Text(), ":")
		if !ok {
			continue
		}
		name := strings.TrimSpace(namePart)
		if name == "" {
			continue
		}
		fields := strings.Fields(values)
		// /proc/net/dev has eight receive and eight transmit counters.
		if len(fields) < 12 {
			continue
		}
		counters := InterfaceCounters{}
		positions := []struct {
			index int
			dest  *uint64
		}{
			{index: 0, dest: &counters.RXBytes},
			{index: 1, dest: &counters.RXPackets},
			{index: 2, dest: &counters.RXErrors},
			{index: 3, dest: &counters.RXDropped},
			{index: 8, dest: &counters.TXBytes},
			{index: 9, dest: &counters.TXPackets},
			{index: 10, dest: &counters.TXErrors},
			{index: 11, dest: &counters.TXDropped},
		}
		valid := true
		for _, position := range positions {
			value, parseErr := strconv.ParseUint(positionValue(fields, position.index), 10, 64)
			if parseErr != nil {
				valid = false
				break
			}
			*position.dest = value
		}
		if valid {
			counters.Available = true
			out[name] = counters
		}
	}
	return out, scan.Err()
}

func positionValue(fields []string, index int) string {
	if index < 0 || index >= len(fields) {
		return ""
	}
	return fields[index]
}

func parseIPv6Routes(r io.Reader) ([]Route, error) {
	out := make([]Route, 0)
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		// Linux /proc/net/ipv6_route has destination, destination prefix,
		// source, source prefix, next-hop, metric, refcnt, use, flags, dev.
		if len(fields) < 10 {
			continue
		}
		destination, ok := decodeIPv6(fields[0])
		if !ok {
			continue
		}
		source, ok := decodeIPv6(fields[2])
		if !ok {
			continue
		}
		gateway, ok := decodeIPv6(fields[4])
		if !ok {
			continue
		}
		destinationPrefix, err := parseKernelHex(fields[1], 128)
		if err != nil {
			continue
		}
		sourcePrefix, err := parseKernelHex(fields[3], 128)
		if err != nil {
			continue
		}
		metric, err := parseKernelHex(fields[5], int(^uint(0)>>1))
		if err != nil {
			continue
		}
		out = append(out, Route{
			Family:             "ipv6",
			Destination:        destination,
			PrefixLength:       destinationPrefix,
			Source:             source,
			SourcePrefixLength: sourcePrefix,
			Gateway:            gateway,
			Interface:          fields[9],
			Metric:             metric,
		})
	}
	return out, scan.Err()
}

func parseProcNeighbors(r io.Reader) ([]Neighbor, error) {
	out := make([]Neighbor, 0)
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		if item, ok := parseNeighborLine(scan.Text(), "proc"); ok {
			out = append(out, item)
		}
	}
	return out, scan.Err()
}

func parseIPNeighbors(output string) []Neighbor {
	out := make([]Neighbor, 0)
	for _, line := range strings.Split(output, "\n") {
		if item, ok := parseNeighborLine(line, "ip"); ok {
			out = append(out, item)
		}
	}
	return out
}

func parseNeighborLine(line, source string) (Neighbor, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return Neighbor{}, false
	}
	addressIndex := -1
	var address string
	for i, field := range fields {
		if parsed, ok := parseIPv6Token(field); ok {
			addressIndex = i
			address = parsed
			break
		}
	}
	if addressIndex < 0 {
		return Neighbor{}, false
	}
	item := Neighbor{Family: "ipv6", Address: address, Source: source}
	for i := addressIndex + 1; i < len(fields); i++ {
		field := fields[i]
		switch strings.ToLower(field) {
		case "dev":
			if i+1 < len(fields) {
				item.Interface = fields[i+1]
				i++
			}
		case "lladdr":
			if i+1 < len(fields) {
				item.MAC = fields[i+1]
				i++
			}
		case "router":
			item.Router = true
		case "proxy":
			item.Proxy = true
		default:
			if isNeighborState(field) {
				item.State = strings.ToUpper(field)
			}
		}
	}
	if item.Interface == "" && addressIndex+1 < len(fields) {
		// Some older procfs variants use: <address> <device> <lladdr> ...
		candidate := fields[addressIndex+1]
		if !isNeighborKeyword(candidate) && !isHardwareAddress(candidate) && !isNeighborState(candidate) {
			item.Interface = candidate
		}
	}
	return item, true
}

func parseIPv6Token(value string) (string, bool) {
	value = strings.TrimSpace(strings.Trim(value, "[]"))
	if zone := strings.IndexByte(value, '%'); zone >= 0 {
		value = value[:zone]
	}
	if ip := net.ParseIP(value); ip != nil && ip.To4() == nil {
		return ip.String(), true
	}
	if len(value) != 32 {
		return "", false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return "", false
	}
	return net.IP(decoded).String(), true
}

func isNeighborState(value string) bool {
	switch strings.ToUpper(value) {
	case "INCOMPLETE", "REACHABLE", "STALE", "DELAY", "PROBE", "FAILED", "NOARP", "PERMANENT", "NONE", "DORMANT", "VALID":
		return true
	default:
		return false
	}
}

func isNeighborKeyword(value string) bool {
	switch strings.ToLower(value) {
	case "dev", "lladdr", "router", "proxy":
		return true
	default:
		return isNeighborState(value)
	}
}

func isHardwareAddress(value string) bool {
	_, err := net.ParseMAC(value)
	return err == nil
}

func parseKernelHex(value string, max int) (int, error) {
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil || parsed > uint64(max) {
		return 0, errors.New("invalid kernel hexadecimal value")
	}
	return int(parsed), nil
}

func ipv4PrefixLength(mask string) int {
	parsed, err := strconv.ParseUint(mask, 16, 32)
	if err != nil {
		return 0
	}
	return bitsOnesCount32(uint32(parsed))
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
	bytes := make([]byte, 4)
	for i := range bytes {
		v, err := strconv.ParseUint(hexValue[(3-i)*2:(4-i)*2], 16, 8)
		if err != nil {
			return hexValue
		}
		bytes[i] = byte(v)
	}
	return net.IP(bytes).String()
}

func decodeIPv6(hexValue string) (string, bool) {
	if len(hexValue) != 32 {
		return "", false
	}
	bytes, err := hex.DecodeString(hexValue)
	if err != nil {
		return "", false
	}
	return net.IP(bytes).String(), true
}

func bitsOnesCount32(value uint32) int {
	count := 0
	for value != 0 {
		value &= value - 1
		count++
	}
	return count
}

func findIP() (string, error) {
	for _, p := range []string{"/sbin/ip", "/usr/sbin/ip", "/bin/ip", "/usr/bin/ip"} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("ip utility unavailable; use QNAP runtime probe")
}
