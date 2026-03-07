package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestTaskStateStoreAppendEventDeduplicatesByIdempotencyKey(t *testing.T) {
	base, service := openTestBaseStore(t)

	firstResponse := base.ExecuteTaskStateStoreTool(context.Background(), TaskStateStoreRequest{
		Op: TaskStateStoreOpAppendEvent,
		Payload: mustJSON(t, AppendEventPayload{
			EventID:        "event-1",
			EntityType:     "task",
			EntityID:       "Sprint-01/Task-04",
			EventType:      "task.started",
			Source:         "leader",
			IdempotencyKey: "evt-1",
			PayloadJSON: map[string]any{
				"status": "in_progress",
			},
			OccurredAt: "2026-03-07T00:00:00Z",
		}),
	})
	if !firstResponse.OK {
		t.Fatalf("expected first append to succeed, got %#v", firstResponse.Error)
	}
	if firstResponse.Data == nil || firstResponse.Data.AppendEvent == nil {
		t.Fatal("expected append_event response payload")
	}
	if firstResponse.Data.AppendEvent.EventID != "event-1" {
		t.Fatalf("unexpected event_id: %s", firstResponse.Data.AppendEvent.EventID)
	}
	if firstResponse.Data.AppendEvent.Deduplicated {
		t.Fatal("expected first append not to be deduplicated")
	}

	secondResponse := base.ExecuteTaskStateStoreTool(context.Background(), TaskStateStoreRequest{
		Op: TaskStateStoreOpAppendEvent,
		Payload: mustJSON(t, AppendEventPayload{
			EventID:        "event-2",
			EntityType:     "task",
			EntityID:       "Sprint-01/Task-04",
			EventType:      "task.started",
			Source:         "leader",
			IdempotencyKey: "evt-1",
			PayloadJSON: map[string]any{
				"status": "in_progress",
			},
			OccurredAt: "2026-03-07T00:00:00Z",
		}),
	})
	if !secondResponse.OK {
		t.Fatalf("expected deduplicated append to succeed, got %#v", secondResponse.Error)
	}
	if secondResponse.Data == nil || secondResponse.Data.AppendEvent == nil {
		t.Fatal("expected append_event response payload")
	}
	if secondResponse.Data.AppendEvent.EventID != "event-1" {
		t.Fatalf("expected stored event_id to be returned, got %s", secondResponse.Data.AppendEvent.EventID)
	}
	if !secondResponse.Data.AppendEvent.Deduplicated {
		t.Fatal("expected duplicate append to be deduplicated")
	}

	if got := countRowsByValue(t, service, "events", "idempotency_key", "evt-1"); got != 1 {
		t.Fatalf("expected one event row after dedupe, got %d", got)
	}
}

func TestTaskStateStoreUpdateTaskStateOnlyTouchesProvidedFields(t *testing.T) {
	base, service := openTestBaseStore(t)
	insertSprintRow(t, service, sprintSeed{
		SprintID:          "Sprint-01",
		SequenceNo:        1,
		GitHubIssueNumber: 101,
		Status:            "in_progress",
		SprintBranch:      stringPtr("sprint-01"),
		NeedsHuman:        true,
		HumanReason:       stringPtr("waiting on review"),
	})
	insertTaskRow(t, service, taskSeed{
		TaskID:                    "Sprint-01/Task-04",
		SprintID:                  "Sprint-01",
		TaskLocalID:               "Task-04",
		SequenceNo:                4,
		GitHubIssueNumber:         201,
		ParentGitHubIssueNumber:   101,
		Status:                    "review_in_progress",
		AttemptTotal:              4,
		QAFailCount:               1,
		ReviewFailCount:           2,
		CIFailCount:               3,
		CurrentFailureFingerprint: stringPtr("fp-1"),
		ActivePRNumber:            intPtr(88),
		TaskBranch:                stringPtr("task-04"),
		WorktreePath:              stringPtr("/tmp/original"),
		NeedsHuman:                true,
		HumanReason:               stringPtr("waiting on review"),
	})

	updatedProjection, err := base.UpdateTaskState(context.Background(), UpdateTaskStatePayload{
		TaskID:       "Sprint-01/Task-04",
		Status:       "ci_in_progress",
		WorktreePath: stringPtr("/tmp/updated"),
	})
	if err != nil {
		t.Fatalf("update task state: %v", err)
	}

	if updatedProjection.Status != "ci_in_progress" {
		t.Fatalf("unexpected status: %s", updatedProjection.Status)
	}
	if updatedProjection.WorktreePath == nil || *updatedProjection.WorktreePath != "/tmp/updated" {
		t.Fatalf("unexpected worktree_path: %#v", updatedProjection.WorktreePath)
	}
	if updatedProjection.ActivePRNumber == nil || *updatedProjection.ActivePRNumber != 88 {
		t.Fatalf("expected active_pr_number to remain unchanged, got %#v", updatedProjection.ActivePRNumber)
	}
	if updatedProjection.TaskBranch == nil || *updatedProjection.TaskBranch != "task-04" {
		t.Fatalf("expected task_branch to remain unchanged, got %#v", updatedProjection.TaskBranch)
	}
	if !updatedProjection.NeedsHuman {
		t.Fatal("expected needs_human to remain unchanged")
	}
	if updatedProjection.HumanReason == nil || *updatedProjection.HumanReason != "waiting on review" {
		t.Fatalf("expected human_reason to remain unchanged, got %#v", updatedProjection.HumanReason)
	}

	row := loadTaskStateColumns(t, service, "Sprint-01/Task-04")
	if row.AttemptTotal != 4 || row.QAFailCount != 1 || row.ReviewFailCount != 2 || row.CIFailCount != 3 {
		t.Fatalf("expected counters to remain unchanged, got %+v", row)
	}
	if row.CurrentFailureFingerprint == nil || *row.CurrentFailureFingerprint != "fp-1" {
		t.Fatalf("expected failure fingerprint to remain unchanged, got %#v", row.CurrentFailureFingerprint)
	}

	loadResponse := base.ExecuteTaskStateStoreTool(context.Background(), TaskStateStoreRequest{
		Op:      TaskStateStoreOpLoadTaskProjection,
		Payload: mustJSON(t, LoadTaskProjectionPayload{TaskID: "Sprint-01/Task-04"}),
	})
	if !loadResponse.OK {
		t.Fatalf("expected load_task_projection to succeed, got %#v", loadResponse.Error)
	}
	if loadResponse.Data == nil || loadResponse.Data.LoadTaskProjection == nil {
		t.Fatal("expected load_task_projection response payload")
	}
	if loadResponse.Data.LoadTaskProjection.Status != "ci_in_progress" {
		t.Fatalf("unexpected loaded status: %s", loadResponse.Data.LoadTaskProjection.Status)
	}
}

func TestTaskStateStoreUpdateTaskStateClearsOptionalStringFieldsWhenBlank(t *testing.T) {
	base, service := openTestBaseStore(t)
	insertSprintRow(t, service, sprintSeed{
		SprintID:          "Sprint-01",
		SequenceNo:        1,
		GitHubIssueNumber: 101,
		Status:            "in_progress",
	})
	insertTaskRow(t, service, taskSeed{
		TaskID:                    "Sprint-01/Task-04",
		SprintID:                  "Sprint-01",
		TaskLocalID:               "Task-04",
		SequenceNo:                4,
		GitHubIssueNumber:         201,
		ParentGitHubIssueNumber:   101,
		Status:                    "review_in_progress",
		AttemptTotal:              2,
		CurrentFailureFingerprint: stringPtr("fp-1"),
		NeedsHuman:                true,
		HumanReason:               stringPtr("manual check"),
	})

	updatedProjection, err := base.UpdateTaskState(context.Background(), UpdateTaskStatePayload{
		TaskID:                    "Sprint-01/Task-04",
		Status:                    "pr_open",
		CurrentFailureFingerprint: stringPtr(""),
		NeedsHuman:                boolPtr(false),
		HumanReason:               stringPtr(""),
	})
	if err != nil {
		t.Fatalf("update task state: %v", err)
	}

	if updatedProjection.Status != "pr_open" {
		t.Fatalf("unexpected status: %s", updatedProjection.Status)
	}
	if updatedProjection.NeedsHuman {
		t.Fatalf("expected needs_human to clear, got %+v", updatedProjection)
	}
	if updatedProjection.HumanReason != nil {
		t.Fatalf("expected human_reason to clear, got %#v", updatedProjection.HumanReason)
	}

	row := loadTaskStateColumns(t, service, "Sprint-01/Task-04")
	if row.CurrentFailureFingerprint != nil {
		t.Fatalf("expected current_failure_fingerprint to clear, got %#v", row.CurrentFailureFingerprint)
	}
	if row.NeedsHuman {
		t.Fatalf("expected row needs_human to clear, got %+v", row)
	}
	if row.HumanReason != nil {
		t.Fatalf("expected row human_reason to clear, got %#v", row.HumanReason)
	}
}

func TestTaskStateStoreUpdateSprintStateOnlyTouchesProvidedFields(t *testing.T) {
	base, service := openTestBaseStore(t)
	insertSprintRow(t, service, sprintSeed{
		SprintID:             "Sprint-01",
		SequenceNo:           1,
		GitHubIssueNumber:    101,
		Status:               "sprint_pr_open",
		SprintBranch:         stringPtr("sprint-01"),
		ActiveSprintPRNumber: intPtr(55),
		NeedsHuman:           true,
		HumanReason:          stringPtr("manual check"),
	})

	updatedProjection, err := base.UpdateSprintState(context.Background(), UpdateSprintStatePayload{
		SprintID: "Sprint-01",
		Status:   "sprint_reviewing",
	})
	if err != nil {
		t.Fatalf("update sprint state: %v", err)
	}

	if updatedProjection.Status != "sprint_reviewing" {
		t.Fatalf("unexpected status: %s", updatedProjection.Status)
	}
	if updatedProjection.SprintBranch == nil || *updatedProjection.SprintBranch != "sprint-01" {
		t.Fatalf("expected sprint_branch to remain unchanged, got %#v", updatedProjection.SprintBranch)
	}
	if updatedProjection.ActiveSprintPRNumber == nil || *updatedProjection.ActiveSprintPRNumber != 55 {
		t.Fatalf("expected active_sprint_pr_number to remain unchanged, got %#v", updatedProjection.ActiveSprintPRNumber)
	}
	if !updatedProjection.NeedsHuman {
		t.Fatal("expected needs_human to remain unchanged")
	}
	if updatedProjection.HumanReason == nil || *updatedProjection.HumanReason != "manual check" {
		t.Fatalf("expected human_reason to remain unchanged, got %#v", updatedProjection.HumanReason)
	}
}

func TestTaskStateStoreEnqueueOutboxActionDeduplicatesAndLoadsCurrentPending(t *testing.T) {
	base, service := openTestBaseStore(t)

	firstResponse := base.ExecuteTaskStateStoreTool(context.Background(), TaskStateStoreRequest{
		Op: TaskStateStoreOpEnqueueOutboxAction,
		Payload: mustJSON(t, EnqueueOutboxActionPayload{
			ActionID:         "action-1",
			EntityType:       "task",
			EntityID:         "Sprint-01/Task-04",
			ActionType:       "comment_issue",
			GitHubTargetType: "issue",
			IdempotencyKey:   "outbox-1",
			RequestPayloadJSON: map[string]any{
				"body": "hello",
			},
		}),
	})
	if !firstResponse.OK {
		t.Fatalf("expected first enqueue to succeed, got %#v", firstResponse.Error)
	}
	if firstResponse.Data == nil || firstResponse.Data.EnqueueOutboxAction == nil {
		t.Fatal("expected enqueue_outbox_action response payload")
	}
	if firstResponse.Data.EnqueueOutboxAction.Deduplicated {
		t.Fatal("expected first enqueue not to be deduplicated")
	}

	secondResponse := base.ExecuteTaskStateStoreTool(context.Background(), TaskStateStoreRequest{
		Op: TaskStateStoreOpEnqueueOutboxAction,
		Payload: mustJSON(t, EnqueueOutboxActionPayload{
			ActionID:         "action-2",
			EntityType:       "task",
			EntityID:         "Sprint-01/Task-04",
			ActionType:       "comment_issue",
			GitHubTargetType: "issue",
			IdempotencyKey:   "outbox-1",
			RequestPayloadJSON: map[string]any{
				"body": "hello",
			},
		}),
	})
	if !secondResponse.OK {
		t.Fatalf("expected deduplicated enqueue to succeed, got %#v", secondResponse.Error)
	}
	if secondResponse.Data == nil || secondResponse.Data.EnqueueOutboxAction == nil {
		t.Fatal("expected enqueue_outbox_action response payload")
	}
	if secondResponse.Data.EnqueueOutboxAction.ActionID != "action-1" {
		t.Fatalf("expected stored action_id to be returned, got %s", secondResponse.Data.EnqueueOutboxAction.ActionID)
	}
	if !secondResponse.Data.EnqueueOutboxAction.Deduplicated {
		t.Fatal("expected duplicate enqueue to be deduplicated")
	}

	now := time.Now().UTC()
	insertOutboxActionRow(t, service, outboxSeed{
		ActionID:         "action-3",
		EntityType:       "task",
		EntityID:         "Sprint-01/Task-04",
		ActionType:       "close_issue",
		GitHubTargetType: "issue",
		IdempotencyKey:   "outbox-3",
		RequestPayload:   `{"close":true}`,
		Status:           "pending",
		NextAttemptAt:    stringPtr(now.Add(-10 * time.Minute).Format(time.RFC3339)),
		CreatedAt:        now.Add(-9 * time.Minute).Format(time.RFC3339),
		UpdatedAt:        now.Add(-9 * time.Minute).Format(time.RFC3339),
	})
	insertOutboxActionRow(t, service, outboxSeed{
		ActionID:         "action-4",
		EntityType:       "task",
		EntityID:         "Sprint-01/Task-04",
		ActionType:       "close_issue",
		GitHubTargetType: "issue",
		IdempotencyKey:   "outbox-4",
		RequestPayload:   `{"close":true}`,
		Status:           "pending",
		NextAttemptAt:    stringPtr(now.Add(10 * time.Minute).Format(time.RFC3339)),
		CreatedAt:        now.Add(-8 * time.Minute).Format(time.RFC3339),
		UpdatedAt:        now.Add(-8 * time.Minute).Format(time.RFC3339),
	})
	insertOutboxActionRow(t, service, outboxSeed{
		ActionID:         "action-5",
		EntityType:       "task",
		EntityID:         "Sprint-01/Task-04",
		ActionType:       "close_issue",
		GitHubTargetType: "issue",
		IdempotencyKey:   "outbox-5",
		RequestPayload:   `{"close":true}`,
		Status:           "running",
		NextAttemptAt:    stringPtr(now.Add(-5 * time.Minute).Format(time.RFC3339)),
		CreatedAt:        now.Add(-7 * time.Minute).Format(time.RFC3339),
		UpdatedAt:        now.Add(-7 * time.Minute).Format(time.RFC3339),
	})

	loadResponse := base.ExecuteTaskStateStoreTool(context.Background(), TaskStateStoreRequest{
		Op:      TaskStateStoreOpLoadPendingOutboxAction,
		Payload: mustJSON(t, LoadPendingOutboxActionsPayload{Limit: 10}),
	})
	if !loadResponse.OK {
		t.Fatalf("expected load_pending_outbox_actions to succeed, got %#v", loadResponse.Error)
	}
	if loadResponse.Data == nil || loadResponse.Data.LoadPendingOutboxActions == nil {
		t.Fatal("expected load_pending_outbox_actions response payload")
	}

	actions := loadResponse.Data.LoadPendingOutboxActions.Actions
	if len(actions) != 2 {
		t.Fatalf("expected 2 executable pending actions, got %d", len(actions))
	}
	if actions[0].ActionID != "action-1" || actions[1].ActionID != "action-3" {
		t.Fatalf("unexpected pending action order: %#v", actions)
	}
	if actions[0].RequestPayloadJSON["body"] != "hello" {
		t.Fatalf("expected request payload to be decoded, got %#v", actions[0].RequestPayloadJSON)
	}

	if got := countRowsByValue(t, service, "outbox_actions", "idempotency_key", "outbox-1"); got != 1 {
		t.Fatalf("expected one outbox row after dedupe, got %d", got)
	}
}

func openTestBaseStore(t *testing.T) (BaseStore, *Service) {
	t.Helper()

	repoRoot := newTestRepoRoot(t)
	service := New(Dependencies{})
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	if err := service.Open(context.Background(), OpenOptions{
		ConfigPath:   repoRoot + "/config/config.yaml",
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

type sprintSeed struct {
	SprintID             string
	SequenceNo           int
	GitHubIssueNumber    int
	Status               string
	SprintBranch         *string
	ActiveSprintPRNumber *int
	NeedsHuman           bool
	HumanReason          *string
}

func insertSprintRow(t *testing.T, service *Service, seed sprintSeed) {
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
		seed.SprintBranch,
		seed.ActiveSprintPRNumber,
		"logs/"+seed.SprintID+".log",
		boolToInt(seed.NeedsHuman),
		seed.HumanReason,
		nil,
		nil,
		nil,
		createdAt,
		createdAt,
	); err != nil {
		t.Fatalf("insert sprint row: %v", err)
	}
}

type taskSeed struct {
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
	CIFailCount               int
	CurrentFailureFingerprint *string
	ActivePRNumber            *int
	TaskBranch                *string
	WorktreePath              *string
	NeedsHuman                bool
	HumanReason               *string
}

func insertTaskRow(t *testing.T, service *Service, seed taskSeed) {
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
		seed.CIFailCount,
		seed.CurrentFailureFingerprint,
		seed.ActivePRNumber,
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

type outboxSeed struct {
	ActionID         string
	EntityType       string
	EntityID         string
	ActionType       string
	GitHubTargetType string
	IdempotencyKey   string
	RequestPayload   string
	Status           string
	NextAttemptAt    *string
	CreatedAt        string
	UpdatedAt        string
}

func insertOutboxActionRow(t *testing.T, service *Service, seed outboxSeed) {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	if _, err := db.ExecContext(
		context.Background(),
		`INSERT INTO outbox_actions (
			action_id,
			entity_type,
			entity_id,
			action_type,
			github_target_type,
			github_target_number,
			idempotency_key,
			request_payload_json,
			status,
			attempt_count,
			last_error,
			next_attempt_at,
			created_at,
			updated_at,
			completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seed.ActionID,
		seed.EntityType,
		seed.EntityID,
		seed.ActionType,
		seed.GitHubTargetType,
		nil,
		seed.IdempotencyKey,
		seed.RequestPayload,
		seed.Status,
		0,
		nil,
		seed.NextAttemptAt,
		seed.CreatedAt,
		seed.UpdatedAt,
		nil,
	); err != nil {
		t.Fatalf("insert outbox action row: %v", err)
	}
}

type taskStateColumns struct {
	AttemptTotal              int
	QAFailCount               int
	ReviewFailCount           int
	CIFailCount               int
	CurrentFailureFingerprint *string
	NeedsHuman                bool
	HumanReason               *string
}

func loadTaskStateColumns(t *testing.T, service *Service, taskID string) taskStateColumns {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	var row taskStateColumns
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT attempt_total, qa_fail_count, review_fail_count, ci_fail_count, current_failure_fingerprint, needs_human, human_reason
		FROM tasks WHERE task_id = ?`,
		taskID,
	).Scan(
		&row.AttemptTotal,
		&row.QAFailCount,
		&row.ReviewFailCount,
		&row.CIFailCount,
		&row.CurrentFailureFingerprint,
		&row.NeedsHuman,
		&row.HumanReason,
	); err != nil {
		t.Fatalf("load task state columns: %v", err)
	}
	return row
}

func countRowsByValue(t *testing.T, service *Service, table, column, value string) int {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	var count int
	query := "SELECT COUNT(*) FROM " + table + " WHERE " + column + " = ?"
	if err := db.QueryRowContext(context.Background(), query, value).Scan(&count); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	return count
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
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

func intPtr(value int) *int {
	return &value
}
