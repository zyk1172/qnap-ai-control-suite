package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type ComposeInventory struct {
	Supported bool             `json:"supported"`
	Projects  []ComposeProject `json:"projects"`
	Reason    string           `json:"reason,omitempty"`
}

type ComposeProject struct {
	Name        string         `json:"name"`
	Status      string         `json:"status,omitempty"`
	WorkingDir  string         `json:"working_dir,omitempty"`
	ConfigFiles []string       `json:"config_files,omitempty"`
	Raw         map[string]any `json:"raw,omitempty"`
}

// ComposeProjects returns the structured output of `docker compose ls`.
// Compose is an optional Docker CLI plugin on QNAP, so a missing or
// unsupported compose subcommand is reported as Supported=false in addition
// to being returned as an error for callers that need strict behavior.
func (s Service) ComposeProjects(ctx context.Context) (ComposeInventory, error) {
	result, err := s.Run(ctx, []string{"compose", "ls", "--all", "--format", "json"}, 30)
	if err != nil {
		return ComposeInventory{Supported: false, Projects: []ComposeProject{}, Reason: "docker compose ls is unavailable"}, err
	}
	projects, err := ParseComposeProjects(result.Stdout)
	if err != nil {
		return ComposeInventory{Supported: false, Projects: []ComposeProject{}, Reason: "docker compose returned invalid JSON"}, err
	}
	return ComposeInventory{Supported: true, Projects: projects}, nil
}

// ComposeProjectInventory is an operation-oriented alias for ComposeProjects.
func (s Service) ComposeProjectInventory(ctx context.Context) (ComposeInventory, error) {
	return s.ComposeProjects(ctx)
}

func ParseComposeProjects(stdout string) ([]ComposeProject, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(stdout)))
	projects := []ComposeProject{}
	for {
		var raw json.RawMessage
		err := decoder.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse docker compose project inventory: %w", err)
		}
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode docker compose project inventory: %w", err)
		}
		items, err := composeProjectValues(value)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			project, err := composeProjectFromMap(item)
			if err != nil {
				return nil, err
			}
			projects = append(projects, project)
		}
	}
	return projects, nil
}

func composeProjectValues(value any) ([]map[string]any, error) {
	switch typed := value.(type) {
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			object, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("docker compose project item is not an object")
			}
			out = append(out, object)
		}
		return out, nil
	case map[string]any:
		for _, key := range []string{"projects", "Projects", "items", "Items"} {
			if nested, ok := typed[key]; ok {
				return composeProjectValues(nested)
			}
		}
		return []map[string]any{typed}, nil
	default:
		return nil, fmt.Errorf("docker compose project inventory must be an object or array")
	}
}

func composeProjectFromMap(raw map[string]any) (ComposeProject, error) {
	project := ComposeProject{Raw: raw}
	project.Name = composeString(raw, "Name", "name")
	project.Status = composeString(raw, "Status", "status")
	project.WorkingDir = composeString(raw, "WorkingDir", "working_dir", "working-directory")
	project.ConfigFiles = composeStrings(raw, "ConfigFiles", "config_files", "configFiles")
	if project.Name == "" {
		return ComposeProject{}, fmt.Errorf("docker compose project is missing Name")
	}
	return project, nil
}

func composeString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if text, ok := value.(string); ok {
				return text
			}
		}
	}
	return ""
}

func composeStrings(raw map[string]any, keys ...string) []string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return []string{typed}
			}
			return []string{}
		case []any:
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if text, ok := item.(string); ok && text != "" {
					out = append(out, text)
				}
			}
			return out
		case []string:
			return append([]string(nil), typed...)
		}
	}
	return []string{}
}
