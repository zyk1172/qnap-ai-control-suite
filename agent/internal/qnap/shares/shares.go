package shares

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Service struct{ Exec qexec.Executor }
type Share struct {
	Name        string            `json:"name"`
	Path        string            `json:"path"`
	Comment     string            `json:"comment"`
	Browsable   bool              `json:"browsable"`
	Writable    bool              `json:"writable"`
	SystemShare bool              `json:"system_share,omitempty"`
	Note        string            `json:"note,omitempty"`
	Raw         map[string]string `json:"raw"`
}
type NFSExport struct {
	Path  string   `json:"path"`
	Hosts []string `json:"hosts"`
	Raw   string   `json:"raw"`
}
type ACLResult struct {
	Path     string        `json:"path"`
	ACL      string        `json:"acl,omitempty"`
	Entries  []string      `json:"entries,omitempty"`
	Mode     string        `json:"mode,omitempty"`
	Owner    string        `json:"owner,omitempty"`
	Group    string        `json:"group,omitempty"`
	Fallback bool          `json:"fallback,omitempty"`
	Note     string        `json:"note,omitempty"`
	Command  *qexec.Result `json:"command,omitempty"`
}

func (s Service) List() ([]Share, error) {
	path := "/etc/config/smb.conf"
	if _, err := os.Stat(path); err != nil {
		path = "/etc/samba/smb.conf"
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseShares(f)
}
func parseShares(r io.Reader) ([]Share, error) {
	out := []Share{}
	var current *Share
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if len(line) > 2 && line[0] == '[' && line[len(line)-1] == ']' {
			name := line[1 : len(line)-1]
			if strings.EqualFold(name, "global") {
				current = nil
				continue
			}
			out = append(out, Share{Name: name, Raw: map[string]string{}})
			current = &out[len(out)-1]
			continue
		}
		if current == nil || !strings.Contains(line, "=") {
			continue
		}
		p := strings.SplitN(line, "=", 2)
		key, value := strings.ToLower(strings.TrimSpace(p[0])), strings.TrimSpace(p[1])
		current.Raw[key] = value
		switch key {
		case "path":
			current.Path = value
		case "comment":
			current.Comment = value
		case "browseable":
			current.Browsable = value != "no"
		case "read only":
			current.Writable = value == "no"
		}
	}
	for i := range out {
		if out[i].Path == "" {
			out[i].SystemShare = true
			if out[i].Note == "" {
				out[i].Note = "QNAP system share without an SMB path field"
			}
		}
	}
	return out, scan.Err()
}
func (s Service) NFS() ([]NFSExport, error) {
	path := "/etc/config/exports"
	if _, err := os.Stat(path); err != nil {
		path = "/etc/exports"
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseNFS(f), nil
}
func ParseNFS(r interface{ Read([]byte) (int, error) }) []NFSExport {
	scan := bufio.NewScanner(r)
	out := []NFSExport{}
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "/") {
			continue
		}
		out = append(out, NFSExport{Path: fields[0], Hosts: fields[1:], Raw: line})
	}
	return out
}
func (s Service) ACL(ctx context.Context, path string) (ACLResult, error) {
	if strings.TrimSpace(path) == "" {
		return ACLResult{}, errors.New("path is required")
	}
	cleaned, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return ACLResult{}, err
	}
	tool := findExecutable([]string{"/usr/bin/getfacl", "/bin/getfacl", "/usr/sbin/getfacl", "/sbin/getfacl"})
	if tool == "" {
		return aclFallback(cleaned, "getfacl utility not found; returned stat fallback")
	}
	result, err := s.Exec.Run(ctx, qexec.Request{Argv: []string{tool, "-p", cleaned}, Timeout: 30 * time.Second, MaxOutput: s.Exec.MaxOutput})
	if err != nil {
		var commandErr *qexec.CommandError
		if errors.As(err, &commandErr) && (commandErr.Kind == qexec.NotFound || commandErr.Kind == qexec.StartFailed) {
			return aclFallback(cleaned, "getfacl utility not found; returned stat fallback")
		}
		return ACLResult{Path: cleaned, Command: &result}, err
	}
	return ACLResult{Path: cleaned, ACL: result.Stdout, Entries: parseACL(result.Stdout), Command: &result}, nil
}
func (s Service) SetACL(ctx context.Context, path, entry string) (qexec.Result, error) {
	if path == "" || entry == "" {
		return qexec.Result{}, errors.New("path and entry are required")
	}
	tool := findExecutable([]string{"/usr/bin/setfacl", "/bin/setfacl", "/usr/sbin/setfacl", "/sbin/setfacl"})
	if tool == "" {
		return qexec.Result{}, errors.New("setfacl utility not found; install ACL tools or use QNAP shared-folder permissions")
	}
	return s.Exec.Run(ctx, qexec.Request{Argv: []string{tool, "-m", entry, path}, Timeout: 30 * time.Second, MaxOutput: s.Exec.MaxOutput})
}
func (s Service) Directory(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	return filepath.Abs(filepath.Clean(path))
}
func findExecutable(paths []string) string {
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return ""
}
func parseACL(stdout string) []string {
	entries := []string{}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	return entries
}
func aclFallback(path, note string) (ACLResult, error) {
	if _, err := os.Stat(path); err != nil {
		return ACLResult{Path: path}, err
	}
	mode, owner, group := statIdentity(path)
	return ACLResult{Path: path, Mode: mode, Owner: owner, Group: group, Fallback: true, Note: note}, nil
}
func statIdentity(path string) (mode, owner, group string) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", ""
	}
	mode = info.Mode().String()
	if sys, ok := info.Sys().(*syscall.Stat_t); ok {
		owner = idName("/etc/passwd", strconv.FormatUint(uint64(sys.Uid), 10), 0, 2)
		group = idName("/etc/group", strconv.FormatUint(uint64(sys.Gid), 10), 0, 2)
	}
	return mode, owner, group
}
func idName(path, id string, nameIndex, idIndex int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		fields := strings.Split(scan.Text(), ":")
		if len(fields) > idIndex && fields[idIndex] == id && len(fields) > nameIndex {
			return fields[nameIndex]
		}
	}
	return ""
}
