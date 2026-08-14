package users

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	qexec "qnap-ai-control-suite/agent/internal/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Service struct{ Exec qexec.Executor }
type User struct {
	Name   string   `json:"name"`
	Home   string   `json:"home,omitempty"`
	Shell  string   `json:"shell,omitempty"`
	UID    int      `json:"uid"`
	GID    int      `json:"gid"`
	Groups []string `json:"groups"`
}
type Group struct {
	Name    string   `json:"name"`
	GID     int      `json:"gid"`
	Members []string `json:"members"`
}

func (s Service) List() ([]User, error) {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	groups, err := s.Groups()
	if err != nil {
		return nil, err
	}
	return parseUsers(f, groups)
}
func (s Service) Groups() ([]Group, error) {
	f, err := os.Open("/etc/group")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseGroups(f)
}

func parseUsers(r io.Reader, groups []Group) ([]User, error) {
	byGID := map[int][]string{}
	byMember := map[string][]string{}
	for _, group := range groups {
		byGID[group.GID] = append(byGID[group.GID], group.Name)
		for _, member := range group.Members {
			byMember[member] = append(byMember[member], group.Name)
		}
	}
	out := []User{}
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		p := strings.Split(scan.Text(), ":")
		if len(p) < 7 {
			continue
		}
		uid, _ := strconv.Atoi(p[2])
		gid, _ := strconv.Atoi(p[3])
		memberships := append(append([]string{}, byGID[gid]...), byMember[p[0]]...)
		sort.Strings(memberships)
		memberships = compact(memberships)
		out = append(out, User{Name: p[0], UID: uid, GID: gid, Home: p[5], Shell: p[6], Groups: memberships})
	}
	return out, scan.Err()
}

func parseGroups(r io.Reader) ([]Group, error) {
	out := []Group{}
	scan := bufio.NewScanner(r)
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
	command := map[string]string{"create": "groupadd", "update": "groupmod", "delete": "groupdel", "member_add": "gpasswd", "member_remove": "gpasswd"}[action]
	bin, err := lookup(command)
	if err != nil {
		return qexec.Result{}, err
	}
	argv := []string{bin}
	switch action {
	case "create", "update", "delete":
		argv = append(argv, args...)
	case "member_add", "member_remove":
		if len(args) != 1 || !safeName(args[0]) {
			return qexec.Result{}, errors.New("group membership actions require exactly one valid user name in args")
		}
		flag := "-a"
		if action == "member_remove" {
			flag = "-d"
		}
		argv = append(argv, flag, args[0])
	default:
		return qexec.Result{}, errors.New("unsupported group action")
	}
	argv = append(argv, name)
	return s.Exec.Run(ctx, qexec.Request{Argv: argv, Timeout: 60 * time.Second, MaxOutput: s.Exec.MaxOutput})
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

func compact(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
