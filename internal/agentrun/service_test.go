package agentrun

import (
	"context"
	"encoding/json"
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
			if !strings.Contains(args, "codex --ask-for-approval never --sandbox workspace-write exec") {
				t.Fatalf("unexpected codex args: %s", args)
			}

			lastMessagePath := findFlagValue(req.Args, "-o")
			if lastMessagePath == "" {
				t.Fatal("missing last message path")
			}

			payload := `{"status":"success","summary":"implemented","next_action":"proceed"}`
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
}

func TestRunTaskReviewerUsesReadOnlySandbox(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
			args := strings.Join(req.Args, " ")
			if !strings.Contains(args, "--sandbox read-only") {
				t.Fatalf("expected read-only sandbox, got %s", args)
			}
			lastMessagePath := findFlagValue(req.Args, "-o")
			if err := os.WriteFile(lastMessagePath, []byte(`{"status":"success","summary":"ok","next_action":"done"}`), 0o644); err != nil {
				t.Fatalf("write last message: %v", err)
			}
			return CommandResult{}, nil
		},
	}

	executor := NewExecutor(runner)
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

func TestTryDecodePayloadBytesFindsNestedPayload(t *testing.T) {
	raw := []byte(`{"event":"done","content":"{\"status\":\"success\",\"summary\":\"done\",\"next_action\":\"proceed\"}"}`)
	payload, ok := tryDecodePayloadBytes(raw)
	if !ok {
		t.Fatal("expected payload")
	}
	if payload.Summary != "done" {
		t.Fatalf("unexpected summary: %s", payload.Summary)
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
	prompt := buildPrompt(AgentDeveloper, task, sprint, 1, "delivery", contextRefs, "/repo")
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
