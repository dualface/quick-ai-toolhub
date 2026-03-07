package agentrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
			for _, key := range []string{"TMPDIR", "TMP", "TEMP", "GOTMPDIR", "GOCACHE", "GOMODCACHE", "XDG_CACHE_HOME"} {
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

func TestRunTaskDeveloperUsesLatestUsableFeedbackArtifactsAsContext(t *testing.T) {
	repo := setupTestRepo(t)
	initGitRepo(t, repo)
	oldQAReport, oldQALog := writeHistoricalRunResult(t, repo, "Sprint-04/Task-01", AgentQA, 2, "default", "20260306T115900.000000000Z-oldqa", Result{
		Status:     "pass",
		Summary:    "older qa summary",
		NextAction: "none",
	})
	newQAReport, newQALog := writeHistoricalRunResult(t, repo, "Sprint-04/Task-01", AgentQA, 9, "default", "20260306T120000.000000000Z-newqa", Result{
		Status:     "completed_with_findings",
		Summary:    "latest usable qa summary",
		NextAction: "return_to_developer",
		Findings: []Finding{
			{
				ReviewerID:         "qa-agent",
				Lens:               "correctness",
				Severity:           "high",
				Confidence:         "high",
				Category:           "state_recovery",
				FileRefs:           []string{"internal/orchestrator/run.go"},
				Summary:            "qa finding summary",
				FindingFingerprint: "qa:finding",
				SuggestedAction:    "fix qa issue",
			},
		},
	})
	invalidQAReport, invalidQALog := writeHistoricalRunResult(t, repo, "Sprint-04/Task-01", AgentQA, 10, "default", "20260306T120500.000000000Z-invalidqa", Result{
		Status:             "failed",
		Summary:            "qa malformed output",
		NextAction:         "retry",
		FailureFingerprint: ErrorCodeMalformedOutput,
	})
	oldReviewerReport, oldReviewerLog := writeHistoricalRunResult(t, repo, "Sprint-04/Task-01", AgentReviewer, 1, "default", "20260306T115500.000000000Z-oldreview", Result{
		Status:     "pass",
		Summary:    "older reviewer summary",
		NextAction: "none",
	})
	newReviewerReport, newReviewerLog := writeHistoricalRunResult(t, repo, "Sprint-04/Task-01", AgentReviewer, 4, "default", "20260306T120100.000000000Z-newreview", Result{
		Status:     "completed_with_findings",
		Summary:    "latest usable reviewer summary",
		NextAction: "needs_fix",
		Findings: []Finding{
			{
				ReviewerID:         "reviewer-agent",
				Lens:               "architecture",
				Severity:           "medium",
				Confidence:         "high",
				Category:           "contract_drift",
				FileRefs:           []string{"internal/orchestrator/service.go"},
				Summary:            "reviewer finding summary",
				FindingFingerprint: "reviewer:finding",
				SuggestedAction:    "fix reviewer issue",
			},
		},
	})
	invalidReviewerReport, invalidReviewerLog := writeHistoricalRunResult(t, repo, "Sprint-04/Task-01", AgentReviewer, 5, "default", "20260306T120200.000000000Z-invalidreview", Result{
		Status:             "failed",
		Summary:            "reviewer execution error",
		NextAction:         "retry",
		FailureFingerprint: ErrorCodeRunnerExecution,
	})
	writeHistoricalRunResult(t, repo, "Sprint-04/Task-01", AgentDeveloper, 7, "default", "20260306T120300.000000000Z-devrun", Result{
		Status:     "completed",
		Summary:    "continued previous developer work",
		NextAction: "qa",
	})
	mustWriteFile(t, filepath.Join(repo, "README.md"), "# README updated\n")
	mustWriteFile(t, filepath.Join(repo, "internal/orchestrator/run.go"), "package orchestrator\n")

	runner := &fakeCommandRunner{
		run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
			prompt := string(req.Stdin)
			if !strings.Contains(prompt, "- latest_qa_artifact_refs:\n  - log: "+newQALog+"\n  - report: "+newQAReport) {
				t.Fatalf("expected latest QA report in prompt, got:\n%s", prompt)
			}
			if !strings.Contains(prompt, "- latest_reviewer_artifact_refs:\n  - log: "+newReviewerLog+"\n  - report: "+newReviewerReport) {
				t.Fatalf("expected latest reviewer report in prompt, got:\n%s", prompt)
			}
			for _, stale := range []string{oldQAReport, oldQALog, invalidQAReport, invalidQALog, oldReviewerReport, oldReviewerLog, invalidReviewerReport, invalidReviewerLog} {
				if strings.Contains(prompt, stale) {
					t.Fatalf("expected older feedback artifacts to be ignored, got:\n%s", prompt)
				}
			}
			for _, expected := range []string{
				"- latest_qa_feedback:\n  - attempt: 9\n  - status: completed_with_findings\n  - next_action: return_to_developer\n  - summary: latest usable qa summary\n  - findings:\n    - severity=high confidence=high lens=correctness category=state_recovery reviewer_id=qa-agent",
				"- latest_reviewer_feedback:\n  - attempt: 4\n  - status: completed_with_findings\n  - next_action: needs_fix\n  - summary: latest usable reviewer summary\n  - findings:\n    - severity=medium confidence=high lens=architecture category=contract_drift reviewer_id=reviewer-agent",
				"- previous_developer_context:\n  - attempt: 7\n  - status: completed\n  - next_action: qa\n  - summary: continued previous developer work\n  - changed_files:\n    - README.md\n    - internal/orchestrator/run.go",
				"If execution context includes latest_qa_feedback, read it first, then use latest_qa_artifact_refs for full detail before making changes.",
				"After the latest QA issues are addressed, read latest_reviewer_feedback, then use latest_reviewer_artifact_refs to fix the latest reviewer findings.",
				"If previous_developer_context is present, continue from that summary and changed file list instead of re-discovering the same work.",
				"After fixing the explicit findings, inspect adjacent branches in the same control flow, persistence path, and recovery path for similar defects.",
			} {
				if !strings.Contains(prompt, expected) {
					t.Fatalf("expected prompt to contain %q, got:\n%s", expected, prompt)
				}
			}
			if strings.Index(prompt, "latest_qa_artifact_refs") > strings.Index(prompt, "latest_reviewer_artifact_refs") {
				t.Fatalf("expected QA artifacts to appear before reviewer artifacts, got:\n%s", prompt)
			}
			if strings.Contains(prompt, ".toolhub/runs/") && strings.Contains(prompt, "changed_files:\n    - .toolhub/") {
				t.Fatalf("expected runtime artifacts to be excluded from changed file list, got:\n%s", prompt)
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
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
}

func TestRunTaskDeveloperKeepsExplicitArtifactContextAlongsideAutoFeedback(t *testing.T) {
	repo := setupTestRepo(t)
	autoQAReport, autoQALog := writeHistoricalRunArtifacts(t, repo, "Sprint-04/Task-01", AgentQA, 3, "default", "20260306T120000.000000000Z-qarun")
	autoReviewerReport, autoReviewerLog := writeHistoricalRunArtifacts(t, repo, "Sprint-04/Task-01", AgentReviewer, 2, "default", "20260306T120100.000000000Z-reviewrun")

	runner := &fakeCommandRunner{
		run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
			prompt := string(req.Stdin)
			if !strings.Contains(prompt, "- artifact_refs:") || !strings.Contains(prompt, "  - report: manual/report.json") {
				t.Fatalf("expected explicit report in prompt, got:\n%s", prompt)
			}
			for _, expected := range []string{autoQAReport, autoQALog, autoReviewerReport, autoReviewerLog} {
				if !strings.Contains(prompt, expected) {
					t.Fatalf("expected auto feedback artifacts to still be present when explicit context is provided, got:\n%s", prompt)
				}
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
		ContextRefs: ContextRefs{
			ArtifactRefs: ArtifactRefs{
				Report: "manual/report.json",
			},
		},
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

func TestRunTaskRunnerFailureWithValidPayloadReturnsRunnerFailure(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
			lastMessagePath := findFlagValue(req.Args, "-o")
			payload := `{"status":"success","summary":"ok","next_action":"done","failure_fingerprint":null,"artifact_refs":null,"findings":null}`
			if err := os.WriteFile(lastMessagePath, []byte(payload), 0o644); err != nil {
				t.Fatalf("write last message: %v", err)
			}
			return CommandResult{
				Stdout: []byte(`{"session_id":"123e4567-e89b-12d3-a456-426614174000"}` + "\n"),
				Stderr: []byte("runner failed\n"),
			}, errors.New("exit status 1")
		},
	}

	executor := NewExecutor(runner)
	executor.now = func() time.Time {
		return time.Date(2026, 3, 6, 12, 1, 15, 0, time.UTC)
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
	if result.FailureFingerprint != ErrorCodeRunnerExecution {
		t.Fatalf("unexpected failure fingerprint: %s", result.FailureFingerprint)
	}
	if result.Summary != "codex_exec failed before producing a structured result" {
		t.Fatalf("unexpected summary: %s", result.Summary)
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

func TestRunTaskRejectsUnknownStatusPayload(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
			lastMessagePath := findFlagValue(req.Args, "-o")
			payload := `{"status":"green","summary":"implemented","next_action":"proceed","failure_fingerprint":null,"artifact_refs":null,"findings":null}`
			if err := os.WriteFile(lastMessagePath, []byte(payload), 0o644); err != nil {
				t.Fatalf("write last message: %v", err)
			}
			return CommandResult{}, nil
		},
	}

	executor := NewExecutor(runner)
	executor.now = func() time.Time {
		return time.Date(2026, 3, 6, 12, 1, 45, 0, time.UTC)
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

	if result.Status != "timeout" {
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

func TestRunTaskRejectsInvalidReviewerFindingPayload(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload string
	}{
		{
			name:    "invalid lens",
			payload: `{"status":"completed_with_findings","summary":"review found issues","next_action":"needs_changes","failure_fingerprint":null,"artifact_refs":null,"findings":[{"reviewer_id":"reviewer-ops","lens":"ops","severity":"high","confidence":"high","category":"correctness","file_refs":["internal/orchestrator/run.go"],"summary":"invalid lens","evidence":"lens is outside the supported set","finding_fingerprint":"review:invalid-lens","suggested_action":"use a standard lens"}]}`,
		},
		{
			name:    "invalid severity",
			payload: `{"status":"completed_with_findings","summary":"review found issues","next_action":"needs_changes","failure_fingerprint":null,"artifact_refs":null,"findings":[{"reviewer_id":"reviewer-ops","lens":"correctness","severity":"urgent","confidence":"high","category":"correctness","file_refs":["internal/orchestrator/run.go"],"summary":"invalid severity","evidence":"severity is outside the supported set","finding_fingerprint":"review:invalid-severity","suggested_action":"use a standard severity"}]}`,
		},
		{
			name:    "invalid confidence",
			payload: `{"status":"completed_with_findings","summary":"review found issues","next_action":"needs_changes","failure_fingerprint":null,"artifact_refs":null,"findings":[{"reviewer_id":"reviewer-ops","lens":"correctness","severity":"high","confidence":"certain","category":"correctness","file_refs":["internal/orchestrator/run.go"],"summary":"invalid confidence","evidence":"confidence is outside the supported set","finding_fingerprint":"review:invalid-confidence","suggested_action":"use a standard confidence"}]}`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := setupTestRepo(t)
			runner := &fakeCommandRunner{
				run: func(_ context.Context, req CommandRequest) (CommandResult, error) {
					lastMessagePath := findFlagValue(req.Args, "-o")
					if err := os.WriteFile(lastMessagePath, []byte(tc.payload), 0o644); err != nil {
						t.Fatalf("write last message: %v", err)
					}
					return CommandResult{}, nil
				},
			}

			executor := NewExecutor(runner)
			executor.now = func() time.Time {
				return time.Date(2026, 3, 6, 12, 4, 30, 0, time.UTC)
			}
			executor.runID = func() string { return "runid123" }

			result, err := executor.RunTask(context.Background(), RunOptions{
				TaskID:    "Sprint-04/Task-01",
				AgentType: AgentReviewer,
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
		})
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
		"TMPDIR":         filepath.Join(repo, ".toolhub", "runtime", "tmp"),
		"TMP":            filepath.Join(repo, ".toolhub", "runtime", "tmp"),
		"TEMP":           filepath.Join(repo, ".toolhub", "runtime", "tmp"),
		"GOTMPDIR":       filepath.Join(repo, ".toolhub", "runtime", "go-build"),
		"GOCACHE":        filepath.Join(repo, ".toolhub", "runtime", "go-cache"),
		"GOMODCACHE":     filepath.Join(repo, ".toolhub", "runtime", "go-mod-cache"),
		"XDG_CACHE_HOME": filepath.Join(repo, ".toolhub", "runtime", ".cache"),
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

func TestBuildCommandEnvReusesPopulatedLegacyGoModCache(t *testing.T) {
	repo := t.TempDir()
	legacyCacheDir := filepath.Join(repo, ".toolhub", "runtime", "tmp", "gomodcache", "cache")
	if err := os.MkdirAll(legacyCacheDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyCacheDir, "module.info"), []byte("warm"), 0o644); err != nil {
		t.Fatalf("seed legacy cache: %v", err)
	}

	env, err := buildCommandEnv(repo, AgentDeveloper, false)
	if err != nil {
		t.Fatalf("build command env: %v", err)
	}

	want := filepath.Join(repo, ".toolhub", "runtime", "tmp", "gomodcache")
	if got := envValue(env, "GOMODCACHE"); got != want {
		t.Fatalf("unexpected GOMODCACHE fallback: got %q want %q", got, want)
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
	if strings.Join(keys, ",") != "TMPDIR,TMP,TEMP,GOTMPDIR,GOCACHE,GOMODCACHE,XDG_CACHE_HOME" {
		t.Fatalf("unexpected default env keys: %v", keys)
	}

	keys = commandEnvKeys(RunOptions{AgentType: AgentDeveloper, IsolatedCodexHome: true})
	if strings.Join(keys, ",") != "TMPDIR,TMP,TEMP,GOTMPDIR,GOCACHE,GOMODCACHE,XDG_CACHE_HOME,HOME" {
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
	if strings.Contains(progress.String(), "s)") {
		t.Fatalf("expected heartbeat output without seconds: %q", progress.String())
	}
}

func TestFormatHeartbeatElapsed(t *testing.T) {
	cases := []struct {
		name    string
		elapsed time.Duration
		want    string
	}{
		{
			name:    "sub minute",
			elapsed: 30 * time.Second,
			want:    "<1m",
		},
		{
			name:    "minutes only",
			elapsed: 3*time.Minute + 40*time.Second,
			want:    "3m",
		},
		{
			name:    "hours only",
			elapsed: time.Hour + 20*time.Second,
			want:    "1h",
		},
		{
			name:    "hours and minutes",
			elapsed: 2*time.Hour + 5*time.Minute + 50*time.Second,
			want:    "2h5m",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatHeartbeatElapsed(tc.elapsed); got != tc.want {
				t.Fatalf("formatHeartbeatElapsed(%s) = %q, want %q", tc.elapsed, got, tc.want)
			}
		})
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

func TestResultJSONUsesNullForUnsetOptionalFields(t *testing.T) {
	result := Result{
		Runner:     RunnerCodexExec,
		Status:     "success",
		Summary:    "ok",
		NextAction: "proceed",
		Findings: []Finding{
			{},
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
	if value["failure_fingerprint"] != nil {
		t.Fatalf("expected null failure_fingerprint, got %v", value["failure_fingerprint"])
	}

	artifactRefs, ok := value["artifact_refs"].(map[string]any)
	if !ok {
		t.Fatalf("artifact_refs is not an object: %T", value["artifact_refs"])
	}
	for _, key := range []string{"log", "worktree", "patch", "report"} {
		if artifactRefs[key] != nil {
			t.Fatalf("expected artifact_refs.%s to be null, got %v", key, artifactRefs[key])
		}
	}

	findings, ok := value["findings"].([]any)
	if !ok || len(findings) != 1 {
		t.Fatalf("unexpected findings payload: %v", value["findings"])
	}
	finding, ok := findings[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected finding payload: %T", findings[0])
	}
	for _, key := range []string{"reviewer_id", "lens", "severity", "confidence", "category", "file_refs", "summary", "evidence", "finding_fingerprint", "suggested_action"} {
		if _, exists := finding[key]; !exists {
			t.Fatalf("missing finding key %q in result json: %s", key, string(raw))
		}
	}
}

func TestRequestJSONMatchesToolSchemaFieldNames(t *testing.T) {
	request := Request{
		AgentType:      AgentReviewer,
		TaskID:         "Sprint-04/Task-01",
		Attempt:        2,
		TimeoutSeconds: 120,
		ContextRefs: ContextRefs{
			SprintID:     "Sprint-04",
			WorktreePath: "/repo/worktree",
			ArtifactRefs: ArtifactRefs{
				Log: "logs/input.log",
			},
		},
	}

	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	for _, key := range []string{"agent_type", "task_id", "attempt", "timeout_seconds", "context_refs"} {
		if _, ok := value[key]; !ok {
			t.Fatalf("missing key %q in request json: %s", key, string(raw))
		}
	}

	contextRefs, ok := value["context_refs"].(map[string]any)
	if !ok {
		t.Fatalf("context_refs is not an object: %T", value["context_refs"])
	}
	for _, key := range []string{"sprint_id", "worktree_path", "artifact_refs"} {
		if _, ok := contextRefs[key]; !ok {
			t.Fatalf("missing context_refs key %q in request json: %s", key, string(raw))
		}
	}
}

func TestExecuteMapsRequestTimeoutSeconds(t *testing.T) {
	repo := setupTestRepo(t)
	runner := &fakeCommandRunner{
		run: func(ctx context.Context, req CommandRequest) (CommandResult, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("expected timeout deadline on runner context")
			}
			remaining := time.Until(deadline)
			if remaining <= 0 || remaining > 2*time.Second {
				t.Fatalf("unexpected remaining timeout: %s", remaining)
			}

			lastMessagePath := findFlagValue(req.Args, "-o")
			if err := os.WriteFile(lastMessagePath, []byte(`{"status":"success","summary":"ok","next_action":"done","failure_fingerprint":null,"artifact_refs":null,"findings":null}`), 0o644); err != nil {
				t.Fatalf("write last message: %v", err)
			}
			return CommandResult{}, nil
		},
	}

	executor := NewExecutor(runner)
	response := executor.Execute(context.Background(), Request{
		AgentType:      AgentDeveloper,
		TaskID:         "Sprint-04/Task-01",
		TimeoutSeconds: 1,
	}, ExecuteOptions{
		PlanFile: "plan/SPRINTS-V1.md",
		TasksDir: "plan/tasks",
		WorkDir:  repo,
	})

	if !response.OK || response.Data == nil {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestExecuteRejectsNegativeTimeoutSeconds(t *testing.T) {
	executor := NewExecutor(&fakeCommandRunner{})
	response := executor.Execute(context.Background(), Request{
		TaskID:         "Sprint-04/Task-01",
		TimeoutSeconds: -1,
	}, ExecuteOptions{})

	if response.OK {
		t.Fatalf("expected error response: %+v", response)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeInvalidRequest {
		t.Fatalf("unexpected error response: %+v", response)
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
- Start by identifying the binding contract for this task from README.md, TECH-V1.md, PROJECT-DEVELOPER-GUIDE.md, and the task brief before making code changes.
- If the task implements or changes a named tool, keep its public schema and caller-facing semantics aligned with TECH-V1.md; do not invent new fields or statuses unless you update the spec and callers in the same change.
- If execution context includes latest_qa_feedback, read it first, then use latest_qa_artifact_refs for full detail before making changes.
- Fix the concrete problems called out by that latest QA round before doing any follow-on work.
- After the latest QA issues are addressed, read latest_reviewer_feedback, then use latest_reviewer_artifact_refs to fix the latest reviewer findings.
- If previous_developer_context is present, continue from that summary and changed file list instead of re-discovering the same work.
- Before finishing, verify that each acceptance criterion and each relevant contract rule is covered by code changes plus a validation step or test.
- After fixing a validation or contract finding, inspect sibling invalid-input and edge cases for the same interface instead of stopping at the exact failing example.
- For tool-contract tasks, audit adjacent required fields, enum values, uniqueness constraints, empty-input combinations, and contradictory status/result combinations touched by the change.
- When multiple signals can coexist, explicitly separate metadata from the final decision and check which combinations should preserve both.
- For non-trivial decision logic, reason through the decision table before coding and add regression tests for combinations such as blocking + conflict, blocking + supplemental-review, and invalid-input + terminal status.
- After fixing the explicit findings, inspect adjacent branches in the same control flow, persistence path, and recovery path for similar defects.
- Before handing off, remove dead code, stale helpers, and replaced branches that this task made obsolete, especially if they can trip lint or confuse the active code path.
- Run the smallest validation that proves both the reported issue and the contract-level behavior are covered before finishing.
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

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if str, ok := value.(string); ok && str == want {
			return true
		}
	}
	return false
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
	status := props["status"].(map[string]any)
	enum := status["enum"].([]any)
	if len(enum) != len(allowedResultStatusValues) {
		t.Fatalf("unexpected status enum count: %d", len(enum))
	}
	for _, expected := range []string{"success", "failed", "timeout"} {
		if !containsAnyString(enum, expected) {
			t.Fatalf("missing status enum value %q: %v", expected, enum)
		}
	}

	artifactRefs := props["artifact_refs"].(map[string]any)
	artifactAnyOf := artifactRefs["anyOf"].([]any)
	artifactObject := artifactAnyOf[1].(map[string]any)
	artifactRequired := artifactObject["required"].([]any)
	if len(artifactRequired) != 4 {
		t.Fatalf("unexpected artifact_refs required count: %d", len(artifactRequired))
	}

	findings := props["findings"].(map[string]any)
	findingsAnyOf := findings["anyOf"].([]any)
	findingItems := findingsAnyOf[1].(map[string]any)["items"].(map[string]any)
	findingProps := findingItems["properties"].(map[string]any)
	lens := findingProps["lens"].(map[string]any)
	lensEnum := lens["enum"].([]any)
	if len(lensEnum) != len(AllowedReviewerLenses()) {
		t.Fatalf("unexpected reviewer lens enum count: %d", len(lensEnum))
	}
	for _, expected := range AllowedReviewerLenses() {
		if !containsAnyString(lensEnum, expected) {
			t.Fatalf("missing reviewer lens enum value %q: %v", expected, lensEnum)
		}
	}
	severity := findingProps["severity"].(map[string]any)
	severityEnum := severity["enum"].([]any)
	if len(severityEnum) != len(AllowedFindingSeverities()) {
		t.Fatalf("unexpected finding severity enum count: %d", len(severityEnum))
	}
	for _, expected := range AllowedFindingSeverities() {
		if !containsAnyString(severityEnum, expected) {
			t.Fatalf("missing finding severity enum value %q: %v", expected, severityEnum)
		}
	}
	confidence := findingProps["confidence"].(map[string]any)
	confidenceEnum := confidence["enum"].([]any)
	if len(confidenceEnum) != len(AllowedFindingConfidences()) {
		t.Fatalf("unexpected finding confidence enum count: %d", len(confidenceEnum))
	}
	for _, expected := range AllowedFindingConfidences() {
		if !containsAnyString(confidenceEnum, expected) {
			t.Fatalf("missing finding confidence enum value %q: %v", expected, confidenceEnum)
		}
	}
	reviewerID := findingProps["reviewer_id"].(map[string]any)
	if reviewerID["type"] != "string" {
		t.Fatalf("unexpected reviewer_id type: %v", reviewerID["type"])
	}
	if reviewerID["pattern"] == "" {
		t.Fatalf("expected reviewer_id pattern to reject blank values: %v", reviewerID)
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

func TestBuildPromptDeveloperIncludesFeedbackRepairRules(t *testing.T) {
	task := &issuesync.TaskBrief{
		TaskID: "Sprint-04/Task-01",
		Goal:   "Goal",
		Source: "/repo/plan/tasks/Sprint-04/Task-01.md",
	}
	sprint := &issuesync.Sprint{ID: "Sprint-04", Goal: "Sprint Goal"}
	prompt := buildPrompt(AgentDeveloper, task, sprint, 2, "", ContextRefs{
		SprintID:     "Sprint-04",
		WorktreePath: "/repo",
		QAArtifactRefs: ArtifactRefs{
			Report: ".toolhub/runs/Sprint-04/Task-01/qa/attempt-02/default/run/result.json",
		},
		QAFeedback: FeedbackRefs{
			Attempt:    2,
			Status:     "completed_with_findings",
			NextAction: "return_to_developer",
			Summary:    "qa summary",
			Findings: []Finding{
				{
					ReviewerID:      "qa-agent",
					Lens:            "correctness",
					Severity:        "high",
					Confidence:      "high",
					Category:        "state_recovery",
					Summary:         "qa finding",
					SuggestedAction: "fix qa finding",
				},
			},
		},
		ReviewerArtifactRefs: ArtifactRefs{
			Report: ".toolhub/runs/Sprint-04/Task-01/reviewer/attempt-01/default/run/result.json",
		},
		ReviewerFeedback: FeedbackRefs{
			Attempt:    1,
			Status:     "completed_with_findings",
			NextAction: "needs_fix",
			Summary:    "reviewer summary",
		},
		PreviousDeveloper: DeveloperRefs{
			Attempt:      1,
			Status:       "completed",
			NextAction:   "qa",
			Summary:      "developer summary",
			ChangedFiles: []string{"internal/orchestrator/run.go"},
		},
	}, "/repo", "")

	for _, needle := range []string{
		"If execution context includes latest_qa_feedback, read it first, then use latest_qa_artifact_refs for full detail before making changes.",
		"Fix the concrete problems called out by that latest QA round before doing any follow-on work.",
		"After the latest QA issues are addressed, read latest_reviewer_feedback, then use latest_reviewer_artifact_refs to fix the latest reviewer findings.",
		"If previous_developer_context is present, continue from that summary and changed file list instead of re-discovering the same work.",
		"After fixing a validation or contract finding, inspect sibling invalid-input and edge cases for the same interface instead of stopping at the exact failing example.",
		"When multiple signals can coexist, explicitly separate metadata from the final decision and check which combinations should preserve both.",
		"Before handing off, remove dead code, stale helpers, and replaced branches that this task made obsolete, especially if they can trip lint or confuse the active code path.",
		"After fixing the explicit findings, inspect adjacent branches in the same control flow, persistence path, and recovery path for similar defects.",
		"- Contract Checklist:",
		"- Adjacent Contract Audit:",
		"- Decision Table Audit:",
		"- Validation Checklist:",
		"- Acceptance Sweep:",
		"- latest_qa_artifact_refs:",
		"- latest_qa_feedback:",
		"- latest_reviewer_artifact_refs:",
		"- latest_reviewer_feedback:",
		"- previous_developer_context:",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in prompt:\n%s", needle, prompt)
		}
	}
	if strings.Index(prompt, "latest_qa_artifact_refs") > strings.Index(prompt, "latest_reviewer_artifact_refs") {
		t.Fatalf("expected QA artifact refs to appear before reviewer artifact refs, got:\n%s", prompt)
	}
}

func TestBuildPromptDeveloperIncludesToolContractExcerpt(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "TECH-V1.md"), strings.Join([]string{
		"# TECH-V1",
		"",
		"### `review-aggregation-tool`",
		"",
		"```yaml",
		"request:",
		"  task_id: string",
		"  review_results:",
		"    - reviewer_id: string",
		"      lens: string",
		"      status: string",
		"      findings: [finding]",
		"",
		"response.data:",
		"  aggregated_findings: [finding]",
		"  decision: pass | request_changes | awaiting_human",
		"  summary: string",
		"```",
		"",
		"### `task-pr-tool`",
		"",
		"```yaml",
		"request:",
		"  op: create",
		"```",
		"",
	}, "\n"))

	task := &issuesync.TaskBrief{
		TaskID:             "Sprint-04/Task-03",
		Title:              "实现 review-aggregation-tool",
		Goal:               "实现纯聚合的 `review-aggregation-tool`。",
		Reads:              []string{"TECH-V1.md"},
		AcceptanceCriteria: []string{"输出结构符合 `TECH-V1.md`"},
		Source:             filepath.Join(repo, "plan/tasks/Sprint-04/Task-03.md"),
	}
	sprint := &issuesync.Sprint{ID: "Sprint-04", Goal: "Sprint Goal"}

	prompt := buildPrompt(AgentDeveloper, task, sprint, 1, "", ContextRefs{
		SprintID:     "Sprint-04",
		WorktreePath: repo,
	}, repo, "")

	for _, needle := range []string{
		"Preserve the public request/response contract defined in `TECH-V1.md` for `review-aggregation-tool`;",
		"- Relevant Spec Excerpts:",
		"TECH-V1 `review-aggregation-tool` contract:",
		"task_id: string",
		"aggregated_findings: [finding]",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in prompt:\n%s", needle, prompt)
		}
	}
	if strings.Contains(prompt, "TECH-V1 `task-pr-tool` contract:") {
		t.Fatalf("unexpected unrelated tool excerpt in prompt:\n%s", prompt)
	}
}

func writeHistoricalRunArtifacts(t *testing.T, repo, taskID string, agentType AgentType, attempt int, lens, runLeaf string) (string, string) {
	return writeHistoricalRunResult(t, repo, taskID, agentType, attempt, lens, runLeaf, Result{
		Status:     "pass",
		Summary:    "historical run",
		NextAction: "none",
	})
}

func writeHistoricalRunResult(t *testing.T, repo, taskID string, agentType AgentType, attempt int, lens, runLeaf string, result Result) (string, string) {
	t.Helper()

	runDir := filepath.Join(
		repo,
		".toolhub",
		"runs",
		filepath.FromSlash(taskID),
		string(agentType),
		fmt.Sprintf("attempt-%02d", attempt),
		lens,
		runLeaf,
	)
	reportPath := filepath.Join(runDir, "result.json")
	logPath := filepath.Join(runDir, "runner.log")
	reportBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal historical result: %v", err)
	}
	mustWriteFile(t, reportPath, string(reportBytes)+"\n")
	mustWriteFile(t, logPath, "runner log\n")
	return filepath.ToSlash(strings.TrimPrefix(reportPath, repo+string(os.PathSeparator))), filepath.ToSlash(strings.TrimPrefix(logPath, repo+string(os.PathSeparator)))
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

func initGitRepo(t *testing.T, repo string) {
	t.Helper()

	for _, args := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "codex@example.com"},
		{"git", "config", "user.name", "Codex"},
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repo
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, string(output))
		}
	}
}
