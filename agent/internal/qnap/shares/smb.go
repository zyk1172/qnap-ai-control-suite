package shares

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"strconv"
	"strings"
	"time"
)

// SMBSession is one authenticated Samba client session reported by
// smbstatus. Fields that are not present in a particular Samba version stay
// empty; the raw human-oriented output is intentionally not returned.
type SMBSession struct {
	PID        int    `json:"pid"`
	Username   string `json:"username,omitempty"`
	Group      string `json:"group,omitempty"`
	Machine    string `json:"machine,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	Encryption string `json:"encryption,omitempty"`
	Signing    string `json:"signing,omitempty"`
}

// SMBLock is one open Samba file lock. Raw is retained because smbstatus
// column layouts differ across Samba releases and QNAP firmware.
type SMBLock struct {
	PID       int    `json:"pid"`
	UID       string `json:"uid,omitempty"`
	DenyMode  string `json:"deny_mode,omitempty"`
	Access    string `json:"access,omitempty"`
	ReadWrite string `json:"read_write,omitempty"`
	Oplock    string `json:"oplock,omitempty"`
	SharePath string `json:"share_path,omitempty"`
	Name      string `json:"name,omitempty"`
	Time      string `json:"time,omitempty"`
	Raw       string `json:"raw,omitempty"`
}

// SMBStatusReport describes the read-only SMB session and lock capability.
// Supported means that smbstatus was found and executed successfully; it
// does not imply that any client session or lock currently exists.
type SMBStatusReport struct {
	Supported bool          `json:"supported"`
	Source    string        `json:"source,omitempty"`
	Sessions  []SMBSession  `json:"sessions"`
	Locks     []SMBLock     `json:"locks"`
	Reason    string        `json:"reason,omitempty"`
	Command   *qexec.Result `json:"command,omitempty"`
}

// SMBStatus executes only the smbstatus inventory command. No Samba control
// or write subcommand is used. A missing utility is an expected capability
// gap and is returned as a structured unavailable report without an error.
func (s Service) SMBStatus(ctx context.Context) (SMBStatusReport, error) {
	path := s.smbStatusPath()
	if path == "" {
		return SMBStatusReport{
			Supported: false,
			Sessions:  []SMBSession{},
			Locks:     []SMBLock{},
			Reason:    "capability unavailable: smbstatus utility not found",
		}, nil
	}

	result, err := s.Exec.Run(ctx, qexec.Request{
		Argv:      []string{path},
		Timeout:   15 * time.Second,
		MaxOutput: smbStatusMaxOutput(s.Exec.MaxOutput),
	})
	if err != nil {
		return SMBStatusReport{
			Supported: false,
			Source:    path,
			Sessions:  []SMBSession{},
			Locks:     []SMBLock{},
			Reason:    fmt.Sprintf("capability unavailable: smbstatus probe failed: %v", err),
			Command:   &result,
		}, err
	}

	report := ParseSMBStatus(result.Stdout)
	report.Source = path
	report.Command = &result
	return report, nil
}

// ParseSMBStatus parses the stable table portions of the default smbstatus
// output. It intentionally accepts partial rows because Samba and QNAP ship
// several different column layouts. Unknown lines are ignored while the
// complete original row is retained for parsed locks.
func ParseSMBStatus(stdout string) SMBStatusReport {
	report := SMBStatusReport{
		Supported: true,
		Sessions:  []SMBSession{},
		Locks:     []SMBLock{},
	}

	const (
		sectionNone = iota
		sectionSessions
		sectionLocks
	)
	section := sectionNone
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)

		switch {
		case strings.Contains(lower, "locked files"):
			section = sectionLocks
			continue
		case isSMBSessionHeader(lower):
			section = sectionSessions
			continue
		case isSMBSectionBoundary(line):
			continue
		case strings.HasPrefix(lower, "no locked files"):
			section = sectionLocks
			continue
		}

		switch section {
		case sectionSessions:
			if item, ok := parseSMBSessionLine(line); ok {
				report.Sessions = append(report.Sessions, item)
			}
		case sectionLocks:
			if item, ok := parseSMBLockLine(line); ok {
				report.Locks = append(report.Locks, item)
			}
		}
	}
	return report
}

func (s Service) smbStatusPath() string {
	if path := strings.TrimSpace(s.SMBStatusPath); path != "" {
		if executableFile(path) {
			return path
		}
		return ""
	}
	return findCommand("smbstatus")
}

func findCommand(name string) string {
	for _, directory := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if directory == "" {
			continue
		}
		path := filepath.Join(directory, name)
		if executableFile(path) {
			return path
		}
	}
	return findExecutable([]string{
		"/usr/bin/" + name,
		"/usr/sbin/" + name,
		"/bin/" + name,
		"/sbin/" + name,
	})
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}

func smbStatusMaxOutput(configured int) int {
	const defaultMax = 2 * 1024 * 1024
	if configured <= 0 || configured > defaultMax {
		return defaultMax
	}
	return configured
}

func isSMBSessionHeader(line string) bool {
	return strings.HasPrefix(line, "pid ") && strings.Contains(line, "username") && strings.Contains(line, "machine")
}

func isSMBSectionBoundary(line string) bool {
	if strings.Trim(line, "-=") == "" {
		return true
	}
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "samba version") ||
		strings.HasPrefix(lower, "service ") ||
		strings.HasPrefix(lower, "share ")
}

func parseSMBSessionLine(line string) (SMBSession, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return SMBSession{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return SMBSession{}, false
	}

	item := SMBSession{PID: pid, Username: fields[1], Group: fields[2]}
	protocolIndex := -1
	for index := 3; index < len(fields); index++ {
		if isSMBProtocol(fields[index]) {
			protocolIndex = index
			break
		}
	}
	if protocolIndex == -1 {
		item.Machine = fields[3]
		return item, true
	}
	item.Machine = strings.Join(fields[3:protocolIndex], " ")
	item.Protocol = fields[protocolIndex]
	if protocolIndex+1 < len(fields) {
		item.Encryption = fields[protocolIndex+1]
	}
	if protocolIndex+2 < len(fields) {
		item.Signing = fields[protocolIndex+2]
	}
	return item, true
}

func isSMBProtocol(value string) bool {
	upper := strings.ToUpper(strings.TrimSpace(value))
	return strings.HasPrefix(upper, "SMB") || upper == "NT1" || upper == "CORE" || upper == "LANMAN"
}

func parseSMBLockLine(line string) (SMBLock, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return SMBLock{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return SMBLock{}, false
	}

	item := SMBLock{PID: pid, UID: fields[1], Raw: line}
	if len(fields) > 2 {
		item.DenyMode = fields[2]
	}
	if len(fields) > 3 {
		item.Access = fields[3]
	}
	if len(fields) > 4 {
		item.ReadWrite = fields[4]
	}
	if len(fields) > 5 {
		item.Oplock = fields[5]
	}
	if len(fields) <= 6 {
		return item, true
	}

	remaining := fields[6:]
	pathIndex := -1
	for index, value := range remaining {
		if strings.HasPrefix(value, "/") {
			pathIndex = index
			break
		}
	}
	if pathIndex >= 0 {
		item.SharePath = remaining[pathIndex]
		if pathIndex+1 < len(remaining) {
			item.Name = strings.Join(remaining[pathIndex+1:], " ")
		}
	}
	return item, true
}
