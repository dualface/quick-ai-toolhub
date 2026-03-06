package agentrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quick-ai-toolhub/internal/issuesync"
)

type fakeCommandRunner struct {
	requests []CommandRequest
	run      func(context.Context, CommandRequest) (CommandResult, error)
}

func (f *fakeCommandRunner) Run(ctx context.Context, req CommandRequest) (CommandResult, error) {
	f.requests = append(f.requests, req)
	return f.run(ctx, req)
}

func TestRunTaskCodexExec(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
			args := strings.Join(req.Args, " ")
			if !strings.Contains(args, "codex --ask-for-approval never --sandbox workspace-write --model gpt-5-codex exec") {
				t.Fatalf("unexpected codex args: %s", args)
			}
			if !strings.Contains(string(req.Stdin), "Finish Sprint-04/Task-01 in scope.") {
				t.Fatalf("expected developer template content in prompt, got:\n%s", string(req.Stdin))
			}

			lastMessagePath := findFlagValue(req.Args, "-o")
			if lastMessagePath == "" {
				t.Fatal("missing last message path")
			}
			for _, key := range []string{"TMPDIR", "TMP", "TEMP", "GOTMPDIR", "GOCACHE"} {
				if !envContainsKey(req.Env, key) {
					t.Fatalf("missing %s in command env", key)
				}
			}

			payload := `{"status":"success","summary":"implemented","next_action":"proceed","failure_fingerprint":null,"artifact_refs":null,"findings":null}`
			if err := os.WriteFile(lastMessagePath, []byte(payload), 0o644); err != nil {
				t.Fatalf("write last message: %v", err)
			}

			return CommandResult{
				Stdout: []byte(`{"session_id":"123e4567-e89b-12d3-a456-426614174000"}` + "\n"),
			}, nil
		},
	}

	executor := NewExecutor(runner)
	executor.now = func() time.Time {
		return time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC)
	}
	executor.runID = func() string { return "runid123" }

	result, err := executor.RunTask(context.Background(), RunOptions{
		TaskID:    "Sprint-04/Task-01",
		AgentType: AgentDeveloper,
		PlanFile:  filepath.Join(repo, "plan/SPRINTS-V1.md"),
		TasksDir:  filepath.Join(repo, "plan/tasks"),
		WorkDir:   repo,
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}

	if result.Runner != RunnerCodexExec {
		t.Fatalf("unexpected runner: %s", result.Runner)
	}
	if result.Status != "success" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	if result.SessionID == "" {
		t.Fatal("expected session id")
	}
	if result.ArtifactRefs.Report == "" || result.ArtifactRefs.Log == "" {
		t.Fatalf("missing artifact refs: %+v", result.ArtifactRefs)
	}
	if result.ArtifactRefs.Worktree != "." {
		t.Fatalf("unexpected worktree ref: %s", result.ArtifactRefs.Worktree)
	}
	if want := ".toolhub/runs/Sprint-04/Task-01/developer/attempt-01/default/20260306T120000.000000000Z-runid123/result.json"; result.ArtifactRefs.Report != want {
		t.Fatalf("unexpected report ref: %s", result.ArtifactRefs.Report)
	}
}

func TestRunTaskExplicitModelOverridesConfig(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
			args := strings.Join(req.Args, " ")
			if !strings.Contains(args, "--model gpt-5-codex-override") {
				t.Fatalf("expected explicit model override, got %s", args)
			}
			lastMessagePath := findFlagValue(req.Args, "-o")
			if err := os.WriteFile(lastMessagePath, []byte(`{"status":"success","summary":"ok","next_action":"done","failure_fingerprint":null,"artifact_refs":null,"findings":null}`), 0o644); err != nil {
				t.Fatalf("write last message: %v", err)
			}
			return CommandResult{}, nil
		},
	}

	executor := NewExecutor(runner)
	_, err := executor.RunTask(context.Background(), RunOptions{
		TaskID:    "Sprint-04/Task-01",
		AgentType: AgentDeveloper,
		PlanFile:  filepath.Join(repo, "plan/SPRINTS-V1.md"),
		TasksDir:  filepath.Join(repo, "plan/tasks"),
		WorkDir:   repo,
		Model:     "gpt-5-codex-override",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
}

func TestRunTaskYoloBypassesSandbox(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
			args := strings.Join(req.Args, " ")
			if !strings.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
				t.Fatalf("expected yolo flag, got %s", args)
			}
			if strings.Contains(args, "--ask-for-approval") {
				t.Fatalf("did not expect ask-for-approval flag in yolo mode, got %s", args)
			}
			if strings.Contains(args, "--sandbox") {
				t.Fatalf("did not expect sandbox flag in yolo mode, got %s", args)
			}
			lastMessagePath := findFlagValue(req.Args, "-o")
			if err := os.WriteFile(lastMessagePath, []byte(`{"status":"success","summary":"ok","next_action":"done","failure_fingerprint":null,"artifact_refs":null,"findings":null}`), 0o644); err != nil {
				t.Fatalf("write last message: %v", err)
			}
			return CommandResult{}, nil
		},
	}

	executor := NewExecutor(runner)
	_, err := executor.RunTask(context.Background(), RunOptions{
		TaskID:    "Sprint-04/Task-01",
		AgentType: AgentDeveloper,
		PlanFile:  filepath.Join(repo, "plan/SPRINTS-V1.md"),
		TasksDir:  filepath.Join(repo, "plan/tasks"),
		WorkDir:   repo,
		Yolo:      true,
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
}

func TestRunTaskReviewerUsesReadOnlySandbox(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
			args := strings.Join(req.Args, " ")
			if !strings.Contains(args, "--sandbox read-only") {
				t.Fatalf("expected read-only sandbox, got %s", args)
			}
			expectedAddDir := filepath.Join(os.TempDir(), "toolhub-codex", "Sprint-04", "Task-01", "reviewer", "attempt-01", "default", "20260306T120001.000000000Z-runid123")
			if !strings.Contains(args, "--add-dir "+expectedAddDir) {
				t.Fatalf("expected add-dir for reviewer runner output, got %s", args)
			}
			lastMessagePath := findFlagValue(req.Args, "-o")
			if err := os.WriteFile(lastMessagePath, []byte(`{"status":"success","summary":"ok","next_action":"done","failure_fingerprint":null,"artifact_refs":null,"findings":null}`), 0o644); err != nil {
				t.Fatalf("write last message: %v", err)
			}
			return CommandResult{}, nil
		},
	}

	executor := NewExecutor(runner)
	executor.now = func() time.Time {
		return time.Date(2026, 3, 6, 12, 0, 1, 0, time.UTC)
	}
	executor.runID = func() string { return "runid123" }
	_, err := executor.RunTask(context.Background(), RunOptions{
		TaskID:    "Sprint-04/Task-01",
		AgentType: AgentReviewer,
		PlanFile:  filepath.Join(repo, "plan/SPRINTS-V1.md"),
		TasksDir:  filepath.Join(repo, "plan/tasks"),
		WorkDir:   repo,
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
}

func TestRunTaskRunnerFailureReturnsStructuredResult(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(_ context.Context, _ CommandRequest) (CommandResult, error) {
			return CommandResult{
				Stdout: []byte(`{"session_id":"123e4567-e89b-12d3-a456-426614174000"}` + "\n"),
				Stderr: []byte("runner failed\n"),
			}, errors.New("exit status 1")
		},
	}

	executor := NewExecutor(runner)
	executor.now = func() time.Time {
		return time.Date(2026, 3, 6, 12, 1, 0, 0, time.UTC)
	}
	executor.runID = func() string { return "runid123" }

	result, err := executor.RunTask(context.Background(), RunOptions{
		TaskID:    "Sprint-04/Task-01",
		AgentType: AgentDeveloper,
		PlanFile:  filepath.Join(repo, "plan/SPRINTS-V1.md"),
		TasksDir:  filepath.Join(repo, "plan/tasks"),
		WorkDir:   repo,
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}

	if result.Status != "failed" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	if result.NextAction != "retry" {
		t.Fatalf("unexpected next action: %s", result.NextAction)
	}
	if result.FailureFingerprint != ErrorCodeMalformedOutput {
		t.Fatalf("unexpected failure fingerprint: %s", result.FailureFingerprint)
	}
	if result.ArtifactRefs.Log == "" || result.ArtifactRefs.Report == "" {
		t.Fatalf("missing artifact refs: %+v", result.ArtifactRefs)
	}
	if result.SessionID == "" {
		t.Fatal("expected session id")
	}

	reportPath := filepath.Join(repo, filepath.FromSlash(result.ArtifactRefs.Report))
	reportBytes, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(reportBytes), `"failure_fingerprint": "malformed_output"`) {
		t.Fatalf("report missing failure fingerprint: %s", string(reportBytes))
	}
}

func TestRunTaskRunnerFailureWithMalformedPayloadReturnsMalformedOutput(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
			lastMessagePath := findFlagValue(req.Args, "-o")
			if err := os.WriteFile(lastMessagePath, []byte(`{"status":"success","summary":"ok","next_action":"done"}`), 0o644); err != nil {
				t.Fatalf("write last message: %v", err)
			}
			return CommandResult{
				Stdout: []byte(`{"session_id":"123e4567-e89b-12d3-a456-426614174000"}` + "\n"),
			}, errors.New("exit status 1")
		},
	}

	executor := NewExecutor(runner)
	executor.now = func() time.Time {
		return time.Date(2026, 3, 6, 12, 1, 30, 0, time.UTC)
	}
	executor.runID = func() string { return "runid123" }

	result, err := executor.RunTask(context.Background(), RunOptions{
		TaskID:    "Sprint-04/Task-01",
		AgentType: AgentDeveloper,
		PlanFile:  filepath.Join(repo, "plan/SPRINTS-V1.md"),
		TasksDir:  filepath.Join(repo, "plan/tasks"),
		WorkDir:   repo,
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}

	if result.FailureFingerprint != ErrorCodeMalformedOutput {
		t.Fatalf("unexpected failure fingerprint: %s", result.FailureFingerprint)
	}
}

func TestRunTaskTimeoutReturnsStructuredResult(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(ctx context.Context, _ CommandRequest) (CommandResult, error) {
			<-ctx.Done()
			return CommandResult{}, ctx.Err()
		},
	}

	executor := NewExecutor(runner)
	executor.now = func() time.Time {
		return time.Date(2026, 3, 6, 12, 2, 0, 0, time.UTC)
	}
	executor.runID = func() string { return "runid123" }

	result, err := executor.RunTask(context.Background(), RunOptions{
		TaskID:    "Sprint-04/Task-01",
		AgentType: AgentDeveloper,
		PlanFile:  filepath.Join(repo, "plan/SPRINTS-V1.md"),
		TasksDir:  filepath.Join(repo, "plan/tasks"),
		WorkDir:   repo,
		Timeout:   10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}

	if result.Status != "failed" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	if result.FailureFingerprint != ErrorCodeRunnerTimeout {
		t.Fatalf("unexpected failure fingerprint: %s", result.FailureFingerprint)
	}
	if result.ArtifactRefs.Log == "" || result.ArtifactRefs.Report == "" {
		t.Fatalf("missing artifact refs: %+v", result.ArtifactRefs)
	}
}

func TestRunTaskPreservesAgentReportArtifact(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
			lastMessagePath := findFlagValue(req.Args, "-o")
			payload := `{"status":"success","summary":"implemented","next_action":"proceed","failure_fingerprint":null,"artifact_refs":{"log":null,"worktree":null,"patch":null,"report":"agent/report.md"},"findings":null}`
			if err := os.WriteFile(lastMessagePath, []byte(payload), 0o644); err != nil {
				t.Fatalf("write last message: %v", err)
			}
			return CommandResult{}, nil
		},
	}

	executor := NewExecutor(runner)
	executor.now = func() time.Time {
		return time.Date(2026, 3, 6, 12, 3, 0, 0, time.UTC)
	}
	executor.runID = func() string { return "runid123" }

	result, err := executor.RunTask(context.Background(), RunOptions{
		TaskID:    "Sprint-04/Task-01",
		AgentType: AgentDeveloper,
		PlanFile:  filepath.Join(repo, "plan/SPRINTS-V1.md"),
		TasksDir:  filepath.Join(repo, "plan/tasks"),
		WorkDir:   repo,
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}

	if result.ArtifactRefs.Report != "agent/report.md" {
		t.Fatalf("unexpected report ref: %s", result.ArtifactRefs.Report)
	}

	localReportPath := filepath.Join(repo, ".toolhub/runs/Sprint-04/Task-01/developer/attempt-01/default/20260306T120300.000000000Z-runid123/result.json")
	if _, err := os.Stat(localReportPath); err != nil {
		t.Fatalf("expected local result report: %v", err)
	}
}

func TestRunTaskRejectsPartialArtifactRefsPayload(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
			lastMessagePath := findFlagValue(req.Args, "-o")
			payload := `{"status":"success","summary":"implemented","next_action":"proceed","failure_fingerprint":null,"artifact_refs":{"report":"agent/report.md"},"findings":null}`
			if err := os.WriteFile(lastMessagePath, []byte(payload), 0o644); err != nil {
				t.Fatalf("write last message: %v", err)
			}
			return CommandResult{}, nil
		},
	}

	executor := NewExecutor(runner)
	executor.now = func() time.Time {
		return time.Date(2026, 3, 6, 12, 4, 0, 0, time.UTC)
	}
	executor.runID = func() string { return "runid123" }

	result, err := executor.RunTask(context.Background(), RunOptions{
		TaskID:    "Sprint-04/Task-01",
		AgentType: AgentDeveloper,
		PlanFile:  filepath.Join(repo, "plan/SPRINTS-V1.md"),
		TasksDir:  filepath.Join(repo, "plan/tasks"),
		WorkDir:   repo,
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}

	if result.Status != "failed" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	if result.FailureFingerprint != ErrorCodeMalformedOutput {
		t.Fatalf("unexpected failure fingerprint: %s", result.FailureFingerprint)
	}
}

func TestBuildCommandAddsWritableDirOutsideWorktree(t *testing.T) {
	req, err := buildCommand(
		RunOptions{WorkDir: "/repo", AgentType: AgentDeveloper},
		"prompt",
		"/tmp/toolhub/output-schema.json",
		"/tmp/toolhub/last-message.json",
	)
	if err != nil {
		t.Fatalf("build command: %v", err)
	}

	args := strings.Join(req.Args, " ")
	if !strings.Contains(args, "--add-dir /tmp/toolhub") {
		t.Fatalf("expected add-dir for external output root, got %s", args)
	}
}

func TestBuildCommandSkipsWritableDirInsideWorktree(t *testing.T) {
	req, err := buildCommand(
		RunOptions{WorkDir: "/repo", AgentType: AgentDeveloper},
		"prompt",
		"/repo/.toolhub/output-schema.json",
		"/repo/.toolhub/last-message.json",
	)
	if err != nil {
		t.Fatalf("build command: %v", err)
	}

	args := strings.Join(req.Args, " ")
	if strings.Contains(args, "--add-dir") {
		t.Fatalf("did not expect add-dir for in-worktree output root, got %s", args)
	}
}

func TestBuildCommandEnvDeveloperSetsRepoLocalRuntimeDirs(t *testing.T) {
	repo := t.TempDir()

	env, err := buildCommandEnv(repo, AgentDeveloper, false)
	if err != nil {
		t.Fatalf("build command env: %v", err)
	}

	want := map[string]string{
		"TMPDIR":   filepath.Join(repo, ".toolhub", "runtime", "tmp"),
		"TMP":      filepath.Join(repo, ".toolhub", "runtime", "tmp"),
		"TEMP":     filepath.Join(repo, ".toolhub", "runtime", "tmp"),
		"GOTMPDIR": filepath.Join(repo, ".toolhub", "runtime", "go-build"),
		"GOCACHE":  filepath.Join(repo, ".toolhub", "runtime", "go-cache"),
	}
	for key, expected := range want {
		if got := envValue(env, key); got != expected {
			t.Fatalf("unexpected %s: got %q want %q", key, got, expected)
		}
	}
	if got := envValue(env, "HOME"); got == filepath.Join(repo, ".toolhub", "runtime", "home") {
		t.Fatalf("did not expect HOME override by default, got %q", got)
	}
}

func TestBuildCommandEnvIsolatedCodexHomeOverridesHome(t *testing.T) {
	repo := t.TempDir()

	env, err := buildCommandEnv(repo, AgentDeveloper, true)
	if err != nil {
		t.Fatalf("build command env: %v", err)
	}

	wantHome := filepath.Join(repo, ".toolhub", "runtime", "home")
	if got := envValue(env, "HOME"); got != wantHome {
		t.Fatalf("unexpected HOME: got %q want %q", got, wantHome)
	}
	if _, err := os.Stat(wantHome); err != nil {
		t.Fatalf("expected isolated HOME dir to exist: %v", err)
	}
}

func TestCommandEnvKeysIncludeHomeOnlyForIsolatedDeveloperRuns(t *testing.T) {
	keys := commandEnvKeys(RunOptions{AgentType: AgentDeveloper})
	if strings.Join(keys, ",") != "TMPDIR,TMP,TEMP,GOTMPDIR,GOCACHE" {
		t.Fatalf("unexpected default env keys: %v", keys)
	}

	keys = commandEnvKeys(RunOptions{AgentType: AgentDeveloper, IsolatedCodexHome: true})
	if strings.Join(keys, ",") != "TMPDIR,TMP,TEMP,GOTMPDIR,GOCACHE,HOME" {
		t.Fatalf("unexpected isolated env keys: %v", keys)
	}

	if keys := commandEnvKeys(RunOptions{AgentType: AgentReviewer}); len(keys) != 0 {
		t.Fatalf("expected reviewer env keys to be empty, got %v", keys)
	}
}

func TestRunnerFailureSummaryIncludesCodexRuntimeHint(t *testing.T) {
	stderr := []byte("WARNING: proceeding, even though we could not update PATH: Permission denied (os error 13) at path \"/home/work/.codex/tmp/arg0/codex-arg0abcd\"")
	got := runnerFailureSummary(stderr)
	if !strings.Contains(got, "~/.codex/tmp/arg0") {
		t.Fatalf("expected runtime hint in summary, got %q", got)
	}
}

func TestValidateOptionsRejectsIsolatedCodexHomeForReviewer(t *testing.T) {
	err := validateOptions(&RunOptions{
		TaskID:            "Sprint-04/Task-01",
		AgentType:         AgentReviewer,
		IsolatedCodexHome: true,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	toolErr, ok := err.(*ToolError)
	if !ok {
		t.Fatalf("expected tool error, got %T", err)
	}
	if toolErr.Code != ErrorCodeInvalidRequest {
		t.Fatalf("unexpected error code: %s", toolErr.Code)
	}
}

func TestBuildRunDirIncludesAgentAttemptLensAndRunID(t *testing.T) {
	got := buildRunDir("/artifacts", "Sprint-04/Task-01", AgentReviewer, "qa review", 2, time.Date(2026, 3, 6, 12, 4, 5, 123, time.UTC), "abcd1234")
	want := filepath.Join("/artifacts", "Sprint-04", "Task-01", "reviewer", "attempt-02", "qa_review", "20260306T120405.000000123Z-abcd1234")
	if got != want {
		t.Fatalf("unexpected run dir: %s", got)
	}
}

func TestStartProgressHeartbeatWritesSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var progress bytes.Buffer
	stop := startProgressHeartbeat(ctx, &progress, 10*time.Millisecond)
	time.Sleep(25 * time.Millisecond)
	stop()

	if !strings.Contains(progress.String(), "[progress] still running (") {
		t.Fatalf("unexpected heartbeat output: %q", progress.String())
	}
	if !strings.Contains(progress.String(), "[progress] still running (21ms)") &&
		!strings.Contains(progress.String(), "[progress] still running (20ms)") &&
		!strings.Contains(progress.String(), "[progress] still running (19ms)") {
		t.Fatalf("expected elapsed time to advance beyond the first tick: %q", progress.String())
	}
}

func TestTryDecodePayloadBytesFindsNestedPayload(t *testing.T) {
	raw := []byte(`{"event":"done","content":"{\"status\":\"success\",\"summary\":\"done\",\"next_action\":\"proceed\",\"failure_fingerprint\":null,\"artifact_refs\":null,\"findings\":null}"}`)
	payload, ok := tryDecodePayloadBytes(raw)
	if !ok {
		t.Fatal("expected payload")
	}
	if payload.Summary != "done" {
		t.Fatalf("unexpected summary: %s", payload.Summary)
	}
}

func TestResultJSONIncludesRequiredContractKeys(t *testing.T) {
	result := Result{
		Runner:     RunnerCodexExec,
		Status:     "success",
		Summary:    "ok",
		NextAction: "proceed",
		ArtifactRefs: ArtifactRefs{
			Log:      "a",
			Worktree: "b",
		},
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	for _, key := range []string{"failure_fingerprint", "artifact_refs", "findings"} {
		if _, ok := value[key]; !ok {
			t.Fatalf("missing key %q in result json: %s", key, string(raw))
		}
	}

	artifactRefs, ok := value["artifact_refs"].(map[string]any)
	if !ok {
		t.Fatalf("artifact_refs is not an object: %T", value["artifact_refs"])
	}
	for _, key := range []string{"log", "worktree", "patch", "report"} {
		if _, ok := artifactRefs[key]; !ok {
			t.Fatalf("missing artifact_refs key %q in result json: %s", key, string(raw))
		}
	}
}

func setupTestRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "PROJECT-DEVELOPER-GUIDE.md"), "# Guide\n")
	mustWriteFile(t, filepath.Join(root, "README.md"), "# README\n")
	mustWriteFile(t, filepath.Join(root, "TECH-V1.md"), "# TECH\n")
	mustWriteFile(t, filepath.Join(root, "TOOLS-V1.md"), "# TOOLS\n")
	mustWriteFile(t, filepath.Join(root, "AGENT-CLI-V1.md"), "# AGENT\n")
	mustWriteFile(t, filepath.Join(root, "config/config.yaml"), strings.TrimSpace(`
repo:
  github_owner: example-owner
  github_repo: quick-ai-toolhub
  default_branch: main

database:
  path: .toolhub/toolhub.db

server:
  listen_addr: 127.0.0.1:8080

default_model: gpt-5-codex

agents:
  developer:
    template_file: prompts/agents/developer.md
  qa:
    template_file: prompts/agents/qa.md
  reviewer:
    template_file: prompts/agents/reviewer.md
`)+"\n")
	mustWriteFile(t, filepath.Join(root, "prompts/agents/developer.md"), strings.TrimSpace(`
- Implement the task end-to-end within scope.
- Run the smallest relevant validation before finishing.
- Finish {{.TaskID}} in scope.
`)+"\n")
	mustWriteFile(t, filepath.Join(root, "prompts/agents/qa.md"), strings.TrimSpace(`
- Validate the current implementation.
- Focus on build, test, and lint behavior.
- Use the provided repo-local temp/cache environment for Go commands instead of relying on /tmp defaults.
- Prefer repository-defined validation commands; do not block solely because a global lint tool is absent unless the repository explicitly requires it.
- If environment limits prevent a check from running, report that as a verification gap, not as a code defect.
- Do not make unrelated code changes.
`)+"\n")
	mustWriteFile(t, filepath.Join(root, "prompts/agents/reviewer.md"), strings.TrimSpace(`
- Review the current state and report findings.
- Do not modify files.
`)+"\n")
	mustWriteFile(t, filepath.Join(root, "plan/SPRINTS-V1.md"), strings.TrimSpace(`
## [Sprint-04] Task Execution

### Goal

Build the task execution loop.

### Done When

- run-agent-tool is available.

### Tasks

| task_id | title |
| --- | --- |
| Task-01 | 实现 run-agent-tool |
`)+"\n")
	mustWriteFile(t, filepath.Join(root, "plan/tasks/Sprint-04/Task-01.md"), strings.TrimSpace(`
# [Sprint-04][Task-01] 实现 run-agent-tool

## Goal

Build the runner.

## Reads

- PROJECT-DEVELOPER-GUIDE.md
- README.md
- TECH-V1.md
- TOOLS-V1.md
- AGENT-CLI-V1.md

## Dependencies

- Sprint-03/Task-04

## In Scope

- Define the runner interface

## Out of Scope

- PR logic

## Deliverables

- run-agent-tool implementation

## Acceptance Criteria

- Works

## Notes

- Keep it testable
`)+"\n")
	return root
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func findFlagValue(args []string, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func TestResultSchemaJSONIsValidJSON(t *testing.T) {
	schema, err := resultSchemaJSON()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal(schema, &value); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	required := value["required"].([]any)
	if len(required) != 6 {
		t.Fatalf("unexpected root required count: %d", len(required))
	}

	props := value["properties"].(map[string]any)
	artifactRefs := props["artifact_refs"].(map[string]any)
	artifactAnyOf := artifactRefs["anyOf"].([]any)
	artifactObject := artifactAnyOf[1].(map[string]any)
	artifactRequired := artifactObject["required"].([]any)
	if len(artifactRequired) != 4 {
		t.Fatalf("unexpected artifact_refs required count: %d", len(artifactRequired))
	}
}

func TestFormatCommandFailureIncludesStdout(t *testing.T) {
	got := formatCommandFailure("schema error", "warning")
	if !strings.Contains(got, "stdout: schema error") {
		t.Fatalf("missing stdout: %s", got)
	}
	if !strings.Contains(got, "stderr: warning") {
		t.Fatalf("missing stderr: %s", got)
	}
}

func TestBuildPromptPreservesInlineCodeAndUsesRelativeTaskSource(t *testing.T) {
	task := &issuesync.TaskBrief{
		TaskID:             "Sprint-04/Task-01",
		Goal:               "Goal",
		Reads:              []string{"`TECH-V1.md`"},
		InScope:            []string{"收集结构化结果和 `artifact_refs`"},
		AcceptanceCriteria: []string{"默认 runner 为 `codex_exec`"},
		Source:             "/repo/plan/tasks/Sprint-04/Task-01.md",
	}
	sprint := &issuesync.Sprint{ID: "Sprint-04", Goal: "Sprint Goal"}

	contextRefs := ContextRefs{
		SprintID:     "Sprint-04",
		WorktreePath: "/repo",
	}
	prompt := buildPrompt(AgentDeveloper, task, sprint, 1, "delivery", contextRefs, "/repo", "")
	if !strings.Contains(prompt, "- plan/tasks/Sprint-04/Task-01.md") {
		t.Fatalf("expected relative task source, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "收集结构化结果和 `artifact_refs`") {
		t.Fatalf("expected inline code to be preserved, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "默认 runner 为 `codex_exec`") {
		t.Fatalf("expected inline code in acceptance criteria, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Lens: delivery") {
		t.Fatalf("expected lens in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "- sprint_id: Sprint-04") {
		t.Fatalf("expected sprint context in prompt, got:\n%s", prompt)
	}
}

func TestBuildPromptQAIncludesValidationRules(t *testing.T) {
	task := &issuesync.TaskBrief{
		TaskID: "Sprint-04/Task-01",
		Goal:   "Goal",
		Source: "/repo/plan/tasks/Sprint-04/Task-01.md",
	}
	sprint := &issuesync.Sprint{ID: "Sprint-04", Goal: "Sprint Goal"}
	prompt := buildPrompt(AgentQA, task, sprint, 1, "", ContextRefs{
		SprintID:     "Sprint-04",
		WorktreePath: "/repo",
	}, "/repo", "")

	for _, needle := range []string{
		"Use the provided repo-local temp/cache environment for Go commands instead of relying on /tmp defaults.",
		"Prefer repository-defined validation commands; do not block solely because a global lint tool is absent unless the repository explicitly requires it.",
		"If environment limits prevent a check from running, report that as a verification gap, not as a code defect.",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in prompt:\n%s", needle, prompt)
		}
	}
}

func TestRenderRoleTemplateUsesTaskAndSprintContext(t *testing.T) {
	rendered, err := renderRoleTemplate("- Work on {{.TaskID}} for {{.SprintID}}.", promptTemplateData{
		TaskID:   "Sprint-04/Task-01",
		SprintID: "Sprint-04",
	})
	if err != nil {
		t.Fatalf("render template: %v", err)
	}
	if rendered != "- Work on Sprint-04/Task-01 for Sprint-04." {
		t.Fatalf("unexpected rendered template: %q", rendered)
	}
}

func envContainsKey(env []string, key string) bool {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
