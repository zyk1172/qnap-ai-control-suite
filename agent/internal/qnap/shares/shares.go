package shares

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"strings"
	"time"
)

type Service struct{ Exec qexec.Executor }
type Share struct {
	Name      string            `json:"name"`
	Path      string            `json:"path"`
	Comment   string            `json:"comment"`
	Browsable bool              `json:"browsable"`
	Writable  bool              `json:"writable"`
	Raw       map[string]string `json:"raw"`
}
type NFSExport struct {
	Path  string   `json:"path"`
	Hosts []string `json:"hosts"`
	Raw   string   `json:"raw"`
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
	out := []Share{}
	var current *Share
	scan := bufio.NewScanner(f)
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
func (s Service) ACL(ctx context.Context, path string) (qexec.Result, error) {
	return s.Exec.Run(ctx, qexec.Request{Argv: []string{"/bin/getfacl", "-p", path}, Timeout: 30 * time.Second, MaxOutput: s.Exec.MaxOutput})
}
func (s Service) SetACL(ctx context.Context, path, entry string) (qexec.Result, error) {
	if path == "" || entry == "" {
		return qexec.Result{}, errors.New("path and entry are required")
	}
	return s.Exec.Run(ctx, qexec.Request{Argv: []string{"/bin/setfacl", "-m", entry, path}, Timeout: 30 * time.Second, MaxOutput: s.Exec.MaxOutput})
}
func (s Service) Directory(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	return filepath.Abs(filepath.Clean(path))
}
