package api

import (
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"

	qexec "qnap-ai-control-suite/agent/internal/exec"
	"qnap-ai-control-suite/agent/internal/files"
)

// statusSnapshot is intentionally best-effort: a missing optional NAS
// subsystem must not turn a basic health check into a failed request.
func (s *Server) statusSnapshot(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"partial": false,
		"agent": map[string]any{
			"version":  Version,
			"host":     s.hostname,
			"uptime_s": int(time.Since(s.started).Seconds()),
			"profile":  s.Config.Profile,
			"approval": s.Config.Approval,
		},
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	add := func(name string, fn func() (any, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := fn()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out[name] = map[string]any{"available": false, "reason": err.Error()}
				out["partial"] = true
				return
			}
			out[name] = value
		}()
	}
	add("resources", func() (any, error) { return s.System.Info(r.Context()) })
	add("thermal", func() (any, error) { return s.thermalSnapshot(r.Context()), nil })
	add("network", func() (any, error) {
		interfaces, err := s.Network.Interfaces()
		if err != nil {
			return nil, err
		}
		routes, routeErr := s.Network.Routes()
		return map[string]any{"interfaces": interfaces, "routes": routes, "routes_error": errorText(routeErr)}, nil
	})
	add("storage", func() (any, error) {
		disks, err := s.Storage.Disks()
		if err != nil {
			return nil, err
		}
		raid, raidErr := s.Storage.RAID()
		volumes, volumeErr := s.Storage.Volumes()
		return map[string]any{"disks": disks, "raid_groups": raid, "raid_error": errorText(raidErr), "volumes": volumes, "volumes_error": errorText(volumeErr)}, nil
	})
	add("docker", func() (any, error) { return s.Docker.Health(r.Context()) })
	add("qpkg", func() (any, error) { return s.QPKG.Inventory(r.Context()) })
	add("smb", func() (any, error) { return s.Shares.SMBStatus(r.Context()) })
	add("jobs", func() (any, error) { return map[string]any{"jobs": s.Jobs.List()}, nil })
	wg.Wait()
	s.ok(w, r, out)
}

func (s *Server) fileReadText(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string `json:"path"`
		Offset   int64  `json:"offset"`
		MaxBytes int64  `json:"max_bytes"`
	}
	if !decode(w, r, &req) {
		return
	}
	result, err := s.Files.Read(req.Path, req.Offset, req.MaxBytes)
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	data, err := base64.StdEncoding.DecodeString(result.ContentBase64)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "file_decode_failed", err.Error(), nil)
		return
	}
	s.ok(w, r, map[string]any{"path": result.Path, "text": string(data), "bytes": result.Bytes, "offset": result.Offset, "truncated": result.Truncated})
}

func (s *Server) fileReadLines(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string `json:"path"`
		Start    int    `json:"start"`
		Limit    int    `json:"limit"`
		MaxBytes int64  `json:"max_bytes"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Start < 0 {
		req.Start = 0
	}
	if req.Limit <= 0 || req.Limit > 2000 {
		req.Limit = 200
	}
	result, err := s.Files.Read(req.Path, 0, req.MaxBytes)
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	data, err := base64.StdEncoding.DecodeString(result.ContentBase64)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "file_decode_failed", err.Error(), nil)
		return
	}
	lines := strings.Split(string(data), "\n")
	if req.Start > len(lines) {
		req.Start = len(lines)
	}
	end := req.Start + req.Limit
	if end > len(lines) {
		end = len(lines)
	}
	s.ok(w, r, map[string]any{"path": result.Path, "start": req.Start, "next": end, "lines": lines[req.Start:end], "truncated": result.Truncated})
}

func (s *Server) fileGrep(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string `json:"path"`
		Query    string `json:"query"`
		Limit    int    `json:"limit"`
		MaxBytes int64  `json:"max_bytes"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		s.fail(w, r, http.StatusBadRequest, "invalid_request", "query is required", nil)
		return
	}
	if req.Limit <= 0 || req.Limit > 2000 {
		req.Limit = 200
	}
	result, err := s.Files.Read(req.Path, 0, req.MaxBytes)
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	data, err := base64.StdEncoding.DecodeString(result.ContentBase64)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "file_decode_failed", err.Error(), nil)
		return
	}
	matches := make([]map[string]any, 0)
	for index, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, req.Query) {
			matches = append(matches, map[string]any{"line": index + 1, "text": line})
			if len(matches) == req.Limit {
				break
			}
		}
	}
	s.ok(w, r, map[string]any{"path": result.Path, "query": req.Query, "matches": matches, "truncated": result.Truncated})
}

func (s *Server) fileTree(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action           string `json:"action"`
		Source           string `json:"source"`
		Target           string `json:"target"`
		DeleteExtraneous bool   `json:"delete_extraneous"`
	}
	if !decode(w, r, &req) {
		return
	}
	var result files.TreeResult
	var err error
	switch req.Action {
	case "copy_tree":
		result, err = s.Files.CopyTree(req.Source, req.Target)
	case "sync_tree":
		result, err = s.Files.SyncTree(req.Source, req.Target, req.DeleteExtraneous)
	default:
		s.fail(w, r, http.StatusBadRequest, "invalid_tree_action", "action must be copy_tree or sync_tree", nil)
		return
	}
	if err != nil {
		s.fileErr(w, r, err)
		return
	}
	s.ok(w, r, result)
}

func (s *Server) dockerHealth(w http.ResponseWriter, r *http.Request) {
	report, err := s.Docker.Health(r.Context())
	if err != nil {
		s.respondCommand(w, r, qexec.Result{}, err)
		return
	}
	s.ok(w, r, report)
}

func (s *Server) dockerComposeProjects(w http.ResponseWriter, r *http.Request) {
	inventory, err := s.Docker.ComposeProjects(r.Context())
	if err != nil && !inventory.Supported {
		s.ok(w, r, inventory)
		return
	}
	if err != nil {
		s.respondCommand(w, r, qexec.Result{}, err)
		return
	}
	s.ok(w, r, inventory)
}

func (s *Server) dockerReconstruct(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	reconstruction, err := s.Docker.Reconstruct(r.Context(), req.Name)
	if err != nil {
		s.respondCommand(w, r, qexec.Result{}, err)
		return
	}
	s.ok(w, r, reconstruction)
}

func (s *Server) networkIPv6Routes(w http.ResponseWriter, r *http.Request) {
	routes, err := s.Network.IPv6Routes()
	if err != nil {
		s.fail(w, r, http.StatusServiceUnavailable, "ipv6_routes_unavailable", err.Error(), nil)
		return
	}
	s.ok(w, r, map[string]any{"routes": routes})
}

func (s *Server) networkIPv6Neighbors(w http.ResponseWriter, r *http.Request) {
	neighbors, err := s.Network.IPv6Neighbors()
	if err != nil {
		s.fail(w, r, http.StatusServiceUnavailable, "ipv6_neighbors_unavailable", err.Error(), nil)
		return
	}
	s.ok(w, r, map[string]any{"neighbors": neighbors})
}
