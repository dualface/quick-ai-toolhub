package githubsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	toolgithub "quick-ai-toolhub/internal/github"
	"quick-ai-toolhub/internal/store"
)

func TestExecuteFullReconcileProjectsGitHubState(t *testing.T) {
	baseStore, storeService := openProjectionStore(t)

	reader := fakeGitHubReader{
		sprintIssues: []toolgithub.Issue{
			sprintIssue(102, "Sprint-02", "GitHub task projection"),
		},
		openTaskIssues: []toolgithub.Issue{
			taskIssue(201, "Sprint-02", "Task-01", "Implement github-sync-tool"),
		},
		issues: map[int]toolgithub.Issue{
			201: taskIssue(201, "Sprint-02", "Task-01", "Implement github-sync-tool"),
		},
		subIssues: map[int][]toolgithub.IssueLink{
			102: {
				{GitHubIssueNumber: 201, GitHubIssueNodeID: "I_task_201", Title: "[Sprint-02][Task-01] Implement github-sync-tool"},
			},
		},
		pullRequests: []toolgithub.PullRequest{
			{
				GitHubPRNumber:   301,
				GitHubPRNodeID:   "PR_301",
				Title:            "Task PR",
				State:            "open",
				URL:              "https://example/pr/301",
				HeadBranch:       "task/Sprint-02/Task-01",
				HeadSHA:          "sha-task-301",
				BaseBranch:       "sprint/Sprint-02",
				AutoMergeEnabled: true,
				CreatedAt:        "2026-03-07T12:00:00Z",
			},
			{
				GitHubPRNumber: 401,
				GitHubPRNodeID: "PR_401",
				Title:          "Sprint PR",
				State:          "open",
				URL:            "https://example/pr/401",
				HeadBranch:     "sprint/Sprint-02",
				HeadSHA:        "sha-sprint-401",
				BaseBranch:     "main",
				CreatedAt:      "2026-03-07T12:10:00Z",
			},
		},
		workflowRuns: []toolgithub.WorkflowRun{
			{
				GitHubRunID:  501,
				Name:         "CI",
				WorkflowName: "CI",
				HeadBranch:   "task/Sprint-02/Task-01",
				HeadSHA:      "sha-task-301",
				Status:       "completed",
				Conclusion:   "success",
				URL:          "https://example/run/501",
				StartedAt:    "2026-03-07T12:01:00Z",
				UpdatedAt:    "2026-03-07T12:05:00Z",
			},
			{
				GitHubRunID:  601,
				Name:         "CI",
				WorkflowName: "CI",
				HeadBranch:   "sprint/Sprint-02",
				HeadSHA:      "sha-sprint-401",
				Status:       "completed",
				Conclusion:   "success",
				URL:          "https://example/run/601",
				StartedAt:    "2026-03-07T12:11:00Z",
				UpdatedAt:    "2026-03-07T12:15:00Z",
			},
		},
	}

	response := New(Dependencies{
		GitHub: reader,
		Store:  baseStore,
	}).Execute(context.Background(), Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	}, ExecuteOptions{
		WorkDir:       t.TempDir(),
		Repo:          "acme/quick-ai-toolhub",
		DefaultBranch: "main",
	})
	if !response.OK {
		t.Fatalf("expected full_reconcile to succeed, got %#v", response.Error)
	}
	if response.Data == nil {
		t.Fatal("expected response data")
	}
	if response.Data.SyncSummary.ChangedCount != 6 {
		t.Fatalf("expected 6 changed entities, got %d", response.Data.SyncSummary.ChangedCount)
	}

	db, err := storeService.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	assertCount(t, db, `SELECT COUNT(*) FROM sprints`, 1)
	assertCount(t, db, `SELECT COUNT(*) FROM tasks`, 1)
	assertCount(t, db, `SELECT COUNT(*) FROM pull_requests`, 2)
	assertCount(t, db, `SELECT COUNT(*) FROM ci_runs`, 2)
	assertCount(t, db, `SELECT COUNT(*) FROM sync_state WHERE name = 'last_full_reconcile_at'`, 1)

	var activePR sql.NullInt64
	if err := db.QueryRowContext(context.Background(), `
		SELECT active_pr_number FROM tasks WHERE task_id = ?
	`, "Sprint-02/Task-01").Scan(&activePR); err != nil {
		t.Fatalf("load active_pr_number: %v", err)
	}
	if !activePR.Valid || activePR.Int64 != 301 {
		t.Fatalf("expected active_pr_number=301, got %+v", activePR)
	}
}

func TestExecuteFullReconcileRejectsOrphanTaskIssues(t *testing.T) {
	baseStore, _ := openProjectionStore(t)

	response := New(Dependencies{
		GitHub: fakeGitHubReader{
			sprintIssues: []toolgithub.Issue{
				sprintIssue(102, "Sprint-02", "GitHub task projection"),
			},
			openTaskIssues: []toolgithub.Issue{
				taskIssue(201, "Sprint-02", "Task-01", "Implement github-sync-tool"),
				taskIssue(202, "Sprint-02", "Task-02", "Orphan task"),
			},
			issues: map[int]toolgithub.Issue{
				201: taskIssue(201, "Sprint-02", "Task-01", "Implement github-sync-tool"),
			},
			subIssues: map[int][]toolgithub.IssueLink{
				102: {
					{GitHubIssueNumber: 201, GitHubIssueNodeID: "I_task_201", Title: "[Sprint-02][Task-01] Implement github-sync-tool"},
				},
			},
		},
		Store: baseStore,
	}).Execute(context.Background(), Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	}, ExecuteOptions{
		WorkDir:       t.TempDir(),
		Repo:          "acme/quick-ai-toolhub",
		DefaultBranch: "main",
	})

	if response.OK {
		t.Fatal("expected orphan task validation to fail")
	}
	if response.Error == nil || response.Error.Code != ErrorCodeGitHubData {
		t.Fatalf("expected github_data_invalid error, got %#v", response.Error)
	}
	if !strings.Contains(response.Error.Message, "orphaned") {
		t.Fatalf("expected orphaned task message, got %q", response.Error.Message)
	}
}

func TestExecuteFullReconcileRejectsCrossSprintDependencies(t *testing.T) {
	baseStore, _ := openProjectionStore(t)

	response := New(Dependencies{
		GitHub: fakeGitHubReader{
			sprintIssues: []toolgithub.Issue{
				sprintIssue(102, "Sprint-02", "GitHub task projection"),
				sprintIssue(103, "Sprint-03", "Follow-up"),
			},
			openTaskIssues: []toolgithub.Issue{
				taskIssue(201, "Sprint-02", "Task-01", "Implement github-sync-tool"),
				taskIssue(301, "Sprint-03", "Task-01", "Follow-up task"),
			},
			issues: map[int]toolgithub.Issue{
				201: taskIssue(201, "Sprint-02", "Task-01", "Implement github-sync-tool"),
				301: taskIssue(301, "Sprint-03", "Task-01", "Follow-up task"),
			},
			subIssues: map[int][]toolgithub.IssueLink{
				102: {
					{GitHubIssueNumber: 201, GitHubIssueNodeID: "I_task_201", Title: "[Sprint-02][Task-01] Implement github-sync-tool"},
				},
				103: {
					{GitHubIssueNumber: 301, GitHubIssueNodeID: "I_task_301", Title: "[Sprint-03][Task-01] Follow-up task"},
				},
			},
			dependencies: map[int][]toolgithub.IssueLink{
				201: {
					{GitHubIssueNumber: 301, GitHubIssueNodeID: "I_task_301", Title: "[Sprint-03][Task-01] Follow-up task"},
				},
			},
		},
		Store: baseStore,
	}).Execute(context.Background(), Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	}, ExecuteOptions{
		WorkDir:       t.TempDir(),
		Repo:          "acme/quick-ai-toolhub",
		DefaultBranch: "main",
	})

	if response.OK {
		t.Fatal("expected cross-sprint dependency validation to fail")
	}
	if response.Error == nil || response.Error.Code != ErrorCodeGitHubData {
		t.Fatalf("expected github_data_invalid error, got %#v", response.Error)
	}
	if !strings.Contains(response.Error.Message, "cross-sprint dependency") {
		t.Fatalf("expected cross-sprint dependency message, got %q", response.Error.Message)
	}
}

func TestExecuteFullReconcileRejectsTaskNumberMismatch(t *testing.T) {
	baseStore, _ := openProjectionStore(t)

	mismatched := taskIssue(201, "Sprint-02", "Task-01", "Implement github-sync-tool")
	mismatched.Body = taskBody("Sprint-02", "Task-02", "Implement github-sync-tool")

	response := New(Dependencies{
		GitHub: fakeGitHubReader{
			sprintIssues: []toolgithub.Issue{
				sprintIssue(102, "Sprint-02", "GitHub task projection"),
			},
			openTaskIssues: []toolgithub.Issue{mismatched},
			issues: map[int]toolgithub.Issue{
				201: mismatched,
			},
			subIssues: map[int][]toolgithub.IssueLink{
				102: {
					{GitHubIssueNumber: 201, GitHubIssueNodeID: "I_task_201", Title: mismatched.Title},
				},
			},
		},
		Store: baseStore,
	}).Execute(context.Background(), Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	}, ExecuteOptions{
		WorkDir:       t.TempDir(),
		Repo:          "acme/quick-ai-toolhub",
		DefaultBranch: "main",
	})

	if response.OK {
		t.Fatal("expected task numbering validation to fail")
	}
	if response.Error == nil || response.Error.Code != ErrorCodeGitHubData {
		t.Fatalf("expected github_data_invalid error, got %#v", response.Error)
	}
	if !strings.Contains(response.Error.Message, "mismatched task id") {
		t.Fatalf("expected mismatched task id message, got %q", response.Error.Message)
	}
}

func TestExecuteReconcileIssueMatchesFullReconcile(t *testing.T) {
	reader := newIncrementalReader()
	targetBase, targetStore := openProjectionStore(t)
	fullBase, fullStore := openProjectionStore(t)

	runGitHubSync(t, targetBase, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})
	runGitHubSync(t, fullBase, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})

	updatedTask := taskIssue(201, "Sprint-02", "Task-01", "Implement incremental github-sync-tool")
	updatedTask.Labels = []string{"kind/task", "needs-human"}
	updatedTask.UpdatedAt = "2026-03-07T02:00:00Z"
	reader.openTaskIssues[0] = updatedTask
	reader.issues[201] = updatedTask

	response := runGitHubSync(t, targetBase, reader, Request{
		Op:      OpReconcileIssue,
		Payload: mustJSON(t, ReconcileIssuePayload{GitHubIssueNumber: 201}),
	})
	if !response.OK {
		t.Fatalf("expected reconcile_issue to succeed, got %#v", response.Error)
	}
	if response.Data == nil || response.Data.SyncSummary.ChangedCount == 0 {
		t.Fatalf("expected reconcile_issue to report changed entities, got %#v", response.Data)
	}

	runGitHubSync(t, fullBase, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})

	assertProjectedTablesMatch(t, targetStore, fullStore)
}

func TestExecuteReconcilePullRequestMatchesFullReconcile(t *testing.T) {
	reader := newIncrementalReader()
	targetBase, targetStore := openProjectionStore(t)
	fullBase, fullStore := openProjectionStore(t)

	runGitHubSync(t, targetBase, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})
	runGitHubSync(t, fullBase, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})

	reader.pullRequests[0].State = "merged"
	reader.pullRequests[0].ClosedAt = "2026-03-07T03:05:00Z"
	reader.pullRequests[0].MergedAt = "2026-03-07T03:05:00Z"
	reader.pullRequests[0].UpdatedAt = "2026-03-07T03:05:00Z"

	response := runGitHubSync(t, targetBase, reader, Request{
		Op:      OpReconcilePullReq,
		Payload: mustJSON(t, ReconcilePullRequestPayload{GitHubPRNumber: 301}),
	})
	if !response.OK {
		t.Fatalf("expected reconcile_pull_request to succeed, got %#v", response.Error)
	}
	if response.Data == nil || response.Data.SyncSummary.ChangedCount == 0 {
		t.Fatalf("expected reconcile_pull_request to report changed entities, got %#v", response.Data)
	}

	runGitHubSync(t, fullBase, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})

	assertProjectedTablesMatch(t, targetStore, fullStore)
}

func TestExecuteReconcileCIRunMatchesFullReconcile(t *testing.T) {
	reader := newIncrementalReader()
	targetBase, targetStore := openProjectionStore(t)
	fullBase, fullStore := openProjectionStore(t)

	runGitHubSync(t, targetBase, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})
	runGitHubSync(t, fullBase, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})

	reader.workflowRuns[0].Status = "completed"
	reader.workflowRuns[0].Conclusion = "success"
	reader.workflowRuns[0].UpdatedAt = "2026-03-07T04:05:00Z"

	response := runGitHubSync(t, targetBase, reader, Request{
		Op:      OpReconcileCIRun,
		Payload: mustJSON(t, ReconcileCIRunPayload{GitHubRunID: 501}),
	})
	if !response.OK {
		t.Fatalf("expected reconcile_ci_run to succeed, got %#v", response.Error)
	}
	if response.Data == nil || response.Data.SyncSummary.ChangedCount == 0 {
		t.Fatalf("expected reconcile_ci_run to report changed entities, got %#v", response.Data)
	}

	runGitHubSync(t, fullBase, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})

	assertProjectedTablesMatch(t, targetStore, fullStore)
}

func TestExecuteIngestWebhookDeduplicatesAndMatchesFullReconcile(t *testing.T) {
	reader := newIncrementalReader()
	targetBase, targetStore := openProjectionStore(t)
	fullBase, fullStore := openProjectionStore(t)

	runGitHubSync(t, targetBase, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})
	runGitHubSync(t, fullBase, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})

	updatedTask := taskIssue(201, "Sprint-02", "Task-01", "Implement incremental github-sync-tool")
	updatedTask.Labels = []string{"kind/task", "needs-human"}
	updatedTask.UpdatedAt = "2026-03-07T05:00:00Z"
	reader.openTaskIssues[0] = updatedTask
	reader.issues[201] = updatedTask

	webhookPayload := IngestWebhookPayload{
		DeliveryID: "delivery-1",
		EventName:  "issues",
		PayloadJSON: map[string]any{
			"action": "edited",
			"issue": map[string]any{
				"number":     updatedTask.GitHubIssueNumber,
				"node_id":    updatedTask.GitHubIssueNodeID,
				"title":      updatedTask.Title,
				"body":       updatedTask.Body,
				"state":      updatedTask.State,
				"html_url":   updatedTask.URL,
				"created_at": updatedTask.CreatedAt,
				"updated_at": updatedTask.UpdatedAt,
				"labels": []map[string]any{
					{"name": "kind/task"},
					{"name": "needs-human"},
				},
			},
		},
	}

	first := runGitHubSync(t, targetBase, reader, Request{
		Op:      OpIngestWebhook,
		Payload: mustJSON(t, webhookPayload),
	})
	if !first.OK {
		t.Fatalf("expected first ingest_webhook to succeed, got %#v", first.Error)
	}
	if first.Data == nil || first.Data.SyncSummary.ChangedCount == 0 {
		t.Fatalf("expected first ingest_webhook to project changes, got %#v", first.Data)
	}

	second := runGitHubSync(t, targetBase, reader, Request{
		Op:      OpIngestWebhook,
		Payload: mustJSON(t, webhookPayload),
	})
	if !second.OK {
		t.Fatalf("expected duplicate ingest_webhook to succeed, got %#v", second.Error)
	}
	if second.Data == nil || second.Data.SyncSummary.ChangedCount != 0 {
		t.Fatalf("expected duplicate ingest_webhook to be deduplicated, got %#v", second.Data)
	}

	db, err := targetStore.DB()
	if err != nil {
		t.Fatalf("target store db: %v", err)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM events WHERE idempotency_key = 'github_webhook:delivery-1'`, 1)

	runGitHubSync(t, fullBase, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})

	assertProjectedTablesMatch(t, targetStore, fullStore)
}

type fakeGitHubReader struct {
	sprintIssues   []toolgithub.Issue
	openTaskIssues []toolgithub.Issue
	issues         map[int]toolgithub.Issue
	subIssues      map[int][]toolgithub.IssueLink
	dependencies   map[int][]toolgithub.IssueLink
	pullRequests   []toolgithub.PullRequest
	workflowRuns   []toolgithub.WorkflowRun
}

func (f fakeGitHubReader) ListSprintIssues(context.Context, toolgithub.ListSprintIssuesRequest) ([]toolgithub.Issue, error) {
	return append([]toolgithub.Issue(nil), f.sprintIssues...), nil
}

func (f fakeGitHubReader) ListIssues(context.Context, toolgithub.ListIssuesRequest) ([]toolgithub.Issue, error) {
	return append([]toolgithub.Issue(nil), f.openTaskIssues...), nil
}

func (f fakeGitHubReader) GetIssue(_ context.Context, req toolgithub.GetIssueRequest) (toolgithub.Issue, error) {
	issue, ok := f.issues[req.GitHubIssueNumber]
	if !ok {
		return toolgithub.Issue{}, sql.ErrNoRows
	}
	return issue, nil
}

func (f fakeGitHubReader) ListSubIssues(_ context.Context, req toolgithub.ListSubIssuesRequest) ([]toolgithub.IssueLink, error) {
	return append([]toolgithub.IssueLink(nil), f.subIssues[req.ParentIssueNumber]...), nil
}

func (f fakeGitHubReader) ListIssueDependencies(_ context.Context, req toolgithub.ListIssueDependenciesRequest) ([]toolgithub.IssueLink, error) {
	return append([]toolgithub.IssueLink(nil), f.dependencies[req.GitHubIssueNumber]...), nil
}

func (f fakeGitHubReader) ListPullRequests(context.Context, toolgithub.ListPullRequestsRequest) ([]toolgithub.PullRequest, error) {
	return append([]toolgithub.PullRequest(nil), f.pullRequests...), nil
}

func (f fakeGitHubReader) GetPullRequest(_ context.Context, req toolgithub.GetPullRequestRequest) (toolgithub.PullRequest, error) {
	for _, item := range f.pullRequests {
		if item.GitHubPRNumber == req.GitHubPRNumber {
			return item, nil
		}
	}
	return toolgithub.PullRequest{}, sql.ErrNoRows
}

func (f fakeGitHubReader) ListWorkflowRuns(context.Context, toolgithub.ListWorkflowRunsRequest) ([]toolgithub.WorkflowRun, error) {
	return append([]toolgithub.WorkflowRun(nil), f.workflowRuns...), nil
}

func (f fakeGitHubReader) GetWorkflowRun(_ context.Context, req toolgithub.GetWorkflowRunRequest) (toolgithub.WorkflowRun, error) {
	for _, item := range f.workflowRuns {
		if item.GitHubRunID == req.GitHubRunID {
			return item, nil
		}
	}
	return toolgithub.WorkflowRun{}, sql.ErrNoRows
}

func sprintIssue(number int, sprintID, title string) toolgithub.Issue {
	return toolgithub.Issue{
		GitHubIssueNumber: number,
		GitHubIssueNodeID: "I_sprint_" + sprintID,
		Title:             "[" + sprintID + "] " + title,
		Body: strings.TrimSpace(`
## Sprint ID

` + sprintID + `

## Goal

Project GitHub state into SQLite.

## Done When

- SQLite projections refresh.
`),
		State:     "open",
		Labels:    []string{"kind/sprint"},
		CreatedAt: "2026-03-07T00:00:00Z",
		UpdatedAt: "2026-03-07T01:00:00Z",
	}
}

func taskIssue(number int, sprintID, taskID, title string) toolgithub.Issue {
	return toolgithub.Issue{
		GitHubIssueNumber: number,
		GitHubIssueNodeID: "I_task_" + taskID,
		Title:             "[" + sprintID + "][" + taskID + "] " + title,
		Body:              taskBody(sprintID, taskID, title),
		State:             "open",
		Labels:            []string{"kind/task"},
		CreatedAt:         "2026-03-07T00:00:00Z",
		UpdatedAt:         "2026-03-07T01:00:00Z",
	}
}

func taskBody(sprintID, taskID, title string) string {
	return strings.TrimSpace(`
## Sprint ID

` + sprintID + `

## Task ID

` + taskID + `

## Goal

` + title + `

## Acceptance Criteria

- Projection row exists.

## Out of Scope

- Webhook sync.
`)
}

func newIncrementalReader() *fakeGitHubReader {
	task := taskIssue(201, "Sprint-02", "Task-01", "Implement github-sync-tool")
	return &fakeGitHubReader{
		sprintIssues: []toolgithub.Issue{
			sprintIssue(102, "Sprint-02", "GitHub task projection"),
		},
		openTaskIssues: []toolgithub.Issue{task},
		issues: map[int]toolgithub.Issue{
			102: sprintIssue(102, "Sprint-02", "GitHub task projection"),
			201: task,
		},
		subIssues: map[int][]toolgithub.IssueLink{
			102: {
				{
					GitHubIssueNumber: 201,
					GitHubIssueNodeID: "I_task_201",
					Title:             task.Title,
					URL:               "https://example/issues/201",
				},
			},
		},
		pullRequests: []toolgithub.PullRequest{
			{
				GitHubPRNumber:   301,
				GitHubPRNodeID:   "PR_301",
				Title:            "Task PR",
				State:            "open",
				URL:              "https://example/pr/301",
				HeadBranch:       "task/Sprint-02/Task-01",
				HeadSHA:          "sha-task-301",
				BaseBranch:       "sprint/Sprint-02",
				AutoMergeEnabled: true,
				CreatedAt:        "2026-03-07T12:00:00Z",
				UpdatedAt:        "2026-03-07T12:00:00Z",
			},
			{
				GitHubPRNumber: 401,
				GitHubPRNodeID: "PR_401",
				Title:          "Sprint PR",
				State:          "open",
				URL:            "https://example/pr/401",
				HeadBranch:     "sprint/Sprint-02",
				HeadSHA:        "sha-sprint-401",
				BaseBranch:     "main",
				CreatedAt:      "2026-03-07T12:10:00Z",
				UpdatedAt:      "2026-03-07T12:10:00Z",
			},
		},
		workflowRuns: []toolgithub.WorkflowRun{
			{
				GitHubRunID:  501,
				Name:         "CI",
				WorkflowName: "CI",
				HeadBranch:   "task/Sprint-02/Task-01",
				HeadSHA:      "sha-task-301",
				Status:       "in_progress",
				URL:          "https://example/run/501",
				StartedAt:    "2026-03-07T12:01:00Z",
				UpdatedAt:    "2026-03-07T12:02:00Z",
			},
			{
				GitHubRunID:  601,
				Name:         "CI",
				WorkflowName: "CI",
				HeadBranch:   "sprint/Sprint-02",
				HeadSHA:      "sha-sprint-401",
				Status:       "completed",
				Conclusion:   "success",
				URL:          "https://example/run/601",
				StartedAt:    "2026-03-07T12:11:00Z",
				UpdatedAt:    "2026-03-07T12:15:00Z",
			},
		},
	}
}

func runGitHubSync(t *testing.T, base store.BaseStore, reader GitHubReader, request Request) Response {
	t.Helper()

	response := New(Dependencies{
		GitHub: reader,
		Store:  base,
	}).Execute(context.Background(), request, ExecuteOptions{
		WorkDir:       t.TempDir(),
		Repo:          "acme/quick-ai-toolhub",
		DefaultBranch: "main",
	})
	if !response.OK {
		t.Fatalf("github sync failed: %#v", response.Error)
	}
	return response
}

type rowSetQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func assertProjectedTablesMatch(t *testing.T, actualStore, expectedStore *store.Service) {
	t.Helper()

	actualDB, err := actualStore.DB()
	if err != nil {
		t.Fatalf("actual store db: %v", err)
	}
	expectedDB, err := expectedStore.DB()
	if err != nil {
		t.Fatalf("expected store db: %v", err)
	}

	queries := map[string]string{
		"sprints":           `SELECT sprint_id, sequence_no, github_issue_number, github_issue_node_id, title, body_md, goal, done_when_json, status, sprint_branch, active_sprint_pr_number, timeline_log_path, needs_human, human_reason, opened_at, closed_at FROM sprints ORDER BY sprint_id`,
		"tasks":             `SELECT task_id, sprint_id, task_local_id, sequence_no, github_issue_number, github_issue_node_id, parent_github_issue_number, title, body_md, goal, acceptance_criteria_json, out_of_scope_json, status, attempt_total, qa_fail_count, review_fail_count, ci_fail_count, current_failure_fingerprint, active_pr_number, task_branch, worktree_path, needs_human, human_reason, opened_at, closed_at FROM tasks ORDER BY task_id`,
		"task_dependencies": `SELECT task_id, depends_on_task_id, source FROM task_dependencies ORDER BY task_id, depends_on_task_id`,
		"pull_requests":     `SELECT github_pr_number, github_pr_node_id, pr_kind, sprint_id, task_id, head_branch, base_branch, status, auto_merge_enabled, head_sha, url, opened_at, closed_at, merged_at FROM pull_requests ORDER BY github_pr_number`,
		"ci_runs":           `SELECT github_run_id, sprint_id, task_id, github_pr_number, workflow_name, head_sha, status, conclusion, html_url, started_at, completed_at FROM ci_runs ORDER BY github_run_id`,
	}

	for name, query := range queries {
		actualRows := loadComparableRows(t, actualDB, query)
		expectedRows := loadComparableRows(t, expectedDB, query)
		if !reflect.DeepEqual(actualRows, expectedRows) {
			t.Fatalf("projection mismatch on %s:\nactual=%#v\nexpected=%#v", name, actualRows, expectedRows)
		}
	}
}

func loadComparableRows(t *testing.T, db rowSetQuerier, query string) []map[string]any {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns for %q: %v", query, err)
	}

	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatalf("scan row for %q: %v", query, err)
		}

		row := make(map[string]any, len(columns))
		for i, column := range columns {
			switch value := values[i].(type) {
			case []byte:
				row[column] = string(value)
			default:
				row[column] = value
			}
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rows for %q: %v", query, err)
	}
	return result
}

func openProjectionStore(t *testing.T) (store.BaseStore, *store.Service) {
	t.Helper()

	root := t.TempDir()
	writeProjectionTestFile(t, root, filepath.Join("config", "config.yaml"), "store: test\n")
	writeProjectionTestFile(t, root, filepath.Join("sql", "schema.sql"), loadProjectionSchema(t))

	service := store.New(store.Dependencies{})
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	if err := service.Open(context.Background(), store.OpenOptions{
		ConfigPath:   filepath.Join(root, "config", "config.yaml"),
		DatabasePath: ".toolhub/toolhub.db",
	}); err != nil {
		t.Fatalf("open store: %v", err)
	}

	base, err := service.BaseStore()
	if err != nil {
		t.Fatalf("base store: %v", err)
	}
	return base, service
}

func loadProjectionSchema(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}

	path := filepath.Join(filepath.Dir(file), "..", "..", "sql", "schema.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	return string(data)
}

func writeProjectionTestFile(t *testing.T, root, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func assertCount(t *testing.T, db rowQuerier, query string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRowContext(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("query count %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q returned %d rows, want %d", query, got, want)
	}
}
