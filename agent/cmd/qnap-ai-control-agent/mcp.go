package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const mcpProtocolVersion = "2025-11-25"

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpResponseRecorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *mcpResponseRecorder) Header() http.Header { return r.header }

func (r *mcpResponseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}

func (r *mcpResponseRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

// handleMCP is a stateless Streamable HTTP MCP endpoint. MoviePilot opens a
// fresh session per operation, so no server-side MCP session is required.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeMCPError(w, nil, -32600, "POST required")
		return
	}
	defer r.Body.Close()

	var request mcpRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 8*1024*1024))
	if err := decoder.Decode(&request); err != nil {
		writeMCPError(w, nil, -32700, "parse error")
		return
	}
	if request.JSONRPC != "2.0" || strings.TrimSpace(request.Method) == "" {
		writeMCPError(w, request.ID, -32600, "invalid request")
		return
	}

	result, err := s.handleMCPMethod(request.Method, request.Params)
	if len(request.ID) == 0 || string(request.ID) == "null" {
		// MCP notifications do not receive a JSON-RPC response.
		if err != nil {
			s.audit(r, "mcp.notification_error", map[string]any{"method": request.Method, "error": err.Error()})
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if err != nil {
		writeMCPError(w, request.ID, -32602, err.Error())
		return
	}
	s.audit(r, "mcp.request", map[string]any{"method": request.Method})
	writeMCPResult(w, request.ID, result)
}

func (s *Server) handleMCPMethod(method string, rawParams json.RawMessage) (any, error) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]string{"name": "qnap-ai-control", "version": "0.3.3"},
		}, nil
	case "notifications/initialized":
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpTools()}, nil
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if len(rawParams) == 0 {
			return nil, errors.New("tool name is required")
		}
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return nil, errors.New("tools/call params are invalid")
		}
		if strings.TrimSpace(params.Name) == "" {
			return nil, errors.New("tool name is required")
		}
		if params.Arguments == nil {
			params.Arguments = map[string]any{}
		}
		result, err := s.callMCPTool(params.Name, params.Arguments)
		return mcpToolResult(result, err), nil
	default:
		return nil, fmt.Errorf("method not found: %s", method)
	}
}

func writeMCPResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("MCP-Protocol-Version", mcpProtocolVersion)
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result})
}

func writeMCPError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("MCP-Protocol-Version", mcpProtocolVersion)
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error":   map[string]any{"code": code, "message": message},
	})
}

func mcpToolResult(value any, err error) map[string]any {
	if err != nil {
		return map[string]any{
			"content": []map[string]string{{"type": "text", "text": err.Error()}},
			"isError": true,
		}
	}
	text, marshalErr := json.MarshalIndent(value, "", "  ")
	if marshalErr != nil {
		text = []byte(fmt.Sprintf("%v", value))
	}
	return map[string]any{"content": []map[string]string{{"type": "text", "text": string(text)}}}
}

func mcpTools() []mcpTool {
	stringProp := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	numberProp := func(description string) map[string]any {
		return map[string]any{"type": "number", "description": description}
	}
	boolProp := func(description string) map[string]any {
		return map[string]any{"type": "boolean", "description": description}
	}
	arrayProp := func(description string) map[string]any {
		return map[string]any{"type": "array", "description": description, "items": map[string]any{"type": "string"}}
	}
	objectProp := func(description string) map[string]any {
		return map[string]any{"type": "object", "description": description}
	}
	enumProp := func(values ...string) map[string]any {
		items := make([]any, len(values))
		for i, value := range values {
			items[i] = value
		}
		return map[string]any{"type": "string", "enum": items}
	}
	tool := func(name, description string, properties map[string]any, required ...string) mcpTool {
		if properties == nil {
			properties = map[string]any{}
		}
		if required == nil {
			required = []string{}
		}
		return mcpTool{Name: name, Description: description, InputSchema: map[string]any{
			"type": "object", "properties": properties, "required": required,
		}}
	}

	return []mcpTool{
		tool("nas_health", "Check whether the QNAP AI control agent is reachable.", nil),
		tool("nas_capabilities", "Show allowed roots, allowlisted commands, profile, and confirmation policy.", nil),
		tool("nas_system_overview", "Read host, uptime, uname, and disk overview from QNAP.", nil),
		tool("nas_processes", "Read the QNAP process list.", nil),
		tool("nas_system_thermal", "Read CPU, system, disk, and fan temperatures.", nil),
		tool("nas_audit_tail", "Read recent QNAP AI Control audit entries.", map[string]any{"lines": numberProp("Number of lines, 1-500.")}),
		tool("nas_file_list", "List files under an allowed NAS path.", map[string]any{"path": stringProp("Absolute NAS path.")}, "path"),
		tool("nas_file_stat", "Read metadata for an allowed NAS path.", map[string]any{"path": stringProp("Absolute NAS path.")}, "path"),
		tool("nas_file_read", "Read an allowed NAS file as UTF-8 text when possible.", map[string]any{"path": stringProp("Absolute NAS path."), "max_bytes": numberProp("Maximum bytes to read.")}, "path"),
		tool("nas_file_write", "Write a text file under an allowed NAS path.", map[string]any{"path": stringProp("Absolute NAS path."), "content": stringProp("UTF-8 content."), "mode": stringProp("Octal mode, for example 0644."), "create_parents": boolProp("Create parent directories."), "dry_run": boolProp("Validate without writing."), "reason": stringProp("Reason for the change.")}, "path", "content"),
		tool("nas_command_run", "Run an allowlisted NAS command without shell expansion.", map[string]any{"argv": arrayProp("Executable followed by arguments."), "timeout_sec": numberProp("Timeout in seconds."), "stdin": stringProp("Optional standard input."), "dry_run": boolProp("Validate without executing."), "reason": stringProp("Reason for the command.")}, "argv"),
		tool("nas_qpkg_list", "List QNAP QPKG packages.", nil),
		tool("nas_qpkg_action", "Start, stop, or restart a QNAP QPKG package.", map[string]any{"name": stringProp("QPKG name."), "action": enumProp("start", "stop", "restart"), "dry_run": boolProp("Validate without executing."), "reason": stringProp("Reason for the action.")}, "name", "action"),
		tool("nas_qpkg_manage", "Manage QNAP QPKG packages. Install, remove, and update require confirmation.", map[string]any{"name": stringProp("QPKG name."), "action": enumProp("start", "stop", "restart", "enable", "disable", "status", "download", "add", "install_file", "install_url", "remove", "clean", "cancel", "update_all"), "path": stringProp("QPKG file path for install_file."), "url": stringProp("QPKG URL for install_url."), "dry_run": boolProp("Validate without executing."), "reason": stringProp("Reason for the action.")}, "action"),
		tool("nas_docker_info", "Read Container Station and Docker runtime information.", nil),
		tool("nas_docker_containers", "List Docker containers.", nil),
		tool("nas_docker_images", "List Docker images.", nil),
		tool("nas_docker_inspect", "Inspect one Docker container or image.", map[string]any{"name": stringProp("Container or image name.")}, "name"),
		tool("nas_docker_logs", "Read recent Docker container logs.", map[string]any{"name": stringProp("Container name."), "tail": numberProp("Lines to return, up to 2000.")}, "name"),
		tool("nas_docker_action", "Start, stop, restart, pause, or unpause a Docker container.", map[string]any{"name": stringProp("Container name."), "action": enumProp("start", "stop", "restart", "pause", "unpause"), "dry_run": boolProp("Validate without executing."), "reason": stringProp("Reason for the action.")}, "name", "action"),
		tool("nas_docker_command", "Run an allowlisted Docker subcommand. Creation and destructive commands require confirmation.", map[string]any{"subcommand": enumProp("run", "create", "exec", "pull", "push", "build", "images", "ps", "inspect", "logs", "stats", "top", "port", "diff", "start", "stop", "restart", "pause", "unpause", "kill", "rename", "update", "rm", "rmi", "tag", "save", "load", "cp", "commit", "export", "import", "history", "network", "volume", "system", "compose"), "args": arrayProp("Arguments after the Docker subcommand."), "timeout_sec": numberProp("Timeout in seconds."), "dry_run": boolProp("Validate without executing."), "reason": stringProp("Reason for the command.")}, "subcommand"),
		tool("nas_docker_run", "Prepare or dry-run docker run. Execution requires confirmation.", map[string]any{"args": arrayProp("Arguments after docker run."), "timeout_sec": numberProp("Timeout in seconds."), "dry_run": boolProp("Validate without executing."), "reason": stringProp("Reason for the command.")}, "args"),
		tool("nas_docker_create", "Prepare or dry-run docker create. Execution requires confirmation.", map[string]any{"args": arrayProp("Arguments after docker create."), "timeout_sec": numberProp("Timeout in seconds."), "dry_run": boolProp("Validate without executing."), "reason": stringProp("Reason for the command.")}, "args"),
		tool("nas_docker_remove", "Prepare or dry-run docker rm. Execution requires confirmation.", map[string]any{"args": arrayProp("Arguments after docker rm."), "timeout_sec": numberProp("Timeout in seconds."), "dry_run": boolProp("Validate without executing."), "reason": stringProp("Reason for the command.")}, "args"),
		tool("nas_docker_exec", "Run docker exec with raw arguments and no shell wrapper.", map[string]any{"args": arrayProp("Arguments after docker exec."), "timeout_sec": numberProp("Timeout in seconds."), "dry_run": boolProp("Validate without executing.")}, "args"),
		tool("nas_docker_pull", "Pull a Docker image.", map[string]any{"args": arrayProp("Arguments after docker pull."), "timeout_sec": numberProp("Timeout in seconds."), "dry_run": boolProp("Validate without executing.")}, "args"),
		tool("nas_docker_image_remove", "Prepare or dry-run docker rmi. Execution requires confirmation.", map[string]any{"args": arrayProp("Arguments after docker rmi."), "timeout_sec": numberProp("Timeout in seconds."), "dry_run": boolProp("Validate without executing."), "reason": stringProp("Reason for the command.")}, "args"),
		tool("nas_docker_network", "Manage Docker networks. Remove and prune require confirmation.", map[string]any{"args": arrayProp("Arguments after docker network."), "timeout_sec": numberProp("Timeout in seconds."), "dry_run": boolProp("Validate without executing."), "reason": stringProp("Reason for the command.")}, "args"),
		tool("nas_docker_volume", "Manage Docker volumes. Remove and prune require confirmation.", map[string]any{"args": arrayProp("Arguments after docker volume."), "timeout_sec": numberProp("Timeout in seconds."), "dry_run": boolProp("Validate without executing."), "reason": stringProp("Reason for the command.")}, "args"),
		tool("nas_docker_compose", "Run Docker Compose. Down and rm require confirmation.", map[string]any{"args": arrayProp("Arguments after docker compose."), "timeout_sec": numberProp("Timeout in seconds."), "dry_run": boolProp("Validate without executing."), "reason": stringProp("Reason for the command.")}, "args"),
		tool("nas_docker_stats", "Read Docker stats with no streaming by default.", map[string]any{"args": arrayProp("Arguments after docker stats."), "timeout_sec": numberProp("Timeout in seconds.")}),
		tool("nas_qnap_getcfg", "Read a QNAP config value from /etc/config/*.conf.", map[string]any{"section": stringProp("Config section."), "key": stringProp("Config key."), "file": stringProp("Config file path.")}, "section", "key"),
		tool("nas_prepare_operation", "Prepare a sensitive operation for explicit confirmation.", map[string]any{"operation": enumProp("docker_run_create", "docker_destroy", "qpkg_install_remove"), "arguments": objectProp("Operation arguments."), "reason": stringProp("Reason for the operation.")}, "operation", "arguments"),
		tool("nas_pending_operations", "List sensitive operations waiting for confirmation.", nil),
		tool("nas_confirm_operation", "Execute a prepared sensitive operation using its confirmation phrase.", map[string]any{"id": stringProp("Operation ID."), "confirmation_phrase": stringProp("Exact confirmation phrase.")}, "id", "confirmation_phrase"),
	}
}

func (s *Server) callMCPTool(name string, arguments map[string]any) (any, error) {
	switch name {
	case "nas_health":
		return s.callMCPAPI(http.MethodGet, "/v1/health", nil, nil)
	case "nas_capabilities":
		return s.callMCPAPI(http.MethodGet, "/v1/capabilities", nil, nil)
	case "nas_system_overview":
		return s.callMCPAPI(http.MethodGet, "/v1/system/overview", nil, nil)
	case "nas_processes":
		return s.callMCPAPI(http.MethodGet, "/v1/system/processes", nil, nil)
	case "nas_system_thermal":
		return s.callMCPAPI(http.MethodGet, "/v1/system/thermal", nil, nil)
	case "nas_audit_tail":
		return s.callMCPAPI(http.MethodGet, "/v1/audit/tail", mcpQuery(arguments, "lines"), nil)
	case "nas_file_list":
		return s.callMCPAPI(http.MethodGet, "/v1/files/list", mcpQuery(arguments, "path"), nil)
	case "nas_file_stat":
		return s.callMCPAPI(http.MethodGet, "/v1/files/stat", mcpQuery(arguments, "path"), nil)
	case "nas_file_read":
		return s.callMCPAPI(http.MethodPost, "/v1/files/read", nil, arguments)
	case "nas_file_write":
		return s.callMCPAPI(http.MethodPost, "/v1/files/write", nil, arguments)
	case "nas_command_run":
		return s.callMCPAPI(http.MethodPost, "/v1/command/run", nil, arguments)
	case "nas_qpkg_list":
		return s.callMCPAPI(http.MethodGet, "/v1/qnap/qpkg", nil, nil)
	case "nas_qpkg_action":
		return s.callMCPAPI(http.MethodPost, "/v1/qnap/qpkg/action", nil, arguments)
	case "nas_qpkg_manage":
		if isQpkgInstallRemoveAction(mcpString(arguments, "action")) && !mcpBool(arguments, "dry_run") {
			return s.prepareMCPAction("qpkg_install_remove", arguments)
		}
		return s.callMCPAPI(http.MethodPost, "/v1/qnap/qpkg/manage", nil, arguments)
	case "nas_docker_info":
		return s.callMCPAPI(http.MethodGet, "/v1/docker/info", nil, nil)
	case "nas_docker_containers":
		return s.callMCPAPI(http.MethodGet, "/v1/docker/containers", nil, nil)
	case "nas_docker_images":
		return s.callMCPAPI(http.MethodGet, "/v1/docker/images", nil, nil)
	case "nas_docker_inspect":
		return s.callMCPAPI(http.MethodPost, "/v1/docker/inspect", nil, arguments)
	case "nas_docker_logs":
		return s.callMCPAPI(http.MethodPost, "/v1/docker/logs", nil, arguments)
	case "nas_docker_action":
		return s.callMCPAPI(http.MethodPost, "/v1/docker/action", nil, arguments)
	case "nas_docker_command":
		return s.callMCPDockerCommand(mcpString(arguments, "subcommand"), arguments)
	case "nas_docker_run":
		return s.callMCPDockerCommand("run", arguments)
	case "nas_docker_create":
		return s.callMCPDockerCommand("create", arguments)
	case "nas_docker_remove":
		return s.callMCPDockerCommand("rm", arguments)
	case "nas_docker_exec":
		return s.callMCPDockerCommand("exec", arguments)
	case "nas_docker_pull":
		return s.callMCPDockerCommand("pull", arguments)
	case "nas_docker_image_remove":
		return s.callMCPDockerCommand("rmi", arguments)
	case "nas_docker_network":
		return s.callMCPDockerCommand("network", arguments)
	case "nas_docker_volume":
		return s.callMCPDockerCommand("volume", arguments)
	case "nas_docker_compose":
		return s.callMCPDockerCommand("compose", arguments)
	case "nas_docker_stats":
		return s.callMCPDockerCommand("stats", arguments)
	case "nas_qnap_getcfg":
		return s.callMCPAPI(http.MethodPost, "/v1/qnap/getcfg", nil, arguments)
	case "nas_prepare_operation":
		return s.callMCPAPI(http.MethodPost, "/v1/operations/prepare", nil, arguments)
	case "nas_pending_operations":
		return s.callMCPAPI(http.MethodGet, "/v1/operations/pending", nil, nil)
	case "nas_confirm_operation":
		return s.callMCPAPI(http.MethodPost, "/v1/operations/confirm", nil, arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *Server) callMCPDockerCommand(subcommand string, arguments map[string]any) (any, error) {
	if !allowedDockerSubcommand(subcommand) {
		return nil, fmt.Errorf("unsupported docker subcommand: %s", subcommand)
	}
	payload := map[string]any{
		"subcommand":  subcommand,
		"args":        mcpStringSlice(arguments, "args"),
		"timeout_sec": arguments["timeout_sec"],
		"dry_run":     mcpBool(arguments, "dry_run"),
	}
	if subcommand == "stats" && len(payload["args"].([]string)) == 0 {
		payload["args"] = []string{"--no-stream"}
	}
	if !mcpBool(arguments, "dry_run") {
		operation := ""
		if subcommand == "run" || subcommand == "create" {
			operation = "docker_run_create"
		} else if isDockerDestroyCommand(subcommand, payload["args"].([]string)) {
			operation = "docker_destroy"
		}
		if operation != "" {
			return s.prepareMCPAction(operation, payloadWithReason(payload, mcpString(arguments, "reason")))
		}
	}
	return s.callMCPAPI(http.MethodPost, "/v1/docker/command", nil, payload)
}

func (s *Server) prepareMCPAction(operation string, arguments map[string]any) (any, error) {
	reason := mcpString(arguments, "reason")
	payload := map[string]any{"operation": operation, "arguments": arguments, "reason": reason}
	prepared, err := s.callMCPAPI(http.MethodPost, "/v1/operations/prepare", nil, payload)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"confirmation_required": true,
		"instruction":           "Review the summary, then call nas_confirm_operation with id and confirmation_phrase exactly as returned.",
		"operation":             prepared,
	}, nil
}

func payloadWithReason(payload map[string]any, reason string) map[string]any {
	copy := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		copy[key] = value
	}
	copy["reason"] = reason
	return copy
}

func (s *Server) callMCPAPI(method, path string, query url.Values, payload any) (any, error) {
	if s.api == nil {
		return nil, errors.New("MCP API is not initialized")
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, "http://mcp.internal"+path, body)
	if err != nil {
		return nil, err
	}
	request.RemoteAddr = "mcp"
	if query != nil {
		request.URL.RawQuery = query.Encode()
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := &mcpResponseRecorder{header: make(http.Header)}
	s.api.ServeHTTP(recorder, request)
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}
	var response any
	if recorder.body.Len() > 0 {
		if err := json.Unmarshal(recorder.body.Bytes(), &response); err != nil {
			return nil, errors.New("agent returned an invalid JSON response")
		}
	}
	if recorder.status >= http.StatusBadRequest {
		if object, ok := response.(map[string]any); ok {
			if message, ok := object["error"].(string); ok && message != "" {
				return nil, errors.New(message)
			}
		}
		return nil, fmt.Errorf("agent request failed with HTTP %d", recorder.status)
	}
	return response, nil
}

func mcpQuery(arguments map[string]any, keys ...string) url.Values {
	query := make(url.Values)
	for _, key := range keys {
		if value, ok := arguments[key]; ok && value != nil {
			query.Set(key, fmt.Sprint(value))
		}
	}
	return query
}

func mcpString(arguments map[string]any, key string) string {
	value, _ := arguments[key].(string)
	return strings.TrimSpace(value)
}

func mcpBool(arguments map[string]any, key string) bool {
	value, _ := arguments[key].(bool)
	return value
}

func mcpStringSlice(arguments map[string]any, key string) []string {
	values, ok := arguments[key].([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			return []string{}
		}
		out = append(out, text)
	}
	return out
}
