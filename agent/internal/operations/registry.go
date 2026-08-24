// Package operations contains the control-plane policy that sits in front of
// every authenticated HTTP operation.  It deliberately has no dependency on
// the QNAP service packages so policy stays stable while those adapters grow.
package operations

import (
	"path"
	"regexp"
	"strings"
)

type Risk string

const (
	Read      Risk = "read"
	Write     Risk = "write"
	Sensitive Risk = "sensitive"
)

// Request is the small, already decoded subset of an HTTP request needed for
// policy. Payload must never be copied into an audit record unchanged.
type Request struct {
	Method  string
	Path    string
	Payload map[string]any
}

// Operation is the normalized policy result shared by approval, audit, jobs,
// and future MCP capability reporting.
type Operation struct {
	Name     string `json:"name"`
	Risk     Risk   `json:"risk"`
	Target   string `json:"target,omitempty"`
	Resource string `json:"resource,omitempty"`
	Summary  string `json:"summary"`
	DryRun   bool   `json:"dry_run"`
}

type Definition struct {
	Name string `json:"name"`
	Risk Risk   `json:"risk"`
}

type Registry struct{}

func New() Registry { return Registry{} }

// Catalog contains the stable public concepts. Dynamic actions can raise the
// risk above this baseline, but never lower it below a route's read/write role.
func (Registry) Catalog() []Definition {
	return []Definition{
		{Name: "system.power", Risk: Sensitive},
		{Name: "command.exec", Risk: Write},
		{Name: "files.manage", Risk: Write},
		{Name: "docker.command", Risk: Write},
		{Name: "qpkg.manage", Risk: Write},
		{Name: "jobs.start", Risk: Write},
		{Name: "network.manage", Risk: Write},
		{Name: "qnap.adapter", Risk: Write},
	}
}

func (Registry) Resolve(request Request) Operation {
	method := strings.ToUpper(request.Method)
	pathValue := request.Path
	payload := request.Payload
	name := operationName(pathValue)
	op := Operation{
		Name:     name,
		Risk:     Write,
		Target:   target(pathValue, payload),
		Resource: resource(pathValue, payload),
		DryRun:   boolValue(payload, "dry_run"),
	}
	if method == "GET" || method == "HEAD" || method == "OPTIONS" {
		op.Risk = Read
		op.Summary = summary(op)
		return op
	}

	switch {
	case pathValue == "/v1/system/reboot" || pathValue == "/v1/system/shutdown" || pathValue == "/v1/system/power":
		op.Name, op.Risk = "system.power", Sensitive
	case pathValue == "/v1/exec" || pathValue == "/v1/command/run" || pathValue == "/v1/shell":
		op.Name = "command.exec"
		if dangerousCommand(commandFromPayload(pathValue, payload)) {
			op.Risk = Sensitive
		}
	case pathValue == "/v1/jobs":
		op.Name = "jobs.start"
		if dangerousCommand(commandFromPayload(pathValue, payload)) {
			op.Risk = Sensitive
		}
	case pathValue == "/v1/files/manage":
		op.Name = "files.manage"
		if stringValue(payload, "action") == "delete" && (boolValue(payload, "recursive") || criticalPath(stringValue(payload, "path"))) {
			op.Risk = Sensitive
		}
	case pathValue == "/v1/network/manage":
		op.Name = "network.manage"
		if sensitiveNetworkChange(payload) {
			op.Risk = Sensitive
		}
	case pathValue == "/v1/docker/command":
		op.Name = "docker.command"
		if sensitiveDockerChange(payload) {
			op.Risk = Sensitive
		}
	case pathValue == "/v1/qnap/qpkg/manage" || pathValue == "/v1/qnap/qpkg/action":
		op.Name = "qpkg.manage"
		if stringValue(payload, "action") == "remove" {
			op.Risk = Sensitive
		}
	case pathValue == "/v1/users/manage":
		op.Name = "users.manage"
		if isAdministrator(stringValue(payload, "name")) && (stringValue(payload, "action") == "delete" || stringValue(payload, "action") == "password") {
			op.Risk = Sensitive
		}
	case pathValue == "/v1/storage/snapshots/action":
		op.Name = "storage.snapshot"
		if actionMatches(stringValue(payload, "action"), "restore", "rollback", "revert") {
			op.Risk = Sensitive
		}
	case pathValue == "/v1/qnap/firmware/action":
		op.Name = "qnap.firmware"
		if actionMatches(stringValue(payload, "action"), "install", "apply", "update", "upgrade") {
			op.Risk = Sensitive
		}
	case pathValue == "/v1/qnap/certificates/action":
		op.Name = "qnap.certificates"
		if actionMatches(stringValue(payload, "action"), "replace", "import", "remove", "delete") {
			op.Risk = Sensitive
		}
	case pathValue == "/v1/qnap/virtual-switch/action":
		op.Name = "qnap.virtual_switch"
		if !readAction(stringValue(payload, "action")) {
			op.Risk = Sensitive
		}
	case pathValue == "/v1/qnap/storage/action":
		op.Name = "qnap.storage"
		if actionMatches(stringValue(payload, "action"), "delete", "remove", "destroy", "format", "wipe", "restore", "rollback", "revert") {
			op.Risk = Sensitive
		}
	case strings.HasPrefix(pathValue, "/v1/approvals/"):
		op.Name = "approval.decision"
	}
	op.Summary = summary(op)
	return op
}

func operationName(pathValue string) string {
	trimmed := strings.Trim(strings.TrimPrefix(pathValue, "/v1/"), "/")
	if trimmed == "" {
		return "api.request"
	}
	return strings.ReplaceAll(strings.ReplaceAll(trimmed, "/", "."), "-", "_")
}

func target(pathValue string, payload map[string]any) string {
	for _, key := range []string{"path", "name", "id", "interface", "action"} {
		if value := stringValue(payload, key); value != "" {
			return truncate(value, 160)
		}
	}
	return strings.TrimPrefix(pathValue, "/v1/")
}

func resource(pathValue string, payload map[string]any) string {
	switch {
	case strings.HasPrefix(pathValue, "/v1/files/"):
		return "file:" + truncate(stringValue(payload, "path"), 160)
	case strings.HasPrefix(pathValue, "/v1/docker/"):
		return "docker"
	case strings.Contains(pathValue, "/qpkg"):
		return "qpkg:" + truncate(stringValue(payload, "name"), 80)
	case strings.Contains(pathValue, "/storage/"):
		return "storage"
	case strings.Contains(pathValue, "/network/"):
		return "network:" + truncate(stringValue(payload, "interface"), 80)
	case strings.Contains(pathValue, "/users") || strings.Contains(pathValue, "/groups"):
		return "identity:" + truncate(stringValue(payload, "name"), 80)
	default:
		return strings.TrimPrefix(pathValue, "/v1/")
	}
}

func summary(op Operation) string {
	message := string(op.Risk) + " " + op.Name
	if op.Target != "" {
		message += " target=" + op.Target
	}
	if op.DryRun {
		message += " (dry run)"
	}
	return message
}

func commandFromPayload(pathValue string, payload map[string]any) []string {
	if pathValue == "/v1/shell" {
		if script := stringValue(payload, "script"); script != "" {
			return []string{"shell", script}
		}
		return []string{"shell", stringValue(payload, "shell")}
	}
	if pathValue == "/v1/jobs" {
		if command, ok := payload["command"].(map[string]any); ok {
			if argv := stringsValue(command, "argv"); len(argv) > 0 {
				return argv
			}
		}
		if script := stringValue(payload, "script"); script != "" {
			return []string{"shell", script}
		}
		return []string{"shell", stringValue(payload, "shell")}
	}
	return stringsValue(payload, "argv")
}

func dangerousCommand(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	if argv[0] == "shell" {
		return dangerousShell(strings.Join(argv[1:], " "))
	}
	binary := strings.ToLower(path.Base(argv[0]))
	switch {
	case binary == "wipefs", strings.HasPrefix(binary, "mkfs"):
		return true
	case binary == "zfs" && hasArgument(argv[1:], "destroy"):
		return true
	case binary == "mdadm" && hasArgument(argv[1:], "--zero-superblock"):
		return true
	case binary == "dd":
		for _, arg := range argv[1:] {
			if strings.HasPrefix(arg, "of=/dev/") {
				return true
			}
		}
	}
	return false
}

var dangerousShellPattern = regexp.MustCompile(`(?i)(^|[;&|]\s*)(mkfs(?:\.[[:alnum:]_+-]+)?|wipefs|zfs\s+destroy|mdadm\s+[^\n]*--zero-superblock|dd\s+[^\n]*\bof=/dev/)`)

func dangerousShell(script string) bool { return dangerousShellPattern.MatchString(script) }

func sensitiveNetworkChange(payload map[string]any) bool {
	action, value := stringValue(payload, "action"), strings.ToLower(stringValue(payload, "value"))
	return (action == "set_state" && value == "down") || ((action == "route_add" || action == "route_delete") && value == "default")
}

func sensitiveDockerChange(payload map[string]any) bool {
	subcommand := stringValue(payload, "subcommand")
	args := stringsValue(payload, "args")
	if subcommand == "system" && hasArgument(args, "prune") {
		return true
	}
	return subcommand == "volume" && (hasArgument(args, "prune") || hasArgument(args, "rm") || hasArgument(args, "remove"))
}

func criticalPath(value string) bool {
	cleaned := path.Clean(value)
	return cleaned == "/" || cleaned == "/share" || cleaned == "/etc/config" || strings.HasPrefix(cleaned, "/etc/config/")
}

func readAction(action string) bool {
	return actionMatches(action, "list", "get", "status", "info", "inspect", "inventory", "show")
}

func actionMatches(action string, keywords ...string) bool {
	action = strings.ToLower(action)
	for _, keyword := range keywords {
		if strings.Contains(action, keyword) {
			return true
		}
	}
	return false
}

func isAdministrator(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "admin" || name == "administrator"
}

func hasArgument(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}

func stringValue(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}

func stringsValue(values map[string]any, key string) []string {
	value, ok := values[key].([]any)
	if !ok {
		return nil
	}
	output := make([]string, 0, len(value))
	for _, item := range value {
		if text, ok := item.(string); ok {
			output = append(output, text)
		}
	}
	return output
}

func boolValue(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
