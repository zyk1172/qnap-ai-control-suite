package capability

import "time"

type Status string

const (
	Available   Status = "available"
	Degraded    Status = "degraded"
	Unavailable Status = "unavailable"
)

type Capability struct {
	ID         string         `json:"id"`
	Domain     string         `json:"domain"`
	Action     string         `json:"action,omitempty"`
	Adapter    string         `json:"adapter,omitempty"`
	Status     Status         `json:"status"`
	Backend    string         `json:"backend,omitempty"`
	Provider   string         `json:"provider,omitempty"`
	Verified   bool           `json:"verified"`
	Persistent bool           `json:"persistent"`
	ReadOnly   bool           `json:"read_only,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	Evidence   []string       `json:"evidence,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Device struct {
	Model    string `json:"model,omitempty"`
	Firmware string `json:"firmware,omitempty"`
	Platform string `json:"platform,omitempty"`
	Arch     string `json:"arch,omitempty"`
}

type Manifest struct {
	SchemaVersion int          `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	Fingerprint   string       `json:"fingerprint"`
	Device        Device       `json:"device"`
	Capabilities  []Capability `json:"capabilities"`
}

func (m Manifest) ForAdapter(name string) []Capability {
	out := make([]Capability, 0)
	for _, item := range m.Capabilities {
		if item.Adapter == name {
			out = append(out, item)
		}
	}
	return out
}

func (m Manifest) Get(id string) (Capability, bool) {
	for _, item := range m.Capabilities {
		if item.ID == id {
			return item, true
		}
	}
	return Capability{}, false
}
