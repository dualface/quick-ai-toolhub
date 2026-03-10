package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"quick-ai-toolhub/internal/agentrun"
	"quick-ai-toolhub/internal/app"
	sharedconfig "quick-ai-toolhub/internal/config"
	"quick-ai-toolhub/internal/reviewagg"
	"quick-ai-toolhub/internal/worktreeprep"
)

type fakeRunTaskExecutor struct {
	run func(context.Context, agentrun.RunOptions) (agentrun.Result, error)
}

func (f fakeRunTaskExecutor) RunTask(ctx context.Context, opts agentrun.RunOptions) (agentrun.Result, error) {
	return f.run(ctx, opts)
}

type fakeRunAgentExecutor struct {
	execute func(context.Context, agentrun.Request, agentrun.ExecuteOptions) agentrun.Response
}

func (f fakeRunAgentExecutor) Execute(ctx context.Context, req agentrun.Request, opts agentrun.ExecuteOptions) agentrun.Response {
	return f.execute(ctx, req, opts)
}

type fakePrepareWorktreeExecutor struct {
	execute func(context.Context, worktreeprep.Request, worktreeprep.ExecuteOptions) worktreeprep.Response
}

func (f fakePrepareWorktreeExecutor) Execute(ctx context.Context, req worktreeprep.Request, opts worktreeprep.ExecuteOptions) worktreeprep.Response {
	return f.execute(ctx, req, opts)
}

type fakeReviewResultExecutor struct {
	execute func(context.Context, reviewagg.Request) reviewagg.Response
}

func (f fakeReviewResultExecutor) Execute(ctx context.Context, req reviewagg.Request) reviewagg.Response {
	return f.execute(ctx, req)
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
				if opts.Runner != "" {
					t.Fatalf("expected runner override to be empty, got %s", opts.Runner)
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
		"Runner: codex-cli",
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

func TestRunTaskPassesRunnerOverride(t *testing.T) {
	orig := newRunTaskExecutor
	t.Cleanup(func() { newRunTaskExecutor = orig })
	newRunTaskExecutor = func() runTaskExecutor {
		return fakeRunTaskExecutor{
			run: func(_ context.Context, opts agentrun.RunOptions) (agentrun.Result, error) {
				if opts.Runner != agentrun.RunnerClaudeCLI {
					t.Fatalf("unexpected runner: %s", opts.Runner)
				}
				return agentrun.Result{
					Runner:     agentrun.RunnerClaudeCLI,
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
		"--runner", "claude-cli",
	}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if !strings.Contains(stdout.String(), "Runner: claude-cli") {
		t.Fatalf("missing runner in output:\n%s", stdout.String())
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

	var response agentrun.Response
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.OK || response.Data == nil || response.Data.Status != "success" {
		t.Fatalf("unexpected response: %s", stdout.String())
	}
}

func TestRunAgentToolOutputsJSONResponse(t *testing.T) {
	orig := newRunAgentExecutor
	t.Cleanup(func() { newRunAgentExecutor = orig })
	newRunAgentExecutor = func() runAgentExecutor {
		return fakeRunAgentExecutor{
			execute: func(_ context.Context, req agentrun.Request, opts agentrun.ExecuteOptions) agentrun.Response {
				if req.TaskID != "Sprint-04/Task-01" {
					t.Fatalf("unexpected task id: %s", req.TaskID)
				}
				if req.TimeoutSeconds != 90 {
					t.Fatalf("unexpected timeout seconds: %d", req.TimeoutSeconds)
				}
				if opts.WorkDir == "" {
					t.Fatal("expected workdir")
				}
				if req.Runner != agentrun.RunnerClaudeCLI {
					t.Fatalf("unexpected runner: %s", req.Runner)
				}
				return agentrun.Response{
					OK: true,
					Data: &agentrun.Result{
						Runner:     agentrun.RunnerCodexExec,
						Status:     "success",
						Summary:    "done",
						NextAction: "proceed",
					},
				}
			},
		}
	}

	var stdout bytes.Buffer
	if err := run(context.Background(), []string{
		"run-agent-tool",
		"--task-id", "Sprint-04/Task-01",
		"--timeout-seconds", "90",
		"--runner", "claude-cli",
	}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	var response agentrun.Response
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.OK || response.Data == nil || response.Data.Runner != agentrun.RunnerCodexExec {
		t.Fatalf("unexpected response: %s", stdout.String())
	}
}

func TestRunTaskRejectsUnsupportedRunner(t *testing.T) {
	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"run-task", "Sprint-04/Task-01",
		"--runner", "bad-runner",
	}, &stdout, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected cli exit error")
	}

	var exitErr *cliExitError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Fatalf("unexpected error type: %v", err)
	}
	if !strings.Contains(stdout.String(), `unsupported runner "bad-runner"`) {
		t.Fatalf("unexpected output:\n%s", stdout.String())
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
	for _, needle := range []string{"toolhub serve", "github-sync-tool", "prepare-worktree-tool", "review-result-tool", "run-agent-tool", "--sprint-id", "--task-id", "full_reconcile", "--lens", "--github-pr-number", "--context-log", "--config-file", "--runner", "--yolo", "--isolated-codex-home", "--timeout-seconds", "--no-progress"} {
		if !strings.Contains(output, needle) {
			t.Fatalf("missing %s in help output:\n%s", needle, output)
		}
	}
}

func TestRunReviewResultToolOutputsJSONResponse(t *testing.T) {
	orig := newReviewResultExecutor
	t.Cleanup(func() { newReviewResultExecutor = orig })
	newReviewResultExecutor = func() reviewResultExecutor {
		return fakeReviewResultExecutor{
			execute: func(_ context.Context, req reviewagg.Request) reviewagg.Response {
				if req.TaskID != "Sprint-04/Task-03" {
					t.Fatalf("unexpected task id: %s", req.TaskID)
				}
				if req.ReviewResult.ReviewerID != "reviewer-correctness" {
					t.Fatalf("unexpected request: %+v", req)
				}
				return reviewagg.Response{
					OK: true,
					Data: &reviewagg.ResponseData{
						ReviewFindings: []agentrun.Finding{
							{
								ReviewerID:         "reviewer-correctness",
								Lens:               "correctness",
								Severity:           "high",
								Confidence:         "high",
								Category:           "correctness",
								FileRefs:           []string{"internal/reviewagg/service.go"},
								Summary:            "blocking review finding",
								Evidence:           "same issue reproduced by multiple reviewers",
								FindingFingerprint: "review:blocking",
								SuggestedAction:    "fix the blocker",
							},
						},
						Decision:           reviewagg.DecisionRequestChanges,
						Summary:            "decision=request_changes; review_findings=1; blocking finding present",
						HasBlockingFinding: true,
					},
				}
			},
		}
	}

	var stdout bytes.Buffer
	err := runReviewResultTool(
		context.Background(),
		nil,
		strings.NewReader(`{"task_id":"Sprint-04/Task-03","review_result":{"reviewer_id":"reviewer-correctness","lens":"correctness","status":"request_changes","findings":[{"reviewer_id":"reviewer-correctness","lens":"correctness","severity":"high","confidence":"medium","category":"correctness","file_refs":["internal/reviewagg/service.go"],"summary":"blocking review finding","evidence":"same issue reproduced by the reviewer","finding_fingerprint":"review:blocking","suggested_action":"fix the blocker"}]}}`),
		&stdout,
	)
	if err != nil {
		t.Fatalf("run review-result-tool: %v", err)
	}

	var response reviewagg.Response
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.OK || response.Data == nil {
		t.Fatalf("unexpected response: %s", stdout.String())
	}
	if response.Data.Decision != reviewagg.DecisionRequestChanges || !response.Data.HasBlockingFinding {
		t.Fatalf("unexpected review result payload: %+v", response.Data)
	}
}

func TestRunReviewResultToolPassUsesEmptyReviewFindingsArray(t *testing.T) {
	var stdout bytes.Buffer
	err := runReviewResultTool(
		context.Background(),
		nil,
		strings.NewReader(`{"task_id":"Sprint-04/Task-03","review_result":{"reviewer_id":"reviewer-correctness","lens":"correctness","status":"pass"}}`),
		&stdout,
	)
	if err != nil {
		t.Fatalf("run review-result-tool: %v", err)
	}
	var response reviewagg.Response
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.OK || response.Data == nil {
		t.Fatalf("unexpected response: %s", stdout.String())
	}
	if response.Data.ReviewFindings == nil || len(response.Data.ReviewFindings) != 0 {
		t.Fatalf("expected empty review_findings slice, got %+v", response.Data.ReviewFindings)
	}
}

func TestRunReviewResultToolInvalidRequestReturnsExitCodeTwo(t *testing.T) {
	testCases := []struct {
		name  string
		args  []string
		stdin string
	}{
		{
			name:  "invalid review status",
			stdin: `{"task_id":"Sprint-04/Task-03","review_result":{"reviewer_id":"reviewer-a","lens":"correctness","status":"ship_it"}}`,
		},
		{
			name:  "mismatched sprint id",
			stdin: `{"task_id":"Sprint-04/Task-03","sprint_id":"Sprint-99","review_result":{"reviewer_id":"reviewer-a","lens":"correctness","status":"pass"}}`,
		},
		{
			name:  "pass with findings",
			stdin: `{"task_id":"Sprint-04/Task-03","review_result":{"reviewer_id":"reviewer-a","lens":"correctness","status":"pass","findings":[{"reviewer_id":"reviewer-a","lens":"correctness","severity":"low","confidence":"high","category":"correctness","file_refs":["internal/reviewagg/service.go"],"summary":"unexpected finding","evidence":"review still found an issue","finding_fingerprint":"review:pass-with-finding","suggested_action":"remove contradiction"}]}}`,
		},
		{
			name:  "empty stdin",
			stdin: "",
		},
		{
			name:  "unexpected positional arg",
			args:  []string{"extra"},
			stdin: `{"task_id":"Sprint-04/Task-03","review_result":{"reviewer_id":"reviewer-a","lens":"correctness","status":"pass"}}`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			err := runReviewResultTool(context.Background(), tc.args, strings.NewReader(tc.stdin), &stdout)
			if err == nil {
				t.Fatal("expected cli exit error")
			}

			var exitErr *cliExitError
			if !errors.As(err, &exitErr) || exitErr.code != 2 {
				t.Fatalf("unexpected error type: %v", err)
			}

			var response reviewagg.Response
			if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if response.OK || response.Error == nil || response.Error.Code != reviewagg.ErrorCodeInvalid {
				t.Fatalf("unexpected invalid response: %s", stdout.String())
			}
		})
	}
}

func TestPrepareWorktreeToolOutputsJSONResponse(t *testing.T) {
	root := newServeTestRepo(t)

	orig := newPrepareWorktreeExecutor
	t.Cleanup(func() { newPrepareWorktreeExecutor = orig })

	newPrepareWorktreeExecutor = func() prepareWorktreeExecutor {
		return fakePrepareWorktreeExecutor{
			execute: func(_ context.Context, req worktreeprep.Request, opts worktreeprep.ExecuteOptions) worktreeprep.Response {
				if req.SprintID != "Sprint-03" {
					t.Fatalf("unexpected sprint id: %s", req.SprintID)
				}
				if req.TaskID != "Sprint-03/Task-03" {
					t.Fatalf("unexpected task id: %s", req.TaskID)
				}
				if req.SprintBranch != "sprint/Sprint-03" {
					t.Fatalf("unexpected sprint branch: %s", req.SprintBranch)
				}
				if req.TaskBranch != "task/Sprint-03/Task-03" {
					t.Fatalf("unexpected task branch: %s", req.TaskBranch)
				}
				if req.WorktreeRoot != "/tmp/worktrees" {
					t.Fatalf("unexpected worktree root: %s", req.WorktreeRoot)
				}
				if opts.DefaultBranch != "main" {
					t.Fatalf("unexpected default branch: %s", opts.DefaultBranch)
				}
				if opts.WorkDir != root {
					t.Fatalf("unexpected workdir: %s", opts.WorkDir)
				}
				if opts.Remote != "upstream" {
					t.Fatalf("unexpected remote: %s", opts.Remote)
				}

				return worktreeprep.Response{
					OK: true,
					Data: &worktreeprep.ResponseData{
						WorktreePath:  "/tmp/worktrees/Sprint-03/Task-03",
						TaskBranch:    req.TaskBranch,
						BaseBranch:    req.SprintBranch,
						BaseCommitSHA: "abc123",
						Reused:        true,
					},
				}
			},
		}
	}

	var stdout bytes.Buffer
	if err := run(context.Background(), []string{
		"prepare-worktree-tool",
		"--workdir", root,
		"--sprint-id", "Sprint-03",
		"--task-id", "Sprint-03/Task-03",
		"--worktree-root", "/tmp/worktrees",
		"--remote", "upstream",
	}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run prepare-worktree-tool: %v", err)
	}

	var response worktreeprep.Response
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.OK || response.Data == nil {
		t.Fatalf("unexpected response: %s", stdout.String())
	}
	if response.Data.WorktreePath != "/tmp/worktrees/Sprint-03/Task-03" {
		t.Fatalf("unexpected worktree path: %s", response.Data.WorktreePath)
	}
	if response.Data.BaseCommitSHA != "abc123" {
		t.Fatalf("unexpected base commit sha: %s", response.Data.BaseCommitSHA)
	}
}

func TestServeBootstrapsApplication(t *testing.T) {
	root := newServeTestRepo(t)
	t.Setenv(sharedconfig.ConfigFileEnv, filepath.Join(root, sharedconfig.DefaultFile))

	orig := runServeApplication
	t.Cleanup(func() { runServeApplication = orig })

	var handler http.Handler
	var expectedComponents []string
	runServeApplication = func(ctx context.Context, application *app.Application) error {
		if err := application.Bootstrap(ctx); err != nil {
			return err
		}
		expectedComponents = application.ComponentNames()
		var err error
		handler, err = application.HTTPHandler()
		return err
	}

	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"serve"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run serve: %v", err)
	}

	if handler == nil {
		t.Fatal("expected serve to configure an HTTP handler")
	}

	req := httptest.NewRequest(http.MethodPost, "/github/webhook", strings.NewReader(`{}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected webhook handler to be mounted, got status %d", recorder.Code)
	}

	payload := findServeLogEntry(t, stdout.String(), "toolhub bootstrapped")
	components, ok := payload["components"].([]any)
	if !ok {
		t.Fatalf("missing components: %#v", payload["components"])
	}
	if len(components) != len(expectedComponents) {
		t.Fatalf("unexpected component count: got %d want %d", len(components), len(expectedComponents))
	}
	for i, want := range expectedComponents {
		got, ok := components[i].(string)
		if !ok {
			t.Fatalf("component %d is not a string: %#v", i, components[i])
		}
		if got != want {
			t.Fatalf("unexpected component at index %d: got %s want %s", i, got, want)
		}
	}

	if _, err := os.Stat(filepath.Join(root, ".toolhub", "toolhub.db")); err != nil {
		t.Fatalf("stat bootstrapped database: %v", err)
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

func newServeTestRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeServeTestFile(t, root, sharedconfig.DefaultFile, validServeConfigYAML())
	writeServeTestFile(t, root, filepath.Join("sql", "schema.sql"), repoSchemaForServeTest(t))
	return root
}

func validServeConfigYAML() string {
	return strings.TrimSpace(`
repo:
  github_owner: example-owner
  github_repo: quick-ai-toolhub
  default_branch: main

database:
  path: .toolhub/toolhub.db

server:
  listen_addr: 127.0.0.1:0

default_model: gpt-5.3-codex-spark

agents:
  developer:
    template_file: prompts/agents/developer.md
  qa:
    template_file: prompts/agents/qa.md
  reviewer:
    template_file: prompts/agents/reviewer.md
`) + "\n"
}

func repoSchemaForServeTest(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}

	schemaPath := filepath.Join(filepath.Dir(file), "..", "..", "sql", "schema.sql")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read repo schema: %v", err)
	}
	return string(data)
}

func writeServeTestFile(t *testing.T, root, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func findServeLogEntry(t *testing.T, output, msg string) map[string]any {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("unmarshal serve output: %v\noutput=%s", err, output)
		}
		if entry["msg"] == msg {
			return entry
		}
	}

	t.Fatalf("missing %q log entry in output:\n%s", msg, output)
	return nil
}
