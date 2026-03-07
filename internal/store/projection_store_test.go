package store

import (
	"context"
	"database/sql"
	"testing"
)

func TestApplyGitHubProjectionRefreshesProjectedTables(t *testing.T) {
	base, service := openTestBaseStore(t)

	insertSprintRow(t, service, sprintSeed{
		SprintID:             "Sprint-02",
		SequenceNo:           2,
		GitHubIssueNumber:    102,
		Status:               "in_progress",
		SprintBranch:         stringPtr("sprint/old"),
		ActiveSprintPRNumber: intPtr(80),
	})
	insertTaskRow(t, service, taskSeed{
		TaskID:                  "Sprint-02/Task-01",
		SprintID:                "Sprint-02",
		TaskLocalID:             "Task-01",
		SequenceNo:              1,
		GitHubIssueNumber:       201,
		ParentGitHubIssueNumber: 102,
		Status:                  "dev_in_progress",
		AttemptTotal:            3,
		QAFailCount:             1,
		ReviewFailCount:         2,
		CIFailCount:             4,
		ActivePRNumber:          intPtr(90),
		TaskBranch:              stringPtr("task/old"),
		WorktreePath:            stringPtr("/tmp/task-01"),
	})
	insertTaskRow(t, service, taskSeed{
		TaskID:                  "Sprint-02/Task-99",
		SprintID:                "Sprint-02",
		TaskLocalID:             "Task-99",
		SequenceNo:              99,
		GitHubIssueNumber:       299,
		ParentGitHubIssueNumber: 102,
		Status:                  "todo",
	})

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO task_dependencies (task_id, depends_on_task_id, source, created_at)
		VALUES (?, ?, ?, ?)
	`, "Sprint-02/Task-99", "Sprint-02/Task-01", "github_issue_dependency", "2026-03-07T00:00:00Z"); err != nil {
		t.Fatalf("insert task dependency: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO pull_requests (
			github_pr_number, github_pr_node_id, pr_kind, sprint_id, task_id,
			head_branch, base_branch, status, auto_merge_enabled, head_sha, url,
			opened_at, closed_at, merged_at, last_synced_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 90, "PR_old_task", "task", "Sprint-02", "Sprint-02/Task-01", "task/old", "sprint/Sprint-02", "open", 0, "sha-old", "https://example/pr/90", "2026-03-07T00:00:00Z", nil, nil, "2026-03-07T00:00:00Z", "2026-03-07T00:00:00Z", "2026-03-07T00:00:00Z"); err != nil {
		t.Fatalf("insert old task pr: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO ci_runs (
			github_run_id, sprint_id, task_id, github_pr_number, workflow_name, head_sha,
			status, conclusion, html_url, started_at, completed_at, last_synced_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 9001, "Sprint-02", "Sprint-02/Task-01", 90, "CI", "sha-old", "completed", "success", "https://example/run/9001", "2026-03-07T00:00:00Z", "2026-03-07T00:10:00Z", "2026-03-07T00:10:00Z", "2026-03-07T00:10:00Z", "2026-03-07T00:10:00Z"); err != nil {
		t.Fatalf("insert old ci run: %v", err)
	}

	snapshot := GitHubProjectionSnapshot{
		Reason:   "manual",
		SyncedAt: "2026-03-07T12:00:00Z",
		RepoConfig: RepoConfigSnapshot{
			GitHubOwner:   "acme",
			GitHubRepo:    "quick-ai-toolhub",
			DefaultBranch: "main",
		},
		Sprints: []SprintDefinitionSnapshot{
			{
				SprintID:          "Sprint-02",
				SequenceNo:        2,
				GitHubIssueNumber: 102,
				GitHubIssueNodeID: "I_sprint_102",
				Title:             "GitHub task projection",
				BodyMD:            "## Sprint ID\n\nSprint-02\n",
				Goal:              "Project GitHub issues into SQLite",
				DoneWhen:          []string{"Projection refreshed"},
				OpenedAt:          "2026-03-01T00:00:00Z",
				LastIssueSyncAt:   "2026-03-07T12:00:00Z",
			},
		},
		Tasks: []TaskDefinitionSnapshot{
			{
				TaskID:                  "Sprint-02/Task-01",
				SprintID:                "Sprint-02",
				TaskLocalID:             "Task-01",
				SequenceNo:              1,
				GitHubIssueNumber:       201,
				GitHubIssueNodeID:       "I_task_201",
				ParentGitHubIssueNumber: 102,
				Title:                   "Implement github-sync-tool",
				BodyMD:                  "## Sprint ID\n\nSprint-02\n\n## Task ID\n\nTask-01\n",
				Goal:                    "Write projected rows",
				AcceptanceCriteria:      []string{"SQLite rows refreshed"},
				OutOfScope:              []string{"Webhook sync"},
				OpenedAt:                "2026-03-01T00:00:00Z",
				LastIssueSyncAt:         "2026-03-07T12:00:00Z",
			},
		},
		PullRequests: []PullRequestSnapshot{
			{
				GitHubPRNumber:   501,
				GitHubPRNodeID:   "PR_task_501",
				PRKind:           "task",
				SprintID:         "Sprint-02",
				TaskID:           stringPtr("Sprint-02/Task-01"),
				HeadBranch:       "task/Sprint-02/Task-01",
				BaseBranch:       "sprint/Sprint-02",
				Status:           "open",
				AutoMergeEnabled: true,
				HeadSHA:          "sha-task-501",
				URL:              "https://example/pr/501",
				OpenedAt:         "2026-03-07T12:00:00Z",
				LastSyncedAt:     "2026-03-07T12:00:00Z",
			},
			{
				GitHubPRNumber: 601,
				GitHubPRNodeID: "PR_sprint_601",
				PRKind:         "sprint",
				SprintID:       "Sprint-02",
				HeadBranch:     "sprint/Sprint-02",
				BaseBranch:     "main",
				Status:         "open",
				HeadSHA:        "sha-sprint-601",
				URL:            "https://example/pr/601",
				OpenedAt:       "2026-03-07T12:00:00Z",
				LastSyncedAt:   "2026-03-07T12:00:00Z",
			},
		},
		CIRuns: []CIRunSnapshot{
			{
				GitHubRunID:    7001,
				SprintID:       "Sprint-02",
				TaskID:         stringPtr("Sprint-02/Task-01"),
				GitHubPRNumber: intPtr(501),
				WorkflowName:   "CI",
				HeadSHA:        "sha-task-501",
				Status:         "completed",
				Conclusion:     "success",
				HTMLURL:        "https://example/run/7001",
				StartedAt:      "2026-03-07T12:01:00Z",
				CompletedAt:    "2026-03-07T12:05:00Z",
				LastSyncedAt:   "2026-03-07T12:05:00Z",
			},
		},
	}

	if err := base.ApplyGitHubProjection(context.Background(), snapshot); err != nil {
		t.Fatalf("apply github projection: %v", err)
	}

	if got := countRowsByValue(t, service, "tasks", "task_id", "Sprint-02/Task-99"); got != 0 {
		t.Fatalf("expected stale task row to be removed, got %d", got)
	}

	var (
		taskTitle       string
		taskStatus      string
		taskAttempt     int
		taskQAFails     int
		taskReviewFails int
		taskCIFails     int
		taskPRNumber    sql.NullInt64
		taskBranch      sql.NullString
	)
	if err := db.QueryRowContext(context.Background(), `
		SELECT title, status, attempt_total, qa_fail_count, review_fail_count, ci_fail_count, active_pr_number, task_branch
		FROM tasks
		WHERE task_id = ?
	`, "Sprint-02/Task-01").Scan(
		&taskTitle,
		&taskStatus,
		&taskAttempt,
		&taskQAFails,
		&taskReviewFails,
		&taskCIFails,
		&taskPRNumber,
		&taskBranch,
	); err != nil {
		t.Fatalf("load task row: %v", err)
	}
	if taskTitle != "Implement github-sync-tool" {
		t.Fatalf("unexpected task title: %s", taskTitle)
	}
	if taskStatus != "dev_in_progress" {
		t.Fatalf("expected task status to preserve runtime state, got %s", taskStatus)
	}
	if taskAttempt != 3 || taskQAFails != 1 || taskReviewFails != 2 || taskCIFails != 4 {
		t.Fatalf("expected runtime counters to be preserved, got attempt=%d qa=%d review=%d ci=%d", taskAttempt, taskQAFails, taskReviewFails, taskCIFails)
	}
	if !taskPRNumber.Valid || taskPRNumber.Int64 != 501 {
		t.Fatalf("expected active_pr_number to point at refreshed open PR, got %#v", taskPRNumber)
	}
	if !taskBranch.Valid || taskBranch.String != "task/Sprint-02/Task-01" {
		t.Fatalf("expected task branch to refresh from open PR, got %#v", taskBranch)
	}

	var sprintStatus string
	var sprintPRNumber sql.NullInt64
	var sprintBranch sql.NullString
	if err := db.QueryRowContext(context.Background(), `
		SELECT status, active_sprint_pr_number, sprint_branch
		FROM sprints
		WHERE sprint_id = ?
	`, "Sprint-02").Scan(&sprintStatus, &sprintPRNumber, &sprintBranch); err != nil {
		t.Fatalf("load sprint row: %v", err)
	}
	if sprintStatus != "in_progress" {
		t.Fatalf("expected sprint status to preserve runtime state, got %s", sprintStatus)
	}
	if !sprintPRNumber.Valid || sprintPRNumber.Int64 != 601 {
		t.Fatalf("expected active sprint PR number to refresh, got %#v", sprintPRNumber)
	}
	if !sprintBranch.Valid || sprintBranch.String != "sprint/Sprint-02" {
		t.Fatalf("expected sprint branch to refresh from open sprint PR, got %#v", sprintBranch)
	}

	var prCount int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM pull_requests`).Scan(&prCount); err != nil {
		t.Fatalf("count pull requests: %v", err)
	}
	if prCount != 2 {
		t.Fatalf("expected 2 refreshed pull requests, got %d", prCount)
	}

	var runCount int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM ci_runs`).Scan(&runCount); err != nil {
		t.Fatalf("count ci runs: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("expected 1 refreshed ci run, got %d", runCount)
	}

	var syncStateCount int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM sync_state WHERE name = ?`, "last_full_reconcile_at").Scan(&syncStateCount); err != nil {
		t.Fatalf("count sync state rows: %v", err)
	}
	if syncStateCount != 1 {
		t.Fatalf("expected sync_state to be updated, got %d rows", syncStateCount)
	}
}
