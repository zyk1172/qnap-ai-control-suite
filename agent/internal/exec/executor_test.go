package exec

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecutorSuccessCWDStdinAndEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix only")
	}
	r, err := (Executor{}).Run(context.Background(), Request{Argv: []string{"/bin/sh", "-c", "pwd; cat; printf %s \"$QACS_TEST\""}, CWD: t.TempDir(), Stdin: []byte("input"), Env: map[string]string{"QACS_TEST": "env"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Stdout, "inputenv") {
		t.Fatalf("unexpected stdout %q", r.Stdout)
	}
	if r.CWD == "" {
		t.Fatal("cwd missing")
	}
}
func TestExecutorNonZero(t *testing.T) {
	r, err := (Executor{}).Run(context.Background(), Request{Argv: []string{"/bin/sh", "-c", "echo bad >&2; exit 7"}})
	var commandErr *CommandError
	if !errorsAs(err, &commandErr) || commandErr.Kind != NonZeroExit || r.ExitCode != 7 || !strings.Contains(r.Stderr, "bad") {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}
func TestExecutorTimeout(t *testing.T) {
	_, err := (Executor{}).Run(context.Background(), Request{Argv: []string{"/bin/sh", "-c", "sleep 1"}, Timeout: 10 * time.Millisecond})
	var commandErr *CommandError
	if !errorsAs(err, &commandErr) || commandErr.Kind != TimedOut {
		t.Fatalf("expected timeout, got %v", err)
	}
}
func TestExecutorTruncates(t *testing.T) {
	r, err := (Executor{}).Run(context.Background(), Request{Argv: []string{"/bin/sh", "-c", "printf 1234567890; printf abcdefghij >&2"}, MaxOutput: 5})
	if err != nil {
		t.Fatal(err)
	}
	if r.Stdout != "12345" || r.Stderr != "abcde" || !r.StdoutTruncated || !r.StderrTruncated {
		t.Fatalf("unexpected %+v", r)
	}
}
func TestExecutorDoesNotMarkExactOutputAsTruncated(t *testing.T) {
	r, err := (Executor{}).Run(context.Background(), Request{Argv: []string{"/bin/sh", "-c", "printf 12345"}, MaxOutput: 5})
	if err != nil {
		t.Fatal(err)
	}
	if r.StdoutTruncated {
		t.Fatalf("exact output marked truncated: %+v", r)
	}
}
func TestExecutorDryRun(t *testing.T) {
	r, err := (Executor{}).Run(context.Background(), Request{Argv: []string{"does-not-run"}, DryRun: true})
	if err != nil || !r.DryRun {
		t.Fatalf("unexpected %+v %v", r, err)
	}
}
func errorsAs(err error, target **CommandError) bool {
	for err != nil {
		if e, ok := err.(*CommandError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
