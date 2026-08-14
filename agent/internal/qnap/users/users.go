package users

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"strconv"
	"strings"
	"time"
)

type Service struct{ Exec qexec.Executor }
type User struct {
	Name, Home, Shell string
	UID, GID          int
	Groups            []string
}
type Group struct {
	Name    string
	GID     int
	Members []string
}

func (s Service) List() ([]User, error) {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	groups, _ := s.Groups()
	byGID := map[int][]string{}
	for _, g := range groups {
		byGID[g.GID] = append(byGID[g.GID], g.Name)
	}
	out := []User{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		p := strings.Split(scan.Text(), ":")
		if len(p) < 7 {
			continue
		}
		uid, _ := strconv.Atoi(p[2])
		gid, _ := strconv.Atoi(p[3])
		out = append(out, User{Name: p[0], UID: uid, GID: gid, Home: p[5], Shell: p[6], Groups: byGID[gid]})
	}
	return out, scan.Err()
}
func (s Service) Groups() ([]Group, error) {
	f, err := os.Open("/etc/group")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []Group{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		p := strings.Split(scan.Text(), ":")
		if len(p) < 4 {
			continue
		}
		gid, _ := strconv.Atoi(p[2])
		members := []string{}
		if p[3] != "" {
			members = strings.Split(p[3], ",")
		}
		out = append(out, Group{Name: p[0], GID: gid, Members: members})
	}
	return out, scan.Err()
}
func (s Service) ManageUser(ctx context.Context, action, name string, args []string) (qexec.Result, error) {
	if !safeName(name) {
		return qexec.Result{}, errors.New("invalid user name")
	}
	bin, err := lookup(map[string]string{"create": "useradd", "update": "usermod", "delete": "userdel", "password": "passwd", "enable": "usermod", "disable": "usermod"}[action])
	if err != nil {
		return qexec.Result{}, err
	}
	argv := []string{bin}
	switch action {
	case "create":
		argv = append(argv, args...)
	case "update":
		argv = append(argv, args...)
	case "delete":
		argv = append(argv, args...)
	case "password":
		argv = append(argv, args...)
	case "enable":
		argv = append(argv, "-U")
	case "disable":
		argv = append(argv, "-L")
	default:
		return qexec.Result{}, errors.New("unsupported user action")
	}
	argv = append(argv, name)
	return s.Exec.Run(ctx, qexec.Request{Argv: argv, Timeout: 60 * time.Second, MaxOutput: s.Exec.MaxOutput})
}
func (s Service) ManageGroup(ctx context.Context, action, name string, args []string) (qexec.Result, error) {
	if !safeName(name) {
		return qexec.Result{}, errors.New("invalid group name")
	}
	bin, err := lookup(map[string]string{"create": "groupadd", "update": "groupmod", "delete": "groupdel"}[action])
	if err != nil {
		return qexec.Result{}, err
	}
	return s.Exec.Run(ctx, qexec.Request{Argv: append(append([]string{bin}, args...), name), Timeout: 60 * time.Second, MaxOutput: s.Exec.MaxOutput})
}
func lookup(name string) (string, error) {
	if name == "" {
		return "", errors.New("unsupported action")
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", errors.New("standard account utility unavailable; use nas_exec after QNAP probe")
	}
	return path, nil
}
func safeName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}
