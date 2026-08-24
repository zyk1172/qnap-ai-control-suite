//go:build !darwin && !linux

package exec

import (
	"bytes"
	osexec "os/exec"
	"time"
)

func bytesReader(b []byte) *bytes.Reader  { return bytes.NewReader(b) }
func configureProcessGroup(_ *osexec.Cmd) {}
func terminateProcessGroup(cmd *osexec.Cmd, done <-chan error, _ time.Duration) error {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return <-done
}
