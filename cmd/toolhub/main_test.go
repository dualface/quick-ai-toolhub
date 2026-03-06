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

func TestRunTaskOutputsSuccessEnvelope(t *testing.T) {
	orig := newRunTaskExecutor
	t.Cleanup(func() { newRunTaskExecutor = orig })
	newRunTaskExecutor = func() runTaskExecutor {
		return fakeRunTaskExecutor{
			run: func(_ context.Context, opts agentrun.RunOptions) (agentrun.Result, error) {
				if opts.Lens != "delivery" {
					t.Fatalf("unexpected lens: %s", opts.Lens)
				}
				if opts.ContextRefs.GitHubPRNumber != 42 {
					t.Fatalf("unexpected pr number: %d", opts.ContextRefs.GitHubPRNumber)
				}
				if opts.ContextRefs.ArtifactRefs.Log != "logs/input.log" {
					t.Fatalf("unexpected context log: %s", opts.ContextRefs.ArtifactRefs.Log)
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
		"--lens", "delivery",
		"--github-pr-number", "42",
		"--context-log", "logs/input.log",
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

func TestRunTaskOutputsErrorEnvelope(t *testing.T) {
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

	var response commandResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.OK || response.Error == nil || response.Error.Code != agentrun.ErrorCodeMalformedOutput {
		t.Fatalf("unexpected response: %s", stdout.String())
	}
}

func TestRunTaskHelpIncludesContextFlags(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"help"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run help: %v", err)
	}
	output := stdout.String()
	for _, needle := range []string{"--lens", "--github-pr-number", "--context-log"} {
		if !strings.Contains(output, needle) {
			t.Fatalf("missing %s in help output:\n%s", needle, output)
		}
	}
}
