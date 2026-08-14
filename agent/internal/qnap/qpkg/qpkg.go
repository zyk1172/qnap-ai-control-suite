package qpkg

import (
	"bufio"
	"context"
	"errors"
	"os"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"strings"
)

type Service struct {
	Exec qexec.Executor
	Path string
}

func (s Service) List(ctx context.Context) ([]map[string]string, error) {
	path := s.Path
	if path == "" {
		path = "/etc/config/qpkg.conf"
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f), nil
}
func Parse(r interface{ Read([]byte) (int, error) }) []map[string]string {
	scanner := bufio.NewScanner(r)
	out := []map[string]string{}
	var current map[string]string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) > 2 && line[0] == '[' && line[len(line)-1] == ']' {
			current = map[string]string{"name": line[1 : len(line)-1]}
			out = append(out, current)
			continue
		}
		if current == nil || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		current[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out
}
func (s Service) Manage(ctx context.Context, name, action, path, url string) (qexec.Result, error) {
	if name == "" && action != "update_all" {
		return qexec.Result{}, errors.New("name is required")
	}
	args := []string{action}
	if path != "" {
		args = append(args, path)
	} else if url != "" {
		args = append(args, url)
	} else if name != "" {
		args = append(args, name)
	}
	return s.Exec.Run(ctx, qexec.Request{Argv: append([]string{"/sbin/qpkg_cli"}, args...), Timeout: s.Exec.DefaultTimeout, MaxOutput: s.Exec.MaxOutput})
}
func Destructive(action string) bool {
	switch action {
	case "add", "install_file", "install_url", "remove", "update_all":
		return true
	}
	return false
}
