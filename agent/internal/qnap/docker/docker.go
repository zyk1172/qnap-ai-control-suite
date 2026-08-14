package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	qexec "qnap-ai-control-suite/agent/internal/exec"
)

type Service struct {
	Exec          qexec.Executor
	Paths         []string
	RedactSecrets bool
}

func (s Service) Run(ctx context.Context, args []string, timeout int) (qexec.Result, error) {
	bin, err := s.Binary()
	if err != nil {
		return qexec.Result{}, err
	}
	return s.Exec.Run(ctx, qexec.Request{Argv: append([]string{bin}, args...), Timeout: seconds(timeout), MaxOutput: s.Exec.MaxOutput})
}
func (s Service) Binary() (string, error) {
	for _, candidate := range s.Paths {
		info, err := os.Stat(filepath.Clean(candidate))
		if err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return filepath.Clean(candidate), nil
		}
	}
	return "", errors.New("docker CLI was not found; install or start Container Station, then configure docker_paths")
}
func (s Service) Info(ctx context.Context) (qexec.Result, error) {
	return s.Run(ctx, []string{"info", "--format", "{{json .}}"}, 30)
}
func (s Service) Containers(ctx context.Context) (qexec.Result, error) {
	return s.Run(ctx, []string{"ps", "-a", "--format", "{{json .}}"}, 30)
}
func (s Service) Images(ctx context.Context) (qexec.Result, error) {
	return s.Run(ctx, []string{"images", "--format", "{{json .}}"}, 30)
}
func Allowed(sub string) bool {
	switch sub {
	case "run", "create", "start", "stop", "restart", "pause", "unpause", "kill", "rm", "rmi", "exec", "pull", "push", "build", "images", "ps", "inspect", "logs", "stats", "top", "port", "diff", "rename", "update", "tag", "save", "load", "cp", "commit", "export", "import", "history", "network", "volume", "system", "compose", "container", "image", "builder", "context", "events", "manifest", "plugin", "search", "trust", "version", "wait":
		return true
	}
	return false
}
func Destructive(sub string, args []string) bool {
	if sub == "rm" || sub == "rmi" || sub == "kill" {
		return true
	}
	if sub == "system" || sub == "builder" {
		return has(args, "prune")
	}
	if sub == "volume" || sub == "network" {
		return has(args, "rm") || has(args, "prune")
	}
	return sub == "compose" && (has(args, "down") || has(args, "rm"))
}
func has(args []string, v string) bool {
	for _, arg := range args {
		if arg == v {
			return true
		}
	}
	return false
}
func seconds(n int) time.Duration {
	if n <= 0 {
		n = 120
	}
	return time.Duration(n) * time.Second
}
