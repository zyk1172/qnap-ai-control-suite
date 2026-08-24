package docker

import (
	"context"
	"strings"
)

type ContainerHealthReport struct {
	Supported  bool              `json:"supported"`
	Containers []ContainerHealth `json:"containers"`
	Summary    HealthSummary     `json:"summary"`
	Reason     string            `json:"reason,omitempty"`
}

type HealthSummary struct {
	Total         int `json:"total"`
	Healthy       int `json:"healthy"`
	Unhealthy     int `json:"unhealthy"`
	Starting      int `json:"starting"`
	NotStarted    int `json:"not_started"`
	NoHealthcheck int `json:"no_healthcheck"`
	Unknown       int `json:"unknown"`
}

type ContainerHealth struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	State          string      `json:"state"`
	Health         string      `json:"health"`
	Healthy        *bool       `json:"healthy,omitempty"`
	HasHealthcheck bool        `json:"has_healthcheck"`
	FailingStreak  int         `json:"failing_streak,omitempty"`
	Log            []HealthLog `json:"log,omitempty"`
}

type HealthLog struct {
	Start    string `json:"start,omitempty"`
	End      string `json:"end,omitempty"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output,omitempty"`
}

// Health reads Docker inspect health state for all containers, or for the
// explicitly supplied container names/IDs. It deliberately uses inspect
// rather than parsing the human-oriented ps Status column so health check
// state and recent check output remain structured.
func (s Service) Health(ctx context.Context, names ...string) (ContainerHealthReport, error) {
	report := ContainerHealthReport{Supported: true, Containers: []ContainerHealth{}}
	refs := compactStrings(names)
	if len(refs) == 0 {
		result, err := s.Run(ctx, []string{"ps", "-a", "-q"}, 30)
		if err != nil {
			return ContainerHealthReport{Supported: false, Reason: "docker ps is unavailable"}, err
		}
		refs = strings.Fields(result.Stdout)
	}
	if len(refs) == 0 {
		return report, nil
	}
	documents, err := s.inspectContainers(ctx, refs...)
	if err != nil {
		return ContainerHealthReport{Supported: false, Reason: "docker inspect is unavailable"}, err
	}
	for _, document := range documents {
		item := healthFromInspect(document)
		report.Containers = append(report.Containers, item)
		report.Summary.Total++
		switch item.Health {
		case "healthy":
			report.Summary.Healthy++
		case "unhealthy":
			report.Summary.Unhealthy++
		case "starting":
			report.Summary.Starting++
		case "not_started":
			report.Summary.NotStarted++
		case "none":
			report.Summary.NoHealthcheck++
		default:
			report.Summary.Unknown++
		}
	}
	return report, nil
}

// ContainerHealth is a named alias for callers that prefer an operation-like
// method name while keeping Health as the concise primary API.
func (s Service) ContainerHealth(ctx context.Context, names ...string) (ContainerHealthReport, error) {
	return s.Health(ctx, names...)
}

func healthFromInspect(document inspectDocument) ContainerHealth {
	status := document.State.Status
	if status == "" {
		status = "unknown"
	}
	item := ContainerHealth{
		ID:     document.ID,
		Name:   strings.TrimPrefix(document.Name, "/"),
		State:  status,
		Health: "none",
	}

	configured := document.Config.Healthcheck != nil && !healthcheckDisabled(document.Config.Healthcheck)
	item.HasHealthcheck = configured
	if !configured {
		return item
	}
	item.Health = "not_started"
	if document.State.Health != nil {
		if document.State.Health.Status != "" {
			item.Health = document.State.Health.Status
		} else {
			item.Health = "unknown"
		}
		item.FailingStreak = document.State.Health.FailingStreak
		for _, log := range document.State.Health.Log {
			item.Log = append(item.Log, HealthLog{Start: log.Start, End: log.End, ExitCode: log.ExitCode, Output: log.Output})
		}
	}
	switch item.Health {
	case "healthy":
		value := true
		item.Healthy = &value
	case "unhealthy":
		value := false
		item.Healthy = &value
	}
	return item
}

func healthcheckDisabled(check *inspectHealthcheck) bool {
	if check == nil || len(check.Test) == 0 {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(check.Test[0]), "NONE")
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
