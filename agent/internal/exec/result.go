package exec

import "time"

type Request struct {
	Argv      []string          `json:"argv"`
	CWD       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Stdin     []byte            `json:"-"`
	Timeout   time.Duration     `json:"-"`
	MaxOutput int               `json:"-"`
	DryRun    bool              `json:"dry_run,omitempty"`
}

type Result struct {
	Argv            []string `json:"argv"`
	CWD             string   `json:"cwd,omitempty"`
	ExitCode        int      `json:"exit_code"`
	Stdout          string   `json:"stdout"`
	Stderr          string   `json:"stderr"`
	StdoutTruncated bool     `json:"stdout_truncated"`
	StderrTruncated bool     `json:"stderr_truncated"`
	DurationMS      int64    `json:"duration_ms"`
	DryRun          bool     `json:"dry_run"`
}
