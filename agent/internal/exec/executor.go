package exec

import (
	"context"
	"errors"
	"os"
	osexec "os/exec"
	"time"
)

type Executor struct {
	DefaultTimeout time.Duration
	MaxOutput      int
}

func (e Executor) Run(ctx context.Context, req Request) (Result, error) {
	if len(req.Argv) == 0 || req.Argv[0] == "" {
		return Result{}, &CommandError{Kind: StartFailed, Err: errors.New("argv[0] is required")}
	}
	if req.Timeout <= 0 {
		req.Timeout = e.DefaultTimeout
	}
	if req.Timeout <= 0 {
		req.Timeout = 30 * time.Second
	}
	if req.MaxOutput <= 0 {
		req.MaxOutput = e.MaxOutput
	}
	if req.MaxOutput <= 0 {
		req.MaxOutput = 8 * 1024 * 1024
	}
	result := Result{Argv: append([]string(nil), req.Argv...), CWD: req.CWD, ExitCode: 0, DryRun: req.DryRun}
	if req.DryRun {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	started := time.Now()
	cmd := osexec.Command(req.Argv[0], req.Argv[1:]...)
	cmd.Dir = req.CWD
	cmd.Stdin = bytesReader(req.Stdin)
	configureProcessGroup(cmd)
	if len(req.Env) > 0 {
		cmd.Env = os.Environ()
		for key, value := range req.Env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	stdout, stderr := newLimitedBuffer(req.MaxOutput), newLimitedBuffer(req.MaxOutput)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		result.DurationMS = time.Since(started).Milliseconds()
		if errors.Is(err, osexec.ErrNotFound) {
			return result, &CommandError{Kind: NotFound, Result: result, Err: err}
		}
		return result, &CommandError{Kind: StartFailed, Result: result, Err: err}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		err = terminateProcessGroup(cmd, done, 5*time.Second)
	}
	result.Stdout, result.Stderr, result.StdoutTruncated, result.StderrTruncated = stdout.String(), stderr.String(), stdout.truncated, stderr.truncated
	result.DurationMS = time.Since(started).Milliseconds()
	if err == nil {
		return result, nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		return result, &CommandError{Kind: TimedOut, Result: result, Err: ctx.Err()}
	}
	if ctx.Err() == context.Canceled {
		return result, &CommandError{Kind: Cancelled, Result: result, Err: context.Canceled}
	}
	var exitErr *osexec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, &CommandError{Kind: NonZeroExit, Result: result, Err: err}
	}
	if errors.Is(err, osexec.ErrNotFound) {
		return result, &CommandError{Kind: NotFound, Result: result, Err: err}
	}
	return result, &CommandError{Kind: StartFailed, Result: result, Err: err}
}
