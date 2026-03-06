package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"quick-ai-toolhub/internal/agentrun"
)

type fakeRunTaskExecutor struct {
	run func(context.Context, agentrun.RunOptions) (agentrun.Result, error)
}

func (f fakeRunTaskExecutor) RunTask(ctx context.Context, opts agentrun.RunOptions) (agentrun.Result, error) {
	return f.run(ctx, opts)
}

func TestRunTaskOutputsHumanReadableResult(t *testing.T) {
	orig := newRunTaskExecutor
	t.Cleanup(func() { newRunTaskExecutor = orig })
	newRunTaskExecutor = func() runTaskExecutor {
		return fakeRunTaskExecutor{
			run: func(_ context.Context, opts agentrun.RunOptions) (agentrun.Result, error) {
				if opts.Lens != "delivery" {
					t.Fatalf("unexpected lens: %s", opts.Lens)
				}
				if !opts.Yolo {
					t.Fatal("expected yolo to be enabled")
				}
				if opts.ProgressOutput == nil {
					t.Fatal("expected progress output writer")
				}
				if opts.ContextRefs.GitHubPRNumber != 42 {
					t.Fatalf("unexpected pr number: %d", opts.ContextRefs.GitHubPRNumber)
				}
				if opts.ContextRefs.ArtifactRefs.Log != "logs/input.log" {
					t.Fatalf("unexpected context log: %s", opts.ContextRefs.ArtifactRefs.Log)
				}
				if !opts.IsolatedCodexHome {
					t.Fatal("expected isolated codex home to be enabled")
				}
				return agentrun.Result{
					Runner:     agentrun.RunnerCodexExec,
					Status:     "success",
					Summary:    "done",
					NextAction: "proceed",
					ArtifactRefs: agentrun.ArtifactRefs{
						Log:    ".toolhub/runs/log",
						Report: ".toolhub/runs/result.json",
					},
				}, nil
			},
		}
	}

	var stdout bytes.Buffer
	if err := run(context.Background(), []string{
		"run-task", "Sprint-04/Task-01",
		"--lens", "delivery",
		"--github-pr-number", "42",
		"--context-log", "logs/input.log",
		"--yolo",
		"--isolated-codex-home",
	}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	output := stdout.String()
	for _, needle := range []string{
		"Task: Sprint-04/Task-01",
		"Runner: codex_exec",
		"Status: success",
		"Next: proceed",
		"Summary:",
		"done",
		"Artifacts:",
		"- log: .toolhub/runs/log",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("missing %q in output:\n%s", needle, output)
		}
	}
}

func TestRunTaskStreamDisablesProgress(t *testing.T) {
	orig := newRunTaskExecutor
	t.Cleanup(func() { newRunTaskExecutor = orig })
	newRunTaskExecutor = func() runTaskExecutor {
		return fakeRunTaskExecutor{
			run: func(_ context.Context, opts agentrun.RunOptions) (agentrun.Result, error) {
				if opts.StreamOutput == nil {
					t.Fatal("expected stream output writer")
				}
				if opts.ProgressOutput != nil {
					t.Fatal("expected progress output to be disabled when stream is enabled")
				}
				return agentrun.Result{
					Runner:     agentrun.RunnerCodexExec,
					Status:     "success",
					Summary:    "done",
					NextAction: "proceed",
				}, nil
			},
		}
	}

	var stdout bytes.Buffer
	if err := run(context.Background(), []string{
		"run-task", "Sprint-04/Task-01",
		"--stream",
	}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	var response commandResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.OK || response.Data == nil || response.Data.Status != "success" {
		t.Fatalf("unexpected response: %s", stdout.String())
	}
}

func TestRunTaskOutputsHumanReadableError(t *testing.T) {
	orig := newRunTaskExecutor
	t.Cleanup(func() { newRunTaskExecutor = orig })
	newRunTaskExecutor = func() runTaskExecutor {
		return fakeRunTaskExecutor{
			run: func(_ context.Context, _ agentrun.RunOptions) (agentrun.Result, error) {
				return agentrun.Result{}, agentrun.AsToolError(errors.New("malformed_output: missing summary"))
			},
		}
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"run-task", "Sprint-04/Task-01"}, &stdout, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected cli exit error")
	}

	var exitErr *cliExitError
	if !errors.As(err, &exitErr) || exitErr.code != 1 {
		t.Fatalf("unexpected error type: %v", err)
	}

	output := stdout.String()
	for _, needle := range []string{
		"Error: malformed_output",
		"Message: malformed_output: missing summary",
		"Retryable: true",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("missing %q in output:\n%s", needle, output)
		}
	}
}

func TestRunTaskHelpIncludesContextFlags(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"help"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run help: %v", err)
	}
	output := stdout.String()
	for _, needle := range []string{"toolhub serve", "--lens", "--github-pr-number", "--context-log", "--config-file", "--yolo", "--isolated-codex-home", "--no-progress"} {
		if !strings.Contains(output, needle) {
			t.Fatalf("missing %s in help output:\n%s", needle, output)
		}
	}
}

func TestServeBootstrapsApplication(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"serve"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run serve: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &payload); err != nil {
		t.Fatalf("unmarshal serve output: %v\noutput=%s", err, stdout.String())
	}
	if payload["msg"] != "toolhub bootstrapped" {
		t.Fatalf("unexpected message: %#v", payload["msg"])
	}

	components, ok := payload["components"].([]any)
	if !ok {
		t.Fatalf("missing components: %#v", payload["components"])
	}
	if len(components) != 6 {
		t.Fatalf("unexpected component count: %d", len(components))
	}
}

func TestRunTaskInvalidFlagIsClassifiedAsInvalidRequest(t *testing.T) {
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"run-task", "--timeout", "not-a-duration", "Sprint-04/Task-01"}, &stdout, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected cli exit error")
	}

	var exitErr *cliExitError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Fatalf("unexpected error type: %v", err)
	}

	output := stdout.String()
	for _, needle := range []string{
		"Error: invalid_request",
		"Message:",
		"Retryable: false",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("missing %q in output:\n%s", needle, output)
		}
	}
}
