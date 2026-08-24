//go:build darwin || linux

package exec

import (
	"bytes"
	osexec "os/exec"
	"syscall"
	"time"
)

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

func configureProcessGroup(cmd *osexec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateProcessGroup first gives every descendant a chance to exit cleanly,
// then kills the group. Using -pid is essential for sh -c / compose trees.
func terminateProcessGroup(cmd *osexec.Cmd, done <-chan error, grace time.Duration) error {
	if cmd.Process == nil {
		return <-done
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return <-done
	}
}
