package command

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestExecutorRunSuccessCapturesWorkDirEnvAndStreams(t *testing.T) {
	workdir := t.TempDir()
	runner := Executor{}

	result := runner.Run(context.Background(), Request{
		WorkDir: workdir,
		Args: []string{
			requireShell(t), "-c",
			"pwd; printf '%s' \"$TOOLHUB_EXEC_TEST\"; printf 'stderr-output' >&2",
		},
		Env: append(os.Environ(), "TOOLHUB_EXEC_TEST=env-output"),
	})
	if result.Err != nil {
		t.Fatalf("run command: %v", result.Err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %d", result.ExitCode)
	}
	if result.TimedOut {
		t.Fatal("expected non-timeout result")
	}
	if !strings.Contains(string(result.Stdout), workdir) {
		t.Fatalf("expected stdout to include workdir %q, got %q", workdir, string(result.Stdout))
	}
	if !strings.Contains(string(result.Stdout), "env-output") {
		t.Fatalf("expected stdout to include env value, got %q", string(result.Stdout))
	}
	if string(result.Stderr) != "stderr-output" {
		t.Fatalf("unexpected stderr: %q", string(result.Stderr))
	}
}

func TestExecutorRunFailurePreservesExitCodeAndOutput(t *testing.T) {
	runner := Executor{}

	result := runner.Run(context.Background(), Request{
		Args: []string{
			requireShell(t), "-c",
			"printf 'stdout-output'; printf 'stderr-output' >&2; exit 7",
		},
	})
	if result.Err == nil {
		t.Fatal("expected command failure")
	}
	if result.ExitCode != 7 {
		t.Fatalf("unexpected exit code: %d", result.ExitCode)
	}
	if result.TimedOut {
		t.Fatal("expected non-timeout failure")
	}
	if string(result.Stdout) != "stdout-output" {
		t.Fatalf("unexpected stdout: %q", string(result.Stdout))
	}
	if string(result.Stderr) != "stderr-output" {
		t.Fatalf("unexpected stderr: %q", string(result.Stderr))
	}
}

func TestExecutorRunTimeoutMarksResult(t *testing.T) {
	runner := Executor{}

	result := runner.Run(context.Background(), Request{
		Args: []string{
			requireShell(t), "-c",
			"printf 'stdout-before-timeout'; printf 'stderr-before-timeout' >&2; sleep 1",
		},
		Timeout: 50 * time.Millisecond,
	})
	if !result.TimedOut {
		t.Fatal("expected timeout result")
	}
	if !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", result.Err)
	}
	if result.ExitCode != ExitCodeUnavailable {
		t.Fatalf("unexpected exit code for timeout: %d", result.ExitCode)
	}
	if !strings.Contains(string(result.Stdout), "stdout-before-timeout") {
		t.Fatalf("expected partial stdout, got %q", string(result.Stdout))
	}
	if !strings.Contains(string(result.Stderr), "stderr-before-timeout") {
		t.Fatalf("expected partial stderr, got %q", string(result.Stderr))
	}
}

func requireShell(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	return path
}
