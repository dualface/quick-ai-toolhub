package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"quick-ai-toolhub/internal/agentrun"
	"quick-ai-toolhub/internal/store"
)

type fakeAgentRunner struct {
	t     *testing.T
	mu    sync.Mutex
	steps []fakeAgentStep
}

type fakeAgentStep struct {
	agentType agentrun.AgentType
	attempt   int
	lens      string
	assert    func(*testing.T, agentrun.Request, agentrun.ExecuteOptions)
	result    agentrun.Result
}

func (f *fakeAgentRunner) Execute(_ context.Context, req agentrun.Request, opts agentrun.ExecuteOptions) agentrun.Response {
	f.t.Helper()
	step := f.takeStep(req)
	if req.AgentType != step.agentType {
		f.t.Fatalf("unexpected agent type: got %s want %s", req.AgentType, step.agentType)
	}
	if req.Attempt != step.attempt {
		f.t.Fatalf("unexpected attempt for %s: got %d want %d", req.AgentType, req.Attempt, step.attempt)
	}
	if strings.TrimSpace(step.lens) != "" && req.Lens != step.lens {
		f.t.Fatalf("unexpected lens for %s: got %s want %s", req.AgentType, req.Lens, step.lens)
	}
	if step.assert != nil {
		step.assert(f.t, req, opts)
	}

	result := step.result
	if result.Runner == "" {
		result.Runner = agentrun.RunnerCodexExec
	}
	return agentrun.Response{
		OK:   true,
		Data: &result,
	}
}

func (f *fakeAgentRunner) takeStep(req agentrun.Request) fakeAgentStep {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.steps) == 0 {
		f.t.Fatalf("unexpected agent call: %+v", req)
	}

	if req.AgentType == agentrun.AgentReviewer && strings.TrimSpace(req.Lens) != "" {
		for index, step := range f.steps {
			if step.agentType == req.AgentType && step.attempt == req.Attempt && step.lens == req.Lens {
				f.steps = append(f.steps[:index], f.steps[index+1:]...)
				return step
			}
		}
	}

	step := f.steps[0]
	f.steps = f.steps[1:]
	return step
}

func (f *fakeAgentRunner) assertExhausted(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.steps) != 0 {
		t.Fatalf("expected all fake agent steps to run, remaining: %+v", f.steps)
	}
}

func TestRunTaskCompletesDeveloperQAReviewLoop(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   1,
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer finished implementation",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("developer-01"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   1,
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "qa passed",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("qa-01"),
				},
			},
			{
				agentType: agentrun.AgentReviewer,
				attempt:   1,
				lens:      "correctness",
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "review passed",
					NextAction:   "open_task_pr",
					ArtifactRefs: artifactRefsFor("review-01"),
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID:       "Sprint-04/Task-02",
		ReviewerLens: " Correctness ",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "pr_open" {
		t.Fatalf("unexpected final status: %s", result.Status)
	}
	if result.NextAction != "open_task_pr" {
		t.Fatalf("unexpected next action: %s", result.NextAction)
	}
	if result.Stage != StageReview {
		t.Fatalf("unexpected final stage: %s", result.Stage)
	}
	if len(result.StageResults) != 3 {
		t.Fatalf("unexpected stage result count: %d", len(result.StageResults))
	}
	if result.StageResults[0].Stage != StageDeveloper || result.StageResults[0].TaskStatus != "qa_in_progress" {
		t.Fatalf("unexpected developer stage result: %+v", result.StageResults[0])
	}
	if result.StageResults[1].Stage != StageQA || result.StageResults[1].TaskStatus != "review_in_progress" {
		t.Fatalf("unexpected qa stage result: %+v", result.StageResults[1])
	}
	if result.StageResults[2].Stage != StageReview || result.StageResults[2].TaskStatus != "pr_open" {
		t.Fatalf("unexpected review stage result: %+v", result.StageResults[2])
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "pr_open" {
		t.Fatalf("unexpected task status: %s", taskRow.Status)
	}
	if taskRow.AttemptTotal != 1 {
		t.Fatalf("unexpected attempt total: %d", taskRow.AttemptTotal)
	}
	if taskRow.QAFailCount != 0 || taskRow.ReviewFailCount != 0 {
		t.Fatalf("unexpected fail counters: %+v", taskRow)
	}

	assertOrchestratorEventTypes(t, storeService, []string{
		eventDeveloperStarted,
		eventDeveloperCompleted,
		eventQAPassed,
		eventReviewStarted,
		eventReviewAggregated,
	})
}

func TestRunTaskRetriesTransientDeveloperFailureWithoutBlockingTask(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   1,
				result: agentrun.Result{
					Status:             "timeout",
					Summary:            "developer runner timed out",
					NextAction:         "retry",
					FailureFingerprint: agentrun.ErrorCodeRunnerTimeout,
					ArtifactRefs:       artifactRefsFor("developer-timeout-01"),
				},
			},
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   1,
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer finished implementation",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("developer-01"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   1,
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "qa passed",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("qa-01"),
				},
			},
			{
				agentType: agentrun.AgentReviewer,
				attempt:   1,
				lens:      "correctness",
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "review passed",
					NextAction:   "open_task_pr",
					ArtifactRefs: artifactRefsFor("review-01"),
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID: "Sprint-04/Task-02",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "pr_open" {
		t.Fatalf("unexpected final status: %+v", result)
	}
	if len(result.StageResults) != 4 {
		t.Fatalf("expected transient developer retry plus success path, got %+v", result.StageResults)
	}
	if result.StageResults[0].Stage != StageDeveloper || result.StageResults[0].TaskStatus != "dev_in_progress" {
		t.Fatalf("unexpected transient developer stage result: %+v", result.StageResults[0])
	}
	if result.StageResults[0].NextAction != "retry" || result.StageResults[0].FailureFingerprint != agentrun.ErrorCodeRunnerTimeout {
		t.Fatalf("unexpected transient developer retry metadata: %+v", result.StageResults[0])
	}
	if result.StageResults[0].EventID != "" {
		t.Fatalf("expected transient developer retry to skip event persistence, got %+v", result.StageResults[0])
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "pr_open" || taskRow.AttemptTotal != 1 {
		t.Fatalf("unexpected task row after transient developer retry: %+v", taskRow)
	}

	assertOrchestratorEventTypes(t, storeService, []string{
		eventDeveloperStarted,
		eventDeveloperCompleted,
		eventQAPassed,
		eventReviewStarted,
		eventReviewAggregated,
	})
}

func TestRunTaskRetriesTransientQAFailureWithoutMarkingTaskFailed(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   1,
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer finished implementation",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("developer-01"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   1,
				result: agentrun.Result{
					Status:             "failed",
					Summary:            "qa runner failed before a stable result",
					NextAction:         "retry",
					FailureFingerprint: agentrun.ErrorCodeRunnerExecution,
					ArtifactRefs:       artifactRefsFor("qa-retry-01"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   1,
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "qa passed",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("qa-01"),
				},
			},
			{
				agentType: agentrun.AgentReviewer,
				attempt:   1,
				lens:      "correctness",
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "review passed",
					NextAction:   "open_task_pr",
					ArtifactRefs: artifactRefsFor("review-01"),
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID: "Sprint-04/Task-02",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "pr_open" {
		t.Fatalf("unexpected final status: %+v", result)
	}
	if len(result.StageResults) != 4 {
		t.Fatalf("expected transient qa retry plus success path, got %+v", result.StageResults)
	}
	if result.StageResults[1].Stage != StageQA || result.StageResults[1].TaskStatus != "qa_in_progress" {
		t.Fatalf("unexpected transient qa stage result: %+v", result.StageResults[1])
	}
	if result.StageResults[1].NextAction != "retry" || result.StageResults[1].FailureFingerprint != agentrun.ErrorCodeRunnerExecution {
		t.Fatalf("unexpected transient qa retry metadata: %+v", result.StageResults[1])
	}
	if result.StageResults[1].EventID != "" {
		t.Fatalf("expected transient qa retry to skip event persistence, got %+v", result.StageResults[1])
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "pr_open" || taskRow.QAFailCount != 0 {
		t.Fatalf("unexpected task row after transient qa retry: %+v", taskRow)
	}

	assertOrchestratorEventTypes(t, storeService, []string{
		eventDeveloperStarted,
		eventDeveloperCompleted,
		eventQAPassed,
		eventReviewStarted,
		eventReviewAggregated,
	})
}

func TestRunTaskRetriesTransientReviewFailureWithoutAwaitingHuman(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   1,
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer finished implementation",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("developer-01"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   1,
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "qa passed",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("qa-01"),
				},
			},
			{
				agentType: agentrun.AgentReviewer,
				attempt:   1,
				lens:      "correctness",
				result: agentrun.Result{
					Status:             "timeout",
					Summary:            "review runner timed out",
					NextAction:         "retry",
					FailureFingerprint: agentrun.ErrorCodeRunnerTimeout,
					ArtifactRefs:       artifactRefsFor("review-retry-01"),
				},
			},
			{
				agentType: agentrun.AgentReviewer,
				attempt:   1,
				lens:      "correctness",
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "review passed",
					NextAction:   "open_task_pr",
					ArtifactRefs: artifactRefsFor("review-01"),
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID: "Sprint-04/Task-02",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "pr_open" {
		t.Fatalf("unexpected final status: %+v", result)
	}
	if len(result.StageResults) != 4 {
		t.Fatalf("expected transient review retry plus success path, got %+v", result.StageResults)
	}
	if result.StageResults[2].Stage != StageReview || result.StageResults[2].TaskStatus != "review_in_progress" {
		t.Fatalf("unexpected transient review stage result: %+v", result.StageResults[2])
	}
	if result.StageResults[2].NextAction != "retry" || result.StageResults[2].FailureFingerprint != agentrun.ErrorCodeRunnerTimeout {
		t.Fatalf("unexpected transient review retry metadata: %+v", result.StageResults[2])
	}
	if result.StageResults[2].EventID != "" {
		t.Fatalf("expected transient review retry to skip event persistence, got %+v", result.StageResults[2])
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "pr_open" || taskRow.ReviewFailCount != 0 {
		t.Fatalf("unexpected task row after transient review retry: %+v", taskRow)
	}

	assertOrchestratorEventTypes(t, storeService, []string{
		eventDeveloperStarted,
		eventDeveloperCompleted,
		eventQAPassed,
		eventReviewStarted,
		eventReviewAggregated,
	})
}

func TestRunTaskReturnsToDeveloperAfterQAFailure(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	firstQARefs := artifactRefsFor("qa-fail-01")
	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   1,
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer finished round one",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("developer-01"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   1,
				result: agentrun.Result{
					Status:             "failed",
					Summary:            "lint failed",
					NextAction:         "return_to_developer",
					FailureFingerprint: "lint:pkg/service.go:no-unused",
					ArtifactRefs:       firstQARefs,
				},
			},
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   2,
				assert: func(t *testing.T, req agentrun.Request, _ agentrun.ExecuteOptions) {
					if req.ContextRefs.QAArtifactRefs.Report != firstQARefs.Report {
						t.Fatalf("expected latest qa report %s, got %+v", firstQARefs.Report, req.ContextRefs.QAArtifactRefs)
					}
				},
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer fixed lint issue",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("developer-02"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   2,
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "qa passed",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("qa-pass-02"),
				},
			},
			{
				agentType: agentrun.AgentReviewer,
				attempt:   2,
				lens:      "correctness",
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "review passed",
					NextAction:   "open_task_pr",
					ArtifactRefs: artifactRefsFor("review-02"),
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID: "Sprint-04/Task-02",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "pr_open" || result.Attempt != 2 {
		t.Fatalf("unexpected orchestrator result: %+v", result)
	}
	if result.FailureFingerprint != "" {
		t.Fatalf("expected final task result to clear stale failure fingerprint, got %q", result.FailureFingerprint)
	}
	if len(result.StageResults) != 5 {
		t.Fatalf("unexpected stage result count: %d", len(result.StageResults))
	}
	if result.StageResults[1].TaskStatus != "qa_failed" {
		t.Fatalf("expected first qa result to land on qa_failed, got %+v", result.StageResults[1])
	}
	if result.StageResults[2].Attempt != 2 || result.StageResults[2].Stage != StageDeveloper {
		t.Fatalf("expected retry developer stage on attempt 2, got %+v", result.StageResults[2])
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "pr_open" || taskRow.AttemptTotal != 2 {
		t.Fatalf("unexpected task row after qa retry: %+v", taskRow)
	}
	if taskRow.QAFailCount != 0 {
		t.Fatalf("expected qa fail count to reset after passing, got %d", taskRow.QAFailCount)
	}

	assertOrchestratorEventTypes(t, storeService, []string{
		eventDeveloperStarted,
		eventDeveloperCompleted,
		eventQAFailed,
		eventRetryApproved,
		eventDeveloperStarted,
		eventDeveloperCompleted,
		eventQAPassed,
		eventReviewStarted,
		eventReviewAggregated,
	})
}

func TestRunTaskReturnsToDeveloperAfterReviewFailureAndStoresFindings(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	firstQARefs := artifactRefsFor("qa-pass-01")
	firstReviewRefs := artifactRefsFor("review-fail-01")
	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   1,
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer finished round one",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("developer-01"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   1,
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "qa passed",
					NextAction:   "proceed",
					ArtifactRefs: firstQARefs,
				},
			},
			{
				agentType: agentrun.AgentReviewer,
				attempt:   1,
				lens:      "correctness",
				result: agentrun.Result{
					Status:       "needs_changes",
					Summary:      "review found gaps",
					NextAction:   "return_to_developer",
					ArtifactRefs: firstReviewRefs,
					Findings: []agentrun.Finding{
						{
							ReviewerID:         "reviewer-correctness",
							Lens:               "correctness",
							Severity:           "high",
							Confidence:         "high",
							Category:           "correctness",
							FileRefs:           []string{"service.go"},
							Summary:            "missing nil guard",
							Evidence:           "service.go can dereference nil input",
							FindingFingerprint: "correctness:service.go:nil-guard",
							SuggestedAction:    "add the nil guard",
						},
						{
							ReviewerID:         "reviewer-correctness",
							Lens:               "correctness",
							Severity:           "high",
							Confidence:         "high",
							Category:           "correctness",
							FileRefs:           []string{"service.go"},
							Summary:            "missing nil guard",
							Evidence:           "service.go can dereference nil input",
							FindingFingerprint: "correctness:service.go:nil-guard",
							SuggestedAction:    "add the nil guard",
						},
					},
				},
			},
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   2,
				assert: func(t *testing.T, req agentrun.Request, _ agentrun.ExecuteOptions) {
					if req.ContextRefs.QAArtifactRefs.Report != firstQARefs.Report {
						t.Fatalf("expected latest qa report %s, got %+v", firstQARefs.Report, req.ContextRefs.QAArtifactRefs)
					}
					if req.ContextRefs.ReviewerArtifactRefs.Report != firstReviewRefs.Report {
						t.Fatalf("expected latest reviewer report %s, got %+v", firstReviewRefs.Report, req.ContextRefs.ReviewerArtifactRefs)
					}
				},
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer fixed reviewer finding",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("developer-02"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   2,
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "qa passed again",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("qa-pass-02"),
				},
			},
			{
				agentType: agentrun.AgentReviewer,
				attempt:   2,
				lens:      "correctness",
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "review passed",
					NextAction:   "open_task_pr",
					ArtifactRefs: artifactRefsFor("review-pass-02"),
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID: "Sprint-04/Task-02",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "pr_open" || result.Attempt != 2 {
		t.Fatalf("unexpected orchestrator result: %+v", result)
	}
	if result.FailureFingerprint != "" {
		t.Fatalf("expected final task result to clear stale failure fingerprint, got %q", result.FailureFingerprint)
	}
	if len(result.StageResults) != 6 {
		t.Fatalf("unexpected stage result count: %d", len(result.StageResults))
	}
	if result.StageResults[2].TaskStatus != "review_failed" {
		t.Fatalf("expected first review result to land on review_failed, got %+v", result.StageResults[2])
	}
	if len(result.StageResults[2].Findings) != 1 {
		t.Fatalf("expected duplicate findings to be deduplicated, got %+v", result.StageResults[2].Findings)
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "pr_open" || taskRow.AttemptTotal != 2 {
		t.Fatalf("unexpected task row after review retry: %+v", taskRow)
	}
	if taskRow.ReviewFailCount != 0 {
		t.Fatalf("expected review fail count to reset after passing, got %d", taskRow.ReviewFailCount)
	}
	if got := countReviewFindings(t, storeService, "Sprint-04/Task-02"); got != 1 {
		t.Fatalf("expected one persisted review finding after dedupe, got %d", got)
	}

	assertOrchestratorEventTypes(t, storeService, []string{
		eventDeveloperStarted,
		eventDeveloperCompleted,
		eventQAPassed,
		eventReviewStarted,
		eventReviewAggregated,
		eventRetryApproved,
		eventDeveloperStarted,
		eventDeveloperCompleted,
		eventQAPassed,
		eventReviewStarted,
		eventReviewAggregated,
	})
}

func TestRunTaskRecoversQAArtifactsAfterRestartFromQAFailed(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "qa_failed",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	firstQARefs := artifactRefsFor("qa-fail-01")
	appendOrchestratorStageEvent(t, storeService, "Sprint-04/Task-02", "Sprint-04", "qa_in_progress", "qa_failed", eventQAFailed, 1, StageResult{
		Stage:              StageQA,
		AgentType:          agentrun.AgentQA,
		Attempt:            1,
		Status:             "failed",
		Summary:            "lint failed",
		NextAction:         "return_to_developer",
		FailureFingerprint: "lint:pkg/service.go:no-unused",
		ArtifactRefs:       firstQARefs,
	})

	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   2,
				assert: func(t *testing.T, req agentrun.Request, _ agentrun.ExecuteOptions) {
					if req.ContextRefs.QAArtifactRefs.Report != firstQARefs.Report {
						t.Fatalf("expected latest qa report %s, got %+v", firstQARefs.Report, req.ContextRefs.QAArtifactRefs)
					}
					if hasAnyArtifactRefs(req.ContextRefs.ReviewerArtifactRefs) {
						t.Fatalf("expected no reviewer refs on qa_failed resume, got %+v", req.ContextRefs.ReviewerArtifactRefs)
					}
				},
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer fixed qa failure after restart",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("developer-02"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   2,
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "qa passed",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("qa-pass-02"),
				},
			},
			{
				agentType: agentrun.AgentReviewer,
				attempt:   2,
				lens:      "correctness",
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "review passed",
					NextAction:   "open_task_pr",
					ArtifactRefs: artifactRefsFor("review-pass-02"),
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID: "Sprint-04/Task-02",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "pr_open" || result.Attempt != 2 {
		t.Fatalf("unexpected orchestrator result after qa_failed resume: %+v", result)
	}
}

func TestRunTaskRecoversQAAndReviewArtifactsAfterRestartFromReviewFailed(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "review_failed",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	firstQARefs := artifactRefsFor("qa-pass-01")
	firstReviewRefs := artifactRefsFor("review-fail-01")
	appendOrchestratorStageEvent(t, storeService, "Sprint-04/Task-02", "Sprint-04", "qa_in_progress", "review_in_progress", eventQAPassed, 1, StageResult{
		Stage:        StageQA,
		AgentType:    agentrun.AgentQA,
		Attempt:      1,
		Status:       "pass",
		Summary:      "qa passed",
		NextAction:   "proceed",
		ArtifactRefs: firstQARefs,
	})
	appendOrchestratorStageEvent(t, storeService, "Sprint-04/Task-02", "Sprint-04", "review_in_progress", "review_failed", eventReviewAggregated, 1, StageResult{
		Stage:              StageReview,
		AgentType:          agentrun.AgentReviewer,
		Attempt:            1,
		Status:             "needs_changes",
		Summary:            "review requested changes",
		NextAction:         "return_to_developer",
		FailureFingerprint: "correctness:service.go:nil-guard",
		ArtifactRefs:       firstReviewRefs,
		Findings: []agentrun.Finding{
			{
				ReviewerID:         "reviewer-correctness",
				Lens:               "correctness",
				Severity:           "high",
				Confidence:         "high",
				Category:           "correctness",
				FileRefs:           []string{"service.go"},
				Summary:            "missing nil guard",
				Evidence:           "service.go can dereference nil input",
				FindingFingerprint: "correctness:service.go:nil-guard",
				SuggestedAction:    "add the nil guard",
			},
		},
	})

	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   2,
				assert: func(t *testing.T, req agentrun.Request, _ agentrun.ExecuteOptions) {
					if req.ContextRefs.QAArtifactRefs.Report != firstQARefs.Report {
						t.Fatalf("expected latest qa report %s, got %+v", firstQARefs.Report, req.ContextRefs.QAArtifactRefs)
					}
					if req.ContextRefs.ReviewerArtifactRefs.Report != firstReviewRefs.Report {
						t.Fatalf("expected latest reviewer report %s, got %+v", firstReviewRefs.Report, req.ContextRefs.ReviewerArtifactRefs)
					}
				},
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer fixed review failure after restart",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("developer-02"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   2,
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "qa passed again",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("qa-pass-02"),
				},
			},
			{
				agentType: agentrun.AgentReviewer,
				attempt:   2,
				lens:      "correctness",
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "review passed",
					NextAction:   "open_task_pr",
					ArtifactRefs: artifactRefsFor("review-pass-02"),
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID: "Sprint-04/Task-02",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "pr_open" || result.Attempt != 2 {
		t.Fatalf("unexpected orchestrator result after review_failed resume: %+v", result)
	}
}

func TestRunTaskNoOpReadyForPRUsesLatestReviewArtifactsWithoutFailureFingerprint(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "pr_open",
		AttemptTotal:            2,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	reviewRefs := artifactRefsFor("review-pass-02")
	appendOrchestratorStageEvent(t, storeService, "Sprint-04/Task-02", "Sprint-04", "review_in_progress", "pr_open", eventReviewAggregated, 2, StageResult{
		Stage:        StageReview,
		AgentType:    agentrun.AgentReviewer,
		Attempt:      2,
		Status:       "pass",
		Summary:      "review passed",
		NextAction:   "open_task_pr",
		ArtifactRefs: reviewRefs,
	})
	setOrchestratorTaskFailureFingerprint(t, storeService, "Sprint-04/Task-02", "review:stale")

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: &fakeAgentRunner{t: t},
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID: "Sprint-04/Task-02",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}

	if result.Status != "pr_open" || result.Stage != StageReview {
		t.Fatalf("unexpected no-op orchestrator result: %+v", result)
	}
	if result.FailureFingerprint != "" {
		t.Fatalf("expected no failure fingerprint for ready task, got %q", result.FailureFingerprint)
	}
	if result.ArtifactRefs.Report != reviewRefs.Report {
		t.Fatalf("expected latest review artifact refs, got %+v", result.ArtifactRefs)
	}
	if len(result.StageResults) != 0 {
		t.Fatalf("expected no stage executions for ready task, got %+v", result.StageResults)
	}
}

func TestRunTaskNoOpAwaitingHumanPreservesLastReviewStageContext(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                    "Sprint-04/Task-02",
		SprintID:                  "Sprint-04",
		TaskLocalID:               "Task-02",
		SequenceNo:                2,
		GitHubIssueNumber:         402,
		ParentGitHubIssueNumber:   401,
		Status:                    "awaiting_human",
		AttemptTotal:              2,
		CurrentFailureFingerprint: stringPtr("review:needs-human"),
		TaskBranch:                stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:              &worktreePath,
		NeedsHuman:                true,
		HumanReason:               stringPtr("review requires manual validation"),
	})

	reviewRefs := artifactRefsFor("review-await-human-02")
	appendOrchestratorStageEvent(t, storeService, "Sprint-04/Task-02", "Sprint-04", "review_in_progress", "review_failed", eventReviewAggregated, 2, StageResult{
		Stage:              StageReview,
		AgentType:          agentrun.AgentReviewer,
		Attempt:            2,
		Status:             "blocked",
		Summary:            "review requires human validation",
		NextAction:         "await_human",
		FailureFingerprint: "review:needs-human",
		ArtifactRefs:       reviewRefs,
		Findings: []agentrun.Finding{
			{
				ReviewerID:         "reviewer-correctness",
				Lens:               "correctness",
				Severity:           "medium",
				Confidence:         "low",
				Category:           "correctness",
				FileRefs:           []string{"internal/orchestrator/run.go"},
				Summary:            "manual validation required",
				Evidence:           "reviewers disagree on the runtime behavior",
				FindingFingerprint: "review:needs-human",
				SuggestedAction:    "validate manually",
			},
		},
	})

	base, err := storeService.BaseStore()
	if err != nil {
		t.Fatalf("store base: %v", err)
	}
	if _, err := base.AppendEvent(context.Background(), store.AppendEventPayload{
		EventID:        taskEventID("Sprint-04/Task-02", eventTaskAwaitingHuman, 2),
		EntityType:     "task",
		EntityID:       "Sprint-04/Task-02",
		SprintID:       stringPtr("Sprint-04"),
		TaskID:         stringPtr("Sprint-04/Task-02"),
		EventType:      eventTaskAwaitingHuman,
		Source:         orchestratorSource,
		Attempt:        intPointer(2),
		IdempotencyKey: taskEventIDKey("Sprint-04/Task-02", eventTaskAwaitingHuman, 2),
		PayloadJSON: map[string]any{
			"task_status_from":    "review_failed",
			"task_status_to":      "awaiting_human",
			"summary":             "review requires human validation",
			"failure_fingerprint": "review:needs-human",
		},
		OccurredAt: "2026-03-07T00:00:01Z",
	}); err != nil {
		t.Fatalf("append awaiting human event: %v", err)
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: &fakeAgentRunner{t: t},
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID: "Sprint-04/Task-02",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}

	if result.Status != "awaiting_human" || result.Stage != StageReview {
		t.Fatalf("unexpected no-op orchestrator result: %+v", result)
	}
	if result.NextAction != "await_human" {
		t.Fatalf("unexpected next action: %+v", result)
	}
	if result.ArtifactRefs.Report != reviewRefs.Report {
		t.Fatalf("expected latest review artifact refs, got %+v", result.ArtifactRefs)
	}
	if result.FailureFingerprint != "review:needs-human" {
		t.Fatalf("unexpected failure fingerprint: %+v", result)
	}
	if len(result.StageResults) != 0 {
		t.Fatalf("expected no stage executions for awaiting_human task, got %+v", result.StageResults)
	}
}

func TestClassifyReviewResultNormalizesNextAction(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		result StageResult
		want   reviewDecision
	}{
		{
			name: "await human mixed case",
			result: StageResult{
				Status:     "pass",
				NextAction: "Await_Human",
			},
			want: reviewDecisionAwaitHuman,
		},
		{
			name: "return to developer mixed case",
			result: StageResult{
				Status:     "pass",
				NextAction: "RETURN_TO_DEVELOPER",
			},
			want: reviewDecisionRequestChange,
		},
		{
			name: "open task pr mixed case",
			result: StageResult{
				Status:     "pass",
				NextAction: "OPEN_TASK_PR",
			},
			want: reviewDecisionPass,
		},
		{
			name: "reviewer hint does not override pass aggregate",
			result: StageResult{
				Status:     "pass",
				NextAction: "needs_fix",
			},
			want: reviewDecisionPass,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyReviewResult(tc.result); got != tc.want {
				t.Fatalf("unexpected decision: got %s want %s for %+v", got, tc.want, tc.result)
			}
		})
	}
}

func TestPersistReviewFindingsRejectsInvalidFindingContract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		finding agentrun.Finding
	}{
		{
			name: "invalid lens",
			finding: agentrun.Finding{
				ReviewerID:         "reviewer-ops",
				Lens:               "ops",
				Severity:           "high",
				Confidence:         "high",
				Category:           "correctness",
				FileRefs:           []string{"internal/orchestrator/run.go"},
				Summary:            "invalid review finding contract",
				Evidence:           "lens is outside the supported set",
				FindingFingerprint: "review:invalid-contract",
				SuggestedAction:    "use a supported lens",
			},
		},
		{
			name: "invalid severity",
			finding: agentrun.Finding{
				ReviewerID:         "reviewer-ops",
				Lens:               "correctness",
				Severity:           "urgent",
				Confidence:         "high",
				Category:           "correctness",
				FileRefs:           []string{"internal/orchestrator/run.go"},
				Summary:            "invalid review severity",
				Evidence:           "severity is outside the supported set",
				FindingFingerprint: "review:invalid-severity",
				SuggestedAction:    "use a supported severity",
			},
		},
		{
			name: "invalid confidence",
			finding: agentrun.Finding{
				ReviewerID:         "reviewer-ops",
				Lens:               "correctness",
				Severity:           "high",
				Confidence:         "certain",
				Category:           "correctness",
				FileRefs:           []string{"internal/orchestrator/run.go"},
				Summary:            "invalid review confidence",
				Evidence:           "confidence is outside the supported set",
				FindingFingerprint: "review:invalid-confidence",
				SuggestedAction:    "use a supported confidence",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			storeService := openOrchestratorTestStore(t)
			worktreePath := t.TempDir()
			insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
				SprintID:          "Sprint-04",
				SequenceNo:        4,
				GitHubIssueNumber: 401,
				Status:            "in_progress",
			})
			insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
				TaskID:                  "Sprint-04/Task-02",
				SprintID:                "Sprint-04",
				TaskLocalID:             "Task-02",
				SequenceNo:              2,
				GitHubIssueNumber:       402,
				ParentGitHubIssueNumber: 401,
				Status:                  "review_in_progress",
				AttemptTotal:            2,
				TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
				WorktreePath:            &worktreePath,
			})

			appendOrchestratorStageEvent(t, storeService, "Sprint-04/Task-02", "Sprint-04", "review_in_progress", "review_failed", eventReviewAggregated, 2, StageResult{
				Stage:        StageReview,
				AgentType:    agentrun.AgentReviewer,
				Attempt:      2,
				Status:       "needs_changes",
				Summary:      "review requested changes",
				NextAction:   "return_to_developer",
				ArtifactRefs: artifactRefsFor("review-invalid-02"),
			})

			db, err := storeService.DB()
			if err != nil {
				t.Fatalf("store db: %v", err)
			}

			err = persistReviewFindings(
				context.Background(),
				db,
				"Sprint-04/Task-02",
				taskEventID("Sprint-04/Task-02", eventReviewAggregated, 2),
				[]agentrun.Finding{tc.finding},
			)
			if err == nil {
				t.Fatal("expected invalid review finding contract to fail")
			}
			if got := countReviewFindings(t, storeService, "Sprint-04/Task-02"); got != 0 {
				t.Fatalf("expected no persisted review findings, got %d", got)
			}
		})
	}
}

func TestHandleQAPassClearsFailureAndHumanMetadata(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                    "Sprint-04/Task-02",
		SprintID:                  "Sprint-04",
		TaskLocalID:               "Task-02",
		SequenceNo:                2,
		GitHubIssueNumber:         402,
		ParentGitHubIssueNumber:   401,
		Status:                    "qa_in_progress",
		AttemptTotal:              2,
		QAFailCount:               3,
		CurrentFailureFingerprint: stringPtr("qa:stale"),
		TaskBranch:                stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:              &worktreePath,
		NeedsHuman:                true,
		HumanReason:               stringPtr("stale human gate"),
	})

	db, err := storeService.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	snapshot, err := loadTaskRuntimeSnapshot(context.Background(), db, "Sprint-04/Task-02")
	if err != nil {
		t.Fatalf("load task runtime: %v", err)
	}

	service := New(Dependencies{Store: storeService})
	result, updatedSnapshot, err := service.handleQAResult(context.Background(), snapshot, StageResult{
		Stage:        StageQA,
		AgentType:    agentrun.AgentQA,
		Attempt:      2,
		Status:       "pass",
		Summary:      "qa passed",
		NextAction:   "proceed",
		ArtifactRefs: artifactRefsFor("qa-pass-02"),
	})
	if err != nil {
		t.Fatalf("handle qa result: %v", err)
	}

	if updatedSnapshot.Status != "review_in_progress" {
		t.Fatalf("unexpected updated task status: %s", updatedSnapshot.Status)
	}
	if result.TaskStatus != "review_in_progress" {
		t.Fatalf("unexpected stage result task status: %+v", result)
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "review_in_progress" {
		t.Fatalf("unexpected task status after qa pass: %+v", taskRow)
	}
	if taskRow.QAFailCount != 0 {
		t.Fatalf("expected qa fail count reset, got %d", taskRow.QAFailCount)
	}
	if taskRow.CurrentFailureFingerprint != nil {
		t.Fatalf("expected failure fingerprint to clear after qa pass, got %#v", taskRow.CurrentFailureFingerprint)
	}
	if taskRow.NeedsHuman {
		t.Fatalf("expected needs_human to clear after qa pass, got %+v", taskRow)
	}
	if taskRow.HumanReason != nil {
		t.Fatalf("expected human_reason to clear after qa pass, got %#v", taskRow.HumanReason)
	}

	assertOrchestratorEventTypes(t, storeService, []string{eventQAPassed})
}

func TestHandleQAPassReturnToDeveloperMapsToQAFailed(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "qa_in_progress",
		AttemptTotal:            2,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	db, err := storeService.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	snapshot, err := loadTaskRuntimeSnapshot(context.Background(), db, "Sprint-04/Task-02")
	if err != nil {
		t.Fatalf("load task runtime: %v", err)
	}

	service := New(Dependencies{Store: storeService})
	result, updatedSnapshot, err := service.handleQAResult(context.Background(), snapshot, StageResult{
		Stage:        StageQA,
		AgentType:    agentrun.AgentQA,
		Attempt:      2,
		Status:       "pass",
		Summary:      "qa found a regression and wants another dev pass",
		NextAction:   "return_to_developer",
		ArtifactRefs: artifactRefsFor("qa-return-02"),
	})
	if err != nil {
		t.Fatalf("handle qa result: %v", err)
	}

	if updatedSnapshot.Status != "qa_failed" || result.TaskStatus != "qa_failed" {
		t.Fatalf("expected qa fallback to fail the qa stage, got snapshot=%+v result=%+v", updatedSnapshot, result)
	}
	if result.FailureFingerprint == "" {
		t.Fatalf("expected qa fallback to synthesize a failure fingerprint, got %+v", result)
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "qa_failed" || taskRow.QAFailCount != 1 {
		t.Fatalf("expected qa_failed task row, got %+v", taskRow)
	}
	if taskRow.CurrentFailureFingerprint == nil || *taskRow.CurrentFailureFingerprint != result.FailureFingerprint {
		t.Fatalf("unexpected qa failure fingerprint: row=%#v result=%q", taskRow.CurrentFailureFingerprint, result.FailureFingerprint)
	}

	assertOrchestratorEventTypes(t, storeService, []string{eventQAFailed})
}

func TestHandleQAPassAwaitHumanBlocksTask(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "qa_in_progress",
		AttemptTotal:            2,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	db, err := storeService.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	snapshot, err := loadTaskRuntimeSnapshot(context.Background(), db, "Sprint-04/Task-02")
	if err != nil {
		t.Fatalf("load task runtime: %v", err)
	}

	service := New(Dependencies{Store: storeService})
	result, updatedSnapshot, err := service.handleQAResult(context.Background(), snapshot, StageResult{
		Stage:        StageQA,
		AgentType:    agentrun.AgentQA,
		Attempt:      2,
		Status:       "pass",
		Summary:      "qa needs a human call on environment parity",
		NextAction:   "await_human",
		ArtifactRefs: artifactRefsFor("qa-blocked-02"),
	})
	if err != nil {
		t.Fatalf("handle qa result: %v", err)
	}

	if updatedSnapshot.Status != "blocked" || result.TaskStatus != "blocked" {
		t.Fatalf("expected qa human gate to block the task, got snapshot=%+v result=%+v", updatedSnapshot, result)
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "blocked" || !taskRow.NeedsHuman {
		t.Fatalf("expected blocked qa task row, got %+v", taskRow)
	}
	if taskRow.HumanReason == nil || !strings.Contains(*taskRow.HumanReason, "await_human") {
		t.Fatalf("unexpected blocked qa human_reason: %#v", taskRow.HumanReason)
	}

	assertOrchestratorEventTypes(t, storeService, []string{eventTaskBlocked})
}

func TestNormalizedReviewerLens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{
			name: "default lens",
			want: "correctness",
		},
		{
			name:  "canonicalizes supported lenses",
			input: " Correctness ",
			want:  "correctness",
		},
		{
			name:    "rejects invalid lens",
			input:   "ops",
			wantErr: `invalid reviewer lens "ops"`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizedReviewerLens(tc.input)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalized reviewer lens: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected lens: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestRunReviewStageRejectsBlockingDecisionWithoutFindings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		status     string
		nextAction string
	}{
		{
			name:       "request changes without findings",
			status:     "needs_changes",
			nextAction: "return_to_developer",
		},
		{
			name:       "await human without findings",
			status:     "blocked",
			nextAction: "await_human",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			storeService := openOrchestratorTestStore(t)
			worktreePath := t.TempDir()
			insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
				SprintID:          "Sprint-04",
				SequenceNo:        4,
				GitHubIssueNumber: 401,
				Status:            "in_progress",
			})
			insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
				TaskID:                  "Sprint-04/Task-02",
				SprintID:                "Sprint-04",
				TaskLocalID:             "Task-02",
				SequenceNo:              2,
				GitHubIssueNumber:       402,
				ParentGitHubIssueNumber: 401,
				Status:                  "review_in_progress",
				AttemptTotal:            2,
				TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
				WorktreePath:            &worktreePath,
			})

			db, err := storeService.DB()
			if err != nil {
				t.Fatalf("store db: %v", err)
			}
			snapshot, err := loadTaskRuntimeSnapshot(context.Background(), db, "Sprint-04/Task-02")
			if err != nil {
				t.Fatalf("load task runtime: %v", err)
			}

			reviewRefs := artifactRefsFor("review-missing-findings-02")
			runner := &fakeAgentRunner{
				t: t,
				steps: []fakeAgentStep{
					{
						agentType: agentrun.AgentReviewer,
						attempt:   2,
						lens:      "correctness",
						result: agentrun.Result{
							Status:       tc.status,
							Summary:      "review requests a blocking action without structured findings",
							NextAction:   tc.nextAction,
							ArtifactRefs: reviewRefs,
						},
					},
				},
			}

			service := New(Dependencies{
				Store:       storeService,
				AgentRunner: runner,
			})

			result, _, err := service.runReviewStage(context.Background(), snapshot, RunTaskOptions{}, "correctness", artifactRefsFor("qa-02"))
			if err != nil {
				t.Fatalf("run review stage: %v", err)
			}
			runner.assertExhausted(t)

			if result.Status != "failed" || result.NextAction != "retry" {
				t.Fatalf("expected malformed review output to request retry, got %+v", result)
			}
			if result.FailureFingerprint != agentrun.ErrorCodeMalformedOutput {
				t.Fatalf("unexpected failure fingerprint: %+v", result)
			}
			if result.ArtifactRefs.Report != reviewRefs.Report {
				t.Fatalf("expected reviewer artifact refs to be preserved, got %+v", result.ArtifactRefs)
			}
			if !strings.Contains(result.Summary, "without structured findings") {
				t.Fatalf("unexpected retry summary: %s", result.Summary)
			}
		})
	}
}

func TestRunReviewStageRejectsInvalidFindingEnums(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		severity   string
		confidence string
	}{
		{
			name:       "invalid severity",
			severity:   "urgent",
			confidence: "high",
		},
		{
			name:       "invalid confidence",
			severity:   "high",
			confidence: "certain",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			storeService := openOrchestratorTestStore(t)
			worktreePath := t.TempDir()
			insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
				SprintID:          "Sprint-04",
				SequenceNo:        4,
				GitHubIssueNumber: 401,
				Status:            "in_progress",
			})
			insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
				TaskID:                  "Sprint-04/Task-02",
				SprintID:                "Sprint-04",
				TaskLocalID:             "Task-02",
				SequenceNo:              2,
				GitHubIssueNumber:       402,
				ParentGitHubIssueNumber: 401,
				Status:                  "review_in_progress",
				AttemptTotal:            2,
				TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
				WorktreePath:            &worktreePath,
			})

			db, err := storeService.DB()
			if err != nil {
				t.Fatalf("store db: %v", err)
			}
			snapshot, err := loadTaskRuntimeSnapshot(context.Background(), db, "Sprint-04/Task-02")
			if err != nil {
				t.Fatalf("load task runtime: %v", err)
			}

			reviewRefs := artifactRefsFor("review-invalid-enum-02")
			runner := &fakeAgentRunner{
				t: t,
				steps: []fakeAgentStep{
					{
						agentType: agentrun.AgentReviewer,
						attempt:   2,
						lens:      "correctness",
						result: agentrun.Result{
							Status:       "completed_with_findings",
							Summary:      "review reported an invalid finding enum",
							NextAction:   "needs_changes",
							ArtifactRefs: reviewRefs,
							Findings: []agentrun.Finding{
								{
									ReviewerID:         "reviewer-correctness",
									Lens:               "correctness",
									Severity:           tc.severity,
									Confidence:         tc.confidence,
									Category:           "correctness",
									FileRefs:           []string{"internal/orchestrator/run.go"},
									Summary:            "review finding enum should be rejected",
									Evidence:           "invalid finding enums would otherwise reach aggregation",
									FindingFingerprint: "review:invalid-enum",
									SuggestedAction:    "reject malformed finding payloads",
								},
							},
						},
					},
				},
			}

			service := New(Dependencies{
				Store:       storeService,
				AgentRunner: runner,
			})

			result, _, err := service.runReviewStage(context.Background(), snapshot, RunTaskOptions{}, "correctness", artifactRefsFor("qa-02"))
			if err != nil {
				t.Fatalf("run review stage: %v", err)
			}
			runner.assertExhausted(t)

			if result.Status != "failed" || result.NextAction != "retry" {
				t.Fatalf("expected invalid finding enum to request retry, got %+v", result)
			}
			if result.FailureFingerprint != agentrun.ErrorCodeMalformedOutput {
				t.Fatalf("unexpected failure fingerprint: %+v", result)
			}
			if result.ArtifactRefs.Report != reviewRefs.Report {
				t.Fatalf("expected reviewer artifact refs to be preserved, got %+v", result.ArtifactRefs)
			}
		})
	}
}

func TestHandleDeveloperFailureMarksBlockedTaskForHumanHandoff(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "dev_in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	db, err := storeService.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	snapshot, err := loadTaskRuntimeSnapshot(context.Background(), db, "Sprint-04/Task-02")
	if err != nil {
		t.Fatalf("load task runtime: %v", err)
	}

	service := New(Dependencies{Store: storeService})
	result, updatedSnapshot, err := service.handleDeveloperResult(context.Background(), snapshot, StageResult{
		Stage:        StageDeveloper,
		AgentType:    agentrun.AgentDeveloper,
		Attempt:      1,
		Status:       "failed",
		Summary:      "developer needs human input on unresolved requirement",
		NextAction:   "await_human",
		ArtifactRefs: artifactRefsFor("developer-blocked-01"),
	})
	if err != nil {
		t.Fatalf("handle developer result: %v", err)
	}

	if updatedSnapshot.Status != "blocked" {
		t.Fatalf("unexpected updated task status: %s", updatedSnapshot.Status)
	}
	if result.TaskStatus != "blocked" {
		t.Fatalf("unexpected stage result task status: %+v", result)
	}
	if result.FailureFingerprint == "" {
		t.Fatalf("expected blocked developer result to synthesize a failure fingerprint, got %+v", result)
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "blocked" || !taskRow.NeedsHuman {
		t.Fatalf("expected blocked task to require human handoff, got %+v", taskRow)
	}
	if taskRow.HumanReason == nil || !strings.Contains(*taskRow.HumanReason, "developer needs human input") || !strings.Contains(*taskRow.HumanReason, "await_human") {
		t.Fatalf("unexpected blocked task human_reason: %#v", taskRow.HumanReason)
	}
	if taskRow.CurrentFailureFingerprint == nil || *taskRow.CurrentFailureFingerprint != result.FailureFingerprint {
		t.Fatalf("unexpected blocked task fingerprint: row=%#v result=%q", taskRow.CurrentFailureFingerprint, result.FailureFingerprint)
	}

	assertOrchestratorEventTypes(t, storeService, []string{eventDeveloperBlocked})
}

func TestRunReviewStageDedupesDuplicateFindingsFromSingleReviewer(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "review_in_progress",
		AttemptTotal:            3,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	db, err := storeService.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	snapshot, err := loadTaskRuntimeSnapshot(context.Background(), db, "Sprint-04/Task-02")
	if err != nil {
		t.Fatalf("load task runtime: %v", err)
	}

	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentReviewer,
				attempt:   3,
				lens:      "correctness",
				result: agentrun.Result{
					Status:       "needs_changes",
					Summary:      "correctness review found a blocking issue",
					NextAction:   "return_to_developer",
					ArtifactRefs: artifactRefsFor("review-correctness-03"),
					Findings: []agentrun.Finding{
						{
							ReviewerID:         "reviewer-correctness",
							Lens:               "correctness",
							Severity:           "medium",
							Confidence:         "low",
							Category:           "correctness",
							FileRefs:           []string{"internal/orchestrator/run.go"},
							Summary:            "review stage should dedupe duplicate findings",
							Evidence:           "the reviewer reported the same fingerprint twice",
							FindingFingerprint: "review:orchestrator:duplicate-finding",
							SuggestedAction:    "keep the strongest duplicate finding",
						},
						{
							ReviewerID:         "reviewer-correctness",
							Lens:               "correctness",
							Severity:           "high",
							Confidence:         "medium",
							Category:           "correctness",
							FileRefs:           []string{"internal/orchestrator/run.go", "internal/orchestrator/service.go"},
							Summary:            "review stage should dedupe duplicate findings",
							Evidence:           "the duplicate carries stronger severity and confidence",
							FindingFingerprint: "review:orchestrator:duplicate-finding",
							SuggestedAction:    "keep the strongest duplicate finding",
						},
					},
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	stageResult, rawFindings, err := service.runReviewStage(context.Background(), snapshot, RunTaskOptions{}, "correctness", artifactRefsFor("qa-03"))
	if err != nil {
		t.Fatalf("run review stage: %v", err)
	}
	runner.assertExhausted(t)

	if stageResult.Status != "needs_changes" || stageResult.NextAction != "return_to_developer" {
		t.Fatalf("expected reviewer decision to be preserved, got %+v", stageResult)
	}
	if len(stageResult.Findings) != 1 {
		t.Fatalf("expected duplicate findings to collapse into one entry, got %+v", stageResult.Findings)
	}
	if stageResult.Findings[0].Severity != "high" || stageResult.Findings[0].Confidence != "medium" {
		t.Fatalf("expected dedupe to keep strongest severity/confidence from the same reviewer, got %+v", stageResult.Findings[0])
	}
	if len(rawFindings) != 1 {
		t.Fatalf("expected deduped findings to drive persistence, got %+v", rawFindings)
	}

	stageResult, updatedSnapshot, err := service.handleReviewResult(context.Background(), snapshot, stageResult, rawFindings)
	if err != nil {
		t.Fatalf("handle review result: %v", err)
	}

	if updatedSnapshot.Status != "review_failed" || stageResult.TaskStatus != "review_failed" {
		t.Fatalf("expected reviewer finding to land on review_failed, got snapshot=%+v result=%+v", updatedSnapshot, stageResult)
	}
	if got := countReviewFindings(t, storeService, "Sprint-04/Task-02"); got != 1 {
		t.Fatalf("expected one persisted review finding after dedupe, got %d", got)
	}

	persisted := loadPersistedReviewFindings(t, storeService, "Sprint-04/Task-02")
	if len(persisted) != 1 {
		t.Fatalf("unexpected persisted review findings: %+v", persisted)
	}
	if persisted[0].ReviewerID != "reviewer-correctness" || persisted[0].Severity != "high" {
		t.Fatalf("expected persisted finding to keep reviewer attribution and strongest severity, got %+v", persisted[0])
	}

	payload := loadOrchestratorEventPayload(t, storeService, eventReviewAggregated)
	stageResultPayload, ok := payload["stage_result"].(map[string]any)
	if !ok {
		t.Fatalf("expected review_aggregated payload to contain stage_result, got %+v", payload)
	}
	findingsPayload, ok := stageResultPayload["findings"].([]any)
	if !ok || len(findingsPayload) != 1 {
		t.Fatalf("expected one deduped finding in the event payload, got %+v", stageResultPayload)
	}
	dedupedFinding, ok := findingsPayload[0].(map[string]any)
	if !ok {
		t.Fatalf("expected deduped finding payload, got %+v", findingsPayload[0])
	}
	if got := dedupedFinding["severity"]; got != "high" {
		t.Fatalf("expected event payload to keep the strongest deduped severity, got %#v", got)
	}
}

func TestHandleDeveloperPassAwaitHumanBlocksTask(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "dev_in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	db, err := storeService.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	snapshot, err := loadTaskRuntimeSnapshot(context.Background(), db, "Sprint-04/Task-02")
	if err != nil {
		t.Fatalf("load task runtime: %v", err)
	}

	service := New(Dependencies{Store: storeService})
	result, updatedSnapshot, err := service.handleDeveloperResult(context.Background(), snapshot, StageResult{
		Stage:        StageDeveloper,
		AgentType:    agentrun.AgentDeveloper,
		Attempt:      1,
		Status:       "success",
		Summary:      "developer finished code but needs a human requirement decision",
		NextAction:   "await_human",
		ArtifactRefs: artifactRefsFor("developer-blocked-02"),
	})
	if err != nil {
		t.Fatalf("handle developer result: %v", err)
	}

	if updatedSnapshot.Status != "blocked" || result.TaskStatus != "blocked" {
		t.Fatalf("expected developer human gate to block the task, got snapshot=%+v result=%+v", updatedSnapshot, result)
	}
	if result.FailureFingerprint == "" {
		t.Fatalf("expected developer human gate to synthesize a failure fingerprint, got %+v", result)
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "blocked" || !taskRow.NeedsHuman {
		t.Fatalf("expected blocked developer task row, got %+v", taskRow)
	}

	assertOrchestratorEventTypes(t, storeService, []string{eventDeveloperBlocked})
}

func TestHandleReviewPassClearsFailureAndHumanMetadata(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                    "Sprint-04/Task-02",
		SprintID:                  "Sprint-04",
		TaskLocalID:               "Task-02",
		SequenceNo:                2,
		GitHubIssueNumber:         402,
		ParentGitHubIssueNumber:   401,
		Status:                    "review_in_progress",
		AttemptTotal:              2,
		ReviewFailCount:           2,
		CurrentFailureFingerprint: stringPtr("review:stale"),
		TaskBranch:                stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:              &worktreePath,
		NeedsHuman:                true,
		HumanReason:               stringPtr("stale human gate"),
	})

	db, err := storeService.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	snapshot, err := loadTaskRuntimeSnapshot(context.Background(), db, "Sprint-04/Task-02")
	if err != nil {
		t.Fatalf("load task runtime: %v", err)
	}

	service := New(Dependencies{Store: storeService})
	result, updatedSnapshot, err := service.handleReviewResult(context.Background(), snapshot, StageResult{
		Stage:        StageReview,
		AgentType:    agentrun.AgentReviewer,
		Attempt:      2,
		Status:       "pass",
		Summary:      "review passed",
		NextAction:   "open_task_pr",
		ArtifactRefs: artifactRefsFor("review-pass-02"),
	}, nil)
	if err != nil {
		t.Fatalf("handle review result: %v", err)
	}

	if updatedSnapshot.Status != "pr_open" {
		t.Fatalf("unexpected updated task status: %s", updatedSnapshot.Status)
	}
	if result.TaskStatus != "pr_open" {
		t.Fatalf("unexpected review stage result task status: %+v", result)
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "pr_open" {
		t.Fatalf("unexpected task status after review pass: %+v", taskRow)
	}
	if taskRow.ReviewFailCount != 0 {
		t.Fatalf("expected review fail count reset, got %d", taskRow.ReviewFailCount)
	}
	if taskRow.CurrentFailureFingerprint != nil {
		t.Fatalf("expected failure fingerprint to clear after review pass, got %#v", taskRow.CurrentFailureFingerprint)
	}
	if taskRow.NeedsHuman {
		t.Fatalf("expected needs_human to clear after review pass, got %+v", taskRow)
	}
	if taskRow.HumanReason != nil {
		t.Fatalf("expected human_reason to clear after review pass, got %#v", taskRow.HumanReason)
	}

	assertOrchestratorEventTypes(t, storeService, []string{eventReviewAggregated})
}

func TestRunTaskUsesConfiguredSingleReviewerLens(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   1,
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer finished implementation",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("developer-01"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   1,
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "qa passed",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("qa-01"),
				},
			},
			{
				agentType: agentrun.AgentReviewer,
				attempt:   1,
				lens:      "architecture",
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "architecture review passed",
					NextAction:   "open_task_pr",
					ArtifactRefs: artifactRefsFor("review-01"),
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID:       "Sprint-04/Task-02",
		ReviewerLens: " architecture ",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "pr_open" || result.NextAction != "open_task_pr" {
		t.Fatalf("unexpected orchestrator result: %+v", result)
	}
	if len(result.StageResults) != 3 || result.StageResults[2].TaskStatus != "pr_open" {
		t.Fatalf("unexpected review stage result: %+v", result.StageResults)
	}
}

func TestRunTaskMovesReviewerEscalationToAwaitingHuman(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   1,
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer finished implementation",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("developer-01"),
				},
			},
			{
				agentType: agentrun.AgentQA,
				attempt:   1,
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "qa passed",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("qa-01"),
				},
			},
			{
				agentType: agentrun.AgentReviewer,
				attempt:   1,
				lens:      "correctness",
				result: agentrun.Result{
					Status:       "blocked",
					Summary:      "review needs a human decision on a low-confidence race condition",
					NextAction:   "await_human",
					ArtifactRefs: artifactRefsFor("review-01"),
					Findings: []agentrun.Finding{
						{
							ReviewerID:         "reviewer-correctness",
							Lens:               "correctness",
							Severity:           "medium",
							Confidence:         "low",
							Category:           "correctness",
							FileRefs:           []string{"internal/orchestrator/run.go"},
							Summary:            "possible race on task state",
							Evidence:           "state is updated after async work completes",
							FindingFingerprint: "correctness:orchestrator:possible-race",
							SuggestedAction:    "review manually before opening the task PR",
						},
					},
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID: "Sprint-04/Task-02",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "awaiting_human" || result.NextAction != "await_human" {
		t.Fatalf("unexpected orchestrator result: %+v", result)
	}
	if len(result.StageResults) != 3 || result.StageResults[2].TaskStatus != "awaiting_human" {
		t.Fatalf("unexpected review stage result: %+v", result.StageResults)
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "awaiting_human" || !taskRow.NeedsHuman {
		t.Fatalf("expected awaiting_human task row, got %+v", taskRow)
	}

	assertOrchestratorEventTypes(t, storeService, []string{
		eventDeveloperStarted,
		eventDeveloperCompleted,
		eventQAPassed,
		eventReviewStarted,
		eventReviewAggregated,
		eventTaskAwaitingHuman,
	})
}

func TestRunTaskIgnoresLateQAResultAfterTaskMovesToAwaitingHuman(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "qa_in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	base, err := storeService.BaseStore()
	if err != nil {
		t.Fatalf("store base: %v", err)
	}

	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentQA,
				attempt:   1,
				assert: func(t *testing.T, _ agentrun.Request, _ agentrun.ExecuteOptions) {
					if _, err := base.UpdateTaskState(context.Background(), store.UpdateTaskStatePayload{
						TaskID:                    "Sprint-04/Task-02",
						Status:                    "awaiting_human",
						CurrentFailureFingerprint: stringPtr("manual:pause"),
						NeedsHuman:                boolPtr(true),
						HumanReason:               stringPtr("manual pause"),
					}); err != nil {
						t.Fatalf("set awaiting_human: %v", err)
					}
				},
				result: agentrun.Result{
					Status:       "pass",
					Summary:      "qa passed",
					NextAction:   "proceed",
					ArtifactRefs: artifactRefsFor("qa-01"),
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID: "Sprint-04/Task-02",
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "awaiting_human" || result.NextAction != "await_human" {
		t.Fatalf("expected late qa result to be ignored, got %+v", result)
	}
	if len(result.StageResults) != 0 {
		t.Fatalf("expected no persisted stage results after late qa write rejection, got %+v", result.StageResults)
	}
	if got := countOrchestratorEvents(t, storeService); got != 0 {
		t.Fatalf("expected no new events after late qa write rejection, got %d", got)
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "awaiting_human" || !taskRow.NeedsHuman {
		t.Fatalf("expected task row to remain awaiting_human, got %+v", taskRow)
	}
}

func TestEnterDeveloperStageRejectsStaleRetrySourceStatus(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "qa_failed",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	db, err := storeService.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	snapshot, err := loadTaskRuntimeSnapshot(context.Background(), db, "Sprint-04/Task-02")
	if err != nil {
		t.Fatalf("load task runtime: %v", err)
	}

	base, err := storeService.BaseStore()
	if err != nil {
		t.Fatalf("store base: %v", err)
	}
	if _, err := base.UpdateTaskState(context.Background(), store.UpdateTaskStatePayload{
		TaskID:                    "Sprint-04/Task-02",
		Status:                    "awaiting_human",
		CurrentFailureFingerprint: stringPtr("manual:pause"),
		NeedsHuman:                boolPtr(true),
		HumanReason:               stringPtr("manual pause"),
	}); err != nil {
		t.Fatalf("set awaiting_human: %v", err)
	}

	service := New(Dependencies{Store: storeService})
	_, err = service.enterDeveloperStage(context.Background(), snapshot, true)
	var rejected *stageWriteRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("expected stage write rejection, got %v", err)
	}
	if rejected.Current.Status != "awaiting_human" {
		t.Fatalf("unexpected rejection snapshot: %+v", rejected.Current)
	}
	if got := countOrchestratorEvents(t, storeService); got != 0 {
		t.Fatalf("expected no retry events after rejection, got %d", got)
	}
}

func TestRecordTaskEscalatedRejectsStaleSnapshot(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "qa_in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	db, err := storeService.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	snapshot, err := loadTaskRuntimeSnapshot(context.Background(), db, "Sprint-04/Task-02")
	if err != nil {
		t.Fatalf("load task runtime: %v", err)
	}

	base, err := storeService.BaseStore()
	if err != nil {
		t.Fatalf("store base: %v", err)
	}
	if _, err := base.UpdateTaskState(context.Background(), store.UpdateTaskStatePayload{
		TaskID:                    "Sprint-04/Task-02",
		Status:                    "awaiting_human",
		CurrentFailureFingerprint: stringPtr("manual:pause"),
		NeedsHuman:                boolPtr(true),
		HumanReason:               stringPtr("manual pause"),
	}); err != nil {
		t.Fatalf("set awaiting_human: %v", err)
	}

	service := New(Dependencies{Store: storeService})
	_, err = service.recordTaskEscalated(
		context.Background(),
		snapshot,
		"task exceeded max stage transitions (1) while in qa_in_progress",
		"orchestrator:max_stage_transitions:qa_in_progress",
		1,
		StageResult{},
		false,
	)
	var rejected *stageWriteRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("expected stage write rejection, got %v", err)
	}
	if rejected.Current.Status != "awaiting_human" {
		t.Fatalf("unexpected rejection snapshot: %+v", rejected.Current)
	}
	if got := countOrchestratorEvents(t, storeService); got != 0 {
		t.Fatalf("expected no escalation events after stale snapshot rejection, got %d", got)
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "awaiting_human" || !taskRow.NeedsHuman {
		t.Fatalf("expected task row to preserve concurrent awaiting_human update, got %+v", taskRow)
	}
	if taskRow.HumanReason == nil || *taskRow.HumanReason != "manual pause" {
		t.Fatalf("unexpected preserved human_reason: %#v", taskRow.HumanReason)
	}
}

func TestRunTaskEscalatesWhenMaxStageTransitionsExceeded(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	developerRefs := artifactRefsFor("developer-01")
	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentDeveloper,
				attempt:   1,
				result: agentrun.Result{
					Status:       "success",
					Summary:      "developer finished implementation",
					NextAction:   "proceed",
					ArtifactRefs: developerRefs,
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID:              "Sprint-04/Task-02",
		MaxStageTransitions: 1,
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "escalated" {
		t.Fatalf("expected escalated terminal status, got %+v", result)
	}
	if result.NextAction != "stop" {
		t.Fatalf("expected stop next action on escalation, got %+v", result)
	}
	if result.Stage != StageDeveloper {
		t.Fatalf("expected latest completed stage to be developer, got %+v", result)
	}
	if result.ArtifactRefs.Report != developerRefs.Report {
		t.Fatalf("expected developer artifact refs to be preserved, got %+v", result.ArtifactRefs)
	}
	wantFingerprint := "orchestrator:max_stage_transitions:qa_in_progress"
	if result.FailureFingerprint != wantFingerprint {
		t.Fatalf("unexpected escalation failure fingerprint: got %q want %q", result.FailureFingerprint, wantFingerprint)
	}
	if !strings.Contains(result.Summary, "max stage transitions (1)") {
		t.Fatalf("unexpected escalation summary: %s", result.Summary)
	}

	taskRow := loadOrchestratorTaskRow(t, storeService, "Sprint-04/Task-02")
	if taskRow.Status != "escalated" {
		t.Fatalf("expected task row to persist escalated, got %+v", taskRow)
	}
	if taskRow.CurrentFailureFingerprint == nil || *taskRow.CurrentFailureFingerprint != wantFingerprint {
		t.Fatalf("unexpected persisted failure fingerprint: %#v", taskRow.CurrentFailureFingerprint)
	}
	if !taskRow.NeedsHuman {
		t.Fatalf("expected escalation to mark needs_human, got %+v", taskRow)
	}
	if taskRow.HumanReason == nil || !strings.Contains(*taskRow.HumanReason, "max stage transitions (1)") {
		t.Fatalf("unexpected escalation human_reason: %#v", taskRow.HumanReason)
	}

	assertOrchestratorEventTypes(t, storeService, []string{
		eventDeveloperStarted,
		eventDeveloperCompleted,
		eventTaskEscalated,
	})
}

func TestRunTaskEscalationPersistsLatestStageArtifactsForRecovery(t *testing.T) {
	t.Parallel()

	storeService := openOrchestratorTestStore(t)
	worktreePath := t.TempDir()
	insertOrchestratorSprintRow(t, storeService, orchestratorSprintSeed{
		SprintID:          "Sprint-04",
		SequenceNo:        4,
		GitHubIssueNumber: 401,
		Status:            "in_progress",
	})
	insertOrchestratorTaskRow(t, storeService, orchestratorTaskSeed{
		TaskID:                  "Sprint-04/Task-02",
		SprintID:                "Sprint-04",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       402,
		ParentGitHubIssueNumber: 401,
		Status:                  "qa_in_progress",
		AttemptTotal:            2,
		TaskBranch:              stringPtr("task/Sprint-04/Task-02"),
		WorktreePath:            &worktreePath,
	})

	qaRefs := artifactRefsFor("qa-timeout-02")
	runner := &fakeAgentRunner{
		t: t,
		steps: []fakeAgentStep{
			{
				agentType: agentrun.AgentQA,
				attempt:   2,
				result: agentrun.Result{
					Status:             "timeout",
					Summary:            "qa timed out while running focused validation",
					NextAction:         "retry",
					FailureFingerprint: agentrun.ErrorCodeRunnerTimeout,
					ArtifactRefs:       qaRefs,
				},
			},
		},
	}

	service := New(Dependencies{
		Store:       storeService,
		AgentRunner: runner,
	})

	result, err := service.RunTask(context.Background(), RunTaskOptions{
		TaskID:              "Sprint-04/Task-02",
		MaxStageTransitions: 1,
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	runner.assertExhausted(t)

	if result.Status != "escalated" || result.Stage != StageQA {
		t.Fatalf("unexpected escalated result: %+v", result)
	}
	if result.ArtifactRefs.Report != qaRefs.Report {
		t.Fatalf("expected qa artifact refs on escalation result, got %+v", result.ArtifactRefs)
	}
	if result.FailureFingerprint != agentrun.ErrorCodeRunnerTimeout {
		t.Fatalf("unexpected escalation failure fingerprint: %+v", result)
	}

	payload := loadOrchestratorEventPayload(t, storeService, eventTaskEscalated)
	stageResultPayload, ok := payload["stage_result"].(map[string]any)
	if !ok {
		t.Fatalf("expected escalated event to persist stage_result, got %+v", payload)
	}
	if got := stageResultPayload["stage"]; got != string(StageQA) {
		t.Fatalf("expected escalated stage_result stage %q, got %#v", StageQA, got)
	}
	artifactRefsPayload, ok := stageResultPayload["artifact_refs"].(map[string]any)
	if !ok {
		t.Fatalf("expected escalated stage_result artifact_refs, got %+v", stageResultPayload)
	}
	if got := artifactRefsPayload["report"]; got != qaRefs.Report {
		t.Fatalf("expected escalated stage_result report %q, got %#v", qaRefs.Report, got)
	}

	db, err := storeService.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	recoveredRefs, err := loadLatestStageArtifactRefs(context.Background(), db, "Sprint-04/Task-02")
	if err != nil {
		t.Fatalf("load latest stage artifact refs: %v", err)
	}
	if recoveredRefs.LastStage != StageQA {
		t.Fatalf("expected escalation recovery to resume from qa, got %+v", recoveredRefs)
	}
	if recoveredRefs.QA.Report != qaRefs.Report || recoveredRefs.LastStageArtifactRefs.Report != qaRefs.Report {
		t.Fatalf("expected escalation recovery to keep latest qa refs, got %+v", recoveredRefs)
	}

	assertOrchestratorEventTypes(t, storeService, []string{eventTaskEscalated})
}

func openOrchestratorTestStore(t *testing.T) *store.Service {
	t.Helper()

	service := store.New(store.Dependencies{})
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	repoRoot := repoRootForOrchestratorTests(t)
	if err := service.Open(context.Background(), store.OpenOptions{
		ConfigPath:   filepath.Join(repoRoot, "config", "config.yaml"),
		DatabasePath: filepath.Join(t.TempDir(), "toolhub.db"),
	}); err != nil {
		t.Fatalf("open store: %v", err)
	}
	return service
}

func repoRootForOrchestratorTests(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

type orchestratorSprintSeed struct {
	SprintID          string
	SequenceNo        int
	GitHubIssueNumber int
	Status            string
}

type orchestratorTaskSeed struct {
	TaskID                    string
	SprintID                  string
	TaskLocalID               string
	SequenceNo                int
	GitHubIssueNumber         int
	ParentGitHubIssueNumber   int
	Status                    string
	AttemptTotal              int
	QAFailCount               int
	ReviewFailCount           int
	CurrentFailureFingerprint *string
	TaskBranch                *string
	WorktreePath              *string
	NeedsHuman                bool
	HumanReason               *string
}

func insertOrchestratorSprintRow(t *testing.T, service *store.Service, seed orchestratorSprintSeed) {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	createdAt := "2026-03-07T00:00:00Z"
	if _, err := db.ExecContext(
		context.Background(),
		`INSERT INTO sprints (
			sprint_id,
			sequence_no,
			github_issue_number,
			github_issue_node_id,
			title,
			body_md,
			goal,
			done_when_json,
			status,
			sprint_branch,
			active_sprint_pr_number,
			timeline_log_path,
			needs_human,
			human_reason,
			opened_at,
			closed_at,
			last_issue_sync_at,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seed.SprintID,
		seed.SequenceNo,
		seed.GitHubIssueNumber,
		seed.SprintID+"-node",
		seed.SprintID+" title",
		"body",
		"goal",
		`["done"]`,
		seed.Status,
		"sprint/"+seed.SprintID,
		nil,
		"logs/"+seed.SprintID+".log",
		0,
		nil,
		nil,
		nil,
		nil,
		createdAt,
		createdAt,
	); err != nil {
		t.Fatalf("insert sprint row: %v", err)
	}
}

func insertOrchestratorTaskRow(t *testing.T, service *store.Service, seed orchestratorTaskSeed) {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	createdAt := "2026-03-07T00:00:00Z"
	if _, err := db.ExecContext(
		context.Background(),
		`INSERT INTO tasks (
			task_id,
			sprint_id,
			task_local_id,
			sequence_no,
			github_issue_number,
			github_issue_node_id,
			parent_github_issue_number,
			title,
			body_md,
			goal,
			acceptance_criteria_json,
			out_of_scope_json,
			status,
			attempt_total,
			qa_fail_count,
			review_fail_count,
			ci_fail_count,
			current_failure_fingerprint,
			active_pr_number,
			task_branch,
			worktree_path,
			needs_human,
			human_reason,
			opened_at,
			closed_at,
			last_issue_sync_at,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seed.TaskID,
		seed.SprintID,
		seed.TaskLocalID,
		seed.SequenceNo,
		seed.GitHubIssueNumber,
		seed.TaskID+"-node",
		seed.ParentGitHubIssueNumber,
		seed.TaskID+" title",
		"body",
		"goal",
		`["ship it"]`,
		`["none"]`,
		seed.Status,
		seed.AttemptTotal,
		seed.QAFailCount,
		seed.ReviewFailCount,
		0,
		seed.CurrentFailureFingerprint,
		nil,
		seed.TaskBranch,
		seed.WorktreePath,
		boolToInt(seed.NeedsHuman),
		seed.HumanReason,
		nil,
		nil,
		nil,
		createdAt,
		createdAt,
	); err != nil {
		t.Fatalf("insert task row: %v", err)
	}
}

func appendOrchestratorStageEvent(
	t *testing.T,
	service *store.Service,
	taskID string,
	sprintID string,
	fromStatus string,
	toStatus string,
	eventType string,
	attempt int,
	result StageResult,
) {
	t.Helper()

	base, err := service.BaseStore()
	if err != nil {
		t.Fatalf("store base: %v", err)
	}

	if _, err := base.AppendEvent(context.Background(), store.AppendEventPayload{
		EventID:        taskEventID(taskID, eventType, attempt),
		EntityType:     "task",
		EntityID:       taskID,
		SprintID:       stringPtr(sprintID),
		TaskID:         stringPtr(taskID),
		EventType:      eventType,
		Source:         orchestratorSource,
		Attempt:        intPointer(attempt),
		IdempotencyKey: taskEventIDKey(taskID, eventType, attempt),
		PayloadJSON: map[string]any{
			"task_status_from": fromStatus,
			"task_status_to":   toStatus,
			"stage_result":     result,
		},
		OccurredAt: "2026-03-07T00:00:00Z",
	}); err != nil {
		t.Fatalf("append stage event %s: %v", eventType, err)
	}
}

type orchestratorTaskRow struct {
	Status                    string
	AttemptTotal              int
	QAFailCount               int
	ReviewFailCount           int
	CurrentFailureFingerprint *string
	NeedsHuman                bool
	HumanReason               *string
}

func loadOrchestratorTaskRow(t *testing.T, service *store.Service, taskID string) orchestratorTaskRow {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	var row orchestratorTaskRow
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT status, attempt_total, qa_fail_count, review_fail_count, current_failure_fingerprint, needs_human, human_reason FROM tasks WHERE task_id = ?`,
		taskID,
	).Scan(
		&row.Status,
		&row.AttemptTotal,
		&row.QAFailCount,
		&row.ReviewFailCount,
		&row.CurrentFailureFingerprint,
		&row.NeedsHuman,
		&row.HumanReason,
	); err != nil {
		t.Fatalf("load task row: %v", err)
	}
	return row
}

func assertOrchestratorEventTypes(t *testing.T, service *store.Service, want []string) {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	rows, err := db.QueryContext(context.Background(), `SELECT event_type FROM events ORDER BY rowid ASC`)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			t.Fatalf("scan event type: %v", err)
		}
		got = append(got, eventType)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate event rows: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected event count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected event order: got %v want %v", got, want)
		}
	}
}

func countOrchestratorEvents(t *testing.T, service *store.Service) int {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return count
}

func loadOrchestratorEventPayload(t *testing.T, service *store.Service, eventType string) map[string]any {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	var payloadJSON string
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT payload_json FROM events WHERE event_type = ? ORDER BY rowid DESC LIMIT 1`,
		eventType,
	).Scan(&payloadJSON); err != nil {
		t.Fatalf("load event payload for %s: %v", eventType, err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode event payload for %s: %v", eventType, err)
	}
	return payload
}

func countReviewFindings(t *testing.T, service *store.Service, taskID string) int {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	var count int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM review_findings WHERE task_id = ?`,
		taskID,
	).Scan(&count); err != nil {
		t.Fatalf("count review findings: %v", err)
	}
	return count
}

type persistedReviewFindingRow struct {
	ReviewerID         string
	Severity           string
	Confidence         string
	FindingFingerprint string
}

func loadPersistedReviewFindings(t *testing.T, service *store.Service, taskID string) []persistedReviewFindingRow {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	rows, err := db.QueryContext(
		context.Background(),
		`SELECT reviewer_id, severity, confidence, finding_fingerprint
		 FROM review_findings
		 WHERE task_id = ?
		 ORDER BY reviewer_id, finding_fingerprint`,
		taskID,
	)
	if err != nil {
		t.Fatalf("query review findings: %v", err)
	}
	defer rows.Close()

	var findings []persistedReviewFindingRow
	for rows.Next() {
		var finding persistedReviewFindingRow
		if err := rows.Scan(&finding.ReviewerID, &finding.Severity, &finding.Confidence, &finding.FindingFingerprint); err != nil {
			t.Fatalf("scan review finding: %v", err)
		}
		findings = append(findings, finding)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate review findings: %v", err)
	}
	return findings
}

func setOrchestratorTaskFailureFingerprint(t *testing.T, service *store.Service, taskID string, fingerprint string) {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	if _, err := db.ExecContext(
		context.Background(),
		`UPDATE tasks SET current_failure_fingerprint = ? WHERE task_id = ?`,
		fingerprint,
		taskID,
	); err != nil {
		t.Fatalf("set task failure fingerprint: %v", err)
	}
}

func artifactRefsFor(tag string) agentrun.ArtifactRefs {
	return agentrun.ArtifactRefs{
		Log:      ".toolhub/runs/" + tag + "/runner.log",
		Report:   ".toolhub/runs/" + tag + "/result.json",
		Worktree: ".",
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func stringPtr(value string) *string {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
