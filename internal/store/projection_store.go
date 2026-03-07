package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uptrace/bun"
)

type RepoConfigSnapshot struct {
	GitHubOwner   string
	GitHubRepo    string
	DefaultBranch string
}

type SprintDefinitionSnapshot struct {
	SprintID          string
	SequenceNo        int
	GitHubIssueNumber int
	GitHubIssueNodeID string
	Title             string
	BodyMD            string
	Goal              string
	DoneWhen          []string
	NeedsHuman        bool
	HumanReason       *string
	OpenedAt          string
	ClosedAt          string
	LastIssueSyncAt   string
}

type TaskDefinitionSnapshot struct {
	TaskID                  string
	SprintID                string
	TaskLocalID             string
	SequenceNo              int
	GitHubIssueNumber       int
	GitHubIssueNodeID       string
	ParentGitHubIssueNumber int
	Title                   string
	BodyMD                  string
	Goal                    string
	AcceptanceCriteria      []string
	OutOfScope              []string
	NeedsHuman              bool
	HumanReason             *string
	OpenedAt                string
	ClosedAt                string
	LastIssueSyncAt         string
}

type TaskDependencySnapshot struct {
	TaskID          string
	DependsOnTaskID string
	Source          string
}

type PullRequestSnapshot struct {
	GitHubPRNumber   int
	GitHubPRNodeID   string
	PRKind           string
	SprintID         string
	TaskID           *string
	HeadBranch       string
	BaseBranch       string
	Status           string
	AutoMergeEnabled bool
	HeadSHA          string
	URL              string
	OpenedAt         string
	ClosedAt         string
	MergedAt         string
	LastSyncedAt     string
}

type CIRunSnapshot struct {
	GitHubRunID    int64
	SprintID       string
	TaskID         *string
	GitHubPRNumber *int
	WorkflowName   string
	HeadSHA        string
	Status         string
	Conclusion     string
	HTMLURL        string
	StartedAt      string
	CompletedAt    string
	LastSyncedAt   string
}

type GitHubProjectionSnapshot struct {
	Reason           string
	SyncedAt         string
	RepoConfig       RepoConfigSnapshot
	Sprints          []SprintDefinitionSnapshot
	Tasks            []TaskDefinitionSnapshot
	TaskDependencies []TaskDependencySnapshot
	PullRequests     []PullRequestSnapshot
	CIRuns           []CIRunSnapshot
}

func (s BaseStore) ApplyGitHubProjection(ctx context.Context, snapshot GitHubProjectionSnapshot) error {
	if err := validateGitHubProjectionSnapshot(snapshot); err != nil {
		return err
	}

	_, err := s.requireDB()
	if err != nil {
		return err
	}

	syncedAt := strings.TrimSpace(snapshot.SyncedAt)
	if syncedAt == "" {
		syncedAt = currentUTCTimestamp()
	}

	return s.RunInTx(ctx, func(ctx context.Context, tx BaseStore) error {
		db, err := tx.requireDB()
		if err != nil {
			return err
		}

		if err := upsertRepoConfig(ctx, db, snapshot.RepoConfig, syncedAt); err != nil {
			return err
		}
		for _, sprint := range snapshot.Sprints {
			if err := upsertSprintDefinition(ctx, db, sprint, syncedAt); err != nil {
				return err
			}
		}

		sprintIDs := make([]string, 0, len(snapshot.Sprints))
		for _, sprint := range snapshot.Sprints {
			sprintIDs = append(sprintIDs, sprint.SprintID)
		}
		taskIDs := make([]string, 0, len(snapshot.Tasks))
		for _, task := range snapshot.Tasks {
			taskIDs = append(taskIDs, task.TaskID)
		}

		if err := replaceTasksWithinSprints(ctx, db, sprintIDs, taskIDs); err != nil {
			return err
		}
		for _, task := range snapshot.Tasks {
			if err := upsertTaskDefinition(ctx, db, task, syncedAt); err != nil {
				return err
			}
		}
		if err := replaceTaskDependencies(ctx, db, sprintIDs, snapshot.TaskDependencies, syncedAt); err != nil {
			return err
		}
		if err := replacePullRequestsAndCIRuns(ctx, db, sprintIDs, snapshot.PullRequests, snapshot.CIRuns, syncedAt); err != nil {
			return err
		}
		if err := updateActivePullRequestRefs(ctx, db, snapshot.Tasks, snapshot.Sprints, snapshot.PullRequests); err != nil {
			return err
		}
		if err := updateFullReconcileState(ctx, db, snapshot, syncedAt); err != nil {
			return err
		}

		return nil
	})
}

func validateGitHubProjectionSnapshot(snapshot GitHubProjectionSnapshot) error {
	if strings.TrimSpace(snapshot.RepoConfig.GitHubOwner) == "" {
		return newValidationError("repo.github_owner is required")
	}
	if strings.TrimSpace(snapshot.RepoConfig.GitHubRepo) == "" {
		return newValidationError("repo.github_repo is required")
	}
	if strings.TrimSpace(snapshot.RepoConfig.DefaultBranch) == "" {
		return newValidationError("repo.default_branch is required")
	}

	sprintIDs := make(map[string]struct{}, len(snapshot.Sprints))
	taskIDs := make(map[string]struct{}, len(snapshot.Tasks))

	for _, sprint := range snapshot.Sprints {
		if strings.TrimSpace(sprint.SprintID) == "" {
			return newValidationError("sprint_id is required")
		}
		sprintIDs[sprint.SprintID] = struct{}{}
	}
	for _, task := range snapshot.Tasks {
		if strings.TrimSpace(task.TaskID) == "" {
			return newValidationError("task_id is required")
		}
		if _, ok := sprintIDs[task.SprintID]; !ok {
			return newValidationError("task %s references unknown sprint %s", task.TaskID, task.SprintID)
		}
		taskIDs[task.TaskID] = struct{}{}
	}
	for _, dependency := range snapshot.TaskDependencies {
		if _, ok := taskIDs[dependency.TaskID]; !ok {
			return newValidationError("dependency task %s is not projected", dependency.TaskID)
		}
		if _, ok := taskIDs[dependency.DependsOnTaskID]; !ok {
			return newValidationError("dependency target %s is not projected", dependency.DependsOnTaskID)
		}
	}
	for _, pr := range snapshot.PullRequests {
		if _, ok := sprintIDs[pr.SprintID]; !ok {
			return newValidationError("pull request %d references unknown sprint %s", pr.GitHubPRNumber, pr.SprintID)
		}
		if pr.TaskID != nil {
			if _, ok := taskIDs[*pr.TaskID]; !ok {
				return newValidationError("pull request %d references unknown task %s", pr.GitHubPRNumber, *pr.TaskID)
			}
		}
	}
	for _, run := range snapshot.CIRuns {
		if _, ok := sprintIDs[run.SprintID]; !ok {
			return newValidationError("ci run %d references unknown sprint %s", run.GitHubRunID, run.SprintID)
		}
		if run.TaskID != nil {
			if _, ok := taskIDs[*run.TaskID]; !ok {
				return newValidationError("ci run %d references unknown task %s", run.GitHubRunID, *run.TaskID)
			}
		}
	}

	return nil
}

func upsertRepoConfig(ctx context.Context, db bun.IDB, repo RepoConfigSnapshot, syncedAt string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO repo_config (
			id,
			github_owner,
			github_repo,
			default_branch,
			created_at,
			updated_at
		) VALUES (1, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			github_owner = excluded.github_owner,
			github_repo = excluded.github_repo,
			default_branch = excluded.default_branch,
			created_at = repo_config.created_at,
			updated_at = excluded.updated_at
	`,
		repo.GitHubOwner,
		repo.GitHubRepo,
		repo.DefaultBranch,
		syncedAt,
		syncedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert repo_config: %w", err)
	}
	return nil
}

func upsertSprintDefinition(ctx context.Context, db bun.IDB, sprint SprintDefinitionSnapshot, syncedAt string) error {
	doneWhenJSON, err := marshalStringList(sprint.DoneWhen)
	if err != nil {
		return err
	}

	needsHuman := sprint.NeedsHuman
	humanReason := normalizeOptionalString(sprint.HumanReason)
	if needsHuman && humanReason == nil {
		value := "needs-human label set on GitHub sprint issue"
		humanReason = &value
	}

	status := defaultProjectedStatus(sprint.ClosedAt)
	_, err = db.ExecContext(ctx, `
		INSERT INTO sprints (
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
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(sprint_id) DO UPDATE SET
			sequence_no = excluded.sequence_no,
			github_issue_number = excluded.github_issue_number,
			github_issue_node_id = excluded.github_issue_node_id,
			title = excluded.title,
			body_md = excluded.body_md,
			goal = excluded.goal,
			done_when_json = excluded.done_when_json,
			status = sprints.status,
			sprint_branch = sprints.sprint_branch,
			active_sprint_pr_number = sprints.active_sprint_pr_number,
			timeline_log_path = excluded.timeline_log_path,
			needs_human = CASE WHEN excluded.needs_human = 1 THEN 1 ELSE sprints.needs_human END,
			human_reason = CASE
				WHEN excluded.needs_human = 1 THEN excluded.human_reason
				ELSE sprints.human_reason
			END,
			opened_at = excluded.opened_at,
			closed_at = excluded.closed_at,
			last_issue_sync_at = excluded.last_issue_sync_at,
			created_at = sprints.created_at,
			updated_at = excluded.updated_at
	`,
		sprint.SprintID,
		sprint.SequenceNo,
		sprint.GitHubIssueNumber,
		sprint.GitHubIssueNodeID,
		sprint.Title,
		sprint.BodyMD,
		nullableText(sprint.Goal),
		doneWhenJSON,
		status,
		nil,
		nil,
		"logs/"+sprint.SprintID+".log",
		needsHuman,
		humanReason,
		nullableText(sprint.OpenedAt),
		nullableText(sprint.ClosedAt),
		syncedAt,
		syncedAt,
		syncedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert sprint %s: %w", sprint.SprintID, err)
	}
	return nil
}

func upsertTaskDefinition(ctx context.Context, db bun.IDB, task TaskDefinitionSnapshot, syncedAt string) error {
	acceptanceJSON, err := marshalStringList(task.AcceptanceCriteria)
	if err != nil {
		return err
	}
	outOfScopeJSON, err := marshalStringList(task.OutOfScope)
	if err != nil {
		return err
	}

	needsHuman := task.NeedsHuman
	humanReason := normalizeOptionalString(task.HumanReason)
	if needsHuman && humanReason == nil {
		value := "needs-human label set on GitHub task issue"
		humanReason = &value
	}

	status := defaultProjectedStatus(task.ClosedAt)
	_, err = db.ExecContext(ctx, `
		INSERT INTO tasks (
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
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			sprint_id = excluded.sprint_id,
			task_local_id = excluded.task_local_id,
			sequence_no = excluded.sequence_no,
			github_issue_number = excluded.github_issue_number,
			github_issue_node_id = excluded.github_issue_node_id,
			parent_github_issue_number = excluded.parent_github_issue_number,
			title = excluded.title,
			body_md = excluded.body_md,
			goal = excluded.goal,
			acceptance_criteria_json = excluded.acceptance_criteria_json,
			out_of_scope_json = excluded.out_of_scope_json,
			status = tasks.status,
			attempt_total = tasks.attempt_total,
			qa_fail_count = tasks.qa_fail_count,
			review_fail_count = tasks.review_fail_count,
			ci_fail_count = tasks.ci_fail_count,
			current_failure_fingerprint = tasks.current_failure_fingerprint,
			active_pr_number = tasks.active_pr_number,
			task_branch = tasks.task_branch,
			worktree_path = tasks.worktree_path,
			needs_human = CASE WHEN excluded.needs_human = 1 THEN 1 ELSE tasks.needs_human END,
			human_reason = CASE
				WHEN excluded.needs_human = 1 THEN excluded.human_reason
				ELSE tasks.human_reason
			END,
			opened_at = excluded.opened_at,
			closed_at = excluded.closed_at,
			last_issue_sync_at = excluded.last_issue_sync_at,
			created_at = tasks.created_at,
			updated_at = excluded.updated_at
	`,
		task.TaskID,
		task.SprintID,
		task.TaskLocalID,
		task.SequenceNo,
		task.GitHubIssueNumber,
		task.GitHubIssueNodeID,
		task.ParentGitHubIssueNumber,
		task.Title,
		task.BodyMD,
		nullableText(task.Goal),
		acceptanceJSON,
		outOfScopeJSON,
		status,
		0,
		0,
		0,
		0,
		nil,
		nil,
		nil,
		nil,
		needsHuman,
		humanReason,
		nullableText(task.OpenedAt),
		nullableText(task.ClosedAt),
		syncedAt,
		syncedAt,
		syncedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert task %s: %w", task.TaskID, err)
	}
	return nil
}

func replaceTasksWithinSprints(ctx context.Context, db bun.IDB, sprintIDs, taskIDs []string) error {
	if len(sprintIDs) == 0 {
		return nil
	}

	if _, err := db.NewDelete().
		Table("task_dependencies").
		Where("task_id IN (SELECT task_id FROM tasks WHERE sprint_id IN (?))", bun.List(sprintIDs)).
		Exec(ctx); err != nil {
		return fmt.Errorf("delete task dependencies for refreshed sprints: %w", err)
	}

	deleteQuery := db.NewDelete().
		Table("tasks").
		Where("sprint_id IN (?)", bun.List(sprintIDs))
	if len(taskIDs) > 0 {
		deleteQuery = deleteQuery.Where("task_id NOT IN (?)", bun.List(taskIDs))
	}
	if _, err := deleteQuery.Exec(ctx); err != nil {
		return fmt.Errorf("delete stale tasks for refreshed sprints: %w", err)
	}

	return nil
}

func replaceTaskDependencies(ctx context.Context, db bun.IDB, sprintIDs []string, dependencies []TaskDependencySnapshot, syncedAt string) error {
	if len(sprintIDs) == 0 {
		return nil
	}

	if _, err := db.NewDelete().
		Table("task_dependencies").
		Where("task_id IN (SELECT task_id FROM tasks WHERE sprint_id IN (?))", bun.List(sprintIDs)).
		Exec(ctx); err != nil {
		return fmt.Errorf("delete task dependencies: %w", err)
	}

	for _, dependency := range dependencies {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO task_dependencies (
				task_id,
				depends_on_task_id,
				source,
				created_at
			) VALUES (?, ?, ?, ?)
		`,
			dependency.TaskID,
			dependency.DependsOnTaskID,
			dependency.Source,
			syncedAt,
		); err != nil {
			return fmt.Errorf("insert task dependency %s -> %s: %w", dependency.TaskID, dependency.DependsOnTaskID, err)
		}
	}

	return nil
}

func replacePullRequestsAndCIRuns(ctx context.Context, db bun.IDB, sprintIDs []string, pullRequests []PullRequestSnapshot, ciRuns []CIRunSnapshot, syncedAt string) error {
	if len(sprintIDs) == 0 {
		return nil
	}

	if _, err := db.NewDelete().
		Table("ci_runs").
		Where("sprint_id IN (?)", bun.List(sprintIDs)).
		Exec(ctx); err != nil {
		return fmt.Errorf("delete ci_runs: %w", err)
	}
	if _, err := db.NewDelete().
		Table("pull_requests").
		Where("sprint_id IN (?)", bun.List(sprintIDs)).
		Exec(ctx); err != nil {
		return fmt.Errorf("delete pull_requests: %w", err)
	}

	for _, pr := range pullRequests {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO pull_requests (
				github_pr_number,
				github_pr_node_id,
				pr_kind,
				sprint_id,
				task_id,
				head_branch,
				base_branch,
				status,
				auto_merge_enabled,
				head_sha,
				url,
				opened_at,
				closed_at,
				merged_at,
				last_synced_at,
				created_at,
				updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			pr.GitHubPRNumber,
			pr.GitHubPRNodeID,
			pr.PRKind,
			pr.SprintID,
			pr.TaskID,
			pr.HeadBranch,
			pr.BaseBranch,
			pr.Status,
			pr.AutoMergeEnabled,
			normalizeOptionalString(&pr.HeadSHA),
			pr.URL,
			normalizeOptionalString(&pr.OpenedAt),
			normalizeOptionalString(&pr.ClosedAt),
			normalizeOptionalString(&pr.MergedAt),
			normalizeOptionalString(&pr.LastSyncedAt),
			syncedAt,
			syncedAt,
		); err != nil {
			return fmt.Errorf("insert pull request %d: %w", pr.GitHubPRNumber, err)
		}
	}

	for _, run := range ciRuns {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO ci_runs (
				github_run_id,
				sprint_id,
				task_id,
				github_pr_number,
				workflow_name,
				head_sha,
				status,
				conclusion,
				html_url,
				started_at,
				completed_at,
				last_synced_at,
				created_at,
				updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			run.GitHubRunID,
			run.SprintID,
			run.TaskID,
			run.GitHubPRNumber,
			normalizeOptionalString(&run.WorkflowName),
			normalizeOptionalString(&run.HeadSHA),
			run.Status,
			normalizeOptionalString(&run.Conclusion),
			run.HTMLURL,
			normalizeOptionalString(&run.StartedAt),
			normalizeOptionalString(&run.CompletedAt),
			normalizeOptionalString(&run.LastSyncedAt),
			syncedAt,
			syncedAt,
		); err != nil {
			return fmt.Errorf("insert ci run %d: %w", run.GitHubRunID, err)
		}
	}

	return nil
}

func updateActivePullRequestRefs(ctx context.Context, db bun.IDB, tasks []TaskDefinitionSnapshot, sprints []SprintDefinitionSnapshot, pullRequests []PullRequestSnapshot) error {
	openTaskPRs := make(map[string]PullRequestSnapshot)
	openSprintPRs := make(map[string]PullRequestSnapshot)
	for _, pr := range pullRequests {
		if pr.Status != "open" {
			continue
		}
		switch pr.PRKind {
		case "task":
			if pr.TaskID != nil {
				openTaskPRs[*pr.TaskID] = pr
			}
		case "sprint":
			openSprintPRs[pr.SprintID] = pr
		}
	}

	for _, task := range tasks {
		pr, ok := openTaskPRs[task.TaskID]
		var activePRNumber *int
		var taskBranch *string
		if ok {
			activePRNumber = &pr.GitHubPRNumber
			taskBranch = normalizeOptionalString(&pr.HeadBranch)
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE tasks
			SET
				active_pr_number = ?,
				task_branch = COALESCE(?, task_branch),
				updated_at = ?
			WHERE task_id = ?
		`, activePRNumber, taskBranch, currentUTCTimestamp(), task.TaskID); err != nil {
			return fmt.Errorf("update active task pr reference for %s: %w", task.TaskID, err)
		}
	}

	for _, sprint := range sprints {
		pr, ok := openSprintPRs[sprint.SprintID]
		var activePRNumber *int
		var sprintBranch *string
		if ok {
			activePRNumber = &pr.GitHubPRNumber
			sprintBranch = normalizeOptionalString(&pr.HeadBranch)
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE sprints
			SET
				active_sprint_pr_number = ?,
				sprint_branch = COALESCE(?, sprint_branch),
				updated_at = ?
			WHERE sprint_id = ?
		`, activePRNumber, sprintBranch, currentUTCTimestamp(), sprint.SprintID); err != nil {
			return fmt.Errorf("update active sprint pr reference for %s: %w", sprint.SprintID, err)
		}
	}

	return nil
}

func updateFullReconcileState(ctx context.Context, db bun.IDB, snapshot GitHubProjectionSnapshot, syncedAt string) error {
	valueJSON, err := json.Marshal(map[string]any{
		"reason":             snapshot.Reason,
		"synced_at":          syncedAt,
		"sprint_count":       len(snapshot.Sprints),
		"task_count":         len(snapshot.Tasks),
		"dependency_count":   len(snapshot.TaskDependencies),
		"pull_request_count": len(snapshot.PullRequests),
		"ci_run_count":       len(snapshot.CIRuns),
	})
	if err != nil {
		return fmt.Errorf("marshal full reconcile sync_state: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO sync_state (name, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`,
		"last_full_reconcile_at",
		string(valueJSON),
		syncedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert sync_state last_full_reconcile_at: %w", err)
	}
	return nil
}

func defaultProjectedStatus(closedAt string) string {
	if strings.TrimSpace(closedAt) != "" {
		return "done"
	}
	return "todo"
}

func marshalStringList(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}

	data, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("marshal string list: %w", err)
	}
	return string(data), nil
}

func nullableText(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
