package exec

import "fmt"

type ErrorKind string

const (
	NotFound    ErrorKind = "not_found"
	TimedOut    ErrorKind = "timeout"
	NonZeroExit ErrorKind = "non_zero_exit"
	StartFailed ErrorKind = "start_failed"
)

type CommandError struct {
	Kind   ErrorKind
	Result Result
	Err    error
}

func (e *CommandError) Error() string {
	if e.Err == nil {
		return string(e.Kind)
	}
	return fmt.Sprintf("%s: %v", e.Kind, e.Err)
}
func (e *CommandError) Unwrap() error { return e.Err }
